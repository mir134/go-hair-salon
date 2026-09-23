package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// ServiceCategoryService 提供服务分类维护（04-API.md:110-118）。
//
// 权限边界由路由中间件保证：查询 both；创建/修改/删除 admin。
// 删除约束：分类下仍有服务（含已软删除服务）时返回 422，提示先处理服务，
// 避免产生 category_id 悬空的孤儿服务行（06-BUSINESS-RULES.md:16）。
type ServiceCategoryService struct {
	categories *repository.ServiceCategoryRepository
}

// NewServiceCategoryService 构造服务分类服务。
func NewServiceCategoryService(categories *repository.ServiceCategoryRepository) *ServiceCategoryService {
	return &ServiceCategoryService{categories: categories}
}

// ServiceCategoryInput 是分类创建/修改输入。
//
// Status 为 nil 表示未提供：创建时默认启用，修改时保持原值。
type ServiceCategoryInput struct {
	Name   string
	Sort   int
	Status *int
}

// List 返回分类列表（按 sort 升序）；status 非 nil 时按状态过滤。
func (s *ServiceCategoryService) List(ctx context.Context, status *int) ([]model.ServiceCategory, error) {
	items, err := s.categories.List(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("查询服务分类失败: %w", err)
	}
	return items, nil
}

// Create 创建分类（仅 admin 入口调用）；未提供 status 时默认启用。
func (s *ServiceCategoryService) Create(ctx context.Context, in ServiceCategoryInput) (*model.ServiceCategory, error) {
	name, err := validateCategoryName(in.Name)
	if err != nil {
		return nil, err
	}
	status := model.StatusEnabled
	if in.Status != nil {
		if err := validateStatusValue(*in.Status); err != nil {
			return nil, err
		}
		status = *in.Status
	}
	category := &model.ServiceCategory{Name: name, Sort: in.Sort, Status: status}
	if err := s.categories.Create(ctx, category); err != nil {
		return nil, fmt.Errorf("创建服务分类失败: %w", err)
	}
	return category, nil
}

// Update 修改分类名称/排序/状态（仅 admin 入口调用）；status 未提供时保持原值。
func (s *ServiceCategoryService) Update(ctx context.Context, id int64, in ServiceCategoryInput) (*model.ServiceCategory, error) {
	category, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	name, err := validateCategoryName(in.Name)
	if err != nil {
		return nil, err
	}
	status := category.Status
	if in.Status != nil {
		if err := validateStatusValue(*in.Status); err != nil {
			return nil, err
		}
		status = *in.Status
	}
	category.Name = name
	category.Sort = in.Sort
	category.Status = status
	if err := s.categories.Update(ctx, category); err != nil {
		return nil, fmt.Errorf("更新服务分类失败: %w", err)
	}
	return category, nil
}

// Delete 删除分类（仅 admin 入口调用）：
//   - 分类不存在 → 404；
//   - 分类下仍有服务（含已软删除服务）→ 422，提示先处理服务（不孤儿化服务）。
func (s *ServiceCategoryService) Delete(ctx context.Context, id int64) error {
	if _, err := s.find(ctx, id); err != nil {
		return err
	}
	count, err := s.categories.CountServicesInCategory(ctx, id)
	if err != nil {
		return fmt.Errorf("统计分类下服务失败: %w", err)
	}
	if count > 0 {
		return Validation("该分类下仍有服务，请先转移或删除这些服务后再删除分类")
	}
	if err := s.categories.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "服务分类不存在")
		}
		return fmt.Errorf("删除服务分类失败: %w", err)
	}
	return nil
}

// find 读取分类；不存在返回 404 服务分类不存在。
func (s *ServiceCategoryService) find(ctx context.Context, id int64) (*model.ServiceCategory, error) {
	category, err := s.categories.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "服务分类不存在")
		}
		return nil, fmt.Errorf("查询服务分类失败: %w", err)
	}
	return category, nil
}

// validateCategoryName 校验并规整分类名称（必填，≤64 字符，与 service_categories.name size 一致）。
func validateCategoryName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", BadRequest("分类名称不能为空")
	}
	if utf8.RuneCountInString(trimmed) > 64 {
		return "", BadRequest("分类名称不能超过 64 个字符")
	}
	return trimmed, nil
}

// validateStatusValue 校验启用/停用状态取值（0 停用 / 1 启用）；
// 服务分类与服务项目共用同一枚举语义（03-DATABASE.md:93,106）。
func validateStatusValue(status int) error {
	if status != model.StatusEnabled && status != model.StatusDisabled {
		return BadRequest("状态取值必须为 0（停用）或 1（启用）")
	}
	return nil
}
