Base URL：

```
/api/v1
```

认证：

```
Authorization: Bearer <JWT>
```

## 通用响应

成功：

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

失败：

```json
{
  "code": 40001,
  "message": "客户不存在",
  "data": null
}
```

分页：

```json
{
  "items": [],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

## Auth

```
POST /auth/login
GET  /auth/me
POST /auth/logout
```

login：

```json
{"username":"admin","password":"***"}
```

## 权限矩阵

每个资源段标注访问角色：`admin`、`staff`，或 `both`（两者皆可）。后端 RBAC 中间件为最终边界，标注仅供实现对齐。

默认约定：

- `admin`：所有接口均可访问。
- `staff`：客户查询/新增、消费（含挂单/结账）、充值、标签等日常操作；**禁止**订单取消、余额调整、退款、删除客户、系统设置、操作日志、员工管理、数据恢复。
- 权限与 06-BUSINESS-RULES.md 第 7 节严格一致，冲突时以 06 为准。

## Customers

权限：查询/新增/编辑 `both`；删除 `admin`。

```
GET    /customers
POST   /customers
GET    /customers/:id
PUT    /customers/:id
DELETE /customers/:id
GET    /customers/:id/orders
GET    /customers/:id/balance-transactions
GET    /customers/:id/points-transactions
```

搜索参数：

```
keyword
phone
tag_id
page
page_size
```

keyword 支持姓名/手机号/微信号。

## Tags

权限：查询 `both`（消费/客户页选择标签）；创建/修改/删除 `admin`。

```
GET    /tags
POST   /tags
PUT    /tags/:id
DELETE /tags/:id
POST   /customers/:id/tags
DELETE /customers/:id/tags/:tag_id
```

## Services

权限：查询 `both`；创建/修改/删除 `admin`。

```
GET    /service-categories
POST   /service-categories
PUT    /service-categories/:id
DELETE /service-categories/:id

GET    /services
POST   /services
GET    /services/:id
PUT    /services/:id
DELETE /services/:id
```

## Orders

权限：查询 `both`；创建/结账 `both`；取消 `admin`；退款 `admin`。

```
GET  /orders
POST /orders              # 创建订单：直接完成或挂单（pending）
GET  /orders/:id
POST /orders/:id/pay      # 结账：pending → completed
POST /orders/:id/items                 # 挂单追加服务项目（仅 pending）
PUT  /orders/:id/items/:item_id        # 挂单修改明细数量（仅 pending）
DELETE /orders/:id/items/:item_id      # 挂单删除明细（仅 pending）
POST /orders/:id/refund
POST /orders/:id/cancel
```

创建订单必须由 service 层统一处理金额、余额和流水。

挂单与结账：

- 创建订单时若未收款可挂单（status=pending），不产生任何资金/余额/积分变动。
- 挂单订单结账前可编辑明细（增删项目、改数量），订单金额随明细重算；仅 pending 状态可编辑，编辑不产生资金变动。
- 明细中修改成交单价仍受改价权限约束（仅 admin，见 06-BUSINESS-RULES.md 3.1）。
- 结账（`POST /orders/:id/pay`）将挂单转为 completed，在同一事务内完成支付方式记录、余额扣减（若余额支付）、余额流水与积分流水。
- 结账幂等：仅 pending 状态可结账，重复结账返回业务错误（409）。
- 取消（`POST /orders/:id/cancel`）仅 admin，且仅针对 pending 挂单；已结账订单不可取消，只能退款。
- 挂单不自动过期，长期保留，由管理员手动取消。

退款规则：MVP 仅支持**全额退款**。退款成功后订单状态置为 refunded，产生反向余额/积分流水，营业额按退款冲减。部分退款暂不实现。

## Recharge

权限：查询 `both`；创建 `both`；退款冲正 `admin`。

```
GET  /recharges
POST /recharges
POST /recharges/:id/refund    # 退款冲正：仅 admin
```

充值接口必须：

1. 校验客户
2. 校验金额 > 0
3. 开启事务
4. 写充值记录
5. 写余额流水
6. 更新余额缓存

## Balance Adjustments

权限：仅 `admin`。

```
POST /customers/:id/balance-adjustments
```

余额调整必须：

1. 填写原因（reason，必填）
2. 金额可为正负，但不得导致余额为负
3. 写 balance_transactions（type=adjustment）
4. 写 operation_logs
5. 事务内完成

仅调整余额，不产生订单，不计入营业额。

## Employees

权限：`admin`。

```
GET    /employees
POST   /employees
GET    /employees/:id
PUT    /employees/:id
DELETE /employees/:id
```

## Settings

权限：查询 `both`（仅公开配置项）；修改 `admin`。

```
GET    /settings
PUT    /settings/:key
```

设置项与 03-DATABASE.md `settings` 表一致。MVP 键：`points_per_yuan`、`shop_name`。

## Dashboard

权限：`both`。

```
GET /dashboard/summary
GET /dashboard/revenue
GET /dashboard/customers
GET /dashboard/employee-performance
```

支持：

```
start_date
end_date
```

统计口径与 06-BUSINESS-RULES.md 第 8 节一致。

## Operation Logs

```
GET /operation-logs
```

仅管理员可访问。

## Backup

权限：仅 `admin`。

```
GET  /backups
POST /backups                  # 手动创建备份
POST /backups/:id/restore      # 恢复（二次确认）
```

恢复流程遵循 08-DEPLOYMENT.md 第 7 节：恢复前自动生成当前数据安全备份，恢复后执行数据检查。

## Health

```
GET /health
```

无需认证，返回 200 表示服务正常运行（供启动验收与排障使用，见 05-TASKS.md P0、08-DEPLOYMENT.md 第 9 节）。

## HTTP 状态码

- 200：成功
- 201：创建成功
- 400：参数错误
- 401：未登录
- 403：无权限
- 404：资源不存在
- 409：业务冲突
- 422：业务校验失败
- 500：服务器错误

## 幂等与并发

充值、消费、退款等资金接口必须防止重复提交。

**强制机制**：

- 客户端在创建订单/充值时携带 `request_id`（UUID）。
- 后端在 `orders.request_id`、`recharge_records.request_id` 建立**唯一索引**。
- 重复 `request_id` 直接返回已存在记录（200 + 原结果），不重复入账。
- 退款以原订单/充值记录为准，天然幂等。

余额扣减必须在事务内完成，禁止：

```
读取余额
↓
应用层判断
↓
再单独 update
```

这种方式存在并发竞态。

## API 演进

禁止把数据库字段结构直接暴露成 API contract。DTO 与 Model 分离，为未来迁移 PostgreSQL、增加小程序或多门店保留空间。