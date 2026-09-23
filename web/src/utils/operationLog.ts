/**
 * 操作日志页的展示与筛选辅助（plan todo 48）。
 *
 * 动作集合以代码为准：对 server 端 `WriteLog(...)` 的实参全量 grep 后固化
 * （controller/{auth,customer,tag,servicecategory,serviceitem,employee,settings}.go、
 * service/{auth,ordertx,orderstate,orderpending,ordercancel,orderrefund,recharge,rechargerefund,balanceadjust}.go）。
 * 后续 wave 新增动作时在此追加中文文案即可，未知动作原样显示。
 *
 * 展示函数全部接收原始字段（而非整行对象）：el-table 的插槽 row 是 DefaultRow，
 * 传整行会被 vue-tsc 拒绝；传字段既满足类型又保持函数可测。
 */

/** 动作 → 中文文案 */
export const OPERATION_LOG_ACTION_LABELS: Readonly<Record<string, string>> = {
  login: '登录',
  login_failed: '登录失败',
  logout: '登出',
  customer_create: '新增客户',
  customer_update: '修改客户',
  customer_delete: '删除客户',
  tag_create: '新增标签',
  tag_update: '修改标签',
  tag_delete: '删除标签',
  tag_attach: '客户挂标签',
  tag_detach: '客户摘标签',
  service_category_create: '新增服务分类',
  service_category_update: '修改服务分类',
  service_category_delete: '删除服务分类',
  service_create: '新增服务',
  service_update: '修改服务',
  service_delete: '删除服务',
  order_create: '创建订单',
  order_pay: '订单结账',
  order_cancel: '取消订单',
  order_item_add: '追加明细',
  order_item_update: '修改明细',
  order_item_delete: '删除明细',
  order_refund: '订单退款',
  recharge: '充值',
  recharge_refund: '充值冲正',
  balance_adjust: '余额调整',
  employee_create: '新增员工',
  employee_update: '修改员工',
  employee_disable: '停用员工',
  setting_update: '修改设置',
}

/** 动作筛选项（与常量表同序，按业务域排列） */
export const OPERATION_LOG_ACTIONS: readonly string[] = Object.keys(OPERATION_LOG_ACTION_LABELS)

/** 日志列表筛选条件：空值表示不筛选 */
export interface OperationLogFilters {
  operatorId: number | null
  action: string
  startDate: string
  endDate: string
}

/** 空筛选（默认全部日志） */
export function emptyOperationLogFilters(): OperationLogFilters {
  return { operatorId: null, action: '', startDate: '', endDate: '' }
}

/** 动作显示文案：未知动作原样展示（避免新动作在页面上"消失"） */
export function operationLogActionLabel(action: string): string {
  return OPERATION_LOG_ACTION_LABELS[action] ?? action
}

/** 操作人显示文案：无操作人（系统动作）显示 "系统"，否则显示用户名 */
export function operationLogOperatorLabel(operatorId: number | null, operatorName: string): string {
  return operatorId === null || operatorName === '' ? '系统' : operatorName
}

/** 目标显示文案：target_type#target_id；无目标显示 "—" */
export function operationLogTargetLabel(targetType: string, targetId: number): string {
  if (targetType === '' || targetId <= 0) {
    return '—'
  }
  return `${targetType} #${targetId}`
}
