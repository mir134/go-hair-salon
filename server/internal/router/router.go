package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
)

// New 装配 gin 引擎：panic recovery → 请求上下文（ip/ua 审计）→ 请求日志 → 路由与静态兜底。
//
// 当前仅注册 GET /health（免认证）；/api/v1 业务路由由后续任务接入。
// 未命中路由交给 NoRoute 兜底（static.go）：
//   - /api 未命中 → 统一 JSON 404 信封（不得回退 HTML）；
//   - 其余路径 → web/dist 静态文件，未命中回退 index.html（SPA history 路由）。
func New(db *gorm.DB, logger *slog.Logger) *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.RequestContext())
	engine.Use(middleware.RequestLogger(logger))
	engine.GET("/health", controller.Health(db, logger))
	engine.NoRoute(newSPAHandler(logger))
	return engine
}
