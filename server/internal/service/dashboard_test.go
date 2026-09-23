package service

// TestLocalDayWindow / TestDashboardRangeResolve 锁定 Dashboard 统计的日期语义（plan todo 44）：
//   - 日界 = 服务器本地时区（固定 UTC+8 时区证明换算不是 UTC 日界）；
//   - 跨午夜：本地 23:59:59 与次日 00:00:01 分属相邻窗口；
//   - 日期范围解析：缺省=本月 1 日~今日、非法/倒置/超长 → 400。

import (
	"testing"
	"time"
)

func TestLocalDayWindow(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)

	// Given: 本地 2026-09-23 10:30（UTC+8）
	day := time.Date(2026, 9, 23, 10, 30, 0, 0, cst)

	// When
	start, end := localDayWindow(day, cst)

	// Then: 窗口 = [2026-09-22T16:00Z, 2026-09-23T16:00Z)
	wantStart := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("localDayWindow = [%s, %s), want [%s, %s)", start, end, wantStart, wantEnd)
	}
	if !start.Before(end) {
		t.Fatalf("窗口起点不早于终点: [%s, %s)", start, end)
	}

	// Then: 本地 23:59:59 与次日 00:00:01 分属相邻窗口（跨午夜归属本地日）
	beforeMidnight := time.Date(2026, 9, 23, 23, 59, 59, 0, cst)
	afterMidnight := time.Date(2026, 9, 24, 0, 0, 1, 0, cst)
	_, beforeEnd := localDayWindow(beforeMidnight, cst)
	afterStart, _ := localDayWindow(afterMidnight, cst)
	if !beforeEnd.Equal(afterStart) {
		t.Errorf("跨午夜窗口不连续: beforeEnd=%s afterStart=%s", beforeEnd, afterStart)
	}
}

func TestDashboardRangeResolve(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 23, 15, 4, 5, 0, cst)

	t.Run("defaults_to_current_month", func(t *testing.T) {
		start, end, err := DashboardRangeQuery{}.resolve(now, cst)
		if err != nil {
			t.Fatalf("resolve 缺省失败: %v", err)
		}
		wantStart := time.Date(2026, 9, 1, 0, 0, 0, 0, cst)
		wantEnd := time.Date(2026, 9, 24, 0, 0, 0, 0, cst)
		if !start.Equal(wantStart) || !end.Equal(wantEnd) {
			t.Errorf("缺省范围 = [%s, %s), want [%s, %s)", start, end, wantStart, wantEnd)
		}
	})

	t.Run("explicit_range_inclusive_end", func(t *testing.T) {
		start, end, err := DashboardRangeQuery{StartDate: "2026-09-20", EndDate: "2026-09-23"}.resolve(now, cst)
		if err != nil {
			t.Fatalf("resolve 显式范围失败: %v", err)
		}
		wantStart := time.Date(2026, 9, 20, 0, 0, 0, 0, cst)
		wantEnd := time.Date(2026, 9, 24, 0, 0, 0, 0, cst) // end_date 含当日
		if !start.Equal(wantStart) || !end.Equal(wantEnd) {
			t.Errorf("显式范围 = [%s, %s), want [%s, %s)", start, end, wantStart, wantEnd)
		}
	})

	invalid := []DashboardRangeQuery{
		{StartDate: "2026-13-01"},                        // 非法月份
		{StartDate: "not-a-date"},                        // 非法格式
		{StartDate: "2026-09-23", EndDate: "2026-09-01"}, // 倒置
		{StartDate: "2000-01-01"},                        // 超长（> 366 天）
	}
	for _, q := range invalid {
		if _, _, err := q.resolve(now, cst); err == nil {
			t.Errorf("resolve(%+v) 未返回错误，want 400 参数错误", q)
		}
	}
}
