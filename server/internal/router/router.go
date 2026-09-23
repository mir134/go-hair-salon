package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
)

// New 装配 gin 引擎：panic recovery → 请求上下文（ip/ua 审计）→ 请求日志 → 路由。
//
// 当前仅注册 GET /health（免认证）；/api/v1 业务路由与静态托管由后续任务接入。
func New(db *gorm.DB, logger *slog.Logger) *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.RequestContext())
	engine.Use(middleware.RequestLogger(logger))
	engine.GET("/health", controller.Health(db, logger))
	return engine
}
