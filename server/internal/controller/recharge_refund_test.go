package controller_test

// TestRechargeRefund 是 todo 36 的验收测试（任务书：`go test ./internal/controller -run TestRechargeRefund -v -count=1`）：
//   - POST /recharges/:id/refund 仅 admin（04-API.md:159,164、06 §7:81）：staff → 403 且零写入；
//   - 仅 active 可冲正（否则 409：重复冲正/不存在 → 404）；
//   - 反向 balance_transactions(type=refund, −(本金+赠送))，余额原子扣减（03-DATABASE.md:172）；
//   - 当前余额 < 本金+赠送 → 422 且整体回滚（防负余额，06 §5:62-65）；
//   - 原始充值记录与本金/赠送流水保留，记录置 refunded（06 §5:65 冲正不得删除原始流水）；
//   - operation_logs(action=recharge_refund) 在事务提交后写入（硬规则：WriteLog 事务外）；
//   - malformed_input：不存在 → 404、非法 id → 400、未登录 → 401；失败路径零写入。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// refundRecharge 发起充值冲正（POST /recharges/:id/refund）。
func (e *customerEnv) refundRecharge(token string, rechargeID int64) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/recharges/%d/refund", rechargeID), "", token)
}

// rechargeRefundLogCount 统计 recharge_refund 审计日志行数。
func (e *customerEnv) rechargeRefundLogCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "recharge_refund").Count(&count).Error; err != nil {
		t.Fatalf("统计 recharge_refund 日志失败: %v", err)
	}
	return count
}

// rechargeRecordStatus 读取充值记录当前状态。
func (e *customerEnv) rechargeRecordStatus(t *testing.T, rechargeID int64) string {
	t.Helper()
	var record model.RechargeRecord
	if err := e.db.First(&record, rechargeID).Error; err != nil {
		t.Fatalf("读取充值记录 %d 失败: %v", rechargeID, err)
	}
	return record.Status
}

func TestRechargeRefund(t *testing.T) {
	t.Run("reversal deducts exact principal plus gift and keeps original ledgers", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"冲正客户甲","phone":"13800009101"}`)
		created := decodeRecharge(t, env.postRecharge(env.staffToken,
			rechargeJSON("req-reversal-gift", customer.ID, 10000, 2000, "cash")))

		// --- Given: 充值本金 10000 + 赠送 2000 → 余额 12000、两条连续流水 ---
		if got := env.customerState(t, customer.ID).BalanceCents; got != 12000 {
			t.Fatalf("冲正前余额 = %d, want 12000", got)
		}
		if rows := env.rechargeLedgerRows(t, customer.ID); len(rows) != 2 {
			t.Fatalf("冲正前流水条数 = %d, want 2", len(rows))
		}

		// --- When: admin 冲正 ---
		w := env.refundRecharge(env.adminToken, created.ID)

		// --- Then: 200 + status=refunded + 金额/客户名保留 ---
		if w.Code != http.StatusOK {
			t.Fatalf("POST /recharges/%d/refund status = %d, want 200 (body=%s)", created.ID, w.Code, w.Body.String())
		}
		refunded := decodeRecharge(t, w)
		if refunded.Status != model.RechargeStatusRefunded {
			t.Errorf("冲正后记录状态 = %q, want refunded", refunded.Status)
		}
		if refunded.ID != created.ID || refunded.CustomerName != "冲正客户甲" {
			t.Errorf("冲正后 id/customer_name = %d/%q, want %d/冲正客户甲", refunded.ID, refunded.CustomerName, created.ID)
		}
		if refunded.RechargeAmountCents != 10000 || refunded.GiftAmountCents != 2000 || refunded.ActualAmountCents != 10000 {
			t.Errorf("冲正后 本金/赠送/实付 = %d/%d/%d, want 10000/2000/10000（金额保留）",
				refunded.RechargeAmountCents, refunded.GiftAmountCents, refunded.ActualAmountCents)
		}

		// --- Then: 余额原子扣减 本金+赠送 = 12000 → 0 ---
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("冲正后余额 = %d, want 0（扣减 12000）", got)
		}

		// --- Then: 原始 2 条流水保留 + 第 3 条反向 refund 流水（before/after 连续） ---
		rows := env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 3 {
			t.Fatalf("冲正后流水条数 = %d, want 3（原始 2 条保留 + 反向 1 条）", len(rows))
		}
		if rows[0].Type != model.BalanceTxRecharge || rows[0].AmountCents != 10000 ||
			rows[0].BalanceBeforeCents != 0 || rows[0].BalanceAfterCents != 10000 {
			t.Errorf("本金流水被改动 = %+v, want 原样保留 recharge/+10000/0→10000", rows[0])
		}
		if rows[1].Type != model.BalanceTxGift || rows[1].AmountCents != 2000 ||
			rows[1].BalanceBeforeCents != 10000 || rows[1].BalanceAfterCents != 12000 {
			t.Errorf("赠送流水被改动 = %+v, want 原样保留 gift/+2000/10000→12000", rows[1])
		}
		reversal := rows[2]
		if reversal.Type != model.BalanceTxRefund || reversal.AmountCents != -12000 ||
			reversal.BalanceBeforeCents != 12000 || reversal.BalanceAfterCents != 0 {
			t.Errorf("冲正流水 = %+v, want refund/-12000/12000→0", reversal)
		}
		if reversal.ReferenceType != model.ReferenceTypeRecharge || reversal.ReferenceID == nil || *reversal.ReferenceID != created.ID {
			t.Errorf("冲正流水 reference = %s/%v, want recharge/%d", reversal.ReferenceType, reversal.ReferenceID, created.ID)
		}
		if reversal.OperatorID == nil || *reversal.OperatorID != env.admin.ID {
			t.Errorf("冲正流水 operator_id = %v, want %d", reversal.OperatorID, env.admin.ID)
		}

		// --- Then: 充值记录未删除（行仍在）且 status=refunded ---
		if got := env.rechargeRecordStatus(t, created.ID); got != model.RechargeStatusRefunded {
			t.Errorf("DB 充值记录状态 = %q, want refunded", got)
		}
		if c := env.rechargeRowCounts(t); c["recharge_records"] != 1 {
			t.Errorf("recharge_records 行数 = %d, want 1（禁止物理删除）", c["recharge_records"])
		}

		// --- Then: operation_logs(action=recharge_refund) 事务外写入，operator=admin ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "recharge_refund").First(&logRow).Error; err != nil {
			t.Fatalf("读取 recharge_refund 审计日志失败: %v", err)
		}
		if logRow.TargetType != "recharge" || logRow.TargetID != created.ID {
			t.Errorf("recharge_refund 日志 target = %s#%d, want recharge#%d", logRow.TargetType, logRow.TargetID, created.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
			t.Errorf("recharge_refund 日志 operator_id = %v, want %d", logRow.OperatorID, env.admin.ID)
		}
		if !strings.Contains(logRow.Content, "10000") || !strings.Contains(logRow.Content, "2000") {
			t.Errorf("recharge_refund 日志 content = %q, want 含本金/赠送金额", logRow.Content)
		}

		// --- When: 二次冲正 ---
		base := env.rechargeRowCounts(t)
		w = env.refundRecharge(env.adminToken, created.ID)

		// --- Then: 409（防重复冲正/重复扣款）+ 零新增 + 余额不被二次扣减 ---
		if w.Code != http.StatusConflict {
			t.Fatalf("二次冲正 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeConflict {
			t.Errorf("二次冲正 code = %d, want %d", envl.Code, service.CodeConflict)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("二次冲正后余额 = %d, want 0（不得二次扣款）", got)
		}
		requireCountsEqual(t, "二次冲正", base, env.rechargeRowCounts(t))
		if n := env.rechargeRefundLogCount(t); n != 1 {
			t.Errorf("二次冲正后 recharge_refund 日志行数 = %d, want 1", n)
		}
	})

	t.Run("spent balance is rejected with 422 and full rollback", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"冲正客户乙","phone":"13800009102"}`)
		created := decodeRecharge(t, env.postRecharge(env.staffToken,
			rechargeJSON("req-reversal-spent", customer.ID, 10000, 0, "cash")))
		hair := env.seedServiceViaAPI(t, "剪发", 6000)
		env.postOrder(t, env.staffToken,
			orderCreateJSON("req-reversal-spend-order", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 6000)))

		// --- Given: 余额已消费 6000，仅余 4000 < 冲正金额 10000 ---
		if got := env.customerState(t, customer.ID).BalanceCents; got != 4000 {
			t.Fatalf("消费后余额 = %d, want 4000", got)
		}
		base := env.rechargeRowCounts(t)

		// --- When: admin 冲正 ---
		w := env.refundRecharge(env.adminToken, created.ID)

		// --- Then: 422（防负余额）+ 记录仍 active + 余额 4000 不变 + 零写入 ---
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("已花掉余额冲正 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		envl := decodeEnvelope(t, w)
		if envl.Code != service.CodeValidationFailed || !strings.Contains(envl.Message, "余额") {
			t.Errorf("已花掉余额冲正 code/message = %d/%q, want 42200 含「余额」", envl.Code, envl.Message)
		}
		if got := env.rechargeRecordStatus(t, created.ID); got != model.RechargeStatusActive {
			t.Errorf("422 后充值记录状态 = %q, want active（整体回滚）", got)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 4000 {
			t.Errorf("422 后余额 = %d, want 4000（零部分写入）", got)
		}
		requireCountsEqual(t, "余额不足 422", base, env.rechargeRowCounts(t))
		if n := env.rechargeRefundLogCount(t); n != 0 {
			t.Errorf("余额不足冲正后 recharge_refund 日志行数 = %d, want 0", n)
		}
	})

	t.Run("staff is forbidden with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"冲正客户丙","phone":"13800009103"}`)
		created := decodeRecharge(t, env.postRecharge(env.staffToken,
			rechargeJSON("req-reversal-staff", customer.ID, 10000, 2000, "wechat")))
		base := env.rechargeRowCounts(t)

		// --- When: staff 尝试冲正 ---
		w := env.refundRecharge(env.staffToken, created.ID)

		// --- Then: 403（路由 admin 分组；后端是最终边界）+ 记录仍 active + 零写入 ---
		if w.Code != http.StatusForbidden {
			t.Fatalf("staff 冲正 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
			t.Errorf("staff 冲正 code = %d, want %d", envl.Code, service.CodeForbidden)
		}
		if got := env.rechargeRecordStatus(t, created.ID); got != model.RechargeStatusActive {
			t.Errorf("staff 冲正后记录状态 = %q, want active（403 不得改动状态）", got)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("staff 冲正后余额 = %d, want 12000", got)
		}
		requireCountsEqual(t, "staff 403", base, env.rechargeRowCounts(t))
		if n := env.rechargeRefundLogCount(t); n != 0 {
			t.Errorf("staff 冲正后 recharge_refund 日志行数 = %d, want 0", n)
		}
	})

	t.Run("guards and malformed input", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"冲正守卫客户","phone":"13800009104"}`)

		// --- When/Then: 不存在充值记录 → 404、非法 id → 400、未登录 → 401 ---
		if w := env.refundRecharge(env.adminToken, 999999); w.Code != http.StatusNotFound {
			t.Errorf("不存在充值记录冲正 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		if w := env.refundRecharge(env.adminToken, 0); w.Code != http.StatusBadRequest {
			t.Errorf("非法充值记录 id 冲正 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		if w := env.do(http.MethodPost, "/api/v1/recharges/abc/refund", "", nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录冲正 status = %d, want 401", w.Code)
		}

		// --- Then: 守卫用例零写入（无记录被冲正、无流水、无日志） ---
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("守卫用例后余额 = %d, want 0", got)
		}
		if c := env.rechargeRowCounts(t); c["recharge_records"] != 0 || c["balance_transactions"] != 0 {
			t.Errorf("守卫用例后行数 = %+v, want 充值/余额流水 0", c)
		}
		if n := env.rechargeRefundLogCount(t); n != 0 {
			t.Errorf("守卫用例后 recharge_refund 日志行数 = %d, want 0", n)
		}
	})
}
