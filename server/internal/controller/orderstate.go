package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// 本文件承载挂单的状态流转接口（plan todo 27-28,35、04-API.md:150-155）：
//
//	POST /orders/:id/pay     结账：pending → completed（both）
//	POST /orders/:id/cancel  取消：pending → cancelled（仅 admin）
//	POST /orders/:id/refund  退款：completed → refunded 全额退款（仅 admin）
//
// 权限边界：结账 both、取消/退款 admin（04-API.md:129,139,152）；业务状态规则在 service 层强制。

// orderPayRequest 是 POST /orders/:id/pay 请求体（结账时记录支付方式）。
type orderPayRequest struct {
	PaymentMethod string `json:"payment_method"`
}

// Pay 处理 POST /api/v1/orders/:id/pay（both）：挂单结账。
//
// 成功 → 200 + 结账后的订单详情；仅 pending 可结账（否则 409，防重复结账）。
func (h *OrderController) Pay(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req orderPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	detail, err := h.orders.Pay(c.Request.Context(), id, service.OrderPayInput{PaymentMethod: req.PaymentMethod})
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}

// Cancel 处理 POST /api/v1/orders/:id/cancel（admin）：取消挂单。
//
// 成功 → 200 + 取消后的订单详情；仅 pending 可取消（completed/refunded/cancelled → 422），
// 不产生任何资金/余额/积分变动（04-API.md:152-153、06 §3:37）。
func (h *OrderController) Cancel(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	detail, err := h.orders.Cancel(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}

// Refund 处理 POST /api/v1/orders/:id/refund（admin）：全额退款。
//
// 成功 → 200 + 退款后的订单详情（status=refunded）；
// 仅 completed 可退款（否则 409，含重复退款）；
// 余额支付订单复原余额并写反向余额流水，积分按原 earn 反向扣减（不足 → 422 整体回滚）。
func (h *OrderController) Refund(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	detail, err := h.orders.Refund(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}
