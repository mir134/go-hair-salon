package controller_test

// TestOrderPayGuards 是 todo 27 的对抗性/边界验收测试
// （`-run TestOrderPay` 同时匹配本函数，见计划 todo 27 验收标准）：
//   - stale_state：并发双结账恰好一次成功（原子条件状态迁移，`-count=3` 反抖动）；
//   - malformed_input：非法/缺失支付方式 → 400、不存在订单 → 404、非法 id → 400、未登录 → 401；
//   - 状态冲突：completed 订单再次结账 → 409；校验失败后挂单仍可正常结账（无残留状态）。

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

func TestOrderPayGuards(t *testing.T) {
	t.Run("concurrent double pay completes exactly once", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"并发结账客户","phone":"13800007004"}`)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-pay-concurrent", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

		// --- When: 2 个 goroutine 同时结账（现金） ---
		const callers = 2
		codes := make([]int, callers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		path := fmt.Sprintf("/api/v1/orders/%d/pay", order.ID)
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-start
				w := env.authed(http.MethodPost, path, `{"payment_method":"cash"}`, env.staffToken)
				codes[idx] = w.Code
			}(i)
		}
		close(start)
		wg.Wait()

		// --- Then: 恰好 1 次 200，其余 409（原子条件状态迁移） ---
		success, conflict := 0, 0
		for i, code := range codes {
			switch code {
			case http.StatusOK:
				success++
			case http.StatusConflict:
				conflict++
			default:
				t.Errorf("并发结账调用 %d status = %d, want 200 或 409", i, code)
			}
		}
		if success != 1 || conflict != callers-1 {
			t.Errorf("并发结账结果 = 成功 %d/冲突 %d, want 1/%d", success, conflict, callers-1)
		}

		// --- Then: DB 只入账一次：completed、1 条积分流水、1 条 order_pay 日志、累计消费 5000 ---
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusCompleted {
			t.Errorf("并发结账后订单状态 = %q, want completed", got)
		}
		if n := env.ledgerRowCounts(t); n["points_transactions"] != 1 {
			t.Errorf("并发结账后积分流水行数 = %d, want 1", n["points_transactions"])
		}
		if n := env.payLogCount(t); n != 1 {
			t.Errorf("并发结账后 order_pay 日志行数 = %d, want 1", n)
		}
		if got := env.customerState(t, customer.ID); got.TotalSpentCents != 5000 || got.Points != 50 {
			t.Errorf("并发结账后累计消费/积分 = %d/%d, want 5000/50", got.TotalSpentCents, got.Points)
		}
	})

	t.Run("malformed input and state conflicts", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"结账校验客户","phone":"13800007005"}`)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-pay-guard", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

		// 非法支付方式 → 400；缺失支付方式 → 400
		if w := env.payOrder(env.staffToken, order.ID, `{"payment_method":"points"}`); w.Code != http.StatusBadRequest {
			t.Errorf("非法支付方式结账 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		if w := env.payOrder(env.staffToken, order.ID, `{}`); w.Code != http.StatusBadRequest {
			t.Errorf("缺失支付方式结账 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		// 不存在订单 → 404；非法 id → 400
		if w := env.payOrder(env.staffToken, 999999, `{"payment_method":"cash"}`); w.Code != http.StatusNotFound {
			t.Errorf("不存在订单结账 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		if w := env.payOrder(env.staffToken, 0, `{"payment_method":"cash"}`); w.Code != http.StatusBadRequest {
			t.Errorf("非法订单 id 结账 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		// 未登录 → 401
		if w := env.do(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/pay", order.ID), `{"payment_method":"cash"}`, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录结账 status = %d, want 401", w.Code)
		}
		// 直接完成的订单再次结账 → 409
		completed := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-pay-already", customer.ID, nil, "cash", orderItemJSON(hair.ID, 1, 5000)))
		if w := env.payOrder(env.staffToken, completed.ID, `{"payment_method":"cash"}`); w.Code != http.StatusConflict {
			t.Errorf("completed 订单结账 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		// 校验失败后挂单仍可正常结账（无残留状态）
		if w := env.payOrder(env.staffToken, order.ID, `{"payment_method":"cash"}`); w.Code != http.StatusOK {
			t.Fatalf("校验失败后正常结账 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusCompleted {
			t.Errorf("校验失败后结账状态 = %q, want completed", got)
		}
	})
}
