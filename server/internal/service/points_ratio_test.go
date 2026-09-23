package service_test

// TestPointsRatioChange 是 todo 43 的验收测试
// （计划：`go test ./internal/service -run TestPointsRatioChange -v -count=3`）：
//   - 比例=1 时 10000 分（100 元）现金订单 earn=100；
//   - 改为 2 后新订单 earn=200（新比例只影响之后的消费）；
//   - 改比例前订单的 points_transactions 行保持原值（快照不追溯，06 §6:71-76）；
//   - 退款按原 earn 反向扣减（即使当前比例已变），且退款不按新比例重算；
//   - 比例改回 1 后历史流水仍无任何追溯变化。
//
// 比例变更走生产更新路径（SettingsService.Update，等价 PUT /settings/:key），
// 消费/退款走生产 OrderService（与 router.New 相同的装配）。

import (
	"net/http"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// ratioTestEnv 在 orderTestEnv 之上追加 settings 服务（比例变更入口）。
type ratioTestEnv struct {
	*orderTestEnv
	settings *service.SettingsService
}

// newRatioTestEnv 装配 todo 43 测试环境（复用 orderTestEnv 的临时库与依赖）。
func newRatioTestEnv(t *testing.T) *ratioTestEnv {
	t.Helper()
	base := newOrderTestEnv(t)
	return &ratioTestEnv{
		orderTestEnv: base,
		settings:     service.NewSettingsService(repository.NewSettingsRepository(base.db)),
	}
}

// updateRatio 通过生产 SettingsService 修改积分比例并断言落库值。
func (e *ratioTestEnv) updateRatio(t *testing.T, ratio string) {
	t.Helper()
	result, err := e.settings.Update(adminCtx(), model.SettingPointsPerYuan, ratio)
	if err != nil {
		t.Fatalf("Update(points_per_yuan=%q): %v", ratio, err)
	}
	if result.Setting.Value != ratio {
		t.Fatalf("比例落库 = %q, want %q", result.Setting.Value, ratio)
	}
}

// earnRowOfOrder 读取订单唯一一条 earn 积分流水（返回行快照）。
func (e *ratioTestEnv) earnRowOfOrder(t *testing.T, orderID int64) model.PointsTransaction {
	t.Helper()
	var row model.PointsTransaction
	if err := e.db.Where("reference_type = ? AND reference_id = ? AND type = ?",
		model.ReferenceTypeOrder, orderID, model.PointsTxEarn).First(&row).Error; err != nil {
		t.Fatalf("读取订单 %d 的 earn 流水失败: %v", orderID, err)
	}
	return row
}

// refundRowOfOrder 读取订单唯一一条 refund 积分流水（不存在返回 (零值,false)）。
func (e *ratioTestEnv) refundRowOfOrder(t *testing.T, orderID int64) (model.PointsTransaction, bool) {
	t.Helper()
	var row model.PointsTransaction
	err := e.db.Where("reference_type = ? AND reference_id = ? AND type = ?",
		model.ReferenceTypeOrder, orderID, model.PointsTxRefund).First(&row).Error
	if err != nil {
		return model.PointsTransaction{}, false
	}
	return row, true
}

// cashOrder 以 admin 上下文创建一张现金直接完成订单（10000 分单行）。
func (e *ratioTestEnv) cashOrder(t *testing.T, requestID string, customerID, serviceID int64) *model.Order {
	t.Helper()
	result, err := e.orders.Create(adminCtx(), orderInput(requestID, customerID, model.PaymentMethodCash,
		service.OrderItemInput{ServiceID: serviceID, Quantity: 1, UnitPriceCents: 10000}))
	if err != nil {
		t.Fatalf("Create(%s): %v", requestID, err)
	}
	return result.Order
}

func TestPointsRatioChange(t *testing.T) {
	env := newRatioTestEnv(t)
	customer := env.seedCustomer(t, "比例客户", 0)
	item := env.seedService(t, "剪发", 10000, model.StatusEnabled)

	// --- Given/When: 比例=1，现金消费 10000 分（100 元） ---
	oldOrder := env.cashOrder(t, "req-ratio-old", customer.ID, item.ID)

	// --- Then: 旧单 earn=100；客户积分 100 ---
	oldEarn := env.earnRowOfOrder(t, oldOrder.ID)
	if oldEarn.Points != 100 || oldEarn.BalanceAfter != 100 {
		t.Fatalf("比例=1 旧单 earn 流水 = points %d/after %d, want 100/100", oldEarn.Points, oldEarn.BalanceAfter)
	}
	if got := env.customerAfter(t, customer.ID).Points; got != 100 {
		t.Fatalf("比例=1 旧单后客户积分 = %d, want 100", got)
	}

	// --- When: 把比例改为 2（生产更新路径） ---
	env.updateRatio(t, "2")

	// --- Then: 旧单流水行未被触碰（stale_state：比例变更不追溯） ---
	oldEarnAfterChange := env.earnRowOfOrder(t, oldOrder.ID)
	if oldEarnAfterChange.ID != oldEarn.ID || oldEarnAfterChange.Points != 100 || oldEarnAfterChange.BalanceAfter != 100 {
		t.Errorf("改比例后旧单 earn 行 = id %d/points %d/after %d, want id %d/100/100（不得追溯）",
			oldEarnAfterChange.ID, oldEarnAfterChange.Points, oldEarnAfterChange.BalanceAfter, oldEarn.ID)
	}

	// --- When: 新比例下再消费 10000 分（新订单） ---
	newOrder := env.cashOrder(t, "req-ratio-new", customer.ID, item.ID)

	// --- Then: 新单 earn=200；客户积分 300；旧单仍 100（快照各自独立） ---
	newEarn := env.earnRowOfOrder(t, newOrder.ID)
	if newEarn.Points != 200 || newEarn.BalanceAfter != 300 {
		t.Errorf("比例=2 新单 earn 流水 = points %d/after %d, want 200/300", newEarn.Points, newEarn.BalanceAfter)
	}
	if got := env.customerAfter(t, customer.ID).Points; got != 300 {
		t.Errorf("新单后客户积分 = %d, want 300（100 + 200）", got)
	}
	if got := env.earnRowOfOrder(t, oldOrder.ID).Points; got != 100 {
		t.Errorf("新单后旧单 earn = %d, want 100（不追溯）", got)
	}

	// --- When: 退款旧单（应按原 earn=100 反向，而非按当前比例 2 重算） ---
	if _, err := env.orders.Refund(adminCtx(), oldOrder.ID); err != nil {
		t.Fatalf("Refund(旧单): %v", err)
	}

	// --- Then: refund 流水 = -100、before/after 连续；客户积分 200；旧单 earn 行仍 100 ---
	refundRow, ok := env.refundRowOfOrder(t, oldOrder.ID)
	if !ok {
		t.Fatal("退款后未找到旧单 refund 积分流水")
	}
	if refundRow.Points != -100 || refundRow.BalanceBefore != 300 || refundRow.BalanceAfter != 200 {
		t.Errorf("退款流水 = points %d/before %d/after %d, want -100/300/200", refundRow.Points, refundRow.BalanceBefore, refundRow.BalanceAfter)
	}
	if got := env.customerAfter(t, customer.ID).Points; got != 200 {
		t.Errorf("退款后客户积分 = %d, want 200（300 - 原 earn 100）", got)
	}
	if got := env.earnRowOfOrder(t, oldOrder.ID); got.Points != 100 {
		t.Errorf("退款后旧单 earn 行 = %d, want 100（原快照不变）", got.Points)
	}
	var refundedOrder model.Order
	if err := env.db.First(&refundedOrder, oldOrder.ID).Error; err != nil {
		t.Fatalf("读取退款订单失败: %v", err)
	}
	if refundedOrder.Status != model.OrderStatusRefunded {
		t.Errorf("退款后订单状态 = %s, want refunded", refundedOrder.Status)
	}

	// --- When: 比例改回 1，再消费 10000 分 ---
	env.updateRatio(t, "1")
	lastOrder := env.cashOrder(t, "req-ratio-back", customer.ID, item.ID)

	// --- Then: 末单 earn=100（当前比例生效）；历史三行快照完全无追溯变化 ---
	if got := env.earnRowOfOrder(t, lastOrder.ID).Points; got != 100 {
		t.Errorf("比例改回 1 后新单 earn = %d, want 100", got)
	}
	if got := env.earnRowOfOrder(t, oldOrder.ID).Points; got != 100 {
		t.Errorf("比例改回 1 后旧单 earn = %d, want 100（无追溯）", got)
	}
	if got := env.earnRowOfOrder(t, newOrder.ID).Points; got != 200 {
		t.Errorf("比例改回 1 后第二单 earn = %d, want 200（无追溯）", got)
	}
	var doubleRefund model.PointsTransaction
	if err := env.db.First(&doubleRefund, refundRow.ID).Error; err != nil {
		t.Fatalf("读取退款流水失败: %v", err)
	}
	if doubleRefund.Points != -100 {
		t.Errorf("比例改回 1 后退款流水 = %d, want -100（无追溯）", doubleRefund.Points)
	}

	// --- Then: 积分守恒：Σ流水 = 客户积分（100 + 200 − 100 + 100 = 300） ---
	var rows []model.PointsTransaction
	if err := env.db.Where("customer_id = ?", customer.ID).Find(&rows).Error; err != nil {
		t.Fatalf("读取客户积分流水失败: %v", err)
	}
	var sum int64
	for _, row := range rows {
		sum += row.Points
	}
	if want := env.customerAfter(t, customer.ID).Points; sum != want {
		t.Errorf("Σ积分流水 = %d, 客户积分 = %d, want 相等", sum, want)
	}
	if len(rows) != 4 {
		t.Errorf("积分流水条数 = %d, want 4（2 earn + 1 refund + 1 earn）", len(rows))
	}

	// --- malformed_input：比例 0/负/非数字仍被设置接口拒绝，且不影响既有流水 ---
	for _, bad := range []string{"0", "-1", "2.5"} {
		_, err := env.settings.Update(adminCtx(), model.SettingPointsPerYuan, bad)
		requireBizError(t, err, http.StatusBadRequest)
	}
	if got := env.earnRowOfOrder(t, oldOrder.ID).Points; got != 100 {
		t.Errorf("非法比例尝试后旧单 earn = %d, want 100", got)
	}
}
