package controller_test

// TestResponseEnvelope 是 todo 4 的验收测试（计划：
// `go test ./internal/controller -run TestResponseEnvelope`）：
//   - 成功/失败/分页三种信封形态（04-API.md:13-44）；
//   - 业务错误码 40001 起的 HTTP 状态映射
//     （200/201/400/401/403/404/409/422/500，04-API.md:265-275）；
//   - panic recovery 返回 500 信封且 body 无 goroutine/stack trace；
//   - malformed JSON 返回 400 信封而非 panic/500。

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestResponseEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	slog.SetDefault(logger)

	router := newEnvelopeTestRouter(logger)

	t.Run("200 success envelope", func(t *testing.T) {
		w := doRequest(t, router, http.MethodGet, "/ok", "")
		env := assertEnvelope(t, w, http.StatusOK, 0, "success")
		var data struct {
			Hello string `json:"hello"`
		}
		decodeData(t, env, &data)
		if data.Hello != "world" {
			t.Errorf("data.hello = %q, want world", data.Hello)
		}
	})

	t.Run("201 created envelope", func(t *testing.T) {
		w := doRequest(t, router, http.MethodGet, "/created", "")
		env := assertEnvelope(t, w, http.StatusCreated, 0, "success")
		var data struct {
			ID int64 `json:"id"`
		}
		decodeData(t, env, &data)
		if data.ID != 1 {
			t.Errorf("data.id = %d, want 1", data.ID)
		}
	})

	t.Run("paginated envelope", func(t *testing.T) {
		w := doRequest(t, router, http.MethodGet, "/paged", "")
		env := assertEnvelope(t, w, http.StatusOK, 0, "success")
		var page controller.PageData
		decodeData(t, env, &page)
		if page.Total != 100 || page.Page != 2 || page.PageSize != 20 {
			t.Errorf("page = %+v, want total=100 page=2 page_size=20", page)
		}
		if items, ok := page.Items.([]any); !ok || len(items) != 0 {
			t.Errorf("items = %#v, want 空数组 []", page.Items)
		}
	})

	statusCases := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   int
		wantMsg    string
	}{
		{"400 invalid params", "/bad", http.StatusBadRequest, 40000, "参数错误"},
		{"401 unauthorized", "/unauth", http.StatusUnauthorized, 40100, "未登录"},
		{"403 forbidden", "/forbidden", http.StatusForbidden, 40300, "无权限"},
		{"404 customer not found", "/missing", http.StatusNotFound, 40001, "客户不存在"},
		{"409 conflict", "/conflict", http.StatusConflict, 40900, "业务冲突"},
		{"422 validation", "/invalid", http.StatusUnprocessableEntity, 42200, "业务校验失败"},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(t, router, http.MethodGet, tc.path, "")
			env := assertEnvelope(t, w, tc.wantStatus, tc.wantCode, tc.wantMsg)
			if string(env.Data) != "null" {
				t.Errorf("失败信封 data = %s, want null", env.Data)
			}
		})
	}

	t.Run("500 unhandled error hides internals", func(t *testing.T) {
		w := doRequest(t, router, http.MethodGet, "/boom", "")
		env := assertEnvelope(t, w, http.StatusInternalServerError, 50000, "服务器内部错误")
		if strings.Contains(string(env.Data), "db down") {
			t.Errorf("500 响应泄漏内部错误: %s", env.Data)
		}
	})

	t.Run("malformed JSON -> 400 envelope", func(t *testing.T) {
		w := doRequest(t, router, http.MethodPost, "/bind", `{"name":`)
		assertEnvelope(t, w, http.StatusBadRequest, 40000, "请求体 JSON 无效")
	})

	t.Run("valid JSON -> 200 envelope", func(t *testing.T) {
		w := doRequest(t, router, http.MethodPost, "/bind", `{"name":"ok"}`)
		env := assertEnvelope(t, w, http.StatusOK, 0, "success")
		var data struct {
			Name string `json:"name"`
		}
		decodeData(t, env, &data)
		if data.Name != "ok" {
			t.Errorf("data.name = %q, want ok", data.Name)
		}
	})

	t.Run("panic -> 500 envelope without stack trace", func(t *testing.T) {
		w := doRequest(t, router, http.MethodGet, "/panic", "")
		env := assertEnvelope(t, w, http.StatusInternalServerError, 50000, "服务器内部错误")
		body := w.Body.String()
		for _, leaked := range []string{"goroutine", "runtime/debug", "panic-marker", "panic("} {
			if strings.Contains(body, leaked) {
				t.Errorf("panic 响应泄漏内部信息 %q: %s", leaked, body)
			}
		}
		if string(env.Data) != "null" {
			t.Errorf("panic 信封 data = %s, want null", env.Data)
		}
	})

	// panic 必须写入服务端日志（含堆栈），但仅存在于服务端。
	logs := logBuf.String()
	if !strings.Contains(logs, "panic recovered") {
		t.Errorf("panic 未写日志: %s", logs)
	}
	if !strings.Contains(logs, "panic-marker") {
		t.Errorf("panic 日志缺少 panic 值: %s", logs)
	}
}

// --- 测试脚手架 ---

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newEnvelopeTestRouter(logger *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.Recovery(logger))
	r.GET("/ok", func(c *gin.Context) { controller.Success(c, gin.H{"hello": "world"}) })
	r.GET("/created", func(c *gin.Context) { controller.Created(c, gin.H{"id": 1}) })
	r.GET("/paged", func(c *gin.Context) {
		controller.Success(c, controller.PageData{Items: []any{}, Total: 100, Page: 2, PageSize: 20})
	})
	r.GET("/bad", func(c *gin.Context) { controller.Fail(c, service.BadRequest("参数错误")) })
	r.GET("/unauth", func(c *gin.Context) { controller.Fail(c, service.Unauthorized("未登录")) })
	r.GET("/forbidden", func(c *gin.Context) { controller.Fail(c, service.Forbidden("无权限")) })
	r.GET("/missing", func(c *gin.Context) { controller.Fail(c, service.ErrCustomerNotFound) })
	r.GET("/conflict", func(c *gin.Context) { controller.Fail(c, service.Conflict("业务冲突")) })
	r.GET("/invalid", func(c *gin.Context) { controller.Fail(c, service.Validation("业务校验失败")) })
	r.GET("/boom", func(c *gin.Context) {
		controller.Fail(c, errors.New("db down: secret internal dsn"))
	})
	r.GET("/panic", func(c *gin.Context) { panic("panic-marker") })
	r.POST("/bind", func(c *gin.Context) {
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			controller.Fail(c, service.BadRequest("请求体 JSON 无效"))
			return
		}
		controller.Success(c, gin.H{"name": req.Name})
	})
	return r
}

func doRequest(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertEnvelope(t *testing.T, w *httptest.ResponseRecorder, wantStatus, wantCode int, wantMessage string) envelope {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("HTTP status = %d, want %d (body=%s)", w.Code, wantStatus, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是合法 JSON 信封: %v (body=%s)", err, w.Body.String())
	}
	if env.Code != wantCode {
		t.Errorf("envelope.code = %d, want %d (body=%s)", env.Code, wantCode, w.Body.String())
	}
	if env.Message != wantMessage {
		t.Errorf("envelope.message = %q, want %q", env.Message, wantMessage)
	}
	if len(env.Data) == 0 {
		t.Error("envelope.data 缺失（成功应为对象/数组，失败应为 null）")
	}
	return env
}

func decodeData(t *testing.T, env envelope, target any) {
	t.Helper()
	if err := json.Unmarshal(env.Data, target); err != nil {
		t.Fatalf("解析 data 失败: %v (data=%s)", err, env.Data)
	}
}
