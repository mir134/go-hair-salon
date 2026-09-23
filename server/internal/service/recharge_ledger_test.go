package service_test

// TestRechargeLedger 是 todo 34 的验收测试之一（计划：`go test ./internal/service -run 'TestRechargeLedger|TestAdjustGuard' -count=3`）：
//   - 双流水连续性：gift>0 → 恰好两条 balance_transactions（type=recharge/gift），
//     balance_before/after 首尾相接，且流水链尾 == customers.balance_cents
//     （03-DATABASE.md:172,294-307、06 §4:46-54）；gift=0 → 恰好一条本金流水；
//   - 幂等：同一 request_id 并发 ≥5 个调用 → 恰好 1 条 recharge_records + 正确流水，
//     全部调用者拿到原记录（ux_recharge_records_request_id 兜底，04-API.md:277-299）；
//   - W8 前置：充值冲正所需不变量「当前余额 ≥ 本金+赠送」可从账本观测（本 todo 不实现冲正）；
//   - malformed_input：金额 0/负、赠送为负、幂等键空/超长、客户不存在、支付方式非法 → 400/404 且零写入。
//
// 共享测试环境与断言辅助见 ledger_test_env_test.go；本文件只新增测试，生产代码零改动。

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestRechargeLedger(t *testing.T) {
	t.Run("gift recharge writes exactly two continuous ledger rows", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "双流水客户", 0)

		// --- When: 充值本金 10000 分、赠送 2000 分 ---
		result, err := env.recharges.CreateRecharge(adminCtx(),
			rechargeInput("req-ledger-gift", customer.ID, 10000, 2000, model.PaymentMethodCash))
		if err != nil {
			t.Fatalf("CreateRecharge(gift): %v", err)
		}
		if !result.Created || result.Record == nil {
			t.Fatalf("首次充值 Created/Record = %v/%v, want true/非空", result.Created, result.Record)
		}
		if result.CustomerName != "双流水客户" {
			t.Errorf("结果 customer_name = %q, want 双流水客户", result.CustomerName)
		}

		// --- Then: 余额 = 本金+赠送 = 12000（03-DATABASE.md:172） ---
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("充值后余额 = %d, want 12000（本金 10000+赠送 2000）", got)
		}

		// --- Then: 恰好两条流水：recharge 0→10000、gift 10000→12000（before/after 连续） ---
		rows := env.balanceRows(t, customer.ID)
		if len(rows) != 2 {
			t.Fatalf("余额流水条数 = %d, want 2（本金+赠送）", len(rows))
		}
		principal, gift := rows[0], rows[1]
		if principal.Type != model.BalanceTxRecharge || principal.AmountCents != 10000 ||
			principal.BalanceBeforeCents != 0 || principal.BalanceAfterCents != 10000 {
			t.Errorf("本金流水 = %+v, want recharge/+10000/0→10000", principal)
		}
		if gift.Type != model.BalanceTxGift || gift.AmountCents != 2000 ||
			gift.BalanceBeforeCents != 10000 || gift.BalanceAfterCents != 12000 {
			t.Errorf("赠送流水 = %+v, want gift/+2000/10000→12000", gift)
		}
		for i, row := range rows {
			if row.ReferenceType != model.ReferenceTypeRecharge || row.ReferenceID == nil || *row.ReferenceID != result.Record.ID {
				t.Errorf("流水[%d] reference = %s/%v, want recharge/%d", i, row.ReferenceType, row.ReferenceID, result.Record.ID)
			}
			if row.OperatorID == nil || *row.OperatorID != 1 {
				t.Errorf("流水[%d] operator_id = %v, want 1", i, row.OperatorID)
			}
		}

		// --- When: 第二笔充值（本金 5000、赠送 500） ---
		second, err := env.recharges.CreateRecharge(adminCtx(),
			rechargeInput("req-ledger-gift-2", customer.ID, 5000, 500, model.PaymentMethodWechat))
		if err != nil {
			t.Fatalf("CreateRecharge(gift-2): %v", err)
		}
		if second.Record.ID == result.Record.ID {
			t.Errorf("第二笔充值 id = %d, want 新记录（原 %d）", second.Record.ID, result.Record.ID)
		}

		// --- Then: 4 条流水、链跨记录仍连续、链尾 == 余额 17500 ---
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 17500 {
			t.Errorf("第二笔充值后余额 = %d, want 17500", got)
		}
		if n := env.countRows(t, &model.RechargeRecord{}); n != 2 {
			t.Errorf("recharge_records 行数 = %d, want 2", n)
		}
		env.requireChainContinuous(t, customer.ID, 0)

		// --- Then: operation_logs(action=recharge) 每笔一条（事务外写入） ---
		var logCount int64
		if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "recharge").Count(&logCount).Error; err != nil {
			t.Fatalf("统计 recharge 日志失败: %v", err)
		}
		if logCount != 2 {
			t.Errorf("recharge 日志行数 = %d, want 2", logCount)
		}
	})

	t.Run("recharge without gift writes exactly one principal row", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "无赠送客户", 0)

		// --- When: 充值本金 5000、无赠送 ---
		if _, err := env.recharges.CreateRecharge(adminCtx(),
			rechargeInput("req-ledger-nogift", customer.ID, 5000, 0, model.PaymentMethodAlipay)); err != nil {
			t.Fatalf("CreateRecharge(no gift): %v", err)
		}

		// --- Then: 恰好 1 条本金流水、余额 +5000、链尾 == 余额 ---
		rows := env.balanceRows(t, customer.ID)
		if len(rows) != 1 || rows[0].Type != model.BalanceTxRecharge || rows[0].AmountCents != 5000 {
			t.Errorf("无赠送流水 = %+v, want 单条 recharge/+5000", rows)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 5000 {
			t.Errorf("无赠送充值后余额 = %d, want 5000", got)
		}
		env.requireChainContinuous(t, customer.ID, 0)
	})

	t.Run("concurrent same request_id charges exactly once", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "并发幂等充值客户", 0)

		// --- When: 同一 request_id 由 8 个 goroutine 并发充值（本金 7000、赠送 1000） ---
		const callers = 8
		results := make([]*service.RechargeResult, callers)
		errs := runConcurrent(callers, func(idx int) error {
			result, err := env.recharges.CreateRecharge(adminCtx(),
				rechargeInput("req-ledger-concurrent", customer.ID, 7000, 1000, model.PaymentMethodCash))
			results[idx] = result
			return err
		})

		// --- Then: 无调用者报错，全部拿到同一原记录，Created=true 恰好一次 ---
		if errs[0] != nil {
			t.Fatalf("并发调用 0 返回错误: %v", errs[0])
		}
		if results[0] == nil || results[0].Record == nil {
			t.Fatal("并发调用 0 返回 nil 记录")
		}
		createdCount := 0
		for i := 0; i < callers; i++ {
			if errs[i] != nil {
				t.Fatalf("并发调用 %d 返回错误: %v", i, errs[i])
			}
			if results[i] == nil || results[i].Record == nil {
				t.Fatalf("并发调用 %d 返回 nil 记录", i)
			}
			if results[i].Record.ID != results[0].Record.ID {
				t.Errorf("调用 %d 返回记录 id = %d, want 与调用 0 相同 %d", i, results[i].Record.ID, results[0].Record.ID)
			}
			if results[i].Created {
				createdCount++
			}
		}
		if createdCount != 1 {
			t.Errorf("Created=true 次数 = %d, want 1（并发下仅一次真正入账）", createdCount)
		}

		// --- Then: 数据库仅一套账（唯一索引兜底）：1 条记录 + 2 条流水 + 1 条审计 ---
		if n := env.countRows(t, &model.RechargeRecord{}); n != 1 {
			t.Errorf("recharge_records 行数 = %d, want 1", n)
		}
		rows := env.balanceRows(t, customer.ID)
		if len(rows) != 2 || rows[0].Type != model.BalanceTxRecharge || rows[1].Type != model.BalanceTxGift {
			t.Fatalf("并发充值后流水 = %+v, want 本金+赠送 两条", rows)
		}
		var logCount int64
		if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "recharge").Count(&logCount).Error; err != nil {
			t.Fatalf("统计 recharge 日志失败: %v", err)
		}
		if logCount != 1 {
			t.Errorf("recharge 日志行数 = %d, want 1（仅一次真正入账）", logCount)
		}

		// --- Then: 余额只入账一次（7000+1000），链连续且链尾 == 余额 ---
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 8000 {
			t.Errorf("并发充值后余额 = %d, want 8000（只入账一次）", got)
		}
		env.requireChainContinuous(t, customer.ID, 0)
	})

	t.Run("reversal precondition is observable", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "冲正前置客户", 0)

		// --- Given: 充值本金 10000 + 赠送 2000 → 余额 12000 ---
		created, err := env.recharges.CreateRecharge(adminCtx(),
			rechargeInput("req-w8-precondition", customer.ID, 10000, 2000, model.PaymentMethodCash))
		if err != nil {
			t.Fatalf("充值建余额: %v", err)
		}
		reversalAmount := created.Record.RechargeAmountCents + created.Record.GiftAmountCents
		if reversalAmount != 12000 {
			t.Fatalf("冲正金额（本金+赠送）= %d, want 12000", reversalAmount)
		}

		// --- Then: 余额 ≥ 本金+赠送（W8 冲正前置不变量为真，可从账本观测） ---
		if got := env.customerAfter(t, customer.ID).BalanceCents; got < reversalAmount {
			t.Errorf("充值后余额 = %d, want ≥ 本金+赠送 %d", got, reversalAmount)
		}

		// --- When: 客户余额支付消费 5000（花掉部分余额） ---
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)
		if _, err := env.orders.Create(adminCtx(), orderInput("req-w8-consume", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000})); err != nil {
			t.Fatalf("余额支付消费: %v", err)
		}

		// --- Then: 余额 7000 < 12000 —— 前置不变量可观测为假（W8 据此拒绝冲正；本 todo 不实现冲正） ---
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 7000 {
			t.Errorf("消费后余额 = %d, want 7000", got)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got >= reversalAmount {
			t.Errorf("消费后余额 = %d, want < 本金+赠送 %d（前置不变量应可观测为假）", got, reversalAmount)
		}

		// --- Then: 原始充值记录未被触碰（未实现冲正：status 仍 active、金额不变、行仍在） ---
		var record model.RechargeRecord
		if err := env.db.First(&record, created.Record.ID).Error; err != nil {
			t.Fatalf("读取充值记录失败: %v", err)
		}
		if record.Status != model.RechargeStatusActive || record.RechargeAmountCents != 10000 || record.GiftAmountCents != 2000 {
			t.Errorf("充值记录 = status %s/本金 %d/赠送 %d, want active/10000/2000（未实现冲正，不得改动）",
				record.Status, record.RechargeAmountCents, record.GiftAmountCents)
		}
	})

	t.Run("malformed input leaves zero rows", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "非法充值客户", 0)

		cases := []struct {
			name       string
			in         service.RechargeInput
			wantStatus int
		}{
			{"本金为 0", rechargeInput("req-bad-zero", customer.ID, 0, 0, model.PaymentMethodCash), http.StatusBadRequest},
			{"本金为负", rechargeInput("req-bad-neg", customer.ID, -100, 0, model.PaymentMethodCash), http.StatusBadRequest},
			{"赠送为负", rechargeInput("req-bad-gift", customer.ID, 10000, -1, model.PaymentMethodCash), http.StatusBadRequest},
			{"幂等键为空", rechargeInput("", customer.ID, 10000, 0, model.PaymentMethodCash), http.StatusBadRequest},
			{"幂等键超长", rechargeInput(strings.Repeat("x", 65), customer.ID, 10000, 0, model.PaymentMethodCash), http.StatusBadRequest},
			{"未知客户", rechargeInput("req-bad-customer", 999999, 10000, 0, model.PaymentMethodCash), http.StatusNotFound},
			{"支付方式非法", rechargeInput("req-bad-method", customer.ID, 10000, 0, "bitcoin"), http.StatusBadRequest},
		}
		for _, tc := range cases {
			// --- When/Then: 每一类非法输入返回对应业务错误 ---
			if _, err := env.recharges.CreateRecharge(adminCtx(), tc.in); err != nil {
				requireBizError(t, err, tc.wantStatus)
			} else {
				t.Errorf("%s 未返回错误", tc.name)
			}
		}

		// --- Then: 全部失败路径零写入、客户余额未变 ---
		for name, entity := range ledgerEntityTables() {
			if n := env.countRows(t, entity); n != 0 {
				t.Errorf("非法充值后 %s 行数 = %d, want 0", name, n)
			}
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("非法充值后余额 = %d, want 0", got)
		}
	})
}
