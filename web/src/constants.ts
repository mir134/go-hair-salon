import type { OrderPaymentMethod } from '@/api'

/**
 * 顶栏门店名称占位常量。
 *
 * 系统设置接口（settings）尚未实现（后续 todo），此值与数据库 seed 的
 * `settings.shop_name` 默认值一致（repository/settings.go）；设置页落地后改为读取接口。
 */
export const DEFAULT_SHOP_NAME = '理发店'

/**
 * 性别展示文案（customers.gender 是自由文本，03-DATABASE.md:51）。
 * 这里只约定新增/编辑表单的常用取值与显示文案，未知值原样展示。
 */
export const GENDER_LABELS: Readonly<Record<string, string>> = {
  male: '男',
  female: '女',
  other: '其他',
}

/** Element Plus 标签色（el-tag type） */
export type ElementTagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

/** 订单状态文案（model/const.go:19-24、03-DATABASE.md:133-145） */
export const ORDER_STATUS_LABELS: Readonly<Record<string, string>> = {
  pending: '待结账',
  completed: '已完成',
  refunded: '已退款',
  cancelled: '已取消',
}

/** 订单状态标签色 */
export const ORDER_STATUS_TAG_TYPES: Readonly<Record<string, ElementTagType>> = {
  pending: 'warning',
  completed: 'success',
  refunded: 'info',
  cancelled: 'danger',
}

/** 支付方式文案（model/const.go:27-32） */
export const PAYMENT_METHOD_LABELS: Readonly<Record<string, string>> = {
  cash: '现金',
  wechat: '微信',
  alipay: '支付宝',
  balance: '余额',
}

/** 支付方式选项顺序（消费第 4 步与结账弹窗共用，04-API.md:24） */
export const PAYMENT_METHODS: readonly OrderPaymentMethod[] = ['cash', 'wechat', 'alipay', 'balance']

/** 余额流水类型文案（model/const.go:35-41、03-DATABASE.md:206-214） */
export const BALANCE_TX_TYPE_LABELS: Readonly<Record<string, string>> = {
  recharge: '充值',
  consume: '消费',
  refund: '退款',
  gift: '赠送',
  adjustment: '调整',
}

/** 积分流水类型文案（model/const.go:44-48、03-DATABASE.md:234-240） */
export const POINTS_TX_TYPE_LABELS: Readonly<Record<string, string>> = {
  earn: '获得',
  refund: '退还',
  adjustment: '调整',
}

/** 流水关联对象类型文案（model/const.go:56-60） */
export const REFERENCE_TYPE_LABELS: Readonly<Record<string, string>> = {
  order: '订单',
  recharge: '充值',
}
