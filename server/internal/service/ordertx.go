package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载直接完成订单的事务内账务写入（service/order.go:Create 调用）：
// 余额原子扣减 + 余额流水、积分流水 + 客户积分、以及事务提交后的审计日志。
// 所有写入必须复用事务句柄 tx（唯一连接池下禁止在事务内调用非事务仓储方法）。

// applyBalanceTx 仅对余额支付执行原子条件扣减并写余额流水（06-BUSINESS-RULES.md:26-27、35）：
//
//	UPDATE customers SET balance_cents = balance_cents - ? WHERE id = ? AND balance_cents >= ?
//
// RowsAffected=0 → 余额不足（422），由事务整体回滚；
// 禁止「先查余额、应用层判断、再单独 UPDATE」（04-API.md:288-298）。
func (s *OrderService) applyBalanceTx(ctx context.Context, tx repository.Tx, order *model.Order, operatorID *int64) error {
	if order.PaymentMethod != model.PaymentMethodBalance {
		return nil
	}
	deducted, err := s.customers.DeductBalanceTx(ctx, tx, order.CustomerID, order.PaidAmountCents)
	if err != nil {
		return fmt.Errorf("扣减余额失败: %w", err)
	}
	if !deducted {
		return errInsufficientBalance
	}
	// 扣减结果已由原子 UPDATE 决定；此处读取仅用于流水 before/after。
	balanceAfter, err := s.customers.BalanceTx(ctx, tx, order.CustomerID)
	if err != nil {
		return fmt.Errorf("读取扣减后余额失败: %w", err)
	}
	if err := s.ledger.CreateBalanceTx(ctx, tx, &model.BalanceTransaction{
		CustomerID:         order.CustomerID,
		Type:               model.BalanceTxConsume,
		AmountCents:        -order.PaidAmountCents,
		BalanceBeforeCents: balanceAfter + order.PaidAmountCents,
		BalanceAfterCents:  balanceAfter,
		ReferenceType:      model.ReferenceTypeOrder,
		ReferenceID:        &order.ID,
		OperatorID:         operatorID,
	}); err != nil {
		return fmt.Errorf("写入余额流水失败: %w", err)
	}
	return nil
}

// applyPointsTx 累加客户积分并写积分流水（消费产生积分，充值不产生；06 §6）。
func (s *OrderService) applyPointsTx(ctx context.Context, tx repository.Tx, order *model.Order, points int64, operatorID *int64) error {
	if err := s.customers.AddPointsTx(ctx, tx, order.CustomerID, points); err != nil {
		return fmt.Errorf("更新客户积分失败: %w", err)
	}
	pointsAfter, err := s.customers.PointsTx(ctx, tx, order.CustomerID)
	if err != nil {
		return fmt.Errorf("读取客户积分失败: %w", err)
	}
	if err := s.ledger.CreatePointsTx(ctx, tx, &model.PointsTransaction{
		CustomerID:    order.CustomerID,
		Type:          model.PointsTxEarn,
		Points:        points,
		BalanceBefore: pointsAfter - points,
		BalanceAfter:  pointsAfter,
		ReferenceType: model.ReferenceTypeOrder,
		ReferenceID:   &order.ID,
		OperatorID:    operatorID,
	}); err != nil {
		return fmt.Errorf("写入积分流水失败: %w", err)
	}
	return nil
}

// writeCreateLog 在事务提交后写 order_create 审计日志。
//
// 挂单（pending）与直接完成使用同一 action，content 区分收款状态；
// 改价原因必须留痕（06 §3.1：记录操作人、原价、成交价、原因）；
// 日志写入失败不阻断已提交的交易（与既有控制器写日志策略一致）。
func (s *OrderService) writeCreateLog(ctx context.Context, order *model.Order, reason string) {
	content := fmt.Sprintf("创建订单 %s，原价 %d 分，优惠 %d 分，实付 %d 分",
		order.OrderNo, order.OriginalAmountCents, order.DiscountAmountCents, order.PaidAmountCents)
	if order.Status == model.OrderStatusPending {
		content = fmt.Sprintf("创建挂单 %s，原价 %d 分，优惠 %d 分，待结账 %d 分",
			order.OrderNo, order.OriginalAmountCents, order.DiscountAmountCents, order.PaidAmountCents)
	}
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		content += fmt.Sprintf("；改价原因：%s", trimmed)
	}
	if err := s.logs.WriteLog(ctx, "order_create", "order", order.ID, content); err != nil {
		slog.Default().Error("写入订单审计日志失败", "order_id", order.ID, "err", err)
	}
}
