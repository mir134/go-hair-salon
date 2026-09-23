package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	users *repository.UserRepository
}

// NewUserService 构造用户服务。
func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
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
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, BadRequest("用户名不能为空")
	}
	if in.Password == "" {
		return nil, BadRequest("密码不能为空")
	}
	if in.Role != model.RoleAdmin && in.Role != model.RoleStaff {
		return nil, BadRequest("角色不合法")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("生成密码哈希失败: %w", err)
	}

	user := &model.User{
		Username:     username,
		PasswordHash: string(hash),
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

// setPassword 校验新密码非空后写入 bcrypt 哈希。
func (s *UserService) setPassword(ctx context.Context, userID int64, password string) error {
	if strings.TrimSpace(password) == "" {
		return BadRequest("密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	if err := s.users.UpdatePassword(ctx, userID, string(hash)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "用户不存在")
		}
		return fmt.Errorf("更新密码失败: %w", err)
	}
	return nil
}
