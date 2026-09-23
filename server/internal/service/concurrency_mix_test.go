package service_test

// TestConcurrencyMix 是 todo 60 的验收测试（计划：`go test ./internal/service -run TestConcurrencyMix -v -count=5`）：
// 并发充值 / 余额消费 / 现金消费 / 余额调整 / 退款混合负载，在 WAL + busy_timeout 下：
//
//   - 阶段 0（精确余额竞速）：余额恰为 2 笔消费额时，10 个并发余额消费**恰好 2 笔成功**、
//     其余 422 余额不足、余额归零、永不超扣（原子条件 UPDATE 的核心不变量，04-API.md:288-298）；
//   - 阶段 1/2（混合负载）：充值 / 余额消费 / 现金消费 / 余额调整 / 退款并发执行；
//   - 无 "database is locked" 死锁（含第二个连接池上的并发只读探针，真实检验 WAL 读写不互斥）；
//   - 终态余额 == 初始余额 + Σ 余额流水（链式 before/after 连续、每笔 after ≥ 0、链尾 == customers.balance_cents）；
//   - 积分 == Σ 积分流水 且 ≥ 0；行数与成功笔数一致（无丢单）。
//
// 并发模型：主连接池 SetMaxOpenConns(1)（写事务在 Go 侧串行，02-AGENTS.md:45-63），
// busy_timeout(5000) 是 SQLite 层的兜底等待；本测试另用 repository.OpenPool 打开
// 第二个连接池做并发只读探针，读写在不同连接上真实并发。
//
// race detector 说明：本机 go1.27.0 windows/386 + CGO_ENABLED=0，`go test -race` 不支持
// （literal 输出见 .omo/evidence/task-60-go-hair-salon-mvp.md），故以「并发不变量断言 +
// -count=5 重复 + -shuffle=on」替代；-race 需在 windows/amd64 或 linux 上执行。

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// isInsufficientBalance 判断 err 是否为「余额不足」422 业务错误（并发次序下合法结果）。
func isInsufficientBalance(err error) bool {
	var biz *service.BizError
	if !errors.As(err, &biz) {
		return false
	}
	return biz.Status == http.StatusUnprocessableEntity &&
		biz.Code == service.CodeValidationFailed &&
		strings.Contains(biz.Message, "余额不足")
}

// mixCounters 汇总混合负载的成功/拒绝笔数（goroutine 内加锁累加）。
type mixCounters struct {
	mu             sync.Mutex
	rechargeOK     int
	consumeOK      int
	consumeReject  int
	cashOK         int
	adjustOK       int
	refundOK       int
	balanceOrderID []int64
}

func (c *mixCounters) add(fn func(*mixCounters)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

func TestConcurrencyMix(t *testing.T) {
	env := newLedgerTestEnv(t)
	const (
		initialCents = 1000 // 精确余额竞速的起始余额（= 2 × 消费单价）
		priceCents   = 500
		racingCnt    = 10
		rechargeCnt  = 12
		consumeCnt   = 12
		cashCnt      = 6
		adjustCnt    = 4
		refundBatch  = 12
		extraBatch   = 8
	)
	customer := env.seedCustomer(t, "并发混合客户", initialCents)
	item := env.seedService(t, "混合剪发", priceCents, model.StatusEnabled)

	// --- Given: WAL 与 busy_timeout 确实生效（DSN PRAGMA，02-AGENTS.md:45-52） ---
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

	// --- Given: 第二个连接池上的并发只读探针（WAL 下读写不互斥） ---
	readerPool, err := repository.OpenPool(env.dbPath)
	if err != nil {
		t.Fatalf("repository.OpenPool: %v", err)
	}
	t.Cleanup(func() { _ = readerPool.Close() })
	readerDone := make(chan error, 1)
	stopReader := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopReader) }) }
	defer stop() // 安全网：任何提前失败都会终止探针
	go func() {
		for {
			select {
			case <-stopReader:
				readerDone <- nil
				return
			default:
			}
			var orders int64
			if err := readerPool.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orders); err != nil {
				readerDone <- fmt.Errorf("并发只读 orders 失败: %w", err)
				return
			}
			var balance int64
			if err := readerPool.QueryRow("SELECT balance_cents FROM customers WHERE id = ?", customer.ID).Scan(&balance); err != nil {
				readerDone <- fmt.Errorf("并发只读 customers 失败: %w", err)
				return
			}
			if balance < 0 {
				readerDone <- fmt.Errorf("并发只读观察到负余额 %d（超扣）", balance)
				return
			}
		}
	}()

	counters := &mixCounters{}
	var allErrs []error
	balanceConsume := func(requestID string) (*service.OrderCreateResult, error) {
		return env.orders.Create(adminCtx(), orderInput(requestID, customer.ID,
			model.PaymentMethodBalance, service.OrderItemInput{ServiceID: item.ID, Quantity: 1, UnitPriceCents: priceCents}))
	}
	recordConsume := func(result *service.OrderCreateResult) {
		counters.add(func(c *mixCounters) {
			c.consumeOK++
			c.balanceOrderID = append(c.balanceOrderID, result.Order.ID)
		})
	}
	handleConsumeErr := func(label string, idx int, err error) error {
		if !isInsufficientBalance(err) {
			return fmt.Errorf("%s %d 非预期错误: %w", label, idx, err)
		}
		counters.add(func(c *mixCounters) { c.consumeReject++ })
		return nil
	}

	// --- When 阶段 0：余额 1000，10 个并发余额消费各 500 → 恰好 2 笔成功（精确竞速） ---
	raceErrs := runConcurrent(racingCnt, func(idx int) error {
		result, err := balanceConsume(fmt.Sprintf("mix-race-%d", idx))
		if err != nil {
			return handleConsumeErr("精确余额并发消费", idx, err)
		}
		recordConsume(result)
		return nil
	})
	allErrs = append(allErrs, raceErrs...)

	// --- Then 阶段 0：恰好 2 笔成功、8 笔 422、余额归零、流水 2 条且每笔 after ≥ 0 ---
	if counters.consumeOK != 2 {
		t.Errorf("精确余额竞速成功笔数 = %d, want 2（1000 ÷ 500；>2 即超扣/双花）", counters.consumeOK)
	}
	if counters.consumeReject != racingCnt-2 {
		t.Errorf("精确余额竞速 422 笔数 = %d, want %d", counters.consumeReject, racingCnt-2)
	}
	if got := env.customerAfter(t, customer.ID).BalanceCents; got != 0 {
		t.Errorf("精确余额竞速后余额 = %d, want 0", got)
	}
	if got := env.countRows(t, &model.BalanceTransaction{}); got != 2 {
		t.Errorf("精确余额竞速后余额流水条数 = %d, want 2（失败尝试零写入）", got)
	}

	// --- Given: 阶段 1 前置充值（单线程，保证混合负载有充足资金） ---
	if _, err := env.recharges.CreateRecharge(adminCtx(),
		rechargeInput("mix-prefund", customer.ID, 20000, 0, model.PaymentMethodCash)); err != nil {
		t.Fatalf("前置充值失败: %v", err)
	}
	counters.add(func(c *mixCounters) { c.rechargeOK++ })

	// --- When 阶段 1：并发混合写入（充值 / 余额消费 / 现金消费 / 余额调整） ---
	errs1 := runConcurrent(rechargeCnt+consumeCnt+cashCnt+adjustCnt, func(idx int) error {
		switch {
		case idx < rechargeCnt:
			if _, err := env.recharges.CreateRecharge(adminCtx(),
				rechargeInput(fmt.Sprintf("mix-recharge-%d", idx), customer.ID, 1000, 100, model.PaymentMethodCash)); err != nil {
				return fmt.Errorf("充值 %d: %w", idx, err)
			}
			counters.add(func(c *mixCounters) { c.rechargeOK++ })
			return nil
		case idx < rechargeCnt+consumeCnt:
			result, err := balanceConsume(fmt.Sprintf("mix-consume-%d", idx))
			if err != nil {
				return handleConsumeErr("余额消费", idx, err)
			}
			recordConsume(result)
			return nil
		case idx < rechargeCnt+consumeCnt+cashCnt:
			if _, err := env.orders.Create(adminCtx(), orderInput(fmt.Sprintf("mix-cash-%d", idx), customer.ID,
				model.PaymentMethodCash, service.OrderItemInput{ServiceID: item.ID, Quantity: 1, UnitPriceCents: priceCents})); err != nil {
				return fmt.Errorf("现金消费 %d: %w", idx, err)
			}
			counters.add(func(c *mixCounters) { c.cashOK++ })
			return nil
		default:
			if _, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
				CustomerID: customer.ID, AmountCents: 300, Reason: "并发混合调整",
			}); err != nil {
				return fmt.Errorf("余额调整 %d: %w", idx, err)
			}
			counters.add(func(c *mixCounters) { c.adjustOK++ })
			return nil
		}
	})
	allErrs = append(allErrs, errs1...)

	// --- When 阶段 2：并发退款 + 继续充值/消费（退款与消费竞争同一余额） ---
	refundTargets := make([]int64, len(counters.balanceOrderID))
	copy(refundTargets, counters.balanceOrderID)
	if len(refundTargets) > refundBatch {
		refundTargets = refundTargets[:refundBatch]
	}
	errs2 := runConcurrent(len(refundTargets)+extraBatch+extraBatch, func(idx int) error {
		switch {
		case idx < len(refundTargets):
			if _, err := env.orders.Refund(adminCtx(), refundTargets[idx]); err != nil {
				return fmt.Errorf("退款订单 %d: %w", refundTargets[idx], err)
			}
			counters.add(func(c *mixCounters) { c.refundOK++ })
			return nil
		case idx < len(refundTargets)+extraBatch:
			if _, err := env.recharges.CreateRecharge(adminCtx(),
				rechargeInput(fmt.Sprintf("mix-recharge2-%d", idx), customer.ID, 700, 0, model.PaymentMethodWechat)); err != nil {
				return fmt.Errorf("二段充值 %d: %w", idx, err)
			}
			counters.add(func(c *mixCounters) { c.rechargeOK++ })
			return nil
		default:
			result, err := balanceConsume(fmt.Sprintf("mix-consume2-%d", idx))
			if err != nil {
				return handleConsumeErr("二段余额消费", idx, err)
			}
			recordConsume(result)
			return nil
		}
	})
	allErrs = append(allErrs, errs2...)

	stop()
	if err := <-readerDone; err != nil {
		t.Errorf("并发只读探针失败: %v", err)
	}

	// --- Then: 无任何失败、无 "database is locked"/死锁 ---
	lockPatterns := []string{"database is locked", "deadlock", "sqlite_busy", "busy_timeout", "锁"}
	for i, err := range allErrs {
		if err == nil {
			continue
		}
		t.Errorf("并发调用 %d 失败: %v", i, err)
		for _, pattern := range lockPatterns {
			if strings.Contains(strings.ToLower(err.Error()), pattern) {
				t.Errorf("并发调用 %d 命中锁等待/死锁模式 %q: %v", i, pattern, err)
			}
		}
	}

	// --- Then: 余额 == 初始 + Σ 流水（链式连续、每笔 after ≥ 0、链尾 == 客户余额） ---
	var txs []model.BalanceTransaction
	if err := env.db.Where("customer_id = ?", customer.ID).Order("id ASC").Find(&txs).Error; err != nil {
		t.Fatalf("读取余额流水失败: %v", err)
	}
	if len(txs) == 0 {
		t.Fatal("余额流水为空（混合负载未产生任何入账）")
	}
	expected := int64(initialCents)
	var ledgerSum int64
	for i, tx := range txs {
		if tx.BalanceBeforeCents != expected {
			t.Errorf("流水[%d] before = %d, want 上一笔 after %d（链断裂）", i, tx.BalanceBeforeCents, expected)
		}
		if tx.BalanceAfterCents != tx.BalanceBeforeCents+tx.AmountCents {
			t.Errorf("流水[%d] after = %d, want before %d + amount %d",
				i, tx.BalanceAfterCents, tx.BalanceBeforeCents, tx.AmountCents)
		}
		if tx.BalanceAfterCents < 0 {
			t.Errorf("流水[%d] after = %d, want ≥ 0（无双花/超扣）", i, tx.BalanceAfterCents)
		}
		expected = tx.BalanceAfterCents
		ledgerSum += tx.AmountCents
	}
	after := env.customerAfter(t, customer.ID)
	if after.BalanceCents != expected {
		t.Errorf("终态余额 = %d, want 流水链尾 %d", after.BalanceCents, expected)
	}
	if after.BalanceCents != int64(initialCents)+ledgerSum {
		t.Errorf("终态余额 = %d, want 初始 %d + Σ流水 %d", after.BalanceCents, initialCents, ledgerSum)
	}
	if after.BalanceCents < 0 {
		t.Errorf("终态余额 = %d, want ≥ 0", after.BalanceCents)
	}

	// --- Then: 积分 == Σ 积分流水 且 ≥ 0 ---
	var points []model.PointsTransaction
	if err := env.db.Where("customer_id = ?", customer.ID).Order("id ASC").Find(&points).Error; err != nil {
		t.Fatalf("读取积分流水失败: %v", err)
	}
	var pointsSum int64
	for _, row := range points {
		pointsSum += row.Points
	}
	if after.Points != pointsSum {
		t.Errorf("终态积分 = %d, want Σ积分流水 %d", after.Points, pointsSum)
	}
	if after.Points < 0 {
		t.Errorf("终态积分 = %d, want ≥ 0", after.Points)
	}

	// --- Then: 无丢单（行数 == 成功笔数；退款订单状态全部 refunded） ---
	if got := env.countRows(t, &model.RechargeRecord{}); got != int64(counters.rechargeOK) {
		t.Errorf("充值记录行数 = %d, want 成功笔数 %d", got, counters.rechargeOK)
	}
	wantOrders := counters.consumeOK + counters.cashOK
	if got := env.countRows(t, &model.Order{}); got != int64(wantOrders) {
		t.Errorf("订单行数 = %d, want 成功笔数 %d（余额 %d + 现金 %d）",
			got, wantOrders, counters.consumeOK, counters.cashOK)
	}
	var refunded int64
	if err := env.db.Model(&model.Order{}).Where("status = ?", model.OrderStatusRefunded).Count(&refunded).Error; err != nil {
		t.Fatalf("统计已退款订单失败: %v", err)
	}
	if refunded != int64(counters.refundOK) {
		t.Errorf("已退款订单数 = %d, want %d", refunded, counters.refundOK)
	}
	t.Logf("混合负载：充值 %d / 余额消费 %d（拒 %d）/ 现金 %d / 调整 %d / 退款 %d；终态余额 %d，流水 %d 条",
		counters.rechargeOK, counters.consumeOK, counters.consumeReject, counters.cashOK,
		counters.adjustOK, counters.refundOK, after.BalanceCents, len(txs))
}
