package model

import (
	"time"

	"gorm.io/gorm"
)

// Order 是消费订单（orders 表，03-DATABASE.md:113-145）。
//
// request_id 由客户端生成并携带，唯一索引保证同一请求只入账一次；
// 所有金额字段均为整数分。订单不得物理删除（退款/取消保留记录）。
type Order struct {
	ID                  int64     `gorm:"primaryKey" json:"id"`
	OrderNo             string    `gorm:"size:32;not null" json:"order_no"`
	RequestID           string    `gorm:"size:64;not null" json:"request_id"`
	CustomerID          int64     `gorm:"not null" json:"customer_id"`
	EmployeeID          *int64    `json:"employee_id"`
	OriginalAmountCents int64     `gorm:"not null;default:0" json:"original_amount_cents"`
	DiscountAmountCents int64     `gorm:"not null;default:0" json:"discount_amount_cents"`
	PaidAmountCents     int64     `gorm:"not null;default:0" json:"paid_amount_cents"`
	PaymentMethod       string    `gorm:"size:16" json:"payment_method"`
	Status              string    `gorm:"size:16;not null;default:pending" json:"status"`
	Remark              string    `gorm:"type:text" json:"remark"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// OrderItem 是订单明细（order_items 表，03-DATABASE.md:147-162）。
//
// service_name_snapshot 冻结下单时的服务名，防止改名后历史订单显示错误。
//
// DeletedAt 支持挂单（pending）明细的「删除」语义：AGENTS.md 第 5 节规定
// order_items 禁止物理删除，因此挂单删明细只置 deleted_at，行与快照保留；
// 已结账/已退款/已取消订单的明细不可编辑（06 §3:32），历史账务事实不受影响。
type OrderItem struct {
	ID                  int64          `gorm:"primaryKey" json:"id"`
	OrderID             int64          `gorm:"not null" json:"order_id"`
	ServiceID           int64          `gorm:"not null" json:"service_id"`
	ServiceNameSnapshot string         `gorm:"size:128;not null" json:"service_name_snapshot"`
	Quantity            int            `gorm:"not null;default:1" json:"quantity"`
	UnitPriceCents      int64          `gorm:"not null;default:0" json:"unit_price_cents"`
	DiscountAmountCents int64          `gorm:"not null;default:0" json:"discount_amount_cents"`
	AmountCents         int64          `gorm:"not null;default:0" json:"amount_cents"`
	EmployeeID          *int64         `json:"employee_id"`
	CreatedAt           time.Time      `json:"created_at"`
	DeletedAt           gorm.DeletedAt `json:"-"`
}
