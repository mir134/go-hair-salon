package controller_test

// TestServices 是 todo 19 的验收测试（计划：`go test ./internal/controller -run TestServices -v -count=1`）：
//   - GET /services（both，含 category 信息）、POST/GET/PUT/DELETE /services（写仅 admin）（04-API.md:120-125）；
//   - price_cents 为 int64 整数分、duration_minutes、status（enabled/disabled）；
//   - 价格 0/负 → 400；非法分类/非法 id → 400；不存在 → 404（malformed_input，不得 500）；
//   - 停用服务仍可见（列表/历史），但新消费校验（EnsureEnabledForConsumption）→ 422；
//   - 已产生订单的服务禁止物理删除 → DELETE 422 并提示改停用；从未下单的服务可软删除（06-BUSINESS-RULES.md:16-18）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// serviceItemView 是服务项目 DTO 的测试镜像（price_cents 整数分，含分类名）。
type serviceItemView struct {
	ID              int64     `json:"id"`
	CategoryID      int64     `json:"category_id"`
	CategoryName    string    `json:"category_name"`
	Name            string    `json:"name"`
	PriceCents      int64     `json:"price_cents"`
	DurationMinutes int       `json:"duration_minutes"`
	Status          int       `json:"status"`
	Remark          string    `json:"remark"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// decodeServices 解析 data 为服务数组。
func decodeServices(t *testing.T, envl envelope) []serviceItemView {
	t.Helper()
	var items []serviceItemView
	if err := json.Unmarshal(envl.Data, &items); err != nil {
		t.Fatalf("解析服务数组失败: %v (data=%s)", err, envl.Data)
	}
	return items
}

// listServices 通过 API 拉取服务数组（query 可为空）。
func (e *customerEnv) listServices(t *testing.T, token, query string) []serviceItemView {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/services"+query, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /services%s status = %d, want 200 (body=%s)", query, w.Code, w.Body.String())
	}
	return decodeServices(t, decodeEnvelope(t, w))
}

// createService 通过 API 建服务并断言 201。
func (e *customerEnv) createService(t *testing.T, token, body string) serviceItemView {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/services", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /services status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created serviceItemView
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// serviceIDs 提取服务 id 序列。
func serviceIDs(items []serviceItemView) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// containsService 判断服务列表中是否存在指定 id。
func containsService(items []serviceItemView, id int64) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestServices(t *testing.T) {
	env := newCustomerEnv(t)
	ctx := context.Background()

	// --- When: staff 创建服务 ---
	w := env.authed(http.MethodPost, "/api/v1/services", `{"category_id":1,"name":"剪发","price_cents":5000}`, env.staffToken)

	// --- Then: 403 + 40300（04-API.md:112 创建/修改/删除仅 admin） ---
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff POST /services status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff POST 服务 code = %d, want %d", envl.Code, service.CodeForbidden)
	}

	// --- Given: 两个分类 ---
	hair := env.createCategory(t, env.adminToken, `{"name":"剪发","sort":10}`)
	dye := env.createCategory(t, env.adminToken, `{"name":"烫染","sort":20}`)

	// --- When: admin 创建启用服务（整数分价格/时长/备注） ---
	svc := env.createService(t, env.adminToken,
		fmt.Sprintf(`{"category_id":%d,"name":"男士剪发","price_cents":5000,"duration_minutes":30,"remark":"标准"}`, hair.ID))

	// --- Then: DTO 回显（含分类名），status 默认启用 ---
	if svc.ID <= 0 || svc.CategoryID != hair.ID || svc.CategoryName != "剪发" ||
		svc.Name != "男士剪发" || svc.PriceCents != 5000 || svc.DurationMinutes != 30 || svc.Status != model.StatusEnabled {
		t.Errorf("创建服务 DTO = %+v, want 男士剪发/剪发/5000/30/status=1", svc)
	}

	// --- When: admin 创建即停用（status=0） ---
	svcDisabled := env.createService(t, env.adminToken,
		fmt.Sprintf(`{"category_id":%d,"name":"女士剪发","price_cents":8000,"duration_minutes":60,"status":0}`, hair.ID))

	// --- Then: status=0 被正确写入（GORM default:1 不得覆盖显式停用） ---
	if svcDisabled.Status != model.StatusDisabled || svcDisabled.PriceCents != 8000 {
		t.Errorf("创建停用服务 DTO = %+v, want status=0/price=8000", svcDisabled)
	}

	// --- When/Then: malformed_input 全部 400（不得 500） ---
	badBodies := map[string]string{
		"价格 0":      fmt.Sprintf(`{"category_id":%d,"name":"免费","price_cents":0}`, hair.ID),
		"价格为负":      fmt.Sprintf(`{"category_id":%d,"name":"负价","price_cents":-100}`, hair.ID),
		"价格非整数":     fmt.Sprintf(`{"category_id":%d,"name":"小数价","price_cents":50.5}`, hair.ID),
		"分类不存在":     `{"category_id":999999,"name":"孤儿服务","price_cents":1000}`,
		"分类 id 为 0": `{"category_id":0,"name":"无分类","price_cents":1000}`,
		"名称为空":      fmt.Sprintf(`{"category_id":%d,"name":"   ","price_cents":1000}`, hair.ID),
		"时长为负":      fmt.Sprintf(`{"category_id":%d,"name":"负时长","price_cents":1000,"duration_minutes":-1}`, hair.ID),
		"非法 status": fmt.Sprintf(`{"category_id":%d,"name":"坏状态","price_cents":1000,"status":9}`, hair.ID),
	}
	for name, body := range badBodies {
		if w = env.authed(http.MethodPost, "/api/v1/services", body, env.adminToken); w.Code != http.StatusBadRequest {
			t.Errorf("POST 服务（%s）status = %d, want 400 (body=%s)", name, w.Code, w.Body.String())
		}
	}

	// --- When: staff 读取服务列表 ---
	items := env.listServices(t, env.staffToken, "")

	// --- Then: both 可查；按 id 升序；含分类名；停用服务仍可见（stale_state：列表可见、新消费不可用） ---
	if len(items) != 2 || items[0].ID != svc.ID || items[1].ID != svcDisabled.ID {
		t.Fatalf("服务列表 = %v, want [%d,%d]（id 升序）", serviceIDs(items), svc.ID, svcDisabled.ID)
	}
	if items[0].CategoryName != "剪发" || items[1].CategoryName != "剪发" {
		t.Errorf("服务列表分类名 = %q/%q, want 剪发/剪发", items[0].CategoryName, items[1].CategoryName)
	}
	if !containsService(items, svcDisabled.ID) {
		t.Error("停用服务必须仍在列表中可见（历史/管理需要）")
	}

	// --- When/Then: category_id / status 过滤 ---
	if only := env.listServices(t, env.staffToken, fmt.Sprintf("?category_id=%d", hair.ID)); len(only) != 2 {
		t.Errorf("?category_id=%d = %v, want 2 条", hair.ID, serviceIDs(only))
	}
	if only := env.listServices(t, env.staffToken, fmt.Sprintf("?category_id=%d", dye.ID)); len(only) != 0 {
		t.Errorf("?category_id=%d = %v, want 空", dye.ID, serviceIDs(only))
	}
	if only := env.listServices(t, env.staffToken, "?status=1"); len(only) != 1 || only[0].ID != svc.ID {
		t.Errorf("?status=1 = %v, want 仅启用服务 %d", serviceIDs(only), svc.ID)
	}
	if only := env.listServices(t, env.staffToken, "?status=0"); len(only) != 1 || only[0].ID != svcDisabled.ID {
		t.Errorf("?status=0 = %v, want 仅停用服务 %d", serviceIDs(only), svcDisabled.ID)
	}
	// 非法过滤值宽松回退为「不过滤」（与分类/分页一致，不得 500）
	if all := env.listServices(t, env.staffToken, "?status=abc&category_id=abc"); len(all) != 2 {
		t.Errorf("非法过滤参数 = %v, want 不过滤（2 条）", serviceIDs(all))
	}

	// --- When: staff 读取服务详情 ---
	w = env.authed(http.MethodGet, fmt.Sprintf("/api/v1/services/%d", svc.ID), "", env.staffToken)

	// --- Then: 200 + 含分类信息 ---
	if w.Code != http.StatusOK {
		t.Fatalf("GET /services/:id status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var detail serviceItemView
	decodeData(t, decodeEnvelope(t, w), &detail)
	if detail.ID != svc.ID || detail.CategoryName != "剪发" || detail.PriceCents != 5000 {
		t.Errorf("服务详情 DTO = %+v, want id=%d/剪发/5000", detail, svc.ID)
	}

	// --- When/Then: 不存在 → 404；非法 id → 400 ---
	if w = env.authed(http.MethodGet, "/api/v1/services/999999", "", env.staffToken); w.Code != http.StatusNotFound {
		t.Errorf("GET 不存在服务 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodGet, "/api/v1/services/abc", "", env.staffToken); w.Code != http.StatusBadRequest {
		t.Errorf("GET 非法服务 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- Given: 消费校验器（todo 21 将调用；此处直接验证 service 层守卫） ---
	catalog := service.NewServiceItemService(
		repository.NewServiceItemRepository(env.db),
		repository.NewServiceCategoryRepository(env.db),
	)

	// --- When: 启用服务用于新消费 ---
	usable, err := catalog.EnsureEnabledForConsumption(ctx, svc.ID)

	// --- Then: 通过并返回服务（含标准价，供订单快照） ---
	if err != nil {
		t.Fatalf("启用服务消费校验 err = %v, want nil", err)
	}
	if usable.ID != svc.ID || usable.PriceCents != 5000 {
		t.Errorf("消费校验返回 = %+v, want id=%d/5000", usable, svc.ID)
	}

	// --- When: 停用服务用于新消费（stale_state：可见但不可新消费） ---
	_, err = catalog.EnsureEnabledForConsumption(ctx, svcDisabled.ID)

	// --- Then: 422 + 明确提示（06-BUSINESS-RULES.md:18） ---
	var biz *service.BizError
	if !errors.As(err, &biz) || biz.Status != http.StatusUnprocessableEntity {
		t.Fatalf("停用服务消费校验 err = %v, want 422 BizError", err)
	}
	if !strings.Contains(biz.Message, "停用") {
		t.Errorf("停用服务消费校验 message = %q, want 含「停用」", biz.Message)
	}

	// --- When: admin 修改服务（名称/分类/价格/时长/状态） ---
	w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/services/%d", svcDisabled.ID),
		fmt.Sprintf(`{"category_id":%d,"name":"女士烫染","price_cents":12000,"duration_minutes":90,"status":1,"remark":"改价"}`, dye.ID),
		env.adminToken)

	// --- Then: 200 + 新值（含新分类名） ---
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /services status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var updated serviceItemView
	decodeData(t, decodeEnvelope(t, w), &updated)
	if updated.ID != svcDisabled.ID || updated.CategoryID != dye.ID || updated.CategoryName != "烫染" ||
		updated.Name != "女士烫染" || updated.PriceCents != 12000 || updated.DurationMinutes != 90 || updated.Status != model.StatusEnabled {
		t.Errorf("修改服务 DTO = %+v, want id=%d/烫染/女士烫染/12000/90/status=1", updated, svcDisabled.ID)
	}

	// --- When/Then: staff 修改/删除 → 403 ---
	if w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/services/%d", svc.ID), `{"name":"x","price_cents":1}`, env.staffToken); w.Code != http.StatusForbidden {
		t.Errorf("staff PUT 服务 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/services/%d", svc.ID), "", env.staffToken); w.Code != http.StatusForbidden {
		t.Errorf("staff DELETE 服务 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 修改不存在的服务 → 404；改价 0 / 非法分类 → 400 ---
	if w = env.authed(http.MethodPut, "/api/v1/services/999999", `{"category_id":1,"name":"x","price_cents":1}`, env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("PUT 不存在服务 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/services/%d", svc.ID), fmt.Sprintf(`{"category_id":%d,"name":"x","price_cents":0}`, hair.ID), env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("PUT 价格 0 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodPut, fmt.Sprintf("/api/v1/services/%d", svc.ID), `{"category_id":999999,"name":"x","price_cents":100}`, env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("PUT 非法分类 status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When: admin 删除从未下单的服务 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/services/%d", svcDisabled.ID), "", env.adminToken)

	// --- Then: 200 软删除（只置 deleted_at） ---
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE 未下单服务 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if containsService(env.listServices(t, env.adminToken, ""), svcDisabled.ID) {
		t.Error("软删除后服务仍出现在列表")
	}
	var deletedRow model.Service
	if err := env.db.Unscoped().First(&deletedRow, svcDisabled.ID).Error; err != nil {
		t.Fatalf("探查软删除服务失败: %v", err)
	}
	if !deletedRow.DeletedAt.Valid {
		t.Error("服务 deleted_at 为空, want 非空（软删除）")
	}
	// --- Then: 已软删除服务不可用于新消费（404，不再是有效服务） ---
	_, err = catalog.EnsureEnabledForConsumption(ctx, svcDisabled.ID)
	if !errors.As(err, &biz) || biz.Status != http.StatusNotFound {
		t.Errorf("软删除服务消费校验 err = %v, want 404 BizError", err)
	}

	// --- Given: 服务已产生订单（order_items 引用 service_id，账务数据不可物理删除） ---
	customer := env.createCustomer(t, env.staffToken, `{"name":"下单客户","phone":"13800004000"}`)
	order := model.Order{
		OrderNo: "T20260923000001", RequestID: "req-todo19-delete-guard",
		CustomerID: customer.ID, OriginalAmountCents: 5000, PaidAmountCents: 5000,
		PaymentMethod: model.PaymentMethodCash, Status: model.OrderStatusCompleted,
	}
	if err := env.db.Create(&order).Error; err != nil {
		t.Fatalf("造订单失败: %v", err)
	}
	orderItem := model.OrderItem{
		OrderID: order.ID, ServiceID: svc.ID, ServiceNameSnapshot: svc.Name,
		Quantity: 1, UnitPriceCents: 5000, AmountCents: 5000,
	}
	if err := env.db.Create(&orderItem).Error; err != nil {
		t.Fatalf("造订单明细失败: %v", err)
	}

	// --- When: admin 删除已下单服务 ---
	w = env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/services/%d", svc.ID), "", env.adminToken)

	// --- Then: 422 + 42200 + 提示改停用（禁止物理删除已产生订单的服务） ---
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE 已下单服务 status = %d, want 422 (body=%s)", w.Code, w.Body.String())
	}
	envl := decodeEnvelope(t, w)
	if envl.Code != service.CodeValidationFailed {
		t.Errorf("DELETE 已下单服务 code = %d, want %d", envl.Code, service.CodeValidationFailed)
	}
	if !strings.Contains(envl.Message, "停用") {
		t.Errorf("DELETE 已下单服务 message = %q, want 含「停用」的引导提示", envl.Message)
	}
	// --- Then: 服务未被删除（仍在列表、deleted_at 仍为空、订单明细仍在） ---
	if !containsService(env.listServices(t, env.adminToken, ""), svc.ID) {
		t.Error("删除被拒后服务应仍在列表")
	}
	var keptRow model.Service
	if err := env.db.First(&keptRow, svc.ID).Error; err != nil {
		t.Fatalf("探查服务失败: %v", err)
	}
	if keptRow.DeletedAt.Valid {
		t.Error("删除被拒后服务 deleted_at 不应被置空/置位")
	}
	var itemCount int64
	if err := env.db.Model(&model.OrderItem{}).Where("service_id = ?", svc.ID).Count(&itemCount).Error; err != nil {
		t.Fatalf("探查订单明细失败: %v", err)
	}
	if itemCount != 1 {
		t.Errorf("order_items 行数 = %d, want 1（历史快照必须保留）", itemCount)
	}

	// --- When/Then: 删除不存在的服务 → 404；非法路径 id → 400 ---
	if w = env.authed(http.MethodDelete, "/api/v1/services/999999", "", env.adminToken); w.Code != http.StatusNotFound {
		t.Errorf("DELETE 不存在服务 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if w = env.authed(http.MethodDelete, "/api/v1/services/abc", "", env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("DELETE 非法服务 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- Then: 审计日志覆盖 create/update/delete，operator 为 admin ---
	var actions []string
	if err := env.db.Model(&model.OperationLog{}).Where("action LIKE ?", "service_%").Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("查询服务审计日志: %v", err)
	}
	for _, want := range []string{"service_create", "service_update", "service_delete"} {
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
	if err := env.db.Where("action = ?", "service_delete").First(&deleteLog).Error; err != nil {
		t.Fatalf("查询 service_delete 日志: %v", err)
	}
	if deleteLog.OperatorID == nil || *deleteLog.OperatorID != env.admin.ID {
		t.Errorf("service_delete operator_id = %v, want %d（admin）", deleteLog.OperatorID, env.admin.ID)
	}
	if deleteLog.TargetType != "service" || deleteLog.TargetID != svcDisabled.ID {
		t.Errorf("service_delete target = %s#%d, want service#%d", deleteLog.TargetType, deleteLog.TargetID, svcDisabled.ID)
	}

	// --- When/Then: 未登录访问 → 401 ---
	if w = env.do(http.MethodGet, "/api/v1/services", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /services status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
