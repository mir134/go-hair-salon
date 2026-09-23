package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载创建订单的输入校验与定价（service.Create 的辅助逻辑）：
// 幂等键、客户、员工（可选）、服务（启用）、支付方式、数量、改价权限（06 §3.1）。

// validateRequestID 校验幂等键（客户端 UUID）：必填且 ≤64 字符（orders.request_id size:64）。
func validateRequestID(raw string) (string, error) {
	requestID := strings.TrimSpace(raw)
	if requestID == "" {
		return "", BadRequest("request_id 不能为空")
	}
	if len(requestID) > 64 {
		return "", BadRequest("request_id 不能超过 64 个字符")
	}
	return requestID, nil
}

// validateAndPrice 校验订单输入并完成定价：
//   - 客户必须存在（含未软删除）→ 404；
//   - 支付方式 ∈ {cash, wechat, alipay, balance} → 400；
//   - 员工可选；提供时必须存在 → 404；
//   - 每行数量 > 0 → 400；服务必须存在且启用（EnsureEnabledForConsumption）→ 404/422；
//   - 成交单价与标准价不同 → 仅 admin 且必须填写原因 → 403/400。
func (s *OrderService) validateAndPrice(ctx context.Context, in OrderCreateInput) ([]orderItemPlan, *model.Customer, *model.Employee, error) {
	if in.CustomerID <= 0 {
		return nil, nil, nil, BadRequest("客户不能为空")
	}
	customer, err := s.customers.FindByID(ctx, in.CustomerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, nil, ErrCustomerNotFound
		}
		return nil, nil, nil, fmt.Errorf("查询客户失败: %w", err)
	}

	if !isValidPaymentMethod(in.PaymentMethod) {
		return nil, nil, nil, BadRequest("支付方式不合法")
	}

	employee, err := s.ensureEmployee(ctx, in.EmployeeID)
	if err != nil {
		return nil, nil, nil, err
	}

	if len(in.Items) == 0 {
		return nil, nil, nil, BadRequest("订单至少包含一个服务项目")
	}

	plans := make([]orderItemPlan, 0, len(in.Items))
	hasOverride := false
	for _, item := range in.Items {
		if item.Quantity <= 0 {
			return nil, nil, nil, BadRequest("服务数量必须大于 0")
		}
		service, err := s.services.EnsureEnabledForConsumption(ctx, item.ServiceID)
		if err != nil {
			return nil, nil, nil, err
		}
		unitPrice := item.UnitPriceCents
		if unitPrice < 0 {
			return nil, nil, nil, BadRequest("成交单价不能为负（整数分）")
		}
		if unitPrice == 0 {
			// 未提供成交价（含 JSON 缺省 0）→ 按标准价；服务标准价必 > 0（06 §2）。
			unitPrice = service.PriceCents
		}
		if unitPrice != service.PriceCents {
			hasOverride = true
		}
		plans = append(plans, orderItemPlan{
			serviceID:      service.ID,
			serviceName:    service.Name,
			quantity:       item.Quantity,
			unitPriceCents: unitPrice,
			originalTotal:  service.PriceCents * int64(item.Quantity),
			discountTotal:  (service.PriceCents - unitPrice) * int64(item.Quantity),
			amountTotal:    unitPrice * int64(item.Quantity),
		})
	}

	if hasOverride {
		if err := ensureCanOverridePrice(ctx, in.DiscountReason); err != nil {
			return nil, nil, nil, err
		}
	}
	return plans, customer, employee, nil
}

// ensureEmployee 校验可选员工：nil 表示不指定；提供时必须存在（03-DATABASE.md:31-42）。
//
// 员工停用状态对历史/新订单的约束文档未定义（员工维护属 todo 38-40），此处只校验存在性。
func (s *OrderService) ensureEmployee(ctx context.Context, employeeID *int64) (*model.Employee, error) {
	if employeeID == nil {
		return nil, nil
	}
	if *employeeID <= 0 {
		return nil, BadRequest("员工 id 不合法")
	}
	employee, err := s.employees.FindByID(ctx, *employeeID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "员工不存在")
		}
		return nil, fmt.Errorf("查询员工失败: %w", err)
	}
	return employee, nil
}

// ensureCanOverridePrice 校验改价权限与原因（06 §3.1：仅 admin 可改价，必须记录原因）。
//
// 权限判定使用请求上下文中的实时用户对象（由 JWT 中间件从数据库加载），
// 后端是最终安全边界，不依赖前端隐藏输入框。
func ensureCanOverridePrice(ctx context.Context, reason string) error {
	user, ok := CurrentUser(ctx)
	if !ok || user.Role != model.RoleAdmin {
		return Forbidden("仅管理员可改价/折扣")
	}
	if strings.TrimSpace(reason) == "" {
		return BadRequest("改价必须填写原因")
	}
	return nil
}

// isValidPaymentMethod 判断支付方式是否属于 06 §3 支持的四种。
func isValidPaymentMethod(method string) bool {
	switch method {
	case model.PaymentMethodCash, model.PaymentMethodWechat, model.PaymentMethodAlipay, model.PaymentMethodBalance:
		return true
	default:
		return false
	}
}

// pointsPerYuan 读取积分比例（settings.points_per_yuan，06 §6）。
//
// 缺失/非法/负数时回退默认值 1：错误配置不得阻塞收银；
// 正常值由 todo 41-43 的设置接口维护。
func (s *OrderService) pointsPerYuan(ctx context.Context) (int64, error) {
	setting, err := s.settings.FindByKey(ctx, model.SettingPointsPerYuan)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return model.DefaultPointsPerYuan, nil
		}
		return 0, fmt.Errorf("读取积分比例失败: %w", err)
	}
	ratio, parseErr := strconv.ParseInt(strings.TrimSpace(setting.Value), 10, 64)
	if parseErr != nil || ratio < 0 {
		return model.DefaultPointsPerYuan, nil
	}
	return ratio, nil
}

// findByRequestID 查询幂等键对应的原订单（含明细）；不存在返回 (nil, nil)。
func (s *OrderService) findByRequestID(ctx context.Context, requestID string) (*OrderCreateResult, error) {
	order, err := s.orders.FindByRequestID(ctx, requestID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询幂等订单失败: %w", err)
	}
	items, err := s.orders.ListItems(ctx, order.ID)
	if err != nil {
		return nil, fmt.Errorf("查询订单明细失败: %w", err)
	}
	result := &OrderCreateResult{Order: order, Items: items, Created: false}

	customerName, err := s.customerName(ctx, order.CustomerID)
	if err != nil {
		return nil, err
	}
	result.CustomerName = customerName
	if order.EmployeeID != nil {
		employeeName, err := s.employeeName(ctx, *order.EmployeeID)
		if err != nil {
			return nil, err
		}
		result.EmployeeName = employeeName
	}
	return result, nil
}

// customerName 读取客户姓名；客户已软删除时返回空名（历史订单仍可幂等返回）。
func (s *OrderService) customerName(ctx context.Context, customerID int64) (string, error) {
	customer, err := s.customers.FindByID(ctx, customerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("查询客户失败: %w", err)
	}
	return customer.Name, nil
}

// employeeName 读取员工姓名；员工不存在时返回空名（历史订单仍可幂等返回）。
func (s *OrderService) employeeName(ctx context.Context, employeeID int64) (string, error) {
	employee, err := s.employees.FindByID(ctx, employeeID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("查询员工失败: %w", err)
	}
	return employee.Name, nil
}
