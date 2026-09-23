package controller

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// CustomerDetailController 提供客户详情聚合接口（04-API.md:80-82）：
// 消费记录、余额流水、积分流水，均为分页 + 时间倒序。
//
// 权限为 both（04-API.md:72）；业务规则（客户不存在 → 404）在 service 层。
type CustomerDetailController struct {
	details *service.CustomerDetailService
}

// NewCustomerDetailController 构造客户详情控制器。
func NewCustomerDetailController(details *service.CustomerDetailService) *CustomerDetailController {
	return &CustomerDetailController{details: details}
}

// OrderView 是订单 DTO（客户详情「消费记录」）。
type OrderView struct {
	ID                  int64     `json:"id"`
	OrderNo             string    `json:"order_no"`
	CustomerID          int64     `json:"customer_id"`
	EmployeeID          *int64    `json:"employee_id"`
	OriginalAmountCents int64     `json:"original_amount_cents"`
	DiscountAmountCents int64     `json:"discount_amount_cents"`
	PaidAmountCents     int64     `json:"paid_amount_cents"`
	PaymentMethod       string    `json:"payment_method"`
	Status              string    `json:"status"`
	Remark              string    `json:"remark"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// BalanceTransactionView 是余额流水 DTO（金额带符号，整数分）。
type BalanceTransactionView struct {
	ID                 int64     `json:"id"`
	CustomerID         int64     `json:"customer_id"`
	Type               string    `json:"type"`
	AmountCents        int64     `json:"amount_cents"`
	BalanceBeforeCents int64     `json:"balance_before_cents"`
	BalanceAfterCents  int64     `json:"balance_after_cents"`
	ReferenceType      string    `json:"reference_type"`
	ReferenceID        *int64    `json:"reference_id"`
	OperatorID         *int64    `json:"operator_id"`
	Remark             string    `json:"remark"`
	CreatedAt          time.Time `json:"created_at"`
}

// PointsTransactionView 是积分流水 DTO（积分带符号）。
type PointsTransactionView struct {
	ID            int64     `json:"id"`
	CustomerID    int64     `json:"customer_id"`
	Type          string    `json:"type"`
	Points        int64     `json:"points"`
	BalanceBefore int64     `json:"balance_before"`
	BalanceAfter  int64     `json:"balance_after"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   *int64    `json:"reference_id"`
	OperatorID    *int64    `json:"operator_id"`
	Remark        string    `json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListOrders 处理 GET /api/v1/customers/:id/orders。
func (h *CustomerDetailController) ListOrders(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.details.ListOrders(c.Request.Context(), id, parsePageQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newOrderView)
}

// ListBalanceTransactions 处理 GET /api/v1/customers/:id/balance-transactions。
func (h *CustomerDetailController) ListBalanceTransactions(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.details.ListBalanceTransactions(c.Request.Context(), id, parsePageQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newBalanceTransactionView)
}

// ListPointsTransactions 处理 GET /api/v1/customers/:id/points-transactions。
func (h *CustomerDetailController) ListPointsTransactions(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.details.ListPointsTransactions(c.Request.Context(), id, parsePageQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newPointsTransactionView)
}

// newOrderView 把 model.Order 转为 DTO。
func newOrderView(order *model.Order) OrderView {
	return OrderView{
		ID:                  order.ID,
		OrderNo:             order.OrderNo,
		CustomerID:          order.CustomerID,
		EmployeeID:          order.EmployeeID,
		OriginalAmountCents: order.OriginalAmountCents,
		DiscountAmountCents: order.DiscountAmountCents,
		PaidAmountCents:     order.PaidAmountCents,
		PaymentMethod:       order.PaymentMethod,
		Status:              order.Status,
		Remark:              order.Remark,
		CreatedAt:           order.CreatedAt,
		UpdatedAt:           order.UpdatedAt,
	}
}

// newBalanceTransactionView 把 model.BalanceTransaction 转为 DTO。
func newBalanceTransactionView(tx *model.BalanceTransaction) BalanceTransactionView {
	return BalanceTransactionView{
		ID:                 tx.ID,
		CustomerID:         tx.CustomerID,
		Type:               tx.Type,
		AmountCents:        tx.AmountCents,
		BalanceBeforeCents: tx.BalanceBeforeCents,
		BalanceAfterCents:  tx.BalanceAfterCents,
		ReferenceType:      tx.ReferenceType,
		ReferenceID:        tx.ReferenceID,
		OperatorID:         tx.OperatorID,
		Remark:             tx.Remark,
		CreatedAt:          tx.CreatedAt,
	}
}

// newPointsTransactionView 把 model.PointsTransaction 转为 DTO。
func newPointsTransactionView(tx *model.PointsTransaction) PointsTransactionView {
	return PointsTransactionView{
		ID:            tx.ID,
		CustomerID:    tx.CustomerID,
		Type:          tx.Type,
		Points:        tx.Points,
		BalanceBefore: tx.BalanceBefore,
		BalanceAfter:  tx.BalanceAfter,
		ReferenceType: tx.ReferenceType,
		ReferenceID:   tx.ReferenceID,
		OperatorID:    tx.OperatorID,
		Remark:        tx.Remark,
		CreatedAt:     tx.CreatedAt,
	}
}
