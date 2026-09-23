package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// CustomerService 提供客户档案的查询与维护（06-BUSINESS-RULES.md 第 1 节）。
//
// 硬规则：
//   - 手机号允许为空；同一门店非空手机号对应唯一未删除客户，重复建档返回 409 并提示改原客户；
//   - 编辑客户只更新原行（改手机号不得新建客户）；
//   - 删除是软删除，历史消费/充值/流水一律保留；
//   - 金额一律整数分，时间一律 UTC。
type CustomerService struct {
	customers *repository.CustomerRepository
}

// NewCustomerService 构造客户服务。
func NewCustomerService(customers *repository.CustomerRepository) *CustomerService {
	return &CustomerService{customers: customers}
}

// CustomerListQuery 是客户列表查询输入。
type CustomerListQuery struct {
	Keyword string
	Phone   string
	TagID   int64
	Sort    string
	PageQuery
}

// CustomerPage 是客户分页结果（含归一化后的分页回显值）。
type CustomerPage struct {
	Items    []model.Customer
	Total    int64
	Page     int
	PageSize int
}

// CustomerProfileInput 是新增/编辑客户的可写字段（DTO 与 Model 分离，04-API.md:300-302）。
type CustomerProfileInput struct {
	Name     string
	Phone    string
	Gender   string
	Birthday *time.Time
	Avatar   string
	Wechat   string
	Source   string
	Remark   string
}

// List 按条件分页查询客户。
func (s *CustomerService) List(ctx context.Context, q CustomerListQuery) (*CustomerPage, error) {
	offset, limit, page, pageSize := q.Normalize()
	items, total, err := s.customers.List(ctx, repository.CustomerListFilter{
		Keyword: q.Keyword,
		Phone:   q.Phone,
		TagID:   q.TagID,
		Sort:    q.Sort,
		Offset:  offset,
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("查询客户列表失败: %w", err)
	}
	return &CustomerPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 按主键读取客户；不存在（含已软删除）返回 404 客户不存在。
func (s *CustomerService) Get(ctx context.Context, id int64) (*model.Customer, error) {
	customer, err := s.customers.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("查询客户失败: %w", err)
	}
	return customer, nil
}

// Create 新增客户：非空手机号重复返回 409，创建时置 first_visit_at=now。
func (s *CustomerService) Create(ctx context.Context, in CustomerProfileInput) (*model.Customer, error) {
	name, err := validateCustomerName(in.Name)
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(in.Phone)
	if err := s.ensurePhoneAvailable(ctx, phone, 0); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	customer := &model.Customer{
		Name:         name,
		Phone:        phone,
		Gender:       strings.TrimSpace(in.Gender),
		Birthday:     normalizeBirthday(in.Birthday),
		Avatar:       strings.TrimSpace(in.Avatar),
		Wechat:       strings.TrimSpace(in.Wechat),
		Source:       strings.TrimSpace(in.Source),
		Remark:       in.Remark,
		FirstVisitAt: &now,
	}
	if err := s.customers.Create(ctx, customer); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errPhoneTaken
		}
		return nil, fmt.Errorf("创建客户失败: %w", err)
	}
	return customer, nil
}

// Update 编辑客户档案：按 id 更新原行（改手机号不新建客户）；手机号与他人重复返回 409。
func (s *CustomerService) Update(ctx context.Context, id int64, in CustomerProfileInput) (*model.Customer, error) {
	customer, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	name, err := validateCustomerName(in.Name)
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(in.Phone)
	if phone != customer.Phone {
		if err := s.ensurePhoneAvailable(ctx, phone, id); err != nil {
			return nil, err
		}
	}

	customer.Name = name
	customer.Phone = phone
	customer.Gender = strings.TrimSpace(in.Gender)
	customer.Birthday = normalizeBirthday(in.Birthday)
	customer.Avatar = strings.TrimSpace(in.Avatar)
	customer.Wechat = strings.TrimSpace(in.Wechat)
	customer.Source = strings.TrimSpace(in.Source)
	customer.Remark = in.Remark

	if err := s.customers.UpdateProfile(ctx, customer); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errPhoneTaken
		}
		return nil, fmt.Errorf("更新客户失败: %w", err)
	}
	return customer, nil
}

// Delete 软删除客户（仅 admin 入口调用，权限由 RBAC 中间件保证）。
func (s *CustomerService) Delete(ctx context.Context, id int64) error {
	if err := s.customers.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCustomerNotFound
		}
		return fmt.Errorf("删除客户失败: %w", err)
	}
	return nil
}

// errPhoneTaken 是重复手机号的标准业务冲突（06-BUSINESS-RULES.md:8）。
var errPhoneTaken = Conflict("手机号已存在，请编辑原客户")

// ensurePhoneAvailable 校验非空手机号未被其他未删除客户占用；空手机号直接放行。
func (s *CustomerService) ensurePhoneAvailable(ctx context.Context, phone string, excludeID int64) error {
	if phone == "" {
		return nil
	}
	taken, err := s.customers.ExistsByPhone(ctx, phone, excludeID)
	if err != nil {
		return fmt.Errorf("校验手机号失败: %w", err)
	}
	if taken {
		return errPhoneTaken
	}
	return nil
}

// validateCustomerName 校验并规整姓名（必填）。
func validateCustomerName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", BadRequest("客户姓名不能为空")
	}
	return trimmed, nil
}

// normalizeBirthday 把生日规整为 UTC 零点的日期（birthday 是 date 列，保留客户端日期的年月日）。
func normalizeBirthday(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &day
}
