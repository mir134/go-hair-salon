// Package main 是理发店客户管理系统的服务端入口（单体服务）。
//
// 当前处于 P0 工程骨架阶段：加载并校验 config.yaml、打印启动信息后正常退出。
// 数据库、日志与 HTTP 服务由后续任务接入；分层边界固定为
// controller -> service -> repository -> model（02-AGENTS.md:15-26）。
package main

import (
	"fmt"
	"os"

	"github.com/mir134/go-hair-salon/server/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[hair-salon-server] 启动失败: %v\n", err)
		os.Exit(1)
	}
}

// run 加载配置并打印启动信息；返回错误时 main 以非 0 状态退出。
func run() error {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		return err
	}
	fmt.Printf("[hair-salon-server] 配置加载完成，监听 %s:%d\n", cfg.ServerHost, cfg.ServerPort)
	fmt.Printf("[hair-salon-server] DB_PATH=%s UPLOAD_DIR=%s BACKUP_DIR=%s LOG_DIR=%s\n",
		cfg.DBPath, cfg.UploadDir, cfg.BackupDir, cfg.LogDir)
	fmt.Printf("[hair-salon-server] 每日自动备份时间 %s\n", cfg.BackupTime)
	fmt.Println("[hair-salon-server] 工程骨架就绪：HTTP 服务、数据库与日志将在后续任务接入")
	return nil
}
