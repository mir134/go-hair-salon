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

## 部署与运维

> 目标部署形态：单个可执行文件 + `web/dist` + `data/` + `logs/` + `config.yaml`，默认监听 `0.0.0.0:8080`。
> 完整设计见 [`.ai/08-DEPLOYMENT.md`](.ai/08-DEPLOYMENT.md)。

### 1. 程序目录结构

构建产物的部署目录（与源码目录 `server/`、`web/` 区分）：

```text
hair-salon/
├── hair-salon-server(.exe)   # 单一可执行程序（后端 + 静态文件托管）
├── web/dist/                 # 前端构建产物（由 server 托管）
├── data/
│   ├── hair-salon.db         # SQLite 数据库（仅本机磁盘）
│   ├── backups/              # 备份目录（默认保留最近 7 份）
│   └── uploads/              # 上传文件
├── logs/                     # 按天日志
└── config.yaml               # 运行配置（含密钥，禁止提交）
```

Windows 使用 `hair-salon-server.exe`，macOS 使用对应架构的可执行文件。
启动时程序会自动创建 `DB_PATH` 父目录、`UPLOAD_DIR`、`BACKUP_DIR`、`LOG_DIR`，无需手工建目录。

### 2. 首次启动

1. 复制配置模板并填写：

   ```bash
   cp config.example.yaml config.yaml   # 程序从工作目录读取 config.yaml
   ```

2. 打开 `config.yaml`，至少确认：
   - `JWT_SECRET`：必填，填写足够长的随机字符串。留空时服务端启动会直接报错退出。
   - `DB_PATH` / `UPLOAD_DIR` / `BACKUP_DIR` / `LOG_DIR`：必填路径（示例值见 `server/config.example.yaml`）。
   - `INITIAL_ADMIN_PASSWORD`：初始管理员密码，默认 `admin123`。
3. 启动程序（双击 `start.bat` / 执行 `start.sh`，或直接运行可执行文件）。
4. 首次启动会自动完成：创建运行目录 → 打开 SQLite（WAL + busy_timeout）→ 执行数据库迁移 → 播种默认设置 → 创建初始管理员 → 启动每日自动备份 → 监听端口。
5. 浏览器打开 `http://127.0.0.1:8080`，用初始管理员登录：
   - 用户名：`admin`
   - 密码：`config.yaml` 中的 `INITIAL_ADMIN_PASSWORD`（默认 `admin123`）
   - **初始管理员仅在 `users` 表为空时创建一次；首次登录后请立即修改密码（右上角用户菜单）。**

### 3. 局域网访问

- 本机访问：`http://127.0.0.1:8080`
- 局域网访问：`http://<服务器局域网IP>:8080`
- 手机必须与服务器电脑处于同一 Wi-Fi / 局域网。
- 查询服务器局域网 IP：Windows 用 `ipconfig`，macOS 用 `ifconfig`（看 `IPv4 地址` / `inet`）。

### 4. 防火墙

- 只放行局域网访问 8080，**不建议把 8080 直接暴露到公网**。
- Windows（管理员 PowerShell，仅专用网络）：

  ```powershell
  New-NetFirewallRule -DisplayName "hair-salon-8080" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8080 -Profile Private
  ```

- macOS：系统设置 > 网络 > 防火墙，允许本程序接受传入连接（首次运行如被拦截，在「安全性与隐私」中放行）。
- 如需互联网访问，应另行设计 HTTPS、反向代理与访问控制，而不是直接映射 8080（见 08 文档「安全边界」）。

### 5. 数据目录（红线）

- SQLite 数据库 `data/hair-salon.db` **必须放在服务器本机本地磁盘**。
- **禁止**放在 SMB / NFS / NAS / 网络共享目录（UNC 路径 `\\...` 会被程序直接拒绝）。
- 数据库启用 WAL（会伴随生成 `*.db-wal` / `*.db-shm`），请勿手工删除或修改生产数据库文件。
- `data/` 必须随程序一起保留：升级只替换程序文件，绝不删除或覆盖 `data/`。

### 6. 备份与恢复

- **自动备份**：每天 `BACKUP_TIME`（默认 `23:00`）自动备份一次；启动时若最近备份缺失或超过 24 小时会立即补备。默认保留最近 **7 份**。
- **手动备份**：管理员登录后进入「备份」页面点击「立即备份」，或调用 `POST /api/v1/backups`。
- 备份内容：SQLite 数据库快照（WAL 安全）、`uploads`、`config.yaml`，打包为带时间戳的 `backup-YYYYMMDD-HHMMSS.zip`。
- **恢复**（仅管理员）：
  1. 进入「备份」页面，选择目标备份并点击「恢复」；
  2. 恢复是破坏性操作，需**二次确认**（勾选并输入确认文字）后才提交 `confirm=true`；
  3. 后端会先进入维护模式（恢复期间业务写请求返回 `503` / `code=50300`「系统维护中」），并**自动创建一份恢复前安全备份**；
  4. 校验备份完整性后替换数据库 / uploads / config，再做数据检查，失败时原数据不受影响；
  5. 恢复完成后建议**重新登录**（若备份中的 `config.yaml` 覆盖了当前 `JWT_SECRET`，旧登录态会失效）。

### 7. 升级步骤

```text
备份 → 停止服务 → 替换程序 → 启动（自动执行数据库迁移） → 健康检查
```

1. **备份**：在「备份」页面手动备份一次，或等待自动备份完成。
2. **停止服务**：执行 `stop.bat` / `stop.sh`，或向进程发送 `Ctrl+C` / `SIGTERM` 等待优雅关闭。
3. **替换程序**：用新版本的可执行文件覆盖旧文件（以及需要更新的 `web/dist`）。**保留 `data/` 与 `logs/` 不动**。
4. **执行数据库迁移**：启动新程序即可，程序启动时会自动执行数据库迁移（向前兼容），无需手工迁移；禁止手工修改生产 SQLite 文件。
5. **启动**：`start.bat` / `start.sh`。
6. **健康检查**：访问 `http://127.0.0.1:8080/health`，返回 `200` 且 `code=0` 表示服务正常。

## 快速开始（开发）

### 后端（server/）

```bash
cd server
cp config.example.yaml config.yaml   # 填写 JWT_SECRET；config.yaml 已被 git 忽略，禁止提交
go build ./...
go run ./cmd/server
```

### 前端（web/）

```bash
cd web
npm install
npm run dev
```

## 配置

服务端读取工作目录下的 `config.yaml`（可从 `server/config.example.yaml` 复制），全部配置项均可被同名环境变量覆盖（环境变量优先）。
完整字段与注释见 [`server/config.example.yaml`](server/config.example.yaml)：

| 配置键 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `DB_PATH` | 是 | 无 | SQLite 数据库路径，必须本机磁盘 |
| `UPLOAD_DIR` | 是 | 无 | 上传文件目录 |
| `BACKUP_DIR` | 是 | 无 | 备份目录 |
| `LOG_DIR` | 是 | 无 | 日志目录 |
| `JWT_SECRET` | 是 | 无 | JWT 签名密钥，禁止为空 |
| `SERVER_HOST` | 否 | `0.0.0.0` | 监听地址 |
| `SERVER_PORT` | 否 | `8080` | 监听端口 |
| `INITIAL_ADMIN_PASSWORD` | 否 | `admin123` | 初始管理员密码 |
| `BACKUP_TIME` | 否 | `23:00` | 每日自动备份时间 |

`config.yaml` 含 `JWT_SECRET` 与初始管理员密码，已被 `.gitignore` 忽略；仓库中只提交 `config.example.yaml`。

## 常用命令清单

```bash
# 后端（server/）
go test ./...           # 全部通过
go vet ./...            # 无告警
gofmt -l .              # 输出为空（无未格式化文件）
go build ./...          # 编译通过

# 前端（web/）
npm run lint            # eslint --max-warnings 0
npm run build           # vue-tsc --noEmit && vite build
```

- 后端 `go test ./...`、`go vet ./...`、`gofmt -l .`（空）必须通过；前端 `npm run lint`、`npm run build` 必须通过。
- 金额一律使用 int64 整数分，禁止 float64（浮点型一律禁止）：
  - 静态门（`go test ./...` 自动执行）：`go test ./internal/model -run TestNoFloat64InServerCode`；
  - 等价人工命令（应为空输出）：`grep -rn "float64" server/ --include="*.go" | grep -v _test`。
- 账务（充值 / 消费 / 退款 / 调整）必须事务化、可追溯，核心流水禁止物理删除。
- SQLite 数据库只放本机磁盘，禁止放在 SMB/NFS/NAS 等网络共享目录。
