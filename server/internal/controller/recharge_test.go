package controller_test

// TestRecharge 是 todo 30 的验收测试（计划：`go test ./internal/controller -run TestRecharge -v -count=1`）：
//   - 充值 100 元送 20 元 → 余额 +12000 分；两条 balance_transactions（type=recharge/gift）
//     且 balance_before/after 连续（03-DATABASE.md:172,294-307、06 §4:46-54）；
//   - 无赠送仅 1 条本金流水；actual_amount_cents 缺省=本金，充值优惠时可小于本金（03-DATABASE.md:170）；
//   - request_id 幂等：重复（含并发同键）返回原记录、零新增写入（04-API.md:277-299）；
//   - malformed_input：金额 0/负、赠送为负、实付为负、幂等键缺失、支付方式非法、客户不存在 → 400/404 且零写入；
//   - operation_logs(action=recharge) 在事务外写入，operator 为操作者。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// rechargeView 是充值记录 DTO 的测试镜像（金额一律整数分）。
type rechargeView struct {
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

// rechargeJSON 构造 POST /recharges 请求体（actual_amount_cents 缺省=本金）。
func rechargeJSON(requestID string, customerID, principal, gift int64, method string) string {
	return fmt.Sprintf(
		`{"request_id":%q,"customer_id":%d,"recharge_amount_cents":%d,"gift_amount_cents":%d,"payment_method":%q}`,
		requestID, customerID, principal, gift, method)
}

// postRecharge 发起充值请求（返回原始响应：201=新建 / 200=幂等命中原记录）。
func (e *customerEnv) postRecharge(token, body string) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, "/api/v1/recharges", body, token)
}

// decodeRecharge 解析充值 DTO。
func decodeRecharge(t *testing.T, w *httptest.ResponseRecorder) rechargeView {
	t.Helper()
	var view rechargeView
	decodeData(t, decodeEnvelope(t, w), &view)
	return view
}

// rechargeLedgerRows 读取客户的余额流水（id 升序=写入顺序，用于断言 before/after 连续）。
func (e *customerEnv) rechargeLedgerRows(t *testing.T, customerID int64) []model.BalanceTransaction {
	t.Helper()
	var rows []model.BalanceTransaction
	if err := e.db.Where("customer_id = ?", customerID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取余额流水失败: %v", err)
	}
	return rows
}

// rechargeRowCounts 统计充值事务涉及的全部表行数（幂等/失败路径「零新增」断言）。
//
// operation_logs 只统计 action=recharge（登录等既有日志不参与充值事务断言）。
func (e *customerEnv) rechargeRowCounts(t *testing.T) map[string]int64 {
	t.Helper()
	entities := map[string]any{
		"recharge_records":     &model.RechargeRecord{},
		"balance_transactions": &model.BalanceTransaction{},
	}
	counts := make(map[string]int64, len(entities)+1)
	for name, entity := range entities {
		var count int64
		if err := e.db.Model(entity).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", name, err)
		}
		counts[name] = count
	}
	var logs int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "recharge").Count(&logs).Error; err != nil {
		t.Fatalf("统计 recharge 日志失败: %v", err)
	}
	counts["recharge_logs"] = logs
	return counts
}

func TestRecharge(t *testing.T) {
	t.Run("principal and gift write two continuous ledger rows", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"充值客户","phone":"13800008001"}`)

		// --- When: staff 充值本金 10000 分、赠送 2000 分 ---
		w := env.postRecharge(env.staffToken, rechargeJSON("req-recharge-gift", customer.ID, 10000, 2000, "cash"))

		// --- Then: 201 + DTO（实付缺省=本金、status=active、customer_name） ---
		if w.Code != http.StatusCreated {
			t.Fatalf("POST /recharges status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
		created := decodeRecharge(t, w)
		if created.ID <= 0 || created.CustomerID != customer.ID || created.CustomerName != "充值客户" {
			t.Errorf("充值 DTO = %+v, want id>0/customer=%d/充值客户", created, customer.ID)
		}
		if created.RechargeAmountCents != 10000 || created.GiftAmountCents != 2000 || created.ActualAmountCents != 10000 {
			t.Errorf("充值金额 本金/赠送/实付 = %d/%d/%d, want 10000/2000/10000（实付缺省=本金）",
				created.RechargeAmountCents, created.GiftAmountCents, created.ActualAmountCents)
		}
		if created.Status != model.RechargeStatusActive || created.PaymentMethod != model.PaymentMethodCash {
			t.Errorf("充值 status/payment = %q/%q, want active/cash", created.Status, created.PaymentMethod)
		}

		// --- Then: 余额 = 本金+赠送 = 12000（03-DATABASE.md:172） ---
		if got := env.customerState(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("充值后余额 = %d, want 12000（本金 10000+赠送 2000）", got)
		}

		// --- Then: 两条流水连续（recharge 0→10000；gift 10000→12000），关联充值记录与操作人 ---
		rows := env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 2 {
			t.Fatalf("余额流水条数 = %d, want 2", len(rows))
		}
		if rows[0].Type != model.BalanceTxRecharge || rows[0].AmountCents != 10000 ||
			rows[0].BalanceBeforeCents != 0 || rows[0].BalanceAfterCents != 10000 {
			t.Errorf("本金流水 = %+v, want recharge/+10000/0→10000", rows[0])
		}
		if rows[1].Type != model.BalanceTxGift || rows[1].AmountCents != 2000 ||
			rows[1].BalanceBeforeCents != 10000 || rows[1].BalanceAfterCents != 12000 {
			t.Errorf("赠送流水 = %+v, want gift/+2000/10000→12000", rows[1])
		}
		for i, row := range rows {
			if row.ReferenceType != model.ReferenceTypeRecharge || row.ReferenceID == nil || *row.ReferenceID != created.ID {
				t.Errorf("流水[%d] reference = %s/%v, want recharge/%d", i, row.ReferenceType, row.ReferenceID, created.ID)
			}
			if row.OperatorID == nil || *row.OperatorID != env.staff.ID {
				t.Errorf("流水[%d] operator_id = %v, want %d", i, row.OperatorID, env.staff.ID)
			}
		}

		// --- Then: operation_logs(action=recharge) 事务外写入，operator 为操作者 ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "recharge").First(&logRow).Error; err != nil {
			t.Fatalf("读取 recharge 审计日志失败: %v", err)
		}
		if logRow.TargetType != "recharge" || logRow.TargetID != created.ID {
			t.Errorf("recharge 日志 target = %s#%d, want recharge#%d", logRow.TargetType, logRow.TargetID, created.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.staff.ID {
			t.Errorf("recharge 日志 operator_id = %v, want %d", logRow.OperatorID, env.staff.ID)
		}
		if !strings.Contains(logRow.Content, "2000") || !strings.Contains(logRow.Content, "cash") {
			t.Errorf("recharge 日志 content = %q, want 含赠送金额与支付方式", logRow.Content)
		}
	})

	t.Run("recharge without gift writes one ledger row and promo actual is recorded", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"无赠送客户","phone":"13800008002"}`)

		// --- When: 充值 5000 无赠送 ---
		w := env.postRecharge(env.staffToken, rechargeJSON("req-recharge-nogift", customer.ID, 5000, 0, "wechat"))

		// --- Then: 201 + 仅 1 条本金流水、余额 +5000 ---
		if w.Code != http.StatusCreated {
			t.Fatalf("无赠送充值 status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
		created := decodeRecharge(t, w)
		if created.GiftAmountCents != 0 || created.ActualAmountCents != 5000 {
			t.Errorf("无赠送 赠送/实付 = %d/%d, want 0/5000", created.GiftAmountCents, created.ActualAmountCents)
		}
		rows := env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 1 || rows[0].Type != model.BalanceTxRecharge || rows[0].AmountCents != 5000 {
			t.Errorf("无赠送流水 = %+v, want 单条 recharge/+5000", rows)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 5000 {
			t.Errorf("无赠送充值后余额 = %d, want 5000", got)
		}

		// --- When: 充值优惠（充 10000 实付 9000） ---
		promo := fmt.Sprintf(
			`{"request_id":"req-recharge-promo","customer_id":%d,"recharge_amount_cents":10000,"actual_amount_cents":9000,"payment_method":"alipay"}`,
			customer.ID)
		w = env.postRecharge(env.adminToken, promo)

		// --- Then: 201 + actual=9000（资金流入口径）；余额增加仍按本金 10000（赠送 0） ---
		if w.Code != http.StatusCreated {
			t.Fatalf("优惠充值 status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
		promoted := decodeRecharge(t, w)
		if promoted.ActualAmountCents != 9000 || promoted.RechargeAmountCents != 10000 {
			t.Errorf("优惠充值 实付/本金 = %d/%d, want 9000/10000", promoted.ActualAmountCents, promoted.RechargeAmountCents)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 15000 {
			t.Errorf("优惠充值后余额 = %d, want 15000（5000+10000）", got)
		}
		if rows := env.rechargeLedgerRows(t, customer.ID); len(rows) != 2 || rows[1].AmountCents != 10000 {
			t.Errorf("优惠充值后流水 = %+v, want 第二条 +10000（余额按本金增加）", rows)
		}
	})

	t.Run("duplicate request_id returns original with no new rows", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"幂等充值客户","phone":"13800008003"}`)
		body := rechargeJSON("req-recharge-idem", customer.ID, 10000, 2000, "cash")
		first := env.postRecharge(env.staffToken, body)
		if first.Code != http.StatusCreated {
			t.Fatalf("首次充值 status = %d, want 201 (body=%s)", first.Code, first.Body.String())
		}
		original := decodeRecharge(t, first)
		counts := env.rechargeRowCounts(t)
		if counts["recharge_records"] != 1 || counts["balance_transactions"] != 2 {
			t.Fatalf("首次充值后行数 = %v, want 记录 1/流水 2", counts)
		}

		// --- When: 同一 request_id 完全重复提交 ---
		w := env.postRecharge(env.staffToken, body)

		// --- Then: 200 + 原记录、零新增 ---
		if w.Code != http.StatusOK {
			t.Fatalf("重复充值 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if second := decodeRecharge(t, w); second.ID != original.ID {
			t.Errorf("重复充值返回 id = %d, want 原记录 %d", second.ID, original.ID)
		}
		after := env.rechargeRowCounts(t)
		for name, got := range after {
			if got != counts[name] {
				t.Errorf("重复充值后 %s 行数 = %d, want %d（不得重复入账）", name, got, counts[name])
			}
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("重复充值后余额 = %d, want 12000（只入账一次）", got)
		}

		// --- When: 换入参复用同一 request_id ---
		w = env.postRecharge(env.staffToken, rechargeJSON("req-recharge-idem", customer.ID, 9999, 999, "wechat"))

		// --- Then: 幂等键优先于入参，仍返回原记录 ---
		if w.Code != http.StatusOK {
			t.Fatalf("换入参重复充值 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if third := decodeRecharge(t, w); third.ID != original.ID {
			t.Errorf("换入参重复充值返回 id = %d, want 原记录 %d", third.ID, original.ID)
		}
	})

	t.Run("concurrent same request_id charges exactly once", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"并发充值客户","phone":"13800008004"}`)
		body := rechargeJSON("req-recharge-concurrent", customer.ID, 7000, 1000, "cash")

		// --- When: 5 个 goroutine 并发提交同一 request_id ---
		const workers = 5
		codes := make([]int, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				codes[idx] = env.postRecharge(env.staffToken, body).Code
			}(i)
		}
		wg.Wait()

		// --- Then: 全部 200/201（幂等命中），恰好 1 条记录/2 条流水/一次入账 ---
		created := 0
		for i, code := range codes {
			switch code {
			case http.StatusCreated:
				created++
			case http.StatusOK:
			default:
				t.Errorf("并发充值第 %d 个 status = %d, want 200/201", i, code)
			}
		}
		if created > 1 {
			t.Errorf("并发充值 201 次数 = %d, want ≤1", created)
		}
		if got := env.rechargeRowCounts(t); got["recharge_records"] != 1 || got["balance_transactions"] != 2 {
			t.Errorf("并发充值后行数 = %v, want 记录 1/流水 2", got)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 8000 {
			t.Errorf("并发充值后余额 = %d, want 8000（只入账一次）", got)
		}
	})

	t.Run("invalid input is rejected with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"非法充值客户","phone":"13800008005"}`)

		cases := []struct {
			name   string
			body   string
			status int
		}{
			{"amount zero", rechargeJSON("req-bad-1", customer.ID, 0, 0, "cash"), http.StatusBadRequest},
			{"amount negative", rechargeJSON("req-bad-2", customer.ID, -100, 0, "cash"), http.StatusBadRequest},
			{"gift negative", rechargeJSON("req-bad-3", customer.ID, 10000, -1, "cash"), http.StatusBadRequest},
			{"actual negative", fmt.Sprintf(`{"request_id":"req-bad-4","customer_id":%d,"recharge_amount_cents":10000,"actual_amount_cents":-1,"payment_method":"cash"}`, customer.ID), http.StatusBadRequest},
			{"missing request_id", rechargeJSON("", customer.ID, 10000, 0, "cash"), http.StatusBadRequest},
			{"invalid payment method", rechargeJSON("req-bad-5", customer.ID, 10000, 0, "bitcoin"), http.StatusBadRequest},
			{"unknown customer", rechargeJSON("req-bad-6", 999999, 10000, 0, "cash"), http.StatusNotFound},
		}
		for _, tc := range cases {
			w := env.postRecharge(env.staffToken, tc.body)
			if w.Code != tc.status {
				t.Errorf("%s status = %d, want %d (body=%s)", tc.name, w.Code, tc.status, w.Body.String())
			}
			if tc.status == http.StatusNotFound {
				if envl := decodeEnvelope(t, w); envl.Code != service.CodeCustomerNotFound {
					t.Errorf("%s code = %d, want %d", tc.name, envl.Code, service.CodeCustomerNotFound)
				}
			}
		}

		// --- Then: 全部失败路径零写入（DB 探查） ---
		if got := env.rechargeRowCounts(t); got["recharge_records"] != 0 || got["balance_transactions"] != 0 || got["operation_logs"] != 0 {
			t.Errorf("非法充值后行数 = %v, want 全 0", got)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("非法充值后余额 = %d, want 0", got)
		}

		// --- Then: 未登录 → 401 ---
		if w := env.do(http.MethodPost, "/api/v1/recharges", rechargeJSON("req-bad-7", customer.ID, 10000, 0, "cash"), nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录充值 status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
	})
}
