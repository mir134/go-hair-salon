package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// EmployeeRepository 提供 employees 表的读写访问（03-DATABASE.md:31-42）。
//
// 硬规则（D7 决议）：员工禁止物理删除（保留订单业绩与用户账号关联），
// 停用通过 status=0 表达；DELETE /employees/:id 由 service 层映射为停用。
type EmployeeRepository struct {
	db *gorm.DB
}

// NewEmployeeRepository 构造员工仓储。
func NewEmployeeRepository(db *gorm.DB) *EmployeeRepository {
	return &EmployeeRepository{db: db}
}

// EmployeeListFilter 是 GET /employees 的查询条件（Status 非 nil 时按状态过滤）。
type EmployeeListFilter struct {
	Status *int
}

// List 返回员工（含已停用），按 id 升序；可按状态过滤。
func (r *EmployeeRepository) List(ctx context.Context, f EmployeeListFilter) ([]model.Employee, error) {
	query := r.db.WithContext(ctx).Model(&model.Employee{})
	if f.Status != nil {
		query = query.Where("status = ?", *f.Status)
	}
	var rows []model.Employee
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByID 按主键查询员工；不存在返回 ErrNotFound。
func (r *EmployeeRepository) FindByID(ctx context.Context, id int64) (*model.Employee, error) {
	var employee model.Employee
	if err := r.db.WithContext(ctx).First(&employee, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &employee, nil
}

// Create 新增员工。
//
// status 带 gorm default:1，GORM 会把零值替换为默认值，因此「创建即停用」需在同一事务内
// 插入后显式回写 status（与 services 仓储同一模式），保证语义正确且无中间可见态。
func (r *EmployeeRepository) Create(ctx context.Context, employee *model.Employee) error {
	status := employee.Status
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(employee).Error; err != nil {
			return err
		}
		if status == model.StatusDisabled {
			return tx.Model(&model.Employee{}).
				Where("id = ?", employee.ID).
				UpdateColumn("status", status).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	employee.Status = status
	return nil
}

// UpdateProfile 更新员工可写字段（调用方已确认员工存在）。
//
// 使用 map 更新：显式写出全部字段（含 status=0），不受默认值替换影响。
func (r *EmployeeRepository) UpdateProfile(ctx context.Context, employee *model.Employee) error {
	return r.db.WithContext(ctx).Model(&model.Employee{}).
		Where("id = ?", employee.ID).
		Updates(map[string]any{
			"name":      employee.Name,
			"phone":     employee.Phone,
			"avatar":    employee.Avatar,
			"position":  employee.Position,
			"status":    employee.Status,
			"joined_at": employee.JoinedAt,
			"remark":    employee.Remark,
		}).Error
}

// UpdateStatus 更新员工启用状态（0 停用）；员工不存在时返回 ErrNotFound。
//
// 只改 status，永不物理删除：历史订单/用户账号对员工的行引用因此始终有效。
func (r *EmployeeRepository) UpdateStatus(ctx context.Context, id int64, status int) error {
	res := r.db.WithContext(ctx).Model(&model.Employee{}).
		Where("id = ?", id).
		Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
