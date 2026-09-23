package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// 本文件承载挂单（pending）明细编辑接口（plan todo 26、04-API.md:136-149）：
//
//	POST   /orders/:id/items              追加服务项目（both）
//	PUT    /orders/:id/items/:item_id     修改数量 / 成交单价（both；改价仅 admin）
//	DELETE /orders/:id/items/:item_id     删除明细（both；软删除）
//
// 权限边界：路由为 both；改价（仅 admin）是业务规则，在 service 层强制（06 §3.1）。
// 所有接口返回更新后的订单详情（订单 + 明细快照），金额为整数分。

// orderItemAddRequest 是 POST /orders/:id/items 请求体（挂单追加明细）。
type orderItemAddRequest struct {
	ServiceID      int64  `json:"service_id"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	DiscountReason string `json:"discount_reason"`
}

// orderItemUpdateRequest 是 PUT /orders/:id/items/:item_id 请求体（挂单改数量/改价）。
//
// 数量与成交单价均可选（指针区分「未提供」与 0）；成交单价 0 表示恢复标准价。
type orderItemUpdateRequest struct {
	Quantity       *int   `json:"quantity"`
	UnitPriceCents *int64 `json:"unit_price_cents"`
	DiscountReason string `json:"discount_reason"`
}

// AddItem 处理 POST /api/v1/orders/:id/items（both）：挂单追加服务项目。
//
// 成功 → 201 + 更新后的订单详情；仅 pending 可操作（否则 409）。
func (h *OrderController) AddItem(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req orderItemAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	detail, err := h.orders.AddItem(c.Request.Context(), id, service.OrderItemAddInput{
		ServiceID:      req.ServiceID,
		Quantity:       req.Quantity,
		UnitPriceCents: req.UnitPriceCents,
		DiscountReason: req.DiscountReason,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	Created(c, newOrderDetailResponse(detail))
}

// UpdateItem 处理 PUT /api/v1/orders/:id/items/:item_id（both）：修改数量/成交单价。
//
// 成功 → 200 + 更新后的订单详情；仅 pending 可操作（否则 409）。
func (h *OrderController) UpdateItem(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	itemID, ok := parseIDParam(c, "item_id")
	if !ok {
		return
	}
	var req orderItemUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	detail, err := h.orders.UpdateItem(c.Request.Context(),
		service.OrderItemRef{OrderID: id, ItemID: itemID},
		service.OrderItemUpdateInput{
			Quantity:       req.Quantity,
			UnitPriceCents: req.UnitPriceCents,
			DiscountReason: req.DiscountReason,
		})
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}

// RemoveItem 处理 DELETE /api/v1/orders/:id/items/:item_id（both）：删除挂单明细（软删除）。
//
// 成功 → 200 + 更新后的订单详情；仅 pending 可操作（否则 409）。
func (h *OrderController) RemoveItem(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	itemID, ok := parseIDParam(c, "item_id")
	if !ok {
		return
	}
	detail, err := h.orders.RemoveItem(c.Request.Context(), service.OrderItemRef{OrderID: id, ItemID: itemID})
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}
