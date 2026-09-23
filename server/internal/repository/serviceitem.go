package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// ServiceItemListFilter 是 GET /services 的查询条件（CategoryID > 0 过滤分类；Status 非 nil 过滤状态）。
type ServiceItemListFilter struct {
	CategoryID int64
	Status     *int
}

// ServiceRow 是 services 与分类名的联表读取结果（GET /services 必须含 category 信息）。
type ServiceRow struct {
	model.Service
	CategoryName string
}

// ServiceItemRepository 提供 services 表的读写访问（03-DATABASE.md:98-111）。
//
// 硬规则（06-BUSINESS-RULES.md:13-18）：
//   - services 只软删除（deleted_at），且 service 层保证「已产生订单的服务禁止删除」；
//   - 订单保存服务名与成交单价快照，改价/改名不影响历史订单。
type ServiceItemRepository struct {
	db *gorm.DB
}

// NewServiceItemRepository 构造服务项目仓储。
func NewServiceItemRepository(db *gorm.DB) *ServiceItemRepository {
	return &ServiceItemRepository{db: db}
}

// serviceItemSelect / serviceItemJoin：服务列 + 分类名左联（分类缺失时 category_name 为空，不阻塞读取）。
const (
	serviceItemSelect = "services.*, service_categories.name AS category_name"
	serviceItemJoin   = "LEFT JOIN service_categories ON service_categories.id = services.category_id"
)

// List 返回未软删除的服务（含分类名），按 id 升序。
func (r *ServiceItemRepository) List(ctx context.Context, f ServiceItemListFilter) ([]ServiceRow, error) {
	query := r.db.WithContext(ctx).Model(&model.Service{}).
		Select(serviceItemSelect).
		Joins(serviceItemJoin)
	if f.CategoryID > 0 {
		query = query.Where("services.category_id = ?", f.CategoryID)
	}
	if f.Status != nil {
		query = query.Where("services.status = ?", *f.Status)
	}
	var rows []ServiceRow
	if err := query.Order("services.id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByID 按主键查询未软删除服务（含分类名）；不存在返回 ErrNotFound。
func (r *ServiceItemRepository) FindByID(ctx context.Context, id int64) (*ServiceRow, error) {
	var row ServiceRow
	err := r.db.WithContext(ctx).Model(&model.Service{}).
		Select(serviceItemSelect).
		Joins(serviceItemJoin).
		Where("services.id = ?", id).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Create 新增服务。
//
// 与分类同理：status 带 gorm default:1，GORM 会把零值替换为默认值，
// 因此「创建即停用」在同一事务内插入后显式回写 status，保证语义正确且无中间态。
func (r *ServiceItemRepository) Create(ctx context.Context, service *model.Service) error {
	status := service.Status
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(service).Error; err != nil {
			return err
		}
		if status == model.StatusDisabled {
			return tx.Model(&model.Service{}).
				Where("id = ?", service.ID).
				UpdateColumn("status", status).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	service.Status = status
	return nil
}

// UpdateProfile 更新服务可写字段（调用方已确认服务存在）。
//
// 使用 map 更新：显式写出全部字段（含 status=0），不受默认值替换影响。
func (r *ServiceItemRepository) UpdateProfile(ctx context.Context, service *model.Service) error {
	return r.db.WithContext(ctx).Model(&model.Service{}).
		Where("id = ?", service.ID).
		Updates(map[string]any{
			"category_id":      service.CategoryID,
			"name":             service.Name,
			"price_cents":      service.PriceCents,
			"duration_minutes": service.DurationMinutes,
			"status":           service.Status,
			"remark":           service.Remark,
		}).Error
}

// SoftDelete 软删除服务（只置 deleted_at，行保留）；不存在返回 ErrNotFound。
//
// 调用方（service 层）必须先确认服务未产生订单（06-BUSINESS-RULES.md:16）。
func (r *ServiceItemRepository) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Service{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountOrderItems 统计引用该服务的订单明细行数（order_items 无软删除、只增不删）。
//
// > 0 表示服务已产生订单，禁止删除（只能停用）。
func (r *ServiceItemRepository) CountOrderItems(ctx context.Context, serviceID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.OrderItem{}).
		Where("service_id = ?", serviceID).
		Count(&count).Error
	return count, err
}
