package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// RequireRole 是 RBAC 中间件：仅当已认证用户的角色命中 roles 时放行，否则 403。
//
// 角色取自已认证用户对象——它由 JWTAuth 在每一次请求中从数据库实时加载，
// 因此 token 中的 role 声明（或伪造声明）不产生任何权限效果（04-API.md:60-68）。
// 必须挂在 JWTAuth 之后；无当前用户时按未认证处理（401）。
//
// 路由装配约定（06-BUSINESS-RULES.md §7）：
//   - admin 分组：RequireRole(model.RoleAdmin) —— 订单取消、余额调整、退款、
//     删除客户、系统设置、操作日志、员工/用户管理、数据恢复、改价；
//   - staff 分组：RequireRole(model.RoleStaff)；
//   - both 分组：RequireRole(model.RoleAdmin, model.RoleStaff)。
//
// 后端是权限的最终边界，禁止仅靠前端隐藏入口（AGENTS.md 第 5 节）。
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		user, ok := service.CurrentUser(c.Request.Context())
		if !ok {
			abortUnauthorized(c, "未登录或凭证无效")
			return
		}
		if _, ok := allowed[user.Role]; !ok {
			controller.FailWith(c, http.StatusForbidden, service.CodeForbidden, "无权访问该接口")
			c.Abort()
			return
		}
		c.Next()
	}
}
