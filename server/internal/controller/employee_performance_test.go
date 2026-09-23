package controller_test

// TestEmployeePerformance 是 todo 45 的验收测试（计划：`go test ./internal/controller -run TestEmployeePerformance -v -count=1`）：
//   - GET /dashboard/employee-performance（both）：按 order_items.employee_id 汇总 completed 明细
//     amount_cents（discount 后成交金额，整数分），明细无员工回退 orders.employee_id（D2 决议、plan:476）；
//   - 两员工各 1 明细 → 各自金额正确；订单级员工 + 明细级员工混合时归属正确；
//   - 退款按退款发生日（orders.updated_at）冲减业绩（与 06 §8:98 营业额冲减同口径）；
//   - 日期范围过滤正确（昨日订单不计入今日）；两处员工都为空 → employee_id=null 行；
//   - 软删除明细不计入；malformed 日期 → 400；未登录 → 401。
//
// 手工计算（种子见下，全部整数分；口径与 06 §8 营业额一致：
// 「completed 明细合计」−「refunded 明细按退款发生日冲减」，已退款订单不再计入 completed，不重复计入）：
//
//	今日（退款前）: 甲 = 6000(明细) + 4000(明细无员工→回退订单甲) + 8000(O4) = 18000；乙 = 7000；未分配 = 2500
//	今日（退款后）: 甲 = 10000(O1，O4 已不计入) − 8000(O4 当日冲减) = 2000；乙 = 7000；未分配 = 2500
//	昨日: 甲 = 9000（仅 O5）；[昨日,今日]: 甲 = 10000 + 9000 − 8000 = 11000；乙 = 7000；未分配 = 2500

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// employeePerformanceRowData 是员工业绩聚合行的测试镜像。
type employeePerformanceRowData struct {
	EmployeeID   *int64 `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
	AmountCents  int64  `json:"amount_cents"`
}

// employeePerformanceData 是 GET /dashboard/employee-performance data 的测试镜像。
type employeePerformanceData struct {
	StartDate string                       `json:"start_date"`
	EndDate   string                       `json:"end_date"`
	Items     []employeePerformanceRowData `json:"items"`
}

// performanceItemSeed 是业绩订单的单行明细种子。
type performanceItemSeed struct {
	employeeID  *int64
	amountCents int64
	softDeleted bool
}

// performanceOrderSeed 是业绩订单种子（订单 + 明细；金额整数分）。
type performanceOrderSeed struct {
	no         string
	requestID  string
	customerID int64
	employeeID *int64
	status     string
	createdAt  time.Time
	items      []performanceItemSeed
}

// seedPerformanceOrder 直接落库一张订单及其明细（业绩口径只看 order_items.amount_cents）。
func seedPerformanceOrder(t *testing.T, env *customerEnv, s performanceOrderSeed) model.Order {
	t.Helper()
	var total int64
	for _, item := range s.items {
		total += item.amountCents
	}
	order := model.Order{
		OrderNo:             s.no,
		RequestID:           s.requestID,
		CustomerID:          s.customerID,
		EmployeeID:          s.employeeID,
		OriginalAmountCents: total,
		PaidAmountCents:     total,
		PaymentMethod:       model.PaymentMethodCash,
		Status:              s.status,
		CreatedAt:           s.createdAt,
		UpdatedAt:           s.createdAt,
	}
	if err := env.db.Create(&order).Error; err != nil {
		t.Fatalf("落库业绩订单 %s 失败: %v", s.no, err)
	}
	for i, item := range s.items {
		row := model.OrderItem{
			OrderID:             order.ID,
			ServiceID:           1,
			ServiceNameSnapshot: "业绩服务",
			Quantity:            1,
			UnitPriceCents:      item.amountCents,
			AmountCents:         item.amountCents,
			EmployeeID:          item.employeeID,
			CreatedAt:           s.createdAt,
		}
		if err := env.db.Create(&row).Error; err != nil {
			t.Fatalf("落库业绩明细 %s#%d 失败: %v", s.no, i, err)
		}
		if item.softDeleted {
			// 挂单删明细只置 deleted_at（AGENTS.md 第 5 节禁止物理删除）：软删除明细不得计入业绩。
			if err := env.db.Delete(&row).Error; err != nil {
				t.Fatalf("软删除业绩明细 %s#%d 失败: %v", s.no, i, err)
			}
		}
	}
	return order
}

// getPerformance 拉取员工业绩报表（断言 200）。
func (e *customerEnv) getPerformance(t *testing.T, token, query string) employeePerformanceData {
	t.Helper()
	var report employeePerformanceData
	e.getDashboard(t, token, "/api/v1/dashboard/employee-performance"+query, &report)
	return report
}

// performanceRowByEmployee 取出指定员工的聚合行；缺失则失败。
func performanceRowByEmployee(t *testing.T, report employeePerformanceData, employeeID int64) employeePerformanceRowData {
	t.Helper()
	for _, row := range report.Items {
		if row.EmployeeID != nil && *row.EmployeeID == employeeID {
			return row
		}
	}
	t.Fatalf("员工业绩缺少 employee_id=%d 的行: %+v", employeeID, report.Items)
	return employeePerformanceRowData{}
}

// performanceUnassignedRow 取出 employee_id 为空（未分配）的聚合行；缺失则失败。
func performanceUnassignedRow(t *testing.T, report employeePerformanceData) employeePerformanceRowData {
	t.Helper()
	for _, row := range report.Items {
		if row.EmployeeID == nil {
			return row
		}
	}
	t.Fatalf("员工业绩缺少 employee_id=null 的未分配行: %+v", report.Items)
	return employeePerformanceRowData{}
}

func TestEmployeePerformance(t *testing.T) {
	env := newCustomerEnv(t)

	// 服务器本地日边界（UTC 存、本地统计；种子与查询窗口同口径 .UTC()）。
	now := time.Now()
	localToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	todayDate := localToday.Format("2006-01-02")
	yesterdayDate := localToday.AddDate(0, 0, -1).Format("2006-01-02")
	todayStart := localToday.UTC()
	yesterday := localToday.AddDate(0, 0, -1).UTC()

	empA := seedEmployee(t, env, "业绩员工甲")
	empB := seedEmployee(t, env, "业绩员工乙")
	cust := env.createCustomer(t, env.staffToken, `{"name":"业绩客户","phone":"13800007001"}`)

	// --- Given: 今日订单（O4 尚未退款） ---
	// O1：订单级员工=甲；明细 [甲 6000, 无员工 4000→回退订单甲]。
	seedPerformanceOrder(t, env, performanceOrderSeed{
		no: "PERF-O1", requestID: "req-perf-o1", customerID: cust.ID, employeeID: &empA.ID,
		status: model.OrderStatusCompleted, createdAt: todayStart.Add(8 * time.Hour),
		items: []performanceItemSeed{{employeeID: &empA.ID, amountCents: 6000}, {amountCents: 4000}},
	})
	// O2：订单级无员工；明细 [乙 7000, 乙 5000 软删除（不计入）]。
	seedPerformanceOrder(t, env, performanceOrderSeed{
		no: "PERF-O2", requestID: "req-perf-o2", customerID: cust.ID,
		status: model.OrderStatusCompleted, createdAt: todayStart.Add(9 * time.Hour),
		items: []performanceItemSeed{{employeeID: &empB.ID, amountCents: 7000}, {employeeID: &empB.ID, amountCents: 5000, softDeleted: true}},
	})
	// O3：订单级与明细级都无员工 → employee_id=null（未分配）。
	seedPerformanceOrder(t, env, performanceOrderSeed{
		no: "PERF-O3", requestID: "req-perf-o3", customerID: cust.ID,
		status: model.OrderStatusCompleted, createdAt: todayStart.Add(10 * time.Hour),
		items: []performanceItemSeed{{amountCents: 2500}},
	})
	// O5：昨日订单（日期范围过滤）。
	seedPerformanceOrder(t, env, performanceOrderSeed{
		no: "PERF-O5", requestID: "req-perf-o5", customerID: cust.ID, employeeID: &empA.ID,
		status: model.OrderStatusCompleted, createdAt: yesterday.Add(23 * time.Hour),
		items: []performanceItemSeed{{employeeID: &empA.ID, amountCents: 9000}},
	})

	t.Run("two_employees_and_unassigned_match_hand_computed", func(t *testing.T) {
		report := env.getPerformance(t, env.staffToken,
			fmt.Sprintf("?start_date=%s&end_date=%s", todayDate, todayDate))

		if report.StartDate != todayDate || report.EndDate != todayDate {
			t.Errorf("业绩范围回显 = %s~%s, want %s~%s", report.StartDate, report.EndDate, todayDate, todayDate)
		}
		if len(report.Items) != 3 {
			t.Fatalf("业绩行数 = %d, want 3 (items=%+v)", len(report.Items), report.Items)
		}
		rowA := performanceRowByEmployee(t, report, empA.ID)
		if rowA.AmountCents != 10000 || rowA.EmployeeName != "业绩员工甲" {
			t.Errorf("甲业绩 = %d/%q, want 10000/业绩员工甲（6000 明细 + 4000 回退订单员工）",
				rowA.AmountCents, rowA.EmployeeName)
		}
		rowB := performanceRowByEmployee(t, report, empB.ID)
		if rowB.AmountCents != 7000 || rowB.EmployeeName != "业绩员工乙" {
			t.Errorf("乙业绩 = %d/%q, want 7000/业绩员工乙（软删除明细 5000 不计入）",
				rowB.AmountCents, rowB.EmployeeName)
		}
		unassigned := performanceUnassignedRow(t, report)
		if unassigned.AmountCents != 2500 || unassigned.EmployeeName != "" {
			t.Errorf("未分配业绩 = %d/%q, want 2500/\"\"（明细与订单都无员工）",
				unassigned.AmountCents, unassigned.EmployeeName)
		}
		// 金额降序（未分配 2500 排最后）。
		if report.Items[0].EmployeeID == nil || *report.Items[0].EmployeeID != empA.ID ||
			report.Items[1].EmployeeID == nil || *report.Items[1].EmployeeID != empB.ID {
			t.Errorf("业绩排序 = %+v, want [甲 10000, 乙 7000, 未分配 2500]", report.Items)
		}
	})

	t.Run("refund_reduces_performance", func(t *testing.T) {
		// O4：今日 8000（明细=甲）→ 先计入，再经真实退款 API 冲减。
		o4 := seedPerformanceOrder(t, env, performanceOrderSeed{
			no: "PERF-O4", requestID: "req-perf-o4", customerID: cust.ID, employeeID: &empA.ID,
			status: model.OrderStatusCompleted, createdAt: todayStart.Add(11 * time.Hour),
			items: []performanceItemSeed{{employeeID: &empA.ID, amountCents: 8000}},
		})
		before := performanceRowByEmployee(t, env.getPerformance(t, env.staffToken,
			fmt.Sprintf("?start_date=%s&end_date=%s", todayDate, todayDate)), empA.ID)
		if before.AmountCents != 18000 {
			t.Fatalf("退款前甲业绩 = %d, want 18000（10000 + O4 8000）", before.AmountCents)
		}

		if w := env.refundOrder(env.adminToken, o4.ID); w.Code != http.StatusOK {
			t.Fatalf("订单退款 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}

		after := performanceRowByEmployee(t, env.getPerformance(t, env.staffToken,
			fmt.Sprintf("?start_date=%s&end_date=%s", todayDate, todayDate)), empA.ID)
		if after.AmountCents != 2000 {
			t.Errorf("退款后甲业绩 = %d, want 2000（18000 − O4 8000：已退款订单不再计入 completed 合计，且按退款发生日冲减 8000）",
				after.AmountCents)
		}
	})

	t.Run("date_range_filter", func(t *testing.T) {
		// 昨日仅 O5（甲 9000）；今日订单（含退款冲减后）不落入。
		yesterdayReport := env.getPerformance(t, env.staffToken,
			fmt.Sprintf("?start_date=%s&end_date=%s", yesterdayDate, yesterdayDate))
		if len(yesterdayReport.Items) != 1 {
			t.Fatalf("昨日业绩行数 = %d, want 1 (items=%+v)", len(yesterdayReport.Items), yesterdayReport.Items)
		}
		if rowA := performanceRowByEmployee(t, yesterdayReport, empA.ID); rowA.AmountCents != 9000 {
			t.Errorf("昨日甲业绩 = %d, want 9000", rowA.AmountCents)
		}

		// 跨两日：甲 = 10000（今日 O1）+ 9000（昨日 O5）− 8000（O4 退款冲减）= 11000。
		rangeReport := env.getPerformance(t, env.staffToken,
			fmt.Sprintf("?start_date=%s&end_date=%s", yesterdayDate, todayDate))
		if rowA := performanceRowByEmployee(t, rangeReport, empA.ID); rowA.AmountCents != 11000 {
			t.Errorf("跨两日甲业绩 = %d, want 11000", rowA.AmountCents)
		}
		if rowB := performanceRowByEmployee(t, rangeReport, empB.ID); rowB.AmountCents != 7000 {
			t.Errorf("跨两日乙业绩 = %d, want 7000", rowB.AmountCents)
		}
	})

	t.Run("malformed_input_and_auth", func(t *testing.T) {
		for _, query := range []string{
			"?start_date=2026-13-01",
			fmt.Sprintf("?start_date=%s&end_date=%s", todayDate, yesterdayDate),
		} {
			w := env.authed(http.MethodGet, "/api/v1/dashboard/employee-performance"+query, "", env.staffToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("GET /dashboard/employee-performance%s status = %d, want 400 (body=%s)",
					query, w.Code, w.Body.String())
				continue
			}
			if envl := decodeEnvelope(t, w); envl.Code != service.CodeInvalidParams {
				t.Errorf("GET /dashboard/employee-performance%s code = %d, want %d",
					query, envl.Code, service.CodeInvalidParams)
			}
		}
		if w := env.do(http.MethodGet, "/api/v1/dashboard/employee-performance", "", nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录 GET /dashboard/employee-performance status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
	})
}
