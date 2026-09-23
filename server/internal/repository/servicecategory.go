package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// ServiceCategoryRepository 提供 service_categories 表的读写访问（03-DATABASE.md:87-96）。
//
// service_categories 没有 deleted_at 列：无服务的分类删除是物理删除；
// 分类下仍有服务（含已软删除服务）时由 service 层拒绝删除，避免产生
// category_id 悬空的孤儿服务行（06-BUSINESS-RULES.md:16）。
type ServiceCategoryRepository struct {
	db *gorm.DB
}

// NewServiceCategoryRepository 构造服务分类仓储。
func NewServiceCategoryRepository(db *gorm.DB) *ServiceCategoryRepository {
	return &ServiceCategoryRepository{db: db}
}

// List 返回分类列表，按 sort 升序、id 升序（sort 是展示顺序字段）。
//
// status 非 nil 时按启用状态过滤（0 停用 / 1 启用）。
func (r *ServiceCategoryRepository) List(ctx context.Context, status *int) ([]model.ServiceCategory, error) {
	query := r.db.WithContext(ctx).Model(&model.ServiceCategory{})
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var items []model.ServiceCategory
	if err := query.Order("sort ASC, id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindByID 按主键查询分类；不存在返回 ErrNotFound。
func (r *ServiceCategoryRepository) FindByID(ctx context.Context, id int64) (*model.ServiceCategory, error) {
	var category model.ServiceCategory
	if err := r.db.WithContext(ctx).First(&category, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &category, nil
}

// Create 新增分类。
//
// GORM 会把带声明默认值的零值字段替换为默认值（status default:1），
// 因此「创建即停用（status=0）」无法一次 Create 写入：在同一事务内
// 插入后再显式回写 status，保证语义正确且不产生中间态。
func (r *ServiceCategoryRepository) Create(ctx context.Context, category *model.ServiceCategory) error {
	status := category.Status
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(category).Error; err != nil {
			return err
		}
		if status == model.StatusDisabled {
			return tx.Model(&model.ServiceCategory{}).
				Where("id = ?", category.ID).
				UpdateColumn("status", status).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	category.Status = status
	return nil
}

// Update 更新分类名称/排序/状态（调用方已确认分类存在）。
func (r *ServiceCategoryRepository) Update(ctx context.Context, category *model.ServiceCategory) error {
	return r.db.WithContext(ctx).Model(&model.ServiceCategory{}).
		Where("id = ?", category.ID).
		Updates(map[string]any{
			"name":   category.Name,
			"sort":   category.Sort,
			"status": category.Status,
		}).Error
}

// Delete 物理删除分类；不存在返回 ErrNotFound。
//
// 调用方（service 层）必须先确认分类下没有服务行。
func (r *ServiceCategoryRepository) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.ServiceCategory{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountServicesInCategory 统计分类下的服务行数（Unscoped：含已软删除服务）。
//
// 分类删除前该值必须为 0：软删除的服务行仍引用 category_id，
// 删除分类会让这些行成为孤儿（06-BUSINESS-RULES.md:16 不孤儿化服务）。
func (r *ServiceCategoryRepository) CountServicesInCategory(ctx context.Context, categoryID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Unscoped().Model(&model.Service{}).
		Where("category_id = ?", categoryID).
		Count(&count).Error
	return count, err
}
