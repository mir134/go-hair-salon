package model

// 本文件集中定义数据库中的枚举值（字符串/整数常量），
// 字段取值语义以 03-DATABASE.md 与 06-BUSINESS-RULES.md 为准。

// 用户角色（users.role，03-DATABASE.md:25）。
const (
	RoleAdmin = "admin"
	RoleStaff = "staff"
)

// 启用/停用状态（users.status、employees.status、service_categories.status、services.status）。
const (
	StatusDisabled = 0
	StatusEnabled  = 1
)

// 订单状态（03-DATABASE.md:133-145）。
const (
	OrderStatusPending   = "pending"
	OrderStatusCompleted = "completed"
	OrderStatusRefunded  = "refunded"
	OrderStatusCancelled = "cancelled"
)

// 支付方式（orders.payment_method、recharge_records.payment_method）。
const (
	PaymentMethodCash    = "cash"
	PaymentMethodWechat  = "wechat"
	PaymentMethodAlipay  = "alipay"
	PaymentMethodBalance = "balance"
)

// 余额流水类型（03-DATABASE.md:206-214）。
const (
	BalanceTxRecharge   = "recharge"
	BalanceTxConsume    = "consume"
	BalanceTxRefund     = "refund"
	BalanceTxGift       = "gift"
	BalanceTxAdjustment = "adjustment"
)

// 积分流水类型（03-DATABASE.md:234-240）。
const (
	PointsTxEarn       = "earn"
	PointsTxRefund     = "refund"
	PointsTxAdjustment = "adjustment"
)

// 充值记录状态（03-DATABASE.md:182）。
const (
	RechargeStatusActive   = "active"
	RechargeStatusRefunded = "refunded"
)

// 流水关联对象类型（*.reference_type，03-DATABASE.md:199,227）。
const (
	ReferenceTypeOrder    = "order"
	ReferenceTypeRecharge = "recharge"
)

// settings 键（03-DATABASE.md:256-261）与默认值。
const (
	SettingPointsPerYuan = "points_per_yuan"
	SettingShopName      = "shop_name"
	DefaultShopName      = "理发店"
	// DefaultPointsPerYuan 是「每消费 1 元获得积分」的默认值（03-DATABASE.md:259）。
	DefaultPointsPerYuan = 1
)
