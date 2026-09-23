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
export { createOrder, getOrder, listOrders } from './order'
export type {
  Order,
  OrderCreatePayload,
  OrderItem,
  OrderItemPayload,
  OrderListQuery,
  OrderPaymentMethod,
} from './order'
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
