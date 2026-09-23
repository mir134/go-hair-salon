package controller

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// UserAdminController 提供用户管理 API（D6 决议：无 04 契约的最小化新增）：
// GET /users、POST /users、PUT /users/:id，全部仅 admin（路由中间件保证）。
//
// 硬规则：
//   - 无物理删除：停用走 users.status=0（06 §11:120-125）；
//   - 停用后旧 token 立即失效——JWTAuth 每次请求实时查库校验 status（middleware/auth.go）；
//   - 审计日志在业务写操作返回后、事务之外写入（见 service.WriteLog），内容不含任何密码。
type UserAdminController struct {
	users *service.UserService
	logs  *service.OperationLogService
}

// NewUserAdminController 构造用户管理控制器。
func NewUserAdminController(users *service.UserService, logs *service.OperationLogService) *UserAdminController {
	return &UserAdminController{users: users, logs: logs}
}

// userCreateRequest 是 POST /users 请求体：employee_id 可选（关联员工）。
type userCreateRequest struct {
	Username   string `json:"username"`
	Role       string `json:"role"`
	Password   string `json:"password"`
	EmployeeID *int64 `json:"employee_id"`
}

// userUpdateRequest 是 PUT /users/:id 请求体：
//
// status 用指针区分「未提供」与「显式停用（0）」；password 非空表示重置密码。
type userUpdateRequest struct {
	Status   *int   `json:"status"`
	Password string `json:"password"`
}

// List 处理 GET /api/v1/users（仅 admin）：含已停用用户；
// ?status=0|1 可选过滤（非法值宽松回退，不得 500）。响应复用 UserView（字段白名单，绝不含密码哈希）。
func (h *UserAdminController) List(c *gin.Context) {
	users, err := h.users.List(c.Request.Context(), parseStatusQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	views := make([]UserView, 0, len(users))
	for i := range users {
		views = append(views, newUserView(&users[i]))
	}
	Success(c, views)
}

// Create 处理 POST /api/v1/users（仅 admin）：
// username/role（仅 admin|staff）/password 必填，employee_id 可选且必须存在；重复 username → 409。
func (h *UserAdminController) Create(c *gin.Context) {
	var req userCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	user, err := h.users.CreateUser(c.Request.Context(), service.CreateUserInput{
		Username:   req.Username,
		Password:   req.Password,
		Role:       req.Role,
		EmployeeID: req.EmployeeID,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "user_create", user.ID, fmt.Sprintf("新增用户 %s（角色 %s）", user.Username, user.Role))
	Created(c, newUserView(user))
}

// Update 处理 PUT /api/v1/users/:id（仅 admin）：
// status 停用（0）/启用（1）与/或 password 重置密码，至少提供一项；用户不存在 → 404。
func (h *UserAdminController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req userUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	updated, err := h.users.UpdateUser(c.Request.Context(), id, service.UpdateUserInput{
		Status:   req.Status,
		Password: req.Password,
	})
	if err != nil {
		Fail(c, err)
		return
	}
	if req.Status != nil {
		if *req.Status == model.StatusDisabled {
			h.writeLog(c, "user_disable", updated.ID, fmt.Sprintf("停用用户 %s", updated.Username))
		} else {
			h.writeLog(c, "user_enable", updated.ID, fmt.Sprintf("启用用户 %s", updated.Username))
		}
	}
	if req.Password != "" {
		h.writeLog(c, "user_password_reset", updated.ID, fmt.Sprintf("重置用户 %s 的密码（提示尽快修改）", updated.Username))
	}
	Success(c, newUserView(updated))
}

// writeLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）；
// content 只含用户名与动作，绝不含密码明文。
func (h *UserAdminController) writeLog(c *gin.Context, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, "user", targetID, content); err != nil {
		slog.Default().Error("写入用户管理审计日志失败", "action", action, "err", err)
	}
}
