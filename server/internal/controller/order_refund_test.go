package controller_test

// TestOrderRefund 是 todo 35 的验收测试（任务书：`go test ./internal/controller -run TestOrderRefund -v -count=1`）：
//   - POST /orders/:id/refund 仅 admin（04-API.md:129,139、06 §7:81）：staff → 403 且零写入；
//   - 仅 completed 可退款（否则 409：pending/cancelled/refunded 均不可重复退款，04-API.md:155）；
//   - 余额支付订单：写反向 balance_transactions(type=refund, +金额) 且余额复原（06 §5:61）；
//   - 现金/微信/支付宝订单：余额与余额流水完全不动，仅反向积分（06 §5:56-65）；
//   - 积分按原 earn 反向扣减（type=refund, -原 earn）；不足 → 422 且整体回滚（06 §6:73-74 积分不得低于 0）；
//   - total_spent_cents 扣减退款金额；orders.status → refunded（营业额按退款发生日冲减，06 §8:91,98）；
//   - operation_logs(action=order_refund) 在事务提交后写入（硬规则：WriteLog 事务外）；
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

// refundOrder 发起订单退款（POST /orders/:id/refund）。
func (e *customerEnv) refundOrder(token string, orderID int64) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/refund", orderID), "", token)
}

// refundLogCount 统计 order_refund 审计日志行数。
func (e *customerEnv) refundLogCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "order_refund").Count(&count).Error; err != nil {
		t.Fatalf("统计 order_refund 日志失败: %v", err)
	}
	return count
}

// pointsLedgerRows 读取客户的积分流水（id 升序=写入顺序，用于断言 before/after 连续）。
func (e *customerEnv) pointsLedgerRows(t *testing.T, customerID int64) []model.PointsTransaction {
	t.Helper()
	var rows []model.PointsTransaction
	if err := e.db.Where("customer_id = ?", customerID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取积分流水失败: %v", err)
	}
	return rows
}

func TestOrderRefund(t *testing.T) {
	t.Run("balance payment refund restores balance and reverses ledgers", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"退款客户甲","phone":"13800009001"}`)
		env.seedBalance(t, customer.ID, 10000)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-refund-balance", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 5000)))

		// --- Given: 结账后余额 5000、积分 50、累计消费 5000 ---
		if got := env.customerState(t, customer.ID); got.BalanceCents != 5000 || got.Points != 50 || got.TotalSpentCents != 5000 {
			t.Fatalf("退款前余额/积分/累计消费 = %d/%d/%d, want 5000/50/5000",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}

		// --- When: admin 全额退款 ---
		w := env.refundOrder(env.adminToken, order.ID)

		// --- Then: 200 + status=refunded + 金额与明细保留 ---
		if w.Code != http.StatusOK {
			t.Fatalf("POST /orders/%d/refund status = %d, want 200 (body=%s)", order.ID, w.Code, w.Body.String())
		}
		refunded := decodeOrderDetail(t, w)
		if refunded.Status != model.OrderStatusRefunded {
			t.Errorf("退款后订单状态 = %q, want refunded", refunded.Status)
		}
		if refunded.CustomerName != "退款客户甲" || refunded.OrderNo != order.OrderNo {
			t.Errorf("退款后 customer_name/order_no = %q/%q, want 退款客户甲/%s",
				refunded.CustomerName, refunded.OrderNo, order.OrderNo)
		}
		requireOrderAmounts(t, "退款后", refunded, 5000, 0, 5000)
		if len(refunded.Items) != 1 || refunded.Items[0].AmountCents != 5000 {
			t.Errorf("退款后明细 = %+v, want 保留 1 条 5000 分明细", refunded.Items)
		}
		// 营业额冲减口径：退款发生日 = 本次 updated_at（06 §8:98），不得回改 created_at。
		if refunded.UpdatedAt.Before(order.CreatedAt) {
			t.Errorf("退款后 updated_at = %v, want 不早于 created_at %v", refunded.UpdatedAt, order.CreatedAt)
		}

		// --- Then: 客户余额复原、积分清零、累计消费扣减退款金额 ---
		after := env.customerState(t, customer.ID)
		if after.BalanceCents != 10000 {
			t.Errorf("退款后余额 = %d, want 10000（复原）", after.BalanceCents)
		}
		if after.Points != 0 {
			t.Errorf("退款后积分 = %d, want 0（反向扣减 50）", after.Points)
		}
		if after.TotalSpentCents != 0 {
			t.Errorf("退款后累计消费 = %d, want 0（扣减退款金额 5000）", after.TotalSpentCents)
		}

		// --- Then: 余额流水 2 条（consume/-5000 后 refund/+5000，before/after 连续） ---
		rows := env.rechargeLedgerRows(t, customer.ID)
		if len(rows) != 2 {
			t.Fatalf("余额流水条数 = %d, want 2（consume + refund）", len(rows))
		}
		refundRow := rows[1]
		if refundRow.Type != model.BalanceTxRefund || refundRow.AmountCents != 5000 ||
			refundRow.BalanceBeforeCents != 5000 || refundRow.BalanceAfterCents != 10000 {
			t.Errorf("退款余额流水 = %+v, want refund/+5000/5000→10000", refundRow)
		}
		if refundRow.ReferenceType != model.ReferenceTypeOrder || refundRow.ReferenceID == nil || *refundRow.ReferenceID != order.ID {
			t.Errorf("退款余额流水 reference = %s/%v, want order/%d", refundRow.ReferenceType, refundRow.ReferenceID, order.ID)
		}
		if refundRow.OperatorID == nil || *refundRow.OperatorID != env.admin.ID {
			t.Errorf("退款余额流水 operator_id = %v, want %d", refundRow.OperatorID, env.admin.ID)
		}

		// --- Then: 积分流水 2 条（earn/50 后 refund/-50，before/after 连续） ---
		prows := env.pointsLedgerRows(t, customer.ID)
		if len(prows) != 2 {
			t.Fatalf("积分流水条数 = %d, want 2（earn + refund）", len(prows))
		}
		pointsRefund := prows[1]
		if pointsRefund.Type != model.PointsTxRefund || pointsRefund.Points != -50 ||
			pointsRefund.BalanceBefore != 50 || pointsRefund.BalanceAfter != 0 {
			t.Errorf("退款积分流水 = %+v, want refund/-50/50→0", pointsRefund)
		}
		if pointsRefund.ReferenceType != model.ReferenceTypeOrder || pointsRefund.ReferenceID == nil || *pointsRefund.ReferenceID != order.ID {
			t.Errorf("退款积分流水 reference = %s/%v, want order/%d", pointsRefund.ReferenceType, pointsRefund.ReferenceID, order.ID)
		}
		if pointsRefund.OperatorID == nil || *pointsRefund.OperatorID != env.admin.ID {
			t.Errorf("退款积分流水 operator_id = %v, want %d", pointsRefund.OperatorID, env.admin.ID)
		}

		// --- Then: operation_logs(action=order_refund) 事务外写入，operator=admin，content 含订单号 ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "order_refund").First(&logRow).Error; err != nil {
			t.Fatalf("读取 order_refund 审计日志失败: %v", err)
		}
		if logRow.TargetType != "order" || logRow.TargetID != order.ID {
			t.Errorf("order_refund 日志 target = %s#%d, want order#%d", logRow.TargetType, logRow.TargetID, order.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
			t.Errorf("order_refund 日志 operator_id = %v, want %d", logRow.OperatorID, env.admin.ID)
		}
		if !strings.Contains(logRow.Content, order.OrderNo) {
			t.Errorf("order_refund 日志 content = %q, want 含订单号 %s", logRow.Content, order.OrderNo)
		}

		// --- When: 二次退款 ---
		base := env.ledgerRowCounts(t)
		w = env.refundOrder(env.adminToken, order.ID)

		// --- Then: 409（防重复退款/重复入账）+ 零新增写入 + 余额不被二次复原 ---
		if w.Code != http.StatusConflict {
			t.Fatalf("二次退款 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeConflict {
			t.Errorf("二次退款 code = %d, want %d", envl.Code, service.CodeConflict)
		}
		if got := env.customerState(t, customer.ID); got.BalanceCents != 10000 || got.Points != 0 || got.TotalSpentCents != 0 {
			t.Errorf("二次退款后余额/积分/累计消费 = %d/%d/%d, want 10000/0/0（不得二次入账）",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		requireCountsEqual(t, "二次退款", base, env.ledgerRowCounts(t))
		if n := env.refundLogCount(t); n != 1 {
			t.Errorf("二次退款后 order_refund 日志行数 = %d, want 1", n)
		}
	})

	t.Run("cash payment refund keeps balance untouched but reverses points", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"退款客户乙","phone":"13800009002"}`)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-refund-cash", customer.ID, nil, "cash", orderItemJSON(hair.ID, 1, 5000)))

		// --- Given: 现金结账后余额 0、积分 50、累计消费 5000 ---
		if got := env.customerState(t, customer.ID); got.BalanceCents != 0 || got.Points != 50 || got.TotalSpentCents != 5000 {
			t.Fatalf("退款前余额/积分/累计消费 = %d/%d/%d, want 0/50/5000",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}

		// --- When: admin 全额退款（现金订单） ---
		w := env.refundOrder(env.adminToken, order.ID)

		// --- Then: 200 + refunded + 现金订单余额与余额流水完全不动 ---
		if w.Code != http.StatusOK {
			t.Fatalf("现金订单退款 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if got := decodeOrderDetail(t, w).Status; got != model.OrderStatusRefunded {
			t.Errorf("现金订单退款后状态 = %q, want refunded", got)
		}
		after := env.customerState(t, customer.ID)
		if after.BalanceCents != 0 {
			t.Errorf("现金订单退款后余额 = %d, want 0（不动余额）", after.BalanceCents)
		}
		if after.Points != 0 || after.TotalSpentCents != 0 {
			t.Errorf("现金订单退款后积分/累计消费 = %d/%d, want 0/0（仅积分反向 + 营业额冲减）",
				after.Points, after.TotalSpentCents)
		}
		if rows := env.rechargeLedgerRows(t, customer.ID); len(rows) != 0 {
			t.Errorf("现金订单退款后余额流水条数 = %d, want 0（不产生余额流水）", len(rows))
		}

		// --- Then: 积分反向扣减（earn 50 → refund -50，before/after 连续） ---
		prows := env.pointsLedgerRows(t, customer.ID)
		if len(prows) != 2 {
			t.Fatalf("积分流水条数 = %d, want 2（earn + refund）", len(prows))
		}
		if prows[1].Type != model.PointsTxRefund || prows[1].Points != -50 ||
			prows[1].BalanceBefore != 50 || prows[1].BalanceAfter != 0 {
			t.Errorf("现金订单退款积分流水 = %+v, want refund/-50/50→0", prows[1])
		}
		if n := env.refundLogCount(t); n != 1 {
			t.Errorf("现金订单退款后 order_refund 日志行数 = %d, want 1", n)
		}
	})

	t.Run("staff is forbidden with zero writes", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"退款客户丙","phone":"13800009003"}`)
		env.seedBalance(t, customer.ID, 10000)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-refund-staff", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 5000)))

		// --- When: staff 尝试退款 ---
		base := env.ledgerRowCounts(t)
		w := env.refundOrder(env.staffToken, order.ID)

		// --- Then: 403（路由 admin 分组；后端是最终边界）+ 零写入 + 订单仍 completed ---
		if w.Code != http.StatusForbidden {
			t.Fatalf("staff 退款 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
			t.Errorf("staff 退款 code = %d, want %d", envl.Code, service.CodeForbidden)
		}
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusCompleted {
			t.Errorf("staff 退款后订单状态 = %q, want completed（403 不得改动状态）", got)
		}
		if got := env.customerState(t, customer.ID); got.BalanceCents != 5000 || got.Points != 50 || got.TotalSpentCents != 5000 {
			t.Errorf("staff 退款后余额/积分/累计消费 = %d/%d/%d, want 5000/50/5000",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		requireCountsEqual(t, "staff 403", base, env.ledgerRowCounts(t))
		if n := env.refundLogCount(t); n != 0 {
			t.Errorf("staff 退款后 order_refund 日志行数 = %d, want 0", n)
		}
	})

	t.Run("insufficient points refund is 422 with full rollback", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"退款客户丁","phone":"13800009004"}`)
		env.seedBalance(t, customer.ID, 10000)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)
		order := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-refund-points", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 5000)))

		// --- Given: 客户积分被消耗至 10（不足反向扣减原 earn 50） ---
		if err := env.db.Model(&model.Customer{}).Where("id = ?", customer.ID).Update("points", 10).Error; err != nil {
			t.Fatalf("消耗客户积分失败: %v", err)
		}
		base := env.ledgerRowCounts(t)

		// --- When: admin 退款 ---
		w := env.refundOrder(env.adminToken, order.ID)

		// --- Then: 422（积分不得低于 0）+ 订单仍 completed + 整体回滚（余额复原也被撤销） ---
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("积分不足退款 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		envl := decodeEnvelope(t, w)
		if envl.Code != service.CodeValidationFailed || !strings.Contains(envl.Message, "积分") {
			t.Errorf("积分不足退款 code/message = %d/%q, want 42200 含「积分」", envl.Code, envl.Message)
		}
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusCompleted {
			t.Errorf("积分不足退款后订单状态 = %q, want completed（整体回滚）", got)
		}
		if got := env.customerState(t, customer.ID); got.BalanceCents != 5000 || got.Points != 10 || got.TotalSpentCents != 5000 {
			t.Errorf("积分不足退款后余额/积分/累计消费 = %d/%d/%d, want 5000/10/5000（零部分写入）",
				got.BalanceCents, got.Points, got.TotalSpentCents)
		}
		requireCountsEqual(t, "积分不足 422", base, env.ledgerRowCounts(t))
		if n := env.refundLogCount(t); n != 0 {
			t.Errorf("积分不足退款后 order_refund 日志行数 = %d, want 0", n)
		}

		// --- When: 恢复积分后重试退款（证明回滚后无残留、可正常退款） ---
		if err := env.db.Model(&model.Customer{}).Where("id = ?", customer.ID).Update("points", 50).Error; err != nil {
			t.Fatalf("恢复客户积分失败: %v", err)
		}
		if w = env.refundOrder(env.adminToken, order.ID); w.Code != http.StatusOK {
			t.Fatalf("恢复积分后退款 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.orderStatus(t, order.ID); got != model.OrderStatusRefunded {
			t.Errorf("恢复积分后订单状态 = %q, want refunded", got)
		}
		after := env.customerState(t, customer.ID)
		if after.BalanceCents != 10000 || after.Points != 0 || after.TotalSpentCents != 0 {
			t.Errorf("恢复积分退款后余额/积分/累计消费 = %d/%d/%d, want 10000/0/0",
				after.BalanceCents, after.Points, after.TotalSpentCents)
		}
		if n := env.refundLogCount(t); n != 1 {
			t.Errorf("成功退款后 order_refund 日志行数 = %d, want 1", n)
		}
	})

	t.Run("state guards and malformed input", func(t *testing.T) {
		env := newCustomerEnv(t)
		customer := env.createCustomer(t, env.staffToken, `{"name":"退款守卫客户","phone":"13800009005"}`)
		hair := env.seedServiceViaAPI(t, "剪发", 5000)

		// --- When/Then: pending 挂单不可退款 → 409 ---
		pending := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-refund-pending", customer.ID, orderItemJSON(hair.ID, 1, 5000)))
		w := env.refundOrder(env.adminToken, pending.ID)
		if w.Code != http.StatusConflict {
			t.Errorf("pending 订单退款 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if got := env.orderStatus(t, pending.ID); got != model.OrderStatusPending {
			t.Errorf("pending 订单退款后状态 = %q, want pending", got)
		}

		// --- When/Then: cancelled 订单不可退款 → 409 ---
		cancelled := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-refund-cancelled", customer.ID, orderItemJSON(hair.ID, 1, 5000)))
		if w = env.cancelOrder(env.adminToken, cancelled.ID); w.Code != http.StatusOK {
			t.Fatalf("取消挂单 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if w = env.refundOrder(env.adminToken, cancelled.ID); w.Code != http.StatusConflict {
			t.Errorf("cancelled 订单退款 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}

		// --- When/Then: 不存在订单 → 404、非法 id → 400、未登录 → 401 ---
		if w = env.refundOrder(env.adminToken, 999999); w.Code != http.StatusNotFound {
			t.Errorf("不存在订单退款 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		if w = env.refundOrder(env.adminToken, 0); w.Code != http.StatusBadRequest {
			t.Errorf("非法订单 id 退款 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		if w = env.do(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/refund", pending.ID), "", nil); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录退款 status = %d, want 401", w.Code)
		}

		// --- Then: 全部失败路径零写入（无订单被退款、无账务流水、无退款日志） ---
		if got := env.orderStatus(t, pending.ID); got != model.OrderStatusPending {
			t.Errorf("守卫用例后 pending 订单状态 = %q, want pending", got)
		}
		if got := env.orderStatus(t, cancelled.ID); got != model.OrderStatusCancelled {
			t.Errorf("守卫用例后 cancelled 订单状态 = %q, want cancelled", got)
		}
		if c := env.ledgerRowCounts(t); c["balance_transactions"] != 0 || c["points_transactions"] != 0 {
			t.Errorf("守卫用例后账本行数 = %+v, want 余额/积分 0", c)
		}
		if n := env.refundLogCount(t); n != 0 {
			t.Errorf("守卫用例后 order_refund 日志行数 = %d, want 0", n)
		}
	})
}
