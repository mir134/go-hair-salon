package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// OperationLogRepository 提供 operation_logs 的读写访问。
//
// operation_logs 是审计表：只允许新增，禁止物理删除（AGENTS.md 第 5 节）。
type OperationLogRepository struct {
	db *gorm.DB
}

// NewOperationLogRepository 构造仓储。
func NewOperationLogRepository(db *gorm.DB) *OperationLogRepository {
	return &OperationLogRepository{db: db}
}

// OperationLogListFilter 是 GET /operation-logs 的查询条件（04-API.md:237-243、plan todo 47）。
//
// OperatorID=0 / Action="" 表示不过滤；StartAt/EndAt 为 created_at 的半开区间 [StartAt, EndAt)，
// 由 service 层按日期解析；Offset/Limit 控制分页（service 层归一化）。
type OperationLogListFilter struct {
	OperatorID int64
	Action     string
	StartAt    *time.Time
	EndAt      *time.Time
	Offset     int
	Limit      int
}

// OperationLogRow 是 operation_logs 与操作人用户名（users.username）的联表读取结果。
//
// 操作人已被停用（status=0）的历史日志仍必须可读，因此联表只取用户名、不参与过滤。
type OperationLogRow struct {
	model.OperationLog
	OperatorName string
}

// Create 新增一条操作日志。
func (r *OperationLogRepository) Create(ctx context.Context, log *model.OperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// operationLogSelect / operationLogJoins：操作日志列 + 操作人用户名左联。
//
// 过滤列一律加 operation_logs. 前缀：Find 阶段会左联 users，
// created_at/id 等同名列在联表后必须限定表名（否则 ambiguous column）。
const (
	operationLogSelect = "operation_logs.*, users.username AS operator_name"
	operationLogJoins  = "LEFT JOIN users ON users.id = operation_logs.operator_id"
)

// List 按条件分页查询操作日志（含操作人名），时间倒序（最新在前，同秒按 id 倒序）。
func (r *OperationLogRepository) List(ctx context.Context, f OperationLogListFilter) ([]OperationLogRow, int64, error) {
	query := r.filtered(ctx, f)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []OperationLogRow
	err := query.Select(operationLogSelect).Joins(operationLogJoins).
		Order("operation_logs.created_at DESC, operation_logs.id DESC").
		Limit(f.Limit).
		Offset(f.Offset).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// filtered 构造带全部过滤条件的查询（不联表，供 Count 复用）。
func (r *OperationLogRepository) filtered(ctx context.Context, f OperationLogListFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&model.OperationLog{})
	if f.OperatorID > 0 {
		query = query.Where("operation_logs.operator_id = ?", f.OperatorID)
	}
	if f.Action != "" {
		query = query.Where("operation_logs.action = ?", f.Action)
	}
	if f.StartAt != nil {
		query = query.Where("operation_logs.created_at >= ?", *f.StartAt)
	}
	if f.EndAt != nil {
		query = query.Where("operation_logs.created_at < ?", *f.EndAt)
	}
	return query
}
