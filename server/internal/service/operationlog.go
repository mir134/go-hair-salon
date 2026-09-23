package service

import (
	"context"
	"fmt"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// OperationLogService 负责写 operation_logs 审计日志（03-DATABASE.md:265-277）。
//
// 硬规则（todo 6 / P0 前置）：
//   - 各业务波次必须在本业务事务提交之后调用 WriteLog，审计记录不随业务回滚丢失
//     （禁止在业务事务内调用，否则会阻塞唯一连接并可能被回滚）；
//   - operation_logs 只增不删，撤销一律通过 refund/adjustment/cancel 业务流程留痕。
type OperationLogService struct {
	repo *repository.OperationLogRepository
}

// NewOperationLogService 构造记账服务。
func NewOperationLogService(repo *repository.OperationLogRepository) *OperationLogService {
	return &OperationLogService{repo: repo}
}

// WriteLog 写入一条操作日志。
//
//	action     业务动作，如 order_create / login_failed
//	targetType 目标类型，如 order / customer / recharge；无目标时传 ""
//	targetID   目标 id；无目标时传 0
//	content    可读描述（禁止写入密码/JWT 等敏感值）
//
// operator_id 取自 JWT 上下文（缺失则 NULL）；ip/ua 取自请求上下文（缺失则为空字符串）。
// 审计日志不随请求取消而丢弃，因此使用 context.WithoutCancel 保留上下文值。
func (s *OperationLogService) WriteLog(ctx context.Context, action, targetType string, targetID int64, content string) error {
	log := &model.OperationLog{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Content:    content,
	}
	if operatorID, ok := OperatorID(ctx); ok {
		log.OperatorID = &operatorID
	}
	meta := RequestMetaFrom(ctx)
	log.IP = meta.IP
	log.UserAgent = meta.UserAgent

	if err := s.repo.Create(context.WithoutCancel(ctx), log); err != nil {
		return fmt.Errorf("写入操作日志失败（action=%s）: %w", action, err)
	}
	return nil
}
