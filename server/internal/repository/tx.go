package repository

import (
	"context"

	"gorm.io/gorm"
)

// Tx 是事务内的数据库句柄（*gorm.DB 的类型别名）。
//
// service 层是资金/余额/积分事务边界的归属层（service/doc.go:3-5）：
// 通过 Transactor 开启事务，并把 Tx 传给各仓储的 *Tx 方法；
// 类型别名让 service 无需直接引用 gorm 类型名即可传递事务句柄。
type Tx = *gorm.DB

// Transactor 提供跨仓储的数据库事务边界。
//
// 唯一连接池（SetMaxOpenConns(1)，repository/db.go:55）下，
// 事务内的所有读写必须复用同一个 Tx：在事务回调中调用非事务仓储方法
// 会等待被事务自己占用的唯一连接，最终以超时失败。
type Transactor struct {
	db *gorm.DB
}

// NewTransactor 构造事务管理器。
func NewTransactor(db *gorm.DB) *Transactor {
	return &Transactor{db: db}
}

// WithinTx 在单个数据库事务内执行 fn：
// fn 返回错误时整体回滚（保证零部分写入），否则提交。
func (t *Transactor) WithinTx(ctx context.Context, fn func(tx Tx) error) error {
	return t.db.WithContext(ctx).Transaction(fn)
}
