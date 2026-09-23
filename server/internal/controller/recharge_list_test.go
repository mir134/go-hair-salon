package controller_test

// TestRechargeList 是 todo 31 的验收测试（计划：`go test ./internal/controller -run TestRechargeList -v -count=1`）：
//   - GET /recharges（both）：customer_id / start_date / end_date 筛选与分页正确，
//     DTO 含 customer_name 与 status（04-API.md:157-165）；
//   - GET /customers/:id/balance-transactions（both）：分页、时间倒序（最新在前），
//     含 type / balance_before_cents / balance_after_cents / reference，且 before/after 连续
//     （03-DATABASE.md:188-216、06 §4:51）；
//   - malformed：非法分页/日期宽松回退；空数据 → []；不存在客户 → 404；未登录 → 401。

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// listRecharges 拉取充值列表（断言 200），返回分页结构。
func (e *customerEnv) listRecharges(t *testing.T, token, query string) pageData {
	t.Helper()
	return e.getPage(t, token, "/api/v1/recharges"+query)
}

func TestRechargeList(t *testing.T) {
	env := newCustomerEnv(t)
	custA := env.createCustomer(t, env.staffToken, `{"name":"列表客户甲","phone":"13800009001"}`)
	custB := env.createCustomer(t, env.staffToken, `{"name":"列表客户乙","phone":"13800009002"}`)

	// --- Given: 甲两笔（10000+2000、5000）、乙一笔（8000），全部走真实充值 API ---
	recA1 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-list-a1", custA.ID, 10000, 2000, "cash")))
	recA2 := decodeRecharge(t, env.postRecharge(env.staffToken, rechargeJSON("req-list-a2", custA.ID, 5000, 0, "wechat")))
	recB1 := decodeRecharge(t, env.postRecharge(env.adminToken, rechargeJSON("req-list-b1", custB.ID, 8000, 0, "alipay")))

	// --- When: staff 拉取充值列表 ---
	page := env.listRecharges(t, env.staffToken, "?page_size=100")

	// --- Then: total=3、时间倒序（最新在前）、DTO 含客户名与 status ---
	if page.Total != 3 {
		t.Fatalf("充值列表 total = %d, want 3 (items=%s)", page.Total, page.Items)
	}
	items := decodeItems[rechargeView](t, page)
	if len(items) != 3 {
		t.Fatalf("充值列表 items 条数 = %d, want 3", len(items))
	}
	if items[0].ID != recB1.ID || items[1].ID != recA2.ID || items[2].ID != recA1.ID {
		t.Errorf("充值列表顺序 = [%d,%d,%d], want [%d,%d,%d]（最新在前）",
			items[0].ID, items[1].ID, items[2].ID, recB1.ID, recA2.ID, recA1.ID)
	}
	if items[0].CustomerName != "列表客户乙" || items[2].CustomerName != "列表客户甲" {
		t.Errorf("充值列表客户名 = %q/%q, want 列表客户乙/列表客户甲", items[0].CustomerName, items[2].CustomerName)
	}
	if items[0].Status != model.RechargeStatusActive || items[0].PaymentMethod != model.PaymentMethodAlipay {
		t.Errorf("充值列表首条 status/payment = %q/%q, want active/alipay", items[0].Status, items[0].PaymentMethod)
	}
	if items[2].RechargeAmountCents != 10000 || items[2].GiftAmountCents != 2000 || items[2].ActualAmountCents != 10000 {
		t.Errorf("充值列表甲首笔金额 = %d/%d/%d, want 10000/2000/10000",
			items[2].RechargeAmountCents, items[2].GiftAmountCents, items[2].ActualAmountCents)
	}

	// --- When/Then: customer_id 筛选 ---
	if got := env.listRecharges(t, env.staffToken, fmt.Sprintf("?customer_id=%d&page_size=100", custA.ID)); got.Total != 2 {
		t.Errorf("?customer_id=%d total = %d, want 2", custA.ID, got.Total)
	}
	if got := env.listRecharges(t, env.staffToken, fmt.Sprintf("?customer_id=%d&page_size=100", custB.ID)); got.Total != 1 {
		t.Errorf("?customer_id=%d total = %d, want 1", custB.ID, got.Total)
	}
	if got := env.listRecharges(t, env.staffToken, "?customer_id=999999&page_size=100"); got.Total != 0 {
		t.Errorf("不存在客户筛选 total = %d, want 0", got.Total)
	}

	// --- When/Then: 分页（page=1&page_size=2 → 2 条；page=2 → 1 条最早） ---
	page1 := env.listRecharges(t, env.staffToken, "?page=1&page_size=2")
	if page1.Total != 3 || page1.Page != 1 || page1.PageSize != 2 || len(decodeItems[rechargeView](t, page1)) != 2 {
		t.Errorf("page=1&page_size=2 = total %d/page %d/size %d, want 3/1/2", page1.Total, page1.Page, page1.PageSize)
	}
	page2 := env.listRecharges(t, env.staffToken, "?page=2&page_size=2")
	if tail := decodeItems[rechargeView](t, page2); len(tail) != 1 || tail[0].ID != recA1.ID {
		t.Errorf("page=2&page_size=2 items = %+v, want [%d]", tail, recA1.ID)
	}

	// --- When/Then: 日期筛选（UTC 日期边界）与非法参数宽松回退 ---
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	for query, want := range map[string]int64{
		"?start_date=" + today:                            3,
		"?end_date=" + today:                              3,
		"?start_date=" + yesterday:                        3,
		"?end_date=" + yesterday:                          0,
		"?start_date=not-a-date":                          3,
		"?page=abc&page_size=-5&customer_id=not-a-number": 3,
	} {
		if got := env.listRecharges(t, env.staffToken, query+"&page_size=100"); got.Total != want {
			t.Errorf("GET /recharges%s total = %d, want %d", query, got.Total, want)
		}
	}

	// --- When: 拉取甲客户余额流水（倒序） ---
	txPage := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/balance-transactions?page_size=100", custA.ID))

	// --- Then: total=3、最新在前（第二笔充值 → 赠送 → 第一笔本金） ---
	if txPage.Total != 3 {
		t.Fatalf("甲余额流水分页 total = %d, want 3", txPage.Total)
	}
	txs := decodeItems[balanceTxView](t, txPage)
	if len(txs) != 3 {
		t.Fatalf("甲余额流水条数 = %d, want 3", len(txs))
	}
	if txs[0].Type != model.BalanceTxRecharge || txs[0].AmountCents != 5000 ||
		txs[0].BalanceBeforeCents != 12000 || txs[0].BalanceAfterCents != 17000 {
		t.Errorf("余额流水[0] = %+v, want recharge/+5000/12000→17000（最新在前）", txs[0])
	}
	if txs[1].Type != model.BalanceTxGift || txs[1].AmountCents != 2000 ||
		txs[1].BalanceBeforeCents != 10000 || txs[1].BalanceAfterCents != 12000 {
		t.Errorf("余额流水[1] = %+v, want gift/+2000/10000→12000", txs[1])
	}
	if txs[2].Type != model.BalanceTxRecharge || txs[2].BalanceBeforeCents != 0 || txs[2].BalanceAfterCents != 10000 {
		t.Errorf("余额流水[2] = %+v, want recharge/0→10000", txs[2])
	}
	// 倒序列表反向遍历即时间正序：before[i+1] 必须等于 after[i]，首尾闭合。
	chain := []balanceTxView{txs[2], txs[1], txs[0]}
	if chain[0].BalanceBeforeCents != 0 || chain[len(chain)-1].BalanceAfterCents != 17000 {
		t.Errorf("余额链首尾 = %d/%d, want 0/17000", chain[0].BalanceBeforeCents, chain[len(chain)-1].BalanceAfterCents)
	}
	for i := 0; i+1 < len(chain); i++ {
		if chain[i].BalanceAfterCents != chain[i+1].BalanceBeforeCents {
			t.Errorf("余额流水不连续: after[%d]=%d != before[%d]=%d",
				i, chain[i].BalanceAfterCents, i+1, chain[i+1].BalanceBeforeCents)
		}
	}
	for i, tx := range txs {
		if tx.ReferenceType != model.ReferenceTypeRecharge || tx.ReferenceID == nil {
			t.Errorf("余额流水[%d] reference = %s/%v, want recharge/非空", i, tx.ReferenceType, tx.ReferenceID)
		}
		if tx.CustomerID != custA.ID {
			t.Errorf("余额流水[%d] customer_id = %d, want %d", i, tx.CustomerID, custA.ID)
		}
	}
	if got := env.customerState(t, custA.ID).BalanceCents; got != 17000 {
		t.Errorf("甲余额 = %d, want 17000（与流水链尾一致）", got)
	}

	// --- When/Then: 分页（page=2&page_size=2 → 1 条最早） ---
	txPage2 := env.getPage(t, env.staffToken,
		fmt.Sprintf("/api/v1/customers/%d/balance-transactions?page=2&page_size=2", custA.ID))
	if tail := decodeItems[balanceTxView](t, txPage2); len(tail) != 1 || tail[0].ID != txs[2].ID {
		t.Errorf("甲余额流水第 2 页 = %+v, want [id=%d]", tail, txs[2].ID)
	}

	// --- When/Then: 无充值客户 → 空数组；不存在客户 → 404 ---
	empty := env.createCustomer(t, env.staffToken, `{"name":"无流水客户","phone":"13800009003"}`)
	if got := env.getPage(t, env.staffToken, fmt.Sprintf("/api/v1/customers/%d/balance-transactions", empty.ID)); got.Total != 0 || string(got.Items) != "[]" {
		t.Errorf("空客户余额流水 = total %d/items %s, want 0/[]", got.Total, got.Items)
	}
	w := env.authed(http.MethodGet, "/api/v1/customers/999999/balance-transactions", "", env.adminToken)
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在客户余额流水 status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	} else if envl := decodeEnvelope(t, w); envl.Code != service.CodeCustomerNotFound {
		t.Errorf("不存在客户余额流水 code = %d, want %d", envl.Code, service.CodeCustomerNotFound)
	}

	// --- Then: 未登录 → 401 ---
	if w := env.do(http.MethodGet, "/api/v1/recharges", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /recharges status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}
