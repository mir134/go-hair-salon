package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// EmployeeRepository 提供 employees 表的读取访问（03-DATABASE.md:31-42）。
//
// 员工禁止物理删除（保留订单业绩关联），停用通过 status 表达；
// 员工维护 API 由 todo 38-40 提供，本文件当前只实现订单创建所需的存在性校验。
type EmployeeRepository struct {
	db *gorm.DB
}

// NewEmployeeRepository 构造员工仓储。
func NewEmployeeRepository(db *gorm.DB) *EmployeeRepository {
	return &EmployeeRepository{db: db}
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
