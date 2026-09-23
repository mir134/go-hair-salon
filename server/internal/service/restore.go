package service

// 恢复服务（todo 52；04-API.md:245-256、08-DEPLOYMENT.md:78、05-TASKS.md:165-169）。
//
// 流程（严格按 08-DEPLOYMENT.md:78 顺序）：
//  1. 二次确认（controller 层校验 confirm=true）；
//  2. 进入维护模式：业务写请求一律 503 + 50300，读请求放行；
//  3. 对当前数据自动生成安全备份（复用 BackupService 的 WAL 安全快照）；
//  4. 校验目标 zip：存在、包含可读 SQLite 快照（integrity_check=ok + 关键表可查）；
//  5. 关闭数据库连接 → 替换 DB 文件/uploads/config → 重新打开 → 数据检查；
//  6. 退出维护模式（defer 保证），在事务之外写 operation_logs(action=restore)。
//
// 失败语义（数据红线）：
//   - 步骤 5 之前的任何失败都不触碰现有数据（只在 staging 目录里操作，见 restorestage.go）；
//   - 步骤 5 内部的失败会回滚原数据库文件并重开连接（见 restoreswap.go）；
//   - 维护标志无论成败都由 defer 清除（cancel_resume 防线）；
//   - 恢复前安全备份始终先于任何破坏性动作生成。
//
// 并发：整个恢复过程持有 BackupService 的互斥锁，与手动/自动备份串行，
// 避免 VACUUM INTO 与数据库文件替换交错。恢复期间业务写请求被维护模式拒绝；
// 读请求可能落在替换窗口内而失败（一次性 5xx），这是「读放行」的已知代价。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// RestoreServiceDeps 是恢复服务依赖（字段较多，避免长参数列表）。
type RestoreServiceDeps struct {
	// Backups 是备份服务（安全备份复用其 createLocked；同时提供 DB/路径/日志依赖）。
	Backups *BackupService
	// Guard 是维护模式标志（与路由中间件共享同一实例）。
	Guard *MaintenanceGuard
	// Logger 是 slog 日志器；nil 时使用默认 logger。
	Logger *slog.Logger
	// OnMaintenanceEntered 是测试钩子：维护标志已置位、数据尚未改动时调用。
	// 生产路径传 nil。
	OnMaintenanceEntered func()
}

// RestoreService 执行「备份 → 校验 → 替换 → 数据检查」的完整恢复流程。
type RestoreService struct {
	backups              *BackupService
	guard                *MaintenanceGuard
	logger               *slog.Logger
	onMaintenanceEntered func()
}

// NewRestoreService 构造恢复服务。
func NewRestoreService(deps RestoreServiceDeps) *RestoreService {
	guard := deps.Guard
	if guard == nil {
		guard = NewMaintenanceGuard()
	}
	return &RestoreService{
		backups:              deps.Backups,
		guard:                guard,
		logger:               orDefaultLogger(deps.Logger),
		onMaintenanceEntered: deps.OnMaintenanceEntered,
	}
}

// RestoreResult 是一次成功恢复的结果（POST /backups/:id/restore 的 data）。
type RestoreResult struct {
	// Restored 是被恢复的备份文件名。
	Restored string
	// SafetyBackup 是恢复前自动生成的安全备份文件名（08-DEPLOYMENT.md:78）。
	SafetyBackup string
	// FileCount 是从备份中恢复的文件条目数（数据库快照 + uploads + config）。
	FileCount int
	// UploadsReplaced 表示备份包含 uploads 且已替换现有目录。
	UploadsReplaced bool
	// ConfigReplaced 表示备份包含 config.yaml 且已替换现有配置。
	ConfigReplaced bool
}

// Restore 恢复指定备份（name 为 backup-YYYYMMDD-HHMMSS.zip）。
//
// 成功返回恢复结果；失败返回 *BizError（400/404/422/500），维护标志必然已清除。
func (s *RestoreService) Restore(ctx context.Context, name string) (*RestoreResult, error) {
	if s.backups == nil || s.backups.db == nil {
		return nil, Internal("恢复服务未正确装配")
	}

	// 整个恢复过程与备份（手动/自动）串行。
	s.backups.mu.Lock()
	defer s.backups.mu.Unlock()

	archivePath, err := s.resolveBackup(name)
	if err != nil {
		return nil, err
	}

	// 维护模式：任何失败（含 panic）都由 defer 清除标志。
	s.guard.Enter("数据恢复进行中")
	defer s.guard.Exit()
	if s.onMaintenanceEntered != nil {
		s.onMaintenanceEntered()
	}

	// 步骤 3：先对当前数据做安全备份，再触碰任何文件。
	safety, err := s.backups.createLocked(ctx, BackupTriggerPreRestore)
	if err != nil {
		return nil, Internal("恢复前安全备份失败，已取消恢复（原数据未受影响）")
	}

	// 步骤 4：校验并解压目标备份到 staging（不改动任何现有数据）。
	staging, err := s.stageRestore(archivePath)
	if err != nil {
		return nil, err
	}
	defer staging.cleanup()

	// 步骤 5：关闭连接 → 替换文件 → 重开 → 数据检查。
	if err := s.swap(staging); err != nil {
		return nil, err
	}

	result := &RestoreResult{
		Restored:        name,
		SafetyBackup:    safety.Info.Name,
		FileCount:       staging.fileCount,
		UploadsReplaced: staging.hasUploads,
		ConfigReplaced:  staging.hasConfig,
	}
	// 步骤 6：审计在事务之外写入（此时连接池已指向恢复后的数据库）。
	s.writeRestoreLog(ctx, result)
	s.logger.Info("数据恢复完成",
		"restored", name, "safety_backup", safety.Info.Name,
		"file_count", staging.fileCount, "uploads_replaced", staging.hasUploads,
		"config_replaced", staging.hasConfig)
	return result, nil
}

// resolveBackup 校验 :id 并把备份文件名解析为磁盘路径。
//
// 只接受 backup-YYYYMMDD-HHMMSS(.zip) 命名：既防目录穿越，也保证列表接口里的
// 每个条目都能被恢复。
func (s *RestoreService) resolveBackup(name string) (string, error) {
	if !backupNamePattern.MatchString(name) {
		return "", BadRequest("备份标识非法（应为 backup-YYYYMMDD-HHMMSS.zip）")
	}
	path := filepath.Join(s.backups.backupDir, name)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", NotFound(CodeNotFound, "备份不存在")
	}
	if err != nil {
		return "", Internal("读取备份文件失败")
	}
	if !info.Mode().IsRegular() {
		return "", BadRequest("备份路径不是普通文件")
	}
	return path, nil
}

// writeRestoreLog 在事务之外写审计日志（action=restore）。
//
// 此时连接池已指向恢复后的数据库，日志落在新库中；日志写失败只记错误，
// 不影响已完成的恢复（恢复结果已在响应中返回）。
func (s *RestoreService) writeRestoreLog(ctx context.Context, result *RestoreResult) {
	if s.backups.logs == nil {
		return
	}
	content := fmt.Sprintf("恢复备份 %s（恢复前安全备份 %s，%d 个文件）",
		result.Restored, result.SafetyBackup, result.FileCount)
	if err := s.backups.logs.WriteLog(ctx, "restore", "backup", 0, content); err != nil {
		s.logger.Error("写入恢复审计日志失败", "restored", result.Restored, "err", err)
	}
}
