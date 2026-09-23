package service_test

// TestOrderRollback 是 todo 24 的验收测试之一（计划：`go test ./internal/service -run 'TestOrderRollback'`）：
//   - 事务任一步失败整体回滚：用「测试库触发器注入写失败」模拟（生产代码零改动），
//     分别在明细写入后、余额流水写入后、积分流水写入后、客户消费统计更新时注入失败，
//     断言 orders/order_items/balance_transactions/points_transactions/operation_logs 全零行，
//     且 customers 的 balance_cents/points/total_spent_cents/last_visit_at 逐列未变（03-DATABASE.md:349、AGENTS.md 第 5 节）；
//   - 余额不足：422 + 零写入 + 余额永不出现负数（05-TASKS.md:110-116）。
//
// 注入缝说明：触发器只存在于测试临时库（t.TempDir），不进入生产二进制，
// 也不存在任何 HTTP 可达路径。RAISE(ABORT) 只回滚「当前语句」，
// 事务中此前写入的行仍留在事务里 —— 只有 service 层事务回滚真正生效，断言才能看到零行。

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// injectWriteFailure 在测试库指定表上注入 BEFORE INSERT 失败触发器。
func injectWriteFailure(t *testing.T, env *orderTestEnv, table string) {
	t.Helper()
	ddl := fmt.Sprintf(
		"CREATE TRIGGER fail_%s BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT, 'injected write failure'); END",
		table, table)
	if err := env.db.Exec(ddl).Error; err != nil {
		t.Fatalf("注入 %s 写失败触发器: %v", table, err)
	}
}

// injectConsumptionFailure 注入事务内最后一步（客户消费统计更新）失败：
// 仅当 UPDATE 改动 total_spent_cents 时触发，余额/积分两次 UPDATE 不受影响。
func injectConsumptionFailure(t *testing.T, env *orderTestEnv) {
	t.Helper()
	ddl := "CREATE TRIGGER fail_customers_consumption BEFORE UPDATE ON customers " +
		"WHEN NEW.total_spent_cents <> OLD.total_spent_cents " +
		"BEGIN SELECT RAISE(ABORT, 'injected consumption failure'); END"
	if err := env.db.Exec(ddl).Error; err != nil {
		t.Fatalf("注入客户消费统计失败触发器: %v", err)
	}
}

func TestOrderRollback(t *testing.T) {
	injections := []struct {
		name   string
		inject func(t *testing.T, env *orderTestEnv)
	}{
		{"order items insert fails after order insert", func(t *testing.T, env *orderTestEnv) {
			injectWriteFailure(t, env, "order_items")
		}},
		{"balance transaction insert fails after balance update", func(t *testing.T, env *orderTestEnv) {
			injectWriteFailure(t, env, "balance_transactions")
		}},
		{"points transaction insert fails after points update", func(t *testing.T, env *orderTestEnv) {
			injectWriteFailure(t, env, "points_transactions")
		}},
		{"customer consumption stats update fails as last step", func(t *testing.T, env *orderTestEnv) {
			injectConsumptionFailure(t, env)
		}},
	}

	for _, injection := range injections {
		t.Run(injection.name, func(t *testing.T) {
			env := newOrderTestEnv(t)
			customer := env.seedCustomer(t, "回滚客户", 10000)
			haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)
			injection.inject(t, env)

			// --- When: 余额支付 5000，事务中途被注入失败 ---
			_, err := env.orders.Create(adminCtx(), orderInput("req-rollback-injected", customer.ID, model.PaymentMethodBalance,
				service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000}))

			// --- Then: 返回内部错误，且确实来自注入（排除「因别的错误恒真」） ---
			if err == nil {
				t.Fatal("注入失败后 Create = nil, want error（注入未生效）")
			}
			var biz *service.BizError
			if errors.As(err, &biz) {
				t.Errorf("注入失败返回业务错误 %d/%s, want 内部错误（证明失败来自注入而非业务校验）", biz.Status, biz.Message)
			}
			if !strings.Contains(err.Error(), "injected") {
				t.Errorf("错误 = %v, want 含注入标记（证明失败确实来自注入）", err)
			}

			// --- Then: 所有账务表零行（含 operation_logs：日志只在提交后写） ---
			for name, entity := range map[string]any{
				"orders":               &model.Order{},
				"order_items":          &model.OrderItem{},
				"balance_transactions": &model.BalanceTransaction{},
				"points_transactions":  &model.PointsTransaction{},
				"operation_logs":       &model.OperationLog{},
			} {
				if n := env.countRows(t, entity); n != 0 {
					t.Errorf("注入失败后 %s 行数 = %d, want 0（整体回滚）", name, n)
				}
			}

			// --- Then: 客户账务缓存逐列未变（updated_at 证明整行未被触碰） ---
			after := env.customerAfter(t, customer.ID)
			if after.BalanceCents != 10000 {
				t.Errorf("注入失败后余额 = %d, want 10000（余额扣减必须随事务回滚）", after.BalanceCents)
			}
			if after.Points != 0 || after.TotalSpentCents != 0 || after.LastVisitAt != nil {
				t.Errorf("注入失败后积分/累计消费/最近到店 = %d/%d/%v, want 0/0/nil",
					after.Points, after.TotalSpentCents, after.LastVisitAt)
			}
			if !after.UpdatedAt.Equal(customer.UpdatedAt) {
				t.Errorf("注入失败后 updated_at = %v, want %v（整行未被触碰）", after.UpdatedAt, customer.UpdatedAt)
			}
		})
	}

	t.Run("insufficient balance is 422 and balance never goes negative", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "负余额防护客户", 5000)
		haircut := env.seedService(t, "剪发", 3000, model.StatusEnabled)

		// --- Given: 一次成功消费 3000，余额 5000 → 2000 ---
		if _, err := env.orders.Create(adminCtx(), orderInput("req-negative-ok", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 3000})); err != nil {
			t.Fatalf("首次余额支付: %v", err)
		}

		// --- When: 余额 2000 再连续尝试 3 次 3000 的消费 ---
		for i := 0; i < 3; i++ {
			_, err := env.orders.Create(adminCtx(), orderInput(fmt.Sprintf("req-negative-%d", i), customer.ID, model.PaymentMethodBalance,
				service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 3000}))

			// --- Then: 每次都是 422/42200「余额不足」，余额保持 2000 不动 ---
			biz := requireBizError(t, err, http.StatusUnprocessableEntity)
			if biz.Code != service.CodeValidationFailed || !strings.Contains(biz.Message, "余额不足") {
				t.Errorf("余额不足 BizError = code %d msg %q, want 42200 含「余额不足」", biz.Code, biz.Message)
			}
			if got := env.customerAfter(t, customer.ID).BalanceCents; got != 2000 {
				t.Fatalf("第 %d 次不足尝试后余额 = %d, want 2000（失败不得改动余额）", i+1, got)
			}
		}

		// --- Then: 只有首次入账：1 单/1 流水/1 审计；失败的尝试零写入 ---
		for name, entity := range map[string]any{
			"orders":               &model.Order{},
			"balance_transactions": &model.BalanceTransaction{},
			"points_transactions":  &model.PointsTransaction{},
			"operation_logs":       &model.OperationLog{},
		} {
			if n := env.countRows(t, entity); n != 1 {
				t.Errorf("3 次不足尝试后 %s 行数 = %d, want 1（仅首次成功单）", name, n)
			}
		}

		// --- Then: 余额与全部流水均非负 ---
		var minAfter int64
		if err := env.db.Raw("SELECT COALESCE(MIN(balance_after_cents), 0) FROM balance_transactions").Scan(&minAfter).Error; err != nil {
			t.Fatalf("查询最小 balance_after: %v", err)
		}
		if minAfter < 0 {
			t.Errorf("balance_transactions 最小 balance_after = %d, want ≥ 0", minAfter)
		}
		if after := env.customerAfter(t, customer.ID); after.BalanceCents < 0 {
			t.Errorf("客户余额 = %d, want ≥ 0（永不出现负余额）", after.BalanceCents)
		}
	})
}
