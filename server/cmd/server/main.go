// Package main 是理发店客户管理系统的服务端入口（单体服务）。
//
// 当前阶段：加载并校验 config.yaml，创建运行期目录，打开 SQLite（WAL + busy_timeout），
// 执行 AutoMigrate 并播种 settings 默认值。HTTP 服务与 slog 日志由后续任务接入；
// 分层边界固定为 controller -> service -> repository -> model（02-AGENTS.md:15-26）。
package main

import (
	"fmt"
	"os"

	"github.com/mir134/go-hair-salon/server/internal/config"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[hair-salon-server] 启动失败: %v\n", err)
		os.Exit(1)
	}
}

// run 完成启动初始化；返回错误时 main 以非 0 状态退出。
func run() error {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	db, err := repository.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := repository.Migrate(db); err != nil {
		return err
	}
	if err := repository.EnsureDefaultSettings(db); err != nil {
		return err
	}

	fmt.Printf("[hair-salon-server] 配置加载完成，监听 %s:%d\n", cfg.ServerHost, cfg.ServerPort)
	fmt.Printf("[hair-salon-server] DB_PATH=%s UPLOAD_DIR=%s BACKUP_DIR=%s LOG_DIR=%s\n",
		cfg.DBPath, cfg.UploadDir, cfg.BackupDir, cfg.LogDir)
	fmt.Printf("[hair-salon-server] 每日自动备份时间 %s\n", cfg.BackupTime)
	fmt.Printf("[hair-salon-server] 数据库就绪：%s（WAL/busy_timeout=5000，AutoMigrate 完成，settings 已播种）\n", cfg.DBPath)
	return nil
}
