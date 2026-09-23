package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// JWTAuth 解析 `Authorization: Bearer <token>` 并完成身份校验：
//   - 方案不是 Bearer、签名/有效期非法 → 401；
//   - 每次请求都从数据库重新加载用户，status=0（已停用）→ 401，停用后旧 token 立即失效；
//   - 角色以数据库实时值为准，不信任 token 中的 role 声明（04-API.md:60-68）；
//   - 鉴权成功后把用户与 operator id 注入 request context，供记账与 handler 使用。
//
// token 本身禁止写入日志（08-DEPLOYMENT.md:90）。
func JWTAuth(tokens *service.TokenService, users *service.UserService, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortUnauthorized(c, "未登录或凭证无效")
			return
		}
		claims, err := tokens.Parse(token)
		if err != nil {
			abortUnauthorized(c, "未登录或凭证无效")
			return
		}

		user, err := users.FindByID(c.Request.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				abortUnauthorized(c, "未登录或凭证无效")
				return
			}
			logger.Error("JWT 鉴权查询用户失败", "user_id", claims.UserID, "err", err)
			controller.FailWith(c, http.StatusInternalServerError, service.CodeInternal, "服务器内部错误")
			c.Abort()
			return
		}
		if user.Status != model.StatusEnabled {
			abortUnauthorized(c, "账号已被禁用")
			return
		}

		ctx := service.WithOperatorID(c.Request.Context(), user.ID)
		ctx = service.WithCurrentUser(ctx, user)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// bearerToken 从 Authorization 头提取 Bearer token；方案不匹配或 token 为空返回 false。
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

// abortUnauthorized 返回 401 统一信封并终止中间件链。
func abortUnauthorized(c *gin.Context, message string) {
	controller.FailWith(c, http.StatusUnauthorized, service.CodeUnauthorized, message)
	c.Abort()
}
