package controller_test

// TestCategories 是 todo 18 的验收测试（计划：`go test ./internal/controller -run TestCategories -v -count=1`）：
//   - GET /service-categories both；POST/PUT/DELETE 仅 admin（04-API.md:110-118）；
//   - sort 排序字段生效（列表按 sort 升序）；status 支持（创建/修改/status 过滤）；
//   - 分类下仍有服务（含已软删除服务）时删除 → 422 + 明确业务提示，不产生孤儿服务行；
//   - 403/404/400/401 与审计日志断言（DTO 与 Model 分离，金额无关）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// categoryView 是分类 DTO 的测试镜像。
type categoryView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Sort      int       `json:"sort"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// decodeCategories 解析 data 为分类数组。
func decodeCategories(t *testing.T, envl envelope) []categoryView {
	t.Helper()
	var items []categoryView
	if err := json.Unmarshal(envl.Data, &items); err != nil {
		t.Fatalf("解析分类数组失败: %v (data=%s)", err, envl.Data)
	}
	return items
}

// listCategories 通过 API 拉取分类数组（query 可为空）。
func (e *customerEnv) listCategories(t *testing.T, token, query string) []categoryView {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/service-categories"+query, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /service-categories%s status = %d, want 200 (body=%s)", query, w.Code, w.Body.String())
	}
	return decodeCategories(t, decodeEnvelope(t, w))
}

// createCategory 通过 API 建分类并断言 201。
func (e *customerEnv) createCategory(t *testing.T, token, body string) categoryView {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/service-categories", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /service-categories status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created categoryView
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// categoryIDs 提取分类 id 序列（用于断言排序）。
func categoryIDs(cats []categoryView) []int64 {
	ids := make([]int64, 0, len(cats))
	for _, c := range cats {
		ids = append(ids, c.ID)
	}
	return ids
}

// containsCategory 判断分类列表中是否存在指定 id。
func containsCategory(cats []categoryView, id int64) bool {
	for _, c := range cats {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestCategories(t *testing.T) {
	env := newCustomerEnv(t)

	// --- When: staff 创建分类 ---
	w := env.authed(http.MethodPost, "/api/v1/service-categories", `{"name":"剪发","sort":20}`, env.staffToken)

	// --- Then: 403 + 40300（04-API.md:112 创建/修改/删除仅 admin） ---
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff POST /service-categories status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff POST 分类 code = %d, want %d", envl.Code, service.CodeForbidden)
	}

	// --- When: admin 创建分类（省略 status → 默认启用；sort=20） ---
	hair := env.createCategory(t, env.adminToken, `{"name":"剪发","sort":20}`)

	// --- Then: DTO 回显，status 默认 1 ---
	if hair.ID <= 0 || hair.Name != "剪发" || hair.Sort != 20 || hair.Status != model.StatusEnabled {
		t.Errorf("创建分类 DTO = %+v, want 剪发/sort=20/status=1", hair)
	}

	// --- When: admin 创建第二个分类（sort 更小、显式 status=0） ---
	dye := env.createCategory(t, env.adminToken, `{"name":"烫染","sort":10,"status":0}`)

	// --- Then: status 支持显式停用 ---
	if dye.Sort != 10 || dye.Status != model.StatusDisabled {
		t.Errorf("创建分类 DTO = %+v, want sort=10/status=0", dye)
	}

	// --- When/Then: 空名称 → 400；非法 status → 400（malformed_input，不得 500） ---
	if w = env.authed(http.MethodPost, "/api/v1/service-categories", `{"name":"   "}`, env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("空名称分类 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodPost, "/api/v1/service-categories", `{"name":"x","status":2}`, env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法 status 分类 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: staff 读取分类列表 ---
	cats := env.listCategories(t, env.staffToken, "")

	// --- Then: both 可查；按 sort 升序（烫染 10 → 剪发 20） ---
	if len(cats) != 2 || cats[0].ID != dye.ID || cats[1].ID != hair.ID {
		t.Errorf("分类列表 = %v, want 排序 [%d,%d]（sort 升序）", categoryIDs(cats), dye.ID, hair.ID)
	}

	// --- When/Then: status 过滤 ---
	if enabled := env.listCategories(t, env.staffToken, "?status=1"); len(enabled) != 1 || enabled[0].ID != hair.ID {
		t.Errorf("?status=1 = %v, want 仅剪发", categoryIDs(enabled))
	}
	if disabled := env.listCategories(t, env.staffToken, "?status=0"); len(disabled) != 1 || disabled[0].ID != dye.ID {
		t.Errorf("?status=0 = %v, want 仅烫染", categoryIDs(disabled))
	}
	// 非法过滤值宽松回退为「不过滤」（与分页参数一致的容错策略，不得 500）
	if all := env.listCategories(t, env.staffToken, "?status=abc"); len(all) != 2 {
		t.Errorf("?status=abc = %v, want 不过滤（2 条）", categoryIDs(all))
	}

	// --- When: admin 修改分类（名称/sort/status） ---
	w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/service-categories/%d", dye.ID),
		`{"name":"烫发染发","sort":5,"status":1}`, env.adminToken)

	// --- Then: 200 + 新值 ---
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /service-categories status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var renamed categoryView
	decodeData(t, decodeEnvelope(t, w), &renamed)
	if renamed.ID != dye.ID || renamed.Name != "烫发染发" || renamed.Sort != 5 || renamed.Status != model.StatusEnabled {
		t.Errorf("修改分类 DTO = %+v, want id=%d 烫发染发/sort=5/status=1", renamed, dye.ID)
	}
	// --- Then: 排序随 sort 变化（烫发染发 5 在剪发 20 之前） ---
	if cats = env.listCategories(t, env.staffToken, ""); len(cats) != 2 || cats[0].ID != dye.ID {
		t.Errorf("修改后排序 = %v, want 烫发染发在前（sort=5）", categoryIDs(cats))
	}

	// --- When/Then: staff 修改/删除 → 403 ---
	if w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/service-categories/%d", hair.ID), `{"name":"x"}`, env.staffToken); w.Code != http.StatusForbidden {
		t.Errorf("staff PUT 分类 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/service-categories/%d", hair.ID), "", env.staffToken); w.Code != http.StatusForbidden {
		t.Errorf("staff DELETE 分类 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 不存在 → 404；非法路径 id → 400 ---
	if w = env.authed(http.MethodPut, "/api/v1/service-categories/999999", `{"name":"x"}`, env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("修改不存在分类 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, "/api/v1/service-categories/999999", "", env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("删除不存在分类 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, "/api/v1/service-categories/abc", "", env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法分类路径 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- Given: 「剪发」分类下有一个服务（直接入库：/services API 属 todo 19） ---
	svc := model.Service{CategoryID: hair.ID, Name: "男士剪发", PriceCents: 5000, DurationMinutes: 30, Status: model.StatusEnabled}
	if err := env.db.Create(&svc).Error; err != nil {
		t.Fatalf("造服务行失败: %v", err)
	}

	// --- When: admin 删除仍有服务的分类 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/service-categories/%d", hair.ID), "", env.adminToken)

	// --- Then: 422 + 42200 + 明确提示先处理服务（不得孤儿化服务） ---
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("删除有服务分类 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	envl := decodeEnvelope(t, w)
	if envl.Code != service.CodeValidationFailed {
		t.Errorf("删除有服务分类 code = %d, want %d", envl.Code, service.CodeValidationFailed)
	}
	if !strings.Contains(envl.Message, "服务") {
		t.Errorf("删除有服务分类 message = %q, want 含「服务」的业务提示", envl.Message)
	}
	// --- Then: 分类仍在（未被物理删除） ---
	if !containsCategory(env.listCategories(t, env.staffToken, ""), hair.ID) {
		t.Error("删除被拒后分类应仍可见")
	}

	// --- Given: 该服务被软删除（仅历史行存在） ---
	if err := env.db.Delete(&model.Service{}, svc.ID).Error; err != nil {
		t.Fatalf("软删除服务行失败: %v", err)
	}

	// --- When: 再删分类（stale_state：服务行仍引用该分类） ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/service-categories/%d", hair.ID), "", env.adminToken)

	// --- Then: 仍 422（不产生 category_id 悬空的孤儿服务行） ---
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("删除仅剩软删除服务的分类 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	// --- Then: services 行仍在且 category_id 未被置空/删除 ---
	var orphan model.Service
	if err := env.db.Unscoped().First(&orphan, svc.ID).Error; err != nil {
		t.Fatalf("探查服务行失败: %v", err)
	}
	if orphan.CategoryID != hair.ID {
		t.Errorf("服务 category_id = %d, want %d", orphan.CategoryID, hair.ID)
	}

	// --- Given: 一个无服务的空分类 ---
	empty := env.createCategory(t, env.adminToken, `{"name":"空分类"}`)

	// --- When: admin 删除空分类 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/service-categories/%d", empty.ID), "", env.adminToken)

	// --- Then: 200，且列表不再包含 ---
	if w.Code != http.StatusOK {
		t.Fatalf("删除空分类 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if containsCategory(env.listCategories(t, env.staffToken, ""), empty.ID) {
		t.Error("删除后分类仍出现在列表")
	}
	// --- Then: DB 行已物理删除（service_categories 无 deleted_at 列） ---
	var count int64
	if err := env.db.Model(&model.ServiceCategory{}).Where("id = ?", empty.ID).Count(&count).Error; err != nil {
		t.Fatalf("探查分类行失败: %v", err)
	}
	if count != 0 {
		t.Errorf("分类行数 = %d, want 0（无服务分类可物理删除）", count)
	}

	// --- Then: 审计日志覆盖 create/update/delete，operator 为 admin ---
	var actions []string
	if err := env.db.Model(&model.OperationLog{}).Where("action LIKE ?", "service_category_%").Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("查询分类审计日志: %v", err)
	}
	for _, want := range []string{"service_category_create", "service_category_update", "service_category_delete"} {
		found := false
		for _, got := range actions {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("缺少审计动作 %s（现有 %v）", want, actions)
		}
	}
	var deleteLog model.OperationLog
	if err := env.db.Where("action = ?", "service_category_delete").First(&deleteLog).Error; err != nil {
		t.Fatalf("查询 service_category_delete 日志: %v", err)
	}
	if deleteLog.OperatorID == nil || *deleteLog.OperatorID != env.admin.ID {
		t.Errorf("service_category_delete operator_id = %v, want %d（admin）", deleteLog.OperatorID, env.admin.ID)
	}
	if deleteLog.TargetType != "service_category" || deleteLog.TargetID != empty.ID {
		t.Errorf("service_category_delete target = %s#%d, want service_category#%d", deleteLog.TargetType, deleteLog.TargetID, empty.ID)
	}

	// --- When/Then: 未登录访问 → 401 ---
	if w = env.do(http.MethodGet, "/api/v1/service-categories", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /service-categories status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
