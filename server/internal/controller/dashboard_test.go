package controller_test

// TestDashboard 是 todo 44 的验收测试（计划：`go test ./internal/controller -run TestDashboard -v -count=1`）：
//   - GET /dashboard/summary（both）：06 §8 口径逐项与手工计算一致 ——
//     今日营业额 = 当日 completed 订单 paid 合计 − 当日退款冲减（退款发生日=orders.updated_at，todo 35 决议）；
//     今日消费单数 = 当日 completed 订单数；今日消费人数 = 当日 completed 订单去重客户数；
//     今日新增客户 = 当日创建客户数；今日充值金额 = 当日 recharge_records.actual 合计 −
//     当日冲正充值 actual 合计（recharge_records 无 updated_at，冲正日以反向余额流水 created_at 标记）；
//     待结账单数 = 当前 pending 总数（实时，不受日期范围影响）；+ 最近消费/最近充值各 10 条；
//   - GET /dashboard/revenue：按服务器本地日切片的日期范围序列（start_date/end_date）；
//   - GET /dashboard/customers：按日的新增/消费人数趋势；
//   - 日界 = 服务器本地时区（UTC 存、查询转本地）：跨午夜订单归属本地日（本机 UTC+8，
//     07:00 本地 = 前一日 23:00 UTC，UTC 日界实现会把它错记到昨天）；
//   - malformed_input：非法/倒置/超长日期范围 → 400；无分页语义（page 忽略）；未登录 → 401。
//
// 手工计算（种子见下，全部整数分）：
//
//	今日营业额   = (10000 + 5000 + 3000) − 6000 = 12000
//	今日消费单数 = 3；今日消费人数 = 2（甲去重）；今日新增客户 = 3（甲/乙/丁）
//	今日充值金额 = (10000 + 8000) − (8000 + 4000) = 6000
//	待结账单数   = 2（当前 pending 总数）
//	昨日营业额   = 20000（B1；23:59:59 本地归属昨日）

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// dashboardSummaryData 是 GET /dashboard/summary data 的测试镜像。
type dashboardSummaryData struct {
	Date                   string         `json:"date"`
	RevenueCents           int64          `json:"revenue_cents"`
	CompletedOrderCount    int64          `json:"completed_order_count"`
	ConsumingCustomerCount int64          `json:"consuming_customer_count"`
	NewCustomerCount       int64          `json:"new_customer_count"`
	RechargeCents          int64          `json:"recharge_cents"`
	PendingOrderCount      int64          `json:"pending_order_count"`
	RecentOrders           []orderAPIView `json:"recent_orders"`
	RecentRecharges        []rechargeView `json:"recent_recharges"`
}

// revenuePointData 是营收序列的单日数据点。
type revenuePointData struct {
	Date         string `json:"date"`
	RevenueCents int64  `json:"revenue_cents"`
}

// revenueSeriesData 是 GET /dashboard/revenue data 的测试镜像。
type revenueSeriesData struct {
	StartDate string             `json:"start_date"`
	EndDate   string             `json:"end_date"`
	Items     []revenuePointData `json:"items"`
}

// customerPointData 是客户趋势序列的单日数据点。
type customerPointData struct {
	Date                   string `json:"date"`
	NewCustomerCount       int64  `json:"new_customer_count"`
	ConsumingCustomerCount int64  `json:"consuming_customer_count"`
}

// customerSeriesData 是 GET /dashboard/customers data 的测试镜像。
type customerSeriesData struct {
	StartDate string              `json:"start_date"`
	EndDate   string              `json:"end_date"`
	Items     []customerPointData `json:"items"`
}

// getDashboard 拉取 Dashboard 端点（断言 200）并解码 data。
func (e *customerEnv) getDashboard(t *testing.T, token, path string, target any) {
	t.Helper()
	w := e.authed(http.MethodGet, path, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (body=%s)", path, w.Code, w.Body.String())
	}
	decodeData(t, decodeEnvelope(t, w), target)
}

// orderSeed 是直接落库的订单参数（Dashboard 测试需精确控制 created_at/updated_at）。
type orderSeed struct {
	no         string
	requestID  string
	customerID int64
	paidCents  int64
	status     string
	createdAt  time.Time
}

// seedDashboardOrder 直接落库一张订单（金额整数分；updated_at 初值=created_at）。
func seedDashboardOrder(t *testing.T, env *customerEnv, s orderSeed) model.Order {
	t.Helper()
	order := model.Order{
		OrderNo:             s.no,
		RequestID:           s.requestID,
		CustomerID:          s.customerID,
		OriginalAmountCents: s.paidCents,
		PaidAmountCents:     s.paidCents,
		PaymentMethod:       model.PaymentMethodCash,
		Status:              s.status,
		CreatedAt:           s.createdAt,
		UpdatedAt:           s.createdAt,
	}
	if err := env.db.Create(&order).Error; err != nil {
		t.Fatalf("落库订单 %s 失败: %v", s.no, err)
	}
	return order
}

// backdateCustomer 回填客户 created_at（构造「昨日创建」）。
func backdateCustomer(t *testing.T, env *customerEnv, id int64, at time.Time) {
	t.Helper()
	if err := env.db.Model(&model.Customer{}).Where("id = ?", id).Update("created_at", at).Error; err != nil {
		t.Fatalf("回填客户 %d created_at 失败: %v", id, err)
	}
}

// backdateRecharge 回填充值记录 created_at（构造「昨日充值」）。
func backdateRecharge(t *testing.T, env *customerEnv, id int64, at time.Time) {
	t.Helper()
	if err := env.db.Model(&model.RechargeRecord{}).Where("id = ?", id).Update("created_at", at).Error; err != nil {
		t.Fatalf("回填充值 %d created_at 失败: %v", id, err)
	}
}

func TestDashboard(t *testing.T) {
	env := newCustomerEnv(t)

	// 服务器本地日边界（D3 决议：UTC 存、按服务器本地时区统计）。
	// 数据库统一存 UTC（repository.NowFunc 与服务层均写 time.Now().UTC()），
	// 因此种子时间一律 .UTC()，与查询窗口（UTC 半开区间）同口径。
	now := time.Now()
	localToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	todayDate := localToday.Format("2006-01-02")
	yesterdayDate := localToday.AddDate(0, 0, -1).Format("2006-01-02")
	todayStart := localToday.UTC()
	yesterday := localToday.AddDate(0, 0, -1).UTC()

	// --- Given: 客户（甲/乙/丁今日创建；丙/戊回填昨日 → 不计入今日新增） ---
	custA := env.createCustomer(t, env.staffToken, `{"name":"看板客户甲","phone":"13800006001"}`)
	custB := env.createCustomer(t, env.staffToken, `{"name":"看板客户乙","phone":"13800006002"}`)
	custC := env.createCustomer(t, env.staffToken, `{"name":"看板客户丙","phone":"13800006003"}`)
	custD := env.createCustomer(t, env.staffToken, `{"name":"看板客户丁","phone":"13800006004"}`)
	custE := env.createCustomer(t, env.staffToken, `{"name":"看板客户戊","phone":"13800006005"}`)
	backdateCustomer(t, env, custC.ID, yesterday.Add(time.Hour))
	backdateCustomer(t, env, custE.ID, yesterday.Add(time.Hour))

	// --- Given: 订单（直接落库以精确控制 created_at） ---
	// 甲：今日两单（10000 + 5000）→ 消费人数去重后甲只算 1 人。
	ordA1 := seedDashboardOrder(t, env, orderSeed{"DASH-A1", "req-dash-a1", custA.ID, 10000, model.OrderStatusCompleted, todayStart.Add(7 * time.Hour)})
	ordA2 := seedDashboardOrder(t, env, orderSeed{"DASH-A2", "req-dash-a2", custA.ID, 5000, model.OrderStatusCompleted, todayStart.Add(9*time.Hour + 30*time.Minute)})
	// 乙：昨日 23:59:59（本地）与今日 00:00:01（本地）各一单 → 验证本地日边界。
	ordB1 := seedDashboardOrder(t, env, orderSeed{"DASH-B1", "req-dash-b1", custB.ID, 20000, model.OrderStatusCompleted, yesterday.Add(23*time.Hour + 59*time.Minute + 59*time.Second)})
	ordB2 := seedDashboardOrder(t, env, orderSeed{"DASH-B2", "req-dash-b2", custB.ID, 3000, model.OrderStatusCompleted, todayStart.Add(time.Second)})
	// 丁：今日 6000，经真实退款 API 冲减（updated_at=今天）→ 不计入 completed 合计，今日退款冲减 6000。
	ordD1 := seedDashboardOrder(t, env, orderSeed{"DASH-D1", "req-dash-d1", custD.ID, 6000, model.OrderStatusCompleted, todayStart.Add(10 * time.Hour)})
	// 挂单：今日 1 张 + 昨日 1 张 → 待结账单数=2（实时，与日期范围无关）。
	seedDashboardOrder(t, env, orderSeed{"DASH-P1", "req-dash-p1", custA.ID, 0, model.OrderStatusPending, todayStart.Add(8 * time.Hour)})
	seedDashboardOrder(t, env, orderSeed{"DASH-P2", "req-dash-p2", custB.ID, 0, model.OrderStatusPending, yesterday.Add(20 * time.Hour)})

	if w := env.refundOrder(env.adminToken, ordD1.ID); w.Code != http.StatusOK {
		t.Fatalf("订单退款 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// --- Given: 充值 ---
	// R1 今日 actual=10000；R2 回填昨日 5000；R3 今日 8000 当日冲正；R4 回填昨日 4000 今日冲正。
	r1 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-dash-r1", custA.ID, 10000, 2000, "cash")))
	r2 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-dash-r2", custB.ID, 5000, 0, "wechat")))
	r3 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-dash-r3", custC.ID, 8000, 0, "cash")))
	r4 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-dash-r4", custE.ID, 4000, 0, "alipay")))
	backdateRecharge(t, env, r2.ID, yesterday.Add(22*time.Hour))
	backdateRecharge(t, env, r4.ID, yesterday.Add(21*time.Hour))
	if w := env.refundRecharge(env.adminToken, r3.ID); w.Code != http.StatusOK {
		t.Fatalf("充值冲正 R3 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if w := env.refundRecharge(env.adminToken, r4.ID); w.Code != http.StatusOK {
		t.Fatalf("充值冲正 R4 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	t.Run("summary_kpi_matches_hand_computed", func(t *testing.T) {
		var summary dashboardSummaryData
		env.getDashboard(t, env.staffToken, "/api/v1/dashboard/summary", &summary)

		if summary.Date != todayDate {
			t.Errorf("summary.date = %q, want %q（服务器本地日）", summary.Date, todayDate)
		}
		if summary.RevenueCents != 12000 {
			t.Errorf("今日营业额 = %d, want 12000（completed 18000 − 退款 6000）", summary.RevenueCents)
		}
		if summary.CompletedOrderCount != 3 {
			t.Errorf("今日消费单数 = %d, want 3", summary.CompletedOrderCount)
		}
		if summary.ConsumingCustomerCount != 2 {
			t.Errorf("今日消费人数 = %d, want 2（甲两单去重）", summary.ConsumingCustomerCount)
		}
		if summary.NewCustomerCount != 3 {
			t.Errorf("今日新增客户 = %d, want 3（丙/戊为昨日）", summary.NewCustomerCount)
		}
		if summary.RechargeCents != 6000 {
			t.Errorf("今日充值金额 = %d, want 6000（18000 − 冲正 12000）", summary.RechargeCents)
		}
		if summary.PendingOrderCount != 2 {
			t.Errorf("待结账单数 = %d, want 2", summary.PendingOrderCount)
		}

		// 最近消费：仅 completed（丁的退款单被排除），时间倒序。
		wantOrders := []int64{ordA2.ID, ordA1.ID, ordB2.ID, ordB1.ID}
		if len(summary.RecentOrders) != len(wantOrders) {
			t.Fatalf("最近消费条数 = %d, want %d (items=%+v)", len(summary.RecentOrders), len(wantOrders), summary.RecentOrders)
		}
		for i, want := range wantOrders {
			if summary.RecentOrders[i].ID != want {
				t.Errorf("最近消费[%d].id = %d, want %d（时间倒序）", i, summary.RecentOrders[i].ID, want)
			}
			if summary.RecentOrders[i].Status != model.OrderStatusCompleted {
				t.Errorf("最近消费[%d].status = %q, want completed", i, summary.RecentOrders[i].Status)
			}
		}
		if summary.RecentOrders[0].CustomerName != "看板客户甲" {
			t.Errorf("最近消费[0].customer_name = %q, want 看板客户甲", summary.RecentOrders[0].CustomerName)
		}

		// 最近充值：全部充值记录（含已冲正，状态可见），时间倒序。
		wantRecharges := []int64{r3.ID, r1.ID, r2.ID, r4.ID}
		if len(summary.RecentRecharges) != len(wantRecharges) {
			t.Fatalf("最近充值条数 = %d, want %d (items=%+v)", len(summary.RecentRecharges), len(wantRecharges), summary.RecentRecharges)
		}
		for i, want := range wantRecharges {
			if summary.RecentRecharges[i].ID != want {
				t.Errorf("最近充值[%d].id = %d, want %d（时间倒序）", i, summary.RecentRecharges[i].ID, want)
			}
		}
		if summary.RecentRecharges[0].Status != model.RechargeStatusRefunded {
			t.Errorf("最近充值[0].status = %q, want refunded（冲正记录保留）", summary.RecentRecharges[0].Status)
		}
		if summary.RecentRecharges[0].ActualAmountCents != 8000 {
			t.Errorf("最近充值[0].actual_amount_cents = %d, want 8000", summary.RecentRecharges[0].ActualAmountCents)
		}
	})

	t.Run("pending_count_ignores_date_range", func(t *testing.T) {
		// 待结账单数为当前实时数据，不受日期范围影响（07-UI.md:82、plan todo 44）。
		var ranged dashboardSummaryData
		env.getDashboard(t, env.staffToken,
			fmt.Sprintf("/api/v1/dashboard/summary?start_date=%s&end_date=%s", yesterdayDate, yesterdayDate), &ranged)
		if ranged.PendingOrderCount != 2 {
			t.Errorf("带日期范围的待结账单数 = %d, want 2（实时总数）", ranged.PendingOrderCount)
		}
		if ranged.RevenueCents != 12000 || ranged.CompletedOrderCount != 3 ||
			ranged.ConsumingCustomerCount != 2 || ranged.NewCustomerCount != 3 ||
			ranged.RechargeCents != 6000 || ranged.Date != todayDate {
			t.Errorf("summary 带日期参数后 KPI 漂移: %+v（summary 恒为今日口径）", ranged)
		}
	})

	t.Run("revenue_series_local_day_boundary", func(t *testing.T) {
		var revenue revenueSeriesData
		env.getDashboard(t, env.staffToken,
			fmt.Sprintf("/api/v1/dashboard/revenue?start_date=%s&end_date=%s", yesterdayDate, todayDate), &revenue)

		if revenue.StartDate != yesterdayDate || revenue.EndDate != todayDate {
			t.Errorf("revenue 范围回显 = %s~%s, want %s~%s", revenue.StartDate, revenue.EndDate, yesterdayDate, todayDate)
		}
		if len(revenue.Items) != 2 {
			t.Fatalf("revenue 序列长度 = %d, want 2 (items=%+v)", len(revenue.Items), revenue.Items)
		}
		// 昨日 20000 = B1（本地 23:59:59）；今日 12000 = (10000+5000+3000) − 6000。
		// 注意 A1 在本地 07:00 = 前一日 23:00 UTC：若按 UTC 日界实现会被错记到昨日，
		// 今日将只剩 2000；本断言锁定「服务器本地时区」日界。
		if revenue.Items[0].Date != yesterdayDate || revenue.Items[0].RevenueCents != 20000 {
			t.Errorf("昨日营收 = %+v, want %s/20000", revenue.Items[0], yesterdayDate)
		}
		if revenue.Items[1].Date != todayDate || revenue.Items[1].RevenueCents != 12000 {
			t.Errorf("今日营收 = %+v, want %s/12000", revenue.Items[1], todayDate)
		}

		// 缺省范围 = 本月 1 日 ~ 今日（本地时区），逐日补齐。
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		var defaults revenueSeriesData
		env.getDashboard(t, env.staffToken, "/api/v1/dashboard/revenue", &defaults)
		wantDays := int(localToday.Sub(monthStart).Hours()/24) + 1
		if defaults.StartDate != monthStart.Format("2006-01-02") || defaults.EndDate != todayDate || len(defaults.Items) != wantDays {
			t.Errorf("revenue 缺省范围 = %s~%s/%d 天, want %s~%s/%d 天",
				defaults.StartDate, defaults.EndDate, len(defaults.Items),
				monthStart.Format("2006-01-02"), todayDate, wantDays)
		}
	})

	t.Run("customers_series_new_and_consuming", func(t *testing.T) {
		var customers customerSeriesData
		env.getDashboard(t, env.staffToken,
			fmt.Sprintf("/api/v1/dashboard/customers?start_date=%s&end_date=%s", yesterdayDate, todayDate), &customers)

		if len(customers.Items) != 2 {
			t.Fatalf("customers 序列长度 = %d, want 2 (items=%+v)", len(customers.Items), customers.Items)
		}
		// 昨日：新增 2（丙/戊回填）；消费人数 1（乙，B1）。
		if customers.Items[0].Date != yesterdayDate ||
			customers.Items[0].NewCustomerCount != 2 || customers.Items[0].ConsumingCustomerCount != 1 {
			t.Errorf("昨日客户点 = %+v, want %s/新增2/消费1", customers.Items[0], yesterdayDate)
		}
		// 今日：新增 3（甲/乙/丁）；消费人数 2（甲去重 + 乙）。
		if customers.Items[1].Date != todayDate ||
			customers.Items[1].NewCustomerCount != 3 || customers.Items[1].ConsumingCustomerCount != 2 {
			t.Errorf("今日客户点 = %+v, want %s/新增3/消费2", customers.Items[1], todayDate)
		}
	})

	t.Run("malformed_input", func(t *testing.T) {
		// 非法日期 / 倒置范围 / 超长范围 → 400 参数错误（不得 500 或静默错误统计）。
		for _, query := range []string{
			"?start_date=2026-13-01",
			"?start_date=not-a-date",
			fmt.Sprintf("?start_date=%s&end_date=%s", todayDate, yesterdayDate),
			"?start_date=2000-01-01",
		} {
			w := env.authed(http.MethodGet, "/api/v1/dashboard/revenue"+query, "", env.staffToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("GET /dashboard/revenue%s status = %d, want 400 (body=%s)", query, w.Code, w.Body.String())
				continue
			}
			if envl := decodeEnvelope(t, w); envl.Code != service.CodeInvalidParams {
				t.Errorf("GET /dashboard/revenue%s code = %d, want %d", query, envl.Code, service.CodeInvalidParams)
			}
		}
		// Dashboard 无分页语义：page 参数被忽略（不得 500）。
		if w := env.authed(http.MethodGet, "/api/v1/dashboard/revenue?page=abc&page_size=-5", "", env.staffToken); w.Code != http.StatusOK {
			t.Errorf("GET /dashboard/revenue?page=abc status = %d, want 200（无分页语义）", w.Code)
		}
	})

	t.Run("auth", func(t *testing.T) {
		// Dashboard 为 both 分组：未登录 → 401（staff 访问由本测试全程使用 staffToken 证明）。
		for _, path := range []string{
			"/api/v1/dashboard/summary",
			"/api/v1/dashboard/revenue",
			"/api/v1/dashboard/customers",
		} {
			if w := env.do(http.MethodGet, path, "", nil); w.Code != http.StatusUnauthorized {
				t.Errorf("未登录 GET %s status = %d, want 401 (body=%s)", path, w.Code, w.Body.String())
			}
		}
	})
}
