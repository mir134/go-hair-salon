package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// RechargeRepository 提供 recharge_records 表的读写访问。
//
// 充值记录禁止物理删除（06-BUSINESS-RULES.md:54：错误充值通过退款/调整纠正）；
// request_id 唯一索引（ux_recharge_records_request_id）是幂等入账的最终保证。
type RechargeRepository struct {
	db *gorm.DB
}

// NewRechargeRepository 构造充值记录仓储。
func NewRechargeRepository(db *gorm.DB) *RechargeRepository {
	return &RechargeRepository{db: db}
}

// FindByRequestID 按幂等键查询充值记录；不存在返回 ErrNotFound。
func (r *RechargeRepository) FindByRequestID(ctx context.Context, requestID string) (*model.RechargeRecord, error) {
	var record model.RechargeRecord
	if err := r.db.WithContext(ctx).Where("request_id = ?", requestID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

// CreateTx 在事务内创建充值记录。
//
// 唯一索引冲突（request_id）返回 ErrDuplicate：service 层据此识别
// 「同一幂等键并发重复提交」，改为返回原记录（04-API.md:277-299）。
func (r *RechargeRepository) CreateTx(ctx context.Context, tx Tx, record *model.RechargeRecord) error {
	if err := tx.WithContext(ctx).Create(record).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}
