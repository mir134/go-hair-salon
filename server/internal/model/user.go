package model

import "time"

// User 是系统登录用户（users 表，03-DATABASE.md:18-29）。
//
// password_hash 只存 bcrypt 哈希（禁止明文），并在 JSON 中隐藏。
type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         string    `gorm:"size:16;not null" json:"role"`
	EmployeeID   *int64    `json:"employee_id"`
	Status       int       `gorm:"not null;default:1" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Employee 是店内员工（employees 表，03-DATABASE.md:31-42）。
//
// 03-DATABASE 未定义 created_at/updated_at，因此本表不设时间戳字段；
// 停用通过 status 字段表达，禁止物理删除（保留订单业绩关联）。
type Employee struct {
	ID       int64      `gorm:"primaryKey" json:"id"`
	Name     string     `gorm:"size:64;not null" json:"name"`
	Phone    string     `gorm:"size:32" json:"phone"`
	Avatar   string     `gorm:"size:255" json:"avatar"`
	Position string     `gorm:"size:64" json:"position"`
	Status   int        `gorm:"not null;default:1" json:"status"`
	JoinedAt *time.Time `json:"joined_at"`
	Remark   string     `gorm:"type:text" json:"remark"`
}
