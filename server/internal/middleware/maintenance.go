package middleware

// 维护模式拦截（todo 52；08-DEPLOYMENT.md:78「执行恢复前：停止业务写入」）。
//
// 恢复期间（数据库文件正在被替换）业务写请求必须被拒绝，否则会出现
// 「订单成功但余额没扣」这类跨库撕裂；读请求放行（查询旧库或新库都不会破坏数据）。
// 权限边界不受影响：本中间件只回答「现在能不能写」，不回答「谁能不能写」。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// Maintenance 在维护模式激活时拒绝业务写请求（POST/PUT/PATCH/DELETE），
// 返回 503 + code=50300 统一信封；读请求（GET/HEAD/OPTIONS）放行。
//
// 注册顺序必须早于 JWTAuth：维护期写请求在触碰数据库之前就被拒绝
// （恢复过程中连接池会被短暂关闭）。
func Maintenance(guard *service.MaintenanceGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		if guard.Active() && isBusinessWrite(c.Request.Method) {
			controller.FailWith(c, http.StatusServiceUnavailable, service.CodeServiceUnavailable, guard.Message())
			c.Abort()
			return
		}
		c.Next()
	}
}

// isBusinessWrite 判定请求是否为业务写方法（GET/HEAD/OPTIONS 之外的 HTTP 方法）。
func isBusinessWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
