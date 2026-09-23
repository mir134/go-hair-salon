package controller_test

// TestUsersAdmin 是 todo 39 的验收测试（计划：`go test ./internal/controller -run TestUsersAdmin -v -count=1`）：
//   - GET/POST /users、PUT /users/:id 仅 admin（D6 决议：无 04 契约的最小化新增）→ staff 一律 403 且零写入；
//   - POST /users：username/role 仅 admin|staff/password/employee_id 可选；重复 username → 409；
//     未知 employee_id → 400；密码只落 bcrypt 哈希（禁止明文）；
//   - PUT /users/:id：status 停用/启用（无物理删除，users 走 status，06 §11:120-125）、重置密码；
//   - 停用后其 token 立即失效（JWTAuth 每请求实时查库）→ 401；重新启用后可登录；
//   - 重置密码：新密码可登录、旧密码 401；
//   - malformed_input：非法 id/role/status/JSON/超长密码 → 400，不存在 → 404，绝不 500；
//   - 审计：user_create / user_disable / user_enable / user_password_reset 写 operation_logs（事务外）。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// createUserAPI 通过 API 建用户并断言 201，返回 DTO（userData 见 auth_test.go）。
func (e *customerEnv) createUserAPI(t *testing.T, token, body string) userData {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/users", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /users status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created userData
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// countUsers 统计 users 表行数。
func (e *customerEnv) countUsers(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&model.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

// userRow 直接从数据库读取用户行（用于禁止物理删除/落库断言）。
func (e *customerEnv) userRow(t *testing.T, id int64) model.User {
	t.Helper()
	var row model.User
	if err := e.db.First(&row, id).Error; err != nil {
		t.Fatalf("用户 #%d 落库行不存在: %v", id, err)
	}
	return row
}

// rawLogin 走真实登录接口并返回原始响应（用于断言失败状态码）。
func (e *customerEnv) rawLogin(username, password string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	return e.do(http.MethodPost, "/api/v1/auth/login", body, nil)
}

// countUserLogs 统计指定 action 的审计条数。
func (e *customerEnv) countUserLogs(t *testing.T, action string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&model.OperationLog{}).
		Where("action = ? AND target_type = ?", action, "user").
		Count(&n).Error; err != nil {
		t.Fatalf("count operation_logs(action=%s): %v", action, err)
	}
	return n
}

func TestUsersAdmin(t *testing.T) {
	env := newCustomerEnv(t)

	t.Run("staff_is_forbidden_with_zero_writes", func(t *testing.T) {
		usersBefore := env.countUsers(t)
		logsBefore := env.countOperationLogs(t, "")

		cases := []struct{ method, path, body string }{
			{http.MethodGet, "/api/v1/users", ""},
			{http.MethodPost, "/api/v1/users", `{"username":"hacker","role":"staff","password":"Hack-Pwd-1"}`},
			{http.MethodPut, "/api/v1/users/1", `{"status":0}`},
		}
		for _, tc := range cases {
			w := env.authed(tc.method, tc.path, tc.body, env.staffToken)
			if w.Code != http.StatusForbidden {
				t.Errorf("staff %s %s status = %d, want 403 (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
				continue
			}
			if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
				t.Errorf("staff %s %s code = %d, want %d", tc.method, tc.path, envl.Code, service.CodeForbidden)
			}
		}

		if got := env.countUsers(t); got != usersBefore {
			t.Errorf("staff 403 后 users 行数 = %d, want %d（零写入）", got, usersBefore)
		}
		if got := env.countOperationLogs(t, ""); got != logsBefore {
			t.Errorf("staff 403 后 operation_logs = %d, want %d（零审计）", got, logsBefore)
		}
	})

	t.Run("create_linked_staff_and_duplicate_conflict", func(t *testing.T) {
		// Given: 先经员工 API 建一个员工（todo 38）。
		emp := env.createEmployee(t, env.adminToken, `{"name":"用户关联员工","position":"技师"}`)
		usersBefore := env.countUsers(t)

		// When: admin 创建关联员工的 staff 账号。
		created := env.createUserAPI(t, env.adminToken,
			fmt.Sprintf(`{"username":"staff-u1","role":"staff","password":"Staff-U1-Pwd","employee_id":%d}`, emp.ID))

		// Then: DTO 正确、默认启用、employee_id 关联（DTO 与落库一致）。
		if created.ID <= 0 || created.Username != "staff-u1" || created.Role != model.RoleStaff ||
			created.EmployeeID == nil || *created.EmployeeID != emp.ID || created.Status != model.StatusEnabled {
			t.Errorf("创建用户 DTO = %+v, want staff-u1/staff/employee=%d/status=1", created, emp.ID)
		}
		row := env.userRow(t, created.ID)
		if row.Role != model.RoleStaff || row.EmployeeID == nil || *row.EmployeeID != emp.ID || row.Status != model.StatusEnabled {
			t.Errorf("落库用户 = %+v, want role=staff/employee=%d/status=1", row, emp.ID)
		}

		// Then: 密码只存 bcrypt 哈希（非明文，且可校验）。
		if row.PasswordHash == "Staff-U1-Pwd" || !strings.HasPrefix(row.PasswordHash, "$2a$") {
			t.Errorf("password_hash = %q, want bcrypt 哈希（非明文）", row.PasswordHash)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("Staff-U1-Pwd")); err != nil {
			t.Errorf("创建密码 bcrypt 校验失败: %v", err)
		}

		// Then: 新账号可登录。
		if token := env.login(t, "staff-u1", "Staff-U1-Pwd"); token == "" {
			t.Error("新账号登录 token 为空")
		}

		// --- When: 重复 username ---
		w := env.authed(http.MethodPost, "/api/v1/users",
			`{"username":"staff-u1","role":"staff","password":"Other-Pwd-1"}`, env.adminToken)

		// --- Then: 409，且未新增行 ---
		if w.Code != http.StatusConflict {
			t.Fatalf("重复 username status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeConflict {
			t.Errorf("重复 username code = %d, want %d", envl.Code, service.CodeConflict)
		}
		if got := env.countUsers(t); got != usersBefore+1 {
			t.Errorf("重复创建后 users 行数 = %d, want %d", got, usersBefore+1)
		}

		// --- Then: 列表包含新用户；?status=1 含、?status=0 不含 ---
		w = env.authed(http.MethodGet, "/api/v1/users?status=1", "", env.adminToken)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /users status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		var list []userData
		decodeData(t, decodeEnvelope(t, w), &list)
		found := false
		for _, u := range list {
			if u.ID == created.ID {
				found = true
				if u.EmployeeID == nil || *u.EmployeeID != emp.ID {
					t.Errorf("列表中 employee_id = %v, want %d", u.EmployeeID, emp.ID)
				}
			}
			if u.Status != model.StatusEnabled {
				t.Errorf("?status=1 列表含停用用户: %+v", u)
			}
		}
		if !found {
			t.Errorf("?status=1 列表未包含新建用户 #%d", created.ID)
		}
	})

	t.Run("disable_invalidates_existing_token", func(t *testing.T) {
		// Given: 一个可登录的 staff 账号及其真实 token。
		created := env.createUserAPI(t, env.adminToken,
			`{"username":"staff-u2","role":"staff","password":"Staff-U2-Pwd"}`)
		token := env.login(t, "staff-u2", "Staff-U2-Pwd")
		usersBefore := env.countUsers(t)

		// --- When: admin 停用该账号（PUT /users/:id status=0） ---
		w := env.authed(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", created.ID), `{"status":0}`, env.adminToken)

		// --- Then: 200 + DTO status=0 ---
		if w.Code != http.StatusOK {
			t.Fatalf("停用用户 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		var updated userData
		decodeData(t, decodeEnvelope(t, w), &updated)
		if updated.Status != model.StatusDisabled {
			t.Errorf("停用后 DTO status = %d, want 0", updated.Status)
		}

		// --- Then: 行仍在（禁止物理删除，06 §11:120-125）、status=0 ---
		if row := env.userRow(t, created.ID); row.Status != model.StatusDisabled {
			t.Errorf("停用落库 status = %d, want 0（行保留）", row.Status)
		}
		if got := env.countUsers(t); got != usersBefore {
			t.Errorf("停用后 users 行数 = %d, want %d（禁止物理删除）", got, usersBefore)
		}

		// --- Then: 旧 token 立即失效（JWTAuth 每请求实时查库）→ 401 ---
		w = env.authed(http.MethodGet, "/api/v1/auth/me", "", token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("停用后旧 token GET /auth/me status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeUnauthorized {
			t.Errorf("停用后旧 token code = %d, want %d", envl.Code, service.CodeUnauthorized)
		}

		// --- Then: 停用账号重新登录 → 403（不得签发新 token） ---
		if w := env.rawLogin("staff-u2", "Staff-U2-Pwd"); w.Code != http.StatusForbidden {
			t.Errorf("停用账号登录 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}

		// --- When: 重新启用（status=1） ---
		w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", created.ID), `{"status":1}`, env.adminToken)

		// --- Then: 200 且可再次登录 ---
		if w.Code != http.StatusOK {
			t.Fatalf("启用用户 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if row := env.userRow(t, created.ID); row.Status != model.StatusEnabled {
			t.Errorf("启用落库 status = %d, want 1", row.Status)
		}
		if token := env.login(t, "staff-u2", "Staff-U2-Pwd"); token == "" {
			t.Error("重新启用后登录 token 为空")
		}
	})

	t.Run("reset_password_replaces_old", func(t *testing.T) {
		created := env.createUserAPI(t, env.adminToken,
			`{"username":"staff-u3","role":"staff","password":"Old-Pwd-333"}`)
		oldHash := env.userRow(t, created.ID).PasswordHash

		// --- When: admin 重置密码 ---
		w := env.authed(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", created.ID), `{"password":"New-Pwd-333"}`, env.adminToken)

		// --- Then: 200 + 落库哈希改变、bcrypt 校验新密码 ---
		if w.Code != http.StatusOK {
			t.Fatalf("重置密码 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		row := env.userRow(t, created.ID)
		if row.PasswordHash == oldHash {
			t.Error("重置密码后哈希未变化")
		}
		if row.PasswordHash == "New-Pwd-333" {
			t.Error("password_hash 存了明文密码")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("New-Pwd-333")); err != nil {
			t.Errorf("新密码 bcrypt 校验失败: %v", err)
		}

		// --- Then: 旧密码 401、新密码 200 ---
		if w := env.rawLogin("staff-u3", "Old-Pwd-333"); w.Code != http.StatusUnauthorized {
			t.Errorf("旧密码登录 status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
		if token := env.login(t, "staff-u3", "New-Pwd-333"); token == "" {
			t.Error("新密码登录 token 为空")
		}
	})

	t.Run("malformed_input", func(t *testing.T) {
		weak := env.createUserAPI(t, env.adminToken,
			`{"username":"staff-u4","role":"staff","password":"Staff-U4-Pwd"}`)

		malformed := []struct {
			method, path, body string
			wantStatus         int
		}{
			{http.MethodPost, "/api/v1/users", `{"username":"","role":"staff","password":"x-Pwd-1"}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"no-role","role":"","password":"x-Pwd-1"}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"bad-role","role":"superadmin","password":"x-Pwd-1"}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"no-pwd","role":"staff","password":""}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"ghost-link","role":"staff","password":"x-Pwd-1","employee_id":99999}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"neg-link","role":"staff","password":"x-Pwd-1","employee_id":-1}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{"username":"long-pwd","role":"staff","password":"` + strings.Repeat("a", 80) + `"}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/users", `{bad json`, http.StatusBadRequest},
			{http.MethodPut, "/api/v1/users/abc", `{"status":0}`, http.StatusBadRequest},
			{http.MethodPut, "/api/v1/users/0", `{"status":0}`, http.StatusBadRequest},
			{http.MethodPut, fmt.Sprintf("/api/v1/users/%d", weak.ID), `{}`, http.StatusBadRequest},
			{http.MethodPut, fmt.Sprintf("/api/v1/users/%d", weak.ID), `{"status":7}`, http.StatusBadRequest},
			{http.MethodPut, fmt.Sprintf("/api/v1/users/%d", weak.ID), `{"password":""}`, http.StatusBadRequest},
			{http.MethodPut, "/api/v1/users/99999", `{"status":0}`, http.StatusNotFound},
			{http.MethodPut, "/api/v1/users/99999", `{"password":"Whatever-Pwd-1"}`, http.StatusNotFound},
		}
		for _, tc := range malformed {
			w := env.authed(tc.method, tc.path, tc.body, env.adminToken)
			if w.Code != tc.wantStatus {
				t.Errorf("%s %s status = %d, want %d (body=%s)", tc.method, tc.path, w.Code, tc.wantStatus, w.Body.String())
			}
			if w.Code >= http.StatusInternalServerError {
				t.Errorf("%s %s 返回 5xx（malformed 输入不得 500）: %s", tc.method, tc.path, w.Body.String())
			}
		}

		// Then: 失败路径未产生多余用户（弱密码/非法角色等都未落库）。
		usersBefore := env.countUsers(t)
		if w := env.authed(http.MethodPost, "/api/v1/users", `{"username":"bad-role","role":"superadmin","password":"x-Pwd-1"}`, env.adminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("再次非法创建 status = %d, want 400", w.Code)
		}
		if got := env.countUsers(t); got != usersBefore {
			t.Errorf("非法创建后 users 行数 = %d, want %d（零写入）", got, usersBefore)
		}
	})

	t.Run("audit_logs_written", func(t *testing.T) {
		for _, action := range []string{"user_create", "user_disable", "user_enable", "user_password_reset"} {
			var row model.OperationLog
			err := env.db.Where("action = ? AND target_type = ?", action, "user").
				Order("id DESC").First(&row).Error
			if err != nil {
				t.Fatalf("缺少审计 action=%s: %v", action, err)
			}
			if row.OperatorID == nil || *row.OperatorID != env.admin.ID {
				t.Errorf("action=%s operator_id = %v, want %d", action, row.OperatorID, env.admin.ID)
			}
			if row.TargetID <= 0 {
				t.Errorf("action=%s target_id = %d, want > 0", action, row.TargetID)
			}
		}
		// 审计内容不得出现密码（明文）。
		var leaked int64
		if err := env.db.Model(&model.OperationLog{}).
			Where("action LIKE 'user_%' AND (content LIKE ? OR content LIKE ?)", "%Pwd%", "%password%").
			Count(&leaked).Error; err != nil {
			t.Fatalf("检查审计内容泄露: %v", err)
		}
		if leaked != 0 {
			t.Errorf("审计日志疑似包含密码内容: %d 条", leaked)
		}
	})
}
