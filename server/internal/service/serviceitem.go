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

// ServiceItemService 提供服务项目维护与新消费可用性校验（04-API.md:110-125、06-BUSINESS-RULES.md:13-18）。
//
// 硬规则：
//   - price_cents 一律整数分；
//   - 已产生订单的服务禁止物理删除，只能停用（DELETE → 422 引导停用）；
//   - 停用（status=0）服务不能用于新消费，但历史订单仍可查看；
//   - 订单保存服务名与成交单价快照，改价不影响历史订单。
type ServiceItemService struct {
	items      *repository.ServiceItemRepository
	categories *repository.ServiceCategoryRepository
}

// NewServiceItemService 构造服务项目服务。
func NewServiceItemService(items *repository.ServiceItemRepository, categories *repository.ServiceCategoryRepository) *ServiceItemService {
	return &ServiceItemService{items: items, categories: categories}
}

// ServiceItemListQuery 是 GET /services 查询输入。
type ServiceItemListQuery struct {
	CategoryID int64
	Status     *int
}

// ServiceItemInput 是服务创建/修改输入（DTO 与 Model 分离，04-API.md:300-302）。
//
// Status 为 nil 表示未提供：创建时默认启用，修改时保持原值。
type ServiceItemInput struct {
	CategoryID      int64
	Name            string
	PriceCents      int64
	DurationMinutes int
	Status          *int
	Remark          string
}

// ServiceItemView 是服务读取视图：服务本体 + 分类名（GET /services 含 category 信息）。
//
// 内嵌 model.Service 以便调用方直接访问 ID/Name/PriceCents 等字段。
type ServiceItemView struct {
	model.Service
	CategoryName string
}

// List 返回服务列表（按 id 升序，含分类名）。
func (s *ServiceItemService) List(ctx context.Context, q ServiceItemListQuery) ([]ServiceItemView, error) {
	rows, err := s.items.List(ctx, repository.ServiceItemListFilter{CategoryID: q.CategoryID, Status: q.Status})
	if err != nil {
		return nil, fmt.Errorf("查询服务列表失败: %w", err)
	}
	return newServiceItemViews(rows), nil
}

// Get 按主键读取服务（含分类名）；不存在（含已软删除）返回 404 服务不存在。
func (s *ServiceItemService) Get(ctx context.Context, id int64) (*ServiceItemView, error) {
	row, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	view := newServiceItemView(row)
	return &view, nil
}

// Create 创建服务（仅 admin 入口调用）：分类必须存在、价格 > 0（整数分）。
func (s *ServiceItemService) Create(ctx context.Context, in ServiceItemInput) (*ServiceItemView, error) {
	category, err := s.ensureCategory(ctx, in.CategoryID)
	if err != nil {
		return nil, err
	}
	name, err := validateServiceName(in.Name)
	if err != nil {
		return nil, err
	}
	if err := validateServicePrice(in.PriceCents); err != nil {
		return nil, err
	}
	if err := validateServiceDuration(in.DurationMinutes); err != nil {
		return nil, err
	}
	status, err := resolveStatus(in.Status, model.StatusEnabled)
	if err != nil {
		return nil, err
	}
	service := &model.Service{
		CategoryID:      category.ID,
		Name:            name,
		PriceCents:      in.PriceCents,
		DurationMinutes: in.DurationMinutes,
		Status:          status,
		Remark:          strings.TrimSpace(in.Remark),
	}
	if err := s.items.Create(ctx, service); err != nil {
		return nil, fmt.Errorf("创建服务失败: %w", err)
	}
	return &ServiceItemView{Service: *service, CategoryName: category.Name}, nil
}

// Update 修改服务（仅 admin 入口调用）：分类必须存在、价格 > 0；status 未提供时保持原值。
func (s *ServiceItemService) Update(ctx context.Context, id int64, in ServiceItemInput) (*ServiceItemView, error) {
	row, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	category, err := s.ensureCategory(ctx, in.CategoryID)
	if err != nil {
		return nil, err
	}
	name, err := validateServiceName(in.Name)
	if err != nil {
		return nil, err
	}
	if err := validateServicePrice(in.PriceCents); err != nil {
		return nil, err
	}
	if err := validateServiceDuration(in.DurationMinutes); err != nil {
		return nil, err
	}
	status, err := resolveStatus(in.Status, row.Status)
	if err != nil {
		return nil, err
	}
	service := row.Service
	service.CategoryID = category.ID
	service.Name = name
	service.PriceCents = in.PriceCents
	service.DurationMinutes = in.DurationMinutes
	service.Status = status
	service.Remark = strings.TrimSpace(in.Remark)
	if err := s.items.UpdateProfile(ctx, &service); err != nil {
		return nil, fmt.Errorf("更新服务失败: %w", err)
	}
	return &ServiceItemView{Service: service, CategoryName: category.Name}, nil
}

// Delete 删除服务（仅 admin 入口调用）：
//   - 服务不存在 → 404；
//   - 已产生订单（order_items 引用）→ 422，提示改为停用（06-BUSINESS-RULES.md:16）；
//   - 从未下单 → 软删除（只置 deleted_at，行保留）。
func (s *ServiceItemService) Delete(ctx context.Context, id int64) error {
	if _, err := s.find(ctx, id); err != nil {
		return err
	}
	count, err := s.items.CountOrderItems(ctx, id)
	if err != nil {
		return fmt.Errorf("统计服务订单明细失败: %w", err)
	}
	if count > 0 {
		return Validation("该服务已产生订单，不能删除，请改为停用")
	}
	if err := s.items.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "服务不存在")
		}
		return fmt.Errorf("删除服务失败: %w", err)
	}
	return nil
}

// EnsureEnabledForConsumption 校验服务可用于创建新消费（06-BUSINESS-RULES.md:18），
// 供消费事务（todo 21）在创建订单前调用：
//   - 服务不存在或已软删除 → 404 服务不存在；
//   - 服务已停用（status=0）→ 422，禁止用于新消费；
//   - 历史订单不受影响（订单保存服务名与成交单价快照）。
//
// 返回服务本体，供调用方读取标准价用于订单明细快照。
func (s *ServiceItemService) EnsureEnabledForConsumption(ctx context.Context, serviceID int64) (*model.Service, error) {
	row, err := s.find(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if row.Status != model.StatusEnabled {
		return nil, Validation("服务已停用，不能用于新消费")
	}
	service := row.Service
	return &service, nil
}

// find 读取服务（含分类名）；不存在返回 404 服务不存在。
func (s *ServiceItemService) find(ctx context.Context, id int64) (*repository.ServiceRow, error) {
	row, err := s.items.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "服务不存在")
		}
		return nil, fmt.Errorf("查询服务失败: %w", err)
	}
	return row, nil
}

// ensureCategory 校验分类存在；id 非法或不存在返回 400（请求体引用的分类无效）。
func (s *ServiceItemService) ensureCategory(ctx context.Context, categoryID int64) (*model.ServiceCategory, error) {
	if categoryID <= 0 {
		return nil, BadRequest("分类不能为空")
	}
	category, err := s.categories.FindByID(ctx, categoryID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, BadRequest("分类不存在")
		}
		return nil, fmt.Errorf("查询服务分类失败: %w", err)
	}
	return category, nil
}

// resolveStatus 解析 status 入参：nil 时保持 fallback（创建默认启用、修改保持原值），
// 非 nil 时校验取值合法。
func resolveStatus(in *int, fallback int) (int, error) {
	if in == nil {
		return fallback, nil
	}
	if err := validateStatusValue(*in); err != nil {
		return 0, err
	}
	return *in, nil
}

// validateServiceName 校验并规整服务名称（必填，≤128 字符，与 services.name size 一致）。
func validateServiceName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", BadRequest("服务名称不能为空")
	}
	if utf8.RuneCountInString(trimmed) > 128 {
		return "", BadRequest("服务名称不能超过 128 个字符")
	}
	return trimmed, nil
}

// validateServicePrice 校验标准价（整数分，必须大于 0；禁止浮点型金额）。
func validateServicePrice(priceCents int64) error {
	if priceCents <= 0 {
		return BadRequest("服务价格必须大于 0（整数分）")
	}
	return nil
}

// validateServiceDuration 校验服务时长（分钟，不得为负）。
func validateServiceDuration(minutes int) error {
	if minutes < 0 {
		return BadRequest("服务时长不能为负数")
	}
	return nil
}

// newServiceItemView 把仓储读取行转为服务视图。
func newServiceItemView(row *repository.ServiceRow) ServiceItemView {
	return ServiceItemView{Service: row.Service, CategoryName: row.CategoryName}
}

// newServiceItemViews 批量转换服务视图，保证空结果为 []。
func newServiceItemViews(rows []repository.ServiceRow) []ServiceItemView {
	views := make([]ServiceItemView, 0, len(rows))
	for i := range rows {
		views = append(views, newServiceItemView(&rows[i]))
	}
	return views
}
