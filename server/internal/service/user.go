package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// DefaultAdminUsername 是首次启动播种的初始管理员登录名。
const DefaultAdminUsername = "admin"

// UserService 提供用户账号管理：初始管理员播种、创建、查询、改密/重置密码、停用。
//
// 硬规则：密码只以 bcrypt 哈希落库（禁止明文、禁止写日志）；
// 用户停用只改 status，不物理删除（06-BUSINESS-RULES.md:120-125）。
type UserService struct {
	users     *repository.UserRepository
	employees *repository.EmployeeRepository
}

// NewUserService 构造用户服务；employees 用于校验可选的员工关联（users.employee_id）。
func NewUserService(users *repository.UserRepository, employees *repository.EmployeeRepository) *UserService {
	return &UserService{users: users, employees: employees}
}

// CreateUserInput 是创建用户的输入。
type CreateUserInput struct {
	Username   string
	Password   string
	Role       string
	EmployeeID *int64
}

// SeedAdmin 在 users 表为空时播种初始管理员（配置文件 INITIAL_ADMIN_PASSWORD）。
//
// 幂等：只要表中已存在任一用户就直接返回 false，不新增、不重置任何密码，
// 因此重复启动（含升级重启）不会覆盖管理员已修改过的密码。
func (s *UserService) SeedAdmin(ctx context.Context, initialPassword string) (bool, error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("统计用户数失败: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	if strings.TrimSpace(initialPassword) == "" {
		return false, Validation("初始管理员密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("生成初始管理员密码哈希失败: %w", err)
	}

	admin := &model.User{
		Username:     DefaultAdminUsername,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
		Status:       model.StatusEnabled,
	}
	if err := s.users.Create(ctx, admin); err != nil {
		// 并发启动竞态：另一进程可能已抢先播种，确认表非空后按幂等处理。
		if n, countErr := s.users.Count(ctx); countErr == nil && n > 0 {
			return false, nil
		}
		return false, fmt.Errorf("创建初始管理员失败: %w", err)
	}
	return true, nil
}

// CreateUser 创建用户（员工账号由管理员创建）；用户名重复返回 409。
//
// 校验顺序：用户名/角色/员工关联 → 密码哈希（bcrypt 昂贵，放在最后）。
// 密码长度上限 72 字节（bcrypt 限制），超限返回 400 而非 500。
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, BadRequest("用户名不能为空")
	}
	if utf8.RuneCountInString(username) > 64 {
		return nil, BadRequest("用户名不能超过 64 个字符")
	}
	if in.Role != model.RoleAdmin && in.Role != model.RoleStaff {
		return nil, BadRequest("角色不合法")
	}
	if err := s.ensureEmployeeExists(ctx, in.EmployeeID); err != nil {
		return nil, err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		Role:         in.Role,
		EmployeeID:   in.EmployeeID,
		Status:       model.StatusEnabled,
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, Conflict("用户名已存在")
		}
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

// List 返回用户列表（含已停用，按 id 升序）；status 非 nil 时按状态过滤。
func (s *UserService) List(ctx context.Context, status *int) ([]model.User, error) {
	users, err := s.users.List(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}
	return users, nil
}

// FindByUsername 按登录名查询用户；不存在时返回包装 repository.ErrNotFound 的错误。
func (s *UserService) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	return s.users.FindByUsername(ctx, username)
}

// FindByID 按主键查询用户；不存在时返回包装 repository.ErrNotFound 的错误。
func (s *UserService) FindByID(ctx context.Context, userID int64) (*model.User, error) {
	return s.users.FindByID(ctx, userID)
}

// ChangeOwnPassword 修改自己的密码：先校验原密码，再写入新哈希。
func (s *UserService) ChangeOwnPassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Unauthorized("登录状态已失效，请重新登录")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return Validation("原密码不正确")
	}
	return s.setPassword(ctx, userID, newPassword)
}

// ResetPassword 重置他人密码（仅管理员入口调用，权限由 RBAC 中间件保证）。
func (s *UserService) ResetPassword(ctx context.Context, targetUserID int64, newPassword string) error {
	if _, err := s.users.FindByID(ctx, targetUserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "用户不存在")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}
	return s.setPassword(ctx, targetUserID, newPassword)
}

// DisableUser 停用用户（status=0，保留账号记录，禁止物理删除）。
func (s *UserService) DisableUser(ctx context.Context, targetUserID int64) error {
	if err := s.users.UpdateStatus(ctx, targetUserID, model.StatusDisabled); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "用户不存在")
		}
		return fmt.Errorf("停用用户失败: %w", err)
	}
	return nil
}

// UpdateUserInput 是 PUT /users/:id 的管理输入（D6 决议：status 停用/启用 + 重置密码）。
type UpdateUserInput struct {
	Status   *int
	Password string
}

// UpdateUser 管理入口修改用户（仅 admin 入口调用，权限由 RBAC 中间件保证）：
//   - Status：停用/启用（只改 status，保留账号记录，禁止物理删除，06 §11:120-125）；
//   - Password：重置密码（只存 bcrypt 哈希）；重置后应由前端提示用户尽快自行修改。
//
// 两个字段至少提供一个，否则 400；写入为单条 UPDATE（不会出现只改一半的中间态）。
func (s *UserService) UpdateUser(ctx context.Context, targetUserID int64, in UpdateUserInput) (*model.User, error) {
	if in.Status == nil && in.Password == "" {
		return nil, BadRequest("没有需要更新的字段")
	}
	if _, err := s.users.FindByID(ctx, targetUserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "用户不存在")
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if in.Status != nil {
		if err := validateStatusValue(*in.Status); err != nil {
			return nil, err
		}
	}
	passwordHash := ""
	if in.Password != "" {
		hash, err := hashPassword(in.Password)
		if err != nil {
			return nil, err
		}
		passwordHash = hash
	}
	if err := s.users.UpdateAdminFields(ctx, targetUserID, in.Status, passwordHash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "用户不存在")
		}
		return nil, fmt.Errorf("更新用户失败: %w", err)
	}
	updated, err := s.users.FindByID(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return updated, nil
}

// ensureEmployeeExists 校验可选关联员工：nil 表示不关联；提供时必须存在（users.employee_id）。
func (s *UserService) ensureEmployeeExists(ctx context.Context, employeeID *int64) error {
	if employeeID == nil {
		return nil
	}
	if *employeeID <= 0 {
		return BadRequest("员工 id 不合法")
	}
	if _, err := s.employees.FindByID(ctx, *employeeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return BadRequest("关联员工不存在")
		}
		return fmt.Errorf("查询员工失败: %w", err)
	}
	return nil
}

// hashPassword 校验密码并生成 bcrypt 哈希。
//
//   - 空（含纯空白）→ 400 密码不能为空；
//   - 超过 72 字节 → 400（bcrypt 上限，避免生成哈希失败被误报 500）。
//
// 明文仅存在于本函数栈上，禁止写日志。
func hashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", BadRequest("密码不能为空")
	}
	if len(password) > 72 {
		return "", BadRequest("密码长度不能超过 72 字节")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(hash), nil
}

// setPassword 校验新密码后写入 bcrypt 哈希。
func (s *UserService) setPassword(ctx context.Context, userID int64, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, userID, hash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "用户不存在")
		}
		return fmt.Errorf("更新密码失败: %w", err)
	}
	return nil
}
