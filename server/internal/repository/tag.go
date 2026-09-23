package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// TagRepository 提供 tags 与 customer_tag_relations 的读写访问。
//
// 硬规则（03-DATABASE.md:69-85）：
//   - 标签只软删除（deleted_at）；删除标签不得级联删除历史 customer_tag_relations；
//   - customer_tag_relations(customer_id, tag_id) 唯一联合索引防止重复挂标签，
//     重复插入返回 ErrDuplicate（由调用方映射为 409）。
type TagRepository struct {
	db *gorm.DB
}

// NewTagRepository 构造标签仓储。
func NewTagRepository(db *gorm.DB) *TagRepository {
	return &TagRepository{db: db}
}

// List 返回全部未删除标签，按 id 升序。
func (r *TagRepository) List(ctx context.Context) ([]model.Tag, error) {
	var tags []model.Tag
	if err := r.db.WithContext(ctx).Order("id").Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

// ListByCustomer 返回客户关联的全部标签（含已软删除标签），按 tag id 升序。
//
// 使用 Unscoped 是刻意的：删除标签不级联删除历史 relation，
// 客户历史标签必须仍可显示（03-DATABASE.md:77）。
func (r *TagRepository) ListByCustomer(ctx context.Context, customerID int64) ([]model.Tag, error) {
	var tags []model.Tag
	err := r.db.WithContext(ctx).Unscoped().
		Joins("JOIN customer_tag_relations AS rel ON rel.tag_id = tags.id").
		Where("rel.customer_id = ?", customerID).
		Order("tags.id").
		Find(&tags).Error
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// FindByID 按主键查询未删除标签；不存在返回 ErrNotFound。
func (r *TagRepository) FindByID(ctx context.Context, id int64) (*model.Tag, error) {
	var tag model.Tag
	if err := r.db.WithContext(ctx).First(&tag, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &tag, nil
}

// Create 新增标签；唯一约束冲突返回 ErrDuplicate。
func (r *TagRepository) Create(ctx context.Context, tag *model.Tag) error {
	if err := r.db.WithContext(ctx).Create(tag).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// UpdateProfile 更新标签名称与颜色（调用方已确认标签存在）。
func (r *TagRepository) UpdateProfile(ctx context.Context, tag *model.Tag) error {
	err := r.db.WithContext(ctx).Model(&model.Tag{}).
		Where("id = ?", tag.ID).
		Updates(map[string]any{"name": tag.Name, "color": tag.Color}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicate
	}
	return err
}

// SoftDelete 软删除标签（只置 deleted_at）；不存在返回 ErrNotFound。
func (r *TagRepository) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Tag{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// AttachRelation 建立客户-标签关联；重复挂标签由唯一联合索引拒绝并返回 ErrDuplicate。
func (r *TagRepository) AttachRelation(ctx context.Context, customerID, tagID int64) error {
	err := r.db.WithContext(ctx).
		Create(&model.CustomerTagRelation{CustomerID: customerID, TagID: tagID}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicate
	}
	return err
}

// DetachRelation 解除客户-标签关联；未挂该标签时返回 ErrNotFound。
func (r *TagRepository) DetachRelation(ctx context.Context, customerID, tagID int64) error {
	res := r.db.WithContext(ctx).
		Where("customer_id = ? AND tag_id = ?", customerID, tagID).
		Delete(&model.CustomerTagRelation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
