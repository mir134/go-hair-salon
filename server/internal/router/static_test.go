package router_test

// TestStaticHosting* 是 todo 8 的验收测试（plan go-hair-salon-mvp todo 8）：
//   - 生产模式托管 web/dist：/ 与静态资源命中文件；
//   - SPA 回退：非 /api 的未命中路由（如 /customers）返回 index.html 200；
//   - /api 未命中路由返回统一 JSON 404 信封，且不得回退 HTML；
//   - 目录穿越探针不得读到 web/dist 之外的文件（config.yaml 等）。

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
)

const (
	// testIndexHTML 模拟 web/dist/index.html 的挂载点。
	testIndexHTML = `<!doctype html><html><body><div id="app"></div></body></html>`
	// testAssetJS 模拟带 hash 的构建产物，用于校验服务端确实在托管当前 dist。
	testAssetJS = `console.log("dist-current-marker")`
	// secretMarker 是 dist 之外“敏感文件”的内容，穿越探针命中即泄漏。
	secretMarker = "top-secret-outside-dist"
)

// writeTestFile 写入测试文件，失败即终止。
func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
}

// newDistRoot 在临时目录构造最小 web/dist，并在 dist 外放一份“敏感文件”供穿越探针使用。
func newDistRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "web", "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("创建 dist 失败: %v", err)
	}
	writeTestFile(t, filepath.Join(dist, "index.html"), testIndexHTML)
	writeTestFile(t, filepath.Join(dist, "assets", "app.js"), testAssetJS)
	writeTestFile(t, filepath.Join(root, "config.yaml"), "JWT_SECRET: "+secretMarker)
	return root
}

// newTestEngine 打开临时 DB 并装配引擎；调用方须先 t.Chdir 到静态目录解析基准。
func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "router-static.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	return router.New(db, logger, router.Options{JWTSecret: "unit-test-secret"})
}

// doRequest 直接经引擎发起请求并返回响应。
func doRequest(t *testing.T, engine *gin.Engine, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

// assertJSONNotFound 断言响应是 404 + 统一 JSON 信封（code=40400, data=null）。
func assertJSONNotFound(t *testing.T, w *httptest.ResponseRecorder, target string) {
	t.Helper()
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET %s status = %d, want 404 (body=%s)", target, w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("GET %s Content-Type = %q, want application/json; charset=utf-8", target, contentType)
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("GET %s 响应不是 JSON 信封: %v (body=%s)", target, err, w.Body.String())
	}
	if envelope.Code != 40400 || envelope.Message == "" || string(envelope.Data) != "null" {
		t.Errorf("GET %s envelope = %+v (data=%s), want code=40400 non-empty message data=null",
			target, envelope, envelope.Data)
	}
	if strings.Contains(w.Body.String(), `<div id="app">`) {
		t.Errorf("GET %s /api 未命中路由回退了 HTML: %s", target, w.Body.String())
	}
}

// TestStaticHostingSPAFallback 覆盖 happy path：/ 与静态资源命中文件，/customers 回退 index.html。
func TestStaticHostingSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: 工作目录内存在真实 web/dist（index.html + assets/app.js）。
	t.Chdir(newDistRoot(t))
	engine := newTestEngine(t)

	// When: GET /。
	w := doRequest(t, engine, http.MethodGet, "/")

	// Then: 200 + text/html + 含挂载点。
	if w.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", contentType)
	}
	if body := w.Body.String(); !strings.Contains(body, `<div id="app">`) {
		t.Errorf("GET / body 不含挂载点: %s", body)
	}

	// When: GET /customers（前端 client 路由，无对应静态文件）。
	w2 := doRequest(t, engine, http.MethodGet, "/customers")

	// Then: 200 且与 index.html 完全一致（SPA 回退）。
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /customers status = %d, want 200 (body=%s)", w2.Code, w2.Body.String())
	}
	if w2.Body.String() != w.Body.String() {
		t.Errorf("GET /customers 未回退同一 index.html:\nroot=%s\ncustomers=%s", w.Body.String(), w2.Body.String())
	}

	// When: GET /assets/app.js（命中静态资源）。
	w3 := doRequest(t, engine, http.MethodGet, "/assets/app.js")

	// Then: 200 + 当前 dist 内容（非缓存/非 index.html）。
	if w3.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.js status = %d, want 200 (body=%s)", w3.Code, w3.Body.String())
	}
	if body := w3.Body.String(); body != testAssetJS {
		t.Errorf("GET /assets/app.js body = %q, want %q", body, testAssetJS)
	}
	if contentType := w3.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Errorf("GET /assets/app.js Content-Type = %q, want javascript", contentType)
	}
}

// TestAPINotFoundReturnsJSONEnvelope 覆盖 /api 未命中路由的 JSON 404 契约。
func TestAPINotFoundReturnsJSONEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: 静态托管已启用（确保 /api 不会被 SPA 回退“截胡”）。
	t.Chdir(newDistRoot(t))
	engine := newTestEngine(t)

	// When/Then: 多形态 /api 路径均返回 JSON 404 信封。
	for _, target := range []string{"/api", "/api/", "/api/v1/nonexist", "/api/v1/customers/999"} {
		assertJSONNotFound(t, doRequest(t, engine, http.MethodGet, target), target)
	}

	// When: POST /api/v1/nonexist（非 GET 也不得回退 HTML）。
	// Then: 同样 JSON 404。
	assertJSONNotFound(t, doRequest(t, engine, http.MethodPost, "/api/v1/nonexist"), "POST /api/v1/nonexist")
}

// TestStaticPathTraversalDoesNotLeak 覆盖穿越探针：dist 之外的 config.yaml 绝不可达。
func TestStaticPathTraversalDoesNotLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: dist 外存在含敏感标记的 config.yaml。
	t.Chdir(newDistRoot(t))
	engine := newTestEngine(t)

	probes := []string{
		"/../config.yaml",           // 明文 ..
		"/assets/../../config.yaml", // 多级 ..
		"/..%2fconfig.yaml",         // %2f 编码分隔符
		"/%2e%2e%2fconfig.yaml",     // %2e 编码点 + %2f
		"/..%5cconfig.yaml",         // %5c（Windows 反斜杠）
		"/%2e%2e%5cconfig.yaml",     // 编码点 + 编码反斜杠
	}
	for _, target := range probes {
		// When: 发起穿越请求。
		w := doRequest(t, engine, http.MethodGet, target)

		// Then: 拒绝为 JSON 404 信封，响应体不含 dist 外文件内容。
		assertJSONNotFound(t, w, target)
		if strings.Contains(w.Body.String(), secretMarker) {
			t.Errorf("GET %s 泄漏了 dist 之外的文件内容: %s", target, w.Body.String())
		}
	}
}

// TestNoRouteWithoutDistIsAPIOnly 覆盖未找到 web/dist 时的降级：不 panic，API 仍是 JSON 404。
func TestNoRouteWithoutDistIsAPIOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: 工作目录内没有 web/dist。
	t.Chdir(t.TempDir())
	engine := newTestEngine(t)

	// When: GET /customers（无静态产物可回退）。
	w := doRequest(t, engine, http.MethodGet, "/customers")

	// Then: JSON 404 信封（降级为仅 API 模式，而非 500/panic）。
	assertJSONNotFound(t, w, "/customers")

	// When/Then: /api 未命中在降级模式下仍是 JSON 404。
	assertJSONNotFound(t, doRequest(t, engine, http.MethodGet, "/api/v1/nonexist"), "/api/v1/nonexist")
}
