package controller_test

// TestBalanceAdjust 是 todo 32 的验收测试（计划：`go test ./internal/controller -run TestBalanceAdjust -v -count=1`）：
//   - POST /customers/:id/balance-adjustments 仅 admin：staff → 403（后端 RBAC 为最终边界）；
//   - reason 必填（缺失/空白 → 400）、金额非 0（0 → 400）；
//   - 金额可正负，但不得致负余额：负向超额 → 422 且零写入（原子条件更新，04-API.md:176-193、06 §5:62-65）；
//   - 正/负调整后余额与 balance_transactions(type=adjustment, before/after 连续) 正确；
//   - 仅调整余额：不产生订单、不计入营业额、不改累计消费/积分（04-API.md:192）；
//   - operation_logs(action=balance_adjust) 在事务外写入；不存在客户 → 404；非法 id → 400；未登录 → 401。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// balanceAdjustView 是余额调整响应 DTO 的测试镜像（金额带符号，整数分）。
type balanceAdjustView struct {
	TransactionID      int64     `json:"transaction_id"`
	CustomerID         int64     `json:"customer_id"`
	AmountCents        int64     `json:"amount_cents"`
	BalanceBeforeCents int64     `json:"balance_before_cents"`
	BalanceAfterCents  int64     `json:"balance_after_cents"`
	Reason             string    `json:"reason"`
	CreatedAt          time.Time `json:"created_at"`
}

// adjustJSON 构造余额调整请求体。
func adjustJSON(amountCents int64, reason string) string {
	return fmt.Sprintf(`{"amount_cents":%d,"reason":%q}`, amountCents, reason)
}

// adjustBalance 发起余额调整请求（返回原始响应：staff 场景需要断言 403）。
func (e *customerEnv) adjustBalance(token string, customerID int64, body string) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost,
		fmt.Sprintf("/api/v1/customers/%d/balance-adjustments", customerID), body, token)
}

// adjustRowCounts 统计余额调整相关的表行数（失败路径「零写入」断言）。
//
// orders 恒为 0 用于证明调整不产生订单（不计营业额）；operation_logs 只统计
// action=balance_adjust（登录等既有日志不参与调整事务断言）。
func (e *customerEnv) adjustRowCounts(t *testing.T) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, 3)
	for name, entity := range map[string]any{
		"orders":               &model.Order{},
		"balance_transactions": &model.BalanceTransaction{},
	} {
		var count int64
		if err := e.db.Model(entity).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", name, err)
		}
		counts[name] = count
	}
	var logs int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "balance_adjust").Count(&logs).Error; err != nil {
		t.Fatalf("统计 balance_adjust 日志失败: %v", err)
	}
	counts["adjust_logs"] = logs
	return counts
}

// requireCountsEqual 断言两组行数完全一致（失败路径不得有任何新增写入）。
func requireCountsEqual(t *testing.T, label string, before, after map[string]int64) {
	t.Helper()
	for name, want := range before {
		if got := after[name]; got != want {
			t.Errorf("%s 后 %s 行数 = %d, want %d（零写入）", label, name, got, want)
		}
	}
}

// seedRechargeBalance 通过真实充值 API 给客户建余额（避免直接改库）。
func (e *customerEnv) seedRechargeBalance(t *testing.T, requestID string, customerID, amountCents int64) {
	t.Helper()
	w := e.postRecharge(e.staffToken, rechargeJSON(requestID, customerID, amountCents, 0, "cash"))
	if w.Code != http.StatusCreated {
		t.Fatalf("充值建余额 status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
}

func TestBalanceAdjust(t *testing.T) {
	t.Run("staff is forbidden with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"调整客户甲","phone":"13800010001"}`)
		env.seedRechargeBalance(t, "req-adjust-seed-1", customer.ID, 10000)
		base := env.adjustRowCounts(t)

		// --- When: staff 尝试调整余额 ---
		w := env.adjustBalance(env.staffToken, customer.ID, adjustJSON(5000, "越权补偿"))

		// --- Then: 403（仅 admin）+ 零写入 ---
		if w.Code != http.StatusForbidden {
			t.Fatalf("staff 调整余额 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
			t.Errorf("staff 调整余额 code = %d, want %d", envl.Code, service.CodeForbidden)
		}
		requireCountsEqual(t, "staff 403", base, env.adjustRowCounts(t))
		if got := env.customerState(t, customer.ID).BalanceCents; got != 10000 {
			t.Errorf("staff 调整被拒后余额 = %d, want 10000", got)
		}
	})

	t.Run("missing reason and invalid amount are 400 with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"调整客户乙","phone":"13800010002"}`)
		env.seedRechargeBalance(t, "req-adjust-seed-2", customer.ID, 10000)
		base := env.adjustRowCounts(t)

		cases := []struct {
			name string
			body string
		}{
			{"missing reason", `{"amount_cents":5000}`},
			{"blank reason", adjustJSON(5000, "   ")},
			{"zero amount", adjustJSON(0, "零调整")},
		}
		for _, tc := range cases {
			// --- When/Then: admin 提交非法调整 → 400 ---
			w := env.adjustBalance(env.adminToken, customer.ID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s status = %d, want 400 (body=%s)", tc.name, w.Code, w.Body.String())
			} else if envl := decodeEnvelope(t, w); envl.Code != service.CodeInvalidParams {
				t.Errorf("%s code = %d, want %d", tc.name, envl.Code, service.CodeInvalidParams)
			}
		}

		// --- Then: 零写入、余额不变 ---
		requireCountsEqual(t, "非法调整", base, env.adjustRowCounts(t))
		if got := env.customerState(t, customer.ID).BalanceCents; got != 10000 {
			t.Errorf("非法调整后余额 = %d, want 10000", got)
		}
	})

	t.Run("negative adjustment beyond balance is 422 with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"调整客户丙","phone":"13800010003"}`)
		env.seedRechargeBalance(t, "req-adjust-seed-3", customer.ID, 10000)
		base := env.adjustRowCounts(t)

		// --- When: 余额 10000，调整 -10001（恰好超过 1 分） ---
		w := env.adjustBalance(env.adminToken, customer.ID, adjustJSON(-10001, "超额扣减"))

		// --- Then: 422 + 提示余额不足/不能为负 + 零写入 ---
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("致负调整 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		envl := decodeEnvelope(t, w)
		if envl.Code != service.CodeValidationFailed {
			t.Errorf("致负调整 code = %d, want %d", envl.Code, service.CodeValidationFailed)
		}
		if !strings.Contains(envl.Message, "不能为负") {
			t.Errorf("致负调整 message = %q, want 含「不能为负」", envl.Message)
		}
		requireCountsEqual(t, "致负调整", base, env.adjustRowCounts(t))
		if got := env.customerState(t, customer.ID).BalanceCents; got != 10000 {
			t.Errorf("致负调整被拒后余额 = %d, want 10000", got)
		}

		// --- When: 恰好扣到 0（-10000） ---
		w = env.adjustBalance(env.adminToken, customer.ID, adjustJSON(-10000, "清空余额"))

		// --- Then: 200、余额 0 ---
		if w.Code != http.StatusOK {
			t.Fatalf("恰好清空 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("清空后余额 = %d, want 0", got)
		}

		// --- When: 余额 0 再扣 1 分 ---
		w = env.adjustBalance(env.adminToken, customer.ID, adjustJSON(-1, "再扣"))

		// --- Then: 422 且余额仍为 0 ---
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("零余额再扣 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("零余额再扣被拒后余额 = %d, want 0", got)
		}
	})

	t.Run("positive and negative adjustments update balance and ledger", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"调整客户丁","phone":"13800010004"}`)
		env.seedRechargeBalance(t, "req-adjust-seed-4", customer.ID, 10000)
		base := env.adjustRowCounts(t)

		// --- When: admin 正向调整 +5000 ---
		w := env.adjustBalance(env.adminToken, customer.ID, adjustJSON(5000, "活动补偿"))

		// --- Then: 200 + DTO（before 10000 → after 15000） ---
		if w.Code != http.StatusOK {
			t.Fatalf("正向调整 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		var added balanceAdjustView
		decodeData(t, decodeEnvelope(t, w), &added)
		if added.TransactionID <= 0 || added.CustomerID != customer.ID || added.AmountCents != 5000 ||
			added.BalanceBeforeCents != 10000 || added.BalanceAfterCents != 15000 || added.Reason != "活动补偿" {
			t.Errorf("正向调整 DTO = %+v, want +5000/10000→15000/活动补偿", added)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 15000 {
			t.Errorf("正向调整后余额 = %d, want 15000", got)
		}

		// --- Then: 流水 type=adjustment、金额带符号、operator 为 admin ---
		rows := env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 2 {
			t.Fatalf("正向调整后余额流水条数 = %d, want 2（充值 1+调整 1）", len(rows))
		}
		adjustRow := rows[1]
		if adjustRow.Type != model.BalanceTxAdjustment || adjustRow.AmountCents != 5000 ||
			adjustRow.BalanceBeforeCents != 10000 || adjustRow.BalanceAfterCents != 15000 {
			t.Errorf("正向调整流水 = %+v, want adjustment/+5000/10000→15000", adjustRow)
		}
		if adjustRow.OperatorID == nil || *adjustRow.OperatorID != env.admin.ID {
			t.Errorf("正向调整流水 operator_id = %v, want %d（admin）", adjustRow.OperatorID, env.admin.ID)
		}
		if adjustRow.Remark != "活动补偿" {
			t.Errorf("正向调整流水 remark = %q, want 活动补偿", adjustRow.Remark)
		}

		// --- When: admin 负向调整 -3000 ---
		w = env.adjustBalance(env.adminToken, customer.ID, adjustJSON(-3000, "修正误差"))

		// --- Then: 200 + 余额 12000 + 流水连续 ---
		if w.Code != http.StatusOK {
			t.Fatalf("负向调整 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		var deducted balanceAdjustView
		decodeData(t, decodeEnvelope(t, w), &deducted)
		if deducted.AmountCents != -3000 || deducted.BalanceBeforeCents != 15000 || deducted.BalanceAfterCents != 12000 {
			t.Errorf("负向调整 DTO = %+v, want -3000/15000→12000", deducted)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("负向调整后余额 = %d, want 12000", got)
		}
		rows = env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 3 || rows[2].Type != model.BalanceTxAdjustment || rows[2].AmountCents != -3000 ||
			rows[2].BalanceBeforeCents != 15000 || rows[2].BalanceAfterCents != 12000 {
			t.Errorf("负向调整流水 = %+v, want adjustment/-3000/15000→12000", rows)
		}

		// --- Then: operation_logs(action=balance_adjust) 事务外写入，operator 为 admin ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "balance_adjust").Order("id DESC").First(&logRow).Error; err != nil {
			t.Fatalf("读取 balance_adjust 审计日志失败: %v", err)
		}
		if logRow.TargetType != "customer" || logRow.TargetID != customer.ID {
			t.Errorf("balance_adjust 日志 target = %s#%d, want customer#%d", logRow.TargetType, logRow.TargetID, customer.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
			t.Errorf("balance_adjust 日志 operator_id = %v, want %d（admin）", logRow.OperatorID, env.admin.ID)
		}
		if !strings.Contains(logRow.Content, "修正误差") {
			t.Errorf("balance_adjust 日志 content = %q, want 含原因", logRow.Content)
		}
		if n := env.adjustRowCounts(t)["adjust_logs"]; n != 2 {
			t.Errorf("balance_adjust 日志行数 = %d, want 2（每次调整一条）", n)
		}

		// --- Then: 仅调整余额：不产生订单、不计营业额、不动累计消费/积分 ---
		after := env.adjustRowCounts(t)
		if after["orders"] != 0 {
			t.Errorf("调整后 orders 行数 = %d, want 0（调整不产生订单，不计营业额）", after["orders"])
		}
		if after["balance_transactions"] != base["balance_transactions"]+2 {
			t.Errorf("调整后 balance_transactions = %d, want %d", after["balance_transactions"], base["balance_transactions"]+2)
		}
		state := env.customerState(t, customer.ID)
		if state.TotalSpentCents != 0 || state.Points != 0 {
			t.Errorf("调整后累计消费/积分 = %d/%d, want 0/0（仅调整余额）", state.TotalSpentCents, state.Points)
		}
	})

	t.Run("unknown customer is 404 and unauthenticated is 401", func(t *testing.T) {
		env := newCustomerEnv(t)

		// --- When/Then: 不存在客户 → 404 + 40001 ---
		w := env.adjustBalance(env.adminToken, 999999, adjustJSON(5000, "补偿"))
		if w.Code != http.StatusNotFound {
			t.Fatalf("不存在客户调整 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeCustomerNotFound {
			t.Errorf("不存在客户调整 code = %d, want %d", envl.Code, service.CodeCustomerNotFound)
		}

		// --- When/Then: 非法 id → 400 ---
		if w = env.authed(http.MethodPost, "/api/v1/customers/abc/balance-adjustments", adjustJSON(5000, "补偿"), env.adminToken); w.Code != http.StatusBadRequest {
			t.Errorf("非法 id 调整 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}

		// --- When/Then: 未登录 → 401 ---
		if w = env.do(http.MethodPost, "/api/v1/customers/1/balance-adjustments", adjustJSON(5000, "补偿"), nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录调整 status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
	})
}
