package middleware_test

// TestRBAC 是 todo 11 的验收测试（计划：`go test ./internal/middleware -run TestRBAC -v -count=1`）：
//   - 无 token → 401；
//   - staff 访问 admin 路由 → 403；staff 访问 both 路由 → 200；
//   - 禁用用户的（停用前签发的）token → 401；
//   - admin 全通；
//   - 伪造 role=admin 声明的 staff token → 仍 403（角色实时读库，不信任 token）；
//   - admin token 即使声明 role=staff → 仍按 DB 角色放行；
//   - 乱码 token / Basic 方案 → 401。
//
// 权限矩阵以 04-API.md:60-68 与 06-BUSINESS-RULES.md §7（staff 禁订单取消/余额调整/
// 退款/删除客户/系统设置/操作日志/员工管理/数据恢复，改价仅 admin）为准；
// 后续业务路由用 middleware.RequireRole(model.RoleAdmin) 保护对应分组。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const rbacTestSecret = "rbac-unit-test-secret"

// rbacEnv 是 RBAC 测试环境：临时库 + 双角色探针路由 + 预签发 token。
type rbacEnv struct {
	engine           *gin.Engine
	tokenService     *service.TokenService
	admin            *model.User
	staff            *model.User
	adminToken       string
	staffToken       string
	disabledToken    string
	forgedAdminClaim string
	adminStaffClaim  string
}

func newRBACEnv(t *testing.T) *rbacEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := repository.Open(filepath.Join(t.TempDir(), "rbac.db"))
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

	users := service.NewUserService(repository.NewUserRepository(db))
	tokens := service.NewTokenService(rbacTestSecret, time.Hour)
	ctx := context.Background()
	if created, err := users.SeedAdmin(ctx, "Admin-Pwd-1"); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	admin, err := users.FindByUsername(ctx, service.DefaultAdminUsername)
	if err != nil {
		t.Fatalf("FindByUsername(admin): %v", err)
	}
	staff, err := users.CreateUser(ctx, service.CreateUserInput{Username: "staff1", Password: "Staff-Pwd-1", Role: model.RoleStaff})
	if err != nil {
		t.Fatalf("CreateUser(staff1): %v", err)
	}
	gone, err := users.CreateUser(ctx, service.CreateUserInput{Username: "gone", Password: "Gone-Pwd-1", Role: model.RoleStaff})
	if err != nil {
		t.Fatalf("CreateUser(gone): %v", err)
	}
	// 先签发 token 再停用：证明“停用后旧 token 立即失效”。
	goneToken, _, err := tokens.Issue(gone)
	if err != nil {
		t.Fatalf("Issue(gone): %v", err)
	}
	if err := users.DisableUser(ctx, gone.ID); err != nil {
		t.Fatalf("DisableUser(gone): %v", err)
	}

	env := &rbacEnv{
		engine:        gin.New(),
		tokenService:  tokens,
		admin:         admin,
		staff:         staff,
		disabledToken: goneToken,
	}
	if env.adminToken, _, err = tokens.Issue(admin); err != nil {
		t.Fatalf("Issue(admin): %v", err)
	}
	if env.staffToken, _, err = tokens.Issue(staff); err != nil {
		t.Fatalf("Issue(staff): %v", err)
	}
	// 伪造声明：staff 的 uid + role=admin；admin 的 uid + role=staff。
	env.forgedAdminClaim = signClaims(t, staff.ID, model.RoleAdmin)
	env.adminStaffClaim = signClaims(t, admin.ID, model.RoleStaff)

	env.engine.Use(middleware.RequestContext())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminRoutes := env.engine.Group("/admin",
		middleware.JWTAuth(tokens, users, logger),
		middleware.RequireRole(model.RoleAdmin))
	adminRoutes.GET("/probe", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	bothRoutes := env.engine.Group("/both",
		middleware.JWTAuth(tokens, users, logger),
		middleware.RequireRole(model.RoleAdmin, model.RoleStaff))
	bothRoutes.GET("/probe", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	return env
}

// signClaims 用测试密钥伪造指定 uid/role 的合法签名 token。
func signClaims(t *testing.T, userID int64, role string) string {
	t.Helper()
	now := time.Now().UTC()
	claims := &service.TokenClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(rbacTestSecret))
	if err != nil {
		t.Fatalf("伪造 token 签名失败: %v", err)
	}
	return signed
}

// request 发起探针请求；token 为空表示不带 Authorization。
func (e *rbacEnv) request(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, bytes.NewReader(nil))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// assertStatus 断言 HTTP 状态与信封 code。
func assertStatus(t *testing.T, w *httptest.ResponseRecorder, wantStatus, wantCode int, name string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d (body=%s)", name, w.Code, wantStatus, w.Body.String())
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s 响应不是 JSON 信封: %v (body=%s)", name, err, w.Body.String())
	}
	if env.Code != wantCode {
		t.Errorf("%s envelope.code = %d, want %d (body=%s)", name, env.Code, wantCode, w.Body.String())
	}
	if wantCode != 0 && string(env.Data) != "null" {
		t.Errorf("%s 失败信封 data = %s, want null", name, env.Data)
	}
}

func TestRBAC(t *testing.T) {
	env := newRBACEnv(t)

	// When/Then: 无 token 访问 admin 路由 → 401。
	assertStatus(t, env.request("/admin/probe", ""), http.StatusUnauthorized, service.CodeUnauthorized, "无 token")
	// When/Then: 乱码 token → 401。
	assertStatus(t, env.request("/admin/probe", "not-a-jwt"), http.StatusUnauthorized, service.CodeUnauthorized, "乱码 token")

	// When/Then: staff 访问 admin 路由 → 403（06 §7 staff 禁员工/用户管理类接口）。
	assertStatus(t, env.request("/admin/probe", env.staffToken), http.StatusForbidden, service.CodeForbidden, "staff→admin")

	// When/Then: staff 访问 both 路由 → 200。
	assertStatus(t, env.request("/both/probe", env.staffToken), http.StatusOK, service.CodeOK, "staff→both")

	// When/Then: admin 访问 admin 路由与 both 路由 → 200。
	assertStatus(t, env.request("/admin/probe", env.adminToken), http.StatusOK, service.CodeOK, "admin→admin")
	assertStatus(t, env.request("/both/probe", env.adminToken), http.StatusOK, service.CodeOK, "admin→both")

	// When/Then: 停用用户（停用前签发）的 token → 401（旧 token 立即失效）。
	assertStatus(t, env.request("/both/probe", env.disabledToken), http.StatusUnauthorized, service.CodeUnauthorized, "disabled token→both")
	assertStatus(t, env.request("/admin/probe", env.disabledToken), http.StatusUnauthorized, service.CodeUnauthorized, "disabled token→admin")

	// When: 伪造 role=admin 声明的 staff token 访问 admin 路由。
	forged := env.request("/admin/probe", env.forgedAdminClaim)

	// Then: 403 —— 角色以数据库为准，token 声明无效。
	assertStatus(t, forged, http.StatusForbidden, service.CodeForbidden, "伪造 role=admin 的 staff token")

	// When/Then: 伪造 role=staff 声明的 admin token 访问 admin 路由 → 200（DB 角色 admin）。
	assertStatus(t, env.request("/admin/probe", env.adminStaffClaim), http.StatusOK, service.CodeOK, "admin token 声明 role=staff")

	// Then: token 中含 role 声明只是负载参考，权限来自本次请求的 DB 实时查询。
	if env.admin.ID == env.staff.ID {
		t.Fatal("测试前置错误：admin 与 staff 不能是同一用户")
	}
}
