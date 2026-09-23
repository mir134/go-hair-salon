package router

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/model"
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
	customerRepo := repository.NewCustomerRepository(db)
	customerSvc := service.NewCustomerService(customerRepo)
	tagSvc := service.NewTagService(repository.NewTagRepository(db), customerRepo)
	customerCtl := controller.NewCustomerController(customerSvc, tagSvc, logSvc)
	tagCtl := controller.NewTagController(tagSvc, logSvc)
	categoryRepo := repository.NewServiceCategoryRepository(db)
	categorySvc := service.NewServiceCategoryService(categoryRepo)
	categoryCtl := controller.NewServiceCategoryController(categorySvc, logSvc)
	itemSvc := service.NewServiceItemService(repository.NewServiceItemRepository(db), categoryRepo)
	itemCtl := controller.NewServiceItemController(itemSvc, logSvc)
	orderRepo := repository.NewOrderRepository(db)
	orderSvc := service.NewOrderService(service.OrderServiceDeps{
		Tx:        repository.NewTransactor(db),
		Orders:    orderRepo,
		Customers: customerRepo,
		Services:  itemSvc,
		Employees: repository.NewEmployeeRepository(db),
		Ledger:    repository.NewLedgerRepository(db),
		Settings:  repository.NewSettingsRepository(db),
		Logs:      logSvc,
	})
	orderCtl := controller.NewOrderController(orderSvc)
	rechargeSvc := service.NewRechargeService(service.RechargeServiceDeps{
		Tx:        repository.NewTransactor(db),
		Recharges: repository.NewRechargeRepository(db),
		Customers: customerRepo,
		Ledger:    repository.NewLedgerRepository(db),
		Logs:      logSvc,
	})
	rechargeCtl := controller.NewRechargeController(rechargeSvc)
	adjustSvc := service.NewBalanceAdjustmentService(service.BalanceAdjustmentDeps{
		Tx:        repository.NewTransactor(db),
		Customers: customerRepo,
		Ledger:    repository.NewLedgerRepository(db),
		Logs:      logSvc,
	})
	adjustCtl := controller.NewBalanceAdjustmentController(adjustSvc)
	detailSvc := service.NewCustomerDetailService(customerRepo,
		orderRepo, repository.NewLedgerRepository(db))
	detailCtl := controller.NewCustomerDetailController(detailSvc)

	api := engine.Group("/api/v1")
	auth := api.Group("/auth")
	auth.POST("/login", authCtl.Login)
	// /auth/me 与 /auth/logout 为 both 分组：admin 与 staff 均可访问。
	authed := auth.Group("",
		middleware.JWTAuth(tokenSvc, userSvc, logger),
		middleware.RequireRole(model.RoleAdmin, model.RoleStaff))
	authed.GET("/me", authCtl.Me)
	authed.POST("/logout", authCtl.Logout)

	// 客户：查询/新增/编辑 both，删除仅 admin（04-API.md:70-95、06 §7）。
	both := api.Group("",
		middleware.JWTAuth(tokenSvc, userSvc, logger),
		middleware.RequireRole(model.RoleAdmin, model.RoleStaff))
	both.GET("/customers", customerCtl.List)
	both.POST("/customers", customerCtl.Create)
	both.GET("/customers/:id", customerCtl.Get)
	both.PUT("/customers/:id", customerCtl.Update)
	// 客户详情聚合：消费记录 / 余额流水 / 积分流水（04-API.md:80-82）。
	both.GET("/customers/:id/orders", detailCtl.ListOrders)
	both.GET("/customers/:id/balance-transactions", detailCtl.ListBalanceTransactions)
	both.GET("/customers/:id/points-transactions", detailCtl.ListPointsTransactions)
	// 标签：查询 both；挂/摘标签属于编辑客户（both）（04-API.md:97-108）。
	both.GET("/tags", tagCtl.List)
	both.POST("/customers/:id/tags", tagCtl.AttachToCustomer)
	both.DELETE("/customers/:id/tags/:tag_id", tagCtl.DetachFromCustomer)
	// 服务分类：查询 both（04-API.md:110-118）。
	both.GET("/service-categories", categoryCtl.List)
	// 服务项目：查询 both，含分类信息（04-API.md:120-125）。
	both.GET("/services", itemCtl.List)
	both.GET("/services/:id", itemCtl.Get)
	// 订单：创建/查询 both（04-API.md:127-135）。
	both.POST("/orders", orderCtl.Create)
	both.GET("/orders", orderCtl.List)
	both.GET("/orders/:id", orderCtl.Get)
	// 挂单明细编辑：both（改价仅 admin 由 service 层强制）（04-API.md:136-138,149）。
	both.POST("/orders/:id/items", orderCtl.AddItem)
	both.PUT("/orders/:id/items/:item_id", orderCtl.UpdateItem)
	both.DELETE("/orders/:id/items/:item_id", orderCtl.RemoveItem)
	// 挂单结账：both（仅 pending 可结账，重复结账 409）（04-API.md:150-151）。
	both.POST("/orders/:id/pay", orderCtl.Pay)
	// 充值：创建/查询 both（04-API.md:159-165）。
	both.POST("/recharges", rechargeCtl.Create)
	both.GET("/recharges", rechargeCtl.List)

	adminOnly := api.Group("",
		middleware.JWTAuth(tokenSvc, userSvc, logger),
		middleware.RequireRole(model.RoleAdmin))
	adminOnly.DELETE("/customers/:id", customerCtl.Delete)
	// 标签创建/修改/删除仅 admin（04-API.md:99）。
	adminOnly.POST("/tags", tagCtl.Create)
	adminOnly.PUT("/tags/:id", tagCtl.Update)
	adminOnly.DELETE("/tags/:id", tagCtl.Delete)
	// 服务分类创建/修改/删除仅 admin（04-API.md:112）。
	adminOnly.POST("/service-categories", categoryCtl.Create)
	adminOnly.PUT("/service-categories/:id", categoryCtl.Update)
	adminOnly.DELETE("/service-categories/:id", categoryCtl.Delete)
	// 服务项目创建/修改/删除仅 admin（04-API.md:112）。
	adminOnly.POST("/services", itemCtl.Create)
	adminOnly.PUT("/services/:id", itemCtl.Update)
	adminOnly.DELETE("/services/:id", itemCtl.Delete)
	// 订单取消仅 admin（04-API.md:140,152-153、06 §7）。
	adminOnly.POST("/orders/:id/cancel", orderCtl.Cancel)
	// 余额调整仅 admin（04-API.md:176-193、06 §7）。
	adminOnly.POST("/customers/:id/balance-adjustments", adjustCtl.Adjust)

	// 后续业务路由的权限分组约定（04-API.md:60-68、06 §7）：
	//   adminOnly 仅 admin；both 为 admin + staff。

	engine.NoRoute(newSPAHandler(logger))
	return engine
}
