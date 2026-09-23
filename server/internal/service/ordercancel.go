package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// Cancel 取消挂单（pending → cancelled；仅 admin 入口调用，04-API.md:152-153、06 §3:37）：
//
//   - 仅 pending 可取消：completed/refunded/cancelled → 422（已结账订单只能退款）；
//   - 不产生任何资金/余额/积分变动；订单与明细行保留（禁止物理删除）；
//   - 事务内用条件状态迁移（WHERE status='pending' + RowsAffected）做原子守卫，
//     与结账并发时恰好一方成功（结账成功 → 取消 422；取消成功 → 结账 409）；
//   - 事务提交后写 operation_logs(action=order_cancel)。
//
// 挂单不自动过期（06 §3:33），取消是管理员的手动动作。
func (s *OrderService) Cancel(ctx context.Context, orderID int64) (*OrderDetail, error) {
	if orderID <= 0 {
		return nil, BadRequest("订单 id 不合法")
	}
	now := time.Now().UTC()
	var cancelledOrder *model.Order
	err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		cancelled, err := s.orders.MarkCancelledTx(ctx, tx, orderID, now)
		if err != nil {
			return fmt.Errorf("更新订单状态失败: %w", err)
		}
		if !cancelled {
			return errOrderStateNotPending
		}
		order, err := s.orders.FindByIDTx(ctx, tx, orderID)
		if err != nil {
			return fmt.Errorf("读取订单失败: %w", err)
		}
		cancelledOrder = order
		return nil
	})
	if err != nil {
		if errors.Is(err, errOrderStateNotPending) {
			return nil, s.notPendingValidation(ctx, orderID, "仅待结账（pending）订单可取消，已结账订单请走退款")
		}
		return nil, err
	}

	s.writeOrderLog(ctx, "order_cancel", orderID, fmt.Sprintf("取消挂单 %s，原价 %d 分，待结账 %d 分",
		cancelledOrder.OrderNo, cancelledOrder.OriginalAmountCents, cancelledOrder.PaidAmountCents))
	return s.Get(ctx, orderID)
}
