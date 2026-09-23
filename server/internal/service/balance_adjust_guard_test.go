package service_test

// TestAdjustGuard 是 todo 34 的验收测试之二（计划：`go test ./internal/service -run 'TestRechargeLedger|TestAdjustGuard' -count=3`）：
//   - staff → 403：探测引擎挂载的是路由实际使用的同一守卫
//     （middleware.RequireRole(model.RoleAdmin)）+ 真实 controller + 真实 service；
//     staff 被拒且零写入，admin 通过同一守卫并正确入账（正向对照，证明 403 来自角色而非守卫恒拒）；
//   - reason 必填：空/空白 → 400；金额 0 → 400；客户不存在 → 404，全部零写入；
//   - 负向调整致负余额 → 422/42200 且全部表零写入（原子条件更新，03-DATABASE.md:206-216、06 §5:62-65）；
//     恰好扣到 0 是允许边界；
//   - 正/负调整更新余额 + balance_transactions(type=adjustment, before/after 连续) + operation_logs。
//
// 路由装配（adminOnly.POST("/customers/:id/balance-adjustments", ...)）已由 todo 32 的
// controller 测试端到端覆盖；本文件的探测引擎验证同一守卫对同一 handler 的放行/拦截语义。
// 本文件只新增测试，生产代码零改动。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/middleware"
	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// probeEnvelope 是最小响应信封（断言 403/400 的业务错误码）。
type probeEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// newAdjustProbeEngine 组装最小探测引擎：身份注入 → 真实 RBAC 守卫 → 真实 controller。
func newAdjustProbeEngine(adjust *service.BalanceAdjustmentService, user *model.User) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/customers/:id/balance-adjustments",
		func(c *gin.Context) {
			ctx := service.WithOperatorID(c.Request.Context(), user.ID)
			ctx = service.WithCurrentUser(ctx, user)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		},
		middleware.RequireRole(model.RoleAdmin),
		controller.NewBalanceAdjustmentController(adjust).Adjust,
	)
	return engine
}

// probeAdjust 向探测引擎发起余额调整请求。
func probeAdjust(engine *gin.Engine, customerID int64, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/customers/%d/balance-adjustments", customerID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// decodeProbeEnvelope 解析探测响应的信封。
func decodeProbeEnvelope(t *testing.T, w *httptest.ResponseRecorder) probeEnvelope {
	t.Helper()
	var envl probeEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envl); err != nil {
		t.Fatalf("解析响应信封失败: %v (body=%s)", err, w.Body.String())
	}
	return envl
}

func TestAdjustGuard(t *testing.T) {
	t.Run("staff is forbidden by the admin-only guard with zero writes", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "守卫客户", 10000)
		staff := &model.User{ID: 2, Username: "staff", Role: model.RoleStaff, Status: model.StatusEnabled}
		admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusEnabled}

		// --- When: staff 走真实守卫（middleware.RequireRole(model.RoleAdmin)）+ 真实 handler ---
		w := probeAdjust(newAdjustProbeEngine(env.adjust, staff), customer.ID, `{"amount_cents":5000,"reason":"越权调整"}`)

		// --- Then: 403/40300 + 全表零写入 + 余额不变 ---
		if w.Code != http.StatusForbidden {
			t.Fatalf("staff 调整余额 status = %d, want 403 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeProbeEnvelope(t, w); envl.Code != service.CodeForbidden {
			t.Errorf("staff 调整余额 code = %d, want %d", envl.Code, service.CodeForbidden)
		}
		for name, entity := range ledgerEntityTables() {
			if n := env.countRows(t, entity); n != 0 {
				t.Errorf("staff 403 后 %s 行数 = %d, want 0（零写入）", name, n)
			}
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 10000 {
			t.Errorf("staff 403 后余额 = %d, want 10000", got)
		}

		// --- When: admin 走同一守卫（正向对照：403 来自角色，而非守卫恒拒） ---
		w = probeAdjust(newAdjustProbeEngine(env.adjust, admin), customer.ID, `{"amount_cents":5000,"reason":"守卫放行验证"}`)

		// --- Then: 200 + 余额 15000 + 1 条 adjustment 流水 + 1 条审计 ---
		if w.Code != http.StatusOK {
			t.Fatalf("admin 调整余额 status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		if envl := decodeProbeEnvelope(t, w); envl.Code != service.CodeOK {
			t.Errorf("admin 调整余额 code = %d, want %d", envl.Code, service.CodeOK)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 15000 {
			t.Errorf("admin 调整后余额 = %d, want 15000", got)
		}
		rows := env.balanceRows(t, customer.ID)
		if len(rows) != 1 || rows[0].Type != model.BalanceTxAdjustment || rows[0].AmountCents != 5000 ||
			rows[0].BalanceBeforeCents != 10000 || rows[0].BalanceAfterCents != 15000 {
			t.Errorf("admin 调整流水 = %+v, want 单条 adjustment/+5000/10000→15000", rows)
		}
		var logCount int64
		if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "balance_adjust").Count(&logCount).Error; err != nil {
			t.Fatalf("统计 balance_adjust 日志失败: %v", err)
		}
		if logCount != 1 {
			t.Errorf("balance_adjust 日志行数 = %d, want 1", logCount)
		}
	})

	t.Run("missing or blank reason and invalid input are rejected with zero writes", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "原因校验客户", 10000)
		before := env.customerAfter(t, customer.ID)
		counts := snapshotLedgerTables(t, env)

		cases := []struct {
			name       string
			customerID int64
			amount     int64
			reason     string
			wantStatus int
			wantCode   int
		}{
			{"原因为空", customer.ID, 5000, "", http.StatusBadRequest, service.CodeInvalidParams},
			{"原因为空白", customer.ID, 5000, "   ", http.StatusBadRequest, service.CodeInvalidParams},
			{"金额为 0", customer.ID, 0, "零调整", http.StatusBadRequest, service.CodeInvalidParams},
			{"客户不存在", 999999, 5000, "补偿", http.StatusNotFound, service.CodeCustomerNotFound},
		}
		for _, tc := range cases {
			// --- When/Then: 每类非法输入返回对应业务错误 ---
			_, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
				CustomerID: tc.customerID, AmountCents: tc.amount, Reason: tc.reason})
			biz := requireBizError(t, err, tc.wantStatus)
			if biz.Code != tc.wantCode {
				t.Errorf("%s code = %d, want %d", tc.name, biz.Code, tc.wantCode)
			}
		}

		// --- Then: 全部失败路径零写入、余额与整行未变 ---
		requireLedgerTablesUnchanged(t, env, "非法调整", counts)
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 10000 {
			t.Errorf("非法调整后余额 = %d, want 10000", after.BalanceCents)
		}
		if !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("非法调整后 updated_at = %v, want %v（整行未被触碰）", after.UpdatedAt, before.UpdatedAt)
		}
	})

	t.Run("negative adjustment beyond balance is 422 with zero writes on every table", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "负余额防护客户", 10000)
		before := env.customerAfter(t, customer.ID)
		counts := snapshotLedgerTables(t, env)

		// --- When: 余额 10000，调整 -10001（恰好超过 1 分） ---
		_, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
			CustomerID: customer.ID, AmountCents: -10001, Reason: "超额扣减"})

		// --- Then: 422/42200 含「不能为负」 ---
		biz := requireBizError(t, err, http.StatusUnprocessableEntity)
		if biz.Code != service.CodeValidationFailed || !strings.Contains(biz.Message, "不能为负") {
			t.Errorf("致负调整 BizError = code %d msg %q, want 42200 含「不能为负」", biz.Code, biz.Message)
		}

		// --- Then: 全部表零写入、余额与整行未变（原子条件更新未生效 → 事务整体回滚） ---
		requireLedgerTablesUnchanged(t, env, "致负调整", counts)
		after := env.customerAfter(t, customer.ID)
		if after.BalanceCents != 10000 {
			t.Errorf("致负调整被拒后余额 = %d, want 10000", after.BalanceCents)
		}
		if !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("致负调整被拒后 updated_at = %v, want %v（整行未被触碰）", after.UpdatedAt, before.UpdatedAt)
		}

		// --- When: 恰好扣到 0（-10000，允许边界） ---
		if _, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
			CustomerID: customer.ID, AmountCents: -10000, Reason: "清空余额"}); err != nil {
			t.Fatalf("恰好清空余额: %v", err)
		}

		// --- Then: 余额 0，流水链尾 == 余额，永不为负 ---
		// 客户余额由测试直接播种 10000（无历史流水），故链首 before 期望 = 10000。
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("清空后余额 = %d, want 0", got)
		}
		env.requireChainContinuous(t, customer.ID, 10000)

		// --- When: 余额 0 再扣 1 分 ---
		_, err = env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
			CustomerID: customer.ID, AmountCents: -1, Reason: "再扣"})

		// --- Then: 422 且余额仍为 0 ---
		requireBizError(t, err, http.StatusUnprocessableEntity)
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 0 {
			t.Errorf("零余额再扣被拒后余额 = %d, want 0", got)
		}
	})

	t.Run("positive and negative adjustments update balance ledger and audit log", func(t *testing.T) {
		env := newLedgerTestEnv(t)
		customer := env.seedCustomer(t, "调整客户", 0)

		// --- Given: 通过真实充值服务建余额 10000（本金 10000、无赠送） ---
		if _, err := env.recharges.CreateRecharge(adminCtx(),
			rechargeInput("req-adjust-seed", customer.ID, 10000, 0, model.PaymentMethodCash)); err != nil {
			t.Fatalf("充值建余额: %v", err)
		}

		// --- When: admin 正向调整 +5000 ---
		added, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
			CustomerID: customer.ID, AmountCents: 5000, Reason: "活动补偿"})
		if err != nil {
			t.Fatalf("Adjust(+5000): %v", err)
		}

		// --- Then: 流水 type=adjustment、金额带符号、before/after 连续、operator=1、原因入 remark ---
		if added.Transaction.Type != model.BalanceTxAdjustment || added.Transaction.AmountCents != 5000 ||
			added.Transaction.BalanceBeforeCents != 10000 || added.Transaction.BalanceAfterCents != 15000 ||
			added.Transaction.Remark != "活动补偿" {
			t.Errorf("正向调整流水 = %+v, want adjustment/+5000/10000→15000/活动补偿", added.Transaction)
		}
		if added.Transaction.OperatorID == nil || *added.Transaction.OperatorID != 1 {
			t.Errorf("正向调整 operator_id = %v, want 1", added.Transaction.OperatorID)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 15000 {
			t.Errorf("正向调整后余额 = %d, want 15000", got)
		}

		// --- When: admin 负向调整 -3000 ---
		deducted, err := env.adjust.Adjust(adminCtx(), service.BalanceAdjustmentInput{
			CustomerID: customer.ID, AmountCents: -3000, Reason: "修正误差"})
		if err != nil {
			t.Fatalf("Adjust(-3000): %v", err)
		}
		if deducted.Transaction.AmountCents != -3000 ||
			deducted.Transaction.BalanceBeforeCents != 15000 || deducted.Transaction.BalanceAfterCents != 12000 {
			t.Errorf("负向调整流水 = %+v, want adjustment/-3000/15000→12000", deducted.Transaction)
		}
		if got := env.customerAfter(t, customer.ID).BalanceCents; got != 12000 {
			t.Errorf("负向调整后余额 = %d, want 12000", got)
		}

		// --- Then: 流水链连续（充值 1 + 调整 2），链尾 == 余额 12000 ---
		rows := env.balanceRows(t, customer.ID)
		if len(rows) != 3 {
			t.Fatalf("余额流水条数 = %d, want 3（充值 1 + 调整 2）", len(rows))
		}
		if rows[1].Type != model.BalanceTxAdjustment || rows[1].AmountCents != 5000 ||
			rows[1].BalanceBeforeCents != 10000 || rows[1].BalanceAfterCents != 15000 {
			t.Errorf("正向调整流水[1] = %+v, want adjustment/+5000/10000→15000", rows[1])
		}
		if rows[2].Type != model.BalanceTxAdjustment || rows[2].AmountCents != -3000 ||
			rows[2].BalanceBeforeCents != 15000 || rows[2].BalanceAfterCents != 12000 {
			t.Errorf("负向调整流水[2] = %+v, want adjustment/-3000/15000→12000", rows[2])
		}
		env.requireChainContinuous(t, customer.ID, 0)

		// --- Then: operation_logs(action=balance_adjust) 每次一条，含原因与 operator ---
		var logs []model.OperationLog
		if err := env.db.Where("action = ?", "balance_adjust").Order("id ASC").Find(&logs).Error; err != nil {
			t.Fatalf("读取 balance_adjust 审计日志失败: %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("balance_adjust 日志行数 = %d, want 2", len(logs))
		}
		for i, logRow := range logs {
			if logRow.TargetType != "customer" || logRow.TargetID != customer.ID {
				t.Errorf("balance_adjust 日志[%d] target = %s#%d, want customer#%d",
					i, logRow.TargetType, logRow.TargetID, customer.ID)
			}
			if logRow.OperatorID == nil || *logRow.OperatorID != 1 {
				t.Errorf("balance_adjust 日志[%d] operator_id = %v, want 1", i, logRow.OperatorID)
			}
		}
		if !strings.Contains(logs[1].Content, "修正误差") {
			t.Errorf("balance_adjust 日志[1] content = %q, want 含原因「修正误差」", logs[1].Content)
		}

		// --- Then: 仅调整余额：无订单/积分流水，累计消费与积分不变（04-API.md:192） ---
		for _, name := range []string{"orders", "order_items", "points_transactions"} {
			if n := env.countRows(t, ledgerEntityTables()[name]); n != 0 {
				t.Errorf("调整后 %s 行数 = %d, want 0（调整不产生订单/积分）", name, n)
			}
		}
		after := env.customerAfter(t, customer.ID)
		if after.TotalSpentCents != 0 || after.Points != 0 {
			t.Errorf("调整后累计消费/积分 = %d/%d, want 0/0（仅调整余额）", after.TotalSpentCents, after.Points)
		}
	})
}
