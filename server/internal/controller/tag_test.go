package controller_test

// TestTags 是 todo 15 的验收测试（计划：`go test ./internal/controller -run TestTags -v -count=1`）：
//   - GET /tags both；POST/PUT/DELETE /tags 仅 admin（staff → 403，04-API.md:97-108）；
//   - 标签软删除（deleted_at），删除标签不得级联删除历史 customer_tag_relations（03-DATABASE.md:77）；
//   - 挂标签/摘标签（客户编辑 both）；重复挂标签 409 且唯一联合索引只保留一行；
//   - 删除标签后客户历史标签仍可显示（deleted=true），relation 行仍在 DB；
//   - 不存在资源 → 404；空名称 → 400；写操作全部落 operation_logs。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// tagView 是标签 DTO 的测试镜像。
type tagView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// customerDetailView 是客户详情 DTO（含标签）的测试镜像。
type customerDetailView struct {
	customerData
	Tags []tagView `json:"tags"`
}

// decodeTags 解析 data 为标签数组。
func decodeTags(t *testing.T, envl envelope) []tagView {
	t.Helper()
	var tags []tagView
	if err := json.Unmarshal(envl.Data, &tags); err != nil {
		t.Fatalf("解析标签数组失败: %v (data=%s)", err, envl.Data)
	}
	return tags
}

// createTag 通过 API 建标签（admin）。
func (e *customerEnv) createTag(t *testing.T, name, color string) tagView {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"color":%q}`, name, color)
	w := e.authed(http.MethodPost, "/api/v1/tags", body, e.adminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /tags status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var tag tagView
	decodeData(t, decodeEnvelope(t, w), &tag)
	return tag
}

// listTags 通过 API 拉取标签数组。
func (e *customerEnv) listTags(t *testing.T, token string) []tagView {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/tags", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /tags status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	return decodeTags(t, decodeEnvelope(t, w))
}

// customerTags 读取客户详情里的标签。
func (e *customerEnv) customerTags(t *testing.T, token string, customerID int64) []tagView {
	t.Helper()
	w := e.getCustomer(t, token, customerID)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /customers/%d status = %d, want 200 (body=%s)", customerID, w.Code, w.Body.String())
	}
	var detail customerDetailView
	decodeData(t, decodeEnvelope(t, w), &detail)
	return detail.Tags
}

// relationCount 直接探查 customer_tag_relations 行数。
func (e *customerEnv) relationCount(t *testing.T, customerID, tagID int64) int64 {
	t.Helper()
	var count int64
	err := e.db.Model(&model.CustomerTagRelation{}).
		Where("customer_id = ? AND tag_id = ?", customerID, tagID).
		Count(&count).Error
	if err != nil {
		t.Fatalf("探查 customer_tag_relations: %v", err)
	}
	return count
}

// attachTag 挂标签。
func (e *customerEnv) attachTag(t *testing.T, token string, customerID, tagID int64) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"tag_id":%d}`, tagID)
	return e.authed(http.MethodPost, fmt.Sprintf("/api/v1/customers/%d/tags", customerID), body, token)
}

// detachTag 摘标签。
func (e *customerEnv) detachTag(t *testing.T, token string, customerID, tagID int64) *httptest.ResponseRecorder {
	t.Helper()
	return e.authed(http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d/tags/%d", customerID, tagID), "", token)
}

func TestTags(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"标签客户","phone":"13800003000"}`)

	// --- When: staff 创建标签 ---
	w := env.authed(http.MethodPost, "/api/v1/tags", `{"name":"VIP","color":"#f56c6c"}`, env.staffToken)

	// --- Then: 403（创建/修改/删除仅 admin，04-API.md:99） ---
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff POST /tags status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff POST /tags code = %d, want %d", envl.Code, service.CodeForbidden)
	}

	// --- When: admin 创建两个标签 ---
	vip := env.createTag(t, "VIP", "#f56c6c")
	regular := env.createTag(t, "老客", "#409eff")

	// --- Then: DTO 回显；新标签 deleted=false ---
	if vip.ID <= 0 || vip.Name != "VIP" || vip.Color != "#f56c6c" || vip.Deleted {
		t.Errorf("创建标签 DTO = %+v, want VIP/#f56c6c/deleted=false", vip)
	}

	// --- When: 空名称 ---
	w = env.authed(http.MethodPost, "/api/v1/tags", `{"name":"   "}`, env.adminToken)

	// --- Then: 400 ---
	if w.Code != http.StatusBadRequest {
		t.Errorf("空名称标签 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: admin 修改标签 ---
	w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/tags/%d", regular.ID),
		`{"name":"老客户","color":"#67c23a"}`, env.adminToken)

	// --- Then: 200 + 新值 ---
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /tags status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var renamed tagView
	decodeData(t, decodeEnvelope(t, w), &renamed)
	if renamed.ID != regular.ID || renamed.Name != "老客户" || renamed.Color != "#67c23a" {
		t.Errorf("修改标签 DTO = %+v, want id=%d 老客户/#67c23a", renamed, regular.ID)
	}

	// --- When: staff 查询标签 ---
	tags := env.listTags(t, env.staffToken)

	// --- Then: both 可查，恰好 2 个 ---
	if len(tags) != 2 {
		t.Fatalf("GET /tags 数量 = %d, want 2 (tags=%+v)", len(tags), tags)
	}

	// --- When: staff 挂标签（客户编辑 both） ---
	w = env.attachTag(t, env.staffToken, customer.ID, vip.ID)

	// --- Then: 200，返回客户最新标签列表 ---
	if w.Code != http.StatusOK {
		t.Fatalf("挂标签 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if attached := decodeTags(t, decodeEnvelope(t, w)); len(attached) != 1 || attached[0].ID != vip.ID {
		t.Errorf("挂标签返回 = %+v, want [VIP]", attached)
	}

	// --- Then: 客户详情包含标签 ---
	if got := env.customerTags(t, env.staffToken, customer.ID); len(got) != 1 || got[0].Name != "VIP" {
		t.Errorf("客户详情标签 = %+v, want [VIP]", got)
	}

	// --- When: 重复挂同一标签 ---
	w = env.attachTag(t, env.staffToken, customer.ID, vip.ID)

	// --- Then: 409（唯一联合索引语义） ---
	if w.Code != http.StatusConflict {
		t.Fatalf("重复挂标签 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeConflict {
		t.Errorf("重复挂标签 code = %d, want %d", envl.Code, service.CodeConflict)
	}
	// --- Then: relation 行仍只有 1 行 ---
	if count := env.relationCount(t, customer.ID, vip.ID); count != 1 {
		t.Errorf("重复挂标签后 relation 行数 = %d, want 1", count)
	}

	// --- When/Then: 挂不存在的标签 / 挂到不存在的客户 / 非法 tag_id ---
	if w = env.attachTag(t, env.staffToken, customer.ID, 999999); w.Code != http.StatusNotFound {
		t.Errorf("挂不存在标签 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.attachTag(t, env.staffToken, 999999, vip.ID); w.Code != http.StatusNotFound {
		t.Errorf("挂到不存在客户 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.attachTag(t, env.staffToken, customer.ID, 0); w.Code != http.StatusBadRequest {
		t.Errorf("非法 tag_id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: 摘标签 ---
	w = env.detachTag(t, env.staffToken, customer.ID, vip.ID)

	// --- Then: 200，标签列表为空数组 ---
	if w.Code != http.StatusOK {
		t.Fatalf("摘标签 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if remaining := decodeTags(t, decodeEnvelope(t, w)); len(remaining) != 0 {
		t.Errorf("摘标签返回 = %+v, want 空", remaining)
	}
	// --- Then: relation 行已解除 ---
	if count := env.relationCount(t, customer.ID, vip.ID); count != 0 {
		t.Errorf("摘标签后 relation 行数 = %d, want 0", count)
	}
	// --- When/Then: 重复摘 → 404 ---
	if w = env.detachTag(t, env.staffToken, customer.ID, vip.ID); w.Code != http.StatusNotFound {
		t.Errorf("重复摘标签 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}

	// --- Given: 重新挂上「老客户」标签，然后 admin 软删除该标签 ---
	if w = env.attachTag(t, env.staffToken, customer.ID, regular.ID); w.Code != http.StatusOK {
		t.Fatalf("重新挂标签 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/tags/%d", regular.ID), "", env.adminToken)

	// --- Then: 200 ---
	if w.Code != http.StatusOK {
		t.Fatalf("admin DELETE /tags status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// --- Then: GET /tags 不再包含已删除标签 ---
	for _, tag := range env.listTags(t, env.staffToken) {
		if tag.ID == regular.ID {
			t.Errorf("GET /tags 仍包含已软删除标签 id=%d", regular.ID)
		}
	}

	// --- Then: relation 行仍在（删除标签不级联删历史关系，03-DATABASE.md:77） ---
	if count := env.relationCount(t, customer.ID, regular.ID); count != 1 {
		t.Errorf("软删除标签后 relation 行数 = %d, want 1（历史关系必须保留）", count)
	}

	// --- Then: 客户详情仍能显示历史标签且标记 deleted=true ---
	history := env.customerTags(t, env.staffToken, customer.ID)
	if len(history) != 1 || history[0].ID != regular.ID || !history[0].Deleted {
		t.Errorf("客户历史标签 = %+v, want [老客户 deleted=true]", history)
	}

	// --- When: 尝试把已软删除的「老客户」标签重新挂到客户上（stale_state） ---
	w = env.attachTag(t, env.staffToken, customer.ID, regular.ID)

	// --- Then: 404（软删除标签不可再挂；已存在的历史 relation 不受影响） ---
	if w.Code != http.StatusNotFound {
		t.Errorf("重新挂已软删除标签 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if count := env.relationCount(t, customer.ID, regular.ID); count != 1 {
		t.Errorf("重新挂失败后 relation 行数 = %d, want 1（保持历史关系）", count)
	}

	// --- Then: DB 行仍在且 deleted_at 非空（软删除，禁物理删除） ---
	var tagRow model.Tag
	if err := env.db.Unscoped().First(&tagRow, regular.ID).Error; err != nil {
		t.Fatalf("DB 探查软删除标签失败: %v", err)
	}
	if !tagRow.DeletedAt.Valid {
		t.Error("标签 deleted_at 为空, want 非空（软删除）")
	}

	// --- When: staff 删除标签 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/tags/%d", vip.ID), "", env.staffToken)

	// --- Then: 403，且标签仍可见 ---
	if w.Code != http.StatusForbidden {
		t.Errorf("staff DELETE /tags status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	visible := false
	for _, tag := range env.listTags(t, env.adminToken) {
		if tag.ID == vip.ID {
			visible = true
		}
	}
	if !visible {
		t.Error("staff 删除被拒后标签应仍可见")
	}

	// --- When/Then: 删除/修改不存在的标签 → 404 ---
	if w = env.authed(http.MethodDelete, "/api/v1/tags/999999", "", env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("删除不存在标签 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodPut, "/api/v1/tags/999999", `{"name":"x"}`, env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("修改不存在标签 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 非法路径 id → 400（malformed_input，不得 500） ---
	if w = env.authed(http.MethodPut, "/api/v1/tags/abc", `{"name":"x"}`, env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法标签路径 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodPost, "/api/v1/customers/abc/tags", fmt.Sprintf(`{"tag_id":%d}`, vip.ID), env.staffToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法客户路径 id 挂标签 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d/tags/abc", customer.ID), "", env.staffToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法 tag_id 路径摘标签 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- Then: 审计日志覆盖 tag_create/tag_update/tag_delete/tag_attach/tag_detach ---
	var actions []string
	if err := env.db.Model(&model.OperationLog{}).
		Where("action LIKE ?", "tag_%").
		Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("查询标签审计日志: %v", err)
	}
	for _, want := range []string{"tag_create", "tag_update", "tag_delete", "tag_attach", "tag_detach"} {
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
	if err := env.db.Where("action = ?", "tag_delete").First(&deleteLog).Error; err != nil {
		t.Fatalf("查询 tag_delete 日志: %v", err)
	}
	if deleteLog.OperatorID == nil || *deleteLog.OperatorID != env.admin.ID {
		t.Errorf("tag_delete operator_id = %v, want %d（admin）", deleteLog.OperatorID, env.admin.ID)
	}
	// --- Then: 挂/摘标签的审计目标是客户（target_type/target_id 语义正确） ---
	var attachLog model.OperationLog
	if err := env.db.Where("action = ?", "tag_attach").Order("id").First(&attachLog).Error; err != nil {
		t.Fatalf("查询 tag_attach 日志: %v", err)
	}
	if attachLog.TargetType != "customer" || attachLog.TargetID != customer.ID {
		t.Errorf("tag_attach target = %s#%d, want customer#%d", attachLog.TargetType, attachLog.TargetID, customer.ID)
	}
	var detachLog model.OperationLog
	if err := env.db.Where("action = ?", "tag_detach").Order("id").First(&detachLog).Error; err != nil {
		t.Fatalf("查询 tag_detach 日志: %v", err)
	}
	if detachLog.TargetType != "customer" || detachLog.TargetID != customer.ID {
		t.Errorf("tag_detach target = %s#%d, want customer#%d", detachLog.TargetType, detachLog.TargetID, customer.ID)
	}

	// --- When/Then: 未登录访问标签 → 401 ---
	if w = env.do(http.MethodGet, "/api/v1/tags", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /tags status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
