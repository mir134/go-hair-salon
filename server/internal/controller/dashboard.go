package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// DashboardController 提供 Dashboard 统计接口（04-API.md:217-236）。
//
// 权限 both（04-API.md:219）；统计口径与 06 §8 一致，日界=服务器本地时区（D3 决议）。
// 日期范围参数（start_date/end_date）为 YYYY-MM-DD（含当日），非法/倒置/超长 → 400。
type DashboardController struct {
	dashboard *service.DashboardService
}

// NewDashboardController 构造 Dashboard 控制器。
func NewDashboardController(dashboard *service.DashboardService) *DashboardController {
	return &DashboardController{dashboard: dashboard}
}

// dashboardSummaryView 是 GET /dashboard/summary data（金额一律整数分）。
type dashboardSummaryView struct {
	Date                   string         `json:"date"`
	RevenueCents           int64          `json:"revenue_cents"`
	CompletedOrderCount    int64          `json:"completed_order_count"`
	ConsumingCustomerCount int64          `json:"consuming_customer_count"`
	NewCustomerCount       int64          `json:"new_customer_count"`
	RechargeCents          int64          `json:"recharge_cents"`
	PendingOrderCount      int64          `json:"pending_order_count"`
	RecentOrders           []OrderView    `json:"recent_orders"`
	RecentRecharges        []RechargeView `json:"recent_recharges"`
}

// Summary 处理 GET /api/v1/dashboard/summary（both）：
// 今日 KPI（06 §8 口径）+ 最近消费/最近充值各 10 条；待结账单数为当前实时数据。
func (h *DashboardController) Summary(c *gin.Context) {
	summary, err := h.dashboard.Summary(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	orders := make([]OrderView, 0, len(summary.RecentOrders))
	for i := range summary.RecentOrders {
		orders = append(orders, newOrderRowView(&summary.RecentOrders[i]))
	}
	recharges := make([]RechargeView, 0, len(summary.RecentRecharges))
	for i := range summary.RecentRecharges {
		recharges = append(recharges, newRechargeRowView(&summary.RecentRecharges[i]))
	}
	Success(c, dashboardSummaryView{
		Date:                   summary.Date,
		RevenueCents:           summary.RevenueCents,
		CompletedOrderCount:    summary.CompletedOrderCount,
		ConsumingCustomerCount: summary.ConsumingCustomerCount,
		NewCustomerCount:       summary.NewCustomerCount,
		RechargeCents:          summary.RechargeCents,
		PendingOrderCount:      summary.PendingOrderCount,
		RecentOrders:           orders,
		RecentRecharges:        recharges,
	})
}

// revenuePointView 是营收序列的单日数据点。
type revenuePointView struct {
	Date         string `json:"date"`
	RevenueCents int64  `json:"revenue_cents"`
}

// revenueSeriesView 是 GET /dashboard/revenue data（回显解析后的实际日期范围）。
type revenueSeriesView struct {
	StartDate string             `json:"start_date"`
	EndDate   string             `json:"end_date"`
	Items     []revenuePointView `json:"items"`
}

// Revenue 处理 GET /api/v1/dashboard/revenue（both）：
// 日期范围内按服务器本地日切片的营收序列（start_date/end_date，缺省=本月 1 日~今日）。
func (h *DashboardController) Revenue(c *gin.Context) {
	series, err := h.dashboard.Revenue(c.Request.Context(), newDashboardRangeQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	items := make([]revenuePointView, 0, len(series.Items))
	for _, point := range series.Items {
		items = append(items, revenuePointView{Date: point.Date, RevenueCents: point.RevenueCents})
	}
	Success(c, revenueSeriesView{StartDate: series.StartDate, EndDate: series.EndDate, Items: items})
}

// customerPointView 是客户趋势序列的单日数据点。
type customerPointView struct {
	Date                   string `json:"date"`
	NewCustomerCount       int64  `json:"new_customer_count"`
	ConsumingCustomerCount int64  `json:"consuming_customer_count"`
}

// customerSeriesView 是 GET /dashboard/customers data（回显解析后的实际日期范围）。
type customerSeriesView struct {
	StartDate string              `json:"start_date"`
	EndDate   string              `json:"end_date"`
	Items     []customerPointView `json:"items"`
}

// Customers 处理 GET /api/v1/dashboard/customers（both）：
// 日期范围内按服务器本地日切片的「新增客户 / 消费人数」趋势。
func (h *DashboardController) Customers(c *gin.Context) {
	series, err := h.dashboard.Customers(c.Request.Context(), newDashboardRangeQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	items := make([]customerPointView, 0, len(series.Items))
	for _, point := range series.Items {
		items = append(items, customerPointView{
			Date:                   point.Date,
			NewCustomerCount:       point.NewCustomerCount,
			ConsumingCustomerCount: point.ConsumingCustomerCount,
		})
	}
	Success(c, customerSeriesView{StartDate: series.StartDate, EndDate: series.EndDate, Items: items})
}

// newDashboardRangeQuery 读取 Dashboard 日期范围查询参数（校验在 service 层）。
func newDashboardRangeQuery(c *gin.Context) service.DashboardRangeQuery {
	return service.DashboardRangeQuery{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
	}
}

// employeePerformanceView 是员工业绩聚合行（employee_id 为空 = 明细与订单都未指定员工）。
type employeePerformanceView struct {
	EmployeeID   *int64 `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
	AmountCents  int64  `json:"amount_cents"`
}

// employeePerformanceReportView 是 GET /dashboard/employee-performance data。
type employeePerformanceReportView struct {
	StartDate string                    `json:"start_date"`
	EndDate   string                    `json:"end_date"`
	Items     []employeePerformanceView `json:"items"`
}

// EmployeePerformance 处理 GET /api/v1/dashboard/employee-performance（both）：
// 日期范围内按员工汇总成交金额（明细无员工回退订单员工；退款按退款发生日冲减，plan todo 45）。
func (h *DashboardController) EmployeePerformance(c *gin.Context) {
	report, err := h.dashboard.EmployeePerformance(c.Request.Context(), newDashboardRangeQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	items := make([]employeePerformanceView, 0, len(report.Items))
	for _, row := range report.Items {
		items = append(items, employeePerformanceView{
			EmployeeID:   row.EmployeeID,
			EmployeeName: row.EmployeeName,
			AmountCents:  row.AmountCents,
		})
	}
	Success(c, employeePerformanceReportView{StartDate: report.StartDate, EndDate: report.EndDate, Items: items})
}
