package controller

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// AuthController 提供 /auth 登录、身份与登出接口（04-API.md:46-58）。
type AuthController struct {
	auth *service.AuthService
	logs *service.OperationLogService
}

// NewAuthController 构造认证控制器。
func NewAuthController(auth *service.AuthService, logs *service.OperationLogService) *AuthController {
	return &AuthController{auth: auth, logs: logs}
}

// LoginRequest 是 POST /auth/login 请求体。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// UserView 是用户信息 DTO：字段白名单，绝不包含 password_hash。
type UserView struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	EmployeeID *int64 `json:"employee_id"`
	Status     int    `json:"status"`
}

// Login 处理 POST /api/v1/auth/login：
// 成功返回 data.token（24h JWT）与用户信息；失败按 service.BizError 渲染信封。
func (h *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求参数错误"))
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		Fail(c, service.BadRequest("用户名和密码不能为空"))
		return
	}

	result, err := h.auth.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, gin.H{
		"token":      result.Token,
		"expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
		"user":       newUserView(result.User),
	})
}

// Me 处理 GET /api/v1/auth/me：返回当前登录用户（JWT 中间件已查库并校验 status）。
func (h *AuthController) Me(c *gin.Context) {
	user, ok := service.CurrentUser(c.Request.Context())
	if !ok {
		Fail(c, service.Unauthorized("未登录或凭证无效"))
		return
	}
	Success(c, newUserView(user))
}

// Logout 处理 POST /api/v1/auth/logout：MVP 无黑名单（前端丢弃 token），仅写审计日志。
func (h *AuthController) Logout(c *gin.Context) {
	user, ok := service.CurrentUser(c.Request.Context())
	if !ok {
		Fail(c, service.Unauthorized("未登录或凭证无效"))
		return
	}
	if err := h.logs.WriteLog(c.Request.Context(), "logout", "user", user.ID,
		fmt.Sprintf("用户 %s 登出", user.Username)); err != nil {
		slog.Default().Error("写入登出审计日志失败", "action", "logout", "err", err)
	}
	Success(c, gin.H{})
}

// newUserView 把 model.User 转为脱敏 DTO。
func newUserView(user *model.User) UserView {
	return UserView{
		ID:         user.ID,
		Username:   user.Username,
		Role:       user.Role,
		EmployeeID: user.EmployeeID,
		Status:     user.Status,
	}
}
