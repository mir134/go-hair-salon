package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载挂单（pending）的明细编辑（plan todo 26、04-API.md:136-149、06-BUSINESS-RULES.md:30-33）：
//
//	POST   /orders/:id/items              追加服务项目（both）
//	PUT    /orders/:id/items/:item_id     修改数量 / 成交单价（both；改价仅 admin）
//	DELETE /orders/:id/items/:item_id     删除明细（both；软删除）
//
// 硬规则：
//   - 仅 status=pending 可编辑（否则 409）；事务内以「条件 UPDATE + RowsAffected」原子守卫，
//     禁止先查状态再编辑（并发结账竞态）；
//   - 编辑不产生任何资金/余额/积分变动；订单金额按现存明细重算；
//   - 改价仅 admin 且必填原因，写 operation_logs（06 §3.1）；
//   - 明细删除为软删除（AGENTS.md 第 5 节），行与服务名快照保留。

// OrderItemRef 定位挂单中的一行明细。
type OrderItemRef struct {
	OrderID int64
	ItemID  int64
}

// OrderItemAddInput 是挂单追加明细输入（UnitPriceCents=0 表示按标准价成交）。
type OrderItemAddInput struct {
	ServiceID      int64
	Quantity       int
	UnitPriceCents int64
	DiscountReason string
}

// OrderItemUpdateInput 是挂单修改明细输入：数量与成交单价均可选（nil=不变），
// 至少提供其一；成交单价变化（改价）必须由 admin 操作并填写 DiscountReason。
type OrderItemUpdateInput struct {
	Quantity       *int
	UnitPriceCents *int64
	DiscountReason string
}

// errOrderLastItem 是内部哨兵：删除会清空挂单（订单至少保留一个服务项目）。
var errOrderLastItem = errors.New("order must keep at least one item")

// AddItem 追加一行挂单明细（仅 pending；不产生任何资金变动）：
//   - 服务必须存在且启用（EnsureEnabledForConsumption）→ 404/422；
//   - 数量 > 0 → 400；成交单价与标准价不同 → 仅 admin + 必填原因（06 §3.1）；
//   - 明细继承订单员工快照；事务提交后写 operation_logs(action=order_item_add)。
func (s *OrderService) AddItem(ctx context.Context, orderID int64, in OrderItemAddInput) (*OrderDetail, error) {
	if orderID <= 0 {
		return nil, BadRequest("订单 id 不合法")
	}
	if in.Quantity <= 0 {
		return nil, BadRequest("服务数量必须大于 0")
	}
	service, err := s.services.EnsureEnabledForConsumption(ctx, in.ServiceID)
	if err != nil {
		return nil, err
	}
	unitPrice, err := resolveUnitPrice(in.UnitPriceCents, service.PriceCents)
	if err != nil {
		return nil, err
	}
	if unitPrice != service.PriceCents {
		if err := ensureCanOverridePrice(ctx, in.DiscountReason); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	var order *model.Order
	err = s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		current, err := s.lockPendingOrderTx(ctx, tx, orderID, now)
		if err != nil {
			return err
		}
		order = current
		items := []model.OrderItem{{
			OrderID:             orderID,
			ServiceID:           service.ID,
			ServiceNameSnapshot: service.Name,
			Quantity:            in.Quantity,
			UnitPriceCents:      unitPrice,
			DiscountAmountCents: (service.PriceCents - unitPrice) * int64(in.Quantity),
			AmountCents:         unitPrice * int64(in.Quantity),
			EmployeeID:          order.EmployeeID,
		}}
		if err := s.orders.CreateItemsTx(ctx, tx, items); err != nil {
			return fmt.Errorf("创建订单明细失败: %w", err)
		}
		return s.recalcOrderAmountsTx(ctx, tx, orderID, now)
	})
	if err != nil {
		return nil, s.pendingEditError(ctx, orderID, err)
	}
	s.writeItemLog(ctx, "order_item_add", orderID, fmt.Sprintf(
		"挂单 %s 追加服务 %s ×%d，成交单价 %d 分", order.OrderNo, service.Name, in.Quantity, unitPrice))
	return s.Get(ctx, orderID)
}

// UpdateItem 修改一行挂单明细的数量和/或成交单价（仅 pending；不产生资金变动）：
//   - 至少提供数量或成交单价 → 400；数量 ≤ 0 → 400；成交单价为负 → 400；
//   - 成交单价 0 表示恢复该明细的标准价快照（standard = 成交价 + 单位折扣）；
//   - 成交单价变化 → 仅 admin + 必填原因（403/400），并写 operation_logs（06 §3.1）。
func (s *OrderService) UpdateItem(ctx context.Context, ref OrderItemRef, in OrderItemUpdateInput) (*OrderDetail, error) {
	if ref.OrderID <= 0 || ref.ItemID <= 0 {
		return nil, BadRequest("订单或明细 id 不合法")
	}
	if in.Quantity == nil && in.UnitPriceCents == nil {
		return nil, BadRequest("至少提供数量或成交单价")
	}
	if in.Quantity != nil && *in.Quantity <= 0 {
		return nil, BadRequest("服务数量必须大于 0")
	}
	current, err := s.orders.FindItem(ctx, ref.OrderID, ref.ItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "订单明细不存在")
		}
		return nil, fmt.Errorf("查询订单明细失败: %w", err)
	}

	// 改价判定在事务外预判（权限失败必须零写入）；事务内以条件状态守卫为准。
	standardUnit := itemStandardUnit(current)
	newUnit := current.UnitPriceCents
	priceChanged := false
	if in.UnitPriceCents != nil {
		newUnit, err = resolveUnitPrice(*in.UnitPriceCents, standardUnit)
		if err != nil {
			return nil, err
		}
		priceChanged = newUnit != current.UnitPriceCents
		if priceChanged {
			if err := ensureCanOverridePrice(ctx, in.DiscountReason); err != nil {
				return nil, err
			}
		}
	}
	newQuantity := current.Quantity
	if in.Quantity != nil {
		newQuantity = *in.Quantity
	}
	newDiscount := (standardUnit - newUnit) * int64(newQuantity)
	newAmount := newUnit * int64(newQuantity)

	now := time.Now().UTC()
	var order *model.Order
	err = s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		locked, err := s.lockPendingOrderTx(ctx, tx, ref.OrderID, now)
		if err != nil {
			return err
		}
		order = locked
		item, err := s.orders.FindItemTx(ctx, tx, ref.OrderID, ref.ItemID)
		if err != nil {
			return err
		}
		item.Quantity = newQuantity
		item.UnitPriceCents = newUnit
		item.DiscountAmountCents = newDiscount
		item.AmountCents = newAmount
		if err := s.orders.UpdateItemTx(ctx, tx, item); err != nil {
			return fmt.Errorf("更新订单明细失败: %w", err)
		}
		return s.recalcOrderAmountsTx(ctx, tx, ref.OrderID, now)
	})
	if err != nil {
		return nil, s.pendingEditError(ctx, ref.OrderID, err)
	}
	content := fmt.Sprintf("挂单 %s 修改明细 %s：数量 %d，成交单价 %d 分",
		order.OrderNo, current.ServiceNameSnapshot, newQuantity, newUnit)
	if priceChanged {
		content = fmt.Sprintf("挂单 %s 修改明细 %s 改价：成交单价 %d → %d 分，数量 %d；改价原因：%s",
			order.OrderNo, current.ServiceNameSnapshot, current.UnitPriceCents, newUnit, newQuantity, strings.TrimSpace(in.DiscountReason))
	}
	s.writeItemLog(ctx, "order_item_update", ref.OrderID, content)
	return s.Get(ctx, ref.OrderID)
}

// RemoveItem 软删除一行挂单明细（仅 pending；不产生资金变动）：
//   - 明细不存在（或不属于该订单）→ 404；
//   - 删除最后一条明细 → 422（如需作废请取消订单）；
//   - 行保留（deleted_at 非空），事务提交后写 operation_logs(action=order_item_delete)。
func (s *OrderService) RemoveItem(ctx context.Context, ref OrderItemRef) (*OrderDetail, error) {
	if ref.OrderID <= 0 || ref.ItemID <= 0 {
		return nil, BadRequest("订单或明细 id 不合法")
	}
	now := time.Now().UTC()
	var order *model.Order
	var removed model.OrderItem
	err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		locked, err := s.lockPendingOrderTx(ctx, tx, ref.OrderID, now)
		if err != nil {
			return err
		}
		order = locked
		item, err := s.orders.FindItemTx(ctx, tx, ref.OrderID, ref.ItemID)
		if err != nil {
			return err
		}
		items, err := s.orders.ListItemsTx(ctx, tx, ref.OrderID)
		if err != nil {
			return fmt.Errorf("查询订单明细失败: %w", err)
		}
		if len(items) <= 1 {
			return errOrderLastItem
		}
		ok, err := s.orders.SoftDeleteItemTx(ctx, tx, ref.OrderID, ref.ItemID)
		if err != nil {
			return fmt.Errorf("删除订单明细失败: %w", err)
		}
		if !ok {
			return repository.ErrNotFound
		}
		removed = *item
		return s.recalcOrderAmountsTx(ctx, tx, ref.OrderID, now)
	})
	if err != nil {
		switch {
		case errors.Is(err, errOrderStateNotPending):
			return nil, s.notPendingConflict(ctx, ref.OrderID, "订单已结账或已取消，不能编辑明细")
		case errors.Is(err, errOrderLastItem):
			return nil, Validation("订单至少保留一个服务项目，如需作废请取消订单")
		case errors.Is(err, repository.ErrNotFound):
			return nil, NotFound(CodeNotFound, "订单明细不存在")
		default:
			return nil, err
		}
	}
	s.writeItemLog(ctx, "order_item_delete", ref.OrderID, fmt.Sprintf(
		"挂单 %s 删除明细 %s ×%d（成交单价 %d 分）",
		order.OrderNo, removed.ServiceNameSnapshot, removed.Quantity, removed.UnitPriceCents))
	return s.Get(ctx, ref.OrderID)
}

// pendingEditError 把挂单编辑的事务错误翻译为业务错误。
func (s *OrderService) pendingEditError(ctx context.Context, orderID int64, err error) error {
	switch {
	case errors.Is(err, errOrderStateNotPending):
		return s.notPendingConflict(ctx, orderID, "订单已结账或已取消，不能编辑明细")
	case errors.Is(err, repository.ErrNotFound):
		return NotFound(CodeNotFound, "订单明细不存在")
	default:
		return err
	}
}
