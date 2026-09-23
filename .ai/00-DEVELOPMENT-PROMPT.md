## 1. 开发入口

开始任何代码修改前，先阅读本文件，再按以下顺序读取：

1. 01-PROJECT.md
2. 02-AGENTS.md
3. 06-BUSINESS-RULES.md
4. 03-DATABASE.md
5. 04-API.md
6. 07-UI.md
7. 05-TASKS.md
8. 08-DEPLOYMENT.md

## 2. 文档职责

- 01-PROJECT.md：项目目标、技术栈、架构边界
- 02-AGENTS.md：AI Coding Agent 开发规则
- 03-DATABASE.md：数据库模型与一致性设计
- 04-API.md：前后端接口契约
- 05-TASKS.md：开发顺序与验收任务
- 06-BUSINESS-RULES.md：业务语义与账务规则
- 07-UI.md：页面结构与交互
- 08-DEPLOYMENT.md：Windows/macOS/LAN/备份/升级

## 3. 文档关系

业务规则决定“做什么、怎么算”；

数据库决定“怎么可靠保存”；

API 决定“前后端怎么协作”；

UI 决定“用户怎么操作”；

TASKS 决定“当前做什么”；

DEPLOYMENT 决定“怎么运行”。

依赖关系：

PROJECT → AGENTS → BUSINESS-RULES → DATABASE → API → UI → TASKS → DEPLOYMENT

## 4. OpenCode 工作规则

- 一次只完成一个 TASK。
- 先检查现有代码，再设计和修改。
- 不得自行扩大需求范围。
- 业务规则不明确时，不允许自行猜测。
- 金额统一使用整数分，禁止 float64。
- 充值、消费、退款、余额调整必须事务化并可追溯。
- 核心账务流水禁止物理删除。
- 后端权限是最终安全边界。
- SQLite 主数据库禁止放在 SMB/NAS/网络共享目录。
- 不为 MVP 提前引入 Redis、Kafka、K8s、微服务等基础设施。
- 与文档冲突时先报告冲突，不要自行扩大修改。

## 5. 单任务 Definition of Done

- 功能实现
- 数据库符合 03-DATABASE.md
- 业务符合 06-BUSINESS-RULES.md
- API 符合 04-API.md
- UI 符合 07-UI.md
- 格式化、编译、测试通过
- 无明显回归
- 遗留风险已记录

## 6. 开发顺序

按 05-TASKS.md：

工程初始化 → 认证/RBAC → 客户 → 服务 → 消费 → 充值/余额 → 员工 → 系统设置 → Dashboard → 日志 → 备份恢复 → 移动端 → 跨平台验收。

## 7. 冲突优先级

业务语义：06-BUSINESS-RULES.md

项目架构：01-PROJECT.md

Agent 规范：02-AGENTS.md

数据：03-DATABASE.md

接口：04-API.md

任务：05-TASKS.md

交互：07-UI.md

部署：08-DEPLOYMENT.md

发现冲突时停止相关实现并明确指出冲突。