package controller_test

// TestEmployees 是 todo 38 的验收测试（计划：`go test ./internal/controller -run TestEmployees -v -count=1`）：
//   - GET/POST/GET:id/PUT/DELETE /employees 全部仅 admin（04-API.md:194-204、06 §7）→ staff 一律 403 且零写入；
//   - CRUD 正确：新建字段校验、status 显式停用、joined_at 解析（date 与 RFC3339）、列表 ?status= 过滤；
//   - DELETE = 停用（status=0，行保留；employees 表无 deleted_at，D7），重复 DELETE 幂等；
//   - 被订单引用的员工永不物理删除：DELETE 后订单仍在、订单详情的 employee_name 仍可读（历史不失效）；
//   - malformed_input：非法 id/status/joined_at/JSON → 400，不存在 → 404，绝不 500；
//   - 审计：employee_create / employee_update / employee_disable 写 operation_logs（target_type=employee）。

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// employeeAPIView 是员工 DTO 的测试镜像（03-DATABASE.md:31-42）。
type employeeAPIView struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Phone    string     `json:"phone"`
	Avatar   string     `json:"avatar"`
	Position string     `json:"position"`
	Status   int        `json:"status"`
	JoinedAt *time.Time `json:"joined_at"`
	Remark   string     `json:"remark"`
}

// createEmployee 通过 API 建员工并断言 201，返回 DTO。
func (e *customerEnv) createEmployee(t *testing.T, token, body string) employeeAPIView {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/employees", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /employees status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created employeeAPIView
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// listEmployees 通过 API 拉取员工数组（query 可为空）。
func (e *customerEnv) listEmployees(t *testing.T, token, query string) []employeeAPIView {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/employees"+query, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /employees%s status = %d, want 200 (body=%s)", query, w.Code, w.Body.String())
	}
	var items []employeeAPIView
	decodeData(t, decodeEnvelope(t, w), &items)
	return items
}

// employeeRow 直接从数据库读取员工行（用于「行仍在/status=0」的落库断言）。
func (e *customerEnv) employeeRow(t *testing.T, id int64) model.Employee {
	t.Helper()
	var row model.Employee
	if err := e.db.First(&row, id).Error; err != nil {
		t.Fatalf("员工 #%d 落库行不存在: %v", id, err)
	}
	return row
}

// countEmployees 统计 employees 表行数。
func (e *customerEnv) countEmployees(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&model.Employee{}).Count(&n).Error; err != nil {
		t.Fatalf("count employees: %v", err)
	}
	return n
}

// countOperationLogs 统计审计日志行数（action 为空表示不过滤）。
func (e *customerEnv) countOperationLogs(t *testing.T, action string) int64 {
	t.Helper()
	query := e.db.Model(&model.OperationLog{})
	if action != "" {
		query = query.Where("action = ?", action)
	}
	var n int64
	if err := query.Count(&n).Error; err != nil {
		t.Fatalf("count operation_logs: %v", err)
	}
	return n
}

func TestEmployees(t *testing.T) {
	env := newCustomerEnv(t)

	t.Run("staff_is_forbidden_with_zero_writes", func(t *testing.T) {
		employeesBefore := env.countEmployees(t)
		logsBefore := env.countOperationLogs(t, "")

		cases := []struct{ method, path, body string }{
			{http.MethodGet, "/api/v1/employees", ""},
			{http.MethodPost, "/api/v1/employees", `{"name":"无权限员工"}`},
			{http.MethodGet, "/api/v1/employees/1", ""},
			{http.MethodPut, "/api/v1/employees/1", `{"name":"无权限员工"}`},
			{http.MethodDelete, "/api/v1/employees/1", ""},
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

		if got := env.countEmployees(t); got != employeesBefore {
			t.Errorf("staff 403 后 employees 行数 = %d, want %d（零写入）", got, employeesBefore)
		}
		if got := env.countOperationLogs(t, ""); got != logsBefore {
			t.Errorf("staff 403 后 operation_logs = %d, want %d（零审计）", got, logsBefore)
		}
	})

	t.Run("crud_lifecycle_and_validation", func(t *testing.T) {
		// --- When: admin 建员工（全字段 + date 形式 joined_at） ---
		wang := env.createEmployee(t, env.adminToken,
			`{"name":"王师傅","phone":"13900000001","avatar":"/uploads/wang.png","position":"发型师","joined_at":"2024-05-01","remark":"老员工"}`)

		// --- Then: DTO 回显，status 默认启用，joined_at 解析为 UTC 零点 ---
		wantJoined := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
		if wang.ID <= 0 || wang.Name != "王师傅" || wang.Phone != "13900000001" ||
			wang.Avatar != "/uploads/wang.png" || wang.Position != "发型师" || wang.Remark != "老员工" ||
			wang.Status != model.StatusEnabled {
			t.Errorf("创建员工 DTO = %+v, want 王师傅/发型师/status=1", wang)
		}
		if wang.JoinedAt == nil || !wang.JoinedAt.Equal(wantJoined) {
			t.Errorf("joined_at = %v, want %v", wang.JoinedAt, wantJoined)
		}
		if row := env.employeeRow(t, wang.ID); row.Status != model.StatusEnabled || row.Name != "王师傅" {
			t.Errorf("落库员工 = %+v, want status=1/name=王师傅", row)
		}

		// --- When: admin 创建即停用（status=0）+ RFC3339 joined_at ---
		li := env.createEmployee(t, env.adminToken,
			`{"name":"李师傅","status":0,"joined_at":"2024-06-02T10:00:00Z"}`)

		// --- Then: 落库 status=0（不得被 GORM default 覆盖） ---
		if li.Status != model.StatusDisabled {
			t.Errorf("创建即停用 status = %d, want 0", li.Status)
		}
		if row := env.employeeRow(t, li.ID); row.Status != model.StatusDisabled {
			t.Errorf("落库创建即停用 status = %d, want 0", row.Status)
		}
		if li.JoinedAt == nil || !li.JoinedAt.Equal(time.Date(2024, 6, 2, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("RFC3339 joined_at = %v, want 2024-06-02T10:00:00Z", li.JoinedAt)
		}

		// --- Then: 列表含全部（含停用），?status= 过滤正确 ---
		all := env.listEmployees(t, env.adminToken, "")
		if len(all) != 2 || all[0].ID != wang.ID || all[1].ID != li.ID {
			t.Errorf("员工列表 = %+v, want [%d,%d]（id 升序）", all, wang.ID, li.ID)
		}
		if enabled := env.listEmployees(t, env.adminToken, "?status=1"); len(enabled) != 1 || enabled[0].ID != wang.ID {
			t.Errorf("?status=1 = %+v, want [%d]", enabled, wang.ID)
		}
		if disabled := env.listEmployees(t, env.adminToken, "?status=0"); len(disabled) != 1 || disabled[0].ID != li.ID {
			t.Errorf("?status=0 = %+v, want [%d]", disabled, li.ID)
		}
		if bad := env.listEmployees(t, env.adminToken, "?status=abc"); len(bad) != 2 {
			t.Errorf("?status=abc 宽松回退 = %d 条, want 2", len(bad))
		}

		// --- When: GET 详情 ---
		w := env.authed(http.MethodGet, fmt.Sprintf("/api/v1/employees/%d", wang.ID), "", env.adminToken)

		// --- Then: 200 且字段一致 ---
		if w.Code != http.StatusOK {
			t.Fatalf("GET /employees/%d status = %d, want 200 (body=%s)", wang.ID, w.Code, w.Body.String())
		}
		var got employeeAPIView
		decodeData(t, decodeEnvelope(t, w), &got)
		if got.ID != wang.ID || got.Name != "王师傅" || got.Status != model.StatusEnabled {
			t.Errorf("GET 详情 = %+v, want id=%d/status=1", got, wang.ID)
		}

		// --- When: PUT 改成停用（显式 status=0） ---
		w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/employees/%d", wang.ID),
			`{"name":"王师傅","phone":"13900000009","position":"高级发型师","status":0,"remark":"转兼职"}`, env.adminToken)

		// --- Then: 200 + 落库 status=0/新字段 ---
		if w.Code != http.StatusOK {
			t.Fatalf("PUT /employees/%d status = %d, want 200 (body=%s)", wang.ID, w.Code, w.Body.String())
		}
		var updated employeeAPIView
		decodeData(t, decodeEnvelope(t, w), &updated)
		if updated.Status != model.StatusDisabled || updated.Position != "高级发型师" || updated.Phone != "13900000009" {
			t.Errorf("PUT 后 DTO = %+v, want status=0/高级发型师/13900000009", updated)
		}
		if row := env.employeeRow(t, wang.ID); row.Status != model.StatusDisabled || row.Position != "高级发型师" {
			t.Errorf("PUT 落库 = %+v, want status=0/高级发型师", row)
		}

		// --- When: PUT 不带 status（应保持原值 0，不被默认值重置） ---
		w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/employees/%d", wang.ID),
			`{"name":"王师傅","remark":"仅改备注"}`, env.adminToken)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT（不带 status）status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if row := env.employeeRow(t, wang.ID); row.Status != model.StatusDisabled || row.Remark != "仅改备注" {
			t.Errorf("PUT 不带 status 落库 = %+v, want status=0/仅改备注（保持原值）", row)
		}

		// --- malformed_input：不得 500 ---
		malformed := []struct {
			method, path, body string
			wantStatus         int
		}{
			{http.MethodPost, "/api/v1/employees", `{"name":""}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/employees", `{"name":"x","status":7}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/employees", `{"name":"x","joined_at":"not-a-date"}`, http.StatusBadRequest},
			{http.MethodPost, "/api/v1/employees", `{bad json`, http.StatusBadRequest},
			{http.MethodGet, "/api/v1/employees/abc", "", http.StatusBadRequest},
			{http.MethodGet, "/api/v1/employees/0", "", http.StatusBadRequest},
			{http.MethodGet, "/api/v1/employees/-1", "", http.StatusBadRequest},
			{http.MethodGet, "/api/v1/employees/99999", "", http.StatusNotFound},
			{http.MethodPut, "/api/v1/employees/99999", `{"name":"x"}`, http.StatusNotFound},
			{http.MethodPut, "/api/v1/employees/abc", `{"name":"x"}`, http.StatusBadRequest},
			{http.MethodDelete, "/api/v1/employees/99999", "", http.StatusNotFound},
			{http.MethodDelete, "/api/v1/employees/abc", "", http.StatusBadRequest},
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
	})

	t.Run("delete_disables_and_keeps_row", func(t *testing.T) {
		emp := env.createEmployee(t, env.adminToken, `{"name":"赵师傅","phone":"13900000003"}`)
		employeesBefore := env.countEmployees(t)

		// --- When: DELETE ---
		w := env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/employees/%d", emp.ID), "", env.adminToken)

		// --- Then: 200 + success 信封 ---
		if w.Code != http.StatusOK {
			t.Fatalf("DELETE /employees/%d status = %d, want 200 (body=%s)", emp.ID, w.Code, w.Body.String())
		}
		if envl := decodeEnvelope(t, w); envl.Code != service.CodeOK {
			t.Errorf("DELETE envelope = %+v, want code=0", envl)
		}

		// --- Then: 行仍在（禁止物理删除）、status=0、其余字段保留 ---
		row := env.employeeRow(t, emp.ID)
		if row.Status != model.StatusDisabled || row.Name != "赵师傅" || row.Phone != "13900000003" {
			t.Errorf("DELETE 后落库 = %+v, want status=0/name=赵师傅/phone 保留", row)
		}
		if got := env.countEmployees(t); got != employeesBefore {
			t.Errorf("DELETE 后 employees 行数 = %d, want %d（行保留）", got, employeesBefore)
		}

		// --- Then: 停用员工仍可 GET（记录保留，供历史关联查询） ---
		w = env.authed(http.MethodGet, fmt.Sprintf("/api/v1/employees/%d", emp.ID), "", env.adminToken)
		if w.Code != http.StatusOK {
			t.Fatalf("DELETE 后 GET /employees/%d status = %d, want 200（记录保留）", emp.ID, w.Code)
		}
		var got employeeAPIView
		decodeData(t, decodeEnvelope(t, w), &got)
		if got.Status != model.StatusDisabled {
			t.Errorf("DELETE 后 GET status = %d, want 0", got.Status)
		}

		// --- When: 重复 DELETE（幂等） ---
		w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/employees/%d", emp.ID), "", env.adminToken)

		// --- Then: 仍 200，行数不变 ---
		if w.Code != http.StatusOK {
			t.Errorf("重复 DELETE status = %d, want 200（幂等）", w.Code)
		}
		if got := env.countEmployees(t); got != employeesBefore {
			t.Errorf("重复 DELETE 后 employees 行数 = %d, want %d", got, employeesBefore)
		}
	})

	t.Run("referenced_employee_is_never_removed", func(t *testing.T) {
		cust := env.createCustomer(t, env.staffToken, `{"name":"引用员工客户","phone":"13800007001"}`)
		emp := env.createEmployee(t, env.adminToken, `{"name":"被引用员工","position":"技师"}`)
		hair := env.seedServiceViaAPI(t, "员工引用剪发", 5000)
		order := env.postOrder(t, env.adminToken,
			orderCreateJSON("req-emp-ref-1", cust.ID, &emp.ID, "cash", orderItemJSON(hair.ID, 1, 5000)))

		var ordersBefore int64
		if err := env.db.Model(&model.Order{}).Count(&ordersBefore).Error; err != nil {
			t.Fatalf("count orders: %v", err)
		}

		// --- When: 删除（停用）被订单引用的员工 ---
		w := env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/employees/%d", emp.ID), "", env.adminToken)

		// --- Then: 200 且员工行保留、status=0 ---
		if w.Code != http.StatusOK {
			t.Fatalf("DELETE 被引用员工 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		row := env.employeeRow(t, emp.ID)
		if row.Status != model.StatusDisabled {
			t.Errorf("被引用员工停用后 status = %d, want 0", row.Status)
		}

		// --- Then: 订单未被删除，employee_id 关联不变 ---
		var ordersAfter int64
		if err := env.db.Model(&model.Order{}).Count(&ordersAfter).Error; err != nil {
			t.Fatalf("count orders: %v", err)
		}
		if ordersAfter != ordersBefore {
			t.Errorf("orders 行数 = %d, want %d（订单不受员工停用影响）", ordersAfter, ordersBefore)
		}
		var orderRow model.Order
		if err := env.db.First(&orderRow, order.ID).Error; err != nil {
			t.Fatalf("订单被删除: %v", err)
		}
		if orderRow.EmployeeID == nil || *orderRow.EmployeeID != emp.ID {
			t.Errorf("订单 employee_id = %v, want %d（关联保留）", orderRow.EmployeeID, emp.ID)
		}

		// --- Then: 订单详情仍能读到员工名（历史消费记录不失效） ---
		w = env.getOrderAPI(env.adminToken, fmt.Sprintf("%d", order.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("GET /orders/%d status = %d, want 200 (body=%s)", order.ID, w.Code, w.Body.String())
		}
		var detail orderAPIView
		decodeData(t, decodeEnvelope(t, w), &detail)
		if detail.EmployeeName != "被引用员工" || detail.EmployeeID == nil || *detail.EmployeeID != emp.ID {
			t.Errorf("订单详情 employee = %v/%q, want id=%d/name=被引用员工", detail.EmployeeID, detail.EmployeeName, emp.ID)
		}
	})

	t.Run("audit_logs_written", func(t *testing.T) {
		// Given: 上一条子测试已创建/更新/停用员工。
		// Then: 每类动作都有审计，operator=admin，target_type=employee。
		for _, action := range []string{"employee_create", "employee_update", "employee_disable"} {
			var row model.OperationLog
			err := env.db.Where("action = ? AND target_type = ?", action, "employee").
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

		var disableCount int64
		if err := env.db.Model(&model.OperationLog{}).
			Where("action = ? AND target_type = ?", "employee_disable", "employee").
			Count(&disableCount).Error; err != nil {
			t.Fatalf("count employee_disable: %v", err)
		}
		if disableCount < 2 {
			t.Errorf("employee_disable 审计数 = %d, want ≥2（DELETE + 重复 DELETE 各一条）", disableCount)
		}
	})
}
