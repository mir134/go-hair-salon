package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

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
