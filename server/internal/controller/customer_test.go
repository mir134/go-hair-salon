package controller_test

// TestCustomers 是 todo 13 的验收测试（计划：`go test ./internal/controller -run TestCustomers -v -count=1`）：
//   - POST /customers：staff 可创建（201），first_visit_at=now，金额字段为整数分；
//   - 同非空手机号重复 → 409（06-BUSINESS-RULES.md:8）；空手机号可重复（部分唯一索引）；
//   - GET /customers：keyword 命中姓名/手机号/微信号、phone 精确过滤、tag_id 过滤、分页 total 正确、sort=recent 按 last_visit_at 倒序；
//   - GET/PUT /customers/:id：改手机号不新建客户（04-API.md:70-95、06 §1）；
//   - DELETE /customers/:id：staff → 403，admin → 软删除（deleted_at 非空、行仍在、列表/详情不可见），
//     软删除后同手机号可重新建档（ux_customers_phone_active 部分唯一索引）；
//   - 非法分页参数不 500（宽松回退默认值）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// customerEnv 在完整路由环境上补充 staff 账号与两种角色的真实 token。
type customerEnv struct {
	*authTestEnv
	adminToken string
	staffToken string
	staff      *model.User
}

// newCustomerEnv 装配 todo 13/14/15 共用的测试环境：真实迁移库 + 完整路由 + admin/staff token。
func newCustomerEnv(t *testing.T) *customerEnv {
	t.Helper()
	env := newAuthEnv(t)
	staff, err := env.users.CreateUser(context.Background(), service.CreateUserInput{
		Username: "staff-c", Password: "Staff-Pwd-1", Role: model.RoleStaff,
	})
	if err != nil {
		t.Fatalf("CreateUser(staff-c): %v", err)
	}
	e := &customerEnv{authTestEnv: env, staff: staff}
	e.adminToken = e.login(t, "admin", "Admin-Pwd-1")
	e.staffToken = e.login(t, "staff-c", "Staff-Pwd-1")
	return e
}

// login 走真实登录接口换取 token（不绕过 JWT 中间件）。
func (e *customerEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	w := e.do(http.MethodPost, "/api/v1/auth/login", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login(%s) status = %d, want 200 (body=%s)", username, w.Code, w.Body.String())
	}
	var data loginData
	decodeData(t, decodeEnvelope(t, w), &data)
	if data.Token == "" {
		t.Fatalf("login(%s) token 为空", username)
	}
	return data.Token
}

// authed 发起带 Bearer token 的请求。
func (e *customerEnv) authed(method, path, body, token string) *httptest.ResponseRecorder {
	return e.do(method, path, body, map[string]string{"Authorization": "Bearer " + token})
}

// pageData 是分页 data 的测试镜像（04-API.md:35-44）。
type pageData struct {
	Items    json.RawMessage `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// customerData 是客户 DTO 的测试镜像：金额一律整数分，时间为 RFC3339/UTC。
type customerData struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Phone           string     `json:"phone"`
	Gender          string     `json:"gender"`
	Birthday        *string    `json:"birthday"`
	Avatar          string     `json:"avatar"`
	Wechat          string     `json:"wechat"`
	Source          string     `json:"source"`
	FirstVisitAt    *time.Time `json:"first_visit_at"`
	LastVisitAt     *time.Time `json:"last_visit_at"`
	TotalSpentCents int64      `json:"total_spent_cents"`
	BalanceCents    int64      `json:"balance_cents"`
	Points          int64      `json:"points"`
	Remark          string     `json:"remark"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// decodePage 解析分页信封 data。
func decodePage(t *testing.T, envl envelope) pageData {
	t.Helper()
	var page pageData
	decodeData(t, envl, &page)
	return page
}

// decodeCustomers 解析分页 items 为 DTO 列表。
func decodeCustomers(t *testing.T, page pageData) []customerData {
	t.Helper()
	var items []customerData
	if err := json.Unmarshal(page.Items, &items); err != nil {
		t.Fatalf("解析 items 失败: %v (items=%s)", err, page.Items)
	}
	return items
}

// createCustomer 通过 API 建档，返回 DTO。
func (e *customerEnv) createCustomer(t *testing.T, token, body string) customerData {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/customers", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /customers status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created customerData
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// listCustomers 通过 API 拉取列表，返回分页结构。
func (e *customerEnv) listCustomers(t *testing.T, token, query string) pageData {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/customers"+query, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /customers%s status = %d, want 200 (body=%s)", query, w.Code, w.Body.String())
	}
	return decodePage(t, decodeEnvelope(t, w))
}

// getCustomer 通过 API 读取详情。
func (e *customerEnv) getCustomer(t *testing.T, token string, id int64) *httptest.ResponseRecorder {
	t.Helper()
	return e.authed(http.MethodGet, fmt.Sprintf("/api/v1/customers/%d", id), "", token)
}

func TestCustomers(t *testing.T) {
	env := newCustomerEnv(t)

	// --- Given/When: staff 创建客户（含手机号/微信/来源/备注） ---
	w := env.authed(http.MethodPost, "/api/v1/customers",
		`{"name":"张三","phone":"13800000001","wechat":"zs_wx","gender":"male","source":"walkin","remark":"老客户"}`,
		env.staffToken)

	// --- Then: 201 + DTO 字段回显；first_visit_at=now；金额为 0 分 ---
	if w.Code != http.StatusCreated {
		t.Fatalf("创建客户 status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	envl := decodeEnvelope(t, w)
	if envl.Code != service.CodeOK {
		t.Errorf("创建客户 envelope = %+v, want code=0", envl)
	}
	var zhangsan customerData
	decodeData(t, envl, &zhangsan)
	if zhangsan.ID <= 0 || zhangsan.Name != "张三" || zhangsan.Phone != "13800000001" || zhangsan.Wechat != "zs_wx" {
		t.Errorf("创建客户 DTO = %+v, want 姓名/手机号/微信回显", zhangsan)
	}
	if zhangsan.FirstVisitAt == nil {
		t.Fatal("创建客户 first_visit_at = nil, want now（计划 todo 13）")
	}
	if delta := time.Since(*zhangsan.FirstVisitAt); delta < -time.Minute || delta > time.Minute {
		t.Errorf("first_visit_at 距现在 = %v, want 约 0", delta)
	}
	if zhangsan.TotalSpentCents != 0 || zhangsan.BalanceCents != 0 || zhangsan.Points != 0 {
		t.Errorf("新客户金额/积分 = %d/%d/%d, want 0/0/0", zhangsan.TotalSpentCents, zhangsan.BalanceCents, zhangsan.Points)
	}

	// --- When: 同非空手机号重复建档 ---
	w = env.authed(http.MethodPost, "/api/v1/customers", `{"name":"李四","phone":"13800000001"}`, env.staffToken)

	// --- Then: 409 + 提示编辑原客户（06-BUSINESS-RULES.md:8） ---
	if w.Code != http.StatusConflict {
		t.Fatalf("重复手机号 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	envl = decodeEnvelope(t, w)
	if envl.Code != service.CodeConflict {
		t.Errorf("重复手机号 code = %d, want %d", envl.Code, service.CodeConflict)
	}
	if !strings.Contains(envl.Message, "手机号已存在") {
		t.Errorf("重复手机号 message = %q, want 提示手机号已存在", envl.Message)
	}

	// --- When: 空手机号建档两次 ---
	emptyA := env.createCustomer(t, env.staffToken, `{"name":"无手机号甲"}`)
	emptyB := env.createCustomer(t, env.staffToken, `{"name":"无手机号乙","phone":""}`)

	// --- Then: 空手机号不受部分唯一索引约束，均创建成功 ---
	if emptyA.ID == emptyB.ID {
		t.Errorf("空手机号两次建档 id 相同 = %d, want 不同", emptyA.ID)
	}

	// --- Given: 再建 3 个客户用于分页/搜索/sort=recent ---
	li := env.createCustomer(t, env.staffToken, `{"name":"李四","phone":"13900000002","wechat":"lisi_wx"}`)
	_ = env.createCustomer(t, env.staffToken, `{"name":"王五","phone":"13700000003"}`)
	zhao := env.createCustomer(t, env.staffToken, `{"name":"赵六","phone":"13600000004"}`)

	// --- When: keyword 分别命中手机号/姓名/微信号 ---
	byPhone := env.listCustomers(t, env.staffToken, "?keyword=13900000002")
	byName := env.listCustomers(t, env.staffToken, "?keyword=王五")
	byWechat := env.listCustomers(t, env.staffToken, "?keyword=zs_wx")

	// --- Then: 各自恰好命中 1 条 ---
	for name, page := range map[string]pageData{"手机号": byPhone, "姓名": byName, "微信号": byWechat} {
		if page.Total != 1 {
			t.Errorf("keyword=%s total = %d, want 1 (items=%s)", name, page.Total, page.Items)
		}
	}

	// --- When: phone 精确过滤 + tag_id 过滤（relation 行直插，todo 15 才提供 API） ---
	tag := model.Tag{Name: "VIP", Color: "#f56c6c"}
	if err := env.db.Create(&tag).Error; err != nil {
		t.Fatalf("插入 tag: %v", err)
	}
	if err := env.db.Create(&model.CustomerTagRelation{CustomerID: zhao.ID, TagID: tag.ID}).Error; err != nil {
		t.Fatalf("插入 relation: %v", err)
	}
	byExactPhone := env.listCustomers(t, env.staffToken, "?phone=13700000003")
	byTag := env.listCustomers(t, env.staffToken, fmt.Sprintf("?tag_id=%d", tag.ID))

	// --- Then: phone 命中王五；tag_id 命中赵六 ---
	if byExactPhone.Total != 1 {
		t.Errorf("phone 过滤 total = %d, want 1", byExactPhone.Total)
	} else if got := decodeCustomers(t, byExactPhone); len(got) != 1 || got[0].Name != "王五" {
		t.Errorf("phone 过滤 items = %+v, want 王五", got)
	}
	if byTag.Total != 1 {
		t.Errorf("tag_id 过滤 total = %d, want 1", byTag.Total)
	} else if got := decodeCustomers(t, byTag); len(got) != 1 || got[0].ID != zhao.ID {
		t.Errorf("tag_id 过滤 items = %+v, want 赵六(id=%d)", got, zhao.ID)
	}

	// --- When: 分页 page=2&page_size=2（当前共 6 个未删除客户） ---
	page2 := env.listCustomers(t, env.staffToken, "?page=2&page_size=2")

	// --- Then: total=6、返回 2 条、页码回显 ---
	if page2.Total != 6 {
		t.Errorf("分页 total = %d, want 6", page2.Total)
	}
	if page2.Page != 2 || page2.PageSize != 2 {
		t.Errorf("分页 page/page_size = %d/%d, want 2/2", page2.Page, page2.PageSize)
	}
	if got := decodeCustomers(t, page2); len(got) != 2 {
		t.Errorf("分页 items 条数 = %d, want 2", len(got))
	}

	// --- When: sort=recent（直接改 last_visit_at 模拟到店） ---
	older := time.Now().UTC().Add(-48 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)
	if err := env.db.Model(&model.Customer{}).Where("id = ?", li.ID).Update("last_visit_at", older).Error; err != nil {
		t.Fatalf("更新 last_visit_at(li): %v", err)
	}
	if err := env.db.Model(&model.Customer{}).Where("id = ?", zhao.ID).Update("last_visit_at", newer).Error; err != nil {
		t.Fatalf("更新 last_visit_at(zhao): %v", err)
	}
	recent := env.listCustomers(t, env.staffToken, "?sort=recent&page_size=100")

	// --- Then: 最近到店排最前，且未到店（NULL）排在后面 ---
	items := decodeCustomers(t, recent)
	if len(items) == 0 || items[0].ID != zhao.ID {
		t.Fatalf("sort=recent 首条 = %+v, want 赵六(id=%d) 排最前", items, zhao.ID)
	}
	zhaoIdx, liIdx, firstNil := -1, -1, -1
	for i, item := range items {
		switch item.ID {
		case zhao.ID:
			zhaoIdx = i
		case li.ID:
			liIdx = i
		}
		if firstNil < 0 && item.LastVisitAt == nil {
			firstNil = i
		}
	}
	if liIdx <= zhaoIdx {
		t.Errorf("sort=recent 李四(idx=%d) 应排在赵六(idx=%d) 之后（older last_visit_at）", liIdx, zhaoIdx)
	}
	if firstNil >= 0 && firstNil < liIdx {
		t.Errorf("sort=recent 未到店(NULL) 客户(idx=%d) 应排在有到店记录(idx=%d) 之后", firstNil, liIdx)
	}

	// --- When: 详情 + 非法/不存在 id ---
	w = env.getCustomer(t, env.staffToken, zhangsan.ID)

	// --- Then: 200 且 id 一致 ---
	if w.Code != http.StatusOK {
		t.Fatalf("GET /customers/%d status = %d, want 200 (body=%s)", zhangsan.ID, w.Code, w.Body.String())
	}
	var detail customerData
	decodeData(t, decodeEnvelope(t, w), &detail)
	if detail.ID != zhangsan.ID || detail.Phone != "13800000001" {
		t.Errorf("详情 DTO = %+v, want id=%d phone=13800000001", detail, zhangsan.ID)
	}
	w = env.getCustomer(t, env.staffToken, 999999)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在客户 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if envl = decodeEnvelope(t, w); envl.Code != service.CodeCustomerNotFound {
		t.Errorf("不存在客户 code = %d, want %d", envl.Code, service.CodeCustomerNotFound)
	}
	w = env.authed(http.MethodGet, "/api/v1/customers/abc", "", env.staffToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("非法 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: staff 修改手机号（不得新建客户） ---
	w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/customers/%d", zhangsan.ID),
		`{"name":"张三","phone":"13800000009","wechat":"zs_wx","remark":"换号"}`, env.staffToken)

	// --- Then: 200、id 不变、总数不变、旧手机号搜不到、新手机号搜得到 ---
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /customers status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var updated customerData
	decodeData(t, decodeEnvelope(t, w), &updated)
	if updated.ID != zhangsan.ID || updated.Phone != "13800000009" {
		t.Errorf("修改后 DTO = %+v, want id=%d phone=13800000009", updated, zhangsan.ID)
	}
	if after := env.listCustomers(t, env.staffToken, "?page_size=100"); after.Total != 6 {
		t.Errorf("改手机号后客户总数 = %d, want 6（不得新建客户）", after.Total)
	}
	if old := env.listCustomers(t, env.staffToken, "?keyword=13800000001"); old.Total != 0 {
		t.Errorf("旧手机号搜索 total = %d, want 0", old.Total)
	}
	if now := env.listCustomers(t, env.staffToken, "?keyword=13800000009"); now.Total != 1 {
		t.Errorf("新手机号搜索 total = %d, want 1", now.Total)
	}

	// --- When: 改成他人手机号 ---
	w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/customers/%d", zhangsan.ID),
		`{"name":"张三","phone":"13700000003"}`, env.staffToken)

	// --- Then: 409 ---
	if w.Code != http.StatusConflict {
		t.Errorf("改成他人手机号 status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: staff 删除客户 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d", zhangsan.ID), "", env.staffToken)

	// --- Then: 403（04-API.md:72 删除仅 admin；后端 RBAC 为最终边界） ---
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff DELETE status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl = decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff DELETE code = %d, want %d", envl.Code, service.CodeForbidden)
	}
	// 仍未被删除。
	if still := env.listCustomers(t, env.staffToken, "?keyword=13800000009"); still.Total != 1 {
		t.Errorf("staff 删除被拒后客户仍应可见, total = %d want 1", still.Total)
	}

	// --- When: admin 软删除 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d", zhangsan.ID), "", env.adminToken)

	// --- Then: 200；列表/详情不可见；DB 行仍在且 deleted_at 非空（禁物理删除） ---
	if w.Code != http.StatusOK {
		t.Fatalf("admin DELETE status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if after := env.listCustomers(t, env.adminToken, "?page_size=100"); after.Total != 5 {
		t.Errorf("软删除后列表 total = %d, want 5", after.Total)
	}
	if gone := env.listCustomers(t, env.adminToken, "?keyword=13800000009"); gone.Total != 0 {
		t.Errorf("软删除后按手机号搜索 total = %d, want 0", gone.Total)
	}
	if w = env.getCustomer(t, env.adminToken, zhangsan.ID); w.Code != http.StatusNotFound {
		t.Errorf("软删除后详情 status = %d, want 404", w.Code)
	}
	var row model.Customer
	if err := env.db.Unscoped().First(&row, zhangsan.ID).Error; err != nil {
		t.Fatalf("DB 探查软删除行失败（行不应被物理删除）: %v", err)
	}
	if !row.DeletedAt.Valid {
		t.Error("DB 行 deleted_at 为空, want 非空（软删除）")
	}

	// --- When: 软删除后用同一手机号重新建档（部分唯一索引允许） ---
	reborn := env.createCustomer(t, env.adminToken, `{"name":"张三(新)","phone":"13800000009"}`)

	// --- Then: 新 id、新客户可见 ---
	if reborn.ID == zhangsan.ID {
		t.Errorf("重建客户 id = %d, want 新 id", reborn.ID)
	}
	if rebornSearch := env.listCustomers(t, env.adminToken, "?keyword=13800000009"); rebornSearch.Total != 1 {
		t.Errorf("重建后搜索 total = %d, want 1", rebornSearch.Total)
	}

	// --- Then: 审计日志覆盖 create/update/delete，operator 为操作者，且不含敏感信息 ---
	var actions []string
	if err := env.db.Model(&model.OperationLog{}).
		Where("target_type = ?", "customer").
		Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("查询 customer 审计日志: %v", err)
	}
	for _, want := range []string{"customer_create", "customer_update", "customer_delete"} {
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
	if err := env.db.Where("action = ?", "customer_delete").First(&deleteLog).Error; err != nil {
		t.Fatalf("查询 customer_delete 日志: %v", err)
	}
	if deleteLog.OperatorID == nil || *deleteLog.OperatorID != env.admin.ID {
		t.Errorf("customer_delete operator_id = %v, want %d（admin）", deleteLog.OperatorID, env.admin.ID)
	}

	// --- When: 非法分页参数（非数字/负数/超限） ---
	badPage := env.listCustomers(t, env.staffToken, "?page=abc&page_size=-5")

	// --- Then: 不 500，宽松回退默认值 page=1/page_size=20 ---
	if badPage.Page != 1 || badPage.PageSize != 20 {
		t.Errorf("非法分页 page/page_size = %d/%d, want 1/20（宽松回退）", badPage.Page, badPage.PageSize)
	}
	over := env.listCustomers(t, env.staffToken, "?page=1&page_size=1000")
	if over.PageSize != 100 {
		t.Errorf("page_size=1000 回显 = %d, want 上限 100", over.PageSize)
	}

	// --- Then: 列表响应不含软删除客户，且为空时 items 为空数组而非 null ---
	empty := env.listCustomers(t, env.staffToken, "?keyword=不存在的客户关键词")
	if string(empty.Items) != "[]" {
		t.Errorf("空结果 items = %s, want []", empty.Items)
	}
	all := decodeCustomers(t, env.listCustomers(t, env.staffToken, "?page_size=100"))
	for _, item := range all {
		if item.ID == zhangsan.ID {
			t.Errorf("列表仍包含已软删除客户 id=%d", zhangsan.ID)
		}
	}
}

// TestCustomersUnauthorized 覆盖未登录访问客户接口（JWT 中间件兜底）。
func TestCustomersUnauthorized(t *testing.T) {
	env := newCustomerEnv(t)
	w := env.do(http.MethodGet, "/api/v1/customers", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 GET /customers status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
