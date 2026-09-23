package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// OrderController 提供消费订单的创建与查询（04-API.md:127-155）。
//
// 权限边界由路由中间件保证：创建/查询 both（04-API.md:129）；
// 改价权限（仅 admin）是业务规则，在 service 层强制（06 §3.1）。
type OrderController struct {
	orders *service.OrderService
}

// NewOrderController 构造订单控制器。
func NewOrderController(orders *service.OrderService) *OrderController {
	return &OrderController{orders: orders}
}

// orderItemRequest 是 POST /orders 的单行明细（字段与 web/src/api/order.ts:51-56 对齐）。
type orderItemRequest struct {
	ServiceID      int64 `json:"service_id"`
	Quantity       int   `json:"quantity"`
	UnitPriceCents int64 `json:"unit_price_cents"`
}

// orderCreateRequest 是 POST /orders 请求体（字段与 web/src/api/order.ts:63-73 对齐）。
//
// status 为空/completed 表示直接完成收款，pending 表示挂单（不收款，04-API.md:145-149）。
type orderCreateRequest struct {
	RequestID      string             `json:"request_id"`
	CustomerID     int64              `json:"customer_id"`
	EmployeeID     *int64             `json:"employee_id"`
	PaymentMethod  string             `json:"payment_method"`
	Status         string             `json:"status"`
	DiscountReason string             `json:"discount_reason"`
	Items          []orderItemRequest `json:"items"`
}

// OrderItemView 是订单明细 DTO：服务名快照 + 整数分金额。
type OrderItemView struct {
	ID                  int64     `json:"id"`
	OrderID             int64     `json:"order_id"`
	ServiceID           int64     `json:"service_id"`
	ServiceNameSnapshot string    `json:"service_name_snapshot"`
	Quantity            int       `json:"quantity"`
	UnitPriceCents      int64     `json:"unit_price_cents"`
	DiscountAmountCents int64     `json:"discount_amount_cents"`
	AmountCents         int64     `json:"amount_cents"`
	EmployeeID          *int64    `json:"employee_id"`
	CreatedAt           time.Time `json:"created_at"`
}

// OrderDetailView 是订单详情 DTO：订单 + 明细快照（创建响应与 GET /orders/:id）。
type OrderDetailView struct {
	OrderView
	Items []OrderItemView `json:"items"`
}

// Create 处理 POST /api/v1/orders（both）：直接完成订单或挂单（status=pending）。
//
// 首次创建 → 201；同一 request_id 重复提交 → 200 + 原订单（幂等，04-API.md:283-286）。
func (h *OrderController) Create(c *gin.Context) {
	var req orderCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	result, err := h.orders.Create(c.Request.Context(), service.OrderCreateInput{
		RequestID:      req.RequestID,
		CustomerID:     req.CustomerID,
		EmployeeID:     req.EmployeeID,
		PaymentMethod:  req.PaymentMethod,
		Status:         req.Status,
		DiscountReason: req.DiscountReason,
		Items:          newOrderItemInputs(req.Items),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	view := newOrderCreateView(result)
	if result.Created {
		Created(c, view)
		return
	}
	Success(c, view)
}

// List 处理 GET /api/v1/orders（both）：
// status、customer_id、employee_id、start_date/end_date、page/page_size、sort=recent。
//
// 非法过滤/日期/分页参数宽松回退（与客户列表一致，不得 500）。
func (h *OrderController) List(c *gin.Context) {
	customerID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("customer_id")), 10, 64)
	employeeID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("employee_id")), 10, 64)
	result, err := h.orders.List(c.Request.Context(), service.OrderListQuery{
		Status:     strings.TrimSpace(c.Query("status")),
		CustomerID: customerID,
		EmployeeID: employeeID,
		StartDate:  c.Query("start_date"),
		EndDate:    c.Query("end_date"),
		Sort:       strings.TrimSpace(c.Query("sort")),
		PageQuery:  parsePageQuery(c),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newOrderRowView)
}

// Get 处理 GET /api/v1/orders/:id（both）：订单 + 明细快照；不存在 → 404。
func (h *OrderController) Get(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	detail, err := h.orders.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newOrderDetailResponse(detail))
}

// newOrderItemInputs 把请求明细映射为 service 输入。
func newOrderItemInputs(items []orderItemRequest) []service.OrderItemInput {
	inputs := make([]service.OrderItemInput, 0, len(items))
	for _, item := range items {
		inputs = append(inputs, service.OrderItemInput{
			ServiceID:      item.ServiceID,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		})
	}
	return inputs
}

// newNamedOrderView 把订单模型 + 客户/员工名转为 DTO（订单接口统一带名字）。
func newNamedOrderView(order *model.Order, customerName, employeeName string) OrderView {
	view := newOrderView(order)
	view.CustomerName = customerName
	view.EmployeeName = employeeName
	return view
}

// newOrderItemView 把订单明细模型转为 DTO。
func newOrderItemView(item *model.OrderItem) OrderItemView {
	return OrderItemView{
		ID:                  item.ID,
		OrderID:             item.OrderID,
		ServiceID:           item.ServiceID,
		ServiceNameSnapshot: item.ServiceNameSnapshot,
		Quantity:            item.Quantity,
		UnitPriceCents:      item.UnitPriceCents,
		DiscountAmountCents: item.DiscountAmountCents,
		AmountCents:         item.AmountCents,
		EmployeeID:          item.EmployeeID,
		CreatedAt:           item.CreatedAt,
	}
}

// newOrderItemViews 批量转换订单明细 DTO，保证空结果为 []。
func newOrderItemViews(items []model.OrderItem) []OrderItemView {
	views := make([]OrderItemView, 0, len(items))
	for i := range items {
		views = append(views, newOrderItemView(&items[i]))
	}
	return views
}

// newOrderCreateView 把创建结果转为详情 DTO（含明细快照与客户/员工名）。
func newOrderCreateView(result *service.OrderCreateResult) OrderDetailView {
	return OrderDetailView{
		OrderView: newNamedOrderView(result.Order, result.CustomerName, result.EmployeeName),
		Items:     newOrderItemViews(result.Items),
	}
}

// newOrderRowView 把订单列表读取行转为 DTO（含客户/员工名）。
func newOrderRowView(row *service.OrderListRow) OrderView {
	return newNamedOrderView(&row.Order, row.CustomerName, row.EmployeeName)
}

// newOrderDetailResponse 把订单详情聚合转为 DTO（订单 + 明细快照）。
func newOrderDetailResponse(detail *service.OrderDetail) OrderDetailView {
	return OrderDetailView{
		OrderView: newNamedOrderView(&detail.Order, detail.CustomerName, detail.EmployeeName),
		Items:     newOrderItemViews(detail.Items),
	}
}
