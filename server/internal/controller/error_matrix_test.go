package controller_test

// TestErrorMatrix 是 todo 58 的验收测试（计划：`go test ./internal/controller -run TestErrorMatrix -v -count=1`）：
// 关键接口的「HTTP 状态码 × 业务 code」错误矩阵（04-API.md:265-275），每类状态至少 1 个用例：
//
//	400 参数错误（非法 id / malformed JSON / 缺 reason）
//	401 未登录（无 token / 乱码 token）
//	403 越权（staff 访问 admin 路由）
//	404 不存在（客户 / 订单）
//	409 幂等与重复状态迁移（同 request_id 幂等返回原单；重复结账；二次退款）
//	422 业务校验（余额不足 / 负余额 / 充值冲正致负余额）
//	500 panic 信封（无 stack；服务端日志留痕）
//
// 每个用例同时断言 HTTP status 与 envelope.code，并断言失败信封 data=null。
// 全部请求走真实路由 + 真实迁移库（httptest，不启动服务器、不 mock）。
//
// 口径说明：任务书将「缺 reason」归入 422，但 04-API.md:265-275 与 plan todo 26/32 的
// 验收口径均为 400（参数错误）——本矩阵按文档/计划断言 400。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// requireErrorEnvelope 断言失败响应三件套：HTTP status + 业务 code + data=null。
func requireErrorEnvelope(t *testing.T, label string, w *httptest.ResponseRecorder, wantStatus, wantCode int) envelope {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("%s: HTTP status = %d, want %d (body=%s)", label, w.Code, wantStatus, w.Body.String())
	}
	env := decodeEnvelope(t, w)
	if env.Code != wantCode {
		t.Errorf("%s: envelope.code = %d, want %d (body=%s)", label, env.Code, wantCode, w.Body.String())
	}
	if string(env.Data) != "null" {
		t.Errorf("%s: 失败信封 data = %s, want null", label, env.Data)
	}
	return env
}

func TestErrorMatrix(t *testing.T) {
	env := newCustomerEnv(t)

	// 500 探针：挂在真实引擎上（Recovery/RequestContext/RequestLogger 中间件生效），
	// 路由仅测试期注册，不改变生产路由表。
	env.engine.GET("/api/v1/__error_matrix_panic", func(*gin.Context) {
		panic("error-matrix-panic-marker")
	})

	t.Run("400 invalid params", func(t *testing.T) {
		// 非法路径参数：id 非数字
		w := env.authed(http.MethodGet, "/api/v1/customers/abc", "", env.adminToken)
		requireErrorEnvelope(t, "GET /customers/abc", w, http.StatusBadRequest, service.CodeInvalidParams)

		// malformed JSON 请求体
		w = env.authed(http.MethodPost, "/api/v1/orders", `{"request_id":`, env.staffToken)
		requireErrorEnvelope(t, "POST /orders malformed JSON", w, http.StatusBadRequest, service.CodeInvalidParams)

		customer := env.createCustomer(t, env.staffToken, `{"name":"错误矩阵客户甲","phone":"13800007001"}`)

		// 余额调整缺 reason（04-API.md:176-193、plan todo 32「缺 reason→400」）
		w = env.adjustBalance(env.adminToken, customer.ID, `{"amount_cents":100}`)
		envl := requireErrorEnvelope(t, "POST balance-adjustments 缺 reason", w, http.StatusBadRequest, service.CodeInvalidParams)
		if !strings.Contains(envl.Message, "原因") {
			t.Errorf("缺 reason message = %q, want 含「原因」", envl.Message)
		}

		// 改价缺 reason（仅 admin 可改价且原因必填，06 §3.1、plan todo 26「缺 reason→400」）
		hair := env.seedServiceViaAPI(t, "错误矩阵剪发", 5000)
		pending := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-err-matrix-400", customer.ID, orderItemJSON(hair.ID, 1, 5000)))
		if len(pending.Items) != 1 {
			t.Fatalf("挂单明细数 = %d, want 1", len(pending.Items))
		}
		w = env.updateOrderItem(env.adminToken, pending.ID, pending.Items[0].ID, `{"unit_price_cents":4000}`)
		envl = requireErrorEnvelope(t, "PUT order item 改价缺 reason", w, http.StatusBadRequest, service.CodeInvalidParams)
		if !strings.Contains(envl.Message, "原因") {
			t.Errorf("改价缺 reason message = %q, want 含「原因」", envl.Message)
		}
	})

	t.Run("401 unauthorized", func(t *testing.T) {
		// 无 Authorization
		w := env.do(http.MethodGet, "/api/v1/customers", "", nil)
		requireErrorEnvelope(t, "无 token GET /customers", w, http.StatusUnauthorized, service.CodeUnauthorized)

		// 乱码 token
		w = env.authed(http.MethodGet, "/api/v1/customers", "", "not-a-jwt")
		requireErrorEnvelope(t, "乱码 token GET /customers", w, http.StatusUnauthorized, service.CodeUnauthorized)

		// 未登录访问 admin 路由：认证先于授权 → 401（而非 403）
		w = env.do(http.MethodDelete, "/api/v1/customers/1", "", nil)
		requireErrorEnvelope(t, "无 token DELETE /customers/1", w, http.StatusUnauthorized, service.CodeUnauthorized)
	})

	t.Run("403 staff on admin routes", func(t *testing.T) {
		customer := env.createCustomer(t, env.staffToken, `{"name":"错误矩阵客户乙","phone":"13800007002"}`)
		adminRoutes := []struct {
			label  string
			method string
			path   string
			body   string
		}{
			{"DELETE /customers/:id（删客户）", http.MethodDelete, fmt.Sprintf("/api/v1/customers/%d", customer.ID), ""},
			{"POST /orders/:id/refund（退款）", http.MethodPost, "/api/v1/orders/1/refund", ""},
			{"POST /orders/:id/cancel（取消）", http.MethodPost, "/api/v1/orders/1/cancel", ""},
			{"POST /customers/:id/balance-adjustments（余额调整）", http.MethodPost,
				fmt.Sprintf("/api/v1/customers/%d/balance-adjustments", customer.ID), `{"amount_cents":100,"reason":"x"}`},
			{"POST /recharges/:id/refund（充值冲正）", http.MethodPost, "/api/v1/recharges/1/refund", ""},
			{"PUT /settings/:key（设置修改）", http.MethodPut, "/api/v1/settings/points_per_yuan", `{"value":"2"}`},
			{"GET /operation-logs（操作日志）", http.MethodGet, "/api/v1/operation-logs", ""},
			{"GET /employees（员工管理）", http.MethodGet, "/api/v1/employees", ""},
			{"POST /users（用户管理）", http.MethodPost, "/api/v1/users", `{"username":"x","password":"Passw0rd!","role":"staff"}`},
		}
		// 说明：/backups 与 /backups/:id/restore 仅在装配 BackupService 时注册
		//（router.Options.Backups != nil）；本环境未装配，其 403/权限覆盖见 todo 59
		// TestPermissionMatrix（使用完整路由）。
		for _, tc := range adminRoutes {
			w := env.authed(tc.method, tc.path, tc.body, env.staffToken)
			requireErrorEnvelope(t, "staff "+tc.label, w, http.StatusForbidden, service.CodeForbidden)
		}

		// 正对照：admin 访问同一 admin 路由不被 401/403（证明 403 来自 RBAC，而非路由整体不可用）
		w := env.authed(http.MethodGet, "/api/v1/operation-logs", "", env.adminToken)
		if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Errorf("admin GET /operation-logs status = %d, want 非 401/403 (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("404 missing resources", func(t *testing.T) {
		w := env.authed(http.MethodGet, "/api/v1/customers/999999", "", env.adminToken)
		envl := requireErrorEnvelope(t, "GET /customers/999999", w, http.StatusNotFound, service.CodeCustomerNotFound)
		if !strings.Contains(envl.Message, "客户不存在") {
			t.Errorf("客户 404 message = %q, want 含「客户不存在」", envl.Message)
		}

		w = env.authed(http.MethodGet, "/api/v1/orders/999999", "", env.adminToken)
		requireErrorEnvelope(t, "GET /orders/999999", w, http.StatusNotFound, service.CodeNotFound)

		w = env.authed(http.MethodPost, "/api/v1/orders/999999/refund", "", env.adminToken)
		requireErrorEnvelope(t, "POST /orders/999999/refund", w, http.StatusNotFound, service.CodeNotFound)
	})

	t.Run("409 idempotency and duplicate state transitions", func(t *testing.T) {
		customer := env.createCustomer(t, env.staffToken, `{"name":"错误矩阵客户丙","phone":"13800007003"}`)
		hair := env.seedServiceViaAPI(t, "错误矩阵烫发", 3000)

		// 幂等：同一 request_id 重复提交 → 首次 201、二次 200 返回原订单（04-API.md:277-299），
		// 且账本表零新增（不重复入账；真重复入账的唯一索引兜底在 service 测试覆盖）。
		body := orderCreateJSON("req-err-matrix-idem", customer.ID, nil, "cash", orderItemJSON(hair.ID, 1, 3000))
		first := env.postOrder(t, env.staffToken, body)
		before := env.ledgerRowCounts(t)
		second := env.authed(http.MethodPost, "/api/v1/orders", body, env.staffToken)
		if second.Code != http.StatusOK {
			t.Fatalf("幂等重复提交 status = %d, want 200 (body=%s)", second.Code, second.Body.String())
		}
		repeated := decodeOrderDetail(t, second)
		if repeated.ID != first.ID || repeated.OrderNo != first.OrderNo {
			t.Errorf("幂等重复提交返回订单 = %d/%s, want 原订单 %d/%s", repeated.ID, repeated.OrderNo, first.ID, first.OrderNo)
		}
		after := env.ledgerRowCounts(t)
		for name, count := range before {
			if after[name] != count {
				t.Errorf("幂等重复提交后 %s 行数 = %d, want %d（不重复入账）", name, after[name], count)
			}
		}

		// 重复结账：pending → 结账 200 → 二次结账 409（04-API.md:151）。
		pending := env.postPendingOrder(t, env.staffToken,
			pendingOrderJSON("req-err-matrix-pay", customer.ID, orderItemJSON(hair.ID, 1, 3000)))
		w := env.payOrder(env.staffToken, pending.ID, `{"payment_method":"cash"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("首次结账 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		w = env.payOrder(env.staffToken, pending.ID, `{"payment_method":"cash"}`)
		envl := requireErrorEnvelope(t, "重复结账", w, http.StatusConflict, service.CodeConflict)
		if !strings.Contains(envl.Message, "重复结账") {
			t.Errorf("重复结账 message = %q, want 含「重复结账」", envl.Message)
		}

		// 二次退款：completed → 退款 200 → 二次退款 409（04-API.md:155）。
		env.seedBalance(t, customer.ID, 5000)
		balanceOrder := env.postOrder(t, env.staffToken,
			orderCreateJSON("req-err-matrix-refund", customer.ID, nil, "balance", orderItemJSON(hair.ID, 1, 3000)))
		w = env.refundOrder(env.adminToken, balanceOrder.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("首次退款 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		w = env.refundOrder(env.adminToken, balanceOrder.ID)
		requireErrorEnvelope(t, "二次退款", w, http.StatusConflict, service.CodeConflict)
	})

	t.Run("422 business validation", func(t *testing.T) {
		hair := env.seedServiceViaAPI(t, "错误矩阵染发", 2000)

		// 余额不足：余额 0 的客户余额支付 → 422 且账本零写入。
		poor := env.createCustomer(t, env.staffToken, `{"name":"错误矩阵客户丁","phone":"13800007004"}`)
		before := env.ledgerRowCounts(t)
		w := env.authed(http.MethodPost, "/api/v1/orders",
			orderCreateJSON("req-err-matrix-poor", poor.ID, nil, "balance", orderItemJSON(hair.ID, 1, 2000)), env.staffToken)
		envl := requireErrorEnvelope(t, "余额不足下单", w, http.StatusUnprocessableEntity, service.CodeValidationFailed)
		if !strings.Contains(envl.Message, "余额不足") {
			t.Errorf("余额不足 message = %q, want 含「余额不足」", envl.Message)
		}
		after := env.ledgerRowCounts(t)
		for name, count := range before {
			if after[name] != count {
				t.Errorf("余额不足失败后 %s 行数 = %d, want %d（零写入）", name, after[name], count)
			}
		}

		// 负余额：admin 调整 -1 分（余额 0）→ 422（原子条件更新，04-API.md:184-192）。
		w = env.adjustBalance(env.adminToken, poor.ID, `{"amount_cents":-1,"reason":"错误矩阵负余额"}`)
		envl = requireErrorEnvelope(t, "调整致负余额", w, http.StatusUnprocessableEntity, service.CodeValidationFailed)
		if !strings.Contains(envl.Message, "不能为负") {
			t.Errorf("负余额 message = %q, want 含「不能为负」", envl.Message)
		}

		// 充值冲正致负余额：充值 2000 → 余额消费 2000 → 冲正 → 422（06 §5:62）。
		reversal := env.createCustomer(t, env.staffToken, `{"name":"错误矩阵客户戊","phone":"13800007005"}`)
		w = env.postRecharge(env.staffToken, rechargeJSON("req-err-matrix-recharge", reversal.ID, 2000, 0, "cash"))
		if w.Code != http.StatusCreated {
			t.Fatalf("充值 status = %d, want 201 (body=%s)", w.Code, w.Body.String())
		}
		record := decodeRecharge(t, w)
		_ = env.postOrder(t, env.staffToken,
			orderCreateJSON("req-err-matrix-spend", reversal.ID, nil, "balance", orderItemJSON(hair.ID, 1, 2000)))
		w = env.refundRecharge(env.adminToken, record.ID)
		envl = requireErrorEnvelope(t, "余额不足冲正", w, http.StatusUnprocessableEntity, service.CodeValidationFailed)
		if !strings.Contains(envl.Message, "余额不足") {
			t.Errorf("冲正 422 message = %q, want 含「余额不足」", envl.Message)
		}
	})

	t.Run("500 panic envelope without stack", func(t *testing.T) {
		w := env.do(http.MethodGet, "/api/v1/__error_matrix_panic", "", nil)
		envl := requireErrorEnvelope(t, "panic 探针", w, http.StatusInternalServerError, service.CodeInternal)
		if envl.Message != "服务器内部错误" {
			t.Errorf("panic 信封 message = %q, want 服务器内部错误", envl.Message)
		}
		body := w.Body.String()
		for _, leaked := range []string{"goroutine", "runtime/debug", "error-matrix-panic-marker", "panic(", "middleware/recovery"} {
			if strings.Contains(body, leaked) {
				t.Errorf("panic 响应泄漏内部信息 %q: %s", leaked, body)
			}
		}

		// 正对照：panic 值 + 堆栈必须写入服务端日志（08-DEPLOYMENT.md:90 记录 HTTP 500）。
		logs := env.logBuf.String()
		if !strings.Contains(logs, "panic recovered") || !strings.Contains(logs, "error-matrix-panic-marker") {
			t.Errorf("panic 未写入服务端日志（期望含 panic recovered 与 panic 值）: %s", logs)
		}
	})
}
