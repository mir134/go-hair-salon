package service_test

// TestIdempotencyConcurrent 与 TestConcurrentDeduct 是 todo 25 的验收测试（计划：
// `go test ./internal/service -run 'TestIdempotencyConcurrent|TestConcurrentDeduct' -count=5`）：
//   - 同一 request_id 并发 10 个创建请求 → 仅 1 条订单/一套流水（唯一索引兜底），
//     全部调用者拿到同一订单（Created 仅一次为 true），余额只扣一次（04-API.md:277-299、06 §9）；
//   - 并发余额扣减（同一客户多笔余额支付）→ 总额不超扣、余额永不为负、
//     终值 = 初始余额 − Σ 成功实付；流水链 before/after 连续（原子条件 UPDATE，03-DATABASE.md:288-290）；
//   - WAL + busy_timeout 下并发写全部完成，无 "database is locked" 死锁（02-AGENTS.md:45-63）。
//
// 并发模型说明：连接池固定 SetMaxOpenConns(1)（repository/db.go:55），
// 写事务在 Go 侧被串行化，busy_timeout(5000) 是 SQLite 层的兜底等待；
// 本测试验证「并发调用者下」的业务不变量（幂等/不超扣/不丢单），
// 并直接断言 PRAGMA journal_mode=wal、busy_timeout=5000 确实生效。

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// runConcurrent 并发执行 callers 个任务（start 闸门尽量同时发起），返回每个任务的错误。
func runConcurrent(callers int, task func(idx int) error) []error {
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			errs[idx] = task(idx)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func TestIdempotencyConcurrent(t *testing.T) {
	env := newOrderTestEnv(t)
	customer := env.seedCustomer(t, "并发幂等客户", 100000)
	haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)

	// --- When: 同一 request_id 由 10 个 goroutine 并发创建（余额支付 5000） ---
	const callers = 10
	results := make([]*service.OrderCreateResult, callers)
	errs := runConcurrent(callers, func(idx int) error {
		result, err := env.orders.Create(adminCtx(), orderInput("req-concurrent-idem", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000}))
		results[idx] = result
		return err
	})

	// --- Then: 无调用者报错，全部拿到同一订单（原订单 200 语义），仅一次 Created=true ---
	createdCount := 0
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("并发调用 %d 返回错误: %v", i, errs[i])
		}
		if results[i] == nil || results[i].Order == nil {
			t.Fatalf("并发调用 %d 返回 nil 订单", i)
		}
		if results[i].Order.ID != results[0].Order.ID || results[i].Order.OrderNo != results[0].Order.OrderNo {
			t.Errorf("调用 %d 返回订单 id/no = %d/%s, want 与调用 0 相同 %d/%s",
				i, results[i].Order.ID, results[i].Order.OrderNo, results[0].Order.ID, results[0].Order.OrderNo)
		}
		if results[i].Created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("Created=true 次数 = %d, want 1（并发下仅一次真正入账）", createdCount)
	}

	// --- Then: 数据库仅一套账（唯一索引 ux_orders_request_id 兜底） ---
	for name, entity := range map[string]any{
		"orders":               &model.Order{},
		"order_items":          &model.OrderItem{},
		"balance_transactions": &model.BalanceTransaction{},
		"points_transactions":  &model.PointsTransaction{},
		"operation_logs":       &model.OperationLog{},
	} {
		if n := env.countRows(t, entity); n != 1 {
			t.Errorf("并发同一 request_id 后 %s 行数 = %d, want 1", name, n)
		}
	}

	// --- Then: 余额只扣一次：100000−5000=95000，积分 50，累计消费 5000 ---
	after := env.customerAfter(t, customer.ID)
	if after.BalanceCents != 95000 || after.Points != 50 || after.TotalSpentCents != 5000 {
		t.Errorf("并发后余额/积分/累计消费 = %d/%d/%d, want 95000/50/5000（只入账一次）",
			after.BalanceCents, after.Points, after.TotalSpentCents)
	}
}

func TestConcurrentDeduct(t *testing.T) {
	t.Run("concurrent balance payments never over-deduct", func(t *testing.T) {
		env := newOrderTestEnv(t)
		const (
			initialCents = 10000
			priceCents   = 1500
			callers      = 10
		)
		customer := env.seedCustomer(t, "并发扣减客户", initialCents)
		haircut := env.seedService(t, "剪发", priceCents, model.StatusEnabled)

		// --- When: 10 个 goroutine 各自用不同 request_id 发起余额支付 1500 ---
		results := make([]*service.OrderCreateResult, callers)
		errs := runConcurrent(callers, func(idx int) error {
			result, err := env.orders.Create(adminCtx(), orderInput(fmt.Sprintf("req-deduct-%d", idx), customer.ID, model.PaymentMethodBalance,
				service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: priceCents}))
			results[idx] = result
			return err
		})

		// --- Then: 恰好 6 笔成功（6×1500=9000 ≤ 10000），其余 4 笔 422 余额不足 ---
		succeeded := 0
		var paidSum int64
		for i := 0; i < callers; i++ {
			if errs[i] == nil {
				succeeded++
				paidSum += results[i].Order.PaidAmountCents
				continue
			}
			biz := requireBizError(t, errs[i], http.StatusUnprocessableEntity)
			if biz.Code != service.CodeValidationFailed || !strings.Contains(biz.Message, "余额不足") {
				t.Errorf("失败调用 %d 的 BizError = code %d msg %q, want 42200 含「余额不足」", i, biz.Code, biz.Message)
			}
		}
		if succeeded != 6 {
			t.Errorf("成功笔数 = %d, want 6（10000 ÷ 1500 = 6.67 向下取整）", succeeded)
		}
		if paidSum != 9000 {
			t.Errorf("Σ 成功实付 = %d, want 9000", paidSum)
		}

		// --- Then: 终值 = 初始 − Σ 成功实付，永不为负 ---
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != initialCents-paidSum {
			t.Errorf("并发扣减后余额 = %d, want %d（初始 %d − Σ 成功实付 %d）",
				after.BalanceCents, initialCents-paidSum, initialCents, paidSum)
		}
		if after.BalanceCents != 1000 {
			t.Errorf("并发扣减后余额 = %d, want 1000", after.BalanceCents)
		}
		if after.BalanceCents < 0 {
			t.Errorf("并发扣减后余额 = %d, want ≥ 0（永不超扣为负）", after.BalanceCents)
		}

		// --- Then: 行数与成功笔数一致（失败尝试零写入），积分/累计消费同步 ---
		for name, entity := range map[string]any{
			"orders":               &model.Order{},
			"order_items":          &model.OrderItem{},
			"balance_transactions": &model.BalanceTransaction{},
			"points_transactions":  &model.PointsTransaction{},
			"operation_logs":       &model.OperationLog{},
		} {
			if n := env.countRows(t, entity); n != int64(succeeded) {
				t.Errorf("并发扣减后 %s 行数 = %d, want %d（成功笔数）", name, n, succeeded)
			}
		}
		if after.Points != 90 || after.TotalSpentCents != 9000 {
			t.Errorf("并发扣减后积分/累计消费 = %d/%d, want 90/9000", after.Points, after.TotalSpentCents)
		}

		// --- Then: 余额流水链连续（before/after 首尾相接）且每笔 after ≥ 0 ---
		var txs []model.BalanceTransaction
		if err := env.db.Order("id ASC").Find(&txs).Error; err != nil {
			t.Fatalf("读取余额流水失败: %v", err)
		}
		expectedBefore := int64(initialCents)
		for i, tx := range txs {
			if tx.BalanceBeforeCents != expectedBefore || tx.BalanceAfterCents != tx.BalanceBeforeCents+tx.AmountCents {
				t.Errorf("流水[%d] = before %d/after %d/amount %d, want before %d 且 after=before+amount",
					i, tx.BalanceBeforeCents, tx.BalanceAfterCents, tx.AmountCents, expectedBefore)
			}
			if tx.BalanceAfterCents < 0 {
				t.Errorf("流水[%d] balance_after = %d, want ≥ 0", i, tx.BalanceAfterCents)
			}
			expectedBefore = tx.BalanceAfterCents
		}
		if expectedBefore != after.BalanceCents {
			t.Errorf("流水链终值 = %d, 客户余额 = %d, want 相等", expectedBefore, after.BalanceCents)
		}
	})

	t.Run("concurrent distinct cash orders all complete under WAL", func(t *testing.T) {
		env := newOrderTestEnv(t)
		const callers = 10
		customer := env.seedCustomer(t, "并发现金客户", 0)
		haircut := env.seedService(t, "剪发", 1000, model.StatusEnabled)

		// --- Given: WAL 与 busy_timeout 确实生效（DSN PRAGMA） ---
		var journalMode string
		if err := env.db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
			t.Fatalf("查询 journal_mode: %v", err)
		}
		if strings.ToLower(journalMode) != "wal" {
			t.Errorf("journal_mode = %q, want wal", journalMode)
		}
		var busyTimeout int64
		if err := env.db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
			t.Fatalf("查询 busy_timeout: %v", err)
		}
		if busyTimeout != 5000 {
			t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
		}

		// --- When: 10 个 goroutine 并发创建互不相同的现金订单 ---
		errs := runConcurrent(callers, func(idx int) error {
			_, err := env.orders.Create(adminCtx(), orderInput(fmt.Sprintf("req-cash-concurrent-%d", idx), customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 1000}))
			return err
		})

		// --- Then: 全部完成；任何 "database is locked"/死锁都会使本断言失败 ---
		var failures []string
		for i, err := range errs {
			if err != nil {
				failures = append(failures, fmt.Sprintf("调用 %d: %v", i, err))
			}
		}
		if len(failures) != 0 {
			t.Fatalf("并发现金下单失败 %d/%d：%s", len(failures), callers, strings.Join(failures, "；"))
		}

		// --- Then: 10 单全部落库，无丢单；现金不动余额 ---
		for name, entity := range map[string]any{
			"orders":              &model.Order{},
			"order_items":         &model.OrderItem{},
			"points_transactions": &model.PointsTransaction{},
			"operation_logs":      &model.OperationLog{},
		} {
			if n := env.countRows(t, entity); n != callers {
				t.Errorf("并发现金下单后 %s 行数 = %d, want %d（无丢单）", name, n, callers)
			}
		}
		if n := env.countRows(t, &model.BalanceTransaction{}); n != 0 {
			t.Errorf("现金下单产生余额流水 %d 条, want 0", n)
		}
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 0 || after.TotalSpentCents != 10000 || after.Points != 100 {
			t.Errorf("并发现金下单后余额/累计消费/积分 = %d/%d/%d, want 0/10000/100",
				after.BalanceCents, after.TotalSpentCents, after.Points)
		}
	})
}
