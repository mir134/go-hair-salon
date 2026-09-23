package service

// 分页默认值与上限（04-API.md:35-44 分页信封）。
const (
	// DefaultPage 默认页码。
	DefaultPage = 1
	// DefaultPageSize 默认每页条数。
	DefaultPageSize = 20
	// MaxPageSize 每页条数上限，防止一次拉取过多数据。
	MaxPageSize = 100
)

// PageQuery 是分页查询输入，客户列表与客户详情聚合接口共用。
//
// 分页是只读便利参数：非法值（非数字、0、负数）回退默认值，超限回退上限，
// 绝不因为页码问题让列表查询失败。
type PageQuery struct {
	Page     int
	PageSize int
}

// Normalize 归一化分页参数，返回可直接用于 SQL 的 offset/limit 与回显值。
func (q PageQuery) Normalize() (offset, limit, page, pageSize int) {
	page = q.Page
	if page < 1 {
		page = DefaultPage
	}
	pageSize = q.PageSize
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return (page - 1) * pageSize, pageSize, page, pageSize
}

// PageResult 是分页查询结果，客户列表与客户详情聚合接口共用。
//
// Items 由各 service 保证为已初始化的切片（空结果序列化为 []，不是 null）。
type PageResult[T any] struct {
	Items    []T
	Total    int64
	Page     int
	PageSize int
}
