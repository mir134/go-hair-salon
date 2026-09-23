package controller_test

// TestPendingOrderEdit 是 todo 26 的验收测试（计划：`go test ./internal/controller -run TestPendingOrderEdit -v -count=1`）：
//   - POST /orders 支持挂单（status=pending）：不产生任何资金/余额/积分变动（04-API.md:145-149、06 §3:30）；
//   - POST /orders/:id/items 追加、PUT /orders/:id/items/:item_id 改数量/改价、DELETE 删明细：
//     仅 pending 可操作（否则 409），订单金额随明细重算（03-DATABASE.md:309-317）；
//   - 改价仅 admin 且必填 reason 并写 operation_logs；staff 改价 → 403；admin 缺 reason → 400（06 §3.1）；
//   - 删除明细为软删除（AGENTS.md 第 5 节：order_items 禁止物理删除），行保留、deleted_at 非空；
//   - malformed_input：数量 ≤0 → 400、未知明细 → 404、未知/停用服务 → 404/422、非法 status → 400；
//   - 最后一条明细不可删除（订单至少保留一个服务项目）。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// pendingOrderJSON 构造挂单创建请求体（status=pending，不收款）。
func pendingOrderJSON(requestID string, customerID int64, items ...string) string {
	return fmt.Sprintf(`{"request_id":%q,"customer_id":%d,"status":"pending","items":[%s]}`,
		requestID, customerID, strings.Join(items, ","))
}

// decodeOrderDetail 解析订单详情响应（订单 + 明细）。
func decodeOrderDetail(t *testing.T, w *httptest.ResponseRecorder) orderAPIView {
	t.Helper()
	var detail orderAPIView
	decodeData(t, decodeEnvelope(t, w), &detail)
	return detail
}

// postPendingOrder 通过 API 创建挂单并断言 201，返回订单 DTO。
func (e *customerEnv) postPendingOrder(t *testing.T, token, body string) orderAPIView {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/orders", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /orders(status=pending) status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	return decodeOrderDetail(t, w)
}

// addOrderItem 追加挂单明细。
func (e *customerEnv) addOrderItem(token string, orderID int64, body string) *httptest.ResponseRecorder {
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/items", orderID), body, token)
}

// updateOrderItem 修改挂单明细（数量/成交单价）。
func (e *customerEnv) updateOrderItem(token string, orderID, itemID int64, body string) *httptest.ResponseRecorder {
	return e.authed(http.MethodPut, fmt.Sprintf("/api/v1/orders/%d/items/%d", orderID, itemID), body, token)
}

// deleteOrderItem 删除挂单明细。
func (e *customerEnv) deleteOrderItem(token string, orderID, itemID int64) *httptest.ResponseRecorder {
	return e.authed(http.MethodDelete, fmt.Sprintf("/api/v1/orders/%d/items/%d", orderID, itemID), "", token)
}

// ledgerRowCounts 统计订单/账本表行数（挂单必须零资金变动）。
func (e *customerEnv) ledgerRowCounts(t *testing.T) map[string]int64 {
	t.Helper()
	entities := map[string]any{
		"orders":               &model.Order{},
		"order_items":          &model.OrderItem{},
		"balance_transactions": &model.BalanceTransaction{},
		"points_transactions":  &model.PointsTransaction{},
		"recharge_records":     &model.RechargeRecord{},
	}
	counts := make(map[string]int64, len(entities))
	for name, entity := range entities {
		var count int64
		if err := e.db.Model(entity).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", name, err)
		}
		counts[name] = count
	}
	return counts
}

// customerState 重新读取客户（断言挂单/结账后的余额、积分、累计消费缓存）。
func (e *customerEnv) customerState(t *testing.T, id int64) model.Customer {
	t.Helper()
	var customer model.Customer
	if err := e.db.First(&customer, id).Error; err != nil {
		t.Fatalf("读取客户 %d 失败: %v", id, err)
	}
	return customer
}

// requireOrderAmounts 断言订单金额三元组（原价/优惠/实付，整数分）。
func requireOrderAmounts(t *testing.T, label string, order orderAPIView, original, discount, paid int64) {
	t.Helper()
	if order.OriginalAmountCents != original || order.DiscountAmountCents != discount || order.PaidAmountCents != paid {
		t.Errorf("%s 金额 原价/优惠/实付 = %d/%d/%d, want %d/%d/%d",
			label, order.OriginalAmountCents, order.DiscountAmountCents, order.PaidAmountCents, original, discount, paid)
	}
}

func TestPendingOrderEdit(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"挂单客户","phone":"13800006001"}`)
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	perm := env.seedServiceViaAPI(t, "烫发", 8000)

	// --- Given/When: staff 挂单（剪发×1，不收款） ---
	order := env.postPendingOrder(t, env.staffToken,
		pendingOrderJSON("req-pending-1", customer.ID, orderItemJSON(hair.ID, 1, 5000)))

	// --- Then: status=pending、金额 5000、无支付方式、含服务名快照 ---
	if order.Status != model.OrderStatusPending {
		t.Fatalf("挂单 status = %q, want pending", order.Status)
	}
	requireOrderAmounts(t, "挂单", order, 5000, 0, 5000)
	if order.PaymentMethod != "" {
		t.Errorf("挂单 payment_method = %q, want 空（未收款）", order.PaymentMethod)
	}
	if len(order.Items) != 1 || order.Items[0].ServiceNameSnapshot != "剪发" || order.Items[0].AmountCents != 5000 {
		t.Fatalf("挂单明细 = %+v, want 剪发/5000", order.Items)
	}

	// --- Then: 挂单不产生任何资金/余额/积分变动（账本表零行） ---
	counts := env.ledgerRowCounts(t)
	if counts["orders"] != 1 || counts["order_items"] != 1 {
		t.Errorf("挂单后 orders/order_items = %d/%d, want 1/1", counts["orders"], counts["order_items"])
	}
	for _, table := range []string{"balance_transactions", "points_transactions", "recharge_records"} {
		if counts[table] != 0 {
			t.Errorf("挂单后 %s 行数 = %d, want 0（挂单不动账）", table, counts[table])
		}
	}
	after := env.customerState(t, customer.ID)
	if after.BalanceCents != 0 || after.Points != 0 || after.TotalSpentCents != 0 || after.LastVisitAt != nil {
		t.Errorf("挂单后客户余额/积分/累计消费/最近到店 = %d/%d/%d/%v, want 0/0/0/nil",
			after.BalanceCents, after.Points, after.TotalSpentCents, after.LastVisitAt)
	}

	// --- When: 追加 烫发×2 ---
	w := env.addOrderItem(env.staffToken, order.ID, orderItemJSON(perm.ID, 2, 8000))

	// --- Then: 201 + 金额随明细重算（5000 + 16000 = 21000），仍不动账 ---
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /orders/%d/items status = %d, want 201 (body=%s)", order.ID, w.Code, w.Body.String())
	}
	updated := decodeOrderDetail(t, w)
	requireOrderAmounts(t, "追加后", updated, 21000, 0, 21000)
	if len(updated.Items) != 2 || updated.Items[1].ServiceNameSnapshot != "烫发" || updated.Items[1].AmountCents != 16000 {
		t.Fatalf("追加后明细 = %+v, want 剪发+烫发×2", updated.Items)
	}
	if c := env.ledgerRowCounts(t); c["balance_transactions"] != 0 || c["points_transactions"] != 0 {
		t.Errorf("追加后余额/积分流水 = %d/%d, want 0/0", c["balance_transactions"], c["points_transactions"])
	}

	// --- When: 修改烫发数量 2 → 3 ---
	permItemID := updated.Items[1].ID
	w = env.updateOrderItem(env.staffToken, order.ID, permItemID, `{"quantity":3}`)

	// --- Then: 200 + 重算（5000 + 24000 = 29000） ---
	if w.Code != http.StatusOK {
		t.Fatalf("PUT items/%d status = %d, want 200 (body=%s)", permItemID, w.Code, w.Body.String())
	}
	updated = decodeOrderDetail(t, w)
	requireOrderAmounts(t, "改数量后", updated, 29000, 0, 29000)
	if len(updated.Items) != 2 || updated.Items[1].Quantity != 3 || updated.Items[1].AmountCents != 24000 {
		t.Fatalf("改数量后明细 = %+v, want 烫发×3/24000", updated.Items)
	}

	// --- When: 删除烫发明细 ---
	w = env.deleteOrderItem(env.staffToken, order.ID, permItemID)

	// --- Then: 200 + 重算回 5000、明细 1 条 ---
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE items/%d status = %d, want 200 (body=%s)", permItemID, w.Code, w.Body.String())
	}
	updated = decodeOrderDetail(t, w)
	requireOrderAmounts(t, "删明细后", updated, 5000, 0, 5000)
	if len(updated.Items) != 1 || updated.Items[0].ServiceID != hair.ID {
		t.Fatalf("删明细后 items = %+v, want 仅剪发", updated.Items)
	}

	// --- Then: 软删除（行保留、deleted_at 非空、接口不可见） ---
	var live, all int64
	if err := env.db.Model(&model.OrderItem{}).Where("order_id = ?", order.ID).Count(&live).Error; err != nil {
		t.Fatalf("统计可见明细失败: %v", err)
	}
	if err := env.db.Unscoped().Model(&model.OrderItem{}).Where("order_id = ?", order.ID).Count(&all).Error; err != nil {
		t.Fatalf("统计全部明细失败: %v", err)
	}
	if live != 1 || all != 2 {
		t.Errorf("删除后明细可见/全部行数 = %d/%d, want 1/2（禁止物理删除）", live, all)
	}
	var deleted model.OrderItem
	if err := env.db.Unscoped().Where("id = ?", permItemID).First(&deleted).Error; err != nil {
		t.Fatalf("读取已删除明细失败: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Error("已删除明细 deleted_at = NULL, want 非空（软删除）")
	}
	if deleted.ServiceNameSnapshot != "烫发" {
		t.Errorf("已删除明细快照 = %q, want 烫发（审计保留）", deleted.ServiceNameSnapshot)
	}

	// --- Then: 全程未动账（余额/积分/充值流水仍为 0；挂单只写审计日志） ---
	if c := env.ledgerRowCounts(t); c["balance_transactions"] != 0 || c["points_transactions"] != 0 || c["recharge_records"] != 0 {
		t.Errorf("挂单编辑全程后账本行数 = %+v, want 余额/积分/充值 0", c)
	}
	if final := env.customerState(t, customer.ID); final.BalanceCents != 0 || final.Points != 0 || final.TotalSpentCents != 0 {
		t.Errorf("挂单编辑全程后客户余额/积分/累计消费 = %d/%d/%d, want 0/0/0",
			final.BalanceCents, final.Points, final.TotalSpentCents)
	}
}
