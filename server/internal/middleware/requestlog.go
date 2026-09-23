package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger 记录每个 HTTP 请求：方法/路径/状态/耗时/ip/ua
// （08-DEPLOYMENT.md:88-94）。
//
// 只记录 URL.Path，不记录 query 与 body，避免关键词搜索（手机号/微信号）等
// 敏感内容落日志。
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("http_request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"ua", c.Request.UserAgent(),
		)
	}
}
