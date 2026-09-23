package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载充值冲正（plan todo 36、04-API.md:159-165、06 §5:56-65、03-DATABASE.md:172,182）。
//
// 冲正 = 反向入账，不是删除（06 §5:65：冲正不得删除原始流水）：
//  1. 仅 active 可冲正（条件状态迁移 active → refunded；重复冲正 409，记录不存在 404）；
//  2. 冲正金额 = 本金 + 赠送（充值入账口径，03-DATABASE.md:172）；
//  3. 单事务：余额原子扣减（负向条件更新：余额 < 冲正金额 → 422 整体回滚，杜绝负余额）
//     → balance_transactions(type=refund, −冲正金额, before/after 连续, reference=recharge#id)；
//  4. 原始充值记录与本金/赠送流水原样保留；
//  5. 事务提交后写 operation_logs(action=recharge_refund)：硬规则要求 WriteLog 在事务外。

// errRechargeStateNotActive 是内部哨兵：充值记录不存在或不是 active 状态（冲正仅限生效记录）。
var errRechargeStateNotActive = errors.New("recharge is not active")

// errRechargeBalanceNotEnough 是余额不足以冲正的标准业务校验失败（422，整体回滚）。
var errRechargeBalanceNotEnough = Validation("客户余额不足，无法冲正（冲正后余额不能为负）")

// RechargeRefundResult 是充值冲正结果（记录已置 refunded；金额字段保留原值）。
type RechargeRefundResult struct {
	Record       *model.RechargeRecord
	CustomerName string
}

// RefundRecharge 充值冲正：
//
//  1. 事务内条件状态迁移 active → refunded（RowsAffected=0 → 404/409）；
//  2. 余额原子扣减 本金+赠送（不足 → 422 整体回滚，记录保持 active）；
//  3. 写反向余额流水（type=refund，before/after 连续）；
//  4. 事务提交后写 operation_logs(action=recharge_refund)。
//
// 权限（仅 admin）由路由 RBAC 中间件强制（06 §7），service 不重复判权。
func (s *RechargeService) RefundRecharge(ctx context.Context, rechargeID int64) (*RechargeRefundResult, error) {
	if rechargeID <= 0 {
		return nil, BadRequest("充值记录 id 不合法")
	}
	operatorID := OperatorIDPtr(ctx)
	var record model.RechargeRecord
	var reversedCents int64
	err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		ok, err := s.recharges.MarkRefundedTx(ctx, tx, rechargeID)
		if err != nil {
			return fmt.Errorf("更新充值记录状态失败: %w", err)
		}
		if !ok {
			return errRechargeStateNotActive
		}
		found, err := s.recharges.FindByIDTx(ctx, tx, rechargeID)
		if err != nil {
			return fmt.Errorf("读取充值记录失败: %w", err)
		}
		record = *found
		reversedCents = found.RechargeAmountCents + found.GiftAmountCents

		applied, err := s.customers.ApplyBalanceDeltaTx(ctx, tx, found.CustomerID, -reversedCents)
		if err != nil {
			return fmt.Errorf("冲正客户余额失败: %w", err)
		}
		if !applied {
			return s.translateRefundFailureTx(ctx, tx, found.CustomerID)
		}
		// 入账结果由原子 UPDATE 的 RowsAffected 决定；此处读取仅用于流水 before/after。
		balanceAfter, err := s.customers.BalanceTx(ctx, tx, found.CustomerID)
		if err != nil {
			return fmt.Errorf("读取冲正后余额失败: %w", err)
		}
		if err := s.ledger.CreateBalanceTx(ctx, tx, &model.BalanceTransaction{
			CustomerID:         found.CustomerID,
			Type:               model.BalanceTxRefund,
			AmountCents:        -reversedCents,
			BalanceBeforeCents: balanceAfter + reversedCents,
			BalanceAfterCents:  balanceAfter,
			ReferenceType:      model.ReferenceTypeRecharge,
			ReferenceID:        &found.ID,
			OperatorID:         operatorID,
			Remark:             "充值冲正",
		}); err != nil {
			return fmt.Errorf("写入冲正余额流水失败: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errRechargeStateNotActive) {
			return nil, s.notActiveConflict(ctx, rechargeID)
		}
		return nil, err
	}

	s.writeRechargeRefundLog(ctx, &record, reversedCents)
	name, err := s.customerName(ctx, record.CustomerID)
	if err != nil {
		return nil, err
	}
	return &RechargeRefundResult{Record: &record, CustomerName: name}, nil
}

// translateRefundFailureTx 把余额原子扣减未生效翻译为业务错误（事务内调用）：
//   - 客户仍存在 → 422 余额不足（冲正不得致负）；
//   - 客户不存在/已软删除 → 404。
func (s *RechargeService) translateRefundFailureTx(ctx context.Context, tx repository.Tx, customerID int64) error {
	var count int64
	if err := tx.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", customerID).Count(&count).Error; err != nil {
		return fmt.Errorf("查询客户失败: %w", err)
	}
	if count == 0 {
		return ErrCustomerNotFound
	}
	return errRechargeBalanceNotEnough
}

// notActiveConflict 把「充值记录不是 active」翻译为 404（不存在）或 409（已冲正）。
//
// 必须在事务结束之后调用：需要一次独立的非事务读取来区分 404/409
// （唯一连接池下事务内做非事务读会自锁）。
func (s *RechargeService) notActiveConflict(ctx context.Context, rechargeID int64) error {
	if _, err := s.recharges.FindByID(ctx, rechargeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "充值记录不存在")
		}
		return fmt.Errorf("查询充值记录失败: %w", err)
	}
	return Conflict("充值记录已冲正，请勿重复操作")
}

// writeRechargeRefundLog 在事务提交后写 recharge_refund 审计日志（06 §5:64 所有调整必须写 operation_logs）。
//
// 内容记录本金/赠送/扣减额与幂等键；日志写入失败不阻断已提交的交易。
func (s *RechargeService) writeRechargeRefundLog(ctx context.Context, record *model.RechargeRecord, reversedCents int64) {
	content := fmt.Sprintf("充值冲正：本金 %d 分 + 赠送 %d 分 = 扣减余额 %d 分，支付方式 %s，幂等键 %s",
		record.RechargeAmountCents, record.GiftAmountCents, reversedCents, record.PaymentMethod, record.RequestID)
	if err := s.logs.WriteLog(ctx, "recharge_refund", "recharge", record.ID, content); err != nil {
		slog.Default().Error("写入充值冲正审计日志失败", "recharge_id", record.ID, "err", err)
	}
}
