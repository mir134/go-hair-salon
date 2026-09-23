package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// 本文件承载 Dashboard 统计（plan todo 44、06 §8:86-103、07-UI.md:76-83）。
//
// 统计口径（与 06 §8 及已记录决议一致）：
//   - 今日营业额 = 当日 completed 订单 paid_amount_cents 合计 − 当日退款冲减；
//   - 退款发生日 = orders.updated_at（todo 35 决议：退款 worker 以 updated_at 标记退款日）；
//   - 今日消费单数 / 消费人数 = 当日 completed 订单数 / 去重客户数；
//   - 今日新增客户 = 当日创建客户数；
//   - 今日充值金额 = 当日 recharge_records.actual_amount_cents 合计 − 当日冲正充值 actual 合计；
//   - 待结账单数 = 当前 pending 总数（实时，不受日期范围影响）；
//   - 余额消费计入营业额；充值不计入营业额；pending/取消/退款订单不重复计入；
//   - 日界 = 服务器本地时区（UTC 存、查询转本地；D3 决议）。

const (
	// dashboardRecentLimit 是「最近消费/最近充值」列表条数（plan todo 44）。
	dashboardRecentLimit = 10
	// dashboardMaxRangeDays 是日期范围统计的最大跨度：
	// 防止单请求生成超长逐日序列拖垮单连接 SQLite（唯一连接池）。
	dashboardMaxRangeDays = 366
)

// localDayWindow 返回 day 所在「服务器本地日」的 UTC 半开区间 [start, end)。
//
// 数据库统一存 UTC（06 §10:115），日界按服务器本地时区换算（D3 决议、plan todo 44）；
// AddDate 作用于本地 0 点，跨夏令时也不会偏移。
func localDayWindow(day time.Time, loc *time.Location) (time.Time, time.Time) {
	local := day.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

// DashboardRangeQuery 是 Dashboard 日期范围输入（start_date/end_date，YYYY-MM-DD，含当日）。
type DashboardRangeQuery struct {
	StartDate string
	EndDate   string
}

// resolve 解析并校验日期范围（服务器本地时区语义）：
//
//   - 空 start_date → 本月 1 日；空 end_date → 今天（本地）；
//   - 非法格式 / start > end / 跨度超过 dashboardMaxRangeDays → 400 参数错误。
//
// 返回 [start, endExclusive)（本地日 0 点语义，含 end_date 当日）。
func (q DashboardRangeQuery) resolve(now time.Time, loc *time.Location) (start, endExclusive time.Time, err error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	if raw := strings.TrimSpace(q.StartDate); raw != "" {
		parsed, parseErr := time.ParseInLocation("2006-01-02", raw, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, BadRequest("start_date 格式无效（应为 YYYY-MM-DD）")
		}
		start = parsed
	}
	end := today
	if raw := strings.TrimSpace(q.EndDate); raw != "" {
		parsed, parseErr := time.ParseInLocation("2006-01-02", raw, loc)
		if parseErr != nil {
			return time.Time{}, time.Time{}, BadRequest("end_date 格式无效（应为 YYYY-MM-DD）")
		}
		end = parsed
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, BadRequest("start_date 不能晚于 end_date")
	}
	days := 1
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		days++
		if days > dashboardMaxRangeDays {
			return time.Time{}, time.Time{}, BadRequest(fmt.Sprintf("日期范围不能超过 %d 天", dashboardMaxRangeDays))
		}
	}
	return start, end.AddDate(0, 0, 1), nil
}

// DashboardService 提供 Dashboard 只读统计（权限 both，04-API.md:217-236）。
type DashboardService struct {
	dashboard *repository.DashboardRepository
}

// NewDashboardService 构造 Dashboard 统计服务。
func NewDashboardService(dashboard *repository.DashboardRepository) *DashboardService {
	return &DashboardService{dashboard: dashboard}
}

// DashboardSummary 是 GET /dashboard/summary 的统计结果（金额一律整数分）。
type DashboardSummary struct {
	Date                   string // 统计日（服务器本地时区，YYYY-MM-DD）
	RevenueCents           int64  // 今日营业额
	CompletedOrderCount    int64  // 今日消费单数
	ConsumingCustomerCount int64  // 今日消费人数（去重）
	NewCustomerCount       int64  // 今日新增客户
	RechargeCents          int64  // 今日充值金额
	PendingOrderCount      int64  // 待结账单数（当前实时，不受日期范围影响）
	RecentOrders           []repository.OrderRow
	RecentRecharges        []repository.RechargeRow
}

// Summary 汇总今日 KPI（06 §8 口径）+ 最近消费/最近充值各 10 条。
//
// 恒为「今日」口径：日期范围参数不参与（07-UI.md:82 待结账单数为实时数据）。
func (s *DashboardService) Summary(ctx context.Context) (*DashboardSummary, error) {
	now := time.Now()
	start, end := localDayWindow(now, time.Local)

	stats, err := s.dashboard.SumCompletedOrders(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("统计今日订单失败: %w", err)
	}
	refunded, err := s.dashboard.SumRefundedOrders(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("统计今日退款冲减失败: %w", err)
	}
	newCustomers, err := s.dashboard.CountNewCustomers(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("统计今日新增客户失败: %w", err)
	}
	rechargeActual, err := s.dashboard.SumRechargeActual(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("统计今日充值失败: %w", err)
	}
	rechargeReversed, err := s.dashboard.SumReversedRechargeActual(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("统计今日充值冲正失败: %w", err)
	}
	pending, err := s.dashboard.CountPendingOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计待结账单数失败: %w", err)
	}
	recentOrders, err := s.dashboard.RecentCompletedOrders(ctx, dashboardRecentLimit)
	if err != nil {
		return nil, fmt.Errorf("查询最近消费失败: %w", err)
	}
	recentRecharges, err := s.dashboard.RecentRecharges(ctx, dashboardRecentLimit)
	if err != nil {
		return nil, fmt.Errorf("查询最近充值失败: %w", err)
	}

	return &DashboardSummary{
		Date:                   start.In(time.Local).Format("2006-01-02"),
		RevenueCents:           stats.PaidCents - refunded,
		CompletedOrderCount:    stats.OrderCount,
		ConsumingCustomerCount: stats.CustomerCount,
		NewCustomerCount:       newCustomers,
		RechargeCents:          rechargeActual - rechargeReversed,
		PendingOrderCount:      pending,
		RecentOrders:           recentOrders,
		RecentRecharges:        recentRecharges,
	}, nil
}

// RevenuePoint 是营收序列的单日数据点。
type RevenuePoint struct {
	Date         string
	RevenueCents int64
}

// RevenueSeries 是日期范围营收序列（回显解析后的实际范围）。
type RevenueSeries struct {
	StartDate string
	EndDate   string
	Items     []RevenuePoint
}

// Revenue 返回日期范围内按服务器本地日切片的营收序列（06 §8 口径）：
// 每日营收 = 当日 completed 订单 paid 合计 − 当日退款冲减（按退款发生日归属）。
func (s *DashboardService) Revenue(ctx context.Context, q DashboardRangeQuery) (*RevenueSeries, error) {
	start, endExclusive, err := q.resolve(time.Now(), time.Local)
	if err != nil {
		return nil, err
	}
	series := &RevenueSeries{
		StartDate: start.Format("2006-01-02"),
		EndDate:   endExclusive.AddDate(0, 0, -1).Format("2006-01-02"),
		Items:     make([]RevenuePoint, 0),
	}
	for day := start; day.Before(endExclusive); day = day.AddDate(0, 0, 1) {
		dayStart, dayEnd := localDayWindow(day, time.Local)
		stats, err := s.dashboard.SumCompletedOrders(ctx, dayStart, dayEnd)
		if err != nil {
			return nil, fmt.Errorf("统计营收序列失败: %w", err)
		}
		refunded, err := s.dashboard.SumRefundedOrders(ctx, dayStart, dayEnd)
		if err != nil {
			return nil, fmt.Errorf("统计退款冲减失败: %w", err)
		}
		series.Items = append(series.Items, RevenuePoint{
			Date:         day.Format("2006-01-02"),
			RevenueCents: stats.PaidCents - refunded,
		})
	}
	return series, nil
}

// CustomerPoint 是客户趋势序列的单日数据点。
type CustomerPoint struct {
	Date                   string
	NewCustomerCount       int64
	ConsumingCustomerCount int64
}

// CustomerSeries 是日期范围客户趋势序列（回显解析后的实际范围）。
type CustomerSeries struct {
	StartDate string
	EndDate   string
	Items     []CustomerPoint
}

// Customers 返回日期范围内按服务器本地日切片的「新增客户 / 消费人数」趋势。
func (s *DashboardService) Customers(ctx context.Context, q DashboardRangeQuery) (*CustomerSeries, error) {
	start, endExclusive, err := q.resolve(time.Now(), time.Local)
	if err != nil {
		return nil, err
	}
	series := &CustomerSeries{
		StartDate: start.Format("2006-01-02"),
		EndDate:   endExclusive.AddDate(0, 0, -1).Format("2006-01-02"),
		Items:     make([]CustomerPoint, 0),
	}
	for day := start; day.Before(endExclusive); day = day.AddDate(0, 0, 1) {
		dayStart, dayEnd := localDayWindow(day, time.Local)
		newCustomers, err := s.dashboard.CountNewCustomers(ctx, dayStart, dayEnd)
		if err != nil {
			return nil, fmt.Errorf("统计新增客户趋势失败: %w", err)
		}
		stats, err := s.dashboard.SumCompletedOrders(ctx, dayStart, dayEnd)
		if err != nil {
			return nil, fmt.Errorf("统计消费人数趋势失败: %w", err)
		}
		series.Items = append(series.Items, CustomerPoint{
			Date:                   day.Format("2006-01-02"),
			NewCustomerCount:       newCustomers,
			ConsumingCustomerCount: stats.CustomerCount,
		})
	}
	return series, nil
}

// EmployeePerformance 是单个员工的业绩（amount_cents=成交金额净额，整数分）。
type EmployeePerformance struct {
	EmployeeID   *int64 // 为空 = 明细与订单都未指定员工（未分配）
	EmployeeName string
	AmountCents  int64
}

// EmployeePerformanceReport 是员工业绩报表（回显解析后的实际日期范围）。
type EmployeePerformanceReport struct {
	StartDate string
	EndDate   string
	Items     []EmployeePerformance
}

// EmployeePerformance 汇总日期范围内员工业绩（plan todo 45、D2 决议）：
//
//   - completed 明细按成交日（orders.created_at）计入，退款明细按退款发生日
//     （orders.updated_at）冲减 —— 与 06 §8:98 营业额口径一致；
//   - 员工归属优先明细 employee_id，为空回退订单 employee_id（repository.SumEmployeeItems）；
//   - 净额排序：金额降序，其次 employee_id 升序（未分配员工排最后）。
func (s *DashboardService) EmployeePerformance(ctx context.Context, q DashboardRangeQuery) (*EmployeePerformanceReport, error) {
	start, endExclusive, err := q.resolve(time.Now(), time.Local)
	if err != nil {
		return nil, err
	}
	rangeStart, _ := localDayWindow(start, time.Local)
	_, rangeEnd := localDayWindow(endExclusive.AddDate(0, 0, -1), time.Local)

	completed, err := s.dashboard.SumEmployeeItems(ctx, model.OrderStatusCompleted, rangeStart, rangeEnd)
	if err != nil {
		return nil, fmt.Errorf("统计员工业绩失败: %w", err)
	}
	refunded, err := s.dashboard.SumEmployeeItems(ctx, model.OrderStatusRefunded, rangeStart, rangeEnd)
	if err != nil {
		return nil, fmt.Errorf("统计员工退款冲减失败: %w", err)
	}

	return &EmployeePerformanceReport{
		StartDate: start.Format("2006-01-02"),
		EndDate:   endExclusive.AddDate(0, 0, -1).Format("2006-01-02"),
		Items:     mergeEmployeePerformance(completed, refunded),
	}, nil
}

// mergeEmployeePerformance 合并「成交 +」与「退款 −」两组聚合行为净额口径。
func mergeEmployeePerformance(completed, refunded []repository.EmployeePerformanceRow) []EmployeePerformance {
	byEmployee := make(map[int64]*EmployeePerformance, len(completed))
	var unassigned *EmployeePerformance
	pick := func(id *int64) *EmployeePerformance {
		if id == nil {
			if unassigned == nil {
				unassigned = &EmployeePerformance{}
			}
			return unassigned
		}
		row, ok := byEmployee[*id]
		if !ok {
			row = &EmployeePerformance{EmployeeID: id}
			byEmployee[*id] = row
		}
		return row
	}
	for _, row := range completed {
		item := pick(row.EmployeeID)
		item.EmployeeName = row.EmployeeName
		item.AmountCents += row.AmountCents
	}
	for _, row := range refunded {
		item := pick(row.EmployeeID)
		if item.EmployeeName == "" {
			item.EmployeeName = row.EmployeeName
		}
		item.AmountCents -= row.AmountCents
	}

	items := make([]EmployeePerformance, 0, len(byEmployee)+1)
	for _, row := range byEmployee {
		items = append(items, *row)
	}
	if unassigned != nil {
		items = append(items, *unassigned)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AmountCents != items[j].AmountCents {
			return items[i].AmountCents > items[j].AmountCents
		}
		return employeeSortKey(items[i].EmployeeID) < employeeSortKey(items[j].EmployeeID)
	})
	return items
}

// employeeSortKey 把可空员工 id 映射为可比较排序键（未分配排最后）。
func employeeSortKey(id *int64) int64 {
	if id == nil {
		return math.MaxInt64
	}
	return *id
}
