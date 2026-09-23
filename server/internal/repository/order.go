package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// OrderListFilter 是 GET /orders 的查询条件（plan todo 22、04-API.md:127-135）。
//
// StartAt/EndAt 为 created_at 的半开区间 [StartAt, EndAt)，由 service 层按日期解析；
// 全部条件为空时返回全量订单（分页由 Offset/Limit 控制）。
type OrderListFilter struct {
	Status     string
	CustomerID int64
	EmployeeID int64
	StartAt    *time.Time
	EndAt      *time.Time
	Offset     int
	Limit      int
}

// OrderRow 是 orders 与客户/员工姓名的联表读取结果（列表/详情 DTO 需要）。
type OrderRow struct {
	model.Order
	CustomerName string
	EmployeeName string
}

// OrderRepository 提供 orders 表的读取访问。
//
// 订单禁止物理删除（退款/取消保留记录，AGENTS.md 第 5 节）；
// 本文件承载读取路径（客户详情、幂等查询、订单列表/详情）与事务内写入路径，
// 金额与流水由 service 层在事务内统一处理。
type OrderRepository struct {
	db *gorm.DB
}

// NewOrderRepository 构造订单仓储。
func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// ListByCustomer 按客户分页查询订单，时间倒序（最新在前）。
func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID int64, offset, limit int) ([]model.Order, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.Order{}).Where("customer_id = ?", customerID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []model.Order
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// FindByRequestID 按幂等键查询订单（ux_orders_request_id 唯一索引）；不存在返回 ErrNotFound。
func (r *OrderRepository) FindByRequestID(ctx context.Context, requestID string) (*model.Order, error) {
	var order model.Order
	if err := r.db.WithContext(ctx).Where("request_id = ?", requestID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &order, nil
}

// CreateTx 在事务内创建订单。
//
// 唯一索引冲突（request_id 或 order_no）返回 ErrDuplicate：
// service 层据此识别「同一幂等键并发重复提交」，改为返回原订单。
func (r *OrderRepository) CreateTx(ctx context.Context, tx Tx, order *model.Order) error {
	if err := tx.WithContext(ctx).Create(order).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// CreateItemsTx 在事务内批量创建订单明细（服务名与成交单价为快照）。
func (r *OrderRepository) CreateItemsTx(ctx context.Context, tx Tx, items []model.OrderItem) error {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Create(&items).Error
}

// ListItems 按订单查询明细，id 升序（与下单顺序一致）。
func (r *OrderRepository) ListItems(ctx context.Context, orderID int64) ([]model.OrderItem, error) {
	var items []model.OrderItem
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// orderSelect / orderJoins：订单列 + 客户/员工名左联。
//
// 历史订单必须显示下单时的客户/员工姓名，因此客户软删除不参与联表过滤
// （原始 JOIN 不带 deleted_at 条件）；联表只用于读取姓名，不改变 orders 结果集。
const (
	orderSelect = "orders.*, customers.name AS customer_name, employees.name AS employee_name"
	orderJoins  = "LEFT JOIN customers ON customers.id = orders.customer_id LEFT JOIN employees ON employees.id = orders.employee_id"
)

// List 按条件分页查询订单（含客户/员工名），时间倒序（最新在前，同秒按 id 倒序）。
func (r *OrderRepository) List(ctx context.Context, f OrderListFilter) ([]OrderRow, int64, error) {
	query := r.filtered(ctx, f)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []OrderRow
	err := query.Select(orderSelect).Joins(orderJoins).
		Order("orders.created_at DESC, orders.id DESC").
		Limit(f.Limit).
		Offset(f.Offset).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// FindRowByID 按主键查询订单（含客户/员工名）；不存在返回 ErrNotFound。
func (r *OrderRepository) FindRowByID(ctx context.Context, id int64) (*OrderRow, error) {
	var row OrderRow
	err := r.db.WithContext(ctx).Model(&model.Order{}).
		Select(orderSelect).
		Joins(orderJoins).
		Where("orders.id = ?", id).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// filtered 构造带全部过滤条件的查询（不联表，供 Count 复用）。
//
// 过滤列一律加 orders. 前缀：Find 阶段会左联 customers/employees，
// 而 status/created_at 等同名列在联表后必须限定表名（否则 ambiguous column）。
func (r *OrderRepository) filtered(ctx context.Context, f OrderListFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&model.Order{})
	if status := strings.TrimSpace(f.Status); status != "" {
		query = query.Where("orders.status = ?", status)
	}
	if f.CustomerID > 0 {
		query = query.Where("orders.customer_id = ?", f.CustomerID)
	}
	if f.EmployeeID > 0 {
		query = query.Where("orders.employee_id = ?", f.EmployeeID)
	}
	if f.StartAt != nil {
		query = query.Where("orders.created_at >= ?", *f.StartAt)
	}
	if f.EndAt != nil {
		query = query.Where("orders.created_at < ?", *f.EndAt)
	}
	return query
}
