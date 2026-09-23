package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// OrderService 负责消费订单的创建与状态流转（04-API.md:127-155、06-BUSINESS-RULES.md:20-44）。
//
// 直接完成的消费在单个数据库事务内完成（03-DATABASE.md:319-331）：
// 订单 + 明细 + 余额原子扣减 + 余额流水 + 积分流水 + 客户账务缓存；
// 挂单（status=pending）只写订单 + 明细，不产生任何资金/余额/积分变动（03-DATABASE.md:309-317）；
// 结账（pending → completed）复用同一套账务写入（03-DATABASE.md:333-347）；
// 任一步失败整体回滚（零部分写入）。
//
// operation_logs 在事务提交之后写入：审计不随业务回滚丢失，
// 且唯一连接池下事务内写日志会等待被事务占用的连接（见 OperationLogService 注释）。
type OrderService struct {
	tx        *repository.Transactor
	orders    *repository.OrderRepository
	customers *repository.CustomerRepository
	services  *ServiceItemService
	employees *repository.EmployeeRepository
	ledger    *repository.LedgerRepository
	settings  *repository.SettingsRepository
	logs      *OperationLogService
}

// OrderServiceDeps 是订单服务的依赖集合（协作者较多，用结构体聚合避免超长参数列表）。
type OrderServiceDeps struct {
	Tx        *repository.Transactor
	Orders    *repository.OrderRepository
	Customers *repository.CustomerRepository
	Services  *ServiceItemService
	Employees *repository.EmployeeRepository
	Ledger    *repository.LedgerRepository
	Settings  *repository.SettingsRepository
	Logs      *OperationLogService
}

// NewOrderService 构造订单服务。
func NewOrderService(deps OrderServiceDeps) *OrderService {
	return &OrderService{
		tx:        deps.Tx,
		orders:    deps.Orders,
		customers: deps.Customers,
		services:  deps.Services,
		employees: deps.Employees,
		ledger:    deps.Ledger,
		settings:  deps.Settings,
		logs:      deps.Logs,
	}
}

// OrderItemInput 是创建订单的单行明细输入（字段与前端 OrderItemPayload 对齐）。
//
// UnitPriceCents 为成交单价（整数分）：0 表示按服务标准价成交；
// 与标准价不同属于改价/折扣，仅 admin 可操作且必须提供改价原因（06 §3.1）。
type OrderItemInput struct {
	ServiceID      int64
	Quantity       int
	UnitPriceCents int64
}

// OrderCreateInput 是订单创建输入（字段与 web/src/api/order.ts:63-73 对齐）。
//
// Status 为空或 completed 表示直接完成（收款）；pending 表示挂单（不收款，
// 不产生任何资金/余额/积分变动，04-API.md:145-149）。
type OrderCreateInput struct {
	RequestID      string
	CustomerID     int64
	EmployeeID     *int64
	PaymentMethod  string
	Status         string
	DiscountReason string
	Items          []OrderItemInput
}

// OrderCreateResult 是创建订单结果。
//
// Created=false 表示命中 request_id 幂等键：返回原订单（含原明细），本次未写入任何数据。
type OrderCreateResult struct {
	Order        *model.Order
	Items        []model.OrderItem
	Created      bool
	CustomerName string
	EmployeeName string
}

// orderItemPlan 是校验并定价后的明细计划：服务快照 + 金额（全部整数分）。
type orderItemPlan struct {
	serviceID      int64
	serviceName    string
	quantity       int
	unitPriceCents int64
	originalTotal  int64 // 标准价 × 数量
	discountTotal  int64 // (标准价 − 成交价) × 数量
	amountTotal    int64 // 成交价 × 数量
}

// orderDraftInput 聚合订单草稿的输入（避免超长参数列表）。
type orderDraftInput struct {
	RequestID     string
	Status        string
	PaymentMethod string
	Now           time.Time
	CustomerID    int64
	Employee      *model.Employee
	Plans         []orderItemPlan
}

// errInsufficientBalance 是余额不足的标准业务校验失败（422，不产生任何写入）。
var errInsufficientBalance = Validation("余额不足，请充值或更换支付方式")

// errOrderStateNotPending 是内部哨兵：订单不存在或不是 pending 状态。
//
// 事务内用条件 UPDATE + RowsAffected 判定（原子守卫）；事务外翻译为
// 404（订单不存在）或 409/422（状态冲突），翻译需要另一次非事务读取。
var errOrderStateNotPending = errors.New("order is not pending")

// orderNoSeq 是订单号后 4 位的进程内序号（ux_orders_order_no 唯一索引兜底）。
var orderNoSeq atomic.Uint64

// Create 创建订单：status 为空/completed 时直接完成收款，pending 时挂单（不收款）：
//
//  1. request_id 幂等：已存在则返回原订单（调用方渲染 200），不重复入账；
//  2. 校验客户/服务（启用）/员工（可选）/支付方式/数量/改价权限（06 §3.1）；
//  3. 单事务写入：订单 + 明细（直接完成时另加余额扣减/流水、积分/流水、客户统计）；
//  4. 事务提交后写 operation_logs(action=order_create)。
func (s *OrderService) Create(ctx context.Context, in OrderCreateInput) (*OrderCreateResult, error) {
	requestID, err := validateRequestID(in.RequestID)
	if err != nil {
		return nil, err
	}

	// 幂等快路径：同一 request_id 直接返回原订单（唯一索引是并发兜底）。
	existing, err := s.findByRequestID(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	status, err := resolveCreateStatus(in.Status)
	if err != nil {
		return nil, err
	}

	plans, customer, employee, err := s.validateAndPrice(ctx, in, status)
	if err != nil {
		return nil, err
	}

	// 积分比例必须在事务外读取（唯一连接池下事务内非事务读会自锁）。
	var ratio int64
	if status == model.OrderStatusCompleted {
		ratio, err = s.pointsPerYuan(ctx)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	order, items := newOrderDraft(orderDraftInput{
		RequestID:     requestID,
		Status:        status,
		PaymentMethod: in.PaymentMethod,
		Now:           now,
		CustomerID:    customer.ID,
		Employee:      employee,
		Plans:         plans,
	})

	operatorID := OperatorIDPtr(ctx)
	var writeErr error
	if status == model.OrderStatusPending {
		writeErr = s.createPendingTx(ctx, order, items)
	} else {
		writeErr = s.createCompletedTx(ctx, order, items, ratio, now, operatorID)
	}
	if writeErr != nil {
		if errors.Is(writeErr, repository.ErrDuplicate) {
			// 并发兜底：唯一索引已阻止重复入账 → 返回原订单。
			existing, findErr := s.findByRequestID(ctx, requestID)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return existing, nil
			}
			return nil, Conflict("订单已存在，请勿重复提交")
		}
		return nil, writeErr
	}

	// 事务提交后写审计日志（禁止在事务内写，见 OperationLogService 注释）。
	s.writeCreateLog(ctx, order, in.DiscountReason)

	employeeName := ""
	if employee != nil {
		employeeName = employee.Name
	}
	return &OrderCreateResult{
		Order:        order,
		Items:        items,
		Created:      true,
		CustomerName: customer.Name,
		EmployeeName: employeeName,
	}, nil
}

// createPendingTx 单事务写入挂单与明细：不写任何账务表（03-DATABASE.md:309-317）。
func (s *OrderService) createPendingTx(ctx context.Context, order *model.Order, items []model.OrderItem) error {
	return s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		if err := s.orders.CreateTx(ctx, tx, order); err != nil {
			return err
		}
		for i := range items {
			items[i].OrderID = order.ID
		}
		if err := s.orders.CreateItemsTx(ctx, tx, items); err != nil {
			return fmt.Errorf("创建订单明细失败: %w", err)
		}
		return nil
	})
}

// createCompletedTx 单事务写入直接完成订单（03-DATABASE.md:319-331）：
// 订单 + 明细 + 余额原子扣减（仅余额支付）+ 余额流水 + 积分流水 + 客户账务缓存。
func (s *OrderService) createCompletedTx(ctx context.Context, order *model.Order, items []model.OrderItem, ratio int64, now time.Time, operatorID *int64) error {
	// 积分 = floor(实付分 × 每元积分 ÷ 100)：整数先乘后除（06 §6）。
	points := order.PaidAmountCents * ratio / 100
	return s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		if err := s.orders.CreateTx(ctx, tx, order); err != nil {
			return err
		}
		for i := range items {
			items[i].OrderID = order.ID
		}
		if err := s.orders.CreateItemsTx(ctx, tx, items); err != nil {
			return fmt.Errorf("创建订单明细失败: %w", err)
		}
		if err := s.applyBalanceTx(ctx, tx, order, operatorID); err != nil {
			return err
		}
		if err := s.applyPointsTx(ctx, tx, order, points, operatorID); err != nil {
			return err
		}
		if err := s.customers.ApplyConsumptionTx(ctx, tx, order.CustomerID, order.PaidAmountCents, now); err != nil {
			return fmt.Errorf("更新客户消费统计失败: %w", err)
		}
		return nil
	})
}

// newOrderDraft 组装订单与明细快照：金额一律整数分（原价 = Σ 标准价×数量；
// 优惠 = Σ（标准价−成交价）×数量；实付 = 原价 − 优惠），状态由调用方给出。
func newOrderDraft(in orderDraftInput) (*model.Order, []model.OrderItem) {
	var originalCents, discountCents int64
	for _, plan := range in.Plans {
		originalCents += plan.originalTotal
		discountCents += plan.discountTotal
	}
	order := &model.Order{
		OrderNo:             generateOrderNo(in.Now),
		RequestID:           in.RequestID,
		CustomerID:          in.CustomerID,
		OriginalAmountCents: originalCents,
		DiscountAmountCents: discountCents,
		PaidAmountCents:     originalCents - discountCents,
		PaymentMethod:       in.PaymentMethod,
		Status:              in.Status,
	}
	if in.Employee != nil {
		employeeID := in.Employee.ID
		order.EmployeeID = &employeeID
	}

	items := make([]model.OrderItem, 0, len(in.Plans))
	for _, plan := range in.Plans {
		items = append(items, model.OrderItem{
			ServiceID:           plan.serviceID,
			ServiceNameSnapshot: plan.serviceName,
			Quantity:            plan.quantity,
			UnitPriceCents:      plan.unitPriceCents,
			DiscountAmountCents: plan.discountTotal,
			AmountCents:         plan.amountTotal,
			EmployeeID:          order.EmployeeID,
		})
	}
	return order, items
}

// generateOrderNo 生成订单号：yyyyMMddHHmmss + 4 位进程内序号（同一秒内单调递增）。
func generateOrderNo(now time.Time) string {
	seq := orderNoSeq.Add(1) % 10000
	return now.UTC().Format("20060102150405") + fmt.Sprintf("%04d", seq)
}
