package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// BalanceAdjustmentService 负责管理员余额调整（04-API.md:176-193、06 §5:62-65）。
//
// 单事务内完成：余额原子增减（负向附带 balance_cents >= |amount| 条件，杜绝负余额）
// + balance_transactions(type=adjustment，before/after 连续)；事务提交后写
// operation_logs(action=balance_adjust)。
//
// 仅调整余额：不产生订单、不计入营业额、不改累计消费/积分（04-API.md:192）；
// 调整不得删除或覆盖历史流水（06 §5:65）。
type BalanceAdjustmentService struct {
	tx        *repository.Transactor
	customers *repository.CustomerRepository
	ledger    *repository.LedgerRepository
	logs      *OperationLogService
}

// BalanceAdjustmentDeps 是余额调整服务的依赖集合。
type BalanceAdjustmentDeps struct {
	Tx        *repository.Transactor
	Customers *repository.CustomerRepository
	Ledger    *repository.LedgerRepository
	Logs      *OperationLogService
}

// NewBalanceAdjustmentService 构造余额调整服务。
func NewBalanceAdjustmentService(deps BalanceAdjustmentDeps) *BalanceAdjustmentService {
	return &BalanceAdjustmentService{
		tx:        deps.Tx,
		customers: deps.Customers,
		ledger:    deps.Ledger,
		logs:      deps.Logs,
	}
}

// BalanceAdjustmentInput 是余额调整输入。
//
// AmountCents 为带符号整数分（正=增加，负=扣减），必须非 0；Reason 必填。
type BalanceAdjustmentInput struct {
	CustomerID  int64
	AmountCents int64
	Reason      string
}

// BalanceAdjustmentResult 是余额调整结果（返回调整流水，含调整后余额）。
type BalanceAdjustmentResult struct {
	Transaction model.BalanceTransaction
}

// errBalanceWouldGoNegative 是负向调整致负余额的标准业务校验失败（422，零写入）。
var errBalanceWouldGoNegative = Validation("调整后余额不能为负")

// Adjust 余额调整（04-API.md:184-192）：
//
//  1. 校验金额非 0、原因必填、客户存在；
//  2. 单事务：余额原子增减（负向条件更新，未生效 → 422 整体回滚）
//     → 读取调整后余额 → 写 balance_transactions(type=adjustment)；
//  3. 事务提交后写 operation_logs(action=balance_adjust)。
//
// 权限（仅 admin）由路由 RBAC 中间件强制（06 §7），service 不重复判权。
func (s *BalanceAdjustmentService) Adjust(ctx context.Context, in BalanceAdjustmentInput) (*BalanceAdjustmentResult, error) {
	if in.CustomerID <= 0 {
		return nil, BadRequest("客户 id 不合法")
	}
	if in.AmountCents == 0 {
		return nil, BadRequest("调整金额不能为 0（整数分）")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, BadRequest("调整原因不能为空")
	}

	customer, err := s.customers.FindByID(ctx, in.CustomerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("查询客户失败: %w", err)
	}

	operatorID := OperatorIDPtr(ctx)
	var transaction model.BalanceTransaction
	if err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		applied, err := s.customers.ApplyBalanceDeltaTx(ctx, tx, in.CustomerID, in.AmountCents)
		if err != nil {
			return fmt.Errorf("更新客户余额失败: %w", err)
		}
		if !applied {
			return s.translateDeltaFailure(ctx, tx, in)
		}
		// 入账结果由原子 UPDATE 的 RowsAffected 决定；此处读取仅用于流水 before/after。
		balanceAfter, err := s.customers.BalanceTx(ctx, tx, in.CustomerID)
		if err != nil {
			return fmt.Errorf("读取调整后余额失败: %w", err)
		}
		transaction = model.BalanceTransaction{
			CustomerID:         in.CustomerID,
			Type:               model.BalanceTxAdjustment,
			AmountCents:        in.AmountCents,
			BalanceBeforeCents: balanceAfter - in.AmountCents,
			BalanceAfterCents:  balanceAfter,
			OperatorID:         operatorID,
			Remark:             reason,
		}
		if err := s.ledger.CreateBalanceTx(ctx, tx, &transaction); err != nil {
			return fmt.Errorf("写入余额流水失败: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.writeAdjustLog(ctx, &transaction, customer.Name)
	return &BalanceAdjustmentResult{Transaction: transaction}, nil
}

// translateDeltaFailure 把原子增减未生效翻译为业务错误（事务内调用）：
//   - 负向调整：客户存在 → 422 余额不足（不能为负）；客户已消失 → 404；
//   - 正向调整：客户不存在/已软删除 → 404。
func (s *BalanceAdjustmentService) translateDeltaFailure(ctx context.Context, tx repository.Tx, in BalanceAdjustmentInput) error {
	if in.AmountCents < 0 {
		exists, err := s.customerExistsTx(ctx, tx, in.CustomerID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrCustomerNotFound
		}
		return errBalanceWouldGoNegative
	}
	return ErrCustomerNotFound
}

// customerExistsTx 事务内判断客户是否存在（含未软删除）。
func (s *BalanceAdjustmentService) customerExistsTx(ctx context.Context, tx repository.Tx, customerID int64) (bool, error) {
	var count int64
	if err := tx.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", customerID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("查询客户失败: %w", err)
	}
	return count > 0, nil
}

// writeAdjustLog 在事务提交后写 balance_adjust 审计日志（06 §5:64 所有调整必须写 operation_logs）。
//
// 内容记录调整前后余额与原因；日志写入失败不阻断已提交的交易。
func (s *BalanceAdjustmentService) writeAdjustLog(ctx context.Context, transaction *model.BalanceTransaction, customerName string) {
	content := fmt.Sprintf("余额调整 %+d 分（%d → %d），客户 %s，原因：%s",
		transaction.AmountCents, transaction.BalanceBeforeCents, transaction.BalanceAfterCents,
		customerName, transaction.Remark)
	if err := s.logs.WriteLog(ctx, "balance_adjust", "customer", transaction.CustomerID, content); err != nil {
		slog.Default().Error("写入余额调整审计日志失败", "customer_id", transaction.CustomerID, "err", err)
	}
}
