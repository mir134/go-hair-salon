package service_test

// TestAuditCoverage 是 todo 49 的验收测试
// （计划：`go test ./internal/service -run TestAuditCoverage -v -count=1`）：
//
//  1. 覆盖矩阵：以真实路由（router.New）+ httptest 驱动真实 controller/service
//     （不 mock、不直接调用 service），对每个已落地业务域执行真实动作，随后断言
//     operation_logs 中存在对应 action 行，且 operator_id 与执行者完全一致
//     （login_failed 无登录上下文 → operator_id 为 NULL）：
//     认证 / 客户 / 标签 / 服务分类 / 服务项目 / 订单（创建、结账、取消、明细增改删、退款）/
//     充值 / 余额调整 / 员工 / 用户管理 / 系统设置 / 备份 / 恢复 —— 共 12 业务域、27+ 动作。
//  2. 敏感字段擦除（08-DEPLOYMENT.md:90-92、06-BUSINESS-RULES.md:43,64）：
//     任何日志 content 不得出现明文密码、JWT（含 JWT 形状串）或 bcrypt 哈希（$2a$）。
//
// 盲区登记（todo 52 已补齐）：备份/恢复（action=backup/restore）曾无 API，
// 现经真实 HTTP 驱动断言；注意恢复会用备份时点的数据库整体替换当前库，
// 因此「恢复时点之前」的审计行会随旧库被替换（恢复前状态由恢复前安全备份保留），
// 恢复后的 backup/restore 审计行落在恢复后的库中并被本矩阵断言。
//
// 断言一律以 DB 行（operation_logs）为准，不以 HTTP 状态码/"日志行输出"为证据
// （misleading_success_output 防线）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/router"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

const (
	auditAdminPassword = "Audit-Admin-Pwd-1"
	auditStaffPassword = "Audit-Staff-Pwd-1"
	auditUserPassword  = "Audit-User-Pwd-2"
	auditResetPassword = "Audit-Reset-Pwd-2"
	auditJWTSecret     = "audit-coverage-jwt-secret"
	// auditClientIP / auditUserAgent 显式注入请求，用于断言审计来源（ip/ua）确实落库。
	auditClientIP    = "10.10.10.10"
	auditUserAgent   = "audit-coverage/1.0"
	auditAdminUser   = "admin"
	auditStaffUser   = "audit-staff"
	auditMinLogRows  = 25
	auditContentScan = "全部 operation_logs.content"
)

// auditEnv 是审计覆盖测试环境：复用 orderTestEnv 的临时库 + 真实路由 + admin/staff 账号。
type auditEnv struct {
	*orderTestEnv
	engine     *gin.Engine
	admin      *model.User
	staff      *model.User
	adminToken string
	staffToken string
}

// newAuditEnv 装配真实路由环境：临时 SQLite + 迁移 + settings 播种 + admin/staff 用户
// + 备份/恢复服务（todo 50-52 的审计动作需要真实 API）。
func newAuditEnv(t *testing.T) *auditEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	base := newOrderTestEnv(t)
	users := service.NewUserService(repository.NewUserRepository(base.db), repository.NewEmployeeRepository(base.db))
	ctx := context.Background()
	created, err := users.SeedAdmin(ctx, auditAdminPassword)
	if err != nil || !created {
		t.Fatalf("SeedAdmin: created=%v err=%v", created, err)
	}
	admin, err := users.FindByUsername(ctx, service.DefaultAdminUsername)
	if err != nil {
		t.Fatalf("FindByUsername(admin): %v", err)
	}
	staff, err := users.CreateUser(ctx, service.CreateUserInput{
		Username: auditStaffUser, Password: auditStaffPassword, Role: model.RoleStaff,
	})
	if err != nil {
		t.Fatalf("CreateUser(staff): %v", err)
	}

	// 备份/恢复服务（真实临时目录 + 真实 config.yaml；备份内容必须可打包）。
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	backupDir := filepath.Join(root, "backups")
	for _, dir := range []string{uploadDir, backupDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("DB_PATH: audit.db\n"), 0o644); err != nil {
		t.Fatalf("写 config.yaml: %v", err)
	}
	dbPath := base.dbPath
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	backups := service.NewBackupService(service.BackupServiceDeps{
		DB:         base.db,
		DBPath:     dbPath,
		UploadDir:  uploadDir,
		BackupDir:  backupDir,
		ConfigPath: configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(base.db)),
		Logger:     discard,
	})
	guard := service.NewMaintenanceGuard()

	engine := router.New(base.db, slog.New(slog.NewTextHandler(io.Discard, nil)),
		router.Options{JWTSecret: auditJWTSecret, TokenTTL: time.Hour, Backups: backups, Maintenance: guard})
	env := &auditEnv{orderTestEnv: base, engine: engine, admin: admin, staff: staff}
	env.adminToken = env.authLogin(t, auditAdminUser, auditAdminPassword)
	env.staffToken = env.authLogin(t, auditStaffUser, auditStaffPassword)
	return env
}

// call 发起一次真实 HTTP 请求（带审计来源头）。
func (e *auditEnv) call(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Forwarded-For", auditClientIP)
	req.Header.Set("User-Agent", auditUserAgent)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// expect 断言响应状态码（失败即终止：后续审计矩阵失去前提）。
func (e *auditEnv) expect(t *testing.T, want int, w *httptest.ResponseRecorder, label string) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("%s status = %d, want %d (body=%s)", label, w.Code, want, w.Body.String())
	}
}

// auditEnvelope 是统一信封的测试镜像（只取 code / data）。
type auditEnvelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// decodeAuditData 校验信封 code=0 并把 data 解析到 target。
func decodeAuditData(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	var envl auditEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v (body=%s)", err, w.Body.String())
	}
	if envl.Code != service.CodeOK {
		t.Fatalf("信封 code = %d, want 0 (body=%s)", envl.Code, w.Body.String())
	}
	if err := json.Unmarshal(envl.Data, target); err != nil {
		t.Fatalf("解析 data 失败: %v (data=%s)", err, envl.Data)
	}
}

// authLogin 走真实登录接口换取 token（成功即产生 action=login 审计）。
func (e *auditEnv) authLogin(t *testing.T, username, password string) string {
	t.Helper()
	w := e.call(http.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), "")
	e.expect(t, http.StatusOK, w, "login "+username)
	var data struct {
		Token string `json:"token"`
	}
	decodeAuditData(t, w, &data)
	if data.Token == "" {
		t.Fatalf("login(%s) token 为空", username)
	}
	return data.Token
}

// auditCase 是覆盖矩阵的一行：action 的全部日志行，operator_id 必须落在 allowed 内。
type auditCase struct {
	action  string
	allowed []*int64
	minRows int
	what    string
}

// operatorAllowed 判定 operator_id 是否属于允许集合（nil 表示无登录上下文）。
func operatorAllowed(allowed []*int64, got *int64) bool {
	for _, want := range allowed {
		if want == nil && got == nil {
			return true
		}
		if want != nil && got != nil && *want == *got {
			return true
		}
	}
	return false
}

// describeOperators 输出期望操作人集合（失败信息可读）。
func describeOperators(adminID, staffID int64, allowed []*int64) string {
	if len(allowed) == 1 && allowed[0] == nil {
		return "NULL（未认证）"
	}
	parts := make([]string, 0, len(allowed))
	for _, id := range allowed {
		switch {
		case id == nil:
			parts = append(parts, "NULL")
		case *id == adminID:
			parts = append(parts, fmt.Sprintf("admin#%d", adminID))
		case *id == staffID:
			parts = append(parts, fmt.Sprintf("staff#%d", staffID))
		default:
			parts = append(parts, fmt.Sprintf("#%d", *id))
		}
	}
	return strings.Join(parts, " / ")
}

func TestAuditCoverage(t *testing.T) {
	env := newAuditEnv(t)
	adminID, staffID := env.admin.ID, env.staff.ID

	// ============ 认证域 ============

	// --- When: 错误密码登录（未认证上下文） ---
	wrongLogin := env.call(http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"wrong-password-1"}`, "")
	// --- Then: 401（login_failed 在矩阵中断言） ---
	if wrongLogin.Code != http.StatusUnauthorized {
		t.Fatalf("login_failed status = %d, want 401 (body=%s)", wrongLogin.Code, wrongLogin.Body.String())
	}
	// --- When: 登出（无黑名单，仅审计） ---
	env.expect(t, http.StatusOK, env.call(http.MethodPost, "/api/v1/auth/logout", "", env.adminToken), "logout")

	// ============ 客户域 ============

	var customer struct {
		ID int64 `json:"id"`
	}
	createCustomer := env.call(http.MethodPost, "/api/v1/customers",
		`{"name":"审计客户","phone":"13900000001"}`, env.staffToken)
	env.expect(t, http.StatusCreated, createCustomer, "customer_create")
	decodeAuditData(t, createCustomer, &customer)

	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/customers/%d", customer.ID),
		`{"name":"审计客户改","phone":"13900000001"}`, env.staffToken), "customer_update")

	var disposable struct {
		ID int64 `json:"id"`
	}
	createDisposable := env.call(http.MethodPost, "/api/v1/customers",
		`{"name":"审计待删客户","phone":"13900000002"}`, env.staffToken)
	env.expect(t, http.StatusCreated, createDisposable, "customer_create(待删)")
	decodeAuditData(t, createDisposable, &disposable)
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/customers/%d", disposable.ID), "", env.adminToken), "customer_delete")

	// ============ 标签域 ============

	var tag struct {
		ID int64 `json:"id"`
	}
	tagCreated := env.call(http.MethodPost, "/api/v1/tags", `{"name":"审计标签","color":"#112233"}`, env.adminToken)
	env.expect(t, http.StatusCreated, tagCreated, "tag_create")
	decodeAuditData(t, tagCreated, &tag)
	env.expect(t, http.StatusOK, env.call(http.MethodPut, fmt.Sprintf("/api/v1/tags/%d", tag.ID),
		`{"name":"审计标签改","color":"#112233"}`, env.adminToken), "tag_update")
	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/customers/%d/tags", customer.ID),
		fmt.Sprintf(`{"tag_id":%d}`, tag.ID), env.staffToken), "tag_attach")
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/customers/%d/tags/%d", customer.ID, tag.ID), "", env.staffToken), "tag_detach")
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/tags/%d", tag.ID), "", env.adminToken), "tag_delete")

	// ============ 服务分类域 ============

	var categoryA, categoryB struct {
		ID int64 `json:"id"`
	}
	categoryACreated := env.call(http.MethodPost, "/api/v1/service-categories",
		`{"name":"审计分类A","sort":1}`, env.adminToken)
	env.expect(t, http.StatusCreated, categoryACreated, "service_category_create")
	decodeAuditData(t, categoryACreated, &categoryA)
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/service-categories/%d", categoryA.ID),
		`{"name":"审计分类A改","sort":1}`, env.adminToken), "service_category_update")
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/service-categories/%d", categoryA.ID), "", env.adminToken), "service_category_delete")

	categoryBCreated := env.call(http.MethodPost, "/api/v1/service-categories",
		`{"name":"审计分类B","sort":2}`, env.adminToken)
	env.expect(t, http.StatusCreated, categoryBCreated, "service_category_create(B)")
	decodeAuditData(t, categoryBCreated, &categoryB)

	// ============ 服务项目域 ============

	var item, itemDisposable struct {
		ID int64 `json:"id"`
	}
	itemCreated := env.call(http.MethodPost, "/api/v1/services",
		fmt.Sprintf(`{"category_id":%d,"name":"审计服务","price_cents":5000}`, categoryB.ID), env.adminToken)
	env.expect(t, http.StatusCreated, itemCreated, "service_create")
	decodeAuditData(t, itemCreated, &item)

	itemDisposableCreated := env.call(http.MethodPost, "/api/v1/services",
		fmt.Sprintf(`{"category_id":%d,"name":"审计服务待删","price_cents":3000}`, categoryB.ID), env.adminToken)
	env.expect(t, http.StatusCreated, itemDisposableCreated, "service_create(待删)")
	decodeAuditData(t, itemDisposableCreated, &itemDisposable)
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/services/%d", itemDisposable.ID),
		fmt.Sprintf(`{"category_id":%d,"name":"审计服务待删改","price_cents":3000}`, categoryB.ID),
		env.adminToken), "service_update")
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/services/%d", itemDisposable.ID), "", env.adminToken), "service_delete")

	// ============ 订单域：直接完成 + 退款 ============

	var order struct {
		ID int64 `json:"id"`
	}
	orderCreated := env.call(http.MethodPost, "/api/v1/orders", fmt.Sprintf(
		`{"request_id":"audit-order-1","customer_id":%d,"payment_method":"cash","status":"completed",`+
			`"items":[{"service_id":%d,"quantity":1,"unit_price_cents":5000}]}`,
		customer.ID, item.ID), env.staffToken)
	env.expect(t, http.StatusCreated, orderCreated, "order_create")
	decodeAuditData(t, orderCreated, &order)
	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/orders/%d/refund", order.ID), "", env.adminToken), "order_refund")

	// ============ 订单域：挂单明细编辑 + 结账 ============

	var pending struct {
		ID int64 `json:"id"`
	}
	pendingCreated := env.call(http.MethodPost, "/api/v1/orders", fmt.Sprintf(
		`{"request_id":"audit-pending-1","customer_id":%d,"payment_method":"cash","status":"pending",`+
			`"items":[{"service_id":%d,"quantity":1,"unit_price_cents":5000}]}`,
		customer.ID, item.ID), env.staffToken)
	env.expect(t, http.StatusCreated, pendingCreated, "order(pending)")
	decodeAuditData(t, pendingCreated, &pending)

	var added struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	addedResponse := env.call(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/items", pending.ID),
		fmt.Sprintf(`{"service_id":%d,"quantity":1,"unit_price_cents":5000}`, item.ID), env.staffToken)
	env.expect(t, http.StatusCreated, addedResponse, "order_item_add")
	decodeAuditData(t, addedResponse, &added)
	if len(added.Items) != 2 {
		t.Fatalf("追加明细后订单明细数 = %d, want 2", len(added.Items))
	}
	addedItemID := added.Items[len(added.Items)-1].ID

	// 改价仅 admin 且必填原因（06 §3.1）→ order_item_update
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/orders/%d/items/%d", pending.ID, addedItemID),
		`{"unit_price_cents":4000,"discount_reason":"审计改价"}`, env.adminToken), "order_item_update")
	// 删除明细（both；软删除）→ order_item_delete
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/orders/%d/items/%d", pending.ID, addedItemID), "", env.staffToken), "order_item_delete")
	// 结账（pending → completed）→ order_pay
	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/orders/%d/pay", pending.ID), `{"payment_method":"cash"}`, env.staffToken), "order_pay")

	// ============ 订单域：取消 ============

	var pendingCancel struct {
		ID int64 `json:"id"`
	}
	pendingCancelCreated := env.call(http.MethodPost, "/api/v1/orders", fmt.Sprintf(
		`{"request_id":"audit-pending-2","customer_id":%d,"payment_method":"cash","status":"pending",`+
			`"items":[{"service_id":%d,"quantity":1,"unit_price_cents":5000}]}`,
		customer.ID, item.ID), env.staffToken)
	env.expect(t, http.StatusCreated, pendingCancelCreated, "order(pending-2)")
	decodeAuditData(t, pendingCancelCreated, &pendingCancel)
	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/orders/%d/cancel", pendingCancel.ID), "", env.adminToken), "order_cancel")

	// ============ 充值域：充值 + 冲正 ============

	var recharge struct {
		ID int64 `json:"id"`
	}
	rechargeCreated := env.call(http.MethodPost, "/api/v1/recharges", fmt.Sprintf(
		`{"request_id":"audit-recharge-1","customer_id":%d,"recharge_amount_cents":10000,`+
			`"gift_amount_cents":2000,"payment_method":"cash"}`, customer.ID), env.staffToken)
	env.expect(t, http.StatusCreated, rechargeCreated, "recharge")
	decodeAuditData(t, rechargeCreated, &recharge)
	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/recharges/%d/refund", recharge.ID), "", env.adminToken), "recharge_refund")

	// ============ 余额调整域 ============

	env.expect(t, http.StatusOK, env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/customers/%d/balance-adjustments", customer.ID),
		`{"amount_cents":500,"reason":"审计调整"}`, env.adminToken), "balance_adjust")

	// ============ 员工域 ============

	var employee struct {
		ID int64 `json:"id"`
	}
	employeeCreated := env.call(http.MethodPost, "/api/v1/employees",
		`{"name":"审计员工","position":"理发师"}`, env.adminToken)
	env.expect(t, http.StatusCreated, employeeCreated, "employee_create")
	decodeAuditData(t, employeeCreated, &employee)
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/employees/%d", employee.ID),
		`{"name":"审计员工改","position":"理发师"}`, env.adminToken), "employee_update")
	env.expect(t, http.StatusOK, env.call(http.MethodDelete,
		fmt.Sprintf("/api/v1/employees/%d", employee.ID), "", env.adminToken), "employee_disable")

	// ============ 用户管理域（todo 39） ============

	var managedUser struct {
		ID int64 `json:"id"`
	}
	userCreated := env.call(http.MethodPost, "/api/v1/users",
		fmt.Sprintf(`{"username":"audit-user-2","role":"staff","password":%q}`, auditUserPassword), env.adminToken)
	env.expect(t, http.StatusCreated, userCreated, "user_create")
	decodeAuditData(t, userCreated, &managedUser)
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d", managedUser.ID), `{"status":0}`, env.adminToken), "user_disable")
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d", managedUser.ID), `{"status":1}`, env.adminToken), "user_enable")
	env.expect(t, http.StatusOK, env.call(http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d", managedUser.ID),
		fmt.Sprintf(`{"password":%q}`, auditResetPassword), env.adminToken), "user_password_reset")

	// ============ 系统设置域 ============

	env.expect(t, http.StatusOK, env.call(http.MethodPut, "/api/v1/settings/shop_name",
		`{"value":"审计测试门店"}`, env.adminToken), "setting_update")

	// ============ 备份 / 恢复域（todo 50-52，真实 HTTP 驱动） ============

	backupCreated := env.call(http.MethodPost, "/api/v1/backups", "", env.adminToken)
	env.expect(t, http.StatusCreated, backupCreated, "backup")
	var backupInfo struct {
		Name string `json:"name"`
	}
	decodeAuditData(t, backupCreated, &backupInfo)
	if backupInfo.Name == "" {
		t.Fatal("备份创建响应缺少 name")
	}
	// 恢复会用备份时点的数据库整体替换当前库：备份时点之后的审计行随旧库被替换
	//（恢复前状态由「恢复前安全备份」保留），因此恢复完成后再补一次手动备份，
	// 让 action=backup 的审计行落在恢复后的库中（backup 动作持续可审计）。
	restored := env.call(http.MethodPost,
		fmt.Sprintf("/api/v1/backups/%s/restore", backupInfo.Name), `{"confirm":true}`, env.adminToken)
	env.expect(t, http.StatusOK, restored, "restore")
	env.expect(t, http.StatusCreated,
		env.call(http.MethodPost, "/api/v1/backups", "", env.adminToken), "backup(恢复后)")

	// ============ 覆盖矩阵断言（DB 真值） ============

	cases := []auditCase{
		// 认证：login 有 admin/staff 两个执行者；login_failed 无登录上下文 → NULL。
		{action: "login", allowed: []*int64{&adminID, &staffID}, minRows: 2, what: "认证"},
		{action: "login_failed", allowed: []*int64{nil}, what: "认证"},
		{action: "logout", allowed: []*int64{&adminID}, what: "认证"},
		// 客户：创建/更新由 staff，删除由 admin。
		{action: "customer_create", allowed: []*int64{&staffID}, minRows: 2, what: "客户"},
		{action: "customer_update", allowed: []*int64{&staffID}, what: "客户"},
		{action: "customer_delete", allowed: []*int64{&adminID}, what: "客户"},
		// 标签：标签 CRUD 由 admin；挂/摘标签属于编辑客户（both，staff 执行）。
		{action: "tag_create", allowed: []*int64{&adminID}, what: "标签"},
		{action: "tag_update", allowed: []*int64{&adminID}, what: "标签"},
		{action: "tag_delete", allowed: []*int64{&adminID}, what: "标签"},
		{action: "tag_attach", allowed: []*int64{&staffID}, what: "标签"},
		{action: "tag_detach", allowed: []*int64{&staffID}, what: "标签"},
		// 服务分类 / 服务项目：全部 admin。
		{action: "service_category_create", allowed: []*int64{&adminID}, minRows: 2, what: "服务分类"},
		{action: "service_category_update", allowed: []*int64{&adminID}, what: "服务分类"},
		{action: "service_category_delete", allowed: []*int64{&adminID}, what: "服务分类"},
		{action: "service_create", allowed: []*int64{&adminID}, minRows: 2, what: "服务项目"},
		{action: "service_update", allowed: []*int64{&adminID}, what: "服务项目"},
		{action: "service_delete", allowed: []*int64{&adminID}, what: "服务项目"},
		// 订单：创建/结账/明细增删由 staff；改价/取消/退款由 admin。
		{action: "order_create", allowed: []*int64{&staffID}, what: "订单"},
		{action: "order_pay", allowed: []*int64{&staffID}, what: "订单"},
		{action: "order_cancel", allowed: []*int64{&adminID}, what: "订单"},
		{action: "order_item_add", allowed: []*int64{&staffID}, what: "订单"},
		{action: "order_item_update", allowed: []*int64{&adminID}, what: "订单"},
		{action: "order_item_delete", allowed: []*int64{&staffID}, what: "订单"},
		{action: "order_refund", allowed: []*int64{&adminID}, what: "订单"},
		// 充值：充值由 staff，冲正由 admin。
		{action: "recharge", allowed: []*int64{&staffID}, what: "充值"},
		{action: "recharge_refund", allowed: []*int64{&adminID}, what: "充值"},
		// 余额调整 / 员工 / 用户管理 / 系统设置：仅 admin。
		{action: "balance_adjust", allowed: []*int64{&adminID}, what: "余额调整"},
		{action: "employee_create", allowed: []*int64{&adminID}, what: "员工"},
		{action: "employee_update", allowed: []*int64{&adminID}, what: "员工"},
		{action: "employee_disable", allowed: []*int64{&adminID}, what: "员工"},
		{action: "user_create", allowed: []*int64{&adminID}, what: "用户管理"},
		{action: "user_disable", allowed: []*int64{&adminID}, what: "用户管理"},
		{action: "user_enable", allowed: []*int64{&adminID}, what: "用户管理"},
		{action: "user_password_reset", allowed: []*int64{&adminID}, what: "用户管理"},
		{action: "setting_update", allowed: []*int64{&adminID}, what: "系统设置"},
		// 备份/恢复：仅 admin（todo 50-52）；恢复后的 backup 审计行来自「恢复后补备」。
		{action: "backup", allowed: []*int64{&adminID}, what: "备份"},
		{action: "restore", allowed: []*int64{&adminID}, what: "恢复"},
	}

	coveredDomains := map[string]int{}
	for _, tc := range cases {
		var rows []model.OperationLog
		if err := env.db.Where("action = ?", tc.action).Find(&rows).Error; err != nil {
			t.Fatalf("读取 operation_logs[action=%s] 失败: %v", tc.action, err)
		}
		wantRows := tc.minRows
		if wantRows == 0 {
			wantRows = 1
		}
		if len(rows) < wantRows {
			t.Errorf("[%s] operation_logs[action=%s] 行数 = %d, want >= %d（动作未审计）",
				tc.what, tc.action, len(rows), wantRows)
			continue
		}
		coveredDomains[tc.what]++
		for i := range rows {
			if !operatorAllowed(tc.allowed, rows[i].OperatorID) {
				t.Errorf("[%s] operation_logs[action=%s] id=%d operator_id = %v, want %s",
					tc.what, tc.action, rows[i].ID, rows[i].OperatorID,
					describeOperators(adminID, staffID, tc.allowed))
			}
		}
	}
	if len(coveredDomains) < 10 {
		t.Errorf("覆盖业务域数 = %d（%v）, want >= 10", len(coveredDomains), coveredDomains)
	}

	// ============ 审计来源（ip/ua）与总量下限 ============

	var totalLogs int64
	if err := env.db.Model(&model.OperationLog{}).Count(&totalLogs).Error; err != nil {
		t.Fatalf("统计 operation_logs 失败: %v", err)
	}
	if totalLogs < auditMinLogRows {
		t.Errorf("operation_logs 总数 = %d, want >= %d（覆盖动作过少）", totalLogs, auditMinLogRows)
	}
	var withSource int64
	if err := env.db.Model(&model.OperationLog{}).
		Where("ip = ? AND user_agent = ?", auditClientIP, auditUserAgent).
		Count(&withSource).Error; err != nil {
		t.Fatalf("统计审计来源失败: %v", err)
	}
	if withSource == 0 {
		t.Error("没有任何日志记录 ip/user_agent（审计来源缺失）")
	}

	// ============ 敏感字段擦除 ============

	var all []model.OperationLog
	if err := env.db.Order("id ASC").Find(&all).Error; err != nil {
		t.Fatalf("读取全部 operation_logs 失败: %v", err)
	}
	jwtShaped := regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	// 正则有效性自检：真实签发的 JWT 必须命中该模式，否则下面的擦除断言是真空的。
	if !jwtShaped.MatchString(env.adminToken) {
		t.Fatal("JWT 形状正则未命中真实 token：擦除断言失去意义")
	}
	secrets := []struct{ name, value string }{
		{"初始管理员密码", auditAdminPassword},
		{"staff 密码", auditStaffPassword},
		{"新建用户密码", auditUserPassword},
		{"重置密码", auditResetPassword},
		{"admin JWT", env.adminToken},
		{"staff JWT", env.staffToken},
	}
	for i := range all {
		row := &all[i]
		for _, secret := range secrets {
			if strings.Contains(row.Content, secret.value) {
				t.Errorf("%s 泄漏 %s: operation_logs id=%d action=%s content=%q",
					auditContentScan, secret.name, row.ID, row.Action, row.Content)
			}
		}
		if strings.Contains(row.Content, "$2a$") {
			t.Errorf("%s 含 bcrypt 哈希: operation_logs id=%d action=%s content=%q",
				auditContentScan, row.ID, row.Action, row.Content)
		}
		if jwtShaped.MatchString(row.Content) {
			t.Errorf("%s 含 JWT 形状串: operation_logs id=%d action=%s content=%q",
				auditContentScan, row.ID, row.Action, row.Content)
		}
	}
}
