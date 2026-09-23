package controller_test

// TestAuth 是 todo 10 的验收测试（计划：`go test ./internal/controller -run TestAuth -v -count=1`）：
//   - 正确凭据 → 200 + token；/auth/me 返回身份与角色；
//   - 错误密码 → 401 且写 login_failed；重复 3 次 → 3 条 login_failed；
//   - 禁用用户（status=0）→ 403；
//   - 成功/失败均写 operation_logs（04-API.md:46-58）；
//   - token 不含密码哈希/明文，日志不含 token（08-DEPLOYMENT.md:90）。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const authTestSecret = "unit-test-jwt-secret"

// authTestEnv 是装配完整路由的测试环境（真实迁移库 + 内存日志）。
type authTestEnv struct {
	engine *gin.Engine
	db     *gorm.DB
	users  *service.UserService
	logBuf *bytes.Buffer
	admin  *model.User
}

func newAuthEnv(t *testing.T) *authTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := repository.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}

	logBuf := &bytes.Buffer{}
	engine := router.New(db, slog.New(slog.NewTextHandler(logBuf, nil)), router.Options{JWTSecret: authTestSecret})
	env := &authTestEnv{
		engine: engine,
		db:     db,
		users:  service.NewUserService(repository.NewUserRepository(db)),
		logBuf: logBuf,
	}
	ctx := context.Background()
	if created, err := env.users.SeedAdmin(ctx, "Admin-Pwd-1"); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	admin, err := env.users.FindByUsername(ctx, service.DefaultAdminUsername)
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	env.admin = admin
	return env
}

// do 发起一次 HTTP 请求。
func (e *authTestEnv) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// decodeEnvelope 解码响应体为统一信封（envelope 类型见 response_test.go）。
func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// loginData 是登录成功的 data 结构。
type loginData struct {
	Token     string   `json:"token"`
	ExpiresAt string   `json:"expires_at"`
	User      userData `json:"user"`
}

// userData 是用户信息 DTO（不含密码哈希）。
type userData struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	EmployeeID *int64 `json:"employee_id"`
	Status     int    `json:"status"`
}

// decodeJWTPayload 解码 JWT 负载段用于断言敏感信息不外泄。
func decodeJWTPayload(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token 格式非法: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("解码 token 负载失败: %v", err)
	}
	return string(payload)
}

func TestAuth(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	// Given: 一个启用员工与一个已禁用员工。
	if _, err := env.users.CreateUser(ctx, service.CreateUserInput{Username: "staff1", Password: "Staff-Pwd-1", Role: model.RoleStaff}); err != nil {
		t.Fatalf("CreateUser(staff1): %v", err)
	}
	disabled, err := env.users.CreateUser(ctx, service.CreateUserInput{Username: "gone", Password: "Gone-Pwd-1", Role: model.RoleStaff})
	if err != nil {
		t.Fatalf("CreateUser(gone): %v", err)
	}
	if err := env.users.DisableUser(ctx, disabled.ID); err != nil {
		t.Fatalf("DisableUser(gone): %v", err)
	}

	// When: 正确凭据登录。
	w := env.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"Admin-Pwd-1"}`, nil)

	// Then: 200 + 有效 token + 用户角色；expires_at 约 24h。
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	envl := decodeEnvelope(t, w)
	if envl.Code != service.CodeOK || envl.Message != "success" {
		t.Errorf("login envelope = %+v, want code=0 message=success", envl)
	}
	var login loginData
	decodeData(t, envl, &login)
	if login.Token == "" {
		t.Fatal("login data.token 为空")
	}
	if login.User.Role != model.RoleAdmin || login.User.Username != "admin" || login.User.Status != model.StatusEnabled {
		t.Errorf("login data.user = %+v, want admin/role=admin/status=1", login.User)
	}
	expiresAt, err := time.Parse(time.RFC3339, login.ExpiresAt)
	if err != nil {
		t.Fatalf("expires_at 不是 RFC3339: %v (%q)", err, login.ExpiresAt)
	}
	if delta := time.Until(expiresAt); delta < 23*time.Hour || delta > 25*time.Hour {
		t.Errorf("expires_at 距现在 = %v, want 约 24h", delta)
	}

	// Then: 成功登录写 action=login，operator 为本人；token 不含密码哈希/明文。
	var loginLog model.OperationLog
	if err := env.db.Where("action = ?", "login").First(&loginLog).Error; err != nil {
		t.Fatalf("查询 login 日志: %v", err)
	}
	if loginLog.OperatorID == nil || *loginLog.OperatorID != env.admin.ID {
		t.Errorf("login 日志 operator_id = %v, want %d", loginLog.OperatorID, env.admin.ID)
	}
	payload := decodeJWTPayload(t, login.Token)
	if strings.Contains(payload, "$2a$") || strings.Contains(payload, "Admin-Pwd-1") {
		t.Errorf("JWT 负载泄漏密码信息: %s", payload)
	}

	// When: 带 token 请求 /auth/me。
	w = env.do(http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Authorization": "Bearer " + login.Token})

	// Then: 返回当前用户身份与角色。
	if w.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	envl = decodeEnvelope(t, w)
	var me userData
	decodeData(t, envl, &me)
	if me.Username != "admin" || me.Role != model.RoleAdmin || me.ID != env.admin.ID {
		t.Errorf("me data = %+v, want id=%d admin/admin", me, env.admin.ID)
	}

	// When: 连续 3 次错误密码。
	for i := 0; i < 3; i++ {
		w = env.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"Wrong-Pwd!"}`, nil)
		// Then: 每次 401 + 40100，data=null。
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("错误密码第 %d 次 status = %d, want 401 (body=%s)", i+1, w.Code, w.Body.String())
		}
		envl = decodeEnvelope(t, w)
		if envl.Code != service.CodeUnauthorized || string(envl.Data) != "null" {
			t.Errorf("错误密码信封 = %+v, want code=40100 data=null", envl)
		}
	}

	// Then: 恰好 3 条 login_failed，且内容不含密码、operator_id 为空。
	var failed int64
	if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "login_failed").Count(&failed).Error; err != nil {
		t.Fatalf("count login_failed: %v", err)
	}
	if failed != 3 {
		t.Errorf("login_failed 行数 = %d, want 3", failed)
	}
	var failedRows []model.OperationLog
	if err := env.db.Where("action = ?", "login_failed").Find(&failedRows).Error; err != nil {
		t.Fatalf("查询 login_failed: %v", err)
	}
	for _, row := range failedRows {
		if strings.Contains(row.Content, "Wrong-Pwd!") {
			t.Errorf("审计日志写入明文密码: %q", row.Content)
		}
		if row.OperatorID != nil {
			t.Errorf("登录失败 operator_id = %v, want NULL", *row.OperatorID)
		}
	}

	// When: 禁用用户用正确密码登录。
	w = env.do(http.MethodPost, "/api/v1/auth/login", `{"username":"gone","password":"Gone-Pwd-1"}`, nil)

	// Then: 403（06-BUSINESS-RULES.md:81 禁用账号拒绝）。
	if w.Code != http.StatusForbidden {
		t.Fatalf("禁用用户登录 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	envl = decodeEnvelope(t, w)
	if envl.Code != service.CodeForbidden {
		t.Errorf("禁用用户登录 code = %d, want %d", envl.Code, service.CodeForbidden)
	}
	var failedAfter int64
	if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "login_failed").Count(&failedAfter).Error; err != nil {
		t.Fatalf("count login_failed: %v", err)
	}
	if failedAfter != 4 {
		t.Errorf("禁用登录后 login_failed 行数 = %d, want 4", failedAfter)
	}

	// When: 登出（无黑名单，仅写日志）。
	w = env.do(http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"Authorization": "Bearer " + login.Token})

	// Then: 200 信封 + action=logout 审计。
	if w.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var logoutLog model.OperationLog
	if err := env.db.Where("action = ?", "logout").First(&logoutLog).Error; err != nil {
		t.Fatalf("查询 logout 日志: %v", err)
	}
	if logoutLog.OperatorID == nil || *logoutLog.OperatorID != env.admin.ID {
		t.Errorf("logout 日志 operator_id = %v, want %d", logoutLog.OperatorID, env.admin.ID)
	}

	// Then: 日志缓冲（slog）中不出现 token 或密码明文（08-DEPLOYMENT.md:90）。
	logs := env.logBuf.String()
	if strings.Contains(logs, login.Token) || strings.Contains(logs, "Admin-Pwd-1") {
		t.Errorf("服务端日志泄漏 token/密码:\n%s", logs)
	}
}
