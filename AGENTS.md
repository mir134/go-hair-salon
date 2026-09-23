# AGENTS.md

## 1. 项目说明

本项目是一个单店理发店客户管理系统。

项目的完整设计、业务规则、数据库、API、UI、任务和部署规范统一维护在：

```text
.ai/
```

**开发前必须先阅读 `.ai/00-DEVELOPMENT-PROMPT.md`。**

---

## 2. 文档体系

项目设计文档：

```text
.ai/
├── 00-DEVELOPMENT-PROMPT.md   # 开发总入口
├── 01-PROJECT.md              # 项目定义与架构
├── 02-AGENTS.md               # AI 开发详细规范
├── 03-DATABASE.md             # 数据库设计
├── 04-API.md                  # API 规范
├── 05-TASKS.md                # 开发任务
├── 06-BUSINESS-RULES.md       # 业务规则
├── 07-UI.md                   # UI 与页面设计
└── 08-DEPLOYMENT.md           # 部署与运维
```

文档职责：

```text
PROJECT
  ↓
AGENTS
  ↓
BUSINESS-RULES
  ↓
DATABASE
  ↓
API
  ↓
UI
  ↓
TASKS
  ↓
DEPLOYMENT
```

如果文档之间发生冲突：

**不要自行猜测，必须指出冲突。**

---

## 3. 开发前

执行任何开发任务前：

1. 阅读 `.ai/00-DEVELOPMENT-PROMPT.md`
2. 根据任务需要阅读相关 `.ai/*.md`
3. 检查现有代码
4. 理解现有实现后再修改

禁止：

```text
没有阅读项目规范
→ 直接生成大量代码
```

---

## 4. 任务边界

一次只实现一个明确任务。

优先按照：

```text
.ai/05-TASKS.md
```

中的任务顺序执行。

不要在当前任务中擅自增加：

- 无关功能
- 大规模重构
- 新技术栈
- 新基础设施
- 与当前任务无关的数据库修改

如果发现确实需要改变架构：

先说明：

```text
问题：
...

影响：
...

建议：
...

是否需要修改：
...
```

---

## 5. 必须遵守的业务硬规则

以下规则属于项目红线。

### 金额

所有金额使用整数分：

```text
int64
```

禁止使用 `float64` 保存金额。

---

### 客户余额

余额变化必须产生余额流水。

核心流程：

```text
充值 / 消费 / 退款 / 调整
        ↓
数据库事务
        ↓
balance_transactions
        ↓
customer.balance_cents
```

禁止让前端直接修改客户余额。

---

### 账务数据

以下数据不得进行普通物理删除：

```text
orders
order_items
recharge_records
balance_transactions
points_transactions
operation_logs
```

需要撤销时使用业务上的：

```text
refund
adjustment
cancel
```

并保留操作记录。

---

### 事务

涉及资金和余额的操作必须保证事务一致性。

例如：

```text
消费：
订单
+ 余额扣减
+ 余额流水
```

必须作为一个完整事务处理。

不能出现：

```text
订单成功
但余额没有扣除
```

或者：

```text
余额扣除了
但订单创建失败
```

---

### 权限

前端权限控制只是 UI 层。

真正权限必须由后端保证。

例如：

```text
admin 可以调整余额
staff 不可以
```

不能仅通过隐藏按钮实现权限控制。

---

### SQLite

SQLite 数据库必须放在本机本地磁盘。

禁止将 SQLite 数据库放在：

```text
SMB
NFS
NAS
网络共享目录
```

---

## 6. 技术原则

当前 MVP 优先保持简单：

```text
Go + Gin + GORM + SQLite
Vue 3 + TypeScript + Vite
Pinia + Vue Router + Element Plus
```

当前阶段不要为了“以后可能需要”提前加入：

```text
Redis
Kafka
RabbitMQ
Kubernetes
微服务
Service Mesh
Elasticsearch
```

除非项目文档或当前任务明确要求。

---

## 7. 修改代码原则

优先：

```text
理解现有代码
→ 最小修改
→ 验证
→ 再扩展
```

不要：

```text
为了实现一个小功能
→ 重写整个模块
```

优先复用已有：

- 组件
- API 封装
- Service
- Repository
- 工具函数
- 类型定义

避免重复实现。

---

## 8. 完成任务后必须验证

至少根据项目当前工具链执行：

```text
格式化
编译
测试
```

后端通常：

```bash
go test ./...
go vet ./...
```

前端通常：

```bash
npm run build
```

如果项目已有 lint / test 命令，也必须执行。

---

## 9. 完成任务后的输出

每次任务完成后，必须简要说明：

```text
## 完成内容

...

## 修改文件

...

## 测试结果

...

## 数据库/API变化

...

## 风险

...

## 后续建议

...
```

如果测试没有执行，也必须明确说明原因。

---

## 10. 不确定时不要猜

如果需求存在业务歧义：

```text
不要自行定义业务规则。
```

优先查看：

```text
.ai/06-BUSINESS-RULES.md
```

如果仍然没有明确规定：

先提出问题或给出设计建议，再继续实现。

---

## 11. 最终原则

本项目优先级：

```text
业务正确性
>
数据一致性
>
安全性
>
可维护性
>
性能
>
代码简洁
```

不要为了追求“代码能跑”而牺牲业务正确性。

也不要为了所谓的“高并发、高可扩展”而过早复杂化架构。

**先把单店 MVP 做稳定，再根据真实需求演进。**