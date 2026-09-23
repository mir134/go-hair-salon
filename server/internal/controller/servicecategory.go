package controller

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// ServiceCategoryController 提供服务分类 CRUD（04-API.md:110-118）。
//
// 权限边界由路由中间件保证：GET both；POST/PUT/DELETE 仅 admin。
// 控制器只做参数解析、DTO 转换与审计日志，业务规则在 service 层。
type ServiceCategoryController struct {
	categories *service.ServiceCategoryService
	logs       *service.OperationLogService
}

// NewServiceCategoryController 构造服务分类控制器。
func NewServiceCategoryController(categories *service.ServiceCategoryService, logs *service.OperationLogService) *ServiceCategoryController {
	return &ServiceCategoryController{categories: categories, logs: logs}
}

// serviceCategoryRequest 是 POST/PUT /service-categories 请求体。
//
// status 用指针区分「未提供」与「显式停用（0）」。
type serviceCategoryRequest struct {
	Name   string `json:"name"`
	Sort   int    `json:"sort"`
	Status *int   `json:"status"`
}

// ServiceCategoryView 是服务分类 DTO（DTO 与 Model 分离，04-API.md:300-302）。
type ServiceCategoryView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Sort      int       `json:"sort"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// List 处理 GET /api/v1/service-categories（both）：按 sort 升序；
// ?status=0|1 可选过滤（非法值宽松回退为不过滤，不得 500）。
func (h *ServiceCategoryController) List(c *gin.Context) {
	items, err := h.categories.List(c.Request.Context(), parseStatusQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newServiceCategoryViews(items))
}

// Create 处理 POST /api/v1/service-categories（仅 admin）。
func (h *ServiceCategoryController) Create(c *gin.Context) {
	var req serviceCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	category, err := h.categories.Create(c.Request.Context(), service.ServiceCategoryInput{
		Name:   req.Name,
		Sort:   req.Sort,
		Status: req.Status,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_category_create", category.ID, fmt.Sprintf("新增服务分类 %s", category.Name))
	Created(c, newServiceCategoryView(category))
}

// Update 处理 PUT /api/v1/service-categories/:id（仅 admin）。
func (h *ServiceCategoryController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req serviceCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	category, err := h.categories.Update(c.Request.Context(), id, service.ServiceCategoryInput{
		Name:   req.Name,
		Sort:   req.Sort,
		Status: req.Status,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_category_update", category.ID, fmt.Sprintf("修改服务分类 %s", category.Name))
	Success(c, newServiceCategoryView(category))
}

// Delete 处理 DELETE /api/v1/service-categories/:id（仅 admin）：
// 分类下仍有服务时返回 422，提示先处理服务。
func (h *ServiceCategoryController) Delete(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.categories.Delete(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "service_category_delete", id, fmt.Sprintf("删除服务分类 #%d", id))
	Success(c, gin.H{})
}

// writeLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）。
func (h *ServiceCategoryController) writeLog(c *gin.Context, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, "service_category", targetID, content); err != nil {
		slog.Default().Error("写入服务分类审计日志失败", "action", action, "err", err)
	}
}

// parseStatusQuery 解析 ?status= 过滤参数：空值/非法值返回 nil（不过滤）。
//
// 与分页参数一致采用宽松回退：过滤参数问题不应让列表查询失败。
func parseStatusQuery(c *gin.Context) *int {
	raw := strings.TrimSpace(c.Query("status"))
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || (value != model.StatusEnabled && value != model.StatusDisabled) {
		return nil
	}
	return &value
}

// newServiceCategoryView 把 model.ServiceCategory 转为 DTO。
func newServiceCategoryView(category *model.ServiceCategory) ServiceCategoryView {
	return ServiceCategoryView{
		ID:        category.ID,
		Name:      category.Name,
		Sort:      category.Sort,
		Status:    category.Status,
		CreatedAt: category.CreatedAt,
		UpdatedAt: category.UpdatedAt,
	}
}

// newServiceCategoryViews 批量转换分类 DTO，保证空结果为 []。
func newServiceCategoryViews(categories []model.ServiceCategory) []ServiceCategoryView {
	views := make([]ServiceCategoryView, 0, len(categories))
	for i := range categories {
		views = append(views, newServiceCategoryView(&categories[i]))
	}
	return views
}
