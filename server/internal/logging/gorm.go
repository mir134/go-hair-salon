package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger 把 GORM 的告警/错误输出路由到 slog：
//   - 只记录错误（忽略 record not found 等预期查询结果）；
//   - 通过 ParamsFilter 丢弃 SQL 参数，防止手机号/密码哈希等敏感参数进入日志
//     （08-DEPLOYMENT.md:89-92）。
func GormLogger(logger *slog.Logger) gormlogger.Interface {
	return &gormLogger{logger: logger}
}

type gormLogger struct {
	logger *slog.Logger
}

// LogMode 固定输出级别：仅错误。
func (g *gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return g }

// Info 不输出（级别固定为错误）。
func (g *gormLogger) Info(context.Context, string, ...any) {}

// Warn 不输出（级别固定为错误）。
func (g *gormLogger) Warn(context.Context, string, ...any) {}

// Error 记录 GORM 主动上报的错误。
func (g *gormLogger) Error(_ context.Context, msg string, args ...any) {
	g.logger.Error("数据库错误", "detail", fmt.Sprintf(msg, args...))
}

// Trace 记录失败的 SQL（错误非空且不是 record not found）。
func (g *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	sqlText, rows := fc()
	g.logger.Error("数据库错误",
		"err", err,
		"rows", rows,
		"elapsed_ms", time.Since(begin).Milliseconds(),
		"sql", sqlText,
	)
}

// ParamsFilter 返回不带参数的 SQL（参数置空），日志中只保留占位符。
func (g *gormLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}
