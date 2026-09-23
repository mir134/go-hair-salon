package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// Recovery 捕获下游 handler 的 panic：
//   - 向客户端返回 500 统一信封，响应体绝不包含 goroutine/stack trace（02-AGENTS.md:74）；
//   - 完整 panic 值与堆栈写入服务端日志（08-DEPLOYMENT.md:90 要求记录 HTTP 500）。
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			logger.Error("panic recovered",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()),
			)
			if !c.Writer.Written() {
				controller.FailWith(c, http.StatusInternalServerError, service.CodeInternal, "服务器内部错误")
			}
			c.Abort()
		}()
		c.Next()
	}
}
