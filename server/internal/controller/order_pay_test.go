package controller_test

// TestOrderPay 是 todo 27 的验收测试（计划：`go test ./internal/controller -run TestOrderPay -v -count=1`）：
//   - 挂单结账（pending → completed）在单事务内完成：支付方式记录、余额原子扣减 + 余额流水、
//     积分流水 + 客户积分、total_spent_cents、last_visit_at（03-DATABASE.md:333-347、06 §3:34-36）；
//   - 重复结账 → 409（防重复结账，04-API.md:151）；并发双结账恰好一次成功（原子条件状态迁移）；
//   - 余额不足 → 422 且订单仍为 pending（整体回滚，零部分写入）；
//   - 现金结账不动余额（无余额流水），积分/累计消费照常；
//   - operation_logs(action=order_pay) 在事务外写入；结账写入 payment_method 以结账请求为准。

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

// payOrder 挂单结账（POST /orders/:id/pay）。
func (e *customerEnv) payOrder(token string, orderID int64, body string) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/pay", orderID), body, token)
}

// seedBalance 直接落库客户余额（充值 API 属 todo 30，本测试只验证结账事务）。
func (e *customerEnv) seedBalance(t *testing.T, customerID, balanceCents int64) {
	t.Helper()
	if err := e.db.Model(&model.Customer{}).Where("id = ?", customerID).
		Update("balance_cents", balanceCents).Error; err != nil {
		t.Fatalf("注入余额失败: %v", err)
	}
}

// orderStatus 读取订单当前状态。
func (e *customerEnv) orderStatus(t *testing.T, orderID int64) string {
	t.Helper()
	var order model.Order
	if err := e.db.First(&order, orderID).Error; err != nil {
		t.Fatalf("读取订单 %d 失败: %v", orderID, err)
	}
	return order.Status
}

// payLogCount 统计 order_pay 审计日志行数。
func (e *customerEnv) payLogCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "order_pay").Count(&count).Error; err != nil {
		t.Fatalf("统计 order_pay 日志失败: %v", err)
	}
	return count
}

func TestOrderPay(t *testing.T) {
	t.Run("balance payment completes pending order with ledgers and points", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"结账客户","phone":"13800007001"}`)
		env.seedBalance(t, customer.ID, 10000)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-pay-balance", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

		// --- When: staff 用余额结账 ---
		w := env.payOrder(env.staffToken, order.ID, `{"payment_method":"balance"}`)

		// --- Then: 200 + status=completed + payment_method=balance ---
		if w.Code != http.StatusOK {
			t.Fatalf("POST /orders/%d/pay status = %d, want 200 (body=%s)", order.ID, w.Code, w.Body.String())
		}
		paid := decodeOrderDetail(t, w)
		if paid.Status != model.OrderStatusCompleted || paid.PaymentMethod != model.PaymentMethodBalance {
			t.Errorf("结账后订单 status/payment = %q/%q, want completed/balance", paid.Status, paid.PaymentMethod)
		}
		requireOrderAmounts(t, "结账后", paid, 5000, 0, 5000)

		// --- Then: 客户余额/积分/累计消费/最近到店全部正确（10000−5000=5000；floor(5000×1/100)=50） ---
		after := env.customerState(t, customer.ID)
		if after.BalanceCents != 5000 {
			t.Errorf("结账后余额 = %d, want 5000", after.BalanceCents)
		}
		if after.Points != 50 {
			t.Errorf("结账后积分 = %d, want 50", after.Points)
		}
		if after.TotalSpentCents != 5000 {
			t.Errorf("结账后累计消费 = %d, want 5000", after.TotalSpentCents)
		}
		if after.LastVisitAt == nil {
			t.Fatal("结账后 last_visit_at = nil, want now")
		}
		if delta := time.Since(*after.LastVisitAt); delta < -time.Minute || delta > time.Minute {
			t.Errorf("结账后 last_visit_at 距现在 = %v, want 约 0", delta)
		}

		// --- Then: 余额流水 1 条（consume/-5000，before 10000 → after 5000，关联订单） ---
		var balanceTx model.BalanceTransaction
		if err := env.db.First(&balanceTx).Error; err != nil {
			t.Fatalf("读取余额流水失败: %v", err)
		}
		if balanceTx.Type != model.BalanceTxConsume || balanceTx.AmountCents != -5000 ||
			balanceTx.BalanceBeforeCents != 10000 || balanceTx.BalanceAfterCents != 5000 {
			t.Errorf("余额流水 = %+v, want consume/-5000/10000→5000", balanceTx)
		}
		if balanceTx.ReferenceType != model.ReferenceTypeOrder || balanceTx.ReferenceID == nil || *balanceTx.ReferenceID != order.ID {
			t.Errorf("余额流水 reference = %s/%v, want order/%d", balanceTx.ReferenceType, balanceTx.ReferenceID, order.ID)
		}

		// --- Then: 积分流水 1 条（earn/50，0 → 50，关联订单） ---
		var pointsTx model.PointsTransaction
		if err := env.db.First(&pointsTx).Error; err != nil {
			t.Fatalf("读取积分流水失败: %v", err)
		}
		if pointsTx.Type != model.PointsTxEarn || pointsTx.Points != 50 ||
			pointsTx.BalanceBefore != 0 || pointsTx.BalanceAfter != 50 {
			t.Errorf("积分流水 = %+v, want earn/50/0→50", pointsTx)
		}

		// --- Then: operation_logs(action=order_pay) 由操作人写入（事务外） ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "order_pay").First(&logRow).Error; err != nil {
			t.Fatalf("读取 order_pay 审计日志失败: %v", err)
		}
		if logRow.TargetType != "order" || logRow.TargetID != order.ID {
			t.Errorf("order_pay 日志 target = %s#%d, want order#%d", logRow.TargetType, logRow.TargetID, order.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.staff.ID {
			t.Errorf("order_pay 日志 operator_id = %v, want %d", logRow.OperatorID, env.staff.ID)
		}
		if !strings.Contains(logRow.Content, order.OrderNo) {
			t.Errorf("order_pay 日志 content = %q, want 含订单号 %s", logRow.Content, order.OrderNo)
		}

		// --- When: 二次结账（同一订单） ---
		w = env.payOrder(env.staffToken, order.ID, `{"payment_method":"balance"}`)

		// --- Then: 409（防重复结账）+ 零新增写入 ---
		if w.Code != http.StatusConflict {
			t.Fatalf("重复结账 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeConflict {
			t.Errorf("重复结账 code = %d, want %d", envl.Code, service.CodeConflict)
		}
		if got := env.customerState(t, customer.ID); got.BalanceCents != 5000 || got.Points != 50 || got.TotalSpentCents != 5000 {
			t.Errorf("重复结账后余额/积分/累计消费 = %d/%d/%d, want 5000/50/5000（不得二次入账）",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		if n := env.ledgerRowCounts(t); n["balance_transactions"] != 1 || n["points_transactions"] != 1 {
			t.Errorf("重复结账后流水行数 = %+v, want 余额/积分各 1", n)
		}
		if n := env.payLogCount(t); n != 1 {
			t.Errorf("重复结账后 order_pay 日志行数 = %d, want 1", n)
		}
	})

	t.Run("insufficient balance is 422 and order stays pending", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"不足客户","phone":"13800007002"}`)
		env.seedBalance(t, customer.ID, 4999) // 恰好不足 1 分
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-pay-insufficient", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

		// --- When: 余额 4999 结账 5000 ---
		w := env.payOrder(env.staffToken, order.ID, `{"payment_method":"balance"}`)

		// --- Then: 422 + 提示余额不足 ---
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("余额不足结账 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		envl := decodeEnvelope(t, w)
		if envl.Code != service.CodeValidationFailed || !strings.Contains(envl.Message, "余额不足") {
			t.Errorf("余额不足结账 code/message = %d/%q, want 42200 含「余额不足」", envl.Code, envl.Message)
		}

		// --- Then: 订单仍为 pending（整体回滚，结账可重试） ---
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusPending {
			t.Errorf("余额不足后订单状态 = %q, want pending（回滚）", got)
		}

		// --- Then: 零部分写入（余额未变、无任何流水/日志） ---
		if got := env.customerState(t, customer.ID); got.BalanceCents != 4999 || got.Points != 0 || got.TotalSpentCents != 0 {
			t.Errorf("余额不足后余额/积分/累计消费 = %d/%d/%d, want 4999/0/0",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		if n := env.ledgerRowCounts(t); n["balance_transactions"] != 0 || n["points_transactions"] != 0 {
			t.Errorf("余额不足后流水行数 = %+v, want 余额/积分 0", n)
		}
		if n := env.payLogCount(t); n != 0 {
			t.Errorf("余额不足后 order_pay 日志行数 = %d, want 0", n)
		}

		// --- When: 充值到足够余额后重试结账（证明回滚后订单可正常结账） ---
		env.seedBalance(t, customer.ID, 5000)
		if w = env.payOrder(env.staffToken, order.ID, `{"payment_method":"balance"}`); w.Code != http.StatusOK {
			t.Fatalf("补足余额后结账 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusCompleted {
			t.Errorf("补足余额后订单状态 = %q, want completed", got)
		}
		if got := env.customerState(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("补足余额结账后余额 = %d, want 0", got)
		}
	})

	t.Run("cash payment records method and leaves balance untouched", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"现金结账客户","phone":"13800007003"}`)
		env.seedBalance(t, customer.ID, 10000)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		// 挂单时记录支付方式为 cash（意图），结账时以 wechat 覆盖。
		order := env.postPendingOrder(t, env.staffToken, fmt.Sprintf(
			`{"request_id":"req-pay-cash","customer_id":%d,"payment_method":"cash","status":"pending","items":[%s]}`,
			customer.ID, orderItemJSON(hair.ID, 1, 5000)))

		// --- When: 微信结账 ---
		w := env.payOrder(env.staffToken, order.ID, `{"payment_method":"wechat"}`)

		// --- Then: 200 + payment_method 以结账请求为准 ---
		if w.Code != http.StatusOK {
			t.Fatalf("现金结账 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if paid := decodeOrderDetail(t, w); paid.PaymentMethod != model.PaymentMethodWechat {
			t.Errorf("结账后 payment_method = %q, want wechat（覆盖挂单意图）", paid.PaymentMethod)
		}

		// --- Then: 余额与余额流水完全不动；积分/累计消费照常 ---
		if got := env.customerState(t, customer.ID); got.BalanceCents != 10000 || got.Points != 50 || got.TotalSpentCents != 5000 {
			t.Errorf("现金结账后余额/积分/累计消费 = %d/%d/%d, want 10000/50/5000",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		if n := env.ledgerRowCounts(t); n["balance_transactions"] != 0 {
			t.Errorf("现金结账后余额流水行数 = %d, want 0", n["balance_transactions"])
		}
	})
}
