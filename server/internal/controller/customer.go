package controller

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// CustomerController 提供客户 CRUD、搜索与分页接口（04-API.md:70-95）。
//
// 权限边界由路由中间件保证：查询/新增/编辑 both，删除仅 admin；
// 控制器只做参数解析、DTO 转换与审计日志，业务规则在 service 层。
type CustomerController struct {
	customers *service.CustomerService
	logs      *service.OperationLogService
}

// NewCustomerController 构造客户控制器。
func NewCustomerController(customers *service.CustomerService, logs *service.OperationLogService) *CustomerController {
	return &CustomerController{customers: customers, logs: logs}
}

// customerRequest 是 POST/PUT /customers 请求体（DTO 与 Model 分离，04-API.md:300-302）。
type customerRequest struct {
	Name     string  `json:"name"`
	Phone    string  `json:"phone"`
	Gender   string  `json:"gender"`
	Birthday *string `json:"birthday"`
	Avatar   string  `json:"avatar"`
	Wechat   string  `json:"wechat"`
	Source   string  `json:"source"`
	Remark   string  `json:"remark"`
}

// CustomerView 是客户 DTO：字段白名单，金额一律整数分，时间一律 UTC。
type CustomerView struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Phone           string     `json:"phone"`
	Gender          string     `json:"gender"`
	Birthday        *string    `json:"birthday"`
	Avatar          string     `json:"avatar"`
	Wechat          string     `json:"wechat"`
	Source          string     `json:"source"`
	FirstVisitAt    *time.Time `json:"first_visit_at"`
	LastVisitAt     *time.Time `json:"last_visit_at"`
	TotalSpentCents int64      `json:"total_spent_cents"`
	BalanceCents    int64      `json:"balance_cents"`
	Points          int64      `json:"points"`
	Remark          string     `json:"remark"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// List 处理 GET /api/v1/customers：
// keyword（姓名/手机号/微信号）、phone、tag_id、page/page_size、sort=recent。
func (h *CustomerController) List(c *gin.Context) {
	tagID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("tag_id")), 10, 64)
	result, err := h.customers.List(c.Request.Context(), service.CustomerListQuery{
		Keyword:   c.Query("keyword"),
		Phone:     c.Query("phone"),
		TagID:     tagID,
		Sort:      strings.TrimSpace(c.Query("sort")),
		PageQuery: parsePageQuery(c),
	})
	if err != nil {
		Fail(c, err)
		return
	}
	SuccessPage(c, result, newCustomerView)
}

// Create 处理 POST /api/v1/customers：非空手机号重复返回 409，first_visit_at=now。
func (h *CustomerController) Create(c *gin.Context) {
	in, ok := bindCustomerRequest(c)
	if !ok {
		return
	}
	customer, err := h.customers.Create(c.Request.Context(), in)
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "customer_create", customer.ID, fmt.Sprintf("新增客户 %s", customer.Name))
	Created(c, newCustomerView(customer))
}

// Get 处理 GET /api/v1/customers/:id。
func (h *CustomerController) Get(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	customer, err := h.customers.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newCustomerView(customer))
}

// Update 处理 PUT /api/v1/customers/:id：按 id 更新原行，改手机号不新建客户。
func (h *CustomerController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	in, ok := bindCustomerRequest(c)
	if !ok {
		return
	}
	customer, err := h.customers.Update(c.Request.Context(), id, in)
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "customer_update", customer.ID, fmt.Sprintf("修改客户 %s", customer.Name))
	Success(c, newCustomerView(customer))
}

// Delete 处理 DELETE /api/v1/customers/:id：软删除（路由层已限制仅 admin）。
func (h *CustomerController) Delete(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.customers.Delete(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "customer_delete", id, fmt.Sprintf("删除客户 #%d", id))
	Success(c, gin.H{})
}

// bindCustomerRequest 解析请求体并转换为 service 输入；失败时已写出 400 信封。
func bindCustomerRequest(c *gin.Context) (service.CustomerProfileInput, bool) {
	var req customerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return service.CustomerProfileInput{}, false
	}
	birthday, err := parseBirthday(req.Birthday)
	if err != nil {
		Fail(c, err)
		return service.CustomerProfileInput{}, false
	}
	return service.CustomerProfileInput{
		Name:     req.Name,
		Phone:    req.Phone,
		Gender:   req.Gender,
		Birthday: birthday,
		Avatar:   req.Avatar,
		Wechat:   req.Wechat,
		Source:   req.Source,
		Remark:   req.Remark,
	}, true
}

// parseBirthday 解析生日入参：兼容 "2006-01-02" 与 RFC3339；空串/null 视为未填写。
func parseBirthday(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, service.BadRequest("生日格式应为 YYYY-MM-DD")
}

// newCustomerView 把 model.Customer 转为 DTO（生日按日期输出，时间 UTC）。
func newCustomerView(customer *model.Customer) CustomerView {
	var birthday *string
	if customer.Birthday != nil {
		day := customer.Birthday.UTC().Format("2006-01-02")
		birthday = &day
	}
	return CustomerView{
		ID:              customer.ID,
		Name:            customer.Name,
		Phone:           customer.Phone,
		Gender:          customer.Gender,
		Birthday:        birthday,
		Avatar:          customer.Avatar,
		Wechat:          customer.Wechat,
		Source:          customer.Source,
		FirstVisitAt:    customer.FirstVisitAt,
		LastVisitAt:     customer.LastVisitAt,
		TotalSpentCents: customer.TotalSpentCents,
		BalanceCents:    customer.BalanceCents,
		Points:          customer.Points,
		Remark:          customer.Remark,
		CreatedAt:       customer.CreatedAt,
		UpdatedAt:       customer.UpdatedAt,
	}
}

// writeLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）。
func (h *CustomerController) writeLog(c *gin.Context, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, "customer", targetID, content); err != nil {
		slog.Default().Error("写入客户审计日志失败", "action", action, "err", err)
	}
}

// parseIDParam 解析正整数路径参数；非法时写出 400 信封并返回 false。
func parseIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		Fail(c, service.BadRequest("id 不合法"))
		return 0, false
	}
	return id, true
}

// parsePageQuery 解析分页参数；非法/空值交给 service 回退默认值（不报错）。
func parsePageQuery(c *gin.Context) service.PageQuery {
	return service.PageQuery{
		Page:     queryInt(c, "page"),
		PageSize: queryInt(c, "page_size"),
	}
}

// queryInt 读取整数查询参数；非数字返回 0（由 service 归一化）。
func queryInt(c *gin.Context, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return 0
	}
	return value
}
