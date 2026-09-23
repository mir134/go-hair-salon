package model_test

// TestMigrate / TestSchemaColumns 是 todo 3 的验收测试（计划：
// `go test ./internal/model -run TestMigrate`）：
//   - AutoMigrate 幂等（二次执行不报错）；
//   - PRAGMA journal_mode=wal / busy_timeout=5000 / foreign_keys=1；
//   - 14 张表与 03-DATABASE.md:281-292 全部索引（含部分唯一索引）真实存在；
//   - settings 默认种子 points_per_yuan / shop_name；
//   - orders.request_id 唯一约束（重复插入报唯一冲突）；
//   - customers 非空 phone 部分唯一索引语义（软删除后可复用）；
//   - datetime 列以 UTC 落库。

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

func TestMigrate(t *testing.T) {
	// Given: 一个全新的临时 SQLite 数据库。
	dbPath := filepath.Join(t.TempDir(), "migrate-test.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	defer sqlDB.Close()

	// DSN 必须固化 WAL/busy_timeout/foreign_keys 三个 pragma（glebarez/sqlite DSN）。
	dsn := repository.DSN("x.db")
	for _, pragma := range []string{
		"_pragma=busy_timeout(5000)", "_pragma=journal_mode(WAL)", "_pragma=foreign_keys(1)",
	} {
		if !strings.Contains(dsn, pragma) {
			t.Errorf("repository.DSN(%q) = %q, 缺少 %s", "x.db", dsn, pragma)
		}
	}

	// When: 连续执行两次 AutoMigrate（验收：迁移幂等，二次启动不报错）。
	for round := 1; round <= 2; round++ {
		if err := repository.Migrate(db); err != nil {
			t.Fatalf("repository.Migrate round %d: %v", round, err)
		}
	}
	// 播种 settings 默认值，且二次调用幂等（不得新增重复行）。
	for round := 1; round <= 2; round++ {
		if err := repository.EnsureDefaultSettings(db); err != nil {
			t.Fatalf("repository.EnsureDefaultSettings round %d: %v", round, err)
		}
	}

	// Then 1: sql.DB 单例，最大连接数 1（02-AGENTS.md:45-63 禁止每请求创建连接）。
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}

	// Then 2: PRAGMA 值（02-AGENTS.md:45-52）。
	for pragma, want := range map[string]string{
		"journal_mode": "wal",
		"busy_timeout": "5000",
		"foreign_keys": "1",
	} {
		if got := pragmaValue(t, sqlDB, pragma); got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}

	// Then 3: 14 张表全部存在。
	for _, table := range expectedTables {
		if !tableExists(t, sqlDB, table) {
			t.Errorf("table %s 不存在", table)
		}
	}

	// Then 4: 03-DATABASE.md:281-292 索引 + 唯一索引全部存在。
	found := indexSQL(t, sqlDB)
	for _, idx := range expectedIndexes {
		sqlText, ok := found[idx.name]
		if !ok {
			t.Errorf("index %s（表 %s）不存在", idx.name, idx.table)
			continue
		}
		if idx.partial && !strings.Contains(strings.ToLower(sqlText), "deleted_at is null") {
			t.Errorf("index %s 缺少部分唯一条件 deleted_at IS NULL: %s", idx.name, sqlText)
		}
	}

	// Then 5: settings 种子（幂等：各 1 行；points_per_yuan=1；shop_name 非空）。
	if got := settingCount(t, sqlDB, model.SettingPointsPerYuan); got != 1 {
		t.Errorf("settings[key=%s] 行数 = %d, want 1", model.SettingPointsPerYuan, got)
	}
	if got := settingValue(t, sqlDB, model.SettingPointsPerYuan); got != "1" {
		t.Errorf("settings[key=%s] = %q, want \"1\"", model.SettingPointsPerYuan, got)
	}
	if got := settingCount(t, sqlDB, model.SettingShopName); got != 1 {
		t.Errorf("settings[key=%s] 行数 = %d, want 1", model.SettingShopName, got)
	}
	if got := settingValue(t, sqlDB, model.SettingShopName); strings.TrimSpace(got) == "" {
		t.Error("shop_name 种子为空，want 非空默认店名")
	}

	// Then 6: 重复 request_id 命中唯一约束。
	first := model.Order{OrderNo: "T20260101000001", RequestID: "req-dup-1", CustomerID: 1, Status: model.OrderStatusPending}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first order: %v", err)
	}
	dup := model.Order{OrderNo: "T20260101000002", RequestID: "req-dup-1", CustomerID: 1, Status: model.OrderStatusPending}
	if err := db.Create(&dup).Error; !isUniqueViolation(err) {
		t.Fatalf("重复 request_id 插入 err = %v, want 唯一约束冲突", err)
	}

	// Then 6b: 重复 order_no 同样命中唯一约束。
	dupNo := model.Order{OrderNo: "T20260101000001", RequestID: "req-dup-2", CustomerID: 1, Status: model.OrderStatusPending}
	if err := db.Create(&dupNo).Error; !isUniqueViolation(err) {
		t.Fatalf("重复 order_no 插入 err = %v, want 唯一约束冲突", err)
	}

	// Then 7: customers 非空 phone 部分唯一索引语义。
	mustCreate(t, db, &model.Customer{Name: "无手机号 A", Phone: ""})
	mustCreate(t, db, &model.Customer{Name: "无手机号 B", Phone: ""}) // 空手机号可重复
	mustCreate(t, db, &model.Customer{Name: "张三", Phone: "13800000001"})
	phoneDup := db.Create(&model.Customer{Name: "李四", Phone: "13800000001"}).Error
	if !isUniqueViolation(phoneDup) {
		t.Errorf("重复非空 phone 插入 err = %v, want 唯一约束冲突", phoneDup)
	}
	// 软删除后手机号可复用（部分索引条件 WHERE deleted_at IS NULL）。
	if err := db.Delete(&model.Customer{}, "phone = ?", "13800000001").Error; err != nil {
		t.Fatalf("soft delete customer: %v", err)
	}
	mustCreate(t, db, &model.Customer{Name: "王五", Phone: "13800000001"})

	// Then 8: datetime 列以 UTC 落库（02-AGENTS.md:39）。
	rawCreated, err := settingRawCreatedAt(t, sqlDB, model.SettingPointsPerYuan)
	if err != nil {
		t.Fatalf("read raw created_at: %v", err)
	}
	if !strings.HasSuffix(rawCreated, "+00:00") && !strings.HasSuffix(rawCreated, "Z") {
		t.Errorf("settings.created_at 原始值 %q 不是 UTC（want 以 +00:00 或 Z 结尾）", rawCreated)
	}
}

// TestSchemaColumns 锁定每张表的列集合与金额/积分列类型（03-DATABASE.md 字段清单）。
func TestSchemaColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "columns-test.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	defer sqlDB.Close()
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}

	for table, wantColumns := range expectedColumns {
		rows, err := sqlDB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", table, err)
		}
		var got []string
		types := map[string]string{}
		for rows.Next() {
			var (
				cid       int
				name, typ string
				notNull   int
				dflt      sql.NullString
				pk        int
			)
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				t.Fatalf("scan table_info(%s): %v", table, err)
			}
			got = append(got, name)
			types[name] = strings.ToLower(typ)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("table_info(%s) rows: %v", table, err)
		}
		rows.Close()
		if strings.Join(got, ",") != strings.Join(wantColumns, ",") {
			t.Errorf("%s 列集合 = %v, want %v", table, got, wantColumns)
		}
		// 金额/积分类列必须是整数（禁止 float64 表示金额）。
		for _, col := range got {
			if strings.HasSuffix(col, "_cents") || col == "points" {
				if !strings.Contains(types[col], "integer") {
					t.Errorf("%s.%s 类型 = %q, want integer", table, col, types[col])
				}
			}
		}
	}
}

// --- helpers ---

var expectedTables = []string{
	"users", "employees", "customers", "tags", "customer_tag_relations",
	"service_categories", "services", "orders", "order_items",
	"recharge_records", "balance_transactions", "points_transactions",
	"settings", "operation_logs",
}

type indexExpectation struct {
	name    string
	table   string
	partial bool
}

var expectedIndexes = []indexExpectation{
	{"idx_customers_phone", "customers", false},
	{"idx_customers_name", "customers", false},
	{"idx_customers_last_visit_at", "customers", false},
	{"ux_customers_phone_active", "customers", true},
	{"ux_users_username", "users", false},
	{"ux_settings_key", "settings", false},
	{"ux_customer_tag_relations", "customer_tag_relations", false},
	{"ux_orders_request_id", "orders", false},
	{"ux_orders_order_no", "orders", false},
	{"idx_orders_customer_created", "orders", false},
	{"idx_orders_employee_created", "orders", false},
	{"idx_orders_created_at", "orders", false},
	{"ux_recharge_records_request_id", "recharge_records", false},
	{"idx_balance_transactions_customer_created", "balance_transactions", false},
	{"idx_points_transactions_customer_created", "points_transactions", false},
	{"idx_operation_logs_operator_created", "operation_logs", false},
}

var expectedColumns = map[string][]string{
	"users":                  {"id", "username", "password_hash", "role", "employee_id", "status", "created_at", "updated_at"},
	"employees":              {"id", "name", "phone", "avatar", "position", "status", "joined_at", "remark"},
	"customers":              {"id", "name", "phone", "gender", "birthday", "avatar", "wechat", "source", "first_visit_at", "last_visit_at", "total_spent_cents", "balance_cents", "points", "remark", "created_at", "updated_at", "deleted_at"},
	"tags":                   {"id", "name", "color", "created_at", "updated_at", "deleted_at"},
	"customer_tag_relations": {"customer_id", "tag_id"},
	"service_categories":     {"id", "name", "sort", "status", "created_at", "updated_at"},
	"services":               {"id", "category_id", "name", "price_cents", "duration_minutes", "status", "remark", "created_at", "updated_at", "deleted_at"},
	"orders":                 {"id", "order_no", "request_id", "customer_id", "employee_id", "original_amount_cents", "discount_amount_cents", "paid_amount_cents", "payment_method", "status", "remark", "created_at", "updated_at"},
	// order_items.deleted_at 是 todo 26 的已决议新增列（超出 03-DATABASE.md:147-162 字段清单）：
	// 04-API.md:138 要求 DELETE /orders/:id/items/:item_id，而 AGENTS.md 第 5 节红线
	// 禁止 order_items 物理删除 —— 两文档冲突的唯一兼容实现是软删除（挂单明细只置 deleted_at，行保留）。
	"order_items":          {"id", "order_id", "service_id", "service_name_snapshot", "quantity", "unit_price_cents", "discount_amount_cents", "amount_cents", "employee_id", "created_at", "deleted_at"},
	"recharge_records":     {"id", "customer_id", "request_id", "recharge_amount_cents", "gift_amount_cents", "actual_amount_cents", "payment_method", "status", "operator_id", "remark", "created_at"},
	"balance_transactions": {"id", "customer_id", "type", "amount_cents", "balance_before_cents", "balance_after_cents", "reference_type", "reference_id", "operator_id", "remark", "created_at"},
	"points_transactions":  {"id", "customer_id", "type", "points", "balance_before", "balance_after", "reference_type", "reference_id", "operator_id", "remark", "created_at"},
	"settings":             {"id", "key", "value", "description", "updated_by", "created_at", "updated_at"},
	"operation_logs":       {"id", "operator_id", "action", "target_type", "target_id", "content", "ip", "user_agent", "created_at"},
}

func pragmaValue(t *testing.T, sqlDB *sql.DB, name string) string {
	t.Helper()
	var raw any
	if err := sqlDB.QueryRow("PRAGMA " + name).Scan(&raw); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return strings.ToLower(fmt.Sprintf("%v", raw))
}

func tableExists(t *testing.T, sqlDB *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := sqlDB.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	return true
}

func indexSQL(t *testing.T, sqlDB *sql.DB) map[string]string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT name, IFNULL(sql, '') FROM sqlite_master WHERE type = 'index'`)
	if err != nil {
		t.Fatalf("query sqlite_master indexes: %v", err)
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var name, sqlText string
		if err := rows.Scan(&name, &sqlText); err != nil {
			t.Fatalf("scan index row: %v", err)
		}
		found[name] = sqlText
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index rows: %v", err)
	}
	return found
}

func settingCount(t *testing.T, sqlDB *sql.DB, key string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, key).Scan(&count); err != nil {
		t.Fatalf("count settings[%s]: %v", key, err)
	}
	return count
}

func settingValue(t *testing.T, sqlDB *sql.DB, key string) string {
	t.Helper()
	var value string
	if err := sqlDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil {
		t.Fatalf("read settings[%s]: %v", key, err)
	}
	return value
}

func settingRawCreatedAt(t *testing.T, sqlDB *sql.DB, key string) (string, error) {
	t.Helper()
	var raw string
	err := sqlDB.QueryRow(`SELECT created_at FROM settings WHERE key = ?`, key).Scan(&raw)
	return raw, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed: unique")
}

func mustCreate(t *testing.T, db *gorm.DB, value any) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatalf("create %T: %v", value, err)
	}
}
