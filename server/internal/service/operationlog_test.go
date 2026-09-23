package service_test

// TestWriteOperationLog 是 todo 6 的验收测试（计划：
// `go test ./internal/service -run TestWriteOperationLog`）：
//   - httptest 请求带 X-Forwarded-For / User-Agent → operation_logs 落库含 operator/ip/ua/时间；
//   - 无登录上下文调用 WriteLog → operator_id 为空且不 panic；
//   - 日志只增不删（operation_logs 是审计表，03-DATABASE.md:265-277）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestWriteOperationLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Given: 已迁移的临时数据库 + 记账服务。
	dbPath := filepath.Join(t.TempDir(), "operation-log.db")
	db, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	defer sqlDB.Close()
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	svc := service.NewOperationLogService(repository.NewOperationLogRepository(db))

	// When (happy): 经真实请求上下文中间件 + httptest 请求写入（模拟 JWT 中间件注入 operator=42）。
	var writeErr error
	engine := gin.New()
	engine.Use(middleware.RequestContext())
	engine.POST("/api/v1/orders", func(c *gin.Context) {
		ctx := service.WithOperatorID(c.Request.Context(), 42)
		writeErr = svc.WriteLog(ctx, "order_create", "order", 7, "创建订单")
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader("{}"))
	req.Header.Set("X-Forwarded-For", "10.0.0.9, 10.0.0.1")
	req.Header.Set("User-Agent", "unit-test-ua/1.0")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if writeErr != nil {
		t.Fatalf("WriteLog: %v", writeErr)
	}

	// Then: 落库字段完整（operator/ip/ua/时间）。
	var row model.OperationLog
	if err := db.Where("action = ?", "order_create").First(&row).Error; err != nil {
		t.Fatalf("查询 operation_logs: %v", err)
	}
	if row.OperatorID == nil || *row.OperatorID != 42 {
		t.Errorf("operator_id = %v, want 42", row.OperatorID)
	}
	if row.IP != "10.0.0.9" {
		t.Errorf("ip = %q, want 10.0.0.9（X-Forwarded-For 首个地址）", row.IP)
	}
	if row.UserAgent != "unit-test-ua/1.0" {
		t.Errorf("user_agent = %q, want unit-test-ua/1.0", row.UserAgent)
	}
	if row.TargetType != "order" || row.TargetID != 7 || row.Content != "创建订单" {
		t.Errorf("target/content = %s/%d/%q, want order/7/创建订单", row.TargetType, row.TargetID, row.Content)
	}
	if row.CreatedAt.IsZero() {
		t.Error("created_at 为空")
	}
	if delta := time.Since(row.CreatedAt); delta < -time.Minute || delta > time.Minute {
		t.Errorf("created_at = %v 与当前时间偏差过大（%v）", row.CreatedAt, delta)
	}

	// When (failure): 无登录上下文（未经过 JWT 中间件）调用 WriteLog。
	if err := svc.WriteLog(context.Background(), "login_failed", "user", 0, "未知用户尝试登录"); err != nil {
		t.Fatalf("无上下文 WriteLog 不应 panic/报错: %v", err)
	}

	// Then: operator_id 为空（NULL）、ip/ua 为空字符串。
	var anonymous model.OperationLog
	if err := db.Where("action = ?", "login_failed").First(&anonymous).Error; err != nil {
		t.Fatalf("查询匿名日志: %v", err)
	}
	if anonymous.OperatorID != nil {
		t.Errorf("无登录上下文 operator_id = %v, want NULL", *anonymous.OperatorID)
	}
	if anonymous.IP != "" || anonymous.UserAgent != "" {
		t.Errorf("无请求上下文 ip/ua = %q/%q, want 空", anonymous.IP, anonymous.UserAgent)
	}

	// Then: 两条日志都在（只增不删、互不覆盖）。
	var count int64
	if err := db.Model(&model.OperationLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count operation_logs: %v", err)
	}
	if count != 2 {
		t.Errorf("operation_logs 行数 = %d, want 2", count)
	}
}
