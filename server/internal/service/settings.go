package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// SettingsService 管理系统设置（04-API.md:206-215、03-DATABASE.md:242-263、06 §6:67-76）。
//
// 硬规则：
//   - 可读可写的键仅限 MVP 公开键 shop_name / points_per_yuan；未知键不新增行（404）；
//   - points_per_yuan 必须是正整数（值校验），shop_name 非空且 ≤255 字符；
//   - 修改必须有登录上下文：updated_by 记录最后修改人（users.id）；
//   - 比例变更不追溯：积分在消费时按当时比例快照到 points_transactions，
//     历史流水与旧订单永不按新比例重算（06 §6:71-76）。
//
// 写 operation_logs 由 controller 在 service 返回成功之后执行（与既有 CRUD 一致）：
// 本服务只有一条单行 UPDATE，不存在业务事务，天然满足「日志在事务外」。
type SettingsService struct {
	settings *repository.SettingsRepository
}

// NewSettingsService 构造系统设置服务。
func NewSettingsService(settings *repository.SettingsRepository) *SettingsService {
	return &SettingsService{settings: settings}
}

// publicSettingKeys 是 GET /settings 可见、PUT /settings/:key 可改的键集合。
//
// 新增键必须同步 03-DATABASE.md 与 06-BUSINESS-RULES.md（03-DATABASE.md:263），
// 不在 MVP 设置 API 波次内擅自扩展。
var publicSettingKeys = map[string]struct{}{
	model.SettingShopName:      {},
	model.SettingPointsPerYuan: {},
}

// settingValueMaxRunes 与 settings.value 列宽（size:255）一致。
const settingValueMaxRunes = 255

// SettingView 是设置读取视图（DTO 与 Model 分离，04-API.md:300-302）。
type SettingView struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedBy   *int64    `json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SettingUpdateResult 是修改设置的结果：更新后的设置 + 修改前的旧值（审计留痕用）。
type SettingUpdateResult struct {
	Setting  *SettingView
	OldValue string
}

// List 返回公开设置键的现值（按 id 升序）；内部键不出现在响应中。
func (s *SettingsService) List(ctx context.Context) ([]SettingView, error) {
	rows, err := s.settings.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询系统设置失败: %w", err)
	}
	views := make([]SettingView, 0, len(rows))
	for i := range rows {
		if !isPublicSettingKey(rows[i].Key) {
			continue
		}
		views = append(views, newSettingView(&rows[i]))
	}
	return views, nil
}

// Update 修改指定设置项（仅 admin 路由可达，权限由 RBAC 中间件强制，06 §7）。
//
// 流程：键必须属于公开键集合（否则 404）→ 校验并规整值（非法 400）→
// 单行 UPDATE（设置 updated_by=当前操作人）→ 重读返回服务端真值。
func (s *SettingsService) Update(ctx context.Context, key, value string) (*SettingUpdateResult, error) {
	normalizedKey := strings.TrimSpace(key)
	if !isPublicSettingKey(normalizedKey) {
		return nil, NotFound(CodeNotFound, "设置项不存在")
	}
	normalizedValue, err := validateSettingValue(normalizedKey, value)
	if err != nil {
		return nil, err
	}

	current, err := s.settings.FindByKey(ctx, normalizedKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "设置项不存在")
		}
		return nil, fmt.Errorf("查询系统设置失败: %w", err)
	}

	if err := s.settings.UpdateValue(ctx, normalizedKey, normalizedValue, OperatorIDPtr(ctx)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "设置项不存在")
		}
		return nil, fmt.Errorf("更新系统设置失败: %w", err)
	}

	updated, err := s.settings.FindByKey(ctx, normalizedKey)
	if err != nil {
		return nil, fmt.Errorf("读取更新后的系统设置失败: %w", err)
	}
	view := newSettingView(updated)
	return &SettingUpdateResult{Setting: &view, OldValue: current.Value}, nil
}

// isPublicSettingKey 判断键是否属于公开设置键。
func isPublicSettingKey(key string) bool {
	_, ok := publicSettingKeys[key]
	return ok
}

// validateSettingValue 按键校验并规整设置值（返回落库的规范形式）：
//
//   - points_per_yuan：十进制正整数（0/负数/小数/非数字/溢出 → 400），
//     规整为无前导零的十进制串（如 "02" → "2"）；
//   - shop_name：去首尾空白后非空且 ≤255 字符（超出 settings.value 列宽 → 400）。
func validateSettingValue(key, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	switch key {
	case model.SettingPointsPerYuan:
		ratio, err := strconv.ParseInt(value, 10, 64)
		if err != nil || ratio <= 0 {
			return "", BadRequest("积分比例必须是正整数（每消费 1 元获得的积分）")
		}
		return strconv.FormatInt(ratio, 10), nil
	case model.SettingShopName:
		if value == "" {
			return "", BadRequest("门店名称不能为空")
		}
		if utf8.RuneCountInString(value) > settingValueMaxRunes {
			return "", BadRequest("门店名称不能超过 255 个字符")
		}
		return value, nil
	default:
		return "", NotFound(CodeNotFound, "设置项不存在")
	}
}

// newSettingView 把 model.Setting 转为读取视图。
func newSettingView(setting *model.Setting) SettingView {
	return SettingView{
		ID:          setting.ID,
		Key:         setting.Key,
		Value:       setting.Value,
		Description: setting.Description,
		UpdatedBy:   setting.UpdatedBy,
		UpdatedAt:   setting.UpdatedAt,
	}
}
