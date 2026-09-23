package middleware_test

// TestPermissionMatrix 是 todo 59 的验收测试（计划：`go test ./internal/middleware -run TestPermissionMatrix -v -count=1`）：
// 全部受保护路由 × {anon, staff, admin} 的权限矩阵（04-API.md:60-68、06-BUSINESS-RULES.md §7）：
//
//	anon  → 401（未登录，认证先于授权）；
//	staff → admin 路由 403 / 40300；both 路由放行（非 401/403）；
//	admin → 全路由放行（非 401/403）。
//
// 路由清单来自 gin `engine.Routes()`（真实 router.New 装配，含 BackupService 路由），
// 与 protectedPolicies 期望表做**双向**比对：新增/删除路由而不同步期望表 → 测试失败（防漂移）。
// staff 明确禁止的 10 类操作（取消/调整/退款/冲正/删客户/设置改/日志/员工/用户/恢复）
// 另做显式断言。
//
// 请求使用空 body：矩阵只验证 RBAC 关卡结果（401/403/放行），不验证各 handler 的业务校验；
// 因此不会产生任何账务写入（backup/restore 等有副作用的路由在 403 前置或空 body 400 处终止）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const permissionMatrixJWTSecret = "permission-matrix-jwt-secret"

// policyBoth / policyAdmin 是路由的期望 RBAC 策略（与 router.go 分组一一对应）。
const (
	policyBoth  = "both"
	policyAdmin = "admin"
)

// protectedPolicies 是 /api/v1 下全部受保护路由的期望策略（免认证的 /health 与 /auth/login 除外）。
//
// 该表与 engine.Routes() 双向比对；新增路由必须在此登记（04-API.md:60-68）。
var protectedPolicies = map[string]string{
	// --- both：admin + staff ---
	"GET /api/v1/auth/me":                            policyBoth,
	"POST /api/v1/auth/logout":                       policyBoth,
	"GET /api/v1/customers":                          policyBoth,
	"POST /api/v1/customers":                         policyBoth,
	"GET /api/v1/customers/:id":                      policyBoth,
	"PUT /api/v1/customers/:id":                      policyBoth,
	"GET /api/v1/customers/:id/orders":               policyBoth,
	"GET /api/v1/customers/:id/balance-transactions": policyBoth,
	"GET /api/v1/customers/:id/points-transactions":  policyBoth,
	"GET /api/v1/tags":                               policyBoth,
	"POST /api/v1/customers/:id/tags":                policyBoth,
	"DELETE /api/v1/customers/:id/tags/:tag_id":      policyBoth,
	"GET /api/v1/service-categories":                 policyBoth,
	"GET /api/v1/services":                           policyBoth,
	"GET /api/v1/services/:id":                       policyBoth,
	"POST /api/v1/orders":                            policyBoth,
	"GET /api/v1/orders":                             policyBoth,
	"GET /api/v1/orders/:id":                         policyBoth,
	"POST /api/v1/orders/:id/items":                  policyBoth,
	"PUT /api/v1/orders/:id/items/:item_id":          policyBoth,
	"DELETE /api/v1/orders/:id/items/:item_id":       policyBoth,
	"POST /api/v1/orders/:id/pay":                    policyBoth,
	"POST /api/v1/recharges":                         policyBoth,
	"GET /api/v1/recharges":                          policyBoth,
	"GET /api/v1/settings":                           policyBoth,
	"GET /api/v1/dashboard/summary":                  policyBoth,
	"GET /api/v1/dashboard/revenue":                  policyBoth,
	"GET /api/v1/dashboard/customers":                policyBoth,
	"GET /api/v1/dashboard/employee-performance":     policyBoth,
	// --- admin only：06 §7 staff 禁区 ---
	"DELETE /api/v1/customers/:id":                   policyAdmin,
	"POST /api/v1/tags":                              policyAdmin,
	"PUT /api/v1/tags/:id":                           policyAdmin,
	"DELETE /api/v1/tags/:id":                        policyAdmin,
	"POST /api/v1/service-categories":                policyAdmin,
	"PUT /api/v1/service-categories/:id":             policyAdmin,
	"DELETE /api/v1/service-categories/:id":          policyAdmin,
	"POST /api/v1/services":                          policyAdmin,
	"PUT /api/v1/services/:id":                       policyAdmin,
	"DELETE /api/v1/services/:id":                    policyAdmin,
	"POST /api/v1/orders/:id/cancel":                 policyAdmin,
	"POST /api/v1/orders/:id/refund":                 policyAdmin,
	"POST /api/v1/customers/:id/balance-adjustments": policyAdmin,
	"POST /api/v1/recharges/:id/refund":              policyAdmin,
	"PUT /api/v1/settings/:key":                      policyAdmin,
	"GET /api/v1/operation-logs":                     policyAdmin,
	"GET /api/v1/backups":                            policyAdmin,
	"POST /api/v1/backups":                           policyAdmin,
	"POST /api/v1/backups/:id/restore":               policyAdmin,
	"GET /api/v1/employees":                          policyAdmin,
	"POST /api/v1/employees":                         policyAdmin,
	"GET /api/v1/employees/:id":                      policyAdmin,
	"PUT /api/v1/employees/:id":                      policyAdmin,
	"DELETE /api/v1/employees/:id":                   policyAdmin,
	"GET /api/v1/users":                              policyAdmin,
	"POST /api/v1/users":                             policyAdmin,
	"PUT /api/v1/users/:id":                          policyAdmin,
}

// staffForbiddenRoutes 是 06 §7 明确禁止 staff 的操作（验收显式断言，防策略表被误改）。
var staffForbiddenRoutes = map[string]string{
	"订单取消":   "POST /api/v1/orders/:id/cancel",
	"余额调整":   "POST /api/v1/customers/:id/balance-adjustments",
	"订单退款":   "POST /api/v1/orders/:id/refund",
	"充值冲正":   "POST /api/v1/recharges/:id/refund",
	"删除客户":   "DELETE /api/v1/customers/:id",
	"系统设置修改": "PUT /api/v1/settings/:key",
	"操作日志":   "GET /api/v1/operation-logs",
	"员工管理":   "GET /api/v1/employees",
	"用户管理":   "POST /api/v1/users",
	"数据恢复":   "POST /api/v1/backups/:id/restore",
}

// permissionEnv 是权限矩阵环境：完整路由（含备份/恢复）+ 真实迁移库 + 双角色 token。
type permissionEnv struct {
	engine     *gin.Engine
	db         *gorm.DB
	adminToken string
	staffToken string
}

func newPermissionEnv(t *testing.T) *permissionEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	for _, dir := range []string{dataDir, filepath.Join(dataDir, "uploads"), filepath.Join(dataDir, "backups")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	dbPath := filepath.Join(dataDir, "permission.db")
	db, err := repository.Open(dbPath)
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
	if err := repository.EnsureDefaultSettings(db); err != nil {
		t.Fatalf("EnsureDefaultSettings: %v", err)
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("DB_PATH: data/permission.db\nJWT_SECRET: permission-matrix\n"), 0o644); err != nil {
		t.Fatalf("写 config.yaml: %v", err)
	}

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	backups := service.NewBackupService(service.BackupServiceDeps{
		DB:         db,
		DBPath:     dbPath,
		UploadDir:  filepath.Join(dataDir, "uploads"),
		BackupDir:  filepath.Join(dataDir, "backups"),
		ConfigPath: configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(db)),
		Logger:     discard,
	})
	engine := router.New(db, discard, router.Options{
		JWTSecret: permissionMatrixJWTSecret,
		Backups:   backups,
	})

	users := service.NewUserService(repository.NewUserRepository(db), repository.NewEmployeeRepository(db))
	if created, err := users.SeedAdmin(context.Background(), "Perm-Admin-Pwd-1"); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	if _, err := users.CreateUser(context.Background(), service.CreateUserInput{
		Username: "perm-staff", Password: "Perm-Staff-Pwd-1", Role: model.RoleStaff,
	}); err != nil {
		t.Fatalf("CreateUser(perm-staff): %v", err)
	}

	env := &permissionEnv{engine: engine, db: db}
	env.adminToken = env.login(t, service.DefaultAdminUsername, "Perm-Admin-Pwd-1")
	env.staffToken = env.login(t, "perm-staff", "Perm-Staff-Pwd-1")
	return env
}

// login 走真实登录接口换取 token（不绕过 JWT 中间件）。
func (e *permissionEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	w := e.request(http.MethodPost, "/api/v1/auth/login", body, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login(%s) status = %d, want 200 (body=%s)", username, w.Code, w.Body.String())
	}
	var envl struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil || envl.Code != service.CodeOK || envl.Data.Token == "" {
		t.Fatalf("login(%s) 响应异常: err=%v body=%s", username, err, w.Body.String())
	}
	return envl.Data.Token
}

// request 发起矩阵探针请求（token 为空表示匿名）。
func (e *permissionEnv) request(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// probePath 把路由模板中的路径参数替换为探针值（只验证 RBAC 关卡，不要求资源存在）。
func probePath(routePath string) string {
	segments := strings.Split(routePath, "/")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		if strings.TrimPrefix(segment, ":") == "key" {
			segments[i] = model.SettingPointsPerYuan
			continue
		}
		segments[i] = "1"
	}
	return strings.Join(segments, "/")
}

// requireGate 断言 RBAC 关卡结果：want ∈ {401, 403, pass}，并校验信封 code。
func requireGate(t *testing.T, label string, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	switch want {
	case "401":
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401 (body=%s)", label, w.Code, w.Body.String())
		}
		if code := envelopeCode(t, w); code != service.CodeUnauthorized {
			t.Errorf("%s: envelope.code = %d, want %d", label, code, service.CodeUnauthorized)
		}
	case "403":
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403 (body=%s)", label, w.Code, w.Body.String())
		}
		if code := envelopeCode(t, w); code != service.CodeForbidden {
			t.Errorf("%s: envelope.code = %d, want %d", label, code, service.CodeForbidden)
		}
	default: // pass：RBAC 放行（后续业务校验可能 400/404/422，但绝不 401/403）
		if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Fatalf("%s: status = %d, want RBAC 放行（非 401/403）(body=%s)", label, w.Code, w.Body.String())
		}
	}
}

// envelopeCode 读取响应信封的 code（非 JSON 响应视为致命错误）。
func envelopeCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var envl struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v (body=%s)", err, w.Body.String())
	}
	return envl.Code
}

func TestPermissionMatrix(t *testing.T) {
	env := newPermissionEnv(t)

	// --- Given: 真实路由表与期望策略双向比对（防漂移） ---
	routeKeys := make(map[string]struct{})
	for _, r := range env.engine.Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1") || r.Path == "/api/v1/auth/login" {
			continue
		}
		routeKeys[r.Method+" "+r.Path] = struct{}{}
	}
	if len(routeKeys) == 0 {
		t.Fatal("engine.Routes() 未返回任何受保护路由")
	}
	for key := range routeKeys {
		if _, ok := protectedPolicies[key]; !ok {
			t.Errorf("未分类的受保护路由（请在 protectedPolicies 登记）: %s", key)
		}
	}
	for key := range protectedPolicies {
		if _, ok := routeKeys[key]; !ok {
			t.Errorf("protectedPolicies 条目 %s 在真实路由表中不存在（已过期）", key)
		}
	}
	t.Logf("受保护路由数 = %d（both %d / admin %d）", len(routeKeys), countPolicy(policyBoth), countPolicy(policyAdmin))

	// --- When/Then: 逐路由 × 三角色断言 ---
	keys := make([]string, 0, len(routeKeys))
	for key := range routeKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		method, routePath, _ := strings.Cut(key, " ")
		policy := protectedPolicies[key]
		path := probePath(routePath)
		t.Run(key, func(t *testing.T) {
			// anon → 401
			requireGate(t, "anon", env.request(method, path, "", ""), "401")
			// staff → admin 403 / both 放行
			staffWant := "pass"
			if policy == policyAdmin {
				staffWant = "403"
			}
			requireGate(t, "staff", env.request(method, path, "", env.staffToken), staffWant)
			// admin → 放行
			requireGate(t, "admin", env.request(method, path, "", env.adminToken), "pass")
		})
	}

	// --- Then: 06 §7 staff 明确禁区显式断言（策略表 + 实测 403） ---
	t.Run("staff forbidden categories", func(t *testing.T) {
		categories := make([]string, 0, len(staffForbiddenRoutes))
		for category := range staffForbiddenRoutes {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		for _, category := range categories {
			key := staffForbiddenRoutes[category]
			if protectedPolicies[key] != policyAdmin {
				t.Errorf("06 §7 禁区「%s」策略 = %q, want admin（%s）", category, protectedPolicies[key], key)
				continue
			}
			method, routePath, _ := strings.Cut(key, " ")
			w := env.request(method, probePath(routePath), "", env.staffToken)
			requireGate(t, "staff 禁区「"+category+"」", w, "403")
		}
	})
}

// countPolicy 统计期望表中指定策略的路由数（日志用）。
func countPolicy(policy string) int {
	count := 0
	for _, p := range protectedPolicies {
		if p == policy {
			count++
		}
	}
	return count
}
