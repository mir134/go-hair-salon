package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// 本文件承载挂单的状态流转接口（plan todo 27、04-API.md:150-154）：
//
//	POST /orders/:id/pay     结账：pending → completed（both）
//
// 权限边界：路由为 both（04-API.md:129）；取消（admin）见 ordercancel.go。

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
