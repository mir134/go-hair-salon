package controller

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// TagController 提供标签 CRUD 与客户挂标签接口（04-API.md:97-108）。
//
// 权限边界由路由中间件保证：GET /tags both；POST/PUT/DELETE /tags 仅 admin；
// 客户挂/摘标签属于编辑客户（both）。
type TagController struct {
	tags *service.TagService
	logs *service.OperationLogService
}

// NewTagController 构造标签控制器。
func NewTagController(tags *service.TagService, logs *service.OperationLogService) *TagController {
	return &TagController{tags: tags, logs: logs}
}

// tagRequest 是 POST/PUT /tags 请求体。
type tagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// TagView 是标签 DTO；deleted 标记已软删除标签（客户历史标签仍会返回）。
type TagView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// attachTagRequest 是 POST /customers/:id/tags 请求体。
type attachTagRequest struct {
	TagID int64 `json:"tag_id"`
}

// List 处理 GET /api/v1/tags（both）：data 为标签数组（按 id 升序）。
func (h *TagController) List(c *gin.Context) {
	tags, err := h.tags.List(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newTagViews(tags))
}

// Create 处理 POST /api/v1/tags（仅 admin）。
func (h *TagController) Create(c *gin.Context) {
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	tag, err := h.tags.Create(c.Request.Context(), service.TagInput{Name: req.Name, Color: req.Color})
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeTagLog(c, "tag_create", tag.ID, fmt.Sprintf("新增标签 %s", tag.Name))
	Created(c, newTagView(tag))
}

// Update 处理 PUT /api/v1/tags/:id（仅 admin）。
func (h *TagController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	tag, err := h.tags.Update(c.Request.Context(), id, service.TagInput{Name: req.Name, Color: req.Color})
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeTagLog(c, "tag_update", tag.ID, fmt.Sprintf("修改标签 %s", tag.Name))
	Success(c, newTagView(tag))
}

// Delete 处理 DELETE /api/v1/tags/:id（仅 admin，软删除，不级联删历史关系）。
func (h *TagController) Delete(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.tags.Delete(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	h.writeTagLog(c, "tag_delete", id, fmt.Sprintf("删除标签 #%d", id))
	Success(c, gin.H{})
}

// AttachToCustomer 处理 POST /api/v1/customers/:id/tags（both）：挂标签，返回客户最新标签列表。
func (h *TagController) AttachToCustomer(c *gin.Context) {
	customerID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req attachTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	if req.TagID <= 0 {
		Fail(c, service.BadRequest("tag_id 不合法"))
		return
	}
	if err := h.tags.AttachToCustomer(c.Request.Context(), customerID, req.TagID); err != nil {
		Fail(c, err)
		return
	}
	h.writeCustomerTagLog(c, "tag_attach", customerID, fmt.Sprintf("客户 #%d 挂标签 #%d", customerID, req.TagID))
	h.successCustomerTags(c, customerID)
}

// DetachFromCustomer 处理 DELETE /api/v1/customers/:id/tags/:tag_id（both）。
func (h *TagController) DetachFromCustomer(c *gin.Context) {
	customerID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	tagID, ok := parseIDParam(c, "tag_id")
	if !ok {
		return
	}
	if err := h.tags.DetachFromCustomer(c.Request.Context(), customerID, tagID); err != nil {
		Fail(c, err)
		return
	}
	h.writeCustomerTagLog(c, "tag_detach", customerID, fmt.Sprintf("客户 #%d 摘标签 #%d", customerID, tagID))
	h.successCustomerTags(c, customerID)
}

// successCustomerTags 返回客户最新标签列表（含已软删除的历史标签）。
func (h *TagController) successCustomerTags(c *gin.Context, customerID int64) {
	tags, err := h.tags.ListCustomerTags(c.Request.Context(), customerID)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newTagViews(tags))
}

// writeTagLog 写「目标为标签」的审计日志（创建/修改/删除标签）。
func (h *TagController) writeTagLog(c *gin.Context, action string, tagID int64, content string) {
	h.writeAuditLog(c, "tag", action, tagID, content)
}

// writeCustomerTagLog 写「目标为客户」的审计日志：挂/摘标签改变的是客户的标签集合，
// 目标必须是客户（否则 target_id 会被误读为标签 id）。
func (h *TagController) writeCustomerTagLog(c *gin.Context, action string, customerID int64, content string) {
	h.writeAuditLog(c, "customer", action, customerID, content)
}

// writeAuditLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）。
func (h *TagController) writeAuditLog(c *gin.Context, targetType, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, targetType, targetID, content); err != nil {
		slog.Default().Error("写入标签审计日志失败", "target_type", targetType, "action", action, "err", err)
	}
}

// newTagView 把 model.Tag 转为 DTO。
func newTagView(tag *model.Tag) TagView {
	return TagView{
		ID:        tag.ID,
		Name:      tag.Name,
		Color:     tag.Color,
		Deleted:   tag.DeletedAt.Valid,
		CreatedAt: tag.CreatedAt,
		UpdatedAt: tag.UpdatedAt,
	}
}

// newTagViews 批量转换标签 DTO，保证空结果为 []。
func newTagViews(tags []model.Tag) []TagView {
	views := make([]TagView, 0, len(tags))
	for i := range tags {
		views = append(views, newTagView(&tags[i]))
	}
	return views
}
