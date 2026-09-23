package controller

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// ServiceItemController 提供服务项目 CRUD（04-API.md:110-125）。
//
// 权限边界由路由中间件保证：GET both；POST/PUT/DELETE 仅 admin。
// 控制器只做参数解析、DTO 转换与审计日志，业务规则在 service 层。
type ServiceItemController struct {
	items *service.ServiceItemService
	logs  *service.OperationLogService
}

// NewServiceItemController 构造服务项目控制器。
func NewServiceItemController(items *service.ServiceItemService, logs *service.OperationLogService) *ServiceItemController {
	return &ServiceItemController{items: items, logs: logs}
}

// serviceItemRequest 是 POST/PUT /services 请求体。
//
// status 用指针区分「未提供」与「显式停用（0）」。
type serviceItemRequest struct {
	CategoryID      int64  `json:"category_id"`
	Name            string `json:"name"`
	PriceCents      int64  `json:"price_cents"`
	DurationMinutes int    `json:"duration_minutes"`
	Status          *int   `json:"status"`
	Remark          string `json:"remark"`
}

// ServiceView 是服务项目 DTO：price_cents 一律整数分，含分类名（DTO 与 Model 分离）。
type ServiceView struct {
	ID              int64     `json:"id"`
	CategoryID      int64     `json:"category_id"`
	CategoryName    string    `json:"category_name"`
	Name            string    `json:"name"`
	PriceCents      int64     `json:"price_cents"`
	DurationMinutes int       `json:"duration_minutes"`
	Status          int       `json:"status"`
	Remark          string    `json:"remark"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// List 处理 GET /api/v1/services（both）：含分类名；
// ?category_id= 与 ?status=0|1 可选过滤（非法值宽松回退，不得 500）。
func (h *ServiceItemController) List(c *gin.Context) {
	categoryID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("category_id")), 10, 64)
	items, err := h.items.List(c.Request.Context(), service.ServiceItemListQuery{
		CategoryID: categoryID,
		Status:     parseStatusQuery(c),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newServiceViews(items))
}

// Get 处理 GET /api/v1/services/:id（both）。
func (h *ServiceItemController) Get(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	item, err := h.items.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newServiceView(item))
}

// Create 处理 POST /api/v1/services（仅 admin）：价格必须大于 0（整数分）。
func (h *ServiceItemController) Create(c *gin.Context) {
	var req serviceItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	item, err := h.items.Create(c.Request.Context(), serviceItemInput(req))
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_create", item.ID, fmt.Sprintf("新增服务 %s", item.Name))
	Created(c, newServiceView(item))
}

// Update 处理 PUT /api/v1/services/:id（仅 admin）。
func (h *ServiceItemController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req serviceItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	item, err := h.items.Update(c.Request.Context(), id, serviceItemInput(req))
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_update", item.ID, fmt.Sprintf("修改服务 %s", item.Name))
	Success(c, newServiceView(item))
}

// Delete 处理 DELETE /api/v1/services/:id（仅 admin）：
// 已产生订单的服务返回 422，提示改为停用；从未下单的服务软删除。
func (h *ServiceItemController) Delete(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.items.Delete(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_delete", id, fmt.Sprintf("删除服务 #%d", id))
	Success(c, gin.H{})
}

// writeLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）。
func (h *ServiceItemController) writeLog(c *gin.Context, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, "service", targetID, content); err != nil {
		slog.Default().Error("写入服务审计日志失败", "action", action, "err", err)
	}
}

// serviceItemInput 把请求体映射为 service 输入。
func serviceItemInput(req serviceItemRequest) service.ServiceItemInput {
	return service.ServiceItemInput{
		CategoryID:      req.CategoryID,
		Name:            req.Name,
		PriceCents:      req.PriceCents,
		DurationMinutes: req.DurationMinutes,
		Status:          req.Status,
		Remark:          req.Remark,
	}
}

// newServiceView 把 service.ServiceItemView 转为 DTO。
func newServiceView(item *service.ServiceItemView) ServiceView {
	return ServiceView{
		ID:              item.ID,
		CategoryID:      item.CategoryID,
		CategoryName:    item.CategoryName,
		Name:            item.Name,
		PriceCents:      item.PriceCents,
		DurationMinutes: item.DurationMinutes,
		Status:          item.Status,
		Remark:          item.Remark,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

// newServiceViews 批量转换服务 DTO，保证空结果为 []。
func newServiceViews(items []service.ServiceItemView) []ServiceView {
	views := make([]ServiceView, 0, len(items))
	for i := range items {
		views = append(views, newServiceView(&items[i]))
	}
	return views
}
