package service_test

// 本文件是 todo 50/51 备份测试的共享环境与断言辅助（fixtures）：
// 真实迁移库 + uploads/config 目录 + 完整路由 + admin/staff token + zip/SQLite 校验工具。
// 测试只使用 t.TempDir() 内的真实 SQLite（WAL）与真实文件系统，不 mock、不启动服务器。

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
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const (
	backupTestJWTSecret = "backup-test-jwt-secret"
	backupAdminPassword = "Backup-Admin-Pwd-1"
	backupStaffPassword = "Backup-Staff-Pwd-1"
	backupStaffUsername = "backup-staff"
	// backupTestConfig 是测试 config.yaml 内容（bytes 对照用，不解析）。
	backupTestConfig = "DB_PATH: data/salon.db\nBACKUP_TIME: \"23:00\"\n"
)

// backupNamePattern 是备份文件名格式：backup-20060102-150405.zip（同秒冲突追加 -N）。
var backupNamePattern = regexp.MustCompile(`^backup-\d{8}-\d{6}(-\d+)?\.zip$`)

// backupTestEnv 是 todo 50/51 共用测试环境。
type backupTestEnv struct {
	db         *gorm.DB
	dbPath     string
	uploadDir  string
	backupDir  string
	configPath string
	svc        *service.BackupService
	engine     *gin.Engine
	admin      *model.User
	staff      *model.User
	adminToken string
	staffToken string
}

// newBackupTestEnv 装配测试环境（时钟为 time.Now）。
func newBackupTestEnv(t *testing.T) *backupTestEnv {
	t.Helper()
	return newBackupTestEnvWithClock(t, time.Now)
}

// newBackupTestEnvWithClock 装配测试环境，可注入时钟（todo 51 假时钟复用同一构造）。
func newBackupTestEnvWithClock(t *testing.T, now func() time.Time) *backupTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(dataDir, "salon.db")
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

	uploadDir := filepath.Join(dataDir, "uploads")
	backupDir := filepath.Join(dataDir, "backups")
	for _, dir := range []string{uploadDir, backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(backupTestConfig), 0o644); err != nil {
		t.Fatalf("写 config.yaml: %v", err)
	}

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewBackupService(service.BackupServiceDeps{
		DB:         db,
		DBPath:     dbPath,
		UploadDir:  uploadDir,
		BackupDir:  backupDir,
		ConfigPath: configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(db)),
		Now:        now,
		Logger:     discard,
	})

	users := service.NewUserService(repository.NewUserRepository(db), repository.NewEmployeeRepository(db))
	if created, err := users.SeedAdmin(context.Background(), backupAdminPassword); err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	admin, err := users.FindByUsername(context.Background(), service.DefaultAdminUsername)
	if err != nil {
		t.Fatalf("FindByUsername(admin): %v", err)
	}
	staff, err := users.CreateUser(context.Background(), service.CreateUserInput{
		Username: backupStaffUsername, Password: backupStaffPassword, Role: model.RoleStaff,
	})
	if err != nil {
		t.Fatalf("CreateUser(staff): %v", err)
	}

	engine := router.New(db, discard, router.Options{JWTSecret: backupTestJWTSecret, Backups: svc})
	env := &backupTestEnv{
		db: db, dbPath: dbPath, uploadDir: uploadDir, backupDir: backupDir, configPath: configPath,
		svc: svc, engine: engine, admin: admin, staff: staff,
	}
	env.adminToken = env.login(t, service.DefaultAdminUsername, backupAdminPassword)
	env.staffToken = env.login(t, backupStaffUsername, backupStaffPassword)
	return env
}

// login 走真实登录接口换取 token（不绕过 JWT 中间件）。
func (e *backupTestEnv) login(t *testing.T, username, password string) string {
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

// do 发起一次真实 HTTP 请求（带审计来源头）。
func (e *backupTestEnv) do(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Forwarded-For", "10.7.7.7")
	req.Header.Set("User-Agent", "backup-probe/1.0")
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// createBackup 以 admin 登录上下文触发一次备份（真实 service 调用）。
func (e *backupTestEnv) createBackup(t *testing.T, trigger service.BackupTrigger) *service.BackupResult {
	t.Helper()
	ctx := service.WithOperatorID(context.Background(), e.admin.ID)
	result, err := e.svc.Create(ctx, trigger)
	if err != nil {
		t.Fatalf("Create(%s): %v", trigger, err)
	}
	return result
}

// seedCustomer 直接落库客户（快照内容断言的前置数据）。
func (e *backupTestEnv) seedCustomer(t *testing.T, name string) *model.Customer {
	t.Helper()
	customer := &model.Customer{Name: name}
	if err := e.db.Create(customer).Error; err != nil {
		t.Fatalf("造客户失败: %v", err)
	}
	return customer
}

// seedUpload 在 UPLOAD_DIR 下写入一个上传文件。
func (e *backupTestEnv) seedUpload(t *testing.T, rel string, content []byte) string {
	t.Helper()
	path := filepath.Join(e.uploadDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("写上传文件 %s: %v", path, err)
	}
	return path
}

// readZip 把 zip 全部条目读为 name -> content。
func readZip(t *testing.T, path string) map[string][]byte {
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

// openSnapshot 把 zip 中的数据库快照落盘并以只读方式打开，断言 integrity_check=ok。
func openSnapshot(t *testing.T, data []byte) *sql.DB {
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

// countBackupZips 统计 BACKUP_DIR 下匹配 backup-*.zip 的文件数（文件系统真值）。
func countBackupZips(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && backupNamePattern.MatchString(entry.Name()) {
			count++
		}
	}
	return count
}

// zipKeys 输出 zip 条目名列表（失败信息可读）。
func zipKeys(entries map[string][]byte) []string {
	keys := make([]string, 0, len(entries))
	for name := range entries {
		keys = append(keys, name)
	}
	return keys
}
