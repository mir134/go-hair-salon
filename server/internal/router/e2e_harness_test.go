package router_test

// e2e_harness_test.go 是 todo 64 端到端验收的测试环境（真实路由 + 真实 HTTP + 真实 SQLite +
// 真实备份/恢复服务，不 mock、不启动外部进程）。
//
// 设计要点：
//   - 每个用例使用 t.TempDir 下的独立数据库/备份目录/config.yaml，测试之间零共享；
//   - restart() 关闭 HTTP 服务与数据库连接池后用同一文件重新打开并重建路由，
//     等价于「进程重启后数据仍在」（08-DEPLOYMENT.md:104）而不依赖进程管理；
//   - 流程 A~E 的断言在 e2e_flows_test.go，本文件只提供环境与请求工具。

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const (
	e2eJWTSecret     = "e2e-flows-jwt-secret"
	e2eAdminPassword = "E2E-Admin-Pwd-1"
	e2eStaffUsername = "e2e-staff"
	e2eStaffPassword = "E2E-Staff-Pwd-1"
	// e2eConfigContent 是写入 config.yaml 的备份内容（备份 zip 会包含它，恢复时原样换回）。
	e2eConfigContent = "DB_PATH: data/salon.db\nJWT_SECRET: e2e-config-secret\n"
)

// e2eEnvelope 是统一响应信封的测试镜像。
type e2eEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// e2ePage 是分页响应的测试镜像。
type e2ePage[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

// e2eEnv 是一次端到端验收运行的全部运行期依赖。
type e2eEnv struct {
	t          *testing.T
	root       string
	dbPath     string
	uploadDir  string
	backupDir  string
	configPath string
	db         *gorm.DB
	server     *httptest.Server
	logger     *slog.Logger
}

// newE2EEnv 装配环境：迁移真实库、播种 settings 与初始 admin、创建运行目录与 config.yaml。
func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(dataDir, "salon.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	if err := repository.EnsureDefaultSettings(db); err != nil {
		t.Fatalf("EnsureDefaultSettings: %v", err)
	}
	users := service.NewUserService(repository.NewUserRepository(db), repository.NewEmployeeRepository(db))
	if created, err := users.SeedAdmin(t.Context(), e2eAdminPassword); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	env := &e2eEnv{
		t: t, root: root, dbPath: dbPath,
		uploadDir:  filepath.Join(dataDir, "uploads"),
		backupDir:  filepath.Join(dataDir, "backups"),
		configPath: filepath.Join(root, "config.yaml"),
		db:         db,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, dir := range []string{env.uploadDir, env.backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(env.configPath, []byte(e2eConfigContent), 0o644); err != nil {
		t.Fatalf("写 config.yaml: %v", err)
	}
	env.start()
	t.Cleanup(env.stop)
	return env
}

// start 用当前数据库连接构建路由并监听 127.0.0.1 随机端口（真实 HTTP 请求）。
func (e *e2eEnv) start() {
	e.server = httptest.NewServer(e.engine())
}

// engine 按 main.go 的装配顺序构建路由（备份/恢复服务与维护标志共享实例）。
func (e *e2eEnv) engine() *gin.Engine {
	backups := service.NewBackupService(service.BackupServiceDeps{
		DB:         e.db,
		DBPath:     e.dbPath,
		UploadDir:  e.uploadDir,
		BackupDir:  e.backupDir,
		ConfigPath: e.configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(e.db)),
		Logger:     e.logger,
	})
	guard := service.NewMaintenanceGuard()
	restores := service.NewRestoreService(service.RestoreServiceDeps{
		Backups: backups,
		Guard:   guard,
		Logger:  e.logger,
	})
	return router.New(e.db, e.logger, router.Options{
		JWTSecret:   e2eJWTSecret,
		Backups:     backups,
		Maintenance: guard,
		Restores:    restores,
		UploadDir:   e.uploadDir,
	})
}

// stop 关闭 HTTP 服务与「当前」数据库连接池（幂等；恢复流程会原地替换连接池）。
func (e *e2eEnv) stop() {
	if e.server != nil {
		e.server.Close()
		e.server = nil
	}
	if e.db != nil {
		if pool, err := e.db.DB(); err == nil {
			_ = pool.Close()
		}
		e.db = nil
	}
}

// restart 模拟进程重启：停服 → 关闭连接池 → 用同一 SQLite 文件重新打开并重建路由。
func (e *e2eEnv) restart() {
	e.stop()
	db, err := repository.Open(e.dbPath)
	if err != nil {
		e.t.Fatalf("restart repository.Open: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		e.t.Fatalf("restart repository.Migrate: %v", err)
	}
	e.db = db
	e.start()
}

// do 发起一次真实 HTTP 请求并返回状态码与响应体。
func (e *e2eEnv) do(method, path, token, body string) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("http.NewRequest(%s %s): %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		e.t.Fatalf("HTTP %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("读取响应体 %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

// call 断言状态码后返回响应体；不匹配直接失败并打印原始响应。
func (e *e2eEnv) call(method, path, token, body string, wantStatus int) []byte {
	e.t.Helper()
	status, raw := e.do(method, path, token, body)
	if status != wantStatus {
		e.t.Fatalf("%s %s = HTTP %d, want %d (body=%s)", method, path, status, wantStatus, raw)
	}
	return raw
}

// callData 断言状态码 + code=0 并把 data 解码到 out。
func (e *e2eEnv) callData(method, path, token, body string, wantStatus int, out any) {
	e.t.Helper()
	raw := e.call(method, path, token, body, wantStatus)
	var env e2eEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		e.t.Fatalf("%s %s 响应不是 JSON 信封: %v (body=%s)", method, path, err, raw)
	}
	if env.Code != service.CodeOK {
		e.t.Fatalf("%s %s: code=%d message=%s, want code=0", method, path, env.Code, env.Message)
	}
	if out == nil {
		return
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		e.t.Fatalf("%s %s: data 为空 (body=%s)", method, path, raw)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		e.t.Fatalf("%s %s: data 解析失败: %v (data=%s)", method, path, err, env.Data)
	}
}

// callBizError 断言失败响应：HTTP 状态码与业务 code 同时匹配。
func (e *e2eEnv) callBizError(method, path, token, body string, wantStatus, wantCode int) {
	e.t.Helper()
	status, raw := e.do(method, path, token, body)
	if status != wantStatus {
		e.t.Fatalf("%s %s = HTTP %d, want %d (body=%s)", method, path, status, wantStatus, raw)
	}
	var env e2eEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		e.t.Fatalf("%s %s 失败响应不是 JSON 信封: %v (body=%s)", method, path, err, raw)
	}
	if env.Code != wantCode {
		e.t.Fatalf("%s %s: code=%d, want %d (body=%s)", method, path, env.Code, wantCode, raw)
	}
}

// login 走真实登录接口换取 JWT。
func (e *e2eEnv) login(username, password string) string {
	e.t.Helper()
	var data struct {
		Token string `json:"token"`
	}
	e.callData(http.MethodPost, "/api/v1/auth/login", "", `{"username":"`+username+`","password":"`+password+`"}`, http.StatusOK, &data)
	if data.Token == "" {
		e.t.Fatal("登录响应缺少 token")
	}
	return data.Token
}
