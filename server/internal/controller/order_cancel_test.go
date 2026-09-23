package controller_test

// TestOrderCancel 是 todo 28 的验收测试（计划：`go test ./internal/controller -run TestOrderCancel -v -count=1`）：
//   - 取消仅 admin（04-API.md:152-153、06 §7）：staff → 403（后端是最终边界）；
//   - 仅 pending 可取消：completed/cancelled → 422（已结账只能退款，不能取消）；
//   - pending → cancelled；不产生任何资金/余额/积分变动；
//   - operation_logs(action=order_cancel) 在事务外写入，operator=admin；
//   - 状态守卫：取消后不可结账（409）、不可编辑明细（409）；
//   - 挂单不自动过期（06:33）：不取消则长期保留（本测试用 pending 前置验证）。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// cancelOrder 取消挂单（POST /orders/:id/cancel）。
func (e *customerEnv) cancelOrder(token string, orderID int64) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/cancel", orderID), "", token)
}

// cancelLogCount 统计 order_cancel 审计日志行数。
func (e *customerEnv) cancelLogCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.OperationLog{}).Where("action = ?", "order_cancel").Count(&count).Error; err != nil {
		t.Fatalf("统计 order_cancel 日志失败: %v", err)
	}
	return count
}

func TestOrderCancel(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"取消客户","phone":"13800008001"}`)
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	perm := env.seedServiceViaAPI(t, "烫发", 8000)
	order := env.postPendingOrder(t, env.staffToken,
		pendingOrderJSON("req-cancel-1", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

	// --- When: staff 取消挂单 ---
	w := env.cancelOrder(env.staffToken, order.ID)

	// --- Then: 403（路由 admin 分组；后端是最终边界）+ 订单仍 pending + 无取消日志 ---
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff 取消订单 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff 取消订单 code = %d, want %d", envl.Code, service.CodeForbidden)
	}
	if got := env.orderStatus(t, order.ID); got != model.OrderStatusPending {
		t.Errorf("staff 取消后订单状态 = %q, want pending（403 不得改动状态）", got)
	}
	if n := env.cancelLogCount(t); n != 0 {
		t.Errorf("staff 取消后 order_cancel 日志行数 = %d, want 0", n)
	}

	// --- When: admin 取消挂单 ---
	w = env.cancelOrder(env.adminToken, order.ID)

	// --- Then: 200 + status=cancelled + 金额保留（只改状态） ---
	if w.Code != http.StatusOK {
		t.Fatalf("admin 取消订单 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	cancelled := decodeOrderDetail(t, w)
	if cancelled.Status != model.OrderStatusCancelled {
		t.Errorf("取消后订单状态 = %q, want cancelled", cancelled.Status)
	}
	requireOrderAmounts(t, "取消后", cancelled, 5000, 0, 5000)
	if len(cancelled.Items) != 1 {
		t.Errorf("取消后明细条数 = %d, want 1（明细保留）", len(cancelled.Items))
	}

	// --- Then: 不产生任何资金/余额/积分变动 ---
	counts := env.ledgerRowCounts(t)
	for _, table := range []string{"balance_transactions", "points_transactions", "recharge_records"} {
		if counts[table] != 0 {
			t.Errorf("取消后 %s 行数 = %d, want 0（取消不动账）", table, counts[table])
		}
	}
	if got := env.customerState(t, customer.ID); got.BalanceCents != 0 || got.Points != 0 || got.TotalSpentCents != 0 || got.LastVisitAt != nil {
		t.Errorf("取消后客户余额/积分/累计消费/最近到店 = %d/%d/%d/%v, want 0/0/0/nil",
			got.BalanceCents, got.Points, got.TotalSpentCents, got.LastVisitAt)
	}

	// --- Then: operation_logs(action=order_cancel) 由 admin 写入，target=order#id ---
	var logRow model.OperationLog
	if err := env.db.Where("action = ?", "order_cancel").First(&logRow).Error; err != nil {
		t.Fatalf("读取 order_cancel 审计日志失败: %v", err)
	}
	if logRow.TargetType != "order" || logRow.TargetID != order.ID {
		t.Errorf("order_cancel 日志 target = %s#%d, want order#%d", logRow.TargetType, logRow.TargetID, order.ID)
	}
	if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
		t.Errorf("order_cancel 日志 operator_id = %v, want %d", logRow.OperatorID, env.admin.ID)
	}
	if !strings.Contains(logRow.Content, order.OrderNo) {
		t.Errorf("order_cancel 日志 content = %q, want 含订单号 %s", logRow.Content, order.OrderNo)
	}
	if n := env.cancelLogCount(t); n != 1 {
		t.Errorf("order_cancel 日志行数 = %d, want 1", n)
	}

	// --- When/Then: 状态守卫——已取消订单不可再取消（422） ---
	if w = env.cancelOrder(env.adminToken, order.ID); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("重复取消 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	if n := env.cancelLogCount(t); n != 1 {
		t.Errorf("重复取消后 order_cancel 日志行数 = %d, want 1（失败零写入）", n)
	}

	// --- When/Then: 已取消订单不可结账（409） ---
	if w = env.payOrder(env.staffToken, order.ID, `{"payment_method":"cash"}`); w.Code != http.StatusConflict {
		t.Errorf("已取消订单结账 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if got := env.orderStatus(t, order.ID); got != model.OrderStatusCancelled {
		t.Errorf("结账尝试后订单状态 = %q, want cancelled", got)
	}

	// --- When/Then: 已取消订单不可编辑明细（409） ---
	if w = env.addOrderItem(env.staffToken, order.ID, orderItemJSON(perm.ID, 1, 8000)); w.Code != http.StatusConflict {
		t.Errorf("已取消订单追加明细 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}

	// --- Then: 取消全程零资金变动（余额/积分/充值流水仍为 0；仅有审计日志） ---
	// 注意：必须在创建 completed 订单之前断言（completed 会合法产生积分流水与累计消费）。
	if c := env.ledgerRowCounts(t); c["balance_transactions"] != 0 || c["points_transactions"] != 0 || c["recharge_records"] != 0 {
		t.Errorf("取消全程后账本行数 = %+v, want 余额/积分/充值 0", c)
	}

	// --- When/Then: 已结账（completed）订单不可取消 → 422（只能退款） ---
	// completed 订单使用独立客户，避免干扰上述「取消全程零账务」断言。
	completedCustomer := env.createCustomer(t, env.staffToken, `{"name":"取消守卫客户","phone":"13800008002"}`)
	completed := env.postOrder(t, env.staffToken,
		orderCreateJSON("req-cancel-completed", completedCustomer.ID, nil, "cash", orderItemJSON(hair.ID, 1, 5000)))
	if w = env.cancelOrder(env.adminToken, completed.ID); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("completed 订单取消 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	if got := env.orderStatus(t, completed.ID); got != model.OrderStatusCompleted {
		t.Errorf("completed 订单取消后状态 = %q, want completed（422 不得改动状态）", got)
	}

	// --- When/Then: malformed——不存在订单 → 404、非法 id → 400、未登录 → 401 ---
	if w = env.cancelOrder(env.adminToken, 999999); w.Code != http.StatusNotFound {
		t.Errorf("不存在订单取消 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.cancelOrder(env.adminToken, 0); w.Code != http.StatusBadRequest {
		t.Errorf("非法订单 id 取消 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.do(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/cancel", order.ID), "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录取消 status = %d, want 401", w.Code)
	}

	// --- Then: 未取消的挂单不自动过期（06:33）——长期保留，仅管理员手动取消 ---
	kept := env.postPendingOrder(t, env.staffToken,
		pendingOrderJSON("req-cancel-kept", customer.ID, orderItemJSON(hair.ID, 1, 5000)))
	if got := env.orderStatus(t, kept.ID); got != model.OrderStatusPending {
		t.Errorf("未取消挂单状态 = %q, want pending（不自动过期）", got)
	}
}
