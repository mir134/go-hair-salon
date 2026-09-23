package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// OperationLogController 提供操作日志查询接口（04-API.md:237-243、plan todo 47）。
//
// 权限 admin（路由 RBAC 中间件，06 §7「staff 禁操作日志」）。
// 本接口只读：审计日志由 service.OperationLogService 在各业务事务提交后写入，
// 不提供任何修改/删除入口（AGENTS.md 第 5 节账务数据禁止物理删除）。
type OperationLogController struct {
	logs *service.OperationLogService
}

// NewOperationLogController 构造操作日志控制器。
func NewOperationLogController(logs *service.OperationLogService) *OperationLogController {
	return &OperationLogController{logs: logs}
}

// OperationLogView 是操作日志 DTO（03-DATABASE.md:265-277）：
// operator_name 为联表冗余（操作人停用后历史日志仍可读）；
// 不含任何敏感字段（password_hash 等从不落入本表，08-DEPLOYMENT.md:90-92）。
type OperationLogView struct {
	ID           int64     `json:"id"`
	OperatorID   *int64    `json:"operator_id"`
	OperatorName string    `json:"operator_name"`
	Action       string    `json:"action"`
	TargetType   string    `json:"target_type"`
	TargetID     int64     `json:"target_id"`
	Content      string    `json:"content"`
	IP           string    `json:"ip"`
	UserAgent    string    `json:"user_agent"`
	CreatedAt    time.Time `json:"created_at"`
}

// List 处理 GET /api/v1/operation-logs（admin）：
// operator_id、action、start_date/end_date 筛选 + page/page_size 分页，时间倒序。
//
// 非法过滤/日期/分页参数宽松回退（与订单/充值列表一致，不得 500）。
func (h *OperationLogController) List(c *gin.Context) {
	operatorID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("operator_id")), 10, 64)
	result, err := h.logs.ListOperationLogs(c.Request.Context(), service.OperationLogListQuery{
		OperatorID: operatorID,
		Action:     c.Query("action"),
		StartDate:  c.Query("start_date"),
		EndDate:    c.Query("end_date"),
		PageQuery:  parsePageQuery(c),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newOperationLogView)
}

// newOperationLogView 把操作日志读取行转为 DTO。
func newOperationLogView(row *service.OperationLogListRow) OperationLogView {
	return OperationLogView{
		ID:           row.ID,
		OperatorID:   row.OperatorID,
		OperatorName: row.OperatorName,
		Action:       row.Action,
		TargetType:   row.TargetType,
		TargetID:     row.TargetID,
		Content:      row.Content,
		IP:           row.IP,
		UserAgent:    row.UserAgent,
		CreatedAt:    row.CreatedAt,
	}
}
