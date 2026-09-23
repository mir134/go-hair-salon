package controller

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// BalanceAdjustmentController 提供管理员余额调整接口（04-API.md:176-193）。
//
// 权限仅 admin，由路由 RBAC 中间件强制（06 §7：后端是最终边界）；
// 账务事务语义（原子防负余额、流水、审计）在 service 层。
type BalanceAdjustmentController struct {
	adjustments *service.BalanceAdjustmentService
}

// NewBalanceAdjustmentController 构造余额调整控制器。
func NewBalanceAdjustmentController(adjustments *service.BalanceAdjustmentService) *BalanceAdjustmentController {
	return &BalanceAdjustmentController{adjustments: adjustments}
}

// balanceAdjustRequest 是 POST /customers/:id/balance-adjustments 请求体。
//
// amount_cents 为带符号整数分（正=增加，负=扣减）；reason 必填。
type balanceAdjustRequest struct {
	AmountCents int64  `json:"amount_cents"`
	Reason      string `json:"reason"`
}

// BalanceAdjustmentView 是余额调整响应 DTO（金额带符号，整数分）。
type BalanceAdjustmentView struct {
	TransactionID      int64     `json:"transaction_id"`
	CustomerID         int64     `json:"customer_id"`
	AmountCents        int64     `json:"amount_cents"`
	BalanceBeforeCents int64     `json:"balance_before_cents"`
	BalanceAfterCents  int64     `json:"balance_after_cents"`
	Reason             string    `json:"reason"`
	CreatedAt          time.Time `json:"created_at"`
}

// Adjust 处理 POST /api/v1/customers/:id/balance-adjustments（admin）：
// 写 balance_transactions(type=adjustment)；不产生订单、不计入营业额（04-API.md:192）。
func (h *BalanceAdjustmentController) Adjust(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req balanceAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	result, err := h.adjustments.Adjust(c.Request.Context(), service.BalanceAdjustmentInput{
		CustomerID:  id,
		AmountCents: req.AmountCents,
		Reason:      req.Reason,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	tx := result.Transaction
	Success(c, BalanceAdjustmentView{
		TransactionID:      tx.ID,
		CustomerID:         tx.CustomerID,
		AmountCents:        tx.AmountCents,
		BalanceBeforeCents: tx.BalanceBeforeCents,
		BalanceAfterCents:  tx.BalanceAfterCents,
		Reason:             tx.Remark,
		CreatedAt:          tx.CreatedAt,
	})
}
