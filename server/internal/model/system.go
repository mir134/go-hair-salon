package model

import "time"

// Setting 是系统设置键值表（settings 表，03-DATABASE.md:242-263）。
// 新增设置项必须同步更新 03-DATABASE.md 与 06-BUSINESS-RULES.md。
type Setting struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Key         string    `gorm:"column:key;size:64;not null" json:"key"`
	Value       string    `gorm:"size:255" json:"value"`
	Description string    `gorm:"size:255" json:"description"`
	UpdatedBy   *int64    `json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OperationLog 是操作审计日志（operation_logs 表，03-DATABASE.md:265-277）。
// 禁止物理删除；operator_id 为空表示无登录上下文的系统动作。
type OperationLog struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OperatorID *int64    `json:"operator_id"`
	Action     string    `gorm:"size:64;not null" json:"action"`
	TargetType string    `gorm:"size:32" json:"target_type"`
	TargetID   int64     `gorm:"not null;default:0" json:"target_id"`
	Content    string    `gorm:"type:text" json:"content"`
	IP         string    `gorm:"column:ip;size:64" json:"ip"`
	UserAgent  string    `gorm:"column:user_agent;size:255" json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
}
