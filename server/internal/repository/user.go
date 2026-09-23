package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 仓储层哨兵错误：service 层据此映射为用户可见的业务错误，
// 避免业务层直接依赖 gorm 错误类型。
var (
	// ErrNotFound 表示目标记录不存在。
	ErrNotFound = errors.New("记录不存在")
	// ErrDuplicate 表示唯一约束冲突（如 users.username 重复）。
	ErrDuplicate = errors.New("唯一约束冲突")
)

// UserRepository 提供 users 表的读写访问。
//
// users 通过 status 停用，不存在物理删除路径（06-BUSINESS-RULES.md:120-125）。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 构造仓储。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 新增用户；用户名重复时返回 ErrDuplicate。
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := r.db.WithContext(ctx).Create(user).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicate
	}
	return err
}

// FindByUsername 按登录名查询；不存在时返回 ErrNotFound。
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, translateUserError(err)
	}
	return &user, nil
}

// FindByID 按主键查询；不存在时返回 ErrNotFound。
func (r *UserRepository) FindByID(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, translateUserError(err)
	}
	return &user, nil
}

// Count 返回 users 表总行数（初始管理员播种的幂等判据）。
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// List 返回用户列表（含已停用），按 id 升序；status 非 nil 时按状态过滤。
func (r *UserRepository) List(ctx context.Context, status *int) ([]model.User, error) {
	query := r.db.WithContext(ctx).Model(&model.User{})
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var users []model.User
	if err := query.Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateAdminFields 管理入口的原子更新：status 与/或 password_hash 一次 UPDATE 写入
// （调用方保证至少提供一个，避免出现「只改了一半」的中间态）；updated_at 由 GORM 维护。
//
// passwordHash 为空表示不改密码；status 为 nil 表示不改状态；用户不存在时返回 ErrNotFound。
func (r *UserRepository) UpdateAdminFields(ctx context.Context, id int64, status *int, passwordHash string) error {
	updates := make(map[string]any, 2)
	if status != nil {
		updates["status"] = *status
	}
	if passwordHash != "" {
		updates["password_hash"] = passwordHash
	}
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword 更新指定用户的密码哈希；用户不存在时返回 ErrNotFound。
func (r *UserRepository) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Update("password_hash", passwordHash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateStatus 更新指定用户的启用状态（0 停用）；用户不存在时返回 ErrNotFound。
func (r *UserRepository) UpdateStatus(ctx context.Context, id int64, status int) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
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

// translateUserError 把 gorm 的记录不存在错误转换为仓储哨兵错误。
func translateUserError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
