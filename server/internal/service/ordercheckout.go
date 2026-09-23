package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// OrderPayInput 是挂单结账输入（04-API.md:150-154）。
type OrderPayInput struct {
	PaymentMethod string
}

// Pay 挂单结账（pending → completed，03-DATABASE.md:333-347、06-BUSINESS-RULES.md:34-36）：
//
//  1. 支付方式必须 ∈ {cash, wechat, alipay, balance} → 400；
//  2. 单事务：
//     条件状态迁移（WHERE id=? AND status='pending'；RowsAffected=0 → 409 防重复结账）
//     → 读取结账后订单（金额取明细最终重算值）
//     → 余额原子扣减 + 余额流水（仅余额支付）
//     → 积分流水 + 客户积分 → total_spent_cents + last_visit_at；
//  3. 任一步失败整体回滚（订单保持 pending，可重试）；
//  4. 事务提交后写 operation_logs(action=order_pay)。
//
// 账务写入复用直接完成消费的同一套事务原语（applyBalanceTx/applyPointsTx/
// ApplyConsumptionTx），保证结账与直接完成的流水语义完全一致。
func (s *OrderService) Pay(ctx context.Context, orderID int64, in OrderPayInput) (*OrderDetail, error) {
	if orderID <= 0 {
		return nil, BadRequest("订单 id 不合法")
	}
	if !isValidPaymentMethod(in.PaymentMethod) {
		return nil, BadRequest("支付方式不合法")
	}
	// 积分比例必须在事务外读取（唯一连接池下事务内非事务读会自锁）。
	ratio, err := s.pointsPerYuan(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	operatorID := OperatorIDPtr(ctx)
	var paidOrder *model.Order
	err = s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		completed, err := s.orders.MarkCompletedTx(ctx, tx, orderID, in.PaymentMethod, now)
		if err != nil {
			return fmt.Errorf("更新订单状态失败: %w", err)
		}
		if !completed {
			return errOrderStateNotPending
		}
		order, err := s.orders.FindByIDTx(ctx, tx, orderID)
		if err != nil {
			return fmt.Errorf("读取订单失败: %w", err)
		}
		if err := s.applyBalanceTx(ctx, tx, order, operatorID); err != nil {
			return err
		}
		// 积分 = floor(实付分 × 每元积分 ÷ 100)：整数先乘后除（06 §6）。
		points := order.PaidAmountCents * ratio / 100
		if err := s.applyPointsTx(ctx, tx, order, points, operatorID); err != nil {
			return err
		}
		if err := s.customers.ApplyConsumptionTx(ctx, tx, order.CustomerID, order.PaidAmountCents, now); err != nil {
			return fmt.Errorf("更新客户消费统计失败: %w", err)
		}
		paidOrder = order
		return nil
	})
	if err != nil {
		if errors.Is(err, errOrderStateNotPending) {
			return nil, s.notPendingConflict(ctx, orderID, "订单已结账或已取消，请勿重复结账")
		}
		return nil, err
	}

	s.writePayLog(ctx, paidOrder)
	return s.Get(ctx, orderID)
}

// writePayLog 在事务提交后写 order_pay 审计日志。
//
// 日志写入失败不阻断已提交的交易（与既有记账策略一致）；
// 内容记录支付方式与金额，禁止写入敏感信息。
func (s *OrderService) writePayLog(ctx context.Context, order *model.Order) {
	content := fmt.Sprintf("挂单结账 %s，支付方式 %s，实付 %d 分（原价 %d 分，优惠 %d 分）",
		order.OrderNo, strings.TrimSpace(order.PaymentMethod),
		order.PaidAmountCents, order.OriginalAmountCents, order.DiscountAmountCents)
	if err := s.logs.WriteLog(ctx, "order_pay", "order", order.ID, content); err != nil {
		slog.Default().Error("写入结账审计日志失败", "order_id", order.ID, "err", err)
	}
}
