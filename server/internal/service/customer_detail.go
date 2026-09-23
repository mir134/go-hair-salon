package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// CustomerDetailService 提供客户详情聚合查询（04-API.md:80-82）。
//
// 客户详情必须能追溯消费、充值、余额流水、积分流水（06-BUSINESS-RULES.md:11）；
// 三个聚合接口均为分页 + 时间倒序，客户不存在统一返回 404 客户不存在。
type CustomerDetailService struct {
	customers *repository.CustomerRepository
	orders    *repository.OrderRepository
	ledger    *repository.LedgerRepository
}

// NewCustomerDetailService 构造客户详情聚合服务。
func NewCustomerDetailService(
	customers *repository.CustomerRepository,
	orders *repository.OrderRepository,
	ledger *repository.LedgerRepository,
) *CustomerDetailService {
	return &CustomerDetailService{customers: customers, orders: orders, ledger: ledger}
}

// ListOrders 返回客户的消费记录（分页、时间倒序）。
func (s *CustomerDetailService) ListOrders(ctx context.Context, customerID int64, page PageQuery) (*PageResult[model.Order], error) {
	if err := s.ensureCustomer(ctx, customerID); err != nil {
		return nil, err
	}
	offset, limit, pageNo, pageSize := page.Normalize()
	items, total, err := s.orders.ListByCustomer(ctx, customerID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("查询客户订单失败: %w", err)
	}
	return &PageResult[model.Order]{Items: items, Total: total, Page: pageNo, PageSize: pageSize}, nil
}

// ListBalanceTransactions 返回客户的余额流水（分页、时间倒序）。
func (s *CustomerDetailService) ListBalanceTransactions(ctx context.Context, customerID int64, page PageQuery) (*PageResult[model.BalanceTransaction], error) {
	if err := s.ensureCustomer(ctx, customerID); err != nil {
		return nil, err
	}
	offset, limit, pageNo, pageSize := page.Normalize()
	items, total, err := s.ledger.ListBalanceByCustomer(ctx, customerID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("查询客户余额流水失败: %w", err)
	}
	return &PageResult[model.BalanceTransaction]{Items: items, Total: total, Page: pageNo, PageSize: pageSize}, nil
}

// ListPointsTransactions 返回客户的积分流水（分页、时间倒序）。
func (s *CustomerDetailService) ListPointsTransactions(ctx context.Context, customerID int64, page PageQuery) (*PageResult[model.PointsTransaction], error) {
	if err := s.ensureCustomer(ctx, customerID); err != nil {
		return nil, err
	}
	offset, limit, pageNo, pageSize := page.Normalize()
	items, total, err := s.ledger.ListPointsByCustomer(ctx, customerID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("查询客户积分流水失败: %w", err)
	}
	return &PageResult[model.PointsTransaction]{Items: items, Total: total, Page: pageNo, PageSize: pageSize}, nil
}

// ensureCustomer 校验客户存在（含未软删除）；不存在返回 404 客户不存在。
func (s *CustomerDetailService) ensureCustomer(ctx context.Context, customerID int64) error {
	if _, err := s.customers.FindByID(ctx, customerID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCustomerNotFound
		}
		return fmt.Errorf("查询客户失败: %w", err)
	}
	return nil
}
