package service_test

// TestAutoBackup 是 todo 51 的验收测试
// （计划：`go test ./internal/service -run TestAutoBackup -v -count=1`）：
//   - 进程内 time.Ticker 每日 BACKUP_TIME（默认 23:00，config 可配）触发，复用 todo 50 的备份服务与保留策略；
//   - 启动时若最近备份缺失或超过 24h → 立即补备（catch-up）；
//   - BACKUP_TIME 生效：未到点不备份、到点即备份、同一天不重复；
//   - 备份失败只写日志、不退出服务，问题修复后下一个计划点恢复；
//   - ctx 取消后 Run 干净返回（定时器 Stop、无 goroutine 泄漏）。
//
// 文档冲突决议（plan todo 51）：06-BUSINESS-RULES.md:33「MVP 无定时任务」只约束挂单
// （挂单仍不自动过期，本实现不触碰订单），每日自动备份按 08-DEPLOYMENT.md:72 强制实现。
//
// 时钟注入：假时钟让「跨 24h / 到点」以毫秒级真实 tick 断言，不等待真实时间。

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// autoBackupTick 是测试用检查周期（真实 ticker 周期，业务时间由假时钟控制）。
const autoBackupTick = 5 * time.Millisecond

// fakeClock 是并发安全的假时钟（scheduler 在独立 goroutine 读，测试主 goroutine 写）。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// newFakeClock 构造假时钟。
func newFakeClock(at time.Time) *fakeClock { return &fakeClock{now: at} }

// Now 返回当前假时间。
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set 设置假时间。
func (c *fakeClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// autoBackupEnv 是 todo 51 测试环境：todo 50 环境 + 假时钟 + 内存日志。
type autoBackupEnv struct {
	*backupTestEnv
	clock  *fakeClock
	logBuf *bytes.Buffer
	logger *slog.Logger
}

// newAutoBackupEnv 装配假时钟环境（备份服务与定时器共享同一时钟）。
func newAutoBackupEnv(t *testing.T, at time.Time) *autoBackupEnv {
	t.Helper()
	clock := newFakeClock(at)
	logBuf := &bytes.Buffer{}
	return &autoBackupEnv{
		backupTestEnv: newBackupTestEnvWithClock(t, clock.Now),
		clock:         clock,
		logBuf:        logBuf,
		logger:        slog.New(slog.NewTextHandler(logBuf, nil)),
	}
}

// at 构造本地时区的测试时刻。
func at(year int, month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(year, month, day, hour, minute, second, 0, time.Local)
}

// newScheduler 构造定时器（默认 Tick=5ms、CatchUpAfter=24h、RetryAfter=10min）。
func (e *autoBackupEnv) newScheduler(t *testing.T, backupTime string, mutate ...func(*service.AutoBackupSchedulerDeps)) *service.AutoBackupScheduler {
	t.Helper()
	deps := service.AutoBackupSchedulerDeps{
		Backups:      e.svc,
		BackupTime:   backupTime,
		Logger:       e.logger,
		Now:          e.clock.Now,
		Tick:         autoBackupTick,
		CatchUpAfter: 24 * time.Hour,
		RetryAfter:   10 * time.Minute,
	}
	for _, apply := range mutate {
		apply(&deps)
	}
	scheduler, err := service.NewAutoBackupScheduler(deps)
	if err != nil {
		t.Fatalf("NewAutoBackupScheduler(%s): %v", backupTime, err)
	}
	return scheduler
}

// startScheduler 在独立 goroutine 运行定时器，返回停止函数（等待干净退出）。
func startScheduler(t *testing.T, scheduler *service.AutoBackupScheduler) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	return func() {
		t.Helper()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Errorf("定时器未在 3s 内停止（Run 未返回）")
		}
	}
}

// waitFor 轮询条件直到成立或超时（真实 ticker 驱动，5ms 步进）。
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时（%s）：%s", timeout, what)
}

func TestAutoBackup(t *testing.T) {
	t.Run("启动补偿：最近备份超过 24h 时立即备份", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 3, 10, 0, 0))

		// Given: 一份 48 小时前的旧备份。
		old := env.createBackup(t, service.BackupTriggerAutomatic)

		// When: 时钟推进 48h 后启动定时器（BACKUP_TIME=23:00，尚未到点）。
		env.clock.Set(at(2026, 3, 5, 10, 0, 0))
		stop := startScheduler(t, env.newScheduler(t, "23:00"))
		defer stop()

		// Then: 立即补备 1 份，且到点前不再重复。
		waitFor(t, 3*time.Second, "启动补偿备份", func() bool { return countBackupZips(t, env.backupDir) == 2 })
		time.Sleep(80 * time.Millisecond)
		infos, err := env.svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(infos) != 2 {
			t.Fatalf("备份数 = %d, want 2（补偿后当日到点前不应再备份）", len(infos))
		}
		if infos[0].Name == old.Info.Name {
			t.Errorf("最新备份仍是旧文件 %s，启动补偿未产生新备份", old.Info.Name)
		}
		if !strings.Contains(infos[0].Name, "20260305-100000") {
			t.Errorf("补偿备份名 = %s, want 含 20260305-100000（假时钟时间）", infos[0].Name)
		}
		if logs := env.logBuf.String(); !strings.Contains(logs, "启动补偿") {
			t.Errorf("日志缺少启动补偿记录:\n%s", logs)
		}
	})

	t.Run("启动补偿：最近备份不足 24h 不重复备份", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 5, 10, 0, 0))
		recent := env.createBackup(t, service.BackupTriggerAutomatic)

		stop := startScheduler(t, env.newScheduler(t, "23:00"))
		defer stop()

		time.Sleep(120 * time.Millisecond)
		infos, err := env.svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(infos) != 1 || infos[0].Name != recent.Info.Name {
			t.Fatalf("备份 = %+v, want 仅最近一份 %s（不足 24h 不补备）", infos, recent.Info.Name)
		}
	})

	t.Run("BACKUP_TIME 生效：到点触发且同日不重复", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 5, 10, 0, 0))
		env.createBackup(t, service.BackupTriggerAutomatic)

		stop := startScheduler(t, env.newScheduler(t, "23:00"))
		defer stop()

		// 未到点：22:59 仍只有 1 份。
		env.clock.Set(at(2026, 3, 5, 22, 59, 0))
		time.Sleep(100 * time.Millisecond)
		if got := countBackupZips(t, env.backupDir); got != 1 {
			t.Fatalf("22:59 备份数 = %d, want 1（未到 BACKUP_TIME 不应备份）", got)
		}

		// 到点：23:00:01 触发第 2 份。
		env.clock.Set(at(2026, 3, 5, 23, 0, 1))
		waitFor(t, 3*time.Second, "到点备份", func() bool { return countBackupZips(t, env.backupDir) == 2 })

		// 同日更晚：23:05 不再重复。
		env.clock.Set(at(2026, 3, 5, 23, 5, 0))
		time.Sleep(100 * time.Millisecond)
		if got := countBackupZips(t, env.backupDir); got != 2 {
			t.Errorf("23:05 备份数 = %d, want 2（同一天不重复备份）", got)
		}

		// 次日到点：再触发第 3 份。
		env.clock.Set(at(2026, 3, 6, 23, 0, 1))
		waitFor(t, 3*time.Second, "次日到点备份", func() bool { return countBackupZips(t, env.backupDir) == 3 })
	})

	t.Run("备份失败：写日志、不退出服务，修复后恢复", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 5, 10, 0, 0))

		// Given: uploads 目录缺失（备份必然失败），且无任何备份（启动补偿会尝试一次）。
		if err := os.RemoveAll(env.uploadDir); err != nil {
			t.Fatalf("删除 uploads 目录: %v", err)
		}
		stop := startScheduler(t, env.newScheduler(t, "23:00"))
		defer stop()

		waitFor(t, 3*time.Second, "失败日志", func() bool {
			return strings.Contains(env.logBuf.String(), "自动备份失败")
		})
		if got := countBackupZips(t, env.backupDir); got != 0 {
			t.Fatalf("失败路径留下 %d 个备份, want 0", got)
		}

		// When: 修复 uploads 并推进到 BACKUP_TIME。
		if err := os.MkdirAll(env.uploadDir, 0o755); err != nil {
			t.Fatalf("重建 uploads 目录: %v", err)
		}
		env.clock.Set(at(2026, 3, 5, 23, 0, 1))

		// Then: 服务未退出，定时器在下一个计划点成功备份。
		waitFor(t, 3*time.Second, "修复后备份", func() bool { return countBackupZips(t, env.backupDir) == 1 })
	})

	t.Run("ctx 取消：定时器干净退出且无 goroutine 泄漏", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 5, 10, 0, 0))
		before := runtime.NumGoroutine()
		scheduler := env.newScheduler(t, "23:00")

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			scheduler.Run(ctx)
		}()
		waitFor(t, 3*time.Second, "启动补偿完成", func() bool { return countBackupZips(t, env.backupDir) == 1 })

		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Run 未在 ctx 取消后 3s 内返回")
		}
		waitFor(t, 2*time.Second, "goroutine 回落", func() bool { return runtime.NumGoroutine() <= before+1 })
		if logs := env.logBuf.String(); !strings.Contains(logs, "自动备份定时器已停止") {
			t.Errorf("缺少定时器停止日志:\n%s", logs)
		}
	})

	t.Run("BACKUP_TIME 非法：构造返回错误", func(t *testing.T) {
		env := newAutoBackupEnv(t, at(2026, 3, 5, 10, 0, 0))
		if _, err := service.NewAutoBackupScheduler(service.AutoBackupSchedulerDeps{
			Backups: env.svc, BackupTime: "25:90", Now: env.clock.Now,
		}); err == nil {
			t.Fatal("非法 BACKUP_TIME 应返回错误")
		}
	})
}
