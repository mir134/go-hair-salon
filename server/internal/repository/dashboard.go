package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 本文件承载 Dashboard 统计的只读聚合（plan todo 44/45、06 §8:86-103）。
//
// 全部聚合以 UTC 半开区间 [startAt, endAt) 过滤；「日界」换算由 service 层按
// 服务器本地时区完成（D3 决议：UTC 存、查询转本地）。金额一律整数分。

// OrderWindowStats 是订单窗口聚合（营业额/消费单数/消费人数）。
type OrderWindowStats struct {
	PaidCents     int64 // 实付合计（整数分）
	OrderCount    int64 // 订单数
	CustomerCount int64 // 去重客户数
}

// DashboardRepository 提供 Dashboard 只读聚合访问。
type DashboardRepository struct {
	db *gorm.DB
}

// NewDashboardRepository 构造 Dashboard 聚合仓储。
func NewDashboardRepository(db *gorm.DB) *DashboardRepository {
	return &DashboardRepository{db: db}
}

// SumCompletedOrders 统计窗口内 completed 订单的实付合计/单数/去重客户数。
//
// 已退款订单不再是 completed（06 §8:93 不重复计入），其冲减由 SumRefundedOrders 承担。
func (r *DashboardRepository) SumCompletedOrders(ctx context.Context, startAt, endAt time.Time) (OrderWindowStats, error) {
	var row struct {
		PaidCents     int64 `gorm:"column:paid_cents"`
		OrderCount    int64 `gorm:"column:order_count"`
		CustomerCount int64 `gorm:"column:customer_count"`
	}
	err := r.db.WithContext(ctx).Model(&model.Order{}).
		Select("COALESCE(SUM(paid_amount_cents), 0) AS paid_cents, COUNT(*) AS order_count, COUNT(DISTINCT customer_id) AS customer_count").
		Where("status = ? AND created_at >= ? AND created_at < ?", model.OrderStatusCompleted, startAt, endAt).
		Scan(&row).Error
	if err != nil {
		return OrderWindowStats{}, err
	}
	return OrderWindowStats{PaidCents: row.PaidCents, OrderCount: row.OrderCount, CustomerCount: row.CustomerCount}, nil
}

// SumRefundedOrders 统计窗口内发生退款的订单实付合计（当日退款冲减）。
//
// 退款发生日以 orders.updated_at 标记（todo 35 决议：退款 worker 置 refunded 时写 updated_at），
// 不回改历史订单的 created_at（06 §8:98 字面口径）。
func (r *DashboardRepository) SumRefundedOrders(ctx context.Context, startAt, endAt time.Time) (int64, error) {
	var row struct {
		PaidCents int64 `gorm:"column:paid_cents"`
	}
	err := r.db.WithContext(ctx).Model(&model.Order{}).
		Select("COALESCE(SUM(paid_amount_cents), 0) AS paid_cents").
		Where("status = ? AND updated_at >= ? AND updated_at < ?", model.OrderStatusRefunded, startAt, endAt).
		Scan(&row).Error
	return row.PaidCents, err
}

// SumRechargeActual 统计窗口内充值记录的实付（actual_amount_cents）合计（资金流入口径）。
func (r *DashboardRepository) SumRechargeActual(ctx context.Context, startAt, endAt time.Time) (int64, error) {
	var row struct {
		ActualCents int64 `gorm:"column:actual_cents"`
	}
	err := r.db.WithContext(ctx).Model(&model.RechargeRecord{}).
		Select("COALESCE(SUM(actual_amount_cents), 0) AS actual_cents").
		Where("created_at >= ? AND created_at < ?", startAt, endAt).
		Scan(&row).Error
	return row.ActualCents, err
}

// SumReversedRechargeActual 统计窗口内被冲正充值的 actual_amount_cents 合计（当日冲正冲减）。
//
// recharge_records 无 updated_at 列（03-DATABASE.md:174-186），冲正发生日以反向余额流水
// （type=refund、reference_type=recharge）的 created_at 标记；每条充值最多一次冲正
// （状态迁移 active → refunded），不会重复计入。
func (r *DashboardRepository) SumReversedRechargeActual(ctx context.Context, startAt, endAt time.Time) (int64, error) {
	var row struct {
		ActualCents int64 `gorm:"column:actual_cents"`
	}
	err := r.db.WithContext(ctx).
		Table("recharge_records").
		Select("COALESCE(SUM(recharge_records.actual_amount_cents), 0) AS actual_cents").
		Joins("JOIN balance_transactions ON balance_transactions.reference_type = ? AND balance_transactions.reference_id = recharge_records.id",
			model.ReferenceTypeRecharge).
		Where("balance_transactions.type = ?", model.BalanceTxRefund).
		Where("balance_transactions.created_at >= ? AND balance_transactions.created_at < ?", startAt, endAt).
		Scan(&row).Error
	return row.ActualCents, err
}

// CountNewCustomers 统计窗口内新建客户数（默认 scope 排除软删除客户）。
func (r *DashboardRepository) CountNewCustomers(ctx context.Context, startAt, endAt time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Customer{}).
		Where("created_at >= ? AND created_at < ?", startAt, endAt).
		Count(&count).Error
	return count, err
}

// CountPendingOrders 统计当前 pending 挂单总数（实时，不接受任何日期范围）。
func (r *DashboardRepository) CountPendingOrders(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Order{}).
		Where("status = ?", model.OrderStatusPending).
		Count(&count).Error
	return count, err
}

// RecentCompletedOrders 读取最近完成的订单（含客户/员工名，时间倒序）。
func (r *DashboardRepository) RecentCompletedOrders(ctx context.Context, limit int) ([]OrderRow, error) {
	var rows []OrderRow
	err := r.db.WithContext(ctx).Model(&model.Order{}).
		Select(orderSelect).
		Joins(orderJoins).
		Where("orders.status = ?", model.OrderStatusCompleted).
		Order("orders.created_at DESC, orders.id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// RecentRecharges 读取最近充值记录（含客户名，时间倒序；含已冲正记录，状态可见）。
func (r *DashboardRepository) RecentRecharges(ctx context.Context, limit int) ([]RechargeRow, error) {
	var rows []RechargeRow
	err := r.db.WithContext(ctx).Model(&model.RechargeRecord{}).
		Select(rechargeSelect).
		Joins(rechargeJoins).
		Order("recharge_records.created_at DESC, recharge_records.id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
