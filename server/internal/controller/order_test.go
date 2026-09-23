package controller_test

// TestOrdersList / TestOrdersDetail / TestOrdersIdempotent 是 todo 22 的验收测试
// （计划：`go test ./internal/controller -run TestOrdersList -v -count=1`，任务书：-run 'TestOrders'）：
//   - GET /orders（both）：status/customer_id/employee_id/start_date/end_date 筛选、分页、sort=recent，
//     DTO 含 customer/employee 名（04-API.md:127-135,277-299、plan todo 22）；
//   - GET /orders/:id（both）：含 items 快照（service_name_snapshot），不存在→404，非法 id→400；
//   - 幂等：同 request_id 二次创建返回原订单（200）且表行数不变、余额只扣一次；
//   - malformed 日期/分页参数宽松回退（与既有列表接口一致，不得 500）。

import (
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

// orderItemAPIView 是订单明细 DTO 的测试镜像（整数分 + 服务名快照）。
type orderItemAPIView struct {
	ID                  int64  `json:"id"`
	OrderID             int64  `json:"order_id"`
	ServiceID           int64  `json:"service_id"`
	ServiceNameSnapshot string `json:"service_name_snapshot"`
	Quantity            int    `json:"quantity"`
	UnitPriceCents      int64  `json:"unit_price_cents"`
	DiscountAmountCents int64  `json:"discount_amount_cents"`
	AmountCents         int64  `json:"amount_cents"`
	EmployeeID          *int64 `json:"employee_id"`
}

// orderAPIView 是订单 DTO 的测试镜像（含 customer/employee 名与明细快照）。
type orderAPIView struct {
	ID                  int64              `json:"id"`
	OrderNo             string             `json:"order_no"`
	CustomerID          int64              `json:"customer_id"`
	CustomerName        string             `json:"customer_name"`
	EmployeeID          *int64             `json:"employee_id"`
	EmployeeName        string             `json:"employee_name"`
	OriginalAmountCents int64              `json:"original_amount_cents"`
	DiscountAmountCents int64              `json:"discount_amount_cents"`
	PaidAmountCents     int64              `json:"paid_amount_cents"`
	PaymentMethod       string             `json:"payment_method"`
	Status              string             `json:"status"`
	Items               []orderItemAPIView `json:"items"`
	CreatedAt           time.Time          `json:"created_at"`
}

// seedEmployee 直接落库员工（员工维护 API 属 todo 38-40）。
func seedEmployee(t *testing.T, env *customerEnv, name string) model.Employee {
	t.Helper()
	employee := model.Employee{Name: name, Status: model.StatusEnabled}
	if err := env.db.Create(&employee).Error; err != nil {
		t.Fatalf("造员工失败: %v", err)
	}
	return employee
}

// seedServiceViaAPI 通过 API 建分类 + 服务，返回服务 DTO（避免依赖自增 id 假设）。
func (e *customerEnv) seedServiceViaAPI(t *testing.T, name string, priceCents int64) serviceItemView {
	t.Helper()
	category := e.createCategory(t, e.adminToken, fmt.Sprintf(`{"name":%q,"sort":10}`, name+"分类"))
	return e.createService(t, e.adminToken,
		fmt.Sprintf(`{"category_id":%d,"name":%q,"price_cents":%d}`, category.ID, name, priceCents))
}

// orderItemJSON 构造单行明细 JSON。
func orderItemJSON(serviceID int64, quantity int, unitPriceCents int64) string {
	return fmt.Sprintf(`{"service_id":%d,"quantity":%d,"unit_price_cents":%d}`, serviceID, quantity, unitPriceCents)
}

// orderCreateJSON 构造 POST /orders 请求体（字段与 web/src/api/order.ts:63-73 对齐）。
func orderCreateJSON(requestID string, customerID int64, employeeID *int64, method string, items ...string) string {
	employee := "null"
	if employeeID != nil {
		employee = fmt.Sprintf("%d", *employeeID)
	}
	return fmt.Sprintf(
		`{"request_id":%q,"customer_id":%d,"employee_id":%s,"payment_method":%q,"status":"completed","items":[%s]}`,
		requestID, customerID, employee, method, strings.Join(items, ","),
	)
}

// postOrder 通过 API 创建订单并断言 201，返回订单 DTO。
func (e *customerEnv) postOrder(t *testing.T, token, body string) orderAPIView {
	t.Helper()
	w := e.authed(http.MethodPost, "/api/v1/orders", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /orders status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var created orderAPIView
	decodeData(t, decodeEnvelope(t, w), &created)
	return created
}

// listOrders 通过 API 拉取订单列表（query 可为空），返回分页结构。
func (e *customerEnv) listOrders(t *testing.T, token, query string) pageData {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/orders"+query, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /orders%s status = %d, want 200 (body=%s)", query, w.Code, w.Body.String())
	}
	return decodePage(t, decodeEnvelope(t, w))
}

// decodeOrders 解析分页 items 为订单 DTO 列表。
func decodeOrders(t *testing.T, page pageData) []orderAPIView {
	t.Helper()
	var items []orderAPIView
	if err := json.Unmarshal(page.Items, &items); err != nil {
		t.Fatalf("解析订单 items 失败: %v (items=%s)", err, page.Items)
	}
	return items
}

// getOrderAPI 请求订单详情，返回原始响应。
func (e *customerEnv) getOrderAPI(token string, id string) *httptest.ResponseRecorder {
	return e.authed(http.MethodGet, "/api/v1/orders/"+id, "", token)
}

func TestOrdersList(t *testing.T) {
	env := newCustomerEnv(t)
	custA := env.createCustomer(t, env.staffToken, `{"name":"订单客户甲","phone":"13800005001"}`)
	custB := env.createCustomer(t, env.staffToken, `{"name":"订单客户乙","phone":"13800005002"}`)
	empA := seedEmployee(t, env, "员工甲")
	empB := seedEmployee(t, env, "员工乙")
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	perm := env.seedServiceViaAPI(t, "烫发", 8000)
	// 余额支付造单前置：给客户甲充值（本 todo 不测充值 API）。
	if err := env.db.Model(&model.Customer{}).Where("id = ?", custA.ID).Update("balance_cents", 50000).Error; err != nil {
		t.Fatalf("注入余额失败: %v", err)
	}

	// --- Given: 3 张订单（甲/员工甲/现金、乙/无员工/微信、甲/员工乙/余额） ---
	orderA := env.postOrder(t, env.adminToken,
		orderCreateJSON("req-list-a", custA.ID, &empA.ID, "cash", orderItemJSON(hair.ID, 1, 5000)))
	orderB := env.postOrder(t, env.staffToken,
		orderCreateJSON("req-list-b", custB.ID, nil, "wechat", orderItemJSON(perm.ID, 2, 8000)))
	orderC := env.postOrder(t, env.staffToken,
		orderCreateJSON("req-list-c", custA.ID, &empB.ID, "balance", orderItemJSON(hair.ID, 1, 5000)))

	// --- When: both（staff）拉取列表 ---
	page := env.listOrders(t, env.staffToken, "?page_size=100")

	// --- Then: total=3、时间倒序（最新在前）、DTO 含客户/员工名 ---
	if page.Total != 3 {
		t.Fatalf("订单列表 total = %d, want 3", page.Total)
	}
	items := decodeOrders(t, page)
	if len(items) != 3 || items[0].ID != orderC.ID || items[2].ID != orderA.ID {
		t.Fatalf("订单列表顺序 = %v, want [%d,%d,%d]（最新在前）", orderIDs(items), orderC.ID, orderB.ID, orderA.ID)
	}
	if items[0].CustomerName != "订单客户甲" || items[0].EmployeeName != "员工乙" {
		t.Errorf("订单 C 名字 = %q/%q, want 订单客户甲/员工乙", items[0].CustomerName, items[0].EmployeeName)
	}
	if items[0].OriginalAmountCents != 5000 || items[0].PaidAmountCents != 5000 || items[0].Status != model.OrderStatusCompleted {
		t.Errorf("订单 C 金额/状态 = %+v, want 5000/5000/completed", items[0])
	}
	if items[1].EmployeeID != nil || items[1].EmployeeName != "" {
		t.Errorf("无员工订单 employee = %v/%q, want nil/空", items[1].EmployeeID, items[1].EmployeeName)
	}

	// --- When/Then: status 筛选 ---
	if got := env.listOrders(t, env.staffToken, "?status=completed&page_size=100"); got.Total != 3 {
		t.Errorf("?status=completed total = %d, want 3", got.Total)
	}
	if got := env.listOrders(t, env.staffToken, "?status=pending&page_size=100"); got.Total != 0 {
		t.Errorf("?status=pending total = %d, want 0", got.Total)
	}

	// --- When/Then: customer_id / employee_id 筛选 ---
	if got := env.listOrders(t, env.staffToken, fmt.Sprintf("?customer_id=%d&page_size=100", custA.ID)); got.Total != 2 {
		t.Errorf("?customer_id=%d total = %d, want 2", custA.ID, got.Total)
	}
	if got := env.listOrders(t, env.staffToken, fmt.Sprintf("?employee_id=%d&page_size=100", empA.ID)); got.Total != 1 {
		t.Errorf("?employee_id=%d total = %d, want 1", empA.ID, got.Total)
	} else if rows := decodeOrders(t, got); rows[0].ID != orderA.ID {
		t.Errorf("?employee_id=%d items = %v, want [%d]", empA.ID, orderIDs(rows), orderA.ID)
	}

	// --- When/Then: 日期筛选（UTC 日期边界，start 含当日 0 点、end 含当日 23:59:59） ---
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	for query, want := range map[string]int64{
		"?start_date=" + today:                               3,
		"?end_date=" + today:                                 3,
		"?start_date=" + yesterday + "&end_date=" + tomorrow: 3,
		"?start_date=" + tomorrow:                            0,
		"?end_date=" + yesterday:                             0,
		// 非法日期宽松回退为不过滤（与分页/过滤参数一致，不得 500）
		"?start_date=not-a-date": 3,
	} {
		if got := env.listOrders(t, env.staffToken, query+"&page_size=100"); got.Total != want {
			t.Errorf("GET /orders%s total = %d, want %d", query, got.Total, want)
		}
	}

	// --- When/Then: 分页 ---
	page1 := env.listOrders(t, env.staffToken, "?page=1&page_size=2")
	if page1.Total != 3 || page1.Page != 1 || page1.PageSize != 2 || len(decodeOrders(t, page1)) != 2 {
		t.Errorf("page=1&page_size=2 = total %d/page %d/size %d, want 3/1/2", page1.Total, page1.Page, page1.PageSize)
	}
	page2 := env.listOrders(t, env.staffToken, "?page=2&page_size=2")
	if rows := decodeOrders(t, page2); len(rows) != 1 || rows[0].ID != orderA.ID {
		t.Errorf("page=2&page_size=2 items = %v, want [%d]", orderIDs(rows), orderA.ID)
	}
	// 非法分页宽松回退默认值
	badPage := env.listOrders(t, env.staffToken, "?page=abc&page_size=-5")
	if badPage.Page != 1 || badPage.PageSize != 20 {
		t.Errorf("非法分页 page/page_size = %d/%d, want 1/20", badPage.Page, badPage.PageSize)
	}

	// --- When/Then: sort=recent（默认即最新在前） ---
	if got := env.listOrders(t, env.staffToken, "?sort=recent&page_size=100"); decodeOrders(t, got)[0].ID != orderC.ID {
		t.Errorf("sort=recent 首条 = %v, want %d", orderIDs(decodeOrders(t, got)), orderC.ID)
	}
	// 未知 sort 值宽松回退默认排序
	if got := env.listOrders(t, env.staffToken, "?sort=unknown&page_size=100"); got.Total != 3 {
		t.Errorf("未知 sort total = %d, want 3", got.Total)
	}

	// --- Then: 未登录 → 401 ---
	if w := env.do(http.MethodGet, "/api/v1/orders", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /orders status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}

func TestOrdersDetail(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"详情客户","phone":"13800005003"}`)
	employee := seedEmployee(t, env, "详情员工")
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	perm := env.seedServiceViaAPI(t, "烫发", 8000)

	// --- Given: 一张含两行明细、指定员工的订单 ---
	order := env.postOrder(t, env.adminToken, orderCreateJSON("req-detail-1", customer.ID, &employee.ID, "cash",
		orderItemJSON(hair.ID, 1, 5000), orderItemJSON(perm.ID, 2, 8000)))

	// --- When: staff 读取详情 ---
	w := env.getOrderAPI(env.staffToken, fmt.Sprintf("%d", order.ID))

	// --- Then: 200 + 订单头 + 两行明细快照 ---
	if w.Code != http.StatusOK {
		t.Fatalf("GET /orders/%d status = %d, want 200 (body=%s)", order.ID, w.Code, w.Body.String())
	}
	var detail orderAPIView
	decodeData(t, decodeEnvelope(t, w), &detail)
	if detail.ID != order.ID || detail.CustomerName != "详情客户" || detail.EmployeeName != "详情员工" {
		t.Errorf("详情头 = %+v, want id=%d/详情客户/详情员工", detail, order.ID)
	}
	if detail.OriginalAmountCents != 21000 || detail.PaidAmountCents != 21000 {
		t.Errorf("详情金额 = 原价 %d/实付 %d, want 21000/21000", detail.OriginalAmountCents, detail.PaidAmountCents)
	}
	if len(detail.Items) != 2 {
		t.Fatalf("详情明细条数 = %d, want 2", len(detail.Items))
	}
	if detail.Items[0].ServiceNameSnapshot != "剪发" || detail.Items[0].UnitPriceCents != 5000 || detail.Items[0].AmountCents != 5000 {
		t.Errorf("明细[0] = %+v, want 剪发/5000/5000", detail.Items[0])
	}
	if detail.Items[1].ServiceNameSnapshot != "烫发" || detail.Items[1].Quantity != 2 || detail.Items[1].AmountCents != 16000 {
		t.Errorf("明细[1] = %+v, want 烫发/2/16000", detail.Items[1])
	}
	for _, item := range detail.Items {
		if item.EmployeeID == nil || *item.EmployeeID != employee.ID {
			t.Errorf("明细 employee_id = %v, want %d", item.EmployeeID, employee.ID)
		}
	}

	// --- When: 服务改名改价后重读详情 ---
	env.authed(http.MethodPut, fmt.Sprintf("/api/v1/services/%d", hair.ID),
		fmt.Sprintf(`{"category_id":%d,"name":"剪发(新)","price_cents":6000}`, hair.CategoryID), env.adminToken)
	w = env.getOrderAPI(env.staffToken, fmt.Sprintf("%d", order.ID))

	// --- Then: 历史订单仍显示下单时快照（06-BUSINESS-RULES.md:17） ---
	if w.Code != http.StatusOK {
		t.Fatalf("改名后 GET /orders/%d status = %d, want 200", order.ID, w.Code)
	}
	var afterRename orderAPIView
	decodeData(t, decodeEnvelope(t, w), &afterRename)
	if afterRename.Items[0].ServiceNameSnapshot != "剪发" || afterRename.Items[0].UnitPriceCents != 5000 {
		t.Errorf("改名改价后明细快照 = %+v, want 剪发/5000（历史不受影响）", afterRename.Items[0])
	}

	// --- When: 客户被软删除后重读详情 ---
	env.authed(http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d", customer.ID), "", env.adminToken)
	w = env.getOrderAPI(env.staffToken, fmt.Sprintf("%d", order.ID))

	// --- Then: 历史订单仍可查，客户名保留（LEFT JOIN 不受软删除影响，账务数据不可丢） ---
	if w.Code != http.StatusOK {
		t.Fatalf("客户软删除后 GET /orders/%d status = %d, want 200 (body=%s)", order.ID, w.Code, w.Body.String())
	}
	var afterCustomerDelete orderAPIView
	decodeData(t, decodeEnvelope(t, w), &afterCustomerDelete)
	if afterCustomerDelete.CustomerName != "详情客户" {
		t.Errorf("客户软删除后 customer_name = %q, want 详情客户", afterCustomerDelete.CustomerName)
	}
	if len(afterCustomerDelete.Items) != 2 {
		t.Errorf("客户软删除后明细条数 = %d, want 2", len(afterCustomerDelete.Items))
	}

	// --- When/Then: 不存在订单 → 404 + 业务码 40400 ---
	w = env.getOrderAPI(env.staffToken, "999999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET 不存在订单 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeNotFound {
		t.Errorf("不存在订单 code = %d, want %d", envl.Code, service.CodeNotFound)
	}

	// --- When/Then: 非法 id → 400 ---
	if w = env.getOrderAPI(env.staffToken, "abc"); w.Code != http.StatusBadRequest {
		t.Errorf("GET 非法订单 id status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// --- When/Then: 未登录 → 401 ---
	if w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/orders/%d", order.ID), "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /orders/:id status = %d, want 401", w.Code)
	}
}

func TestOrdersIdempotent(t *testing.T) {
	env := newCustomerEnv(t)
	customer := env.createCustomer(t, env.staffToken, `{"name":"幂等接口客户","phone":"13800005004"}`)
	hair := env.seedServiceViaAPI(t, "剪发", 5000)
	if err := env.db.Model(&model.Customer{}).Where("id = ?", customer.ID).Update("balance_cents", 10000).Error; err != nil {
		t.Fatalf("注入余额失败: %v", err)
	}

	// --- Given: 首次余额支付 5000 ---
	body := orderCreateJSON("req-ctrl-idem-1", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 5000))
	first := env.postOrder(t, env.staffToken, body)
	counts := env.orderRowCounts(t)
	if counts["orders"] != 1 || counts["balance_transactions"] != 1 || counts["points_transactions"] != 1 {
		t.Fatalf("首次创建后行数 = %v, want orders/balance/points = 1/1/1", counts)
	}

	// --- When: 完全相同的请求体再次提交（同一 request_id） ---
	w := env.authed(http.MethodPost, "/api/v1/orders", body, env.staffToken)

	// --- Then: 200（不是 201）+ 原订单（同 id/单号/明细） ---
	if w.Code != http.StatusOK {
		t.Fatalf("重复 request_id status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var second orderAPIView
	decodeData(t, decodeEnvelope(t, w), &second)
	if second.ID != first.ID || second.OrderNo != first.OrderNo {
		t.Errorf("重复 request_id 返回 = id %d/no %s, want id %d/no %s", second.ID, second.OrderNo, first.ID, first.OrderNo)
	}
	if len(second.Items) != 1 || second.Items[0].ServiceID != hair.ID {
		t.Errorf("重复 request_id 明细 = %+v, want 原明细（剪发）", second.Items)
	}
	if second.CustomerName != "幂等接口客户" {
		t.Errorf("重复 request_id customer_name = %q, want 幂等接口客户", second.CustomerName)
	}

	// --- Then: 表行数不变、余额只扣一次 ---
	afterCounts := env.orderRowCounts(t)
	for name, got := range afterCounts {
		if got != counts[name] {
			t.Errorf("重复 request_id 后 %s 行数 = %d, want %d（不得重复入账）", name, got, counts[name])
		}
	}
	var after model.Customer
	if err := env.db.First(&after, customer.ID).Error; err != nil {
		t.Fatalf("读取客户失败: %v", err)
	}
	if after.BalanceCents != 5000 || after.TotalSpentCents != 5000 || after.Points != 50 {
		t.Errorf("重复 request_id 后余额/累计消费/积分 = %d/%d/%d, want 5000/5000/50",
			after.BalanceCents, after.TotalSpentCents, after.Points)
	}

	// --- When: 换一套完全不同的入参、但复用同一 request_id ---
	changed := orderCreateJSON("req-ctrl-idem-1", customer.ID, nil, "cash", orderItemJSON(hair.ID, 3, 5000))
	w = env.authed(http.MethodPost, "/api/v1/orders", changed, env.staffToken)

	// --- Then: 仍返回原订单（幂等键优先于入参），且不新增任何行 ---
	if w.Code != http.StatusOK {
		t.Fatalf("换入参重复 request_id status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var third orderAPIView
	decodeData(t, decodeEnvelope(t, w), &third)
	if third.ID != first.ID || third.OrderNo != first.OrderNo {
		t.Errorf("换入参重复 request_id 返回 = id %d/no %s, want 原订单 id %d/no %s",
			third.ID, third.OrderNo, first.ID, first.OrderNo)
	}
	finalCounts := env.orderRowCounts(t)
	for name, got := range finalCounts {
		if got != counts[name] {
			t.Errorf("换入参重复后 %s 行数 = %d, want %d", name, got, counts[name])
		}
	}
}

// orderRowCounts 统计订单事务涉及的全部表行数（幂等「零新增」断言）。
func (e *customerEnv) orderRowCounts(t *testing.T) map[string]int64 {
	t.Helper()
	entities := map[string]any{
		"orders":               &model.Order{},
		"order_items":          &model.OrderItem{},
		"balance_transactions": &model.BalanceTransaction{},
		"points_transactions":  &model.PointsTransaction{},
		"operation_logs":       &model.OperationLog{},
	}
	counts := make(map[string]int64, len(entities))
	for name, entity := range entities {
		var count int64
		if err := e.db.Model(entity).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", name, err)
		}
		counts[name] = count
	}
	return counts
}

// orderIDs 提取订单 id 序列（断言消息用）。
func orderIDs(items []orderAPIView) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
