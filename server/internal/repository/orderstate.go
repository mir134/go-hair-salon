package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 本文件承载挂单/结账/取消（plan todo 26-28）所需的订单状态与明细编辑访问：
//   - 事务内读取（FindByIDTx/ListItemsTx/FindItemTx）必须复用 Tx 句柄
//     （唯一连接池下在事务内调用非事务方法会等待被事务占用的连接）；
//   - 状态迁移一律使用「条件 UPDATE + RowsAffected」原子守卫
//     （WHERE status = 'pending'），禁止「先查状态、再更新」的并发竞态
//     （与余额扣减 DeductBalanceTx 同一模式，03-DATABASE.md:333-347）；
//   - order_items 禁止物理删除（AGENTS.md 第 5 节）：删除明细为软删除。

// FindByID 按主键读取订单；不存在返回 ErrNotFound。
func (r *OrderRepository) FindByID(ctx context.Context, id int64) (*model.Order, error) {
	var order model.Order
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &order, nil
}

// FindByIDTx 在事务内按主键读取订单；不存在返回 ErrNotFound。
func (r *OrderRepository) FindByIDTx(ctx context.Context, tx Tx, id int64) (*model.Order, error) {
	var order model.Order
	if err := tx.WithContext(ctx).Where("id = ?", id).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &order, nil
}

// ListItemsTx 在事务内按订单读取可见明细，id 升序（与下单顺序一致）。
func (r *OrderRepository) ListItemsTx(ctx context.Context, tx Tx, orderID int64) ([]model.OrderItem, error) {
	var items []model.OrderItem
	if err := tx.WithContext(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindItem 按主键读取订单明细（限定归属订单）；不存在返回 ErrNotFound。
//
// 仅用于编辑前的改价权限预判；编辑事务内以 FindItemTx 的实时值为准。
func (r *OrderRepository) FindItem(ctx context.Context, orderID, itemID int64) (*model.OrderItem, error) {
	var item model.OrderItem
	if err := r.db.WithContext(ctx).Where("order_id = ? AND id = ?", orderID, itemID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

// FindItemTx 在事务内按主键读取订单明细（限定归属订单）；不存在返回 ErrNotFound。
func (r *OrderRepository) FindItemTx(ctx context.Context, tx Tx, orderID, itemID int64) (*model.OrderItem, error) {
	var item model.OrderItem
	if err := tx.WithContext(ctx).Where("order_id = ? AND id = ?", orderID, itemID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

// UpdateItemTx 在事务内更新明细的数量/成交单价/折扣/金额（服务名与员工快照不变）。
func (r *OrderRepository) UpdateItemTx(ctx context.Context, tx Tx, item *model.OrderItem) error {
	return tx.WithContext(ctx).Model(&model.OrderItem{}).
		Where("id = ? AND order_id = ?", item.ID, item.OrderID).
		Updates(map[string]any{
			"quantity":              item.Quantity,
			"unit_price_cents":      item.UnitPriceCents,
			"discount_amount_cents": item.DiscountAmountCents,
			"amount_cents":          item.AmountCents,
		}).Error
}

// SoftDeleteItemTx 在事务内软删除明细（只置 deleted_at，行保留）；返回是否命中行。
func (r *OrderRepository) SoftDeleteItemTx(ctx context.Context, tx Tx, orderID, itemID int64) (bool, error) {
	res := tx.WithContext(ctx).Where("order_id = ? AND id = ?", orderID, itemID).Delete(&model.OrderItem{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateAmountsTx 在事务内按明细重算结果回写订单金额（全部整数分）。
func (r *OrderRepository) UpdateAmountsTx(ctx context.Context, tx Tx, orderID, originalCents, discountCents, paidCents int64, now time.Time) error {
	return tx.WithContext(ctx).Model(&model.Order{}).
		Where("id = ?", orderID).
		Updates(map[string]any{
			"original_amount_cents": originalCents,
			"discount_amount_cents": discountCents,
			"paid_amount_cents":     paidCents,
			"updated_at":            now,
		}).Error
}

// MarkCompletedTx 在事务内执行 pending → completed 的条件状态迁移：
//
//	UPDATE orders SET status='completed', payment_method=? WHERE id=? AND status='pending'
//
// 返回是否命中（RowsAffected > 0）：0 表示订单不存在或已被结账/取消，
// 调用方据此返回 404/409（防重复结账，04-API.md:151）。
func (r *OrderRepository) MarkCompletedTx(ctx context.Context, tx Tx, orderID int64, paymentMethod string, now time.Time) (bool, error) {
	res := tx.WithContext(ctx).Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusPending).
		Updates(map[string]any{
			"status":         model.OrderStatusCompleted,
			"payment_method": paymentMethod,
			"updated_at":     now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkCancelledTx 在事务内执行 pending → cancelled 的条件状态迁移（仅 admin 入口调用）。
//
// 返回是否命中：0 表示订单不存在或不是 pending（已结账只能退款，不能取消）。
func (r *OrderRepository) MarkCancelledTx(ctx context.Context, tx Tx, orderID int64, now time.Time) (bool, error) {
	res := tx.WithContext(ctx).Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusPending).
		Updates(map[string]any{
			"status":     model.OrderStatusCancelled,
			"updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkRefundedTx 在事务内执行 completed → refunded 的条件状态迁移（仅 admin 退款入口调用）：
//
//	UPDATE orders SET status='refunded', updated_at=? WHERE id=? AND status='completed'
//
// 返回是否命中（RowsAffected > 0）：0 表示订单不存在或不是 completed
// （pending/cancelled 不可退款、refunded 不可重复退款 → 409）。
// updated_at 取本次退款时间：营业额按退款发生日冲减（06 §8:98 字面口径），不回改历史。
func (r *OrderRepository) MarkRefundedTx(ctx context.Context, tx Tx, orderID int64, now time.Time) (bool, error) {
	res := tx.WithContext(ctx).Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusCompleted).
		Updates(map[string]any{
			"status":     model.OrderStatusRefunded,
			"updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
