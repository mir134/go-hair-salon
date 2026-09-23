package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载订单的只读查询（04-API.md:127-135、plan todo 22）：
// GET /orders（筛选 + 分页，DTO 含客户/员工名）与 GET /orders/:id（含明细快照）。

// OrderListQuery 是 GET /orders 查询输入。
//
// Sort 目前仅支持 recent（按创建时间倒序，也是默认排序），其他值回退默认；
// 日期为 YYYY-MM-DD（UTC 日期边界，含当日），非法格式宽松回退为不过滤。
type OrderListQuery struct {
	Status     string
	CustomerID int64
	EmployeeID int64
	StartDate  string
	EndDate    string
	Sort       string
	PageQuery
}

// OrderListRow 是订单列表读取行（仓储联表结果的类型别名，避免重复建模）。
type OrderListRow = repository.OrderRow

// OrderDetail 是订单详情聚合：订单 + 客户/员工名 + 明细快照。
type OrderDetail struct {
	Order        model.Order
	CustomerName string
	EmployeeName string
	Items        []model.OrderItem
}

// List 按条件分页查询订单（时间倒序，最新在前）。
func (s *OrderService) List(ctx context.Context, q OrderListQuery) (*PageResult[OrderListRow], error) {
	startAt, endAt := parseDateRange(q.StartDate, q.EndDate)
	offset, limit, page, pageSize := q.Normalize()
	rows, total, err := s.orders.List(ctx, repository.OrderListFilter{
		Status:     q.Status,
		CustomerID: q.CustomerID,
		EmployeeID: q.EmployeeID,
		StartAt:    startAt,
		EndAt:      endAt,
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("查询订单列表失败: %w", err)
	}
	return &PageResult[OrderListRow]{Items: rows, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 读取订单详情（含明细快照）；不存在返回 404 订单不存在。
func (s *OrderService) Get(ctx context.Context, id int64) (*OrderDetail, error) {
	row, err := s.orders.FindRowByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "订单不存在")
		}
		return nil, fmt.Errorf("查询订单失败: %w", err)
	}
	items, err := s.orders.ListItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("查询订单明细失败: %w", err)
	}
	return &OrderDetail{
		Order:        row.Order,
		CustomerName: row.CustomerName,
		EmployeeName: row.EmployeeName,
		Items:        items,
	}, nil
}

// parseDateRange 把 YYYY-MM-DD 起止日期解析为 created_at 的半开区间 [start, end)：
// start_date 含当日 0 点；end_date 含当日 23:59:59（即 end_date+1 天的 0 点，UTC）。
// 空串/非法格式回退为不过滤（只读便利参数，不得让列表查询失败）。
func parseDateRange(startDate, endDate string) (startAt, endAt *time.Time) {
	if start, ok := parseDateBound(startDate); ok {
		startAt = &start
	}
	if end, ok := parseDateBound(endDate); ok {
		next := end.AddDate(0, 0, 1)
		endAt = &next
	}
	return startAt, endAt
}

// parseDateBound 解析 UTC 日期（YYYY-MM-DD）；空串或非法格式返回 ok=false。
func parseDateBound(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
