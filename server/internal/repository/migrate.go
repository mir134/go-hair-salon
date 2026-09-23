package repository

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// indexStatements 是 03-DATABASE.md:281-292 要求的全部索引，另含：
//   - orders(order_no) 唯一索引（已决议新增，超出 03 清单）；
//   - customers(phone) 部分唯一索引（非空手机号且未软删除才唯一，软删除后可复用）；
//   - 表文档中标注“唯一”的 users.username / settings.key / customer_tag_relations。
//
// 索引统一用原始 SQL 创建（IF NOT EXISTS），保证与文档逐条对应且可重入。
var indexStatements = []string{
	"CREATE INDEX IF NOT EXISTS idx_customers_phone ON customers(phone)",
	"CREATE INDEX IF NOT EXISTS idx_customers_name ON customers(name)",
	"CREATE INDEX IF NOT EXISTS idx_customers_last_visit_at ON customers(last_visit_at)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_customers_phone_active ON customers(phone) WHERE phone <> '' AND deleted_at IS NULL",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_users_username ON users(username)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_settings_key ON settings(key)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_customer_tag_relations ON customer_tag_relations(customer_id, tag_id)",
	"CREATE INDEX IF NOT EXISTS idx_orders_customer_created ON orders(customer_id, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_orders_employee_created ON orders(employee_id, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_orders_request_id ON orders(request_id)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_orders_order_no ON orders(order_no)",
	"CREATE UNIQUE INDEX IF NOT EXISTS ux_recharge_records_request_id ON recharge_records(request_id)",
	"CREATE INDEX IF NOT EXISTS idx_balance_transactions_customer_created ON balance_transactions(customer_id, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_points_transactions_customer_created ON points_transactions(customer_id, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_operation_logs_operator_created ON operation_logs(operator_id, created_at)",
}

// Migrate 执行数据库迁移：按外键依赖顺序 AutoMigrate 全部模型，再创建全部索引。
// 必须幂等：重复执行（每次启动）不报错、不丢数据。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("AutoMigrate 失败: %w", err)
	}
	for _, stmt := range indexStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("创建索引失败（%s）: %w", stmt, err)
		}
	}
	return nil
}
