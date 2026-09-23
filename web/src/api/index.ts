// src/api 是前端唯一允许直连 axios 的目录（见 eslint.config.js 的 no-restricted-imports）。
// 其他层只从这里 import：import { get, post } from '@/api'
export {
  ApiError,
  CODE_VALIDATION_FAILED,
  TOKEN_STORAGE_KEY,
  del,
  get,
  http,
  post,
  put,
  request,
} from './http'
export type { ApiEnvelope, PageData } from './http'
export { fetchMe, login, logout } from './auth'
export type { AuthUser, LoginPayload, LoginResult, UserRole } from './auth'
export {
  createCustomer,
  deleteCustomer,
  getCustomer,
  listCustomerBalanceTransactions,
  listCustomerOrders,
  listCustomerPointsTransactions,
  listCustomers,
  updateCustomer,
} from './customer'
export type {
  BalanceTransaction,
  Customer,
  CustomerListQuery,
  CustomerOrder,
  CustomerPayload,
  PageQuery,
  PointsTransaction,
} from './customer'
export { attachCustomerTag, detachCustomerTag, listTags } from './tag'
export type { Tag } from './tag'
export {
  addOrderItem,
  cancelOrder,
  createOrder,
  deleteOrderItem,
  getOrder,
  listOrders,
  payOrder,
  refundOrder,
  updateOrderItem,
} from './order'
export type {
  Order,
  OrderCreatePayload,
  OrderItem,
  OrderItemAddPayload,
  OrderItemPayload,
  OrderItemUpdatePayload,
  OrderListQuery,
  OrderPayPayload,
  OrderPaymentMethod,
  OrderSubmitStatus,
} from './order'
export {
  createBalanceAdjustment,
  createRecharge,
  listBalanceTransactions,
  listRecharges,
  refundRecharge,
} from './recharge'
export type {
  BalanceAdjustmentPayload,
  BalanceAdjustmentResult,
  Recharge,
  RechargeCreatePayload,
  RechargeListQuery,
  RechargePaymentMethod,
} from './recharge'
export { listOperationLogs } from './log'
export type { OperationLog, OperationLogListQuery } from './log'
export {
  createService,
  createServiceCategory,
  deleteService,
  deleteServiceCategory,
  getService,
  listServiceCategories,
  listServices,
  updateService,
  updateServiceCategory,
} from './service'
export type {
  Service,
  ServiceCategory,
  ServiceCategoryPayload,
  ServicePayload,
} from './service'
export {
  SETTING_KEY_POINTS_PER_YUAN,
  SETTING_KEY_SHOP_NAME,
  listSettings,
  updateSetting,
} from './settings'
export type { Setting } from './settings'
export {
  EMPLOYEE_STATUS_DISABLED,
  EMPLOYEE_STATUS_ENABLED,
  createEmployee,
  disableEmployee,
  getEmployee,
  listEmployees,
  updateEmployee,
} from './employee'
export type { Employee, EmployeePayload } from './employee'
export {
  USER_STATUS_DISABLED,
  USER_STATUS_ENABLED,
  createUser,
  listUsers,
  updateUser,
} from './user'
export type { User, UserCreatePayload, UserUpdatePayload } from './user'
export {
  getCustomers,
  getEmployeePerformance,
  getRevenue,
  getSummary,
} from './dashboard'
export type {
  DashboardCustomerPoint,
  DashboardCustomers,
  DashboardRangeQuery,
  DashboardRevenue,
  DashboardRevenuePoint,
  DashboardSummary,
  EmployeePerformance,
  EmployeePerformanceRow,
} from './dashboard'
