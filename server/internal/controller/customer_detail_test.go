package controller_test

// TestCustomerDetailEndpoints 是 todo 14 的验收测试（计划：
// `go test ./internal/controller -run TestCustomerDetailEndpoints -v -count=1`）：
//   - GET /customers/:id/orders、/balance-transactions、/points-transactions 三接口各自分页、时间倒序；
//   - 余额流水的 balance_before/balance_after 连续可见（06-BUSINESS-RULES.md:11 可追溯）；
//   - 数据为空返回空数组 []（不是 null）；
//   - customer 不存在 → 404 + 40001（三接口一致）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// orderView 是客户详情「消费记录」DTO 的测试镜像。
type orderView struct {
	ID                  int64     `json:"id"`
	OrderNo             string    `json:"order_no"`
	CustomerID          int64     `json:"customer_id"`
	EmployeeID          *int64    `json:"employee_id"`
	OriginalAmountCents int64     `json:"original_amount_cents"`
	DiscountAmountCents int64     `json:"discount_amount_cents"`
	PaidAmountCents     int64     `json:"paid_amount_cents"`
	PaymentMethod       string    `json:"payment_method"`
	Status              string    `json:"status"`
	Remark              string    `json:"remark"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// balanceTxView 是「余额流水」DTO 的测试镜像。
type balanceTxView struct {
	ID                 int64     `json:"id"`
	CustomerID         int64     `json:"customer_id"`
	Type               string    `json:"type"`
	AmountCents        int64     `json:"amount_cents"`
	BalanceBeforeCents int64     `json:"balance_before_cents"`
	BalanceAfterCents  int64     `json:"balance_after_cents"`
	ReferenceType      string    `json:"reference_type"`
	ReferenceID        *int64    `json:"reference_id"`
	OperatorID         *int64    `json:"operator_id"`
	Remark             string    `json:"remark"`
	CreatedAt          time.Time `json:"created_at"`
}

// pointsTxView 是「积分流水」DTO 的测试镜像。
type pointsTxView struct {
	ID            int64     `json:"id"`
	CustomerID    int64     `json:"customer_id"`
	Type          string    `json:"type"`
	Points        int64     `json:"points"`
	BalanceBefore int64     `json:"balance_before"`
	BalanceAfter  int64     `json:"balance_after"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   *int64    `json:"reference_id"`
	OperatorID    *int64    `json:"operator_id"`
	Remark        string    `json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
}

// getPage 发起 GET 并断言 200，返回分页结构。
func (e *customerEnv) getPage(t *testing.T, token, path string) pageData {
	t.Helper()
	w := e.authed(http.MethodGet, path, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (body=%s)", path, w.Code, w.Body.String())
	}
	return decodePage(t, decodeEnvelope(t, w))
}

// decodeItems 把分页 items 解码为指定切片类型。
func decodeItems[T any](t *testing.T, page pageData) []T {
	t.Helper()
	var items []T
	if err := json.Unmarshal(page.Items, &items); err != nil {
		t.Fatalf("解析 items 失败: %v (items=%s)", err, page.Items)
	}
	return items
}

func TestCustomerDetailEndpoints(t *testing.T) {
	env := newCustomerEnv(t)

	// Given: 一个有完整流水的客户，以及一个无任何流水的客户。
	customer := env.createCustomer(t, env.staffToken, `{"name":"流水客户","phone":"13800001000"}`)
	emptyCustomer := env.createCustomer(t, env.staffToken, `{"name":"空数据客户","phone":"13800002000"}`)

	base := time.Now().UTC().Truncate(time.Second)
	orders := []model.Order{
		{OrderNo: "O-DETAIL-1", RequestID: "req-detail-1", CustomerID: customer.ID,
			OriginalAmountCents: 10000, DiscountAmountCents: 0, PaidAmountCents: 10000,
			PaymentMethod: model.PaymentMethodCash, Status: model.OrderStatusCompleted,
			CreatedAt: base.Add(-3 * time.Hour)},
		{OrderNo: "O-DETAIL-2", RequestID: "req-detail-2", CustomerID: customer.ID,
			OriginalAmountCents: 5000, DiscountAmountCents: 500, PaidAmountCents: 4500,
			PaymentMethod: model.PaymentMethodWechat, Status: model.OrderStatusCompleted,
			CreatedAt: base.Add(-2 * time.Hour)},
		{OrderNo: "O-DETAIL-3", RequestID: "req-detail-3", CustomerID: customer.ID,
			OriginalAmountCents: 2000, DiscountAmountCents: 0, PaidAmountCents: 2000,
			PaymentMethod: model.PaymentMethodBalance, Status: model.OrderStatusPending,
			CreatedAt: base.Add(-1 * time.Hour)},
	}
	for i := range orders {
		if err := env.db.Create(&orders[i]).Error; err != nil {
			t.Fatalf("插入订单 %s: %v", orders[i].OrderNo, err)
		}
	}
	balances := []model.BalanceTransaction{
		{CustomerID: customer.ID, Type: model.BalanceTxRecharge, AmountCents: 10000,
			BalanceBeforeCents: 0, BalanceAfterCents: 10000, ReferenceType: model.ReferenceTypeRecharge,
			CreatedAt: base.Add(-3 * time.Hour)},
		{CustomerID: customer.ID, Type: model.BalanceTxConsume, AmountCents: -3000,
			BalanceBeforeCents: 10000, BalanceAfterCents: 7000, ReferenceType: model.ReferenceTypeOrder,
			CreatedAt: base.Add(-2 * time.Hour)},
		{CustomerID: customer.ID, Type: model.BalanceTxAdjustment, AmountCents: -7000,
			BalanceBeforeCents: 7000, BalanceAfterCents: 0,
			CreatedAt: base.Add(-1 * time.Hour)},
	}
	for i := range balances {
		if err := env.db.Create(&balances[i]).Error; err != nil {
			t.Fatalf("插入余额流水 #%d: %v", i, err)
		}
	}
	points := []model.PointsTransaction{
		{CustomerID: customer.ID, Type: model.PointsTxEarn, Points: 100,
			BalanceBefore: 0, BalanceAfter: 100, ReferenceType: model.ReferenceTypeOrder,
			CreatedAt: base.Add(-2 * time.Hour)},
		{CustomerID: customer.ID, Type: model.PointsTxRefund, Points: -100,
			BalanceBefore: 100, BalanceAfter: 0, ReferenceType: model.ReferenceTypeOrder,
			CreatedAt: base.Add(-1 * time.Hour)},
	}
	for i := range points {
		if err := env.db.Create(&points[i]).Error; err != nil {
			t.Fatalf("插入积分流水 #%d: %v", i, err)
		}
	}

	// --- When: staff 拉取消费记录（page=1&page_size=2） ---
	ordersPage := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/orders?page=1&page_size=2", customer.ID))

	// --- Then: total=3、本页 2 条、时间倒序（最新 O-DETAIL-3 在前）、金额为整数分 ---
	if ordersPage.Total != 3 || ordersPage.Page != 1 || ordersPage.PageSize != 2 {
		t.Fatalf("订单分页 = %+v, want total=3 page=1 page_size=2", ordersPage)
	}
	orderItems := decodeItems[orderView](t, ordersPage)
	if len(orderItems) != 2 {
		t.Fatalf("订单 items 条数 = %d, want 2", len(orderItems))
	}
	if orderItems[0].OrderNo != "O-DETAIL-3" || orderItems[1].OrderNo != "O-DETAIL-2" {
		t.Errorf("订单倒序 = [%s, %s], want [O-DETAIL-3, O-DETAIL-2]", orderItems[0].OrderNo, orderItems[1].OrderNo)
	}
	if orderItems[1].OriginalAmountCents != 5000 || orderItems[1].DiscountAmountCents != 500 || orderItems[1].PaidAmountCents != 4500 {
		t.Errorf("订单金额 = %d/%d/%d, want 5000/500/4500（整数分）",
			orderItems[1].OriginalAmountCents, orderItems[1].DiscountAmountCents, orderItems[1].PaidAmountCents)
	}
	if orderItems[0].CustomerID != customer.ID {
		t.Errorf("订单 customer_id = %d, want %d", orderItems[0].CustomerID, customer.ID)
	}

	// --- When: 第 2 页 ---
	ordersPage2 := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/orders?page=2&page_size=2", customer.ID))

	// --- Then: 仍有 total=3、本页 1 条（最早那条） ---
	if ordersPage2.Total != 3 {
		t.Errorf("订单第 2 页 total = %d, want 3", ordersPage2.Total)
	}
	if tail := decodeItems[orderView](t, ordersPage2); len(tail) != 1 || tail[0].OrderNo != "O-DETAIL-1" {
		t.Errorf("订单第 2 页 items = %+v, want [O-DETAIL-1]", tail)
	}

	// --- When: 拉取余额流水 ---
	balancePage := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/balance-transactions?page_size=100", customer.ID))

	// --- Then: total=3、时间倒序、before/after 连续可追溯 ---
	if balancePage.Total != 3 {
		t.Fatalf("余额流水分页 total = %d, want 3", balancePage.Total)
	}
	balanceItems := decodeItems[balanceTxView](t, balancePage)
	if len(balanceItems) != 3 {
		t.Fatalf("余额流水 items 条数 = %d, want 3", len(balanceItems))
	}
	if balanceItems[0].Type != model.BalanceTxAdjustment || balanceItems[2].Type != model.BalanceTxRecharge {
		t.Errorf("余额流水倒序 = [%s, %s, %s], want [adjustment, consume, recharge]",
			balanceItems[0].Type, balanceItems[1].Type, balanceItems[2].Type)
	}
	// 倒序列表反向遍历即时间正序：before[i+1] 必须等于 after[i]。
	chain := []balanceTxView{balanceItems[2], balanceItems[1], balanceItems[0]}
	if chain[0].BalanceBeforeCents != 0 || chain[2].BalanceAfterCents != 0 {
		t.Errorf("余额链首尾 = %d/%d, want 0/0", chain[0].BalanceBeforeCents, chain[2].BalanceAfterCents)
	}
	for i := 0; i+1 < len(chain); i++ {
		if chain[i].BalanceAfterCents != chain[i+1].BalanceBeforeCents {
			t.Errorf("余额流水不连续: after[%d]=%d != before[%d]=%d",
				i, chain[i].BalanceAfterCents, i+1, chain[i+1].BalanceBeforeCents)
		}
	}
	if chain[1].AmountCents != -3000 {
		t.Errorf("消费流水 amount_cents = %d, want -3000（带符号整数分）", chain[1].AmountCents)
	}

	// --- When: 拉取积分流水 ---
	pointsPage := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/points-transactions?page_size=100", customer.ID))

	// --- Then: total=2、倒序、积分连续 ---
	if pointsPage.Total != 2 {
		t.Fatalf("积分流水分页 total = %d, want 2", pointsPage.Total)
	}
	pointsItems := decodeItems[pointsTxView](t, pointsPage)
	if len(pointsItems) != 2 {
		t.Fatalf("积分流水 items 条数 = %d, want 2", len(pointsItems))
	}
	if pointsItems[0].Type != model.PointsTxRefund || pointsItems[0].Points != -100 {
		t.Errorf("积分流水首条 = %+v, want refund/-100（倒序）", pointsItems[0])
	}
	if pointsItems[0].BalanceBefore != pointsItems[1].BalanceAfter {
		t.Errorf("积分流水不连续: before[0]=%d != after[1]=%d", pointsItems[0].BalanceBefore, pointsItems[1].BalanceAfter)
	}

	// --- When/Then: 无数据客户三接口均返回空数组（不是 null） ---
	for _, suffix := range []string{"orders", "balance-transactions", "points-transactions"} {
		page := env.getPage(t, env.staffToken,
			fmt.Sprintf("/api/v1/customers/%d/%s", emptyCustomer.ID, suffix))
		if page.Total != 0 {
			t.Errorf("%s 空客户 total = %d, want 0", suffix, page.Total)
		}
		if string(page.Items) != "[]" {
			t.Errorf("%s 空客户 items = %s, want []", suffix, page.Items)
		}
	}

	// --- When/Then: 不存在客户 → 404 + 40001（三接口一致） ---
	for _, suffix := range []string{"orders", "balance-transactions", "points-transactions"} {
		path := fmt.Sprintf("/api/v1/customers/999999/%s", suffix)
		w := env.authed(http.MethodGet, path, "", env.adminToken)
		if w.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404 (body=%s)", path, w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeCustomerNotFound {
			t.Errorf("GET %s code = %d, want %d", path, envl.Code, service.CodeCustomerNotFound)
		}
	}

	// --- When/Then: 非法 id → 400；未登录 → 401 ---
	if w := env.authed(http.MethodGet, "/api/v1/customers/abc/orders", "", env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法 id 订单列表 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w := env.do(http.MethodGet, fmt.Sprintf("/api/v1/customers/%d/orders", customer.ID), "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录订单列表 status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
