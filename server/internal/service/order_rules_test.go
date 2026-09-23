package service_test

// TestPointsCalc 与 TestDiscountConsistency 是 todo 24 的验收测试（计划：
// `go test ./internal/service -run 'TestPointsCalc|TestDiscountConsistency'`）：
//   - 积分 = floor(paid_amount_cents × points_per_yuan ÷ 100)，整数先乘后除（06-BUSINESS-RULES.md:67-76）：
//     1 分消费、恰好整除、非 1 比例取 floor 三类边界；断言 customers.points 与 points_transactions 行；
//     比例变更只影响新订单，旧流水不被重写（03-DATABASE.md:218-240）；
//   - 改价两级记录一致：订单级 discount_amount_cents == Σ 明细级 discount_amount_cents，
//     且 paid == original − discount（06 §3.1、04-API.md:127-155），断言从数据库重读的行。

import (
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// setPointsRatio 直接改 settings.points_per_yuan（设置接口属 todo 41-43，本测试直接写库造前置）。
func setPointsRatio(t *testing.T, env *orderTestEnv, ratio string) {
	t.Helper()
	if err := env.db.Model(&model.Setting{}).Where("key = ?", model.SettingPointsPerYuan).
		Update("value", ratio).Error; err != nil {
		t.Fatalf("更新 points_per_yuan=%s: %v", ratio, err)
	}
}

// requireSinglePointsTx 断言恰好 1 条积分流水并逐字段校验 points/before/after。
func requireSinglePointsTx(t *testing.T, env *orderTestEnv, points, before, after int64) {
	t.Helper()
	if n := env.countRows(t, &model.PointsTransaction{}); n != 1 {
		t.Fatalf("points_transactions 行数 = %d, want 1", n)
	}
	var pointsTx model.PointsTransaction
	if err := env.db.First(&pointsTx).Error; err != nil {
		t.Fatalf("读取积分流水失败: %v", err)
	}
	if pointsTx.Type != model.PointsTxEarn || pointsTx.Points != points ||
		pointsTx.BalanceBefore != before || pointsTx.BalanceAfter != after {
		t.Errorf("积分流水 = %+v, want earn/%d/%d→%d", pointsTx, points, before, after)
	}
}

// createCashOrder 以现金支付一笔单价为 unitPriceCents 的订单（积分与余额解耦，专注积分断言）。
func createCashOrder(t *testing.T, env *orderTestEnv, requestID string, customerID, serviceID, unitPriceCents int64) *service.OrderCreateResult {
	t.Helper()
	result, err := env.orders.Create(adminCtx(), orderInput(requestID, customerID, model.PaymentMethodCash,
		service.OrderItemInput{ServiceID: serviceID, Quantity: 1, UnitPriceCents: unitPriceCents}))
	if err != nil {
		t.Fatalf("Create(%s): %v", requestID, err)
	}
	return result
}

func TestPointsCalc(t *testing.T) {
	t.Run("one cent floors to zero under default ratio", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "一分客户", 0)
		cent := env.seedService(t, "一分服务", 1, model.StatusEnabled)

		// --- When: 默认比例 1，现金消费 1 分 ---
		result := createCashOrder(t, env, "req-points-cent", customer.ID, cent.ID, 1)

		// --- Then: floor(1×1/100) = 0，订单实付 1 分 ---
		if result.Order.PaidAmountCents != 1 {
			t.Errorf("实付 = %d, want 1", result.Order.PaidAmountCents)
		}
		if got := env.customerAfter(t, customer.ID).Points; got != 0 {
			t.Errorf("1 分消费后积分 = %d, want 0（floor(1/100)）", got)
		}
		requireSinglePointsTx(t, env, 0, 0, 0)
	})

	t.Run("one cent earns one point when ratio is 100", func(t *testing.T) {
		env := newOrderTestEnv(t)
		setPointsRatio(t, env, "100")
		customer := env.seedCustomer(t, "百分客户", 0)
		cent := env.seedService(t, "一分服务", 1, model.StatusEnabled)

		// --- When: 比例 100，现金消费 1 分 ---
		createCashOrder(t, env, "req-points-cent-100", customer.ID, cent.ID, 1)

		// --- Then: floor(1×100/100) = 1 ---
		if got := env.customerAfter(t, customer.ID).Points; got != 1 {
			t.Errorf("比例 100 消费 1 分后积分 = %d, want 1", got)
		}
		requireSinglePointsTx(t, env, 1, 0, 1)
	})

	t.Run("exact multiple yields exact points", func(t *testing.T) {
		env := newOrderTestEnv(t)
		setPointsRatio(t, env, "25")
		customer := env.seedCustomer(t, "整除客户", 0)
		care := env.seedService(t, "护理", 400, model.StatusEnabled)

		// --- When: 比例 25，消费 400 分 → 4 元 × 25 = 100 积分 ---
		createCashOrder(t, env, "req-points-exact", customer.ID, care.ID, 400)

		// --- Then: 无余数可丢 ---
		if got := env.customerAfter(t, customer.ID).Points; got != 100 {
			t.Errorf("比例 25 消费 400 分后积分 = %d, want 100（4×25）", got)
		}
		requireSinglePointsTx(t, env, 100, 0, 100)
	})

	t.Run("non-1 ratio floors the remainder", func(t *testing.T) {
		env := newOrderTestEnv(t)
		setPointsRatio(t, env, "7")
		customer := env.seedCustomer(t, "取整客户", 0)
		care := env.seedService(t, "护理", 999, model.StatusEnabled)

		// --- When: 比例 7，消费 999 分 → floor(999×7/100) = floor(69.93) ---
		createCashOrder(t, env, "req-points-floor", customer.ID, care.ID, 999)

		// --- Then: 丢弃余数而非四舍五入 ---
		if got := env.customerAfter(t, customer.ID).Points; got != 69 {
			t.Errorf("比例 7 消费 999 分后积分 = %d, want 69（floor(6993/100)）", got)
		}
		requireSinglePointsTx(t, env, 69, 0, 69)
	})

	t.Run("ratio change only affects new orders", func(t *testing.T) {
		env := newOrderTestEnv(t)
		setPointsRatio(t, env, "3")
		customer := env.seedCustomer(t, "比例变更客户", 0)
		care := env.seedService(t, "护理", 1000, model.StatusEnabled)

		// --- Given: 比例 3 时消费 1000 分 → 30 积分 ---
		createCashOrder(t, env, "req-points-before-change", customer.ID, care.ID, 1000)

		// --- When: 管理员把比例改为 5 后再消费 1000 分 ---
		setPointsRatio(t, env, "5")
		createCashOrder(t, env, "req-points-after-change", customer.ID, care.ID, 1000)

		// --- Then: 旧流水仍是 30，新订单按 5 记 50，累计 80 ---
		if got := env.customerAfter(t, customer.ID).Points; got != 80 {
			t.Errorf("比例 3→5 两次消费后积分 = %d, want 80（30+50）", got)
		}
		var txs []model.PointsTransaction
		if err := env.db.Order("id ASC").Find(&txs).Error; err != nil {
			t.Fatalf("读取积分流水失败: %v", err)
		}
		if len(txs) != 2 {
			t.Fatalf("points_transactions 行数 = %d, want 2", len(txs))
		}
		if txs[0].Points != 30 || txs[0].BalanceBefore != 0 || txs[0].BalanceAfter != 30 {
			t.Errorf("比例变更前流水 = %+v, want 30/0→30（历史不得重写）", txs[0])
		}
		if txs[1].Points != 50 || txs[1].BalanceBefore != 30 || txs[1].BalanceAfter != 80 {
			t.Errorf("比例变更后流水 = %+v, want 50/30→80", txs[1])
		}
	})
}

func TestDiscountConsistency(t *testing.T) {
	env := newOrderTestEnv(t)
	customer := env.seedCustomer(t, "折扣一致性客户", 0)
	haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled) // 无改价 ×2
	perm := env.seedService(t, "烫发", 2000, model.StatusEnabled)    // 改价 2000→1500 ×3

	// --- When: admin 改价一行为 1500×3，另一行按标准价 5000×2 ---
	in := orderInput("req-discount-consistency", customer.ID, model.PaymentMethodCash,
		service.OrderItemInput{ServiceID: haircut.ID, Quantity: 2, UnitPriceCents: 5000},
		service.OrderItemInput{ServiceID: perm.ID, Quantity: 3, UnitPriceCents: 1500})
	in.DiscountReason = "套餐改价"
	result, err := env.orders.Create(adminCtx(), in)
	if err != nil {
		t.Fatalf("Create(discount): %v", err)
	}

	// --- Then: 从数据库重读订单与明细（不信返回值） ---
	var dbOrder model.Order
	if err := env.db.First(&dbOrder, result.Order.ID).Error; err != nil {
		t.Fatalf("重读订单失败: %v", err)
	}
	var dbItems []model.OrderItem
	if err := env.db.Where("order_id = ?", result.Order.ID).Order("id ASC").Find(&dbItems).Error; err != nil {
		t.Fatalf("重读明细失败: %v", err)
	}
	if len(dbItems) != 2 {
		t.Fatalf("明细条数 = %d, want 2", len(dbItems))
	}

	// --- Then: 两级折扣一致：订单级 = Σ 明细级 ---
	var sumDiscount, sumAmount int64
	for _, item := range dbItems {
		sumDiscount += item.DiscountAmountCents
		sumAmount += item.AmountCents
	}
	if sumDiscount != dbOrder.DiscountAmountCents {
		t.Errorf("Σ 明细折扣 = %d, 订单级折扣 = %d, want 相等", sumDiscount, dbOrder.DiscountAmountCents)
	}
	if sumAmount != dbOrder.PaidAmountCents {
		t.Errorf("Σ 明细金额 = %d, 订单实付 = %d, want 相等", sumAmount, dbOrder.PaidAmountCents)
	}

	// --- Then: paid = original − discount，且具体金额符合预期 ---
	if dbOrder.PaidAmountCents != dbOrder.OriginalAmountCents-dbOrder.DiscountAmountCents {
		t.Errorf("实付 %d ≠ 原价 %d − 优惠 %d", dbOrder.PaidAmountCents, dbOrder.OriginalAmountCents, dbOrder.DiscountAmountCents)
	}
	if dbOrder.OriginalAmountCents != 16000 || dbOrder.DiscountAmountCents != 1500 || dbOrder.PaidAmountCents != 14500 {
		t.Errorf("订单金额 原价/优惠/实付 = %d/%d/%d, want 16000/1500/14500",
			dbOrder.OriginalAmountCents, dbOrder.DiscountAmountCents, dbOrder.PaidAmountCents)
	}

	// --- Then: 明细级明细：无改价行折扣 0，改价行折扣 (2000−1500)×3 ---
	if dbItems[0].DiscountAmountCents != 0 || dbItems[0].AmountCents != 10000 {
		t.Errorf("明细[0] = %+v, want discount=0/amount=10000", dbItems[0])
	}
	if dbItems[1].DiscountAmountCents != 1500 || dbItems[1].AmountCents != 4500 {
		t.Errorf("明细[1] = %+v, want discount=1500/amount=4500", dbItems[1])
	}
}
