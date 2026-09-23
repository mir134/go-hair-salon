核心关系：

```
customers
  ├── customer_tag_relations -> tags
  ├── orders -> order_items -> services
  ├── recharge_records
  ├── balance_transactions
  └── points_transactions

employees
  └── orders

users
  └── operation_logs
```

## users

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | integer | PK |
| username | varchar | 登录名，唯一 |
| password_hash | varchar | 密码哈希 |
| role | varchar | admin/staff |
| employee_id | integer | 可为空 |
| status | integer | 1启用/0禁用 |
| created_at | datetime |  |
| updated_at | datetime |  |

## employees

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | integer | PK |
| name | varchar | 姓名 |
| phone | varchar | 手机号 |
| avatar | varchar | 头像 |
| position | varchar | 职位 |
| status | integer |  |
| joined_at | datetime | 入职时间 |
| remark | text |  |

## customers

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | integer | PK |
| name | varchar | 姓名 |
| phone | varchar | 手机号，建立索引 |
| gender | varchar |  |
| birthday | date |  |
| avatar | varchar |  |
| wechat | varchar |  |
| source | varchar | 来源 |
| first_visit_at | datetime |  |
| last_visit_at | datetime |  |
| total_spent_cents | integer | 累计消费，单位分 |
| balance_cents | integer | 当前余额缓存，单位分 |
| points | integer | 当前积分缓存 |
| remark | text |  |
| created_at | datetime |  |
| updated_at | datetime |  |
| deleted_at | datetime | 软删除 |

> balance_cents 允许作为查询缓存，但不是余额事实来源；事实来源是 balance_transactions。
> 

## tags / customer_tag_relations

tags：

```
id, name, color, created_at, updated_at, deleted_at
```

`deleted_at` 软删除，删除标签不影响历史 customer_tag_relations 与客户历史显示。

relation：

```
customer_id, tag_id
```

建立唯一联合索引。

## service_categories

```
id
name
sort
status
created_at
updated_at
```

## services

```
id
category_id
name
price_cents
duration_minutes
status
remark
created_at
updated_at
deleted_at
```

## orders

```
id
order_no
request_id                    # 幂等键，唯一
customer_id
employee_id
original_amount_cents
discount_amount_cents
paid_amount_cents
payment_method
status
remark
created_at
updated_at
```

`request_id` 由客户端生成并携带，服务端对同一 `request_id` 的创建请求只入账一次（唯一索引保证）。

状态：

```
pending
completed
refunded
cancelled
```

- pending：挂单（已开单未收款，未产生任何资金/余额/积分变动）。
- completed：已结账完成。
- refunded：已退款（不得物理删除）。
- cancelled：已取消（仅 admin，且仅针对 pending 挂单）。

## order_items

```
id
order_id
service_id
service_name_snapshot
quantity
unit_price_cents
discount_amount_cents
amount_cents
employee_id
created_at
```

保存 service_name_snapshot，防止服务项目改名后历史订单显示错误。

## recharge_records

金额字段语义：

- `recharge_amount_cents`：充值本金（计入余额）。
- `gift_amount_cents`：赠送金额（计入余额）。
- `actual_amount_cents`：客户实际支付金额（资金流入口径，通常等于充值本金；存在充值优惠时可小于本金）。Dashboard「充值金额」统计以此字段为准。

余额增加 = `recharge_amount_cents` + `gift_amount_cents`。

```
id
customer_id
request_id                    # 幂等键，唯一
recharge_amount_cents
gift_amount_cents
actual_amount_cents
payment_method
status                        # active/refunded，退款冲正后置 refunded
operator_id
remark
created_at
```

## balance_transactions

这是余额事实账本：

```
id
customer_id
type
amount_cents
balance_before_cents
balance_after_cents
reference_type
reference_id
operator_id
remark
created_at
```

type：

```
recharge
consume
refund
gift
adjustment
```

所有金额变化必须产生流水。

## points_transactions

```
id
customer_id
type
points
balance_before
balance_after
reference_type
reference_id
operator_id
remark
created_at
```

type：

```
earn        # 消费获得
refund      # 退款反向扣减
adjustment  # 管理员手动调整
```

## settings

系统设置键值表，仅管理员可读写的系统级配置：

```
id
key          # 设置键，唯一，如 points_per_yuan
value        # 设置值（string/json 字符串）
description  # 说明
updated_by   # 最后修改人 users.id
created_at
updated_at
```

MVP 至少包含以下键：

```
points_per_yuan       # 每消费 1 元获得的积分，默认 1
shop_name             # 门店名称（顶部显示）
```

新增设置项必须同步更新本文件与 06-BUSINESS-RULES.md，禁止仅在前端写死。

## operation_logs

```
id
operator_id
action
target_type
target_id
content
ip
user_agent
created_at
```

## 索引

至少：

- customers(phone)
- customers(name)
- customers(last_visit_at)
- orders(customer_id, created_at)
- orders(employee_id, created_at)
- orders(created_at)
- orders(request_id) 唯一索引
- recharge_records(request_id) 唯一索引
- balance_transactions(customer_id, created_at)
- operation_logs(operator_id, created_at)

## 关键事务

充值：

```
BEGIN
  check request_id 唯一（重复则直接返回原记录）
  create recharge_record
  create balance_transaction（type=recharge，本金）
  if gift_amount_cents > 0:
    create balance_transaction（type=gift，赠送）
  update customers.balance_cents
COMMIT
```

挂单（无资金变动）：

```
BEGIN
  check request_id 唯一
  create order（status=pending）
  create order_items
COMMIT
```

直接完成的消费：

```
BEGIN
  check request_id 唯一（重复则直接返回原订单）
  validate balance
  create order（status=completed）
  create order_items
  create balance_transaction
  update customers.balance_cents
  update customers.total_spent_cents
COMMIT
```

结账（挂单 → completed）：

```
BEGIN
  check order.status == pending（否则返回业务错误，防重复结账）
  if 余额支付:
    validate balance
    create balance_transaction
    update customers.balance_cents
  create points_transaction
  update customers.points
  update customers.total_spent_cents
  update order.status = completed, payment_method
COMMIT
```

任一步失败必须整体回滚。