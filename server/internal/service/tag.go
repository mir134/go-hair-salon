package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// TagService 提供标签维护与客户-标签关联（04-API.md:97-108）。
//
// 权限边界由路由中间件保证：查询 both；创建/修改/删除 admin。
// 挂/摘标签属于「编辑客户」（both），由客户子资源路由承载。
// 重复挂标签返回 409（与重复手机号、重复用户名一致的冲突语义），
// 数据库唯一联合索引 ux_customer_tag_relations 是最终防线。
type TagService struct {
	tags      *repository.TagRepository
	customers *repository.CustomerRepository
}

// NewTagService 构造标签服务。
func NewTagService(tags *repository.TagRepository, customers *repository.CustomerRepository) *TagService {
	return &TagService{tags: tags, customers: customers}
}

// TagInput 是标签创建/修改输入。
type TagInput struct {
	Name  string
	Color string
}

// List 返回全部未删除标签（按 id 升序）。
func (s *TagService) List(ctx context.Context) ([]model.Tag, error) {
	tags, err := s.tags.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	return tags, nil
}

// Create 创建标签（仅 admin 入口调用）。
func (s *TagService) Create(ctx context.Context, in TagInput) (*model.Tag, error) {
	name, err := validateTagName(in.Name)
	if err != nil {
		return nil, err
	}
	tag := &model.Tag{Name: name, Color: strings.TrimSpace(in.Color)}
	if err := s.tags.Create(ctx, tag); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, Conflict("标签已存在")
		}
		return nil, fmt.Errorf("创建标签失败: %w", err)
	}
	return tag, nil
}

// Update 修改标签名称与颜色（仅 admin 入口调用）。
func (s *TagService) Update(ctx context.Context, id int64, in TagInput) (*model.Tag, error) {
	tag, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	name, err := validateTagName(in.Name)
	if err != nil {
		return nil, err
	}
	tag.Name = name
	tag.Color = strings.TrimSpace(in.Color)
	if err := s.tags.UpdateProfile(ctx, tag); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, Conflict("标签已存在")
		}
		return nil, fmt.Errorf("更新标签失败: %w", err)
	}
	return tag, nil
}

// Delete 软删除标签（仅 admin 入口调用）；不级联删除历史 relation。
func (s *TagService) Delete(ctx context.Context, id int64) error {
	if err := s.tags.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "标签不存在")
		}
		return fmt.Errorf("删除标签失败: %w", err)
	}
	return nil
}

// ListCustomerTags 返回客户的标签（含已软删除标签，供历史显示）。
//
// 纯读取：不校验客户是否存在（调用方在读取客户详情或挂/摘标签前已完成校验）。
func (s *TagService) ListCustomerTags(ctx context.Context, customerID int64) ([]model.Tag, error) {
	tags, err := s.tags.ListByCustomer(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("查询客户标签失败: %w", err)
	}
	return tags, nil
}

// AttachToCustomer 挂标签：客户或标签不存在 → 404；重复挂 → 409。
func (s *TagService) AttachToCustomer(ctx context.Context, customerID, tagID int64) error {
	if err := ensureCustomerExists(ctx, s.customers, customerID); err != nil {
		return err
	}
	if _, err := s.find(ctx, tagID); err != nil {
		return err
	}
	if err := s.tags.AttachRelation(ctx, customerID, tagID); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return Conflict("该标签已挂在此客户上")
		}
		return fmt.Errorf("挂标签失败: %w", err)
	}
	return nil
}

// DetachFromCustomer 摘标签：客户不存在或该客户未挂此标签 → 404。
func (s *TagService) DetachFromCustomer(ctx context.Context, customerID, tagID int64) error {
	if err := ensureCustomerExists(ctx, s.customers, customerID); err != nil {
		return err
	}
	if err := s.tags.DetachRelation(ctx, customerID, tagID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "该客户未挂此标签")
		}
		return fmt.Errorf("摘标签失败: %w", err)
	}
	return nil
}

// find 读取未删除标签；不存在返回 404 标签不存在。
func (s *TagService) find(ctx context.Context, id int64) (*model.Tag, error) {
	tag, err := s.tags.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "标签不存在")
		}
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	return tag, nil
}

// validateTagName 校验并规整标签名称（必填）。
func validateTagName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", BadRequest("标签名称不能为空")
	}
	return trimmed, nil
}
