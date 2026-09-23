package router_test

// e2e_flows_test.go 是 todo 64 的验收测试：05-TASKS.md:203-252 五条 MVP 流程 + 08-DEPLOYMENT.md:104
// 「重启后数据仍存在」，全部通过真实路由、真实 HTTP、真实 SQLite 与真实备份/恢复服务驱动。
//
//	流程 A：管理员登录 → 新增员工 → 新增服务项目 → 新增客户
//	流程 B：手机（staff）登录 → 搜索客户 → 查看客户 → 快速消费 → 生成消费记录
//	流程 C：客户充值 → 余额增加 → 查看余额流水 → 使用余额消费 → 余额减少
//	流程 D：电脑查看 Dashboard（营业额 / 订单 / 客户 / 充值 / 待结账）与员工业绩
//	流程 E：管理员备份 → 修改数据 → 从备份恢复 → 数据可正常使用
//	重启持久化：停服 → 关闭连接池 → 用同一数据库文件重启 → 客户/余额/流水/订单仍在
//
// 对抗性防线：malformed_input（搜索无结果、余额不足 422、非法 JSON 400、非法 id 400、
// staff 越权 403、未知 API 404 不回退 HTML）、misleading_success_output（收尾直接打开
// SQLite 文件核对行数与金额，不信任响应码与日志）、stale_state（恢复后等于备份时点）。
// 运行：go test ./internal/router -run TestE2EFlows -v -count=1

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const (
	e2eCustomerPhone = "13900000001"
	e2eServicePrice  = int64(8800)
	e2eRechargeCents = int64(50000)
)

// TestE2EFlows 按 05-TASKS 顺序驱动五条流程与重启持久化。
func TestE2EFlows(t *testing.T) {
	env := newE2EEnv(t)

	// ---------- 流程 A ----------
	t.Log("流程 A：管理员登录 → 新增员工 → 新增服务项目 → 新增客户")
	adminToken := env.login(service.DefaultAdminUsername, e2eAdminPassword)

	var employee struct {
		ID int64 `json:"id"`
	}
	env.callData(http.MethodPost, "/api/v1/employees", adminToken,
		`{"name":"员工小美","phone":"13800000001","position":"发型师","joined_at":"2026-01-01"}`,
		http.StatusCreated, &employee)
	if employee.ID <= 0 {
		t.Fatalf("员工 id = %d, want > 0", employee.ID)
	}

	var category struct {
		ID int64 `json:"id"`
	}
	env.callData(http.MethodPost, "/api/v1/service-categories", adminToken,
		`{"name":"剪发","sort":1}`, http.StatusCreated, &category)

	var item struct {
		ID         int64 `json:"id"`
		PriceCents int64 `json:"price_cents"`
	}
	env.callData(http.MethodPost, "/api/v1/services", adminToken,
		`{"category_id":`+itoa(category.ID)+`,"name":"女士剪发","price_cents":`+itoa(e2eServicePrice)+`,"duration_minutes":45}`,
		http.StatusCreated, &item)
	if item.PriceCents != e2eServicePrice {
		t.Fatalf("服务价格 = %d, want %d", item.PriceCents, e2eServicePrice)
	}

	var customer struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		BalanceCents int64  `json:"balance_cents"`
	}
	env.callData(http.MethodPost, "/api/v1/customers", adminToken,
		`{"name":"顾客张女士","phone":"`+e2eCustomerPhone+`","gender":"female"}`,
		http.StatusCreated, &customer)
	if customer.Name != "顾客张女士" || customer.BalanceCents != 0 {
		t.Fatalf("客户 = %+v, want 姓名回显且余额为 0", customer)
	}

	// ---------- 流程 B ----------
	t.Log("流程 B：staff 登录 → 搜索客户 → 查看客户 → 快速消费 → 生成消费记录")
	var staffUser struct {
		ID int64 `json:"id"`
	}
	env.callData(http.MethodPost, "/api/v1/users", adminToken,
		`{"username":"`+e2eStaffUsername+`","role":"staff","password":"`+e2eStaffPassword+`","employee_id":`+itoa(employee.ID)+`}`,
		http.StatusCreated, &staffUser)
	staffToken := env.login(e2eStaffUsername, e2eStaffPassword)

	var search e2ePage[struct {
		ID    int64  `json:"id"`
		Phone string `json:"phone"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers?phone="+e2eCustomerPhone, staffToken, "", http.StatusOK, &search)
	if search.Total != 1 || search.Items[0].ID != customer.ID {
		t.Fatalf("按手机号搜索 = %+v, want 命中客户 #%d", search, customer.ID)
	}

	var detail struct {
		Phone string `json:"phone"`
	}
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), staffToken, "", http.StatusOK, &detail)
	if detail.Phone != e2eCustomerPhone {
		t.Fatalf("客户详情手机号 = %q, want %q", detail.Phone, e2eCustomerPhone)
	}

	var quickOrder struct {
		ID              int64  `json:"id"`
		Status          string `json:"status"`
		PaidAmountCents int64  `json:"paid_amount_cents"`
	}
	env.callData(http.MethodPost, "/api/v1/orders", staffToken,
		`{"request_id":"e2e-order-b1","customer_id":`+itoa(customer.ID)+`,"employee_id":`+itoa(employee.ID)+
			`,"payment_method":"cash","items":[{"service_id":`+itoa(item.ID)+`,"quantity":1}]}`,
		http.StatusCreated, &quickOrder)
	if quickOrder.Status != "completed" || quickOrder.PaidAmountCents != e2eServicePrice {
		t.Fatalf("快速消费 = %+v, want completed/%d", quickOrder, e2eServicePrice)
	}

	var history e2ePage[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/orders", staffToken, "", http.StatusOK, &history)
	if history.Total != 1 || history.Items[0].ID != quickOrder.ID {
		t.Fatalf("客户消费记录 = %+v, want 含订单 #%d", history, quickOrder.ID)
	}

	// ---------- 流程 C ----------
	t.Log("流程 C：充值 → 余额增加 → 余额流水 → 余额消费 → 余额减少")
	var recharge struct {
		RechargeAmountCents int64 `json:"recharge_amount_cents"`
	}
	env.callData(http.MethodPost, "/api/v1/recharges", staffToken,
		`{"request_id":"e2e-recharge-1","customer_id":`+itoa(customer.ID)+`,"recharge_amount_cents":`+itoa(e2eRechargeCents)+
			`,"gift_amount_cents":0,"payment_method":"wechat"}`,
		http.StatusCreated, &recharge)
	if recharge.RechargeAmountCents != e2eRechargeCents {
		t.Fatalf("充值金额 = %d, want %d", recharge.RechargeAmountCents, e2eRechargeCents)
	}

	var afterRecharge struct {
		BalanceCents int64 `json:"balance_cents"`
	}
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), staffToken, "", http.StatusOK, &afterRecharge)
	if afterRecharge.BalanceCents != e2eRechargeCents {
		t.Fatalf("充值后余额 = %d, want %d", afterRecharge.BalanceCents, e2eRechargeCents)
	}

	type ledgerRow struct {
		Type              string `json:"type"`
		AmountCents       int64  `json:"amount_cents"`
		BalanceAfterCents int64  `json:"balance_after_cents"`
	}
	var ledgerAfterRecharge e2ePage[ledgerRow]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/balance-transactions", staffToken, "", http.StatusOK, &ledgerAfterRecharge)
	if ledgerAfterRecharge.Total != 1 || ledgerAfterRecharge.Items[0].Type != "recharge" ||
		ledgerAfterRecharge.Items[0].AmountCents != e2eRechargeCents || ledgerAfterRecharge.Items[0].BalanceAfterCents != e2eRechargeCents {
		t.Fatalf("充值流水 = %+v, want recharge/%d/%d", ledgerAfterRecharge, e2eRechargeCents, e2eRechargeCents)
	}

	var balanceOrder struct {
		ID              int64 `json:"id"`
		PaidAmountCents int64 `json:"paid_amount_cents"`
	}
	env.callData(http.MethodPost, "/api/v1/orders", staffToken,
		`{"request_id":"e2e-order-c1","customer_id":`+itoa(customer.ID)+`,"employee_id":`+itoa(employee.ID)+
			`,"payment_method":"balance","items":[{"service_id":`+itoa(item.ID)+`,"quantity":1}]}`,
		http.StatusCreated, &balanceOrder)

	var afterConsume struct {
		BalanceCents int64 `json:"balance_cents"`
	}
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), staffToken, "", http.StatusOK, &afterConsume)
	wantBalance := e2eRechargeCents - e2eServicePrice
	if afterConsume.BalanceCents != wantBalance {
		t.Fatalf("余额消费后余额 = %d, want %d", afterConsume.BalanceCents, wantBalance)
	}

	var ledgerAfterConsume e2ePage[ledgerRow]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/balance-transactions", staffToken, "", http.StatusOK, &ledgerAfterConsume)
	if ledgerAfterConsume.Total != 2 || ledgerAfterConsume.Items[0].Type != "consume" ||
		ledgerAfterConsume.Items[0].AmountCents != -e2eServicePrice || ledgerAfterConsume.Items[0].BalanceAfterCents != wantBalance {
		t.Fatalf("消费流水 = %+v, want consume/-%d/%d", ledgerAfterConsume, e2eServicePrice, wantBalance)
	}

	// ---------- 对抗性：malformed_input 与权限边界 ----------
	t.Log("对抗性：搜索无结果 / 余额不足 422 / 非法 JSON 400 / 非法 id 400 / staff 403 / 未知 API 404")
	var empty e2ePage[struct {
		ID int64 `json:"id"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers?phone=19999999999", staffToken, "", http.StatusOK, &empty)
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("无结果搜索 = %+v, want 空结果", empty)
	}

	env.callBizError(http.MethodPost, "/api/v1/orders", staffToken,
		`{"request_id":"e2e-order-c2","customer_id":`+itoa(customer.ID)+`,"employee_id":`+itoa(employee.ID)+
			`,"payment_method":"balance","items":[{"service_id":`+itoa(item.ID)+`,"quantity":5}]}`,
		http.StatusUnprocessableEntity, service.CodeValidationFailed)
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), staffToken, "", http.StatusOK, &afterConsume)
	if afterConsume.BalanceCents != wantBalance {
		t.Fatalf("余额不足消费后余额 = %d, want 不变 %d", afterConsume.BalanceCents, wantBalance)
	}
	var ordersAfterFailure e2ePage[struct {
		ID int64 `json:"id"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/orders", staffToken, "", http.StatusOK, &ordersAfterFailure)
	if ordersAfterFailure.Total != 2 {
		t.Fatalf("余额不足消费后订单数 = %d, want 2（失败事务不得落库）", ordersAfterFailure.Total)
	}

	env.callBizError(http.MethodPost, "/api/v1/customers", adminToken, `{"name":`, http.StatusBadRequest, service.CodeInvalidParams)
	env.callBizError(http.MethodGet, "/api/v1/customers/abc", adminToken, "", http.StatusBadRequest, service.CodeInvalidParams)
	env.callBizError(http.MethodGet, "/api/v1/employees", staffToken, "", http.StatusForbidden, service.CodeForbidden)
	notFoundBody := env.call(http.MethodGet, "/api/v1/nope", adminToken, "", http.StatusNotFound)
	if strings.Contains(string(notFoundBody), "<html") {
		t.Fatalf("未知 API 路由回退了 HTML：%s", notFoundBody)
	}

	// ---------- 流程 D ----------
	t.Log("流程 D：Dashboard 营业额/订单/客户/充值/待结账 + 员工业绩")
	type summaryView struct {
		RevenueCents           int64 `json:"revenue_cents"`
		CompletedOrderCount    int64 `json:"completed_order_count"`
		ConsumingCustomerCount int64 `json:"consuming_customer_count"`
		NewCustomerCount       int64 `json:"new_customer_count"`
		RechargeCents          int64 `json:"recharge_cents"`
		PendingOrderCount      int64 `json:"pending_order_count"`
	}
	var summary summaryView
	env.callData(http.MethodGet, "/api/v1/dashboard/summary", adminToken, "", http.StatusOK, &summary)
	wantRevenue := e2eServicePrice * 2
	if summary.RevenueCents != wantRevenue || summary.CompletedOrderCount != 2 ||
		summary.ConsumingCustomerCount != 1 || summary.NewCustomerCount != 1 ||
		summary.RechargeCents != e2eRechargeCents || summary.PendingOrderCount != 0 {
		t.Fatalf("Dashboard summary = %+v, want revenue=%d/orders=2/consuming=1/new=1/recharge=%d/pending=0",
			summary, wantRevenue, e2eRechargeCents)
	}

	var pendingOrder struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	env.callData(http.MethodPost, "/api/v1/orders", staffToken,
		`{"request_id":"e2e-order-d1","customer_id":`+itoa(customer.ID)+`,"employee_id":`+itoa(employee.ID)+
			`,"payment_method":"cash","status":"pending","items":[{"service_id":`+itoa(item.ID)+`,"quantity":1}]}`,
		http.StatusCreated, &pendingOrder)
	if pendingOrder.Status != "pending" {
		t.Fatalf("挂单状态 = %q, want pending", pendingOrder.Status)
	}
	env.callData(http.MethodGet, "/api/v1/dashboard/summary", adminToken, "", http.StatusOK, &summary)
	if summary.PendingOrderCount != 1 || summary.RevenueCents != wantRevenue {
		t.Fatalf("挂单后 summary = %+v, want pending=1 且营业额不变 %d", summary, wantRevenue)
	}
	env.callData(http.MethodPost, "/api/v1/orders/"+itoa(pendingOrder.ID)+"/cancel", adminToken, "", http.StatusOK, nil)
	env.callData(http.MethodGet, "/api/v1/dashboard/summary", adminToken, "", http.StatusOK, &summary)
	if summary.PendingOrderCount != 0 {
		t.Fatalf("取消挂单后 pending = %d, want 0", summary.PendingOrderCount)
	}

	today := time.Now().Format("2006-01-02")
	var performance struct {
		Items []struct {
			EmployeeID   *int64 `json:"employee_id"`
			EmployeeName string `json:"employee_name"`
			AmountCents  int64  `json:"amount_cents"`
		} `json:"items"`
	}
	env.callData(http.MethodGet, "/api/v1/dashboard/employee-performance?start_date="+today+"&end_date="+today,
		adminToken, "", http.StatusOK, &performance)
	if len(performance.Items) != 1 || performance.Items[0].EmployeeID == nil ||
		*performance.Items[0].EmployeeID != employee.ID || performance.Items[0].AmountCents != wantRevenue {
		t.Fatalf("员工业绩 = %+v, want 员工 #%d 金额 %d", performance.Items, employee.ID, wantRevenue)
	}

	var revenue struct {
		Items []struct {
			Date         string `json:"date"`
			RevenueCents int64  `json:"revenue_cents"`
		} `json:"items"`
	}
	env.callData(http.MethodGet, "/api/v1/dashboard/revenue?start_date="+today+"&end_date="+today, adminToken, "", http.StatusOK, &revenue)
	if len(revenue.Items) != 1 || revenue.Items[0].RevenueCents != wantRevenue {
		t.Fatalf("营收序列 = %+v, want 今日 %d", revenue.Items, wantRevenue)
	}

	// ---------- 流程 E ----------
	t.Log("流程 E：备份 → 修改数据 → 恢复 → 数据回到备份时点并可用")
	var backup struct {
		Name string `json:"name"`
	}
	env.callData(http.MethodPost, "/api/v1/backups", adminToken, "", http.StatusCreated, &backup)
	if backup.Name == "" {
		t.Fatal("备份名称为空")
	}

	var mutated struct {
		ID int64 `json:"id"`
	}
	env.callData(http.MethodPost, "/api/v1/customers", adminToken, `{"name":"备份后新增客户","phone":"13900000002"}`, http.StatusCreated, &mutated)
	env.callData(http.MethodGet, "/api/v1/customers?phone=13900000002", adminToken, "", http.StatusOK, &search)
	if search.Total != 1 {
		t.Fatalf("恢复前变动客户 = %+v, want 1", search)
	}

	var restored struct {
		Restored        string `json:"restored"`
		SafetyBackup    string `json:"safety_backup"`
		UploadsReplaced bool   `json:"uploads_replaced"`
		ConfigReplaced  bool   `json:"config_replaced"`
	}
	env.callData(http.MethodPost, "/api/v1/backups/"+backup.Name+"/restore", adminToken, `{"confirm":true}`, http.StatusOK, &restored)
	if restored.Restored != backup.Name || restored.SafetyBackup == "" {
		t.Fatalf("恢复响应 = %+v, want restored=%s 且有安全备份", restored, backup.Name)
	}
	if !restored.ConfigReplaced {
		t.Fatalf("恢复响应 = %+v, want config_replaced=true（备份含 config.yaml）", restored)
	}

	env.callData(http.MethodGet, "/api/v1/customers?phone=13900000002", adminToken, "", http.StatusOK, &search)
	if search.Total != 0 {
		t.Fatalf("恢复后变动客户仍存在 = %+v（stale_state）", search)
	}
	env.callData(http.MethodGet, "/api/v1/customers?phone="+e2eCustomerPhone, adminToken, "", http.StatusOK, &search)
	if search.Total != 1 {
		t.Fatalf("恢复后基线客户 = %+v, want 1", search)
	}
	var postRestoreCustomer struct {
		BalanceCents int64 `json:"balance_cents"`
	}
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), adminToken, "", http.StatusOK, &postRestoreCustomer)
	if postRestoreCustomer.BalanceCents != wantBalance {
		t.Fatalf("恢复后余额 = %d, want %d", postRestoreCustomer.BalanceCents, wantBalance)
	}
	var postRestoreOrders e2ePage[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/orders", adminToken, "", http.StatusOK, &postRestoreOrders)
	if postRestoreOrders.Total != 3 || postRestoreOrders.Items[0].Status != "cancelled" {
		t.Fatalf("恢复后订单 = %+v, want 3 条且含已取消挂单", postRestoreOrders)
	}
	env.callData(http.MethodPost, "/api/v1/customers", adminToken, `{"name":"恢复后可用客户","phone":"13900000003"}`, http.StatusCreated, nil)

	// ---------- 重启持久化 ----------
	t.Log("重启持久化：停服 → 重开同一数据库文件 → 数据仍在")
	env.restart()
	adminToken2 := env.login(service.DefaultAdminUsername, e2eAdminPassword)

	var persisted struct {
		ID           int64 `json:"id"`
		BalanceCents int64 `json:"balance_cents"`
		Points       int64 `json:"points"`
	}
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID), adminToken2, "", http.StatusOK, &persisted)
	if persisted.BalanceCents != wantBalance || persisted.Points != 176 {
		t.Fatalf("重启后客户 = %+v, want balance=%d points=176", persisted, wantBalance)
	}
	var persistedLedger e2ePage[ledgerRow]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/balance-transactions", adminToken2, "", http.StatusOK, &persistedLedger)
	if persistedLedger.Total != 2 {
		t.Fatalf("重启后余额流水 = %d, want 2", persistedLedger.Total)
	}
	var persistedOrders e2ePage[struct {
		Status string `json:"status"`
	}]
	env.callData(http.MethodGet, "/api/v1/customers/"+itoa(customer.ID)+"/orders", adminToken2, "", http.StatusOK, &persistedOrders)
	if persistedOrders.Total != 3 {
		t.Fatalf("重启后订单 = %d, want 3", persistedOrders.Total)
	}

	// ---------- misleading_success_output：直接打开 SQLite 文件核对 ----------
	t.Log("直接读取 SQLite 文件核对（不信任响应码与日志）")
	env.stop()
	live, err := sql.Open("sqlite", repository.DSN(env.dbPath))
	if err != nil {
		t.Fatalf("sql.Open(数据库文件): %v", err)
	}
	defer func() { _ = live.Close() }()

	var integrity string
	if err := live.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("PRAGMA integrity_check = %q (err=%v), want ok", integrity, err)
	}
	var liveCustomers int
	if err := live.QueryRow(`SELECT COUNT(*) FROM customers WHERE deleted_at IS NULL`).Scan(&liveCustomers); err != nil {
		t.Fatalf("直接查询 customers: %v", err)
	}
	if liveCustomers != 2 {
		t.Fatalf("数据库文件 customers = %d, want 2", liveCustomers)
	}
	var fileBalance, filePoints int64
	if err := live.QueryRow(`SELECT balance_cents, points FROM customers WHERE phone = ?`, e2eCustomerPhone).Scan(&fileBalance, &filePoints); err != nil {
		t.Fatalf("直接查询客户余额: %v", err)
	}
	if fileBalance != wantBalance || filePoints != 176 {
		t.Fatalf("数据库文件 balance/points = %d/%d, want %d/176", fileBalance, filePoints, wantBalance)
	}
	var fileLedger, fileCompleted, fileCancelled, fileRestoreLogs int
	for _, q := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM balance_transactions WHERE customer_id = ?`, []any{customer.ID}, &fileLedger},
		{`SELECT COUNT(*) FROM orders WHERE customer_id = ? AND status = 'completed'`, []any{customer.ID}, &fileCompleted},
		{`SELECT COUNT(*) FROM orders WHERE customer_id = ? AND status = 'cancelled'`, []any{customer.ID}, &fileCancelled},
		{`SELECT COUNT(*) FROM operation_logs WHERE action = 'restore'`, nil, &fileRestoreLogs},
	} {
		if err := live.QueryRow(q.query, q.args...).Scan(q.out); err != nil {
			t.Fatalf("直接查询失败 %q: %v", q.query, err)
		}
	}
	if fileLedger != 2 || fileCompleted != 2 || fileCancelled != 1 || fileRestoreLogs != 1 {
		t.Fatalf("数据库文件 ledger/completed/cancelled/restoreLogs = %d/%d/%d/%d, want 2/2/1/1",
			fileLedger, fileCompleted, fileCancelled, fileRestoreLogs)
	}
}

// itoa 是 strconv.FormatInt(v, 10) 的短别名，用于拼接测试请求体。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }
