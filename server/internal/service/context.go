package service

import "context"

// 本文件定义请求/审计上下文（operator/ip/ua）在 context.Context 中的存取。
//
// 键类型为包私有，避免与其他 context 值冲突；中间件负责写入
// （middleware.RequestContext 注入 ip/ua，JWT 中间件在鉴权成功后注入 operator），
// 记账服务（WriteLog）负责读取。

type contextKey int

const (
	ctxKeyOperatorID contextKey = iota
	ctxKeyRequestMeta
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

// WithRequestMeta 把请求来源信息（ip/ua）写入 context（由请求上下文中间件调用）。
func WithRequestMeta(ctx context.Context, ip, userAgent string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestMeta, RequestMeta{IP: ip, UserAgent: userAgent})
}

// RequestMetaFrom 从 context 读取请求来源信息；缺失时返回零值。
func RequestMetaFrom(ctx context.Context) RequestMeta {
	meta, _ := ctx.Value(ctxKeyRequestMeta).(RequestMeta)
	return meta
}
