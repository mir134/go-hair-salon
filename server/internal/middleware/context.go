package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// RequestContext 把请求审计信息（客户端 ip 与 User-Agent）注入 request context，
// 供记账服务（service.OperationLogService.WriteLog）提取后写入 operation_logs。
//
// 登录用户 id（operator）由 JWT 中间件在鉴权成功后通过 service.WithOperatorID 注入
// （todo 11），本中间件不解析身份。
func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := service.WithRequestMeta(c.Request.Context(), clientIP(c), c.Request.UserAgent())
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// clientIP 优先取 X-Forwarded-For 的第一个地址（客户端直连或经代理均适用），
// 否则回退到 RemoteAddr 解析结果。
func clientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	return c.ClientIP()
}
