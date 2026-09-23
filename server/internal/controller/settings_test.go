package controller_test

// TestSettings 是 todo 41 的验收测试（计划：`go test ./internal/controller -run TestSettings -v -count=1`）：
//   - GET /settings（both）：仅公开键 shop_name/points_per_yuan，值为 DB 现值（不写死默认）；
//   - PUT /settings/:key（仅 admin）：staff → 403/40300 且 DB 不变；
//   - malformed_input：比例 0/负/非数字/小数/空白/溢出、未知键、店名空白/超长、非法 JSON → 400/404 且不落值、不新增行；
//   - updated_by = 当前管理员；更新写 operation_logs（action=setting_update，target=setting#id）；
//   - 非追溯（06-BUSINESS-RULES.md:71-76）：比例 1 时旧单 earn=100；改为 2 后新单 earn=200；旧单积分流水保持原快照。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// settingData 是 settings DTO 的测试镜像：value 为字符串（03-DATABASE.md:248-253）。
type settingData struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	UpdatedBy   *int64 `json:"updated_by"`
}

// listSettings 通过 API 拉取设置数组并断言 200。
func (e *customerEnv) listSettings(t *testing.T, token string) []settingData {
	t.Helper()
	w := e.authed(http.MethodGet, "/api/v1/settings", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /settings status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var items []settingData
	decodeData(t, decodeEnvelope(t, w), &items)
	return items
}

// updateSetting 通过 API 修改设置（PUT /settings/:key），返回原始响应。
func (e *customerEnv) updateSetting(token, key, value string) *httptest.ResponseRecorder {
	body, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		panic(fmt.Sprintf("构造设置请求体失败: %v", err))
	}
	return e.authed(http.MethodPut, "/api/v1/settings/"+key, string(body), token)
}

// findSetting 在设置数组中按键查找。
func findSetting(items []settingData, key string) (settingData, bool) {
	for _, item := range items {
		if item.Key == key {
			return item, true
		}
	}
	return settingData{}, false
}

// settingValueInDB 读取 settings 现值（DB 真值，不信任 API 回显）。
func settingValueInDB(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var setting model.Setting
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		t.Fatalf("读取 settings[%s] 失败: %v", key, err)
	}
	return setting.Value
}

// countSettingsRows 统计 settings 行数（未知键不得新增行）。
func countSettingsRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&model.Setting{}).Count(&count).Error; err != nil {
		t.Fatalf("统计 settings 失败: %v", err)
	}
	return count
}

// customerPoints 读取客户积分缓存（DB 真值）。
func customerPoints(t *testing.T, db *gorm.DB, customerID int64) int64 {
	t.Helper()
	var customer model.Customer
	if err := db.First(&customer, customerID).Error; err != nil {
		t.Fatalf("读取客户 %d 失败: %v", customerID, err)
	}
	return customer.Points
}

// orderEarnPoints 汇总指定订单的 earn 积分流水（积分快照，不重算比例）。
func orderEarnPoints(t *testing.T, db *gorm.DB, orderID int64) int64 {
	t.Helper()
	var rows []model.PointsTransaction
	if err := db.Where("reference_type = ? AND reference_id = ? AND type = ?",
		model.ReferenceTypeOrder, orderID, model.PointsTxEarn).Find(&rows).Error; err != nil {
		t.Fatalf("读取订单 %d 积分流水失败: %v", orderID, err)
	}
	var total int64
	for _, row := range rows {
		total += row.Points
	}
	return total
}

func TestSettings(t *testing.T) {
	env := newCustomerEnv(t)
	// 播种 settings 默认值（等价 main.go 启动时的 EnsureDefaultSettings）：
	// authTestEnv 只跑 AutoMigrate，不播种业务默认值。
	if err := repository.EnsureDefaultSettings(env.db); err != nil {
		t.Fatalf("EnsureDefaultSettings: %v", err)
	}

	// --- Given/When: staff 读取设置 ---
	defaults := env.listSettings(t, env.staffToken)

	// --- Then: 恰好两个公开键；值为 DB 播种现值（与 DB 逐条对照，不写死默认） ---
	if len(defaults) != 2 {
		t.Fatalf("GET /settings 返回 %d 项, want 2（仅公开键）", len(defaults))
	}
	ratio, ok := findSetting(defaults, model.SettingPointsPerYuan)
	if !ok || ratio.Value != settingValueInDB(t, env.db, model.SettingPointsPerYuan) {
		t.Errorf("points_per_yuan = %+v, want 与 DB 现值一致 %q", ratio, settingValueInDB(t, env.db, model.SettingPointsPerYuan))
	}
	shop, ok := findSetting(defaults, model.SettingShopName)
	if !ok || shop.Value != settingValueInDB(t, env.db, model.SettingShopName) {
		t.Errorf("shop_name = %+v, want 与 DB 现值一致 %q", shop, settingValueInDB(t, env.db, model.SettingShopName))
	}

	// --- Given: 客户 + 10000 分（100 元）服务；比例=1 时创建旧单 ---
	customer := env.createCustomer(t, env.staffToken, `{"name":"设置客户"}`)
	svc := env.seedServiceViaAPI(t, "剪发", 10000)
	oldOrder := env.postOrder(t, env.adminToken,
		orderCreateJSON("req-settings-old", customer.ID, nil, model.PaymentMethodCash, orderItemJSON(svc.ID, 1, 10000)))

	// --- Then: 旧单 earn = floor(10000×1/100) = 100 ---
	if got := customerPoints(t, env.db, customer.ID); got != 100 {
		t.Fatalf("比例=1 旧单后客户积分 = %d, want 100", got)
	}

	// --- When: staff 修改比例 → 403/40300 且 DB 不变（后端是权限最终边界） ---
	w := env.updateSetting(env.staffToken, model.SettingPointsPerYuan, "2")
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff PUT /settings/points_per_yuan status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if envl := decodeEnvelope(t, w); envl.Code != service.CodeForbidden {
		t.Errorf("staff PUT 设置 code = %d, want %d", envl.Code, service.CodeForbidden)
	}
	if got := settingValueInDB(t, env.db, model.SettingPointsPerYuan); got != "1" {
		t.Errorf("staff PUT 后 DB 比例 = %q, want 1（不得被改动）", got)
	}

	// --- When: admin 把比例改为 2 ---
	w = env.updateSetting(env.adminToken, model.SettingPointsPerYuan, "2")

	// --- Then: 200；回显新值；updated_by=当前 admin；DB 落库 ---
	if w.Code != http.StatusOK {
		t.Fatalf("admin PUT /settings/points_per_yuan status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var updated settingData
	decodeData(t, decodeEnvelope(t, w), &updated)
	if updated.Key != model.SettingPointsPerYuan || updated.Value != "2" {
		t.Errorf("PUT 回显 = %+v, want points_per_yuan=2", updated)
	}
	if updated.UpdatedBy == nil || *updated.UpdatedBy != env.admin.ID {
		t.Errorf("PUT 回显 updated_by = %v, want %d", updated.UpdatedBy, env.admin.ID)
	}
	if got := settingValueInDB(t, env.db, model.SettingPointsPerYuan); got != "2" {
		t.Errorf("admin PUT 后 DB 比例 = %q, want 2", got)
	}
	var stored model.Setting
	if err := env.db.Where("key = ?", model.SettingPointsPerYuan).First(&stored).Error; err != nil {
		t.Fatalf("读取 settings 失败: %v", err)
	}
	if stored.UpdatedBy == nil || *stored.UpdatedBy != env.admin.ID {
		t.Errorf("settings.updated_by = %v, want %d", stored.UpdatedBy, env.admin.ID)
	}

	// --- Then: operation_logs 记录 setting_update（operator/target 齐全） ---
	var logRow model.OperationLog
	if err := env.db.Where("action = ?", "setting_update").First(&logRow).Error; err != nil {
		t.Fatalf("读取 setting_update 审计日志失败: %v", err)
	}
	if logRow.TargetType != "setting" || logRow.TargetID != updated.ID {
		t.Errorf("审计 target = %s#%d, want setting#%d", logRow.TargetType, logRow.TargetID, updated.ID)
	}
	if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
		t.Errorf("审计 operator_id = %v, want %d", logRow.OperatorID, env.admin.ID)
	}
	if !strings.Contains(logRow.Content, model.SettingPointsPerYuan) {
		t.Errorf("审计 content = %q, want 含键名 %s", logRow.Content, model.SettingPointsPerYuan)
	}

	// --- When: 新比例下再消费 10000 分（新单） ---
	newOrder := env.postOrder(t, env.adminToken,
		orderCreateJSON("req-settings-new", customer.ID, nil, model.PaymentMethodCash, orderItemJSON(svc.ID, 1, 10000)))

	// --- Then: 新单 earn=200；客户积分 300；旧单流水保持原快照 100（不追溯） ---
	if got := customerPoints(t, env.db, customer.ID); got != 300 {
		t.Errorf("比例=2 新单后客户积分 = %d, want 300（100 + 200）", got)
	}
	if got := orderEarnPoints(t, env.db, oldOrder.ID); got != 100 {
		t.Errorf("旧单 earn 流水 = %d, want 100（比例变更不追溯）", got)
	}
	if got := orderEarnPoints(t, env.db, newOrder.ID); got != 200 {
		t.Errorf("新单 earn 流水 = %d, want 200（新比例生效）", got)
	}

	// --- Then: staff 也能读到新值（both 可读同一来源） ---
	after := env.listSettings(t, env.staffToken)
	if item, found := findSetting(after, model.SettingPointsPerYuan); !found || item.Value != "2" {
		t.Errorf("staff GET /settings 比例 = %+v, want 2", item)
	}

	// --- When/Then: malformed_input 全部 400/404，且不落值、不新增行 ---
	rowsBefore := countSettingsRows(t, env.db)
	cases := []struct {
		name       string
		key        string
		value      string
		wantStatus int
	}{
		{"比例 0", model.SettingPointsPerYuan, "0", http.StatusBadRequest},
		{"比例负数", model.SettingPointsPerYuan, "-1", http.StatusBadRequest},
		{"比例非数字", model.SettingPointsPerYuan, "abc", http.StatusBadRequest},
		{"比例小数", model.SettingPointsPerYuan, "2.5", http.StatusBadRequest},
		{"比例空白", model.SettingPointsPerYuan, "   ", http.StatusBadRequest},
		{"比例溢出", model.SettingPointsPerYuan, "99999999999999999999", http.StatusBadRequest},
		{"未知键", "unknown_key", "1", http.StatusNotFound},
		{"店名空白", model.SettingShopName, "   ", http.StatusBadRequest},
		{"店名超长", model.SettingShopName, strings.Repeat("店", 256), http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w = env.updateSetting(env.adminToken, tc.key, tc.value); w.Code != tc.wantStatus {
			t.Errorf("PUT settings/%s value=%q status = %d, want %d (body=%s)",
				tc.key, tc.value, w.Code, tc.wantStatus, w.Body.String())
		}
	}
	if w = env.authed(http.MethodPut, "/api/v1/settings/"+model.SettingPointsPerYuan, `{"value":}`, env.adminToken); w.Code != http.StatusBadRequest {
		t.Errorf("非法 JSON status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if got := countSettingsRows(t, env.db); got != rowsBefore {
		t.Errorf("非法输入后 settings 行数 = %d, want %d（未知键不得新增）", got, rowsBefore)
	}
	if got := settingValueInDB(t, env.db, model.SettingPointsPerYuan); got != "2" {
		t.Errorf("非法输入后比例 = %q, want 2（不得被改动）", got)
	}

	// --- When: admin 修改店名（首尾空白应规整） ---
	w = env.updateSetting(env.adminToken, model.SettingShopName, "  新店名  ")

	// --- Then: 200 + 回显规整值；GET 反映最新值（顶部读取同一接口） ---
	if w.Code != http.StatusOK {
		t.Fatalf("admin PUT /settings/shop_name status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var shopUpdated settingData
	decodeData(t, decodeEnvelope(t, w), &shopUpdated)
	if shopUpdated.Value != "新店名" {
		t.Errorf("店名回显 = %q, want 新店名（去首尾空白）", shopUpdated.Value)
	}
	latest := env.listSettings(t, env.staffToken)
	if item, found := findSetting(latest, model.SettingShopName); !found || item.Value != "新店名" {
		t.Errorf("GET /settings 店名 = %+v, want 新店名", item)
	}
}
