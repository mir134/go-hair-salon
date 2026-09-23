package service

// 每日自动备份定时器（todo 51；08-DEPLOYMENT.md:70-76）。
//
// 语义：
//  1. 启动补偿：Run 开始时若「最近一份备份缺失或已超过 CatchUpAfter（默认 24h）」，
//     立即执行一次备份；
//  2. 每日计划：每 Tick（默认 1 分钟）检查一次，到达当日 BACKUP_TIME（默认 23:00，config 可配）
//     且该计划时刻尚未处理时执行备份；同一天不重复；
//  3. 失败容忍：备份失败只写日志、不退出服务，并按 RetryAfter 限流重试；
//  4. 干净退出：ctx 取消后停止 time.Ticker 并返回，不遗留 goroutine。
//
// 文档冲突决议（plan todo 51）：06-BUSINESS-RULES.md:33「MVP 无定时任务」仅约束挂单
// ——挂单订单仍然永不自动过期/取消，本定时器只做备份，不触碰任何订单状态。
//
// 定时器与手动备份（POST /backups）共用同一个 BackupService 实例：
// 其内部互斥锁保证 VACUUM INTO/打包/保留清理串行执行。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// 定时器默认参数（可被 AutoBackupSchedulerDeps 覆盖，测试注入更小值）。
const (
	defaultAutoBackupTick    = time.Minute
	defaultAutoBackupCatchUp = 24 * time.Hour
	defaultAutoBackupRetry   = 10 * time.Minute
	autoBackupTimeLayout     = "15:04"
)

// AutoBackupSchedulerDeps 是自动备份定时器依赖（字段较多，避免长参数列表）。
type AutoBackupSchedulerDeps struct {
	// Backups 是备份服务（与手动备份共享同一实例；必填）。
	Backups *BackupService
	// BackupTime 是每日备份时间 HH:MM（config.BACKUP_TIME，默认 23:00）。
	BackupTime string
	// Logger 是 slog 日志器；nil 时使用默认 logger。
	Logger *slog.Logger
	// Now 是可注入时钟；nil 时使用 time.Now。
	Now func() time.Time
	// Tick 是检查周期；<=0 时使用 1 分钟。
	Tick time.Duration
	// CatchUpAfter 是启动补偿阈值；<=0 时使用 24h。
	CatchUpAfter time.Duration
	// RetryAfter 是失败重试的最小间隔；<=0 时使用 10 分钟。
	RetryAfter time.Duration
}

// AutoBackupScheduler 每日在 BACKUP_TIME 触发备份，并在启动时按需补备。
//
// 单 goroutine 使用：状态字段（attemptedFor/failedAt）只在 Run 所在 goroutine 读写，
// 因此无需加锁；调用方不得并发调用 Run/check。
type AutoBackupScheduler struct {
	backups      *BackupService
	hour         int
	minute       int
	logger       *slog.Logger
	now          func() time.Time
	tick         time.Duration
	catchUpAfter time.Duration
	retryAfter   time.Duration
	// attemptedFor 是已处理的最近计划时刻（同日不重复）。
	attemptedFor time.Time
	// failedAt 是最近一次失败时间（重试限流）。
	failedAt time.Time
}

// NewAutoBackupScheduler 解析 BACKUP_TIME 并构造定时器。
func NewAutoBackupScheduler(deps AutoBackupSchedulerDeps) (*AutoBackupScheduler, error) {
	if deps.Backups == nil {
		return nil, errors.New("自动备份缺少备份服务")
	}
	parsed, err := time.Parse(autoBackupTimeLayout, deps.BackupTime)
	if err != nil {
		return nil, fmt.Errorf("BACKUP_TIME 必须是 HH:MM（24 小时制）格式，当前为 %q", deps.BackupTime)
	}
	scheduler := &AutoBackupScheduler{
		backups:      deps.Backups,
		hour:         parsed.Hour(),
		minute:       parsed.Minute(),
		logger:       orDefaultLogger(deps.Logger),
		now:          orDefaultNow(deps.Now),
		tick:         deps.Tick,
		catchUpAfter: deps.CatchUpAfter,
		retryAfter:   deps.RetryAfter,
	}
	if scheduler.tick <= 0 {
		scheduler.tick = defaultAutoBackupTick
	}
	if scheduler.catchUpAfter <= 0 {
		scheduler.catchUpAfter = defaultAutoBackupCatchUp
	}
	if scheduler.retryAfter <= 0 {
		scheduler.retryAfter = defaultAutoBackupRetry
	}
	return scheduler, nil
}

// Run 阻塞运行定时器直到 ctx 取消；备份失败只记日志，不 panic、不退出进程。
func (s *AutoBackupScheduler) Run(ctx context.Context) {
	s.catchUp(ctx)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("自动备份定时器已停止")
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

// catchUp 执行启动补偿：最近备份缺失或超过 CatchUpAfter 时立即备份一次。
//
// 与「每日计划」相互独立：补偿成功不会占用当日计划（当晚 BACKUP_TIME 仍会再备份一次）。
func (s *AutoBackupScheduler) catchUp(ctx context.Context) {
	latest, ok := s.backups.LatestBackupTime()
	now := s.now()
	if ok && now.Sub(latest) < s.catchUpAfter {
		s.logger.Info("最近备份未超过间隔上限，跳过启动补偿", "latest", latest, "interval", s.catchUpAfter)
		return
	}
	s.logger.Info("启动补偿：最近备份缺失或已过期，立即执行一次备份",
		"has_backup", ok, "catch_up_after", s.catchUpAfter)
	_ = s.run(ctx, "startup-catch-up")
}

// check 处理一次周期检查：到达计划时刻且尚未处理时执行备份。
//
// 若计划时刻之前已有更新的备份（手动备份或启动补偿已覆盖），标记为已处理并跳过。
func (s *AutoBackupScheduler) check(ctx context.Context) {
	now := s.now()
	scheduled := s.latestFireTime(now)
	if s.attemptedFor.Equal(scheduled) {
		return
	}
	if latest, ok := s.backups.LatestBackupTime(); ok && !latest.Before(scheduled) {
		s.attemptedFor = scheduled
		return
	}
	if !s.failedAt.IsZero() && now.Sub(s.failedAt) < s.retryAfter {
		return
	}
	if s.run(ctx, "scheduled") {
		s.attemptedFor = scheduled
	}
}

// run 执行一次自动备份：失败只记日志并记录失败时间（服务不中断），成功返回 true。
func (s *AutoBackupScheduler) run(ctx context.Context, reason string) bool {
	result, err := s.backups.Create(ctx, BackupTriggerAutomatic)
	if err != nil {
		s.failedAt = s.now()
		s.logger.Error("自动备份失败", "reason", reason, "err", err)
		return false
	}
	s.logger.Info("自动备份完成", "reason", reason, "name", result.Info.Name,
		"size_bytes", result.Info.SizeBytes, "pruned", len(result.Pruned))
	return true
}

// latestFireTime 返回最近的计划时刻：当日 BACKUP_TIME 已过则为今天，否则为昨天。
func (s *AutoBackupScheduler) latestFireTime(now time.Time) time.Time {
	today := time.Date(now.Year(), now.Month(), now.Day(), s.hour, s.minute, 0, 0, now.Location())
	if !now.Before(today) {
		return today
	}
	return today.AddDate(0, 0, -1)
}
