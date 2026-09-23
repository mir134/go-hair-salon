package model

import (
	"time"

	"gorm.io/gorm"
)

// Customer 是客户档案（customers 表，03-DATABASE.md:44-67）。
//
// balance_cents / points 只是查询缓存，事实来源分别是
// balance_transactions 与 points_transactions；任何余额变化必须同时产生流水。
type Customer struct {
	ID              int64          `gorm:"primaryKey" json:"id"`
	Name            string         `gorm:"size:64;not null" json:"name"`
	Phone           string         `gorm:"size:32" json:"phone"`
	Gender          string         `gorm:"size:16" json:"gender"`
	Birthday        *time.Time     `gorm:"type:date" json:"birthday"`
	Avatar          string         `gorm:"size:255" json:"avatar"`
	Wechat          string         `gorm:"size:64" json:"wechat"`
	Source          string         `gorm:"size:64" json:"source"`
	FirstVisitAt    *time.Time     `json:"first_visit_at"`
	LastVisitAt     *time.Time     `json:"last_visit_at"`
	TotalSpentCents int64          `gorm:"not null;default:0" json:"total_spent_cents"`
	BalanceCents    int64          `gorm:"not null;default:0" json:"balance_cents"`
	Points          int64          `gorm:"not null;default:0" json:"points"`
	Remark          string         `gorm:"type:text" json:"remark"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-"`
}

// Tag 是客户标签（tags 表，03-DATABASE.md:69-77）；软删除不影响历史关联。
type Tag struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"size:64;not null" json:"name"`
	Color     string         `gorm:"size:32" json:"color"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-"`
}

// CustomerTagRelation 是客户-标签关联（customer_tag_relations 表，03-DATABASE.md:79-85）。
//
// 表本身只有 customer_id/tag_id 两列，唯一联合索引防止重复挂标签。
type CustomerTagRelation struct {
	CustomerID int64 `gorm:"not null" json:"customer_id"`
	TagID      int64 `gorm:"not null" json:"tag_id"`
}
