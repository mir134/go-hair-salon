package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// OperationLogRepository 提供 operation_logs 的写入访问。
//
// operation_logs 是审计表：只允许新增，禁止物理删除（AGENTS.md 第 5 节）。
type OperationLogRepository struct {
	db *gorm.DB
}

// NewOperationLogRepository 构造仓储。
func NewOperationLogRepository(db *gorm.DB) *OperationLogRepository {
	return &OperationLogRepository{db: db}
}

// Create 新增一条操作日志。
func (r *OperationLogRepository) Create(ctx context.Context, log *model.OperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}
