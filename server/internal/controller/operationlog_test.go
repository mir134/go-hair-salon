package controller_test

// TestOperationLogsQuery 是 todo 47 的验收测试（计划：`go test ./internal/controller -run TestOperationLogsQuery -v -count=1`）：
//   - GET /operation-logs（仅 admin）：operator_id / action / start_date / end_date 筛选与分页，
//     时间倒序（最新在前），DTO 含 operator_name / target_type / target_id / content / ip / user_agent / created_at
//     （04-API.md:237-243、03-DATABASE.md:265-277、05-TASKS.md:150-155）；
//   - staff → 403（06 §7「staff 禁操作日志」）；未登录 → 401；
//   - malformed_input：未知 action / 非法日期 / 非法分页宽松回退（空数组 / 默认分页，不得 500）；
//   - stale_state：新写入的日志出现在下一次查询首位（时间倒序由 created_at + id 保证）；
//   - misleading_success_output：分页 total 与筛选结果一律与 DB 行数对照断言，不信任 HTTP 200。

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// operationLogView 是操作日志 DTO 的测试镜像（todo 47，字段与 controller/operationlog.go 对齐）。
type operationLogView struct {
	ID           int64     `json:"id"`
	OperatorID   *int64    `json:"operator_id"`
	OperatorName string    `json:"operator_name"`
	Action       string    `json:"action"`
	TargetType   string    `json:"target_type"`
	TargetID     int64     `json:"target_id"`
	Content      string    `json:"content"`
	IP           string    `json:"ip"`
	UserAgent    string    `json:"user_agent"`
	CreatedAt    time.Time `json:"created_at"`
}

// countLogs 统计 operation_logs 命中行数（DB 真值，用于对照 API 的 total）。
func (e *customerEnv) countLogs(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.OperationLog{}).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("统计 operation_logs(%s) 失败: %v", query, err)
	}
	return count
}

// listOperationLogs 通过 API 拉取操作日志分页（断言 200），返回分页结构与 DTO 列表。
func (e *customerEnv) listOperationLogs(t *testing.T, token, query string) (pageData, []operationLogView) {
	t.Helper()
	w := e.getPage(t, token, "/api/v1/operation-logs"+query)
	return w, decodeItems[operationLogView](t, w)
}

// findLog 在 DTO 列表中查找指定 action 的日志（找不到即失败）。
func findLog(t *testing.T, items []operationLogView, action string) operationLogView {
	t.Helper()
	for _, item := range items {
		if item.Action == action {
			return item
		}
	}
	t.Fatalf("操作日志列表缺少 action=%s（items=%+v）", action, items)
	return operationLogView{}
}

func TestOperationLogsQuery(t *testing.T) {
	env := newCustomerEnv(t)

	// --- Given: staff 建档（显式 ip/ua 头，验证审计来源被记录） ---
	createBody := `{"name":"日志客户","phone":"13800008001"}`
	w := env.do(http.MethodPost, "/api/v1/customers", createBody, map[string]string{
		"Authorization":   "Bearer " + env.staffToken,
		"X-Forwarded-For": "10.9.8.7",
		"User-Agent":      "audit-probe/1.0",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /customers status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var customer customerData
	decodeData(t, decodeEnvelope(t, w), &customer)

	// --- Given: staff 修改客户、admin 建标签，制造 3 类动作 ---
	updateCustomer := env.authed(http.MethodPut, fmt.Sprintf("/api/v1/customers/%d", customer.ID),
		`{"name":"日志客户改","phone":"13800008001"}`, env.staffToken)
	if updateCustomer.Code != http.StatusOK {
		t.Fatalf("PUT /customers/%d status = %d, want 200 (body=%s)", customer.ID, updateCustomer.Code, updateCustomer.Body.String())
	}
	tagResponse := env.authed(http.MethodPost, "/api/v1/tags", `{"name":"日志标签","color":"#336699"}`, env.adminToken)
	if tagResponse.Code != http.StatusCreated {
		t.Fatalf("POST /tags status = %d, want 201 (body=%s)", tagResponse.Code, tagResponse.Body.String())
	}
	var tag struct {
		ID int64 `json:"id"`
	}
	decodeData(t, decodeEnvelope(t, tagResponse), &tag)

	// --- When: admin 拉取全部日志（不分页上限） ---
	page, items := env.listOperationLogs(t, env.adminToken, "?page_size=100")

	// --- Then: total 与 DB 行数一致（misleading_success_output 防线） ---
	dbTotal := env.countLogs(t, "1 = 1")
	if page.Total != dbTotal {
		t.Fatalf("GET /operation-logs total = %d, want DB 行数 %d", page.Total, dbTotal)
	}
	if int64(len(items)) != dbTotal {
		t.Fatalf("items 条数 = %d, want %d", len(items), dbTotal)
	}

	// --- Then: 时间倒序（最新在前）：首条 id == DB 最大 id，且序列严格递减 ---
	var maxID int64
	if err := env.db.Model(&model.OperationLog{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatalf("读取 operation_logs 最大 id 失败: %v", err)
	}
	if items[0].ID != maxID {
		t.Errorf("首条日志 id = %d, want DB 最大 id %d（最新在前）", items[0].ID, maxID)
	}
	for i := 0; i+1 < len(items); i++ {
		if items[i].ID <= items[i+1].ID {
			t.Errorf("日志排序 = id[%d]=%d <= id[%d]=%d, want 严格递减", i, items[i].ID, i+1, items[i+1].ID)
		}
	}

	// --- Then: customer_create DTO 字段齐全（操作人名/目标/content/ip/ua/created_at） ---
	created := findLog(t, items, "customer_create")
	if created.OperatorID == nil || *created.OperatorID != env.staff.ID {
		t.Errorf("customer_create operator_id = %v, want %d", created.OperatorID, env.staff.ID)
	}
	if created.OperatorName != env.staff.Username {
		t.Errorf("customer_create operator_name = %q, want %q（联表 users.username）", created.OperatorName, env.staff.Username)
	}
	if created.TargetType != "customer" || created.TargetID != customer.ID {
		t.Errorf("customer_create target = %s#%d, want customer#%d", created.TargetType, created.TargetID, customer.ID)
	}
	if created.Content == "" {
		t.Error("customer_create content 为空, want 可读描述")
	}
	if created.IP != "10.9.8.7" || created.UserAgent != "audit-probe/1.0" {
		t.Errorf("customer_create ip/ua = %q/%q, want 10.9.8.7/audit-probe/1.0", created.IP, created.UserAgent)
	}
	if created.CreatedAt.IsZero() {
		t.Error("customer_create created_at 为零值, want 写入时间")
	}

	// --- Then: admin 动作的操作人名是 admin ---
	tagCreated := findLog(t, items, "tag_create")
	if tagCreated.OperatorID == nil || *tagCreated.OperatorID != env.admin.ID {
		t.Errorf("tag_create operator_id = %v, want %d", tagCreated.OperatorID, env.admin.ID)
	}
	if tagCreated.OperatorName != env.admin.Username {
		t.Errorf("tag_create operator_name = %q, want %q", tagCreated.OperatorName, env.admin.Username)
	}
	if tagCreated.TargetType != "tag" || tagCreated.TargetID != tag.ID {
		t.Errorf("tag_create target = %s#%d, want tag#%d", tagCreated.TargetType, tagCreated.TargetID, tag.ID)
	}

	// --- When/Then: operator_id 筛选（staff 只有其自己的动作） ---
	if got := env.countLogs(t, "operator_id = ?", env.staff.ID); got == 0 {
		t.Fatal("DB 中 staff 日志行数为 0，前置动作未写审计")
	}
	staffPage, staffItems := env.listOperationLogs(t, env.adminToken, fmt.Sprintf("?operator_id=%d&page_size=100", env.staff.ID))
	if want := env.countLogs(t, "operator_id = ?", env.staff.ID); staffPage.Total != want {
		t.Errorf("?operator_id=%d total = %d, want DB 行数 %d", env.staff.ID, staffPage.Total, want)
	}
	for _, item := range staffItems {
		if item.OperatorID == nil || *item.OperatorID != env.staff.ID {
			t.Errorf("operator_id 筛选返回 operator_id = %v, want %d", item.OperatorID, env.staff.ID)
		}
		if item.OperatorName != env.staff.Username {
			t.Errorf("operator_id 筛选 operator_name = %q, want %q", item.OperatorName, env.staff.Username)
		}
	}

	// --- When/Then: action 筛选（精确匹配、可用 operator 交叉过滤） ---
	actionPage, actionItems := env.listOperationLogs(t, env.adminToken, "?action=customer_create&page_size=100")
	if actionPage.Total != 1 || len(actionItems) != 1 {
		t.Fatalf("?action=customer_create total/条数 = %d/%d, want 1/1", actionPage.Total, len(actionItems))
	}
	if actionItems[0].ID != created.ID || actionItems[0].TargetID != customer.ID {
		t.Errorf("action 筛选返回 = %+v, want customer_create#%d", actionItems[0], created.ID)
	}
	if cross, crossItems := env.listOperationLogs(t, env.adminToken,
		fmt.Sprintf("?action=customer_create&operator_id=%d", env.admin.ID)); cross.Total != 0 || len(crossItems) != 0 {
		t.Errorf("action+operator 交叉筛选 total/条数 = %d/%d, want 0/0", cross.Total, len(crossItems))
	}

	// --- When/Then: 日期筛选（UTC 日期边界）；非法日期宽松回退 ---
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	for query, want := range map[string]int64{
		"?start_date=" + today:      dbTotal,
		"?end_date=" + today:        dbTotal,
		"?start_date=" + yesterday:  dbTotal,
		"?end_date=" + yesterday:    0,
		"?start_date=not-a-date":    dbTotal,
		"?end_date=2020-01-01":      0,
		"?action=no_such_action":    0,
		"?page=abc&page_size=-5":    dbTotal,
		"?operator_id=not-a-number": dbTotal,
		"?operator_id=999999":       0,
	} {
		got, gotItems := env.listOperationLogs(t, env.adminToken, query+"&page_size=100")
		if got.Total != want {
			t.Errorf("GET /operation-logs%s total = %d, want %d", query, got.Total, want)
		}
		if want == 0 && string(got.Items) != "[]" {
			t.Errorf("GET /operation-logs%s items = %s, want []（空结果不是 null）", query, got.Items)
		}
		if want == 0 && len(gotItems) != 0 {
			t.Errorf("GET /operation-logs%s 条数 = %d, want 0", query, len(gotItems))
		}
	}

	// --- When/Then: 分页（page_size=2） ---
	p1, p1Items := env.listOperationLogs(t, env.adminToken, "?page=1&page_size=2")
	if p1.Total != dbTotal || p1.Page != 1 || p1.PageSize != 2 || len(p1Items) != 2 {
		t.Errorf("page=1&page_size=2 = total %d/page %d/size %d/条数 %d, want %d/1/2/2",
			p1.Total, p1.Page, p1.PageSize, len(p1Items), dbTotal)
	}
	_, p2Items := env.listOperationLogs(t, env.adminToken, "?page=2&page_size=2")
	if len(p2Items) != 2 || p2Items[0].ID >= p1Items[0].ID {
		t.Errorf("page=2 首条 = %+v, want 第 3 新（page=1 首条 id=%d）", p2Items, p1Items[0].ID)
	}
	if got, gotItems := env.listOperationLogs(t, env.adminToken, "?page=abc&page_size=999"); got.Page != 1 || got.PageSize != service.MaxPageSize || int64(len(gotItems)) != dbTotal {
		t.Errorf("非法分页回退 = page %d/size %d/条数 %d, want 1/%d/%d",
			got.Page, got.PageSize, len(gotItems), service.MaxPageSize, dbTotal)
	}

	// --- When: staff 再次动作（stale_state：查询后新增的日志必须出现在下一次查询首位） ---
	tagUpdate := env.authed(http.MethodPut, fmt.Sprintf("/api/v1/tags/%d", tag.ID),
		`{"name":"日志标签改","color":"#336699"}`, env.adminToken)
	if tagUpdate.Code != http.StatusOK {
		t.Fatalf("PUT /tags/%d status = %d, want 200 (body=%s)", tag.ID, tagUpdate.Code, tagUpdate.Body.String())
	}
	afterPage, afterItems := env.listOperationLogs(t, env.adminToken, "?action=tag_update&page_size=100")
	if afterPage.Total != 1 || len(afterItems) != 1 {
		t.Fatalf("?action=tag_update total/条数 = %d/%d, want 1/1（新写入日志可见）", afterPage.Total, len(afterItems))
	}
	if afterItems[0].ID != maxID+1 {
		t.Errorf("tag_update 日志 id = %d, want %d（新日志紧接着上一条）", afterItems[0].ID, maxID+1)
	}
	if allPage, allItems := env.listOperationLogs(t, env.adminToken, "?page_size=100"); allPage.Total != dbTotal+1 || allItems[0].Action != "tag_update" {
		t.Errorf("新日志后 total/首条 action = %d/%s, want %d/tag_update", allPage.Total, allItems[0].Action, dbTotal+1)
	}

	// --- Then: 权限（staff → 403、未登录 → 401），且拒绝时零写入 ---
	staffAttempt := env.authed(http.MethodGet, "/api/v1/operation-logs", "", env.staffToken)
	if staffAttempt.Code != http.StatusForbidden {
		t.Errorf("staff GET /operation-logs status = %d, want 403 (body=%s)", staffAttempt.Code, staffAttempt.Body.String())
	} else if envl := decodeEnvelope(t, staffAttempt); envl.Code != service.CodeForbidden {
		t.Errorf("staff GET /operation-logs code = %d, want %d", envl.Code, service.CodeForbidden)
	}
	if w := env.do(http.MethodGet, "/api/v1/operation-logs", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /operation-logs status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if got := env.countLogs(t, "1 = 1"); got != dbTotal+1 {
		t.Errorf("被拒请求后 operation_logs 行数 = %d, want %d（只读查询零写入）", got, dbTotal+1)
	}
}
