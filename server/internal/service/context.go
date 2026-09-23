package service

import (
	"context"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 本文件定义请求/审计上下文（operator/ip/ua/当前用户）在 context.Context 中的存取。
//
// 键类型为包私有，避免与其他 context 值冲突；中间件负责写入
// （middleware.RequestContext 注入 ip/ua，JWT 中间件在鉴权成功后注入 operator 与当前用户），
// 记账服务（WriteLog）与 controller 负责读取。

type contextKey int

const (
	ctxKeyOperatorID contextKey = iota
	ctxKeyRequestMeta
	ctxKeyCurrentUser
)

// RequestMeta 是从 HTTP 请求提取的审计信息。
type RequestMeta struct {
	IP        string
	UserAgent string
}

// WithOperatorID 把登录用户 id 写入 context（由 JWT 中间件在鉴权成功后调用）。
func WithOperatorID(ctx context.Context, operatorID int64) context.Context {
	return context.WithValue(ctx, ctxKeyOperatorID, operatorID)
}

// OperatorID 从 context 读取登录用户 id；无登录上下文时返回 ok=false。
func OperatorID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKeyOperatorID).(int64)
	return id, ok
}

// OperatorIDPtr 返回当前登录用户 id 的指针（无登录上下文时返回 nil），
// 供可空外键列（balance_transactions.operator_id、points_transactions.operator_id 等）使用。
func OperatorIDPtr(ctx context.Context) *int64 {
	id, ok := OperatorID(ctx)
	if !ok {
		return nil
	}
	return &id
}

// WithRequestMeta 把请求来源信息（ip/ua）写入 context（由请求上下文中间件调用）。
func WithRequestMeta(ctx context.Context, ip, userAgent string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestMeta, RequestMeta{IP: ip, UserAgent: userAgent})
}

// RequestMetaFrom 从 context 读取请求来源信息；缺失时返回零值。
func RequestMetaFrom(ctx context.Context) RequestMeta {
	meta, _ := ctx.Value(ctxKeyRequestMeta).(RequestMeta)
	return meta
}

// WithCurrentUser 把当前登录用户写入 context（由 JWT 中间件在鉴权成功后调用）。
//
// 该用户对象是本次请求从数据库实时加载的结果（status 已校验），
// 权限判定必须使用它而不是 token 中的角色声明。
func WithCurrentUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, ctxKeyCurrentUser, user)
}

// CurrentUser 从 context 读取当前登录用户；无登录上下文时返回 ok=false。
func CurrentUser(ctx context.Context) (*model.User, bool) {
	user, ok := ctx.Value(ctxKeyCurrentUser).(*model.User)
	return user, ok
}
