package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// RechargeListFilter 是 GET /recharges 的查询条件（04-API.md:157-165、plan todo 31）。
//
// StartAt/EndAt 为 created_at 的半开区间 [StartAt, EndAt)，由 service 层按日期解析；
// 全部条件为空时返回全量充值记录（分页由 Offset/Limit 控制）。
type RechargeListFilter struct {
	CustomerID int64
	StartAt    *time.Time
	EndAt      *time.Time
	Offset     int
	Limit      int
}

// RechargeRow 是 recharge_records 与客户姓名的联表读取结果（列表 DTO 需要）。
type RechargeRow struct {
	model.RechargeRecord
	CustomerName string
}

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

// FindByID 按主键查询充值记录；不存在返回 ErrNotFound（冲正失败后的 404/409 翻译用）。
func (r *RechargeRepository) FindByID(ctx context.Context, id int64) (*model.RechargeRecord, error) {
	var record model.RechargeRecord
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

// FindByIDTx 在事务内按主键查询充值记录；不存在返回 ErrNotFound。
func (r *RechargeRepository) FindByIDTx(ctx context.Context, tx Tx, id int64) (*model.RechargeRecord, error) {
	var record model.RechargeRecord
	if err := tx.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

// MarkRefundedTx 在事务内执行 active → refunded 的条件状态迁移（仅 admin 冲正入口调用）：
//
//	UPDATE recharge_records SET status='refunded' WHERE id=? AND status='active'
//
// 返回是否命中（RowsAffected > 0）：0 表示记录不存在或已冲正（重复冲正 → 409）。
// 状态只置位不删除：原始记录与本金/赠送流水必须保留（06 §5:65）。
func (r *RechargeRepository) MarkRefundedTx(ctx context.Context, tx Tx, id int64) (bool, error) {
	res := tx.WithContext(ctx).Model(&model.RechargeRecord{}).
		Where("id = ? AND status = ?", id, model.RechargeStatusActive).
		Update("status", model.RechargeStatusRefunded)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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

// rechargeSelect / rechargeJoins：充值记录列 + 客户名左联。
//
// 历史充值记录必须显示客户姓名，因此客户软删除不参与联表过滤
// （原始 JOIN 不带 deleted_at 条件）；联表只用于读取姓名，不改变结果集。
const (
	rechargeSelect = "recharge_records.*, customers.name AS customer_name"
	rechargeJoins  = "LEFT JOIN customers ON customers.id = recharge_records.customer_id"
)

// List 按条件分页查询充值记录（含客户名），时间倒序（最新在前，同秒按 id 倒序）。
func (r *RechargeRepository) List(ctx context.Context, f RechargeListFilter) ([]RechargeRow, int64, error) {
	query := r.filtered(ctx, f)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []RechargeRow
	err := query.Select(rechargeSelect).Joins(rechargeJoins).
		Order("recharge_records.created_at DESC, recharge_records.id DESC").
		Limit(f.Limit).
		Offset(f.Offset).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// filtered 构造带全部过滤条件的查询（不联表，供 Count 复用）。
//
// 过滤列一律加 recharge_records. 前缀：Find 阶段会左联 customers，
// created_at 等同名列在联表后必须限定表名（否则 ambiguous column）。
func (r *RechargeRepository) filtered(ctx context.Context, f RechargeListFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&model.RechargeRecord{})
	if f.CustomerID > 0 {
		query = query.Where("recharge_records.customer_id = ?", f.CustomerID)
	}
	if f.StartAt != nil {
		query = query.Where("recharge_records.created_at >= ?", *f.StartAt)
	}
	if f.EndAt != nil {
		query = query.Where("recharge_records.created_at < ?", *f.EndAt)
	}
	return query
}
