package service_test

// 本文件是 todo 34（充值/调整测试）的共享测试环境与断言辅助：
// 复用 orderTestEnv 的临时库与依赖，追加充值服务与余额调整服务（装配方式与 router.New 一致），
// 供 TestRechargeLedger 与 TestAdjustGuard 复用。
//
// 生产代码零改动；测试库为 t.TempDir() 独立 SQLite（WAL），不启动任何 HTTP 服务器。

import (
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// ledgerTestEnv 是充值/调整验收测试环境：复用 orderTestEnv 的临时库与依赖，
// 追加充值服务与余额调整服务。
type ledgerTestEnv struct {
	*orderTestEnv
	recharges *service.RechargeService
	adjust    *service.BalanceAdjustmentService
}

// newLedgerTestEnv 装配充值/调整测试环境（临时库、迁移、settings 播种、完整依赖注入）。
func newLedgerTestEnv(t *testing.T) *ledgerTestEnv {
	t.Helper()
	base := newOrderTestEnv(t)
	ledgerRepo := repository.NewLedgerRepository(base.db)
	logs := service.NewOperationLogService(repository.NewOperationLogRepository(base.db))
	return &ledgerTestEnv{
		orderTestEnv: base,
		recharges: service.NewRechargeService(service.RechargeServiceDeps{
			Tx:        repository.NewTransactor(base.db),
			Recharges: repository.NewRechargeRepository(base.db),
			Customers: base.customers,
			Ledger:    ledgerRepo,
			Logs:      logs,
		}),
		adjust: service.NewBalanceAdjustmentService(service.BalanceAdjustmentDeps{
			Tx:        repository.NewTransactor(base.db),
			Customers: base.customers,
			Ledger:    ledgerRepo,
			Logs:      logs,
		}),
	}
}

// rechargeInput 构造充值输入（实付缺省=本金，04-API.md:167-174）。
func rechargeInput(requestID string, customerID, principal, gift int64, method string) service.RechargeInput {
	return service.RechargeInput{
		RequestID:           requestID,
		CustomerID:          customerID,
		RechargeAmountCents: principal,
		GiftAmountCents:     gift,
		PaymentMethod:       method,
	}
}

// balanceRows 读取客户余额流水（id 升序=写入顺序，用于断言 before/after 连续）。
func (e *ledgerTestEnv) balanceRows(t *testing.T, customerID int64) []model.BalanceTransaction {
	t.Helper()
	var rows []model.BalanceTransaction
	if err := e.db.Where("customer_id = ?", customerID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取余额流水失败: %v", err)
	}
	return rows
}

// requireChainContinuous 断言余额流水链首尾相接（每笔 before == 上一笔 after、
// after == before + amount），且链尾 == 客户当前余额。
func (e *ledgerTestEnv) requireChainContinuous(t *testing.T, customerID, initialBefore int64) {
	t.Helper()
	expectedBefore := initialBefore
	for i, row := range e.balanceRows(t, customerID) {
		if row.BalanceBeforeCents != expectedBefore || row.BalanceAfterCents != row.BalanceBeforeCents+row.AmountCents {
			t.Errorf("流水[%d] = before %d/after %d/amount %d, want before %d 且 after=before+amount",
				i, row.BalanceBeforeCents, row.BalanceAfterCents, row.AmountCents, expectedBefore)
		}
		expectedBefore = row.BalanceAfterCents
	}
	if tail := e.customerAfter(t, customerID).BalanceCents; tail != expectedBefore {
		t.Errorf("流水链尾 = %d, 客户余额 = %d, want 相等", expectedBefore, tail)
	}
}

// ledgerEntityTables 是充值/调整事务涉及的全部实体表（「零写入」全表断言用）。
func ledgerEntityTables() map[string]any {
	return map[string]any{
		"orders":               &model.Order{},
		"order_items":          &model.OrderItem{},
		"recharge_records":     &model.RechargeRecord{},
		"balance_transactions": &model.BalanceTransaction{},
		"points_transactions":  &model.PointsTransaction{},
		"operation_logs":       &model.OperationLog{},
	}
}

// snapshotLedgerTables 统计全部实体表行数（失败路径「零写入」对照）。
func snapshotLedgerTables(t *testing.T, env *ledgerTestEnv) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, len(ledgerEntityTables()))
	for name, entity := range ledgerEntityTables() {
		counts[name] = env.countRows(t, entity)
	}
	return counts
}

// requireLedgerTablesUnchanged 断言全部实体表行数与快照一致。
func requireLedgerTablesUnchanged(t *testing.T, env *ledgerTestEnv, label string, before map[string]int64) {
	t.Helper()
	for name, entity := range ledgerEntityTables() {
		if got := env.countRows(t, entity); got != before[name] {
			t.Errorf("%s 后 %s 行数 = %d, want %d（零写入）", label, name, got, before[name])
		}
	}
}
