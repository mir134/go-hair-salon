package controller_test

// TestAuthMalformed 覆盖 todo 10 的畸形输入（adversarial: malformed_input）：
// 非法 JSON、缺字段、无 token、乱码 token、错误签名、过期 token、错误认证方案。
// 所有失败都必须返回统一失败信封，不得泄漏内部细节。

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestAuthMalformed(t *testing.T) {
	env := newAuthEnv(t)

	// Given: 合法登录取得的 token（用于对比场景）。
	w := env.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"Admin-Pwd-1"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("前置登录失败: status=%d body=%s", w.Code, w.Body.String())
	}
	var login loginData
	decodeData(t, decodeEnvelope(t, w), &login)

	// When/Then: 非法 JSON 请求体 → 400 + 40000 信封。
	w = env.do(http.MethodPost, "/api/v1/auth/login", `{"username":`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("非法 JSON status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeInvalidParams || string(envl.Data) != "null" {
		t.Errorf("非法 JSON 信封 = %+v, want code=40000 data=null", envl)
	}

	// When/Then: 缺字段（空用户名/密码）→ 400。
	for name, body := range map[string]string{
		"空用户名":   `{"username":"","password":"x"}`,
		"空密码":    `{"username":"admin","password":""}`,
		"空 JSON": `{}`,
	} {
		w = env.do(http.MethodPost, "/api/v1/auth/login", body, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400 (body=%s)", name, w.Code, w.Body.String())
		}
	}

	// When/Then: 无 token → 401。
	w = env.do(http.MethodGet, "/api/v1/auth/me", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无 token status = %d, want 401", w.Code)
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeUnauthorized {
		t.Errorf("无 token code = %d, want %d", envl.Code, service.CodeUnauthorized)
	}

	// When/Then: 乱码 token / 错误 scheme / 空 Bearer / 无方案裸 token → 401。
	for name, header := range map[string]string{
		"乱码 token":   "Bearer not-a-jwt",
		"Basic 方案":   "Basic YWRtaW46cHc=",
		"空 Bearer":   "Bearer ",
		"无方案裸 token": login.Token,
	} {
		w = env.do(http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Authorization": header})
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401 (body=%s)", name, w.Code, w.Body.String())
		}
	}

	// When: 用错误密钥签发的 token（伪造签名）。
	forged, _, err := service.NewTokenService("wrong-secret", time.Hour).Issue(env.admin)
	if err != nil {
		t.Fatalf("签发伪造 token: %v", err)
	}
	w = env.do(http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Authorization": "Bearer " + forged})

	// Then: 拒绝（401）。
	if w.Code != http.StatusUnauthorized {
		t.Errorf("错误签名 token status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}

	// When: 已过期 token（负 TTL 签发）。
	expired, _, err := service.NewTokenService(authTestSecret, -time.Minute).Issue(env.admin)
	if err != nil {
		t.Fatalf("签发过期 token: %v", err)
	}
	w = env.do(http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Authorization": "Bearer " + expired})

	// Then: 拒绝（401）且不泄漏解析细节。
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("过期 token status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "token is expired") || strings.Contains(body, "expired") {
		t.Errorf("401 响应泄漏内部解析细节: %s", body)
	}
}
