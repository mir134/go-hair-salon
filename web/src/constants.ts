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
