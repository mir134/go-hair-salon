package controller_test

// TestPendingOrderEditGuards 是 todo 26 的对抗性/边界验收测试
// （`-run TestPendingOrderEdit` 同时匹配本函数，见计划 todo 26 验收标准）：
//   - malformed_input：数量 ≤0 → 400、未提供字段 → 400、未知明细 → 404、
//     未知/停用服务 → 404/422、非法 status → 400、非法 id → 400；
//   - 改价权限：staff 改价 → 403、admin 缺 reason → 400、admin 带 reason → 200 + operation_logs；
//   - stale_state：completed 订单编辑 → 409；不存在订单 → 404；最后一条明细不可删除 → 422；
//   - 员工快照继承；挂单不自动过期（06 §3:33）。

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

func TestPendingOrderEditGuards(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"挂单守卫客户","phone":"13800006011"}`)
	employee := seedEmployee(t, env, "挂单员工")
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	perm := env.seedServiceViaAPI(t, "烫发", 8000)
	order := env.postPendingOrder(t, env.staffToken,
		pendingOrderJSON("req-pending-guard", customer.ID, orderItemJSON(hair.ID, 1, 5000)))
	hairItemID := order.Items[0].ID

	// --- When/Then: malformed_input（数量 ≤0、字段缺失、未知明细/服务、停用服务） ---
	w := env.updateOrderItem(env.staffToken, order.ID, hairItemID, `{"quantity":0}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("改数量 0 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.staffToken, order.ID, hairItemID, `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("改数量/改价均未提供 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.staffToken, order.ID, 999999, `{"quantity":2}`); w.Code != http.StatusNotFound {
		t.Errorf("未知明细 PUT status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.deleteOrderItem(env.staffToken, order.ID, 999999); w.Code != http.StatusNotFound {
		t.Errorf("未知明细 DELETE status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.addOrderItem(env.staffToken, order.ID, orderItemJSON(hair.ID, 0, 5000)); w.Code != http.StatusBadRequest {
		t.Errorf("追加数量 0 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.addOrderItem(env.staffToken, order.ID, orderItemJSON(999999, 1, 0)); w.Code != http.StatusNotFound {
		t.Errorf("追加未知服务 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	disabled := env.createService(t, env.adminToken,
		fmt.Sprintf(`{"category_id":%d,"name":"停用服务","price_cents":3000,"status":0}`, hair.CategoryID))
	if w = env.addOrderItem(env.staffToken, order.ID, orderItemJSON(disabled.ID, 1, 0)); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("追加停用服务 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	// 最后一条明细不可删除（订单至少保留一个服务项目）
	if w = env.deleteOrderItem(env.staffToken, order.ID, hairItemID); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("删除最后一条明细 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 改价权限（staff 403、admin 缺 reason 400、admin 带 reason 200 + 日志） ---
	override := `{"unit_price_cents":4000,"discount_reason":"老客户折扣"}`
	if w = env.updateOrderItem(env.staffToken, order.ID, hairItemID, override); w.Code != http.StatusForbidden {
		t.Errorf("staff 改价 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.adminToken, order.ID, hairItemID, `{"unit_price_cents":4000}`); w.Code != http.StatusBadRequest {
		t.Errorf("admin 改价缺 reason status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	w = env.updateOrderItem(env.adminToken, order.ID, hairItemID, override)
	if w.Code != http.StatusOK {
		t.Fatalf("admin 改价 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	updated := decodeOrderDetail(t, w)
	requireOrderAmounts(t, "改价后", updated, 5000, 1000, 4000)
	if updated.Items[0].UnitPriceCents != 4000 || updated.Items[0].DiscountAmountCents != 1000 {
		t.Errorf("改价明细 = %+v, want 单价 4000/优惠 1000", updated.Items[0])
	}
	var logRow model.OperationLog
	if err := env.db.Where("action = ?", "order_item_update").Order("id DESC").First(&logRow).Error; err != nil {
		t.Fatalf("读取 order_item_update 审计日志失败: %v", err)
	}
	if logRow.TargetType != "order" || logRow.TargetID != order.ID {
		t.Errorf("改价日志 target = %s#%d, want order#%d", logRow.TargetType, logRow.TargetID, order.ID)
	}
	if !strings.Contains(logRow.Content, "老客户折扣") || !strings.Contains(logRow.Content, "5000") || !strings.Contains(logRow.Content, "4000") {
		t.Errorf("改价日志 content = %q, want 含原价/成交价/原因", logRow.Content)
	}
	if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
		t.Errorf("改价日志 operator_id = %v, want %d", logRow.OperatorID, env.admin.ID)
	}
	// 改价全程仍不动账（余额/积分/充值流水为 0）
	if c := env.ledgerRowCounts(t); c["balance_transactions"] != 0 || c["points_transactions"] != 0 || c["recharge_records"] != 0 {
		t.Errorf("改价后账本行数 = %+v, want 余额/积分/充值 0", c)
	}
	if final := env.customerState(t, customer.ID); final.BalanceCents != 0 || final.Points != 0 || final.TotalSpentCents != 0 {
		t.Errorf("改价后客户余额/积分/累计消费 = %d/%d/%d, want 0/0/0",
			final.BalanceCents, final.Points, final.TotalSpentCents)
	}

	// --- When/Then: stale_state（completed 订单不可编辑 → 409；不存在订单 → 404） ---
	completedCustomer := env.createCustomer(t, env.staffToken, `{"name":"已结账客户","phone":"13800006012"}`)
	completed := env.postOrder(t, env.staffToken,
		orderCreateJSON("req-pending-completed", completedCustomer.ID, nil, "cash", orderItemJSON(hair.ID, 1, 5000)))
	if w = env.addOrderItem(env.staffToken, completed.ID, orderItemJSON(perm.ID, 1, 8000)); w.Code != http.StatusConflict {
		t.Errorf("completed 订单追加明细 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.staffToken, completed.ID, completed.Items[0].ID, `{"quantity":2}`); w.Code != http.StatusConflict {
		t.Errorf("completed 订单改数量 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.deleteOrderItem(env.staffToken, completed.ID, completed.Items[0].ID); w.Code != http.StatusConflict {
		t.Errorf("completed 订单删明细 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.addOrderItem(env.staffToken, 999999, orderItemJSON(hair.ID, 1, 5000)); w.Code != http.StatusNotFound {
		t.Errorf("不存在订单追加明细 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 非法 status / 未知订单/明细 id ---
	if w = env.authed(http.MethodPost, "/api/v1/orders",
		`{"request_id":"req-bad-status","customer_id":1,"status":"cancelled","items":[]}`, env.staffToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法 status status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.staffToken, 999999, 1, `{"quantity":2}`); w.Code != http.StatusNotFound {
		t.Errorf("不存在订单 PUT 明细 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.updateOrderItem(env.staffToken, order.ID, 0, `{"quantity":2}`); w.Code != http.StatusBadRequest {
		t.Errorf("非法明细 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- Then: 未登录 → 401 ---
	if w = env.do(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/items", order.ID), orderItemJSON(hair.ID, 1, 5000), nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录追加明细 status = %d, want 401", w.Code)
	}

	// --- Then: 员工快照随订单继承 ---
	withEmp := env.postPendingOrder(t, env.staffToken, fmt.Sprintf(
		`{"request_id":"req-pending-emp","customer_id":%d,"employee_id":%d,"status":"pending","items":[%s]}`,
		customer.ID, employee.ID, orderItemJSON(hair.ID, 1, 5000)))
	if withEmp.EmployeeID == nil || *withEmp.EmployeeID != employee.ID || withEmp.EmployeeName != "挂单员工" {
		t.Errorf("挂单员工 = %v/%q, want %d/挂单员工", withEmp.EmployeeID, withEmp.EmployeeName, employee.ID)
	}
	w = env.addOrderItem(env.staffToken, withEmp.ID, orderItemJSON(perm.ID, 1, 8000))
	if w.Code != http.StatusCreated {
		t.Fatalf("指定员工挂单追加明细 status = %d, want 201", w.Code)
	}
	withEmpDetail := decodeOrderDetail(t, w)
	if len(withEmpDetail.Items) != 2 || withEmpDetail.Items[1].EmployeeID == nil || *withEmpDetail.Items[1].EmployeeID != employee.ID {
		t.Errorf("追加明细 employee_id = %+v, want 继承订单员工 %d", withEmpDetail.Items, employee.ID)
	}

	// --- Then: 挂单不自动过期（06 §3:33）——状态保持 pending，且无任何过期/定时取消路径 ---
	var pending model.Order
	if err := env.db.First(&pending, order.ID).Error; err != nil {
		t.Fatalf("读取挂单失败: %v", err)
	}
	if pending.Status != model.OrderStatusPending {
		t.Errorf("挂单状态 = %q, want pending（不自动过期）", pending.Status)
	}
}
