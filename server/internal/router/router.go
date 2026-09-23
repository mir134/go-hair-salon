package router

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// Options 是路由装配的运行参数（由 main 从 config 注入）。
type Options struct {
	// JWTSecret 是 JWT 签名密钥（config.JWT_SECRET）。
	JWTSecret string
	// TokenTTL 是 JWT 有效期；0 表示默认 24h。
	TokenTTL time.Duration
}

// New 装配 gin 引擎：panic recovery → 请求上下文（ip/ua 审计）→ 请求日志 → 路由与静态兜底。
//
// 已注册路由：
//   - GET /health（免认证）；
//   - POST /api/v1/auth/login（免认证）；
//   - GET /api/v1/auth/me、POST /api/v1/auth/logout（JWT 认证）。
//
// 未命中路由交给 NoRoute 兜底（static.go）：
//   - /api 未命中 → 统一 JSON 404 信封（不得回退 HTML）；
//   - 其余路径 → web/dist 静态文件，未命中回退 index.html（SPA history 路由）。
func New(db *gorm.DB, logger *slog.Logger, opts Options) *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.RequestContext())
	engine.Use(middleware.RequestLogger(logger))
	engine.GET("/health", controller.Health(db, logger))

	// 依赖装配：controller → service → repository（02-AGENTS.md:15-26）。
	userSvc := service.NewUserService(repository.NewUserRepository(db))
	logSvc := service.NewOperationLogService(repository.NewOperationLogRepository(db))
	tokenSvc := service.NewTokenService(opts.JWTSecret, opts.TokenTTL)
	authSvc := service.NewAuthService(userSvc, tokenSvc, logSvc)
	authCtl := controller.NewAuthController(authSvc, logSvc)

	api := engine.Group("/api/v1")
	auth := api.Group("/auth")
	auth.POST("/login", authCtl.Login)
	authed := auth.Group("", middleware.JWTAuth(tokenSvc, userSvc, logger))
	authed.GET("/me", authCtl.Me)
	authed.POST("/logout", authCtl.Logout)

	engine.NoRoute(newSPAHandler(logger))
	return engine
}
