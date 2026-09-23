package model

// AllModels 返回全部持久化模型，顺序按外键依赖排列（父表在前），
// 供 repository.Migrate 按序执行 AutoMigrate。
func AllModels() []any {
	return []any{
		&User{},
		&Employee{},
		&Customer{},
		&Tag{},
		&CustomerTagRelation{},
		&ServiceCategory{},
		&Service{},
		&Order{},
		&OrderItem{},
		&RechargeRecord{},
		&BalanceTransaction{},
		&PointsTransaction{},
		&Setting{},
		&OperationLog{},
	}
}
