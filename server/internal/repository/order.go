package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// OrderRepository 提供 orders 表的读取访问。
//
// 订单禁止物理删除（退款/取消保留记录，AGENTS.md 第 5 节）；
// 本文件当前只实现客户详情聚合所需的「按客户分页查询」，
// 创建/结账/退款等写路径由订单波次（todo 21+）在同一仓储上扩展。
type OrderRepository struct {
	db *gorm.DB
}

// NewOrderRepository 构造订单仓储。
func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// ListByCustomer 按客户分页查询订单，时间倒序（最新在前）。
func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID int64, offset, limit int) ([]model.Order, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.Order{}).Where("customer_id = ?", customerID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []model.Order
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
