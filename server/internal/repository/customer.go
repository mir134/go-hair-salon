package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// 客户列表排序方式（04-API.md:85-95）。
const (
	// CustomerSortDefault 默认排序：新建倒序。
	CustomerSortDefault = ""
	// CustomerSortRecent 按最近到店时间倒序（last_visit_at DESC，未到店排最后）。
	CustomerSortRecent = "recent"
)

// CustomerListFilter 是 GET /customers 的查询条件。
//
// Keyword 同时匹配姓名/手机号/微信号；Phone 只匹配手机号；TagID > 0 时按标签过滤。
// Offset/Limit 由 service 层按归一化后的分页参数计算。
type CustomerListFilter struct {
	Keyword string
	Phone   string
	TagID   int64
	Sort    string
	Offset  int
	Limit   int
}

// CustomerRepository 提供 customers 表的读写访问。
//
// 客户只允许软删除（deleted_at），历史消费/充值/余额流水必须保留
// （06-BUSINESS-RULES.md:5-11）；手机号唯一性由部分唯一索引
// ux_customers_phone_active 保证（空手机号与已软删除行不参与）。
type CustomerRepository struct {
	db *gorm.DB
}

// NewCustomerRepository 构造仓储。
func NewCustomerRepository(db *gorm.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

// List 按条件分页查询客户，返回当前页数据与满足条件的总行数。
func (r *CustomerRepository) List(ctx context.Context, f CustomerListFilter) ([]model.Customer, int64, error) {
	query := r.filtered(ctx, f)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	switch f.Sort {
	case CustomerSortRecent:
		query = query.Order("last_visit_at DESC, id DESC")
	default:
		query = query.Order("id DESC")
	}
	if f.Limit > 0 {
		query = query.Limit(f.Limit).Offset(f.Offset)
	}

	var items []model.Customer
	if err := query.Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// filtered 构造带全部过滤条件的查询（软删除行由 GORM 自动排除）。
func (r *CustomerRepository) filtered(ctx context.Context, f CustomerListFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&model.Customer{})

	if keyword := strings.TrimSpace(f.Keyword); keyword != "" {
		like := "%" + escapeLike(keyword) + "%"
		query = query.Where(
			`(name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\' OR wechat LIKE ? ESCAPE '\')`,
			like, like, like,
		)
	}
	if phone := strings.TrimSpace(f.Phone); phone != "" {
		query = query.Where(`phone LIKE ? ESCAPE '\'`, "%"+escapeLike(phone)+"%")
	}
	if f.TagID > 0 {
		query = query.Where("id IN (?)",
			r.db.Model(&model.CustomerTagRelation{}).Select("customer_id").Where("tag_id = ?", f.TagID))
	}
	return query
}

// Create 新增客户；非空手机号与未删除客户重复时返回 ErrDuplicate。
func (r *CustomerRepository) Create(ctx context.Context, customer *model.Customer) error {
	if err := r.db.WithContext(ctx).Create(customer).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// FindByID 按主键查询（不含已软删除行）；不存在时返回 ErrNotFound。
func (r *CustomerRepository) FindByID(ctx context.Context, id int64) (*model.Customer, error) {
	var customer model.Customer
	if err := r.db.WithContext(ctx).First(&customer, id).Error; err != nil {
		return nil, translateCustomerError(err)
	}
	return &customer, nil
}

// UpdateProfile 更新客户档案字段（不触碰余额/积分/累计消费等账务缓存列）。
//
// 调用方必须已确认客户存在（service 层先 FindByID），因此不依赖 RowsAffected
// 判断存在性——SQLite 在字段值未变化时不保证 changes() 大于 0。
// 手机号与未删除客户重复时返回 ErrDuplicate。
func (r *CustomerRepository) UpdateProfile(ctx context.Context, customer *model.Customer) error {
	err := r.db.WithContext(ctx).Model(&model.Customer{}).
		Where("id = ?", customer.ID).
		Updates(map[string]any{
			"name":     customer.Name,
			"phone":    customer.Phone,
			"gender":   customer.Gender,
			"birthday": customer.Birthday,
			"avatar":   customer.Avatar,
			"wechat":   customer.Wechat,
			"source":   customer.Source,
			"remark":   customer.Remark,
		}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicate
	}
	return err
}

// SoftDelete 软删除客户（只置 deleted_at，禁止物理删除）；不存在时返回 ErrNotFound。
func (r *CustomerRepository) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Customer{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ExistsByPhone 判断非空手机号是否已被其他未删除客户占用（excludeID 用于编辑自身时排除）。
func (r *CustomerRepository) ExistsByPhone(ctx context.Context, phone string, excludeID int64) (bool, error) {
	query := r.db.WithContext(ctx).Model(&model.Customer{}).Where("phone = ?", phone)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// translateCustomerError 把 gorm 的记录不存在错误转换为仓储哨兵错误。
func translateCustomerError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// likeEscaper 转义 LIKE 通配符，避免用户输入的 % / _ 扩大匹配范围。
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// escapeLike 转义 LIKE 模式中的特殊字符（配合 ESCAPE '\' 使用）。
func escapeLike(s string) string { return likeEscaper.Replace(s) }
