// Package main 是理发店客户管理系统的服务端入口（单体服务）。
//
// 启动流程：加载 config.yaml → 创建运行期目录 → 初始化 slog（控制台 + LOG_DIR 按天文件）
// → 打开 SQLite（WAL + busy_timeout）→ AutoMigrate + settings 播种 → 启动 HTTP 服务。
// 收到 Ctrl+C / SIGTERM 时优雅关闭（08-DEPLOYMENT.md:88-94）。
// 分层边界固定为 controller -> service -> repository -> model（02-AGENTS.md:15-26），
// 禁止在此处编写业务逻辑。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/config"
	"github.com/mir134/go-hair-salon/server/internal/logging"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// shutdownTimeout 是优雅关闭的最长等待时间。
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[hair-salon-server] 启动失败: %v\n", err)
		os.Exit(1)
	}
}

// run 完成启动初始化、开始监听并在退出信号后优雅关闭。
func run() error {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	logger, logCloser, err := logging.New(cfg.LogDir)
	if err != nil {
		return err
	}
	defer func() { _ = logCloser.Close() }()
	slog.SetDefault(logger)

	logger.Info("服务启动",
		"host", cfg.ServerHost, "port", cfg.ServerPort,
		"db_path", cfg.DBPath, "log_dir", cfg.LogDir,
		"upload_dir", cfg.UploadDir, "backup_dir", cfg.BackupDir,
		"backup_time", cfg.BackupTime)

	db, err := repository.Open(cfg.DBPath)
	if err != nil {
		logger.Error("数据库错误", "phase", "open", "err", err)
		return err
	}
	// 数据库日志统一走 slog；SQL 参数不落日志（logging.GormLogger）。
	db.Logger = logging.GormLogger(logger)
	sqlDB, err := db.DB()
	if err != nil {
		logger.Error("数据库错误", "phase", "connection_pool", "err", err)
		return err
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error("数据库关闭失败", "err", err)
		}
	}()

	if err := repository.Migrate(db); err != nil {
		logger.Error("数据库迁移失败", "err", err)
		return err
	}
	logger.Info("数据库迁移完成", "db_path", cfg.DBPath)
	if err := repository.EnsureDefaultSettings(db); err != nil {
		logger.Error("数据库错误", "phase", "settings_seed", "err", err)
		return err
	}
	logger.Info("settings 默认值已就绪")

	// 初始管理员播种（幂等）：users 为空时用 INITIAL_ADMIN_PASSWORD 创建 admin。
	userService := service.NewUserService(repository.NewUserRepository(db), repository.NewEmployeeRepository(db))
	created, err := userService.SeedAdmin(context.Background(), cfg.InitialAdminPassword)
	if err != nil {
		logger.Error("初始管理员播种失败", "err", err)
		return err
	}
	if created {
		logger.Info("初始管理员已创建", "username", service.DefaultAdminUsername)
	} else {
		logger.Info("用户已存在，跳过初始管理员播种")
	}

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort),
		Handler:           router.New(db, logger, router.Options{JWTSecret: cfg.JWTSecret}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	logger.Info("HTTP 服务已启动", "addr", server.Addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		if err != nil {
			logger.Error("HTTP 服务异常退出", "err", err)
			return err
		}
		return nil
	case sig := <-quit:
		logger.Info("收到退出信号，开始优雅关闭", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("HTTP 服务关闭失败", "err", err)
		return err
	}
	logger.Info("服务已停止")
	return nil
}
