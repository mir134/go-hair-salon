package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// dsnPragmas 是每个连接建立时必须执行的 SQLite PRAGMA（02-AGENTS.md:45-52）：
// WAL 提升并发读写、busy_timeout 避免瞬时 database is locked、foreign_keys 打开外键检查。
const dsnPragmas = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

// DSN 将数据库文件路径转换为 glebarez/sqlite 的 file: URI DSN。
func DSN(dbPath string) string {
	return "file:" + filepath.ToSlash(dbPath) + "?" + dsnPragmas
}

// Open 打开（必要时创建目录与文件）SQLite 数据库，返回全局单例 *gorm.DB。
//
// 硬规则：整个进程只允许一个连接池（02-AGENTS.md:45-63 禁止每请求创建连接），
// 因此连接数固定为 1；SQLite 同一时刻只有一个写者，由 busy_timeout 兜底等待。
func Open(dbPath string) (*gorm.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("DB_PATH 不能为空")
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据库目录 %s 失败: %w", dir, err)
		}
	}

	db, err := gorm.Open(sqlite.Open(DSN(dbPath)), &gorm.Config{
		// 时间统一 UTC 存储（02-AGENTS.md:39）。
		NowFunc: func() time.Time { return time.Now().UTC() },
		// 唯一约束冲突转换为 gorm.ErrDuplicatedKey（幂等逻辑依赖）。
		TranslateError: true,
		Logger:         newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s 失败: %w", dbPath, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	return db, nil
}

// OpenReadOnly 以只读方式打开任意 SQLite 数据库文件（不创建、不写入）。
//
// 用途：备份快照的完整性校验（service 层 todo 50）与恢复前的文件校验（todo 52）——
// mode=ro 确保校验动作绝不会改动被校验的文件（尤其不能把快照“修好”后再放行）。
// 返回的 *sql.DB 由调用方负责 Close。
func OpenReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("打开只读数据库 %s 失败: %w", path, err)
	}
	return db, nil
}

// newGormLogger 构造 GORM 日志器：只输出告警/错误，且 SQL 参数不内联，
// 避免把手机号等敏感参数写进日志（08-DEPLOYMENT.md:89-92）。
func newGormLogger() logger.Interface {
	return logger.New(log.New(os.Stderr, "[gorm] ", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
	})
}
