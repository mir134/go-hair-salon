package model

import (
	"time"

	"gorm.io/gorm"
)

// ServiceCategory 是服务分类（service_categories 表，03-DATABASE.md:87-96）；
// sort 控制展示顺序。
type ServiceCategory struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Sort      int       `gorm:"not null;default:0" json:"sort"`
	Status    int       `gorm:"not null;default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service 是服务项目（services 表，03-DATABASE.md:98-111）；
// price_cents 为整数分（禁止浮点数），已产生订单的服务只能停用不能删除。
type Service struct {
	ID              int64          `gorm:"primaryKey" json:"id"`
	CategoryID      int64          `gorm:"not null" json:"category_id"`
	Name            string         `gorm:"size:128;not null" json:"name"`
	PriceCents      int64          `gorm:"not null;default:0" json:"price_cents"`
	DurationMinutes int            `gorm:"not null;default:0" json:"duration_minutes"`
	Status          int            `gorm:"not null;default:1" json:"status"`
	Remark          string         `gorm:"type:text" json:"remark"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-"`
}
