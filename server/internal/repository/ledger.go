package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// LedgerRepository 提供资金/积分账本表的读取访问。
//
// balance_transactions 与 points_transactions 是账务事实来源，禁止物理删除；
// 余额/积分变化必须经 service 层事务写入（03-DATABASE.md:188-240）。
// 本文件当前只实现客户详情聚合所需的「按客户分页查询」，
// 写入路径由账务波次（todo 27/30/32+）在同一仓储上扩展。
type LedgerRepository struct {
	db *gorm.DB
}

// NewLedgerRepository 构造账本仓储。
func NewLedgerRepository(db *gorm.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// ListBalanceByCustomer 按客户分页查询余额流水，时间倒序（最新在前）。
func (r *LedgerRepository) ListBalanceByCustomer(ctx context.Context, customerID int64, offset, limit int) ([]model.BalanceTransaction, int64, error) {
	return listLedger[model.BalanceTransaction](ctx, r.db, &model.BalanceTransaction{}, customerID, offset, limit)
}

// ListPointsByCustomer 按客户分页查询积分流水，时间倒序（最新在前）。
func (r *LedgerRepository) ListPointsByCustomer(ctx context.Context, customerID int64, offset, limit int) ([]model.PointsTransaction, int64, error) {
	return listLedger[model.PointsTransaction](ctx, r.db, &model.PointsTransaction{}, customerID, offset, limit)
}

// CreateBalanceTx 在事务内写入余额流水（只增不删；调用方保证与余额更新同事务）。
func (r *LedgerRepository) CreateBalanceTx(ctx context.Context, tx Tx, transaction *model.BalanceTransaction) error {
	return tx.WithContext(ctx).Create(transaction).Error
}

// CreatePointsTx 在事务内写入积分流水（只增不删；调用方保证与积分更新同事务）。
func (r *LedgerRepository) CreatePointsTx(ctx context.Context, tx Tx, transaction *model.PointsTransaction) error {
	return tx.WithContext(ctx).Create(transaction).Error
}

// SumEarnedPointsByOrderTx 汇总订单产生的原 earn 积分（退款反向扣减依据）。
//
// 06 §6:73「退款按原消费积分进行反向扣减」且 §6:71 积分比例不追溯：
// 反向扣减必须使用订单当次写入积分流水的快照值，而不是按当前 points_per_yuan 重算。
func (r *LedgerRepository) SumEarnedPointsByOrderTx(ctx context.Context, tx Tx, orderID int64) (int64, error) {
	var earned int64
	err := tx.WithContext(ctx).Model(&model.PointsTransaction{}).
		Where("reference_type = ? AND reference_id = ? AND type = ?",
			model.ReferenceTypeOrder, orderID, model.PointsTxEarn).
		Select("COALESCE(SUM(points), 0)").
		Scan(&earned).Error
	if err != nil {
		return 0, err
	}
	return earned, nil
}

// listLedger 是余额/积分流水分页查询的公共实现：
// 按 customer_id 过滤、created_at 倒序（同秒时按 id 倒序保证稳定）。
func listLedger[T any](ctx context.Context, db *gorm.DB, entity any, customerID int64, offset, limit int) ([]T, int64, error) {
	var total int64
	if err := db.WithContext(ctx).Model(entity).Where("customer_id = ?", customerID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []T
	err := db.WithContext(ctx).
		Where("customer_id = ?", customerID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
