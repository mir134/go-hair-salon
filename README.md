# 理发店客户管理系统（go-hair-salon）

单店、局域网、自部署的美发店客户管理系统：店内一台电脑运行单个 Go 服务，店员用手机连接店内 Wi-Fi
即可完成「搜索客户 → 查看历史 → 消费 / 充值」的完整闭环，无需安装任何额外服务。

- 设计文档：`.ai/`（开发前必读 [`.ai/00-DEVELOPMENT-PROMPT.md`](.ai/00-DEVELOPMENT-PROMPT.md)）
- 开发规范（硬规则）：[`AGENTS.md`](AGENTS.md)

## 技术栈

| 端 | 技术 |
| --- | --- |
| 后端 | Go 1.24+、Gin、GORM、SQLite、JWT |
| 前端 | Vue 3、TypeScript、Vite、Pinia、Vue Router、Element Plus |

## 仓库结构

```text
go-hair-salon/
├── AGENTS.md     # OpenCode 项目级入口 / 硬规则
├── .ai/          # 设计文档体系
├── server/       # Go 后端（单体，含静态文件托管）
└── web/          # Vue 3 前端源码（构建产物 web/dist 由 server 托管）
```

## 快速开始

> 当前处于 P0「工程骨架」阶段，以下步骤会随后续任务逐步补齐。

### 后端（server/）

```bash
cd server
cp config.example.yaml config.yaml   # 填写 JWT_SECRET；config.yaml 已被 git 忽略，禁止提交
go build ./...
go run ./cmd/server
```

### 前端（web/）

前端工程将在后续任务初始化（Vite + Vue 3 + TypeScript）。

```bash
cd web
npm install
npm run dev
```

## 配置

服务端读取工作目录下的 `config.yaml`（可从 `server/config.example.yaml` 复制），全部配置项均可被同名环境变量覆盖。
`config.yaml` 含 `JWT_SECRET` 与初始管理员密码，已被 `.gitignore` 忽略；仓库中只提交 `config.example.yaml`。

## 部署

目标部署形态：单个可执行文件 + `web/dist` + `data/` + `logs/` + `config.yaml`，默认监听 `0.0.0.0:8080`。
详见 [`.ai/08-DEPLOYMENT.md`](.ai/08-DEPLOYMENT.md)。

## 开发约定

- 后端 `go test ./...`、`go vet ./...` 必须通过；前端 `npm run build` 必须通过。
- 金额一律使用 int64 整数分，禁止 float64。
- 账务（充值 / 消费 / 退款 / 调整）必须事务化、可追溯，核心流水禁止物理删除。
- SQLite 数据库只放本机磁盘，禁止放在 SMB/NFS/NAS 等网络共享目录。
