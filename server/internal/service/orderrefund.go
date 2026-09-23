package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载订单全额退款（plan todo 35、04-API.md:139,155,286、06 §5:56-65、§6:73-74、§8:91,98）。
//
// 退款只允许通过反向流水实现，不覆盖/删除历史金额（06 §5:59,61,65）：
//   - 余额支付订单：balance_transactions(type=refund, +金额) + customers.balance_cents 原子复原；
//   - 现金/微信/支付宝订单：不动余额，仅反向积分与冲减营业额；
//   - 积分：按订单原 earn 流水快照反向扣减（type=refund），不足 → 422 整体回滚（06 §6:74）；
//   - customers.total_spent_cents 扣减退款金额；
//   - orders.status → refunded（条件状态迁移：仅 completed 命中，重复退款 409）；
//   - 营业额冲减以退款发生日（本次 updated_at）为准，不回改历史订单（06 §8:98 字面口径）。
//
// 审计日志（action=order_refund）在事务提交后写入：硬规则要求 WriteLog 在事务外
// （唯一连接池下事务内写日志会等待被事务占用的连接）。

// errOrderStateNotCompleted 是内部哨兵：订单不存在或不是 completed 状态（退款仅限已完成订单）。
var errOrderStateNotCompleted = errors.New("order is not completed")

// errInsufficientPoints 是积分不足以反向扣减的标准业务校验失败（422，整体回滚）。
var errInsufficientPoints = Validation("客户积分不足，无法退款")

// Refund 订单全额退款（MVP 仅全额，06 §5:60）：
//
//  1. 事务内条件状态迁移 completed → refunded（RowsAffected=0 → 404/409）；
//  2. 余额支付订单复原余额 + 反向余额流水；非余额支付不动余额；
//  3. 反向扣减原 earn 积分（不足 → 422 整体回滚）+ 反向积分流水；
//  4. total_spent_cents 扣减退款金额；
//  5. 事务提交后写 operation_logs(action=order_refund)。
//
// 权限（仅 admin）由路由 RBAC 中间件强制（06 §7），service 不重复判权。
func (s *OrderService) Refund(ctx context.Context, orderID int64) (*OrderDetail, error) {
	if orderID <= 0 {
		return nil, BadRequest("订单 id 不合法")
	}
	now := time.Now().UTC()
	operatorID := OperatorIDPtr(ctx)
	var refunded *model.Order
	var reversedPoints int64
	err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		ok, err := s.orders.MarkRefundedTx(ctx, tx, orderID, now)
		if err != nil {
			return fmt.Errorf("更新订单状态失败: %w", err)
		}
		if !ok {
			return errOrderStateNotCompleted
		}
		order, err := s.orders.FindByIDTx(ctx, tx, orderID)
		if err != nil {
			return fmt.Errorf("读取订单失败: %w", err)
		}
		if err := s.refundBalanceTx(ctx, tx, order, operatorID); err != nil {
			return err
		}
		points, err := s.refundPointsTx(ctx, tx, order, operatorID)
		if err != nil {
			return err
		}
		reversedPoints = points
		if err := s.customers.SubtractTotalSpentTx(ctx, tx, order.CustomerID, order.PaidAmountCents); err != nil {
			return fmt.Errorf("更新客户累计消费失败: %w", err)
		}
		refunded = order
		return nil
	})
	if err != nil {
		if errors.Is(err, errOrderStateNotCompleted) {
			return nil, s.notPendingConflict(ctx, orderID, "仅已完成订单可退款（或订单已退款）")
		}
		return nil, err
	}

	s.writeRefundLog(ctx, refunded, reversedPoints)
	return s.Get(ctx, orderID)
}

// refundBalanceTx 余额支付订单的余额复原：
//
//	balance_cents = balance_cents + paid（原子累加）
//	→ balance_transactions(type=refund, +paid, before/after 连续, reference=order#id)
//
// 非余额支付订单不做任何余额写入（04-API.md:155：退款产生反向余额/积分流水）。
func (s *OrderService) refundBalanceTx(ctx context.Context, tx repository.Tx, order *model.Order, operatorID *int64) error {
	if order.PaymentMethod != model.PaymentMethodBalance {
		return nil
	}
	applied, err := s.customers.ApplyBalanceDeltaTx(ctx, tx, order.CustomerID, order.PaidAmountCents)
	if err != nil {
		return fmt.Errorf("复原客户余额失败: %w", err)
	}
	if !applied {
		return ErrCustomerNotFound
	}
	// 入账结果由原子 UPDATE 的 RowsAffected 决定；此处读取仅用于流水 before/after。
	balanceAfter, err := s.customers.BalanceTx(ctx, tx, order.CustomerID)
	if err != nil {
		return fmt.Errorf("读取退款后余额失败: %w", err)
	}
	if err := s.ledger.CreateBalanceTx(ctx, tx, &model.BalanceTransaction{
		CustomerID:         order.CustomerID,
		Type:               model.BalanceTxRefund,
		AmountCents:        order.PaidAmountCents,
		BalanceBeforeCents: balanceAfter - order.PaidAmountCents,
		BalanceAfterCents:  balanceAfter,
		ReferenceType:      model.ReferenceTypeOrder,
		ReferenceID:        &order.ID,
		OperatorID:         operatorID,
	}); err != nil {
		return fmt.Errorf("写入退款余额流水失败: %w", err)
	}
	return nil
}

// refundPointsTx 按订单原 earn 积分反向扣减（06 §6:73）：
//
//	earned = Σ points_transactions(type=earn, reference=order#id)（当次快照，不重算比例）
//	points = points − earned（原子条件更新：points >= earned，未命中 → 422 整体回滚）
//	→ points_transactions(type=refund, −earned, before/after 连续)
//
// 返回反向扣减的积分数；订单无 earn 流水（历史数据）视为 0，不做积分写入。
func (s *OrderService) refundPointsTx(ctx context.Context, tx repository.Tx, order *model.Order, operatorID *int64) (int64, error) {
	earned, err := s.ledger.SumEarnedPointsByOrderTx(ctx, tx, order.ID)
	if err != nil {
		return 0, fmt.Errorf("读取订单原积分流水失败: %w", err)
	}
	if earned <= 0 {
		return 0, nil
	}
	applied, err := s.customers.ApplyPointsDeltaTx(ctx, tx, order.CustomerID, -earned)
	if err != nil {
		return 0, fmt.Errorf("反向扣减客户积分失败: %w", err)
	}
	if !applied {
		return 0, errInsufficientPoints
	}
	pointsAfter, err := s.customers.PointsTx(ctx, tx, order.CustomerID)
	if err != nil {
		return 0, fmt.Errorf("读取退款后积分失败: %w", err)
	}
	if err := s.ledger.CreatePointsTx(ctx, tx, &model.PointsTransaction{
		CustomerID:    order.CustomerID,
		Type:          model.PointsTxRefund,
		Points:        -earned,
		BalanceBefore: pointsAfter + earned,
		BalanceAfter:  pointsAfter,
		ReferenceType: model.ReferenceTypeOrder,
		ReferenceID:   &order.ID,
		OperatorID:    operatorID,
	}); err != nil {
		return 0, fmt.Errorf("写入退款积分流水失败: %w", err)
	}
	return earned, nil
}

// writeRefundLog 在事务提交后写 order_refund 审计日志（06 §7:83 敏感操作记录 operator_id）。
//
// 内容记录订单号、退款金额、支付方式与反向积分；日志写入失败不阻断已提交的交易。
func (s *OrderService) writeRefundLog(ctx context.Context, order *model.Order, reversedPoints int64) {
	content := fmt.Sprintf("订单 %s 全额退款 %d 分，支付方式 %s",
		order.OrderNo, order.PaidAmountCents, order.PaymentMethod)
	if order.PaymentMethod == model.PaymentMethodBalance {
		content += "，余额已复原"
	}
	if reversedPoints > 0 {
		content += fmt.Sprintf("，反向扣减积分 %d", reversedPoints)
	}
	if err := s.logs.WriteLog(ctx, "order_refund", "order", order.ID, content); err != nil {
		slog.Default().Error("写入订单退款审计日志失败", "order_id", order.ID, "err", err)
	}
}
