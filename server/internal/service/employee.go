package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// EmployeeService 提供员工维护（04-API.md:194-204；D7 决议）。
//
// 硬规则：
//   - 员工没有物理删除路径：DELETE /employees/:id 的语义是停用（status=0，行保留）。
//     employees 表无 deleted_at，订单业绩与用户账号关联永远可回查（06 §11:120-125）；
//   - 员工被 orders/order_items/users 引用时同样不删除——本实现一律不物理删；
//   - 停用员工仍可被读取（列表/详情/历史订单联表），历史记录不因停用失效。
type EmployeeService struct {
	employees *repository.EmployeeRepository
}

// NewEmployeeService 构造员工服务。
func NewEmployeeService(employees *repository.EmployeeRepository) *EmployeeService {
	return &EmployeeService{employees: employees}
}

// EmployeeInput 是员工创建/修改输入（DTO 与 Model 分离）。
//
// Status 为 nil 表示未提供：创建时默认启用，修改时保持原值。
type EmployeeInput struct {
	Name     string
	Phone    string
	Avatar   string
	Position string
	Status   *int
	JoinedAt *time.Time
	Remark   string
}

// List 返回员工列表（含已停用，按 id 升序）；status 非 nil 时按状态过滤。
func (s *EmployeeService) List(ctx context.Context, status *int) ([]model.Employee, error) {
	rows, err := s.employees.List(ctx, repository.EmployeeListFilter{Status: status})
	if err != nil {
		return nil, fmt.Errorf("查询员工列表失败: %w", err)
	}
	return rows, nil
}

// Get 按主键读取员工（含已停用）；不存在返回 404 员工不存在。
func (s *EmployeeService) Get(ctx context.Context, id int64) (*model.Employee, error) {
	return s.find(ctx, id)
}

// Create 创建员工（仅 admin 入口调用）：姓名必填，status 未提供时默认启用。
func (s *EmployeeService) Create(ctx context.Context, in EmployeeInput) (*model.Employee, error) {
	employee, err := buildEmployeeProfile(in, model.StatusEnabled)
	if err != nil {
		return nil, err
	}
	if err := s.employees.Create(ctx, employee); err != nil {
		return nil, fmt.Errorf("创建员工失败: %w", err)
	}
	return employee, nil
}

// Update 修改员工（仅 admin 入口调用）：status 未提供时保持原值。
func (s *EmployeeService) Update(ctx context.Context, id int64, in EmployeeInput) (*model.Employee, error) {
	row, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	profile, err := buildEmployeeProfile(in, row.Status)
	if err != nil {
		return nil, err
	}
	employee := *row
	employee.Name = profile.Name
	employee.Phone = profile.Phone
	employee.Avatar = profile.Avatar
	employee.Position = profile.Position
	employee.Status = profile.Status
	employee.JoinedAt = profile.JoinedAt
	employee.Remark = profile.Remark
	if err := s.employees.UpdateProfile(ctx, &employee); err != nil {
		return nil, fmt.Errorf("更新员工失败: %w", err)
	}
	return &employee, nil
}

// Disable 停用员工（DELETE /employees/:id 的语义）：只置 status=0，永不物理删除。
//
// 幂等：对已停用员工重复调用仍返回成功，行与全部历史关联保持不变。
func (s *EmployeeService) Disable(ctx context.Context, id int64) error {
	if err := s.employees.UpdateStatus(ctx, id, model.StatusDisabled); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NotFound(CodeNotFound, "员工不存在")
		}
		return fmt.Errorf("停用员工失败: %w", err)
	}
	return nil
}

// find 读取员工（含已停用）；不存在返回 404 员工不存在。
func (s *EmployeeService) find(ctx context.Context, id int64) (*model.Employee, error) {
	employee, err := s.employees.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NotFound(CodeNotFound, "员工不存在")
		}
		return nil, fmt.Errorf("查询员工失败: %w", err)
	}
	return employee, nil
}

// buildEmployeeProfile 校验并规整员工档案字段（长度上限与数据库列宽一致）。
//
// fallbackStatus 在 in.Status 为 nil 时生效（创建默认启用、修改保持原值）。
func buildEmployeeProfile(in EmployeeInput, fallbackStatus int) (*model.Employee, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, BadRequest("员工姓名不能为空")
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, BadRequest("员工姓名不能超过 64 个字符")
	}
	phone := strings.TrimSpace(in.Phone)
	if utf8.RuneCountInString(phone) > 32 {
		return nil, BadRequest("手机号不能超过 32 个字符")
	}
	avatar := strings.TrimSpace(in.Avatar)
	if utf8.RuneCountInString(avatar) > 255 {
		return nil, BadRequest("头像地址不能超过 255 个字符")
	}
	position := strings.TrimSpace(in.Position)
	if utf8.RuneCountInString(position) > 64 {
		return nil, BadRequest("职位不能超过 64 个字符")
	}
	status, err := resolveStatus(in.Status, fallbackStatus)
	if err != nil {
		return nil, err
	}
	return &model.Employee{
		Name:     name,
		Phone:    phone,
		Avatar:   avatar,
		Position: position,
		Status:   status,
		JoinedAt: in.JoinedAt,
		Remark:   strings.TrimSpace(in.Remark),
	}, nil
}
