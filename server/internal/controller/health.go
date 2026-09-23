package controller

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// healthPingTimeout 是 /health 探测数据库的超时时间。
const healthPingTimeout = 2 * time.Second

// Health 返回 GET /health 的处理器：免认证，200 表示服务正常
// （04-API.md:257-263，供启动验收与排障使用）。
//
// 数据库不可用时写入“数据库错误”日志并返回 500 信封（内部细节不外泄）。
func Health(db *gorm.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := pingDB(c.Request.Context(), db)
		if err != nil {
			logger.Error("数据库错误", "phase", "health_ping", "err", err)
			FailWith(c, http.StatusInternalServerError, service.CodeInternal, "服务器内部错误")
			return
		}
		Success(c, gin.H{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	}
}

func pingDB(parent context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, healthPingTimeout)
	defer cancel()
	return sqlDB.PingContext(ctx)
}
