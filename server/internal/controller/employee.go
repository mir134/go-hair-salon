package controller

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// EmployeeController 提供员工 CRUD（04-API.md:194-204）。
//
// 权限边界由路由中间件保证：员工列表查询 both（快速消费/挂单需选择服务员工），
// 其余（创建/修改/停用/按 id 查询）仅 admin（06 §7、04-API.md:194-204）。
// DELETE /employees/:id 的语义是停用（status=0，行保留，D7 决议），
// 审计日志在业务写操作完成后、事务之外写入（见 service.WriteLog）。
type EmployeeController struct {
	employees *service.EmployeeService
	logs      *service.OperationLogService
}

// NewEmployeeController 构造员工控制器。
func NewEmployeeController(employees *service.EmployeeService, logs *service.OperationLogService) *EmployeeController {
	return &EmployeeController{employees: employees, logs: logs}
}

// employeeRequest 是 POST/PUT /employees 请求体。
//
// status 用指针区分「未提供」与「显式停用（0）」；
// joined_at 以字符串接收以同时接受 RFC3339 与纯日期（解析失败 → 400，见 parseEmployeeJoinedAt）。
type employeeRequest struct {
	Name     string  `json:"name"`
	Phone    string  `json:"phone"`
	Avatar   string  `json:"avatar"`
	Position string  `json:"position"`
	Status   *int    `json:"status"`
	JoinedAt *string `json:"joined_at"`
	Remark   string  `json:"remark"`
}

// EmployeeView 是员工 DTO（03-DATABASE.md:31-42）。
type EmployeeView struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Phone    string     `json:"phone"`
	Avatar   string     `json:"avatar"`
	Position string     `json:"position"`
	Status   int        `json:"status"`
	JoinedAt *time.Time `json:"joined_at"`
	Remark   string     `json:"remark"`
}

// List 处理 GET /api/v1/employees（both）：含已停用员工；
// ?status=0|1 可选过滤（非法值宽松回退，不得 500）。
func (h *EmployeeController) List(c *gin.Context) {
	rows, err := h.employees.List(c.Request.Context(), parseStatusQuery(c))
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newEmployeeViews(rows))
}

// Get 处理 GET /api/v1/employees/:id（仅 admin）：含已停用员工，记录始终可读。
func (h *EmployeeController) Get(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	employee, err := h.employees.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, newEmployeeView(employee))
}

// Create 处理 POST /api/v1/employees（仅 admin）：姓名必填。
func (h *EmployeeController) Create(c *gin.Context) {
	var req employeeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	joinedAt, err := parseEmployeeJoinedAt(req.JoinedAt)
	if err != nil {
		Fail(c, err)
		return
	}
	employee, err := h.employees.Create(c.Request.Context(), employeeInput(req, joinedAt))
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "employee_create", employee.ID, fmt.Sprintf("新增员工 %s", employee.Name))
	Created(c, newEmployeeView(employee))
}

// Update 处理 PUT /api/v1/employees/:id（仅 admin）：status 未提供时保持原值。
func (h *EmployeeController) Update(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req employeeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体 JSON 无效"))
		return
	}
	joinedAt, err := parseEmployeeJoinedAt(req.JoinedAt)
	if err != nil {
		Fail(c, err)
		return
	}
	employee, err := h.employees.Update(c.Request.Context(), id, employeeInput(req, joinedAt))
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "employee_update", employee.ID, fmt.Sprintf("修改员工 %s", employee.Name))
	Success(c, newEmployeeView(employee))
}

// Delete 处理 DELETE /api/v1/employees/:id（仅 admin）：
// 语义为停用（status=0，行保留；D7 决议、06 §11:120-125），永不物理删除——
// 员工被 orders/order_items/users 引用时历史业绩与账号关联不失效。
func (h *EmployeeController) Delete(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.employees.Disable(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, "employee_disable", id, fmt.Sprintf("停用员工 #%d", id))
	Success(c, gin.H{})
}

// writeLog 在业务写操作完成后写审计日志（禁止在业务事务内调用，见 service.WriteLog）。
func (h *EmployeeController) writeLog(c *gin.Context, action string, targetID int64, content string) {
	if err := h.logs.WriteLog(c.Request.Context(), action, "employee", targetID, content); err != nil {
		slog.Default().Error("写入员工审计日志失败", "action", action, "err", err)
	}
}

// employeeInput 把请求体与已解析的入职时间映射为 service 输入。
func employeeInput(req employeeRequest, joinedAt *time.Time) service.EmployeeInput {
	return service.EmployeeInput{
		Name:     req.Name,
		Phone:    req.Phone,
		Avatar:   req.Avatar,
		Position: req.Position,
		Status:   req.Status,
		JoinedAt: joinedAt,
		Remark:   req.Remark,
	}
}

// parseEmployeeJoinedAt 解析入职时间：
//   - 未提供/空串 → nil（清空或未设置）；
//   - RFC3339（含时区偏移）→ 转 UTC 存储（06 §10:115）；
//   - 纯日期 YYYY-MM-DD → 当日 UTC 零点；
//   - 其余 → 400（不得 500）。
func parseEmployeeJoinedAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		utc := ts.UTC()
		return &utc, nil
	}
	if date, err := time.Parse("2006-01-02", value); err == nil {
		return &date, nil
	}
	return nil, service.BadRequest("joined_at 格式不合法（需 RFC3339 或 YYYY-MM-DD）")
}

// newEmployeeView 把 model.Employee 转为 DTO。
func newEmployeeView(employee *model.Employee) EmployeeView {
	return EmployeeView{
		ID:       employee.ID,
		Name:     employee.Name,
		Phone:    employee.Phone,
		Avatar:   employee.Avatar,
		Position: employee.Position,
		Status:   employee.Status,
		JoinedAt: employee.JoinedAt,
		Remark:   employee.Remark,
	}
}

// newEmployeeViews 批量转换员工 DTO，保证空结果为 []。
func newEmployeeViews(rows []model.Employee) []EmployeeView {
	views := make([]EmployeeView, 0, len(rows))
	for i := range rows {
		views = append(views, newEmployeeView(&rows[i]))
	}
	return views
}
