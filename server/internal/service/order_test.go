package service_test

// TestOrderComplete 是 todo 21 的验收测试（计划：`go test ./internal/service -run 'TestOrderComplete' -v -count=1`）：
//   - 余额支付：订单/明细/原子扣余额/余额流水/积分流水/累计消费/最近到店 在同一事务内全部正确（03-DATABASE.md:319-331、06-BUSINESS-RULES.md:20-38）；
//   - 现金支付：余额与余额流水完全不动（仅积分与累计消费变化）；
//   - 余额不足：422 + 全表零写入（任一步失败整体回滚，AGENTS.md 第 5 节）；
//   - 积分 = floor(paid_amount_cents × points_per_yuan ÷ 100)，比例读 settings（06 §6，修 03 漏写）；
//   - request_id 幂等：重复提交返回原订单、不重复入账（04-API.md:277-299）；
//   - malformed_input：数量 ≤0 / 未知客户 / 未知或停用服务 / 非法支付方式 / 非法 request_id → 400/404/422 且零写入；
//   - 改价仅 admin 且必填原因，订单级折扣 = 明细级合计，原因写入 operation_logs（06 §3.1）。

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// orderNoPattern 是订单号格式：yyyyMMddHHmmss + 4 位数字。
var orderNoPattern = regexp.MustCompile(`^\d{18}$`)

// orderTestEnv 是消费事务验收测试环境：真实迁移库 + 真实仓储/服务装配。
type orderTestEnv struct {
	db        *gorm.DB
	dbPath    string
	orders    *service.OrderService
	customers *repository.CustomerRepository
}

// newOrderTestEnv 装配 todo 21 测试环境（临时库、迁移、settings 播种、完整依赖注入）。
func newOrderTestEnv(t *testing.T) *orderTestEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "order.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	if _, err := db.DB(); err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	// 关闭「退出时当前的」连接池：审计测试（todo 52）会经恢复流程原地替换连接池。
	t.Cleanup(func() {
		if current, err := db.DB(); err == nil {
			_ = current.Close()
		}
	})
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	if err := repository.EnsureDefaultSettings(db); err != nil {
		t.Fatalf("EnsureDefaultSettings: %v", err)
	}

	customerRepo := repository.NewCustomerRepository(db)
	itemSvc := service.NewServiceItemService(
		repository.NewServiceItemRepository(db),
		repository.NewServiceCategoryRepository(db),
	)
	orderSvc := service.NewOrderService(service.OrderServiceDeps{
		Tx:        repository.NewTransactor(db),
		Orders:    repository.NewOrderRepository(db),
		Customers: customerRepo,
		Services:  itemSvc,
		Employees: repository.NewEmployeeRepository(db),
		Ledger:    repository.NewLedgerRepository(db),
		Settings:  repository.NewSettingsRepository(db),
		Logs:      service.NewOperationLogService(repository.NewOperationLogRepository(db)),
	})
	return &orderTestEnv{db: db, dbPath: dbPath, orders: orderSvc, customers: customerRepo}
}

// adminCtx 构造 admin 登录上下文（改价权限 + 审计 operator=1）。
func adminCtx() context.Context {
	return service.WithCurrentUser(
		service.WithOperatorID(context.Background(), 1),
		&model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusEnabled},
	)
}

// staffCtx 构造 staff 登录上下文（只能按标准价成交）。
func staffCtx() context.Context {
	return service.WithCurrentUser(
		service.WithOperatorID(context.Background(), 2),
		&model.User{ID: 2, Username: "staff", Role: model.RoleStaff, Status: model.StatusEnabled},
	)
}

// seedCustomer 直接落库客户（余额为测试前置条件，本测试只验证消费事务）。
func (e *orderTestEnv) seedCustomer(t *testing.T, name string, balanceCents int64) *model.Customer {
	t.Helper()
	customer := &model.Customer{Name: name, BalanceCents: balanceCents}
	if err := e.db.Create(customer).Error; err != nil {
		t.Fatalf("造客户失败: %v", err)
	}
	return customer
}

// seedService 建分类 + 服务；status=0 时插入后显式回写（规避 GORM default:1 覆盖零值）。
func (e *orderTestEnv) seedService(t *testing.T, name string, priceCents int64, status int) *model.Service {
	t.Helper()
	category := &model.ServiceCategory{Name: name + "分类", Sort: 1}
	if err := e.db.Create(category).Error; err != nil {
		t.Fatalf("造分类失败: %v", err)
	}
	item := &model.Service{CategoryID: category.ID, Name: name, PriceCents: priceCents, Status: model.StatusEnabled}
	if err := e.db.Create(item).Error; err != nil {
		t.Fatalf("造服务失败: %v", err)
	}
	if status != model.StatusEnabled {
		if err := e.db.Model(&model.Service{}).Where("id = ?", item.ID).
			UpdateColumn("status", status).Error; err != nil {
			t.Fatalf("更新服务状态失败: %v", err)
		}
		item.Status = status
	}
	return item
}

// seedEmployee 直接落库员工。
func (e *orderTestEnv) seedEmployee(t *testing.T, name string) *model.Employee {
	t.Helper()
	employee := &model.Employee{Name: name, Status: model.StatusEnabled}
	if err := e.db.Create(employee).Error; err != nil {
		t.Fatalf("造员工失败: %v", err)
	}
	return employee
}

// countRows 统计实体行数（断言「失败零写入」）。
func (e *orderTestEnv) countRows(t *testing.T, entity any) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(entity).Count(&count).Error; err != nil {
		t.Fatalf("统计 %T 失败: %v", entity, err)
	}
	return count
}

// customerAfter 重新读取客户（断言事务提交后的余额/积分/累计消费缓存）。
func (e *orderTestEnv) customerAfter(t *testing.T, id int64) *model.Customer {
	t.Helper()
	var customer model.Customer
	if err := e.db.First(&customer, id).Error; err != nil {
		t.Fatalf("读取客户 %d 失败: %v", id, err)
	}
	return &customer
}

// orderInput 构造直接完成订单输入（默认 admin 上下文，明细由调用方给出）。
func orderInput(requestID string, customerID int64, method string, items ...service.OrderItemInput) service.OrderCreateInput {
	return service.OrderCreateInput{
		RequestID:     requestID,
		CustomerID:    customerID,
		PaymentMethod: method,
		Items:         items,
	}
}

// requireBizError 断言 err 是指定 HTTP 状态的业务错误。
func requireBizError(t *testing.T, err error, wantStatus int) *service.BizError {
	t.Helper()
	var biz *service.BizError
	if !errors.As(err, &biz) {
		t.Fatalf("err = %v, want *service.BizError(%d)", err, wantStatus)
	}
	if biz.Status != wantStatus {
		t.Fatalf("BizError status = %d (code=%d msg=%q), want %d", biz.Status, biz.Code, biz.Message, wantStatus)
	}
	return biz
}

func TestOrderComplete(t *testing.T) {
	t.Run("balance payment writes order ledger points and totals", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "余额客户", 10000)
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)
		perm := env.seedService(t, "烫发", 2000, model.StatusEnabled)

		// --- When: 余额支付 9000 分（5000×1 + 2000×2） ---
		result, err := env.orders.Create(adminCtx(), orderInput("req-balance-1", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000},
			service.OrderItemInput{ServiceID: perm.ID, Quantity: 2, UnitPriceCents: 2000},
		))

		// --- Then: 订单为 completed，原价 9000 / 优惠 0 / 实付 9000，订单号 18 位数字 ---
		if err != nil {
			t.Fatalf("Create(balance): %v", err)
		}
		if !result.Created {
			t.Error("首次创建 Created = false, want true")
		}
		order := result.Order
		if order.Status != model.OrderStatusCompleted || order.PaymentMethod != model.PaymentMethodBalance {
			t.Errorf("订单 status/payment = %s/%s, want completed/balance", order.Status, order.PaymentMethod)
		}
		if order.OriginalAmountCents != 9000 || order.DiscountAmountCents != 0 || order.PaidAmountCents != 9000 {
			t.Errorf("订单金额 原价/优惠/实付 = %d/%d/%d, want 9000/0/9000",
				order.OriginalAmountCents, order.DiscountAmountCents, order.PaidAmountCents)
		}
		if !orderNoPattern.MatchString(order.OrderNo) {
			t.Errorf("订单号 = %q, want yyyyMMddHHmmss+4 位数字", order.OrderNo)
		}
		if result.CustomerName != "余额客户" {
			t.Errorf("结果 customer_name = %q, want 余额客户", result.CustomerName)
		}

		// --- Then: 明细保存服务名/成交单价/数量/金额快照 ---
		if len(result.Items) != 2 {
			t.Fatalf("明细条数 = %d, want 2", len(result.Items))
		}
		first := result.Items[0]
		if first.OrderID != order.ID || first.ServiceID != haircut.ID || first.ServiceNameSnapshot != "剪发" ||
			first.Quantity != 1 || first.UnitPriceCents != 5000 || first.DiscountAmountCents != 0 || first.AmountCents != 5000 {
			t.Errorf("明细[0] = %+v, want 剪发/5000/1/0/5000", first)
		}
		second := result.Items[1]
		if second.ServiceID != perm.ID || second.ServiceNameSnapshot != "烫发" ||
			second.Quantity != 2 || second.UnitPriceCents != 2000 || second.AmountCents != 4000 {
			t.Errorf("明细[1] = %+v, want 烫发/2000/2/4000", second)
		}

		// --- Then: 客户余额 10000-9000=1000、积分 floor(9000×1/100)=90、累计消费 9000、最近到店=now ---
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 1000 {
			t.Errorf("余额支付后余额 = %d, want 1000", after.BalanceCents)
		}
		if after.Points != 90 {
			t.Errorf("余额支付后积分 = %d, want 90", after.Points)
		}
		if after.TotalSpentCents != 9000 {
			t.Errorf("余额支付后累计消费 = %d, want 9000", after.TotalSpentCents)
		}
		if after.LastVisitAt == nil {
			t.Fatal("余额支付后 last_visit_at = nil, want now")
		}
		if delta := time.Since(*after.LastVisitAt); delta < -time.Minute || delta > time.Minute {
			t.Errorf("last_visit_at 距现在 = %v, want 约 0", delta)
		}

		// --- Then: 余额流水恰好 1 条：consume/-9000，before/after 连续，关联订单 ---
		if n := env.countRows(t, &model.BalanceTransaction{}); n != 1 {
			t.Fatalf("balance_transactions 行数 = %d, want 1", n)
		}
		var balanceTx model.BalanceTransaction
		if err := env.db.First(&balanceTx).Error; err != nil {
			t.Fatalf("读取余额流水失败: %v", err)
		}
		if balanceTx.Type != model.BalanceTxConsume || balanceTx.AmountCents != -9000 ||
			balanceTx.BalanceBeforeCents != 10000 || balanceTx.BalanceAfterCents != 1000 {
			t.Errorf("余额流水 = %+v, want consume/-9000/10000→1000", balanceTx)
		}
		if balanceTx.ReferenceType != model.ReferenceTypeOrder || balanceTx.ReferenceID == nil || *balanceTx.ReferenceID != order.ID {
			t.Errorf("余额流水 reference = %s/%v, want order/%d", balanceTx.ReferenceType, balanceTx.ReferenceID, order.ID)
		}
		if balanceTx.OperatorID == nil || *balanceTx.OperatorID != 1 {
			t.Errorf("余额流水 operator_id = %v, want 1", balanceTx.OperatorID)
		}

		// --- Then: 积分流水恰好 1 条：earn/90，before/after 连续，关联订单 ---
		if n := env.countRows(t, &model.PointsTransaction{}); n != 1 {
			t.Fatalf("points_transactions 行数 = %d, want 1", n)
		}
		var pointsTx model.PointsTransaction
		if err := env.db.First(&pointsTx).Error; err != nil {
			t.Fatalf("读取积分流水失败: %v", err)
		}
		if pointsTx.Type != model.PointsTxEarn || pointsTx.Points != 90 ||
			pointsTx.BalanceBefore != 0 || pointsTx.BalanceAfter != 90 {
			t.Errorf("积分流水 = %+v, want earn/90/0→90", pointsTx)
		}
		if pointsTx.ReferenceType != model.ReferenceTypeOrder || pointsTx.ReferenceID == nil || *pointsTx.ReferenceID != order.ID {
			t.Errorf("积分流水 reference = %s/%v, want order/%d", pointsTx.ReferenceType, pointsTx.ReferenceID, order.ID)
		}

		// --- Then: 审计日志在事务外写入 order_create ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "order_create").First(&logRow).Error; err != nil {
			t.Fatalf("读取 order_create 审计日志失败: %v", err)
		}
		if logRow.TargetType != "order" || logRow.TargetID != order.ID {
			t.Errorf("审计 target = %s#%d, want order#%d", logRow.TargetType, logRow.TargetID, order.ID)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != 1 {
			t.Errorf("审计 operator_id = %v, want 1", logRow.OperatorID)
		}
		if !strings.Contains(logRow.Content, order.OrderNo) {
			t.Errorf("审计 content = %q, want 含订单号 %s", logRow.Content, order.OrderNo)
		}
	})

	t.Run("cash payment leaves balance untouched", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "现金客户", 10000)
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)

		// --- When: staff 按标准价现金支付 5000 分 ---
		result, err := env.orders.Create(staffCtx(), orderInput("req-cash-1", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000},
		))
		if err != nil {
			t.Fatalf("Create(cash): %v", err)
		}
		if result.Order.Status != model.OrderStatusCompleted || result.Order.PaidAmountCents != 5000 {
			t.Errorf("现金订单 = %+v, want completed/5000", result.Order)
		}

		// --- Then: 余额与余额流水完全不动 ---
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 10000 {
			t.Errorf("现金支付后余额 = %d, want 10000（不得变动）", after.BalanceCents)
		}
		if n := env.countRows(t, &model.BalanceTransaction{}); n != 0 {
			t.Errorf("现金支付产生余额流水 %d 条, want 0", n)
		}

		// --- Then: 积分与累计消费仍然变化（消费产生积分，06 §6） ---
		if after.Points != 50 {
			t.Errorf("现金支付后积分 = %d, want 50", after.Points)
		}
		if after.TotalSpentCents != 5000 {
			t.Errorf("现金支付后累计消费 = %d, want 5000", after.TotalSpentCents)
		}
		if n := env.countRows(t, &model.PointsTransaction{}); n != 1 {
			t.Errorf("现金支付积分流水行数 = %d, want 1", n)
		}
	})

	t.Run("insufficient balance rolls back every table", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "差一分客户", 8999)
		haircut := env.seedService(t, "剪发", 9000, model.StatusEnabled)

		// --- When: 余额 8999 支付 9000（恰好不足 1 分） ---
		_, err := env.orders.Create(adminCtx(), orderInput("req-insufficient-1", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 9000},
		))

		// --- Then: 422/42200 且提示余额不足 ---
		biz := requireBizError(t, err, http.StatusUnprocessableEntity)
		if biz.Code != service.CodeValidationFailed || !strings.Contains(biz.Message, "余额不足") {
			t.Errorf("余额不足 BizError = code %d msg %q, want 42200 含「余额不足」", biz.Code, biz.Message)
		}

		// --- Then: 所有账务表零写入（整体回滚，无部分行） ---
		for name, entity := range map[string]any{
			"orders":               &model.Order{},
			"order_items":          &model.OrderItem{},
			"balance_transactions": &model.BalanceTransaction{},
			"points_transactions":  &model.PointsTransaction{},
			"operation_logs":       &model.OperationLog{},
		} {
			if n := env.countRows(t, entity); n != 0 {
				t.Errorf("余额不足后 %s 行数 = %d, want 0（零部分写入）", name, n)
			}
		}

		// --- Then: 客户余额/积分/累计消费/最近到店均未变化 ---
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 8999 {
			t.Errorf("余额不足后余额 = %d, want 8999", after.BalanceCents)
		}
		if after.Points != 0 || after.TotalSpentCents != 0 || after.LastVisitAt != nil {
			t.Errorf("余额不足后积分/累计消费/最近到店 = %d/%d/%v, want 0/0/nil",
				after.Points, after.TotalSpentCents, after.LastVisitAt)
		}
	})

	t.Run("points floor uses settings ratio", func(t *testing.T) {
		env := newOrderTestEnv(t)
		// Given: 管理员把 points_per_yuan 改为 3（比例读 settings，不写死）。
		if err := env.db.Model(&model.Setting{}).Where("key = ?", model.SettingPointsPerYuan).
			Update("value", "3").Error; err != nil {
			t.Fatalf("更新 points_per_yuan 失败: %v", err)
		}
		customer := env.seedCustomer(t, "积分客户", 0)
		care := env.seedService(t, "护理", 1555, model.StatusEnabled)

		// --- When: 现金支付 1555 分 → floor(1555×3/100) = 46 ---
		if _, err := env.orders.Create(adminCtx(), orderInput("req-points-1", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: care.ID, Quantity: 1, UnitPriceCents: 1555},
		)); err != nil {
			t.Fatalf("Create(points): %v", err)
		}

		// --- Then: 整数先乘后除取 floor ---
		after := env.customerAfter(t, customer.ID)
		if after.Points != 46 {
			t.Errorf("points_per_yuan=3 支付 1555 分后积分 = %d, want 46（floor(4665/100)）", after.Points)
		}
		var pointsTx model.PointsTransaction
		if err := env.db.First(&pointsTx).Error; err != nil {
			t.Fatalf("读取积分流水失败: %v", err)
		}
		if pointsTx.Points != 46 || pointsTx.BalanceAfter != 46 {
			t.Errorf("积分流水 = %+v, want points=46/after=46", pointsTx)
		}
	})

	t.Run("duplicate request id returns original order without new writes", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "幂等客户", 10000)
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)
		perm := env.seedService(t, "烫发", 2000, model.StatusEnabled)

		// --- Given: 首次余额支付 5000 ---
		first, err := env.orders.Create(adminCtx(), orderInput("req-idem-1", customer.ID, model.PaymentMethodBalance,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000},
		))
		if err != nil {
			t.Fatalf("Create(first): %v", err)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 5000 {
			t.Fatalf("首次支付后余额 = %d, want 5000", got)
		}

		// --- When: 同一 request_id 换一套完全不同的入参再次提交 ---
		second, err := env.orders.Create(adminCtx(), orderInput("req-idem-1", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: perm.ID, Quantity: 3, UnitPriceCents: 2000},
		))

		// --- Then: 返回原订单（Created=false、同 id/单号、原明细） ---
		if err != nil {
			t.Fatalf("Create(duplicate): %v", err)
		}
		if second.Created {
			t.Error("重复 request_id Created = true, want false")
		}
		if second.Order.ID != first.Order.ID || second.Order.OrderNo != first.Order.OrderNo {
			t.Errorf("重复 request_id 返回订单 = id %d/no %s, want id %d/no %s",
				second.Order.ID, second.Order.OrderNo, first.Order.ID, first.Order.OrderNo)
		}
		if len(second.Items) != 1 || second.Items[0].ServiceID != haircut.ID {
			t.Errorf("重复 request_id 返回明细 = %+v, want 原明细（剪发）", second.Items)
		}

		// --- Then: 不重复入账：各表行数不变、余额只扣一次 ---
		counts := map[string]int64{
			"orders":               env.countRows(t, &model.Order{}),
			"order_items":          env.countRows(t, &model.OrderItem{}),
			"balance_transactions": env.countRows(t, &model.BalanceTransaction{}),
			"points_transactions":  env.countRows(t, &model.PointsTransaction{}),
			"operation_logs":       env.countRows(t, &model.OperationLog{}),
		}
		for name, got := range counts {
			if got != 1 {
				t.Errorf("重复 request_id 后 %s 行数 = %d, want 1", name, got)
			}
		}
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 5000 || after.TotalSpentCents != 5000 || after.Points != 50 {
			t.Errorf("重复 request_id 后余额/累计消费/积分 = %d/%d/%d, want 5000/5000/50（只入账一次）",
				after.BalanceCents, after.TotalSpentCents, after.Points)
		}
	})

	t.Run("malformed input leaves zero rows", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "校验客户", 5000)
		enabled := env.seedService(t, "启用服务", 5000, model.StatusEnabled)
		disabled := env.seedService(t, "停用服务", 3000, model.StatusDisabled)
		enabledItem := service.OrderItemInput{ServiceID: enabled.ID, Quantity: 1, UnitPriceCents: 5000}
		longRequestID := strings.Repeat("x", 65)

		cases := []struct {
			name       string
			ctx        context.Context
			in         service.OrderCreateInput
			wantStatus int
		}{
			{"数量为 0", adminCtx(), orderInput("req-bad-qty-0", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: enabled.ID, Quantity: 0, UnitPriceCents: 5000}), http.StatusBadRequest},
			{"数量为负", adminCtx(), orderInput("req-bad-qty-neg", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: enabled.ID, Quantity: -1, UnitPriceCents: 5000}), http.StatusBadRequest},
			{"未知客户", adminCtx(), orderInput("req-bad-customer", 999999, model.PaymentMethodCash, enabledItem), http.StatusNotFound},
			{"未知服务", adminCtx(), orderInput("req-bad-service", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: 999999, Quantity: 1, UnitPriceCents: 5000}), http.StatusNotFound},
			{"停用服务", adminCtx(), orderInput("req-disabled-service", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: disabled.ID, Quantity: 1, UnitPriceCents: 3000}), http.StatusUnprocessableEntity},
			{"非法支付方式", adminCtx(), orderInput("req-bad-payment", customer.ID, "points", enabledItem), http.StatusBadRequest},
			{"request_id 为空", adminCtx(), orderInput("", customer.ID, model.PaymentMethodCash, enabledItem), http.StatusBadRequest},
			{"request_id 超长", adminCtx(), orderInput(longRequestID, customer.ID, model.PaymentMethodCash, enabledItem), http.StatusBadRequest},
			{"明细为空", adminCtx(), orderInput("req-no-items", customer.ID, model.PaymentMethodCash), http.StatusBadRequest},
			{"staff 改价", staffCtx(), orderInput("req-staff-override", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: enabled.ID, Quantity: 1, UnitPriceCents: 4000}), http.StatusForbidden},
			{"admin 改价缺原因", adminCtx(), orderInput("req-no-reason", customer.ID, model.PaymentMethodCash,
				service.OrderItemInput{ServiceID: enabled.ID, Quantity: 1, UnitPriceCents: 4000}), http.StatusBadRequest},
		}

		for _, tc := range cases {
			// --- When/Then: 每一类非法输入都返回对应业务错误 ---
			_, err := env.orders.Create(tc.ctx, tc.in)
			requireBizError(t, err, tc.wantStatus)
		}

		// --- Then: 全部失败后所有账务表零写入，客户缓存未被触碰 ---
		for name, entity := range map[string]any{
			"orders":               &model.Order{},
			"order_items":          &model.OrderItem{},
			"balance_transactions": &model.BalanceTransaction{},
			"points_transactions":  &model.PointsTransaction{},
			"operation_logs":       &model.OperationLog{},
		} {
			if n := env.countRows(t, entity); n != 0 {
				t.Errorf("非法输入后 %s 行数 = %d, want 0", name, n)
			}
		}
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 5000 || after.Points != 0 || after.TotalSpentCents != 0 {
			t.Errorf("非法输入后客户余额/积分/累计消费 = %d/%d/%d, want 5000/0/0",
				after.BalanceCents, after.Points, after.TotalSpentCents)
		}
	})

	t.Run("admin price override records discount and reason", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "改价客户", 0)
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)

		// --- When: admin 改价 5000→4000 ×2，原因「老客户八折」 ---
		in := orderInput("req-discount-1", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 2, UnitPriceCents: 4000})
		in.DiscountReason = "老客户八折"
		result, err := env.orders.Create(adminCtx(), in)

		// --- Then: 原价 10000 / 优惠 2000 / 实付 8000；明细级折扣=订单级折扣 ---
		if err != nil {
			t.Fatalf("Create(override): %v", err)
		}
		order := result.Order
		if order.OriginalAmountCents != 10000 || order.DiscountAmountCents != 2000 || order.PaidAmountCents != 8000 {
			t.Errorf("改价订单金额 原价/优惠/实付 = %d/%d/%d, want 10000/2000/8000",
				order.OriginalAmountCents, order.DiscountAmountCents, order.PaidAmountCents)
		}
		if len(result.Items) != 1 || result.Items[0].DiscountAmountCents != 2000 || result.Items[0].AmountCents != 8000 {
			t.Errorf("改价明细 = %+v, want discount=2000/amount=8000", result.Items)
		}

		// --- Then: 改价原因写入 operation_logs（06 §3.1） ---
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "order_create").First(&logRow).Error; err != nil {
			t.Fatalf("读取 order_create 审计日志失败: %v", err)
		}
		if !strings.Contains(logRow.Content, "老客户八折") {
			t.Errorf("审计 content = %q, want 含改价原因「老客户八折」", logRow.Content)
		}
	})

	t.Run("employee snapshot is optional and validated", func(t *testing.T) {
		env := newOrderTestEnv(t)
		customer := env.seedCustomer(t, "员工客户", 0)
		haircut := env.seedService(t, "剪发", 5000, model.StatusEnabled)
		employee := env.seedEmployee(t, "王师傅")

		// --- When: 指定员工开单 ---
		in := orderInput("req-employee-1", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000})
		in.EmployeeID = &employee.ID
		result, err := env.orders.Create(adminCtx(), in)
		if err != nil {
			t.Fatalf("Create(employee): %v", err)
		}

		// --- Then: 订单与明细均记录员工 ---
		if result.Order.EmployeeID == nil || *result.Order.EmployeeID != employee.ID {
			t.Errorf("订单 employee_id = %v, want %d", result.Order.EmployeeID, employee.ID)
		}
		if len(result.Items) != 1 || result.Items[0].EmployeeID == nil || *result.Items[0].EmployeeID != employee.ID {
			t.Errorf("明细 employee_id = %+v, want %d", result.Items, employee.ID)
		}
		if result.EmployeeName != "王师傅" {
			t.Errorf("结果 employee_name = %q, want 王师傅", result.EmployeeName)
		}

		// --- When/Then: 未知员工 → 404 且零写入 ---
		unknown := int64(999999)
		bad := orderInput("req-employee-unknown", customer.ID, model.PaymentMethodCash,
			service.OrderItemInput{ServiceID: haircut.ID, Quantity: 1, UnitPriceCents: 5000})
		bad.EmployeeID = &unknown
		_, err = env.orders.Create(adminCtx(), bad)
		requireBizError(t, err, http.StatusNotFound)
		if n := env.countRows(t, &model.Order{}); n != 1 {
			t.Errorf("未知员工后订单行数 = %d, want 1（仅此前成功单）", n)
		}
	})
}
