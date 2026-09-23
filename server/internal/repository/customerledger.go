package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 本文件承载 customers 表「账务缓存列」的事务内原子更新：
// balance_cents / points / total_spent_cents / last_visit_at 都只是流水的事实缓存，
// 任何变化必须与流水在同一事务内完成（03-DATABASE.md:188-240、06-BUSINESS-RULES.md:20-38）。
//
// 余额扣减一律使用「带条件的原子 UPDATE + RowsAffected」，
// 禁止「先查余额 → 应用层判断 → 再单独 UPDATE」（04-API.md:288-298 并发竞态）。

// DeductBalanceTx 在事务内原子扣减余额：
//
//	UPDATE customers SET balance_cents = balance_cents - ? WHERE id = ? AND balance_cents >= ?
//
// 返回是否扣减成功（RowsAffected > 0）：余额不足时返回 false，且不改动任何行。
func (r *CustomerRepository) DeductBalanceTx(ctx context.Context, tx Tx, customerID, amountCents int64) (bool, error) {
	res := tx.WithContext(ctx).Model(&model.Customer{}).
		Where("id = ? AND balance_cents >= ?", customerID, amountCents).
		UpdateColumn("balance_cents", gorm.Expr("balance_cents - ?", amountCents))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// BalanceTx 读取事务内的当前余额。
//
// 仅在 DeductBalanceTx 成功之后调用，用于写余额流水的 balance_before/after
// （扣减结果由原子 UPDATE 的 RowsAffected 决定，本读取不参与任何判断）。
func (r *CustomerRepository) BalanceTx(ctx context.Context, tx Tx, customerID int64) (int64, error) {
	var customer model.Customer
	if err := tx.WithContext(ctx).Select("balance_cents").Where("id = ?", customerID).First(&customer).Error; err != nil {
		return 0, err
	}
	return customer.BalanceCents, nil
}

// ApplyBalanceDeltaTx 在事务内原子增减余额（balance_cents = balance_cents + delta）：
//
//	delta > 0：直接累加（充值：本金 + 赠送）；
//	delta < 0：附加条件 balance_cents >= |delta|（原子防负余额，RowsAffected=0 → 未生效）。
//
// 返回 RowsAffected > 0（false = 客户不存在/已软删除，或负向调整会致负）。
// 充值入账与余额调整共用本原语；禁止「先查余额 → 应用层判断 → 再单独 UPDATE」
// （04-API.md:288-298 并发竞态）。
func (r *CustomerRepository) ApplyBalanceDeltaTx(ctx context.Context, tx Tx, customerID, deltaCents int64) (bool, error) {
	query := tx.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", customerID)
	if deltaCents < 0 {
		query = query.Where("balance_cents >= ?", -deltaCents)
	}
	res := query.UpdateColumn("balance_cents", gorm.Expr("balance_cents + ?", deltaCents))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// AddPointsTx 在事务内原子累加积分（points = points + ?）。
func (r *CustomerRepository) AddPointsTx(ctx context.Context, tx Tx, customerID, points int64) error {
	return tx.WithContext(ctx).Model(&model.Customer{}).
		Where("id = ?", customerID).
		UpdateColumn("points", gorm.Expr("points + ?", points)).Error
}

// ApplyPointsDeltaTx 在事务内原子增减积分（points = points + delta）：
//
//	delta >= 0：直接累加（消费赠分）；
//	delta < 0：附加条件 points >= |delta|（原子防负积分，RowsAffected=0 → 未生效，
//	           06 §6:74 积分不得低于 0）。
//
// 返回 RowsAffected > 0（false = 客户不存在/已软删除，或负向变更会致负）。
// 退款反向扣分与未来积分调整共用本原语；禁止「先查积分 → 应用层判断 → 再单独 UPDATE」
// （04-API.md:288-298 并发竞态）。
func (r *CustomerRepository) ApplyPointsDeltaTx(ctx context.Context, tx Tx, customerID, deltaPoints int64) (bool, error) {
	query := tx.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", customerID)
	if deltaPoints < 0 {
		query = query.Where("points >= ?", -deltaPoints)
	}
	res := query.UpdateColumn("points", gorm.Expr("points + ?", deltaPoints))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// PointsTx 读取事务内的当前积分（在 AddPointsTx 之后调用，用于积分流水 before/after）。
func (r *CustomerRepository) PointsTx(ctx context.Context, tx Tx, customerID int64) (int64, error) {
	var customer model.Customer
	if err := tx.WithContext(ctx).Select("points").Where("id = ?", customerID).First(&customer).Error; err != nil {
		return 0, err
	}
	return customer.Points, nil
}

// ApplyConsumptionTx 在事务内累计消费额并刷新最近到店时间（UTC）。
func (r *CustomerRepository) ApplyConsumptionTx(ctx context.Context, tx Tx, customerID, paidCents int64, visitedAt time.Time) error {
	return tx.WithContext(ctx).Model(&model.Customer{}).
		Where("id = ?", customerID).
		Updates(map[string]any{
			"total_spent_cents": gorm.Expr("total_spent_cents + ?", paidCents),
			"last_visit_at":     visitedAt,
		}).Error
}

// SubtractTotalSpentTx 在事务内扣减累计消费（订单退款冲减，06 §8:91）：
//
//	total_spent_cents = total_spent_cents - refundedCents
//
// 事实来源是订单退款记录（本节只在同一事务内维护客户统计缓存）。
func (r *CustomerRepository) SubtractTotalSpentTx(ctx context.Context, tx Tx, customerID, refundedCents int64) error {
	return tx.WithContext(ctx).Model(&model.Customer{}).
		Where("id = ?", customerID).
		UpdateColumn("total_spent_cents", gorm.Expr("total_spent_cents - ?", refundedCents)).Error
}
