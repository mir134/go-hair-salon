package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// RechargeController 提供充值创建接口（04-API.md:157-175）。
//
// 权限 both（04-API.md:159）；账务事务语义（幂等、双流水、余额原子累加）在 service 层。
type RechargeController struct {
	recharges *service.RechargeService
}

// NewRechargeController 构造充值控制器。
func NewRechargeController(recharges *service.RechargeService) *RechargeController {
	return &RechargeController{recharges: recharges}
}

// rechargeCreateRequest 是 POST /recharges 请求体。
//
// actual_amount_cents 缺省（null）表示实付 = 本金；存在充值优惠时可显式传入更小值。
type rechargeCreateRequest struct {
	RequestID           string `json:"request_id"`
	CustomerID          int64  `json:"customer_id"`
	RechargeAmountCents int64  `json:"recharge_amount_cents"`
	GiftAmountCents     int64  `json:"gift_amount_cents"`
	ActualAmountCents   *int64 `json:"actual_amount_cents"`
	PaymentMethod       string `json:"payment_method"`
	Remark              string `json:"remark"`
}

// RechargeView 是充值记录 DTO（金额一律整数分；customer_name 为联表冗余）。
type RechargeView struct {
	ID                  int64     `json:"id"`
	CustomerID          int64     `json:"customer_id"`
	CustomerName        string    `json:"customer_name"`
	RequestID           string    `json:"request_id"`
	RechargeAmountCents int64     `json:"recharge_amount_cents"`
	GiftAmountCents     int64     `json:"gift_amount_cents"`
	ActualAmountCents   int64     `json:"actual_amount_cents"`
	PaymentMethod       string    `json:"payment_method"`
	Status              string    `json:"status"`
	OperatorID          *int64    `json:"operator_id"`
	Remark              string    `json:"remark"`
	CreatedAt           time.Time `json:"created_at"`
}

// Create 处理 POST /api/v1/recharges（both）：
// 首次创建 → 201；同一 request_id 重复提交 → 200 + 原记录（幂等，04-API.md:277-299）。
func (h *RechargeController) Create(c *gin.Context) {
	var req rechargeCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	result, err := h.recharges.CreateRecharge(c.Request.Context(), service.RechargeInput{
		RequestID:           req.RequestID,
		CustomerID:          req.CustomerID,
		RechargeAmountCents: req.RechargeAmountCents,
		GiftAmountCents:     req.GiftAmountCents,
		ActualAmountCents:   req.ActualAmountCents,
		PaymentMethod:       req.PaymentMethod,
		Remark:              req.Remark,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	view := newRechargeView(result.Record, result.CustomerName)
	if result.Created {
		Created(c, view)
		return
	}
	Success(c, view)
}

// List 处理 GET /api/v1/recharges（both）：
// customer_id、start_date/end_date、page/page_size 筛选与分页。
//
// 非法过滤/日期/分页参数宽松回退（与订单列表一致，不得 500）。
func (h *RechargeController) List(c *gin.Context) {
	customerID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("customer_id")), 10, 64)
	result, err := h.recharges.ListRecharges(c.Request.Context(), service.RechargeListQuery{
		CustomerID: customerID,
		StartDate:  c.Query("start_date"),
		EndDate:    c.Query("end_date"),
		PageQuery:  parsePageQuery(c),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newRechargeRowView)
}

// newRechargeView 把充值记录模型转为 DTO。
func newRechargeView(record *model.RechargeRecord, customerName string) RechargeView {
	return RechargeView{
		ID:                  record.ID,
		CustomerID:          record.CustomerID,
		CustomerName:        customerName,
		RequestID:           record.RequestID,
		RechargeAmountCents: record.RechargeAmountCents,
		GiftAmountCents:     record.GiftAmountCents,
		ActualAmountCents:   record.ActualAmountCents,
		PaymentMethod:       record.PaymentMethod,
		Status:              record.Status,
		OperatorID:          record.OperatorID,
		Remark:              record.Remark,
		CreatedAt:           record.CreatedAt,
	}
}

// newRechargeRowView 把充值列表读取行转为 DTO（含客户名）。
func newRechargeRowView(row *service.RechargeListRow) RechargeView {
	return newRechargeView(&row.RechargeRecord, row.CustomerName)
}
