package service

// 维护模式标志（todo 52；08-DEPLOYMENT.md:78「执行恢复前：停止业务写入」）。
//
// 恢复流程（RestoreService）在替换数据库前后置位本标志，使所有业务写请求被
// middleware.Maintenance 以 503 + 50300 拒绝；读请求放行，恢复完成后（无论成败）
// 由 defer 保证清除。
//
// 并发：写少读多，Enter/Exit 仅在恢复请求内发生，业务请求只读 Active/Message。

import (
	"strings"
	"sync"
)

// maintenanceReasonDefault 是无自定义原因时的维护说明（面向用户，必须可读）。
const maintenanceReasonDefault = "数据恢复进行中"

// MaintenanceGuard 是进程级维护模式标志。
//
// 零值可用；构造请用 NewMaintenanceGuard。nil 接收者视为「未启用维护模式」，
// 便于 router 在测试装配中省略该依赖。
type MaintenanceGuard struct {
	mu     sync.RWMutex
	active bool
	reason string
}

// NewMaintenanceGuard 构造维护模式标志（初始为关闭）。
func NewMaintenanceGuard() *MaintenanceGuard {
	return &MaintenanceGuard{}
}

// Enter 打开维护模式；reason 为空时使用默认说明。
func (g *MaintenanceGuard) Enter(reason string) {
	if g == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = maintenanceReasonDefault
	}
	g.mu.Lock()
	g.active = true
	g.reason = reason
	g.mu.Unlock()
}

// Exit 关闭维护模式（可重复调用）。
func (g *MaintenanceGuard) Exit() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.active = false
	g.reason = ""
	g.mu.Unlock()
}

// Active 返回当前是否处于维护模式。
func (g *MaintenanceGuard) Active() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.active
}

// Message 返回面向用户的 503 文案（恒含「系统维护中」，前端据此提示）。
func (g *MaintenanceGuard) Message() string {
	reason := ""
	if g != nil {
		g.mu.RLock()
		reason = g.reason
		g.mu.RUnlock()
	}
	if reason == "" {
		reason = maintenanceReasonDefault
	}
	return "系统维护中：" + reason + "，请稍后重试"
}
