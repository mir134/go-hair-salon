package controller_test

// TestRestore 是 todo 52 的验收测试（计划：`go test ./internal/controller -run TestRestore -v -count=1`）：
//
//   - 流程 E（05-TASKS.md:165-169、08-DEPLOYMENT.md:78）：备份 → 改数据 → 恢复 → 数据回到备份时点；
//   - 恢复前自动生成当前数据的安全备份：打开安全备份 zip 的快照，必须能看到被改动后的数据
//     （证明安全备份捕获的是「恢复前」状态）；
//   - 恢复期间维护模式：业务写请求 503 + code=50300 + 「系统维护中」，读请求放行；
//   - 权限：仅 admin（staff→403、未登录→401）；二次确认：缺 confirm / confirm=false → 400；
//   - malformed_input：非法 id→400、不存在→404、损坏 zip→422 且原数据不受影响；
//   - cancel_resume：任何失败后维护标志都清除、业务写入立即恢复可用；
//   - misleading_success_output：直接打开恢复后的数据库文件、安全备份 zip 与 uploads/config
//     核对（不信任响应码/成功文案）；
//   - stale_state：恢复后的数据等于备份时点，而不是恢复前状态。
//
// 测试使用真实迁移库 + 真实路由 + 真实文件系统（t.TempDir），不 mock、不启动服务器。

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const (
	restoreJWTSecret     = "restore-test-jwt-secret"
	restoreAdminPassword = "Restore-Admin-Pwd-1"
	restoreStaffUsername = "restore-staff"
	restoreStaffPassword = "Restore-Staff-Pwd-1"
	// restoreConfigV1 是恢复前写入 config.yaml 的内容（备份会包含它）。
	restoreConfigV1 = "DB_PATH: data/salon.db\nJWT_SECRET: restore-config-secret\n"
	// restoreConfigV2 是备份之后被篡改的内容（恢复后必须回到 V1）。
	restoreConfigV2 = "DB_PATH: data/evil.db\n"
	// restoreUploadV1 / restoreUploadV2 是 uploads 文件在备份前后的内容。
	restoreUploadV1 = "avatar-v1"
	restoreUploadV2 = "avatar-v2"
)

// restoreBackupPattern 是备份文件名格式（与 service.backupNamePattern 一致）。
var restoreBackupPattern = regexp.MustCompile(`^backup-\d{8}-\d{6}(-\d+)?\.zip$`)

// restoreEnv 是 todo 52 的验收环境：真实库 + 真实路由 + 备份/恢复服务 + admin/staff token。
type restoreEnv struct {
	engine     *gin.Engine
	db         *gorm.DB
	dbPath     string
	uploadDir  string
	backupDir  string
	configPath string
	guard      *service.MaintenanceGuard
	adminID    int64
	adminToken string
	staffToken string
	// 维护期观测：OnMaintenanceEntered 钩子在「维护标志已置位、数据尚未替换」时填充。
	duringWrite *httptest.ResponseRecorder
	duringRead  *httptest.ResponseRecorder
}

// newRestoreEnv 装配验收环境；onMaintenance 可为 nil（不需要维护期探针时）。
func newRestoreEnv(t *testing.T, onMaintenance func()) *restoreEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(dataDir, "salon.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	if _, err := db.DB(); err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	// 关闭「退出时当前的」连接池：恢复流程会原地替换连接池，启动时捕获的旧池已关闭。
	t.Cleanup(func() {
		if current, err := db.DB(); err == nil {
			_ = current.Close()
		}
	})
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	if err := repository.EnsureDefaultSettings(db); err != nil {
		t.Fatalf("EnsureDefaultSettings: %v", err)
	}

	uploadDir := filepath.Join(dataDir, "uploads")
	backupDir := filepath.Join(dataDir, "backups")
	for _, dir := range []string{uploadDir, backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(restoreConfigV1), 0o644); err != nil {
		t.Fatalf("写 config.yaml: %v", err)
	}

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	backups := service.NewBackupService(service.BackupServiceDeps{
		DB:         db,
		DBPath:     dbPath,
		UploadDir:  uploadDir,
		BackupDir:  backupDir,
		ConfigPath: configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(db)),
		Logger:     discard,
	})
	guard := service.NewMaintenanceGuard()
	restores := service.NewRestoreService(service.RestoreServiceDeps{
		Backups:              backups,
		Guard:                guard,
		Logger:               discard,
		OnMaintenanceEntered: onMaintenance,
	})

	users := service.NewUserService(repository.NewUserRepository(db), repository.NewEmployeeRepository(db))
	if created, err := users.SeedAdmin(context.Background(), restoreAdminPassword); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	admin, err := users.FindByUsername(context.Background(), service.DefaultAdminUsername)
	if err != nil {
		t.Fatalf("FindByUsername(admin): %v", err)
	}
	if _, err := users.CreateUser(context.Background(), service.CreateUserInput{
		Username: restoreStaffUsername, Password: restoreStaffPassword, Role: model.RoleStaff,
	}); err != nil {
		t.Fatalf("CreateUser(staff): %v", err)
	}

	engine := router.New(db, discard, router.Options{
		JWTSecret:   restoreJWTSecret,
		Backups:     backups,
		Maintenance: guard,
		Restores:    restores,
	})
	env := &restoreEnv{
		engine: engine, db: db, dbPath: dbPath, uploadDir: uploadDir,
		backupDir: backupDir, configPath: configPath, guard: guard, adminID: admin.ID,
	}
	env.adminToken = env.login(t, service.DefaultAdminUsername, restoreAdminPassword)
	env.staffToken = env.login(t, restoreStaffUsername, restoreStaffPassword)
	return env
}

// login 走真实登录接口换取 token。
func (e *restoreEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	w := e.do(http.MethodPost, "/api/v1/auth/login", body, "")
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

// do 发起一次真实 HTTP 请求。
func (e *restoreEnv) do(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	req.Header.Set("User-Agent", "restore-probe/1.0")
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// createBackupViaAPI 走真实接口创建手动备份，返回备份名。
func (e *restoreEnv) createBackupViaAPI(t *testing.T) string {
	t.Helper()
	w := e.do(http.MethodPost, "/api/v1/backups", "", e.adminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /backups status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var envl struct {
		Code int `json:"code"`
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil || envl.Code != service.CodeOK || envl.Data.Name == "" {
		t.Fatalf("POST /backups 响应异常: err=%v body=%s", err, w.Body.String())
	}
	if !restoreBackupPattern.MatchString(envl.Data.Name) {
		t.Fatalf("备份名 %q 不符合 backup-YYYYMMDD-HHMMSS.zip", envl.Data.Name)
	}
	return envl.Data.Name
}

// createCustomerViaAPI 走真实接口建档，返回客户 id。
func (e *restoreEnv) createCustomerViaAPI(t *testing.T, token, body string) int64 {
	t.Helper()
	w := e.do(http.MethodPost, "/api/v1/customers", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /customers status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var envl struct {
		Code int `json:"code"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil || envl.Code != service.CodeOK || envl.Data.ID <= 0 {
		t.Fatalf("POST /customers 响应异常: err=%v body=%s", err, w.Body.String())
	}
	return envl.Data.ID
}

// restoreView 是恢复接口 data 的测试镜像。
type restoreView struct {
	Restored        string `json:"restored"`
	SafetyBackup    string `json:"safety_backup"`
	FileCount       int    `json:"file_count"`
	UploadsReplaced bool   `json:"uploads_replaced"`
	ConfigReplaced  bool   `json:"config_replaced"`
}

// restoreViaAPI 调用恢复接口并返回响应。
func (e *restoreEnv) restoreViaAPI(t *testing.T, name, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(http.MethodPost, "/api/v1/backups/"+name+"/restore", body, token)
}

// decodeRestoreView 断言 200 + code=0 并解析 data。
func decodeRestoreView(t *testing.T, w *httptest.ResponseRecorder) restoreView {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("恢复 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var envl struct {
		Code int         `json:"code"`
		Data restoreView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil || envl.Code != service.CodeOK {
		t.Fatalf("恢复响应异常: err=%v body=%s", err, w.Body.String())
	}
	return envl.Data
}

// openLiveDBFile 以独立连接直接打开数据库文件（misleading_success_output 防线）。
func openLiveDBFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", repository.DSN(path))
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// writeRestoreZip 构造一个 zip（entry -> content），用于对抗用例（zip slip / 坏快照）。
func writeRestoreZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 zip %s: %v", path, err)
	}
	writer := zip.NewWriter(out)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目 %s: %v", name, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("写 zip 条目 %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 zip writer: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("关闭 zip 文件: %v", err)
	}
}

// readRestoreZip 读取 zip 全部条目（name -> content）。
func readRestoreZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("打开 zip %s 失败: %v", path, err)
	}
	defer func() { _ = reader.Close() }()
	entries := make(map[string][]byte, len(reader.File))
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读取 zip 条目 %s 失败: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("读取 zip 条目内容 %s 失败: %v", f.Name, err)
		}
		entries[f.Name] = content
	}
	return entries
}

// openRestoreSnapshot 把 zip 中的快照落盘并只读打开（校验 integrity_check=ok）。
func openRestoreSnapshot(t *testing.T, data []byte) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写出快照失败: %v", err)
	}
	sqlDB, err := repository.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var integrity string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("PRAGMA integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("快照 integrity_check = %q, want \"ok\"", integrity)
	}
	return sqlDB
}

// customerNameByPhone 直接查数据库文件里某个手机号的客户名（空表示不存在）。
func customerNameByPhone(t *testing.T, sqlDB *sql.DB, phone string) string {
	t.Helper()
	var name string
	err := sqlDB.QueryRow("SELECT name FROM customers WHERE phone = ? AND deleted_at IS NULL", phone).Scan(&name)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("查询 customers(phone=%s) 失败: %v", phone, err)
	}
	return name
}

func TestRestore(t *testing.T) {
	t.Run("流程 E：备份→改数据→恢复→数据回到备份时点，且恢复前安全备份存在", func(t *testing.T) {
		env := newRestoreEnv(t, nil)

		// Given: 基线客户 + uploads 文件 + config；随后创建备份（= 恢复目标时点）。
		baselineID := env.createCustomerViaAPI(t, env.staffToken, `{"name":"恢复基线客户","phone":"13911110001"}`)
		uploadPath := filepath.Join(env.uploadDir, "avatars", "a.txt")
		if err := os.MkdirAll(filepath.Dir(uploadPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(uploads): %v", err)
		}
		if err := os.WriteFile(uploadPath, []byte(restoreUploadV1), 0o644); err != nil {
			t.Fatalf("写 uploads 文件: %v", err)
		}
		backupName := env.createBackupViaAPI(t)

		// When 1: 改数据（改名 + 新增客户 + 改 uploads + 改 config）。
		updated := env.do(http.MethodPut, fmt.Sprintf("/api/v1/customers/%d", baselineID),
			`{"name":"恢复后改名","phone":"13911110001"}`, env.staffToken)
		if updated.Code != http.StatusOK {
			t.Fatalf("PUT /customers/%d status = %d, want 200 (body=%s)", baselineID, updated.Code, updated.Body.String())
		}
		env.createCustomerViaAPI(t, env.staffToken, `{"name":"恢复后新增客户","phone":"13911110002"}`)
		if err := os.WriteFile(uploadPath, []byte(restoreUploadV2), 0o644); err != nil {
			t.Fatalf("篡改 uploads 文件: %v", err)
		}
		if err := os.WriteFile(env.configPath, []byte(restoreConfigV2), 0o644); err != nil {
			t.Fatalf("篡改 config.yaml: %v", err)
		}

		// When 2: 恢复。
		view := decodeRestoreView(t, env.restoreViaAPI(t, backupName, `{"confirm":true}`, env.adminToken))

		// Then 1: 响应与磁盘一致：安全备份存在且是有效 zip（内含恢复前状态）。
		if view.Restored != backupName {
			t.Errorf("restored = %q, want %q", view.Restored, backupName)
		}
		if !view.UploadsReplaced || !view.ConfigReplaced {
			t.Errorf("uploads_replaced/config_replaced = %v/%v, want true/true（备份含 uploads 与 config）",
				view.UploadsReplaced, view.ConfigReplaced)
		}
		if !restoreBackupPattern.MatchString(view.SafetyBackup) || view.SafetyBackup == backupName {
			t.Fatalf("safety_backup = %q, want 新的 backup-*.zip", view.SafetyBackup)
		}
		safetyPath := filepath.Join(env.backupDir, view.SafetyBackup)
		if _, err := os.Stat(safetyPath); err != nil {
			t.Fatalf("安全备份文件不存在: %v", err)
		}
		entries := readRestoreZip(t, safetyPath)
		safetySnapshot, ok := entries["db/"+filepath.Base(env.dbPath)]
		if !ok {
			t.Fatalf("安全备份缺少数据库快照（条目=%v）", entries)
		}
		safetyDB := openRestoreSnapshot(t, safetySnapshot)
		if got := customerNameByPhone(t, safetyDB, "13911110001"); got != "恢复后改名" {
			t.Errorf("安全备份中客户 = %q, want 恢复前状态「恢复后改名」（安全备份必须捕获恢复前数据）", got)
		}

		// Then 2: 直接打开恢复后的数据库文件核对（不信任响应码）。
		live := openLiveDBFile(t, env.dbPath)
		if got := customerNameByPhone(t, live, "13911110001"); got != "恢复基线客户" {
			t.Errorf("恢复后客户 = %q, want 备份时点「恢复基线客户」", got)
		}
		if got := customerNameByPhone(t, live, "13911110002"); got != "" {
			t.Errorf("恢复后仍存在备份时点之后的客户 %q（stale_state）", got)
		}

		// Then 3: uploads 与 config 也回到备份时点。
		uploadBytes, err := os.ReadFile(uploadPath)
		if err != nil || string(uploadBytes) != restoreUploadV1 {
			t.Errorf("恢复后 uploads = %q (err=%v), want %q", uploadBytes, err, restoreUploadV1)
		}
		configBytes, err := os.ReadFile(env.configPath)
		if err != nil || string(configBytes) != restoreConfigV1 {
			t.Errorf("恢复后 config.yaml = %q (err=%v), want 备份时点内容", configBytes, err)
		}

		// Then 4: operation_logs(action=restore) 记录真实操作人。
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "restore").First(&logRow).Error; err != nil {
			t.Fatalf("operation_logs 缺少 action=restore: %v", err)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.adminID {
			t.Errorf("restore 日志 operator_id = %v, want admin#%d", logRow.OperatorID, env.adminID)
		}
		if logRow.IP != "10.9.9.9" || logRow.UserAgent != "restore-probe/1.0" {
			t.Errorf("restore 日志 ip/ua = %q/%q, want 10.9.9.9/restore-probe/1.0", logRow.IP, logRow.UserAgent)
		}
	})

	t.Run("维护期：业务写请求 503 + 系统维护中，读请求放行", func(t *testing.T) {
		var env *restoreEnv
		env = newRestoreEnv(t, func() {
			// 钩子在「维护标志已置位、数据尚未替换」时执行。
			env.duringWrite = env.do(http.MethodPost, "/api/v1/customers",
				`{"name":"维护期写入","phone":"13911110003"}`, env.staffToken)
			env.duringRead = env.do(http.MethodGet, "/api/v1/customers", "", env.staffToken)
		})

		env.createCustomerViaAPI(t, env.staffToken, `{"name":"维护期基线客户","phone":"13911110004"}`)
		backupName := env.createBackupViaAPI(t)
		decodeRestoreView(t, env.restoreViaAPI(t, backupName, `{"confirm":true}`, env.adminToken))

		// Then 1: 维护期写请求 503 + 50300 + 明确文案。
		if env.duringWrite == nil || env.duringRead == nil {
			t.Fatal("恢复钩子未执行：无法验证维护期行为")
		}
		if env.duringWrite.Code != http.StatusServiceUnavailable {
			t.Fatalf("维护期 POST /customers status = %d, want 503 (body=%s)",
				env.duringWrite.Code, env.duringWrite.Body.String())
		}
		var writeEnvl struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(env.duringWrite.Body.Bytes(), &writeEnvl); err != nil {
			t.Fatalf("维护期写响应不是信封: %v (body=%s)", err, env.duringWrite.Body.String())
		}
		if writeEnvl.Code != service.CodeServiceUnavailable {
			t.Errorf("维护期写响应 code = %d, want %d", writeEnvl.Code, service.CodeServiceUnavailable)
		}
		if !strings.Contains(writeEnvl.Message, "系统维护中") {
			t.Errorf("维护期写响应 message = %q, want 含「系统维护中」", writeEnvl.Message)
		}
		if env.duringRead.Code != http.StatusOK {
			t.Errorf("维护期 GET /customers status = %d, want 200（读请求放行）", env.duringRead.Code)
		}

		// Then 2: 维护标志在恢复结束后必须清除，写入立即恢复。
		if env.guard.Active() {
			t.Fatal("恢复结束后维护标志仍为激活状态")
		}
		if w := env.do(http.MethodPost, "/api/v1/customers",
			`{"name":"恢复后写入","phone":"13911110005"}`, env.staffToken); w.Code != http.StatusCreated {
			t.Errorf("恢复结束后 POST /customers status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("权限与二次确认：staff→403、未登录→401、缺 confirm→400", func(t *testing.T) {
		env := newRestoreEnv(t, nil)
		backupName := env.createBackupViaAPI(t)

		if w := env.restoreViaAPI(t, backupName, `{"confirm":true}`, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录恢复 status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
		if w := env.restoreViaAPI(t, backupName, `{"confirm":true}`, env.staffToken); w.Code != http.StatusForbidden {
			t.Errorf("staff 恢复 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}
		for _, body := range []string{`{}`, `{"confirm":false}`} {
			w := env.restoreViaAPI(t, backupName, body, env.adminToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("confirm 缺失/为假（body=%s）status = %d, want 400 (body=%s)", body, w.Code, w.Body.String())
			}
		}
		// 未发生恢复：维护标志必须保持清除，备份数不变。
		if env.guard.Active() {
			t.Error("被拒请求不应置位维护标志")
		}
		if entries, err := os.ReadDir(env.backupDir); err != nil || len(entries) != 1 {
			t.Errorf("被拒请求后备份数 = %d (err=%v), want 1", len(entries), err)
		}
	})

	t.Run("malformed_input：非法 id→400、不存在→404、损坏 zip→422 且原数据不受影响", func(t *testing.T) {
		env := newRestoreEnv(t, nil)
		customerID := env.createCustomerViaAPI(t, env.staffToken, `{"name":"损坏恢复基线","phone":"13911110006"}`)
		env.createBackupViaAPI(t)

		// 非法 id（不符合备份命名）→ 400。
		if w := env.restoreViaAPI(t, "not-a-backup", `{"confirm":true}`, env.adminToken); w.Code != http.StatusBadRequest {
			t.Errorf("非法 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
		}
		// 合法命名但不存在 → 404。
		if w := env.restoreViaAPI(t, "backup-20000101-000000.zip", `{"confirm":true}`, env.adminToken); w.Code != http.StatusNotFound {
			t.Errorf("不存在 id status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		// 损坏 zip（命名合法）→ 422。
		corruptName := "backup-20200101-000000.zip"
		if err := os.WriteFile(filepath.Join(env.backupDir, corruptName), []byte("not-a-zip"), 0o644); err != nil {
			t.Fatalf("写损坏 zip: %v", err)
		}
		w := env.restoreViaAPI(t, corruptName, `{"confirm":true}`, env.adminToken)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("损坏 zip status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}

		// zip slip：uploads 条目带 ../ → 422，且不得写出暂存目录之外。
		slipName := "backup-20200202-000000.zip"
		slipTarget := filepath.Join(filepath.Dir(env.uploadDir), "evil.txt")
		writeRestoreZip(t, filepath.Join(env.backupDir, slipName), map[string][]byte{
			"db/salon.db":         []byte("not-sqlite"),
			"uploads/../evil.txt": []byte("evil"),
		})
		if w := env.restoreViaAPI(t, slipName, `{"confirm":true}`, env.adminToken); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("zip slip status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}
		if _, err := os.Stat(slipTarget); !os.IsNotExist(err) {
			t.Errorf("zip slip 写出了暂存目录之外的文件 %s (err=%v)", slipTarget, err)
		}

		// 快照不是可读 SQLite → 422（步骤 4 校验，不进入替换阶段）。
		badDBSnapshot := "backup-20200303-000000.zip"
		writeRestoreZip(t, filepath.Join(env.backupDir, badDBSnapshot), map[string][]byte{
			"db/salon.db": []byte("definitely-not-a-sqlite-file"),
			"config.yaml": []byte("DB_PATH: evil.db\n"),
		})
		if w := env.restoreViaAPI(t, badDBSnapshot, `{"confirm":true}`, env.adminToken); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("坏快照 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
		}

		// Then: 原数据完好（直接打开数据库文件核对），维护标志已清除，写入可用。
		live := openLiveDBFile(t, env.dbPath)
		var name string
		if err := live.QueryRow("SELECT name FROM customers WHERE id = ?", customerID).Scan(&name); err != nil {
			t.Fatalf("损坏恢复后查询原客户失败: %v", err)
		}
		if name != "损坏恢复基线" {
			t.Errorf("损坏恢复后客户 = %q, want 「损坏恢复基线」", name)
		}
		if env.guard.Active() {
			t.Error("失败恢复后维护标志仍为激活状态（cancel_resume 防线）")
		}
		if w := env.do(http.MethodPost, "/api/v1/customers",
			`{"name":"失败后写入","phone":"13911110007"}`, env.staffToken); w.Code != http.StatusCreated {
			t.Errorf("失败恢复后 POST /customers status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("uploads 未包含在备份中：不删除恢复后新增的上传文件", func(t *testing.T) {
		env := newRestoreEnv(t, nil)

		// Given: uploads 为空时创建备份（zip 内没有 uploads 条目）。
		backupName := env.createBackupViaAPI(t)

		// When: 备份之后新增上传文件，然后恢复。
		newFile := filepath.Join(env.uploadDir, "later.txt")
		if err := os.WriteFile(newFile, []byte("later"), 0o644); err != nil {
			t.Fatalf("写上传文件: %v", err)
		}
		view := decodeRestoreView(t, env.restoreViaAPI(t, backupName, `{"confirm":true}`, env.adminToken))

		// Then: 备份未包含 uploads → 现有 uploads 目录原样保留（不因恢复被清空）。
		if view.UploadsReplaced {
			t.Error("uploads_replaced = true, want false（备份未包含 uploads）")
		}
		data, err := os.ReadFile(newFile)
		if err != nil || string(data) != "later" {
			t.Errorf("恢复后新增上传文件 = %q (err=%v), want 保留", data, err)
		}
	})
}
