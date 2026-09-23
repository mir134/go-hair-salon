package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// LoginResult 是登录成功的结果：24h JWT 与登录用户。
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      *model.User
}

// AuthService 负责登录校验与登录审计（04-API.md:46-58）。
//
// 审计在业务逻辑之外独立写入（本流程无业务事务），登录成功写 action=login、
// 失败写 action=login_failed；任何日志/审计内容都禁止包含密码或 token。
type AuthService struct {
	users  *UserService
	tokens *TokenService
	logs   *OperationLogService
}

// NewAuthService 构造认证服务。
func NewAuthService(users *UserService, tokens *TokenService, logs *OperationLogService) *AuthService {
	return &AuthService{users: users, tokens: tokens, logs: logs}
}

// Login 校验用户名密码并签发 JWT：
//   - 用户名不存在/密码错误 → 401（统一文案，不泄漏账号是否存在）；
//   - 账号已停用（status=0）→ 403；
//   - 先校验密码再校验状态，避免未通过密码校验者探测账号状态。
func (s *AuthService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	user, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			s.auditLoginFailure(ctx, username, 0, "用户不存在")
			return nil, Unauthorized("用户名或密码错误")
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.auditLoginFailure(ctx, username, user.ID, "密码错误")
		return nil, Unauthorized("用户名或密码错误")
	}
	if user.Status != model.StatusEnabled {
		s.auditLoginFailure(ctx, username, user.ID, "账号已禁用")
		return nil, Forbidden("账号已被禁用，请联系管理员")
	}

	token, expiresAt, err := s.tokens.Issue(user)
	if err != nil {
		return nil, fmt.Errorf("签发登录凭证失败: %w", err)
	}
	// operator 记为登录者本人；写审计失败不影响登录结果（仅记服务端错误日志）。
	auditCtx := WithOperatorID(ctx, user.ID)
	if err := s.logs.WriteLog(auditCtx, "login", "user", user.ID,
		fmt.Sprintf("用户 %s 登录成功", user.Username)); err != nil {
		slog.Default().Error("写入登录审计日志失败", "action", "login", "err", err)
	}
	return &LoginResult{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

// auditLoginFailure 写登录失败审计：operator_id 为空（未认证），内容不含密码。
func (s *AuthService) auditLoginFailure(ctx context.Context, username string, userID int64, reason string) {
	if err := s.logs.WriteLog(ctx, "login_failed", "user", userID,
		fmt.Sprintf("用户 %s 登录失败：%s", username, reason)); err != nil {
		slog.Default().Error("写入登录审计日志失败", "action", "login_failed", "err", err)
	}
}
