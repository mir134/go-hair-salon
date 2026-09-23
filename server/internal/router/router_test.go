package router_test

// TestHealth 是 todo 5 的验收测试：
//   - GET /health 免认证返回 200 + code=0 信封（04-API.md:257-263）；
//   - 请求日志包含 方法/路径/状态/耗时/ip/ua（08-DEPLOYMENT.md:88-94）；
//   - DB 不可用时 /health 返回 500 信封且写“数据库错误”日志（不泄漏内部细节）；
//   - 日志不得出现 Authorization token。

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
)

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: 已迁移的临时数据库 + 写入内存缓冲的 logger。
	dbPath := filepath.Join(t.TempDir(), "health.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	engine := router.New(db, logger, router.Options{JWTSecret: "unit-test-secret"})

	// When: GET /health（无 Authorization）。
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Authorization", "Bearer super-secret-jwt-token")
	req.Header.Set("User-Agent", "health-probe/1.0")
	req.Header.Set("X-Forwarded-For", "10.0.0.9")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	// Then 1: 200 信封 + data.status=ok。
	if w.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v (body=%s)", err, w.Body.String())
	}
	if env.Code != 0 || env.Message != "success" {
		t.Errorf("envelope = %+v, want code=0 message=success", env)
	}
	var data struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.Status != "ok" {
		t.Errorf("data = %s (err=%v), want {\"status\":\"ok\"}", env.Data, err)
	}

	// Then 2: 请求日志包含 method/path/status/latency/ip/ua，且无 token。
	logs := logBuf.String()
	for _, want := range []string{
		"method=GET", "path=/health", "status=200", "latency_ms=", "ip=", "ua=", "http_request",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("请求日志缺少 %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "super-secret-jwt-token") {
		t.Errorf("日志泄漏 Authorization token:\n%s", logs)
	}

	// When: 数据库连接被关闭后访问 /health。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}
	logBuf.Reset()
	w2 := httptest.NewRecorder()
	engine.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/health", nil))

	// Then 3: 500 信封 + 数据库错误日志 + 无内部细节泄漏。
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("DB 关闭后 /health status = %d, want 500 (body=%s)", w2.Code, w2.Body.String())
	}
	body := w2.Body.String()
	var env2 struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env2); err != nil {
		t.Fatalf("500 响应不是 JSON 信封: %v (body=%s)", err, body)
	}
	if env2.Code != 50000 || env2.Message != "服务器内部错误" {
		t.Errorf("500 envelope = %+v, want code=50000 message=服务器内部错误", env2)
	}
	if strings.Contains(body, "database is closed") || strings.Contains(body, "goroutine") {
		t.Errorf("500 响应泄漏内部细节: %s", body)
	}
	if !strings.Contains(logBuf.String(), "数据库错误") {
		t.Errorf("DB 故障未写“数据库错误”日志:\n%s", logBuf.String())
	}
}
