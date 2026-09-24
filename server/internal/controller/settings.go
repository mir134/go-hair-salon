package controller

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// SettingsController 提供系统设置读写（04-API.md:206-215、07-UI.md:96-110）。
//
// 权限边界由路由中间件保证：GET /settings both（仅公开键）；PUT /settings/:key 仅 admin（06 §7）。
// 控制器只做参数解析、DTO 转换与审计日志，校验规则在 service 层。
type SettingsController struct {
	settings *service.SettingsService
	logs     *service.OperationLogService
}

// NewSettingsController 构造系统设置控制器。
func NewSettingsController(settings *service.SettingsService, logs *service.OperationLogService) *SettingsController {
	return &SettingsController{settings: settings, logs: logs}
}

// settingUpdateRequest 是 PUT /settings/:key 请求体：value 一律字符串（settings.value）。
type settingUpdateRequest struct {
	Value string `json:"value"`
}

// SettingView 是设置 DTO（settings 表快照，03-DATABASE.md:242-263）。
type SettingView struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedBy   *int64    `json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ShopInfoView 是登录页等未认证场景可见的门店公开信息（仅店名）。
type ShopInfoView struct {
	ShopName string `json:"shop_name"`
}

// PublicShopInfo 处理 GET /api/v1/shop（免认证）：仅返回门店名称。
//
// 登录页无 token，无法调用受保护的 GET /settings；店名属公开信息（顶栏/登录页展示），
// 故提供单独只读公开端点，不暴露其它设置项。
func (h *SettingsController) PublicShopInfo(c *gin.Context) {
	views, err := h.settings.List(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	name := model.DefaultShopName
	for i := range views {
		if views[i].Key == model.SettingShopName {
			name = views[i].Value
			break
		}
	}
	Success(c, ShopInfoView{ShopName: name})
}

// List 处理 GET /api/v1/settings（both）：data 为设置数组（仅公开键，空结果为 []）。
func (h *SettingsController) List(c *gin.Context) {
	views, err := h.settings.List(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newSettingViews(views))
}

// Update 处理 PUT /api/v1/settings/:key（仅 admin）：
// 未知键 → 404（不新增行）；非法值（比例非正整数、店名空白/超长）→ 400；
// 成功后写 operation_logs(action=setting_update)，日志在单行 UPDATE 之后（事务外）。
func (h *SettingsController) Update(c *gin.Context) {
	var req settingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	result, err := h.settings.Update(c.Request.Context(), c.Param("key"), req.Value)
	if err != nil {
		Fail(c, err)
		return
	}
	view := newSettingView(result.Setting)
	h.writeSettingLog(c, view, result.OldValue)
	Success(c, view)
}

// writeSettingLog 在设置更新完成后写审计日志（target=setting#id，operator 取自登录上下文）。
//
// 内容记录键名与旧/新值；日志写入失败不阻断已完成的设置更新。
func (h *SettingsController) writeSettingLog(c *gin.Context, view SettingView, oldValue string) {
	content := fmt.Sprintf("修改设置 %s 由「%s」改为「%s」", view.Key, oldValue, view.Value)
	if err := h.logs.WriteLog(c.Request.Context(), "setting_update", "setting", view.ID, content); err != nil {
		slog.Default().Error("写入设置审计日志失败", "key", view.Key, "err", err)
	}
}

// newSettingView 把 service 视图转为 DTO。
func newSettingView(view *service.SettingView) SettingView {
	return SettingView{
		ID:          view.ID,
		Key:         view.Key,
		Value:       view.Value,
		Description: view.Description,
		UpdatedBy:   view.UpdatedBy,
		UpdatedAt:   view.UpdatedAt,
	}
}

// newSettingViews 批量转换设置 DTO，保证空结果为 []。
func newSettingViews(views []service.SettingView) []SettingView {
	items := make([]SettingView, 0, len(views))
	for i := range views {
		items = append(items, newSettingView(&views[i]))
	}
	return items
}
