package model

import "time"

// RechargeRecord 是充值记录（recharge_records 表，03-DATABASE.md:164-186）。
//
// 余额增加 = recharge_amount_cents + gift_amount_cents；
// actual_amount_cents 是资金流入口径（实付），Dashboard 充值统计以此为准。
type RechargeRecord struct {
	ID                  int64     `gorm:"primaryKey" json:"id"`
	CustomerID          int64     `gorm:"not null" json:"customer_id"`
	RequestID           string    `gorm:"size:64;not null" json:"request_id"`
	RechargeAmountCents int64     `gorm:"not null;default:0" json:"recharge_amount_cents"`
	GiftAmountCents     int64     `gorm:"not null;default:0" json:"gift_amount_cents"`
	ActualAmountCents   int64     `gorm:"not null;default:0" json:"actual_amount_cents"`
	PaymentMethod       string    `gorm:"size:16" json:"payment_method"`
	Status              string    `gorm:"size:16;not null;default:active" json:"status"`
	OperatorID          *int64    `json:"operator_id"`
	Remark              string    `gorm:"type:text" json:"remark"`
	CreatedAt           time.Time `json:"created_at"`
}

// BalanceTransaction 是余额事实账本（balance_transactions 表，03-DATABASE.md:188-216）。
// 禁止物理删除；balance_before/after 保证任意时点余额可追溯。
type BalanceTransaction struct {
	ID                 int64     `gorm:"primaryKey" json:"id"`
	CustomerID         int64     `gorm:"not null" json:"customer_id"`
	Type               string    `gorm:"size:16;not null" json:"type"`
	AmountCents        int64     `gorm:"not null" json:"amount_cents"`
	BalanceBeforeCents int64     `gorm:"not null;default:0" json:"balance_before_cents"`
	BalanceAfterCents  int64     `gorm:"not null;default:0" json:"balance_after_cents"`
	ReferenceType      string    `gorm:"size:32" json:"reference_type"`
	ReferenceID        *int64    `json:"reference_id"`
	OperatorID         *int64    `json:"operator_id"`
	Remark             string    `gorm:"type:text" json:"remark"`
	CreatedAt          time.Time `json:"created_at"`
}

// PointsTransaction 是积分流水（points_transactions 表，03-DATABASE.md:218-240）。
// 积分不得低于 0；退款按原 earn 反向扣减。
type PointsTransaction struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	CustomerID    int64     `gorm:"not null" json:"customer_id"`
	Type          string    `gorm:"size:16;not null" json:"type"`
	Points        int64     `gorm:"not null" json:"points"`
	BalanceBefore int64     `gorm:"not null;default:0" json:"balance_before"`
	BalanceAfter  int64     `gorm:"not null;default:0" json:"balance_after"`
	ReferenceType string    `gorm:"size:32" json:"reference_type"`
	ReferenceID   *int64    `json:"reference_id"`
	OperatorID    *int64    `json:"operator_id"`
	Remark        string    `gorm:"type:text" json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
}
