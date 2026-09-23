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

// 本文件承载挂单状态流转的共享事务原语（plan todo 26-28）：
// 状态原子守卫、金额重算、状态错误翻译与明细审计日志。
//
// 唯一连接池（SetMaxOpenConns(1)）下：
//   - 事务内只能使用 *Tx 方法；
//   - 需要非事务读取的错误翻译必须在事务结束之后执行。

// lockPendingOrderTx 在事务内锁定并返回 pending 订单：
//
//	UPDATE orders SET updated_at=? WHERE id=? AND status='pending'
//
// RowsAffected=0 → 订单不存在或已被结账/取消，返回 errOrderStateNotPending
// （由调用方在事务外翻译为 404/409/422）。条件 UPDATE 是原子守卫，
// 禁止「先查状态、再编辑」（03-DATABASE.md:333-347）。
func (s *OrderService) lockPendingOrderTx(ctx context.Context, tx repository.Tx, orderID int64, now time.Time) (*model.Order, error) {
	res := tx.WithContext(ctx).Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusPending).
		Update("updated_at", now)
	if res.Error != nil {
		return nil, fmt.Errorf("更新挂单时间戳失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, errOrderStateNotPending
	}
	order, err := s.orders.FindByIDTx(ctx, tx, orderID)
	if err != nil {
		return nil, fmt.Errorf("读取订单失败: %w", err)
	}
	return order, nil
}

// recalcOrderAmountsTx 在事务内按现存明细重算订单金额（全部整数分）：
// 原价 = Σ（成交金额 + 折扣）= Σ 标准价快照×数量；优惠 = Σ 折扣；实付 = 原价 − 优惠。
// 标准价取明细自身快照（成交价 + 单位折扣），不读当前服务价（服务改价不影响已有明细）。
func (s *OrderService) recalcOrderAmountsTx(ctx context.Context, tx repository.Tx, orderID int64, now time.Time) error {
	items, err := s.orders.ListItemsTx(ctx, tx, orderID)
	if err != nil {
		return fmt.Errorf("查询订单明细失败: %w", err)
	}
	var originalCents, discountCents, paidCents int64
	for i := range items {
		originalCents += items[i].AmountCents + items[i].DiscountAmountCents
		discountCents += items[i].DiscountAmountCents
		paidCents += items[i].AmountCents
	}
	if err := s.orders.UpdateAmountsTx(ctx, tx, orderID, originalCents, discountCents, paidCents, now); err != nil {
		return fmt.Errorf("更新订单金额失败: %w", err)
	}
	return nil
}

// notPendingConflict 把「订单不是 pending」翻译为 404（不存在）或 409（状态冲突）。
//
// 必须在事务结束之后调用：需要一次独立的非事务读取来区分 404/409
// （唯一连接池下事务内做非事务读会自锁）。
func (s *OrderService) notPendingConflict(ctx context.Context, orderID int64, message string) error {
	if _, err := s.orders.FindByID(ctx, orderID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "订单不存在")
		}
		return fmt.Errorf("查询订单失败: %w", err)
	}
	return Conflict(message)
}

// notPendingValidation 把「订单不是 pending」翻译为 404（不存在）或 422（业务校验失败）。
//
// 取消（cancel）对已结账/已退款/已取消订单返回 422（04-API.md:152-153），
// 与结账/明细编辑的 409 区分；必须在事务结束之后调用。
func (s *OrderService) notPendingValidation(ctx context.Context, orderID int64, message string) error {
	if _, err := s.orders.FindByID(ctx, orderID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "订单不存在")
		}
		return fmt.Errorf("查询订单失败: %w", err)
	}
	return Validation(message)
}

// itemStandardUnit 由明细行反推该行的标准单价快照（整数分）：
// standard = 成交单价 + 单位折扣；discount = (standard − unit) × quantity
// 由创建/改价路径保证整除，反推无损，且不受后续服务改价影响。
func itemStandardUnit(item *model.OrderItem) int64 {
	if item.Quantity <= 0 {
		return item.UnitPriceCents
	}
	return item.UnitPriceCents + item.DiscountAmountCents/int64(item.Quantity)
}

// writeOrderLog 在事务提交后写订单状态/明细审计日志（挂单明细编辑与取消共用）。
//
// 改价必须留痕（记录操作人、原价、成交价、原因，06 §3.1）；日志写入失败不阻断已提交的交易。
func (s *OrderService) writeOrderLog(ctx context.Context, action string, orderID int64, content string) {
	if err := s.logs.WriteLog(ctx, action, "order", orderID, content); err != nil {
		slog.Default().Error("写入订单审计日志失败", "order_id", orderID, "action", action, "err", err)
	}
}
