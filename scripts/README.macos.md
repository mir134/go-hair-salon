# 理发店客户管理系统（macOS 部署）

单店、局域网、自部署的理发店客户管理系统。店内一台电脑运行服务，店员用手机
连接店内 Wi-Fi 即可完成「搜索客户 → 查看历史 → 消费 / 充值」。

> 本包为 macOS 版本，内含 Intel（amd64）与 Apple Silicon（arm64）双架构二进制，
> 首次运行需按下方步骤处理执行权限与 Gatekeeper。

---

## 目录结构

```text
hair-salon-macos/
├── start.command             ← 双击启动服务（首次按下方步骤处理）
├── stop.command              ← 双击停止服务
├── backup.command            ← 命令行手动备份（需要登录令牌）
├── hair-salon-server-darwin-arm64    ← Apple Silicon 二进制（M1/M2/M3/M4）
├── hair-salon-server-darwin-amd64    ← Intel 二进制
├── config.yaml               ← 运行配置（JWT_SECRET、端口等）
├── config.example.yaml       ← 配置模板（config.yaml 丢失时用）
├── web/dist/                 ← 前端页面（由服务自动托管）
├── data/                     ← SQLite 数据库、上传文件、备份（**严禁删除**）
│   ├── hair-salon.db
│   ├── uploads/
│   └── backups/
└── logs/                     ← 运行日志
```

---

## 首次运行

### 1. 打开「终端」（Terminal.app）

打开 `应用程序 → 实用工具 → 终端`，或按 `⌘ Space` 搜索「终端」。

### 2. 进入部署目录

假设你把 `hair-salon-macos` 文件夹放在「下载」目录：

```bash
cd ~/Downloads/hair-salon-macos
```

（实际路径按你放置的位置调整。也可以在 Finder 里把文件夹**拖入终端窗口**自动补全路径。）

### 3. 给所有可执行文件加执行权限

```bash
chmod +x start.command stop.command backup.command hair-salon-server-darwin-*
```

只需执行一次，以后启动不再需要。

### 4. 移除 Gatekeeper 隔离属性

文件从网络下载/AirDrop/微信传输后，macOS 会打「下载隔离」标记，导致双击时
弹出「无法验证开发者」而无法打开。**在终端里**执行一次即可：

```bash
xattr -d com.apple.quarantine start.command stop.command backup.command hair-salon-server-darwin-*
```

- 没有任何输出 = 成功。
- 若提示 `No such xattr: com.apple.quarantine`，说明没有隔离属性，可忽略。
- 如果遗漏这一步直接双击，会看到「无法打开『hair-salon-server-darwin-arm64』
  因为无法验证开发者」或类似警告；回终端执行本命令后再双击即可。

> `start.command` 启动时会自动给二进制做二次去隔离与 Apple Silicon ad-hoc 签名；
> 但脚本**自己**被隔离时双击根本无法执行，所以第 4 步对三个 `.command`
> 脚本本身也必须执行。

### 5. 确认 config.yaml 中 JWT_SECRET 已填写

如果是全新解压、还没编辑过 `config.yaml`，打开它检查：

```yaml
JWT_SECRET: "一串随机字符"
```

- 留空时服务启动会直接报错「缺少必填配置项: JWT_SECRET」并退出。
- 打包脚本生成的示例文件里该字段为空，**手动部署时请填一个足够长的随机字符串**
  （任何字母数字组合均可，例如执行 `openssl rand -base64 48` 生成）。
- 初始管理员账号 `admin`，默认密码 `admin123`（首次登录后请立即修改）。

### 6. 启动服务

**方式一（推荐）**：在 Finder 里**双击 `start.command`**，系统会自动打开
Terminal 窗口并启动服务。看到「[信息] 服务已启动」即可。

**方式二**：在终端里执行

```bash
sh start.command
```

启动后不要关闭该 Terminal 窗口（服务在后台运行，但关闭窗口不影响后台进程；
要停止用 `stop.command`）。

---

## 访问

- **本机访问**：浏览器打开 <http://127.0.0.1:8080>
- **局域网访问**（店员手机连同一 Wi-Fi）：浏览器打开
  `http://<Mac 的局域网 IP>:8080`
  - 查询 Mac 的局域网 IP：**系统设置 → 网络 → Wi-Fi → 详细信息 → TCP/IP → IP 地址**，
    或在终端执行 `ifconfig | grep inet`。
  - **首次访问需在「系统设置 → 网络 → 防火墙」允许本程序接受传入连接**
    （否则手机连不上）。建议防火墙保持开启，仅放行本程序的 8080 端口。
- **默认管理员账号**：用户名 `admin`，密码 `admin123`（首次登录后请立即修改）。
- **网页标题会自动显示你设置的门店名称**（系统设置里修改店名后即时生效）。

---

## 日常操作

| 目的 | 操作 |
|---|---|
| 启动服务 | 双击 `start.command`，或 `sh start.command` |
| 停止服务 | 双击 `stop.command`，或 `sh stop.command`（优雅关闭，等待最多 10 秒） |
| 重启服务 | 先 `stop.command`，再 `start.command` |
| 查看日志 | `logs/server-console.log` |
| 立即备份 | 登录 Web → 备份页 → 立即备份（推荐）；或设置 `HAIR_SALON_TOKEN` 后 `sh backup.command` |
| 自动备份 | 每天 `BACKUP_TIME`（默认 23:00）自动备份，保留最近 7 份 |

---

## 升级

```text
备份 → 停止服务 → 替换程序 + web/dist → 保留 data/ 与 logs/ → 启动
```

1. 登录 Web 在备份页手动备份一次。
2. 双击 `stop.command` 停止服务。
3. 用新版本的 `hair-salon-server-darwin-*`、`web/dist/`、`start/stop/backup.command`
   覆盖旧文件。**切勿删除 `data/`、`logs/`、`config.yaml`**。
4. 新版本若更新了二进制，首次启动前可再执行一次：
   ```bash
   chmod +x hair-salon-server-darwin-*
   xattr -d com.apple.quarantine hair-salon-server-darwin-*
   ```
5. 双击 `start.command` 启动，程序会自动执行数据库迁移。
6. 浏览器访问 <http://127.0.0.1:8080/health>，打开页面即表示服务正常。

---

## 常见问题

### 1. 双击 `.command` 或二进制弹出「无法打开，因为无法验证开发者」

执行一次「首次运行」第 4 步的 `xattr -d` 命令即可。若依然拦截，在 Finder 里
**按住 Control 键点击**该文件 → 选「打开」→ 在警告框里再点一次「打开」，之后
永久放行。也可以在「系统设置 → 隐私与安全性」页面底部点「仍要打开」。

### 2. Apple Silicon 启动失败、日志里出现 `Killed: 9` 或 `signal: killed`

二进制缺少 ad-hoc 签名。`start.command` 启动时会自动签名；如果仍有问题，
手动执行：

```bash
codesign --force --deep --sign - hair-salon-server-darwin-arm64
```

Intel Mac 不需要此步骤。

### 3. 启动报错「缺少必填配置项: JWT_SECRET」

打开 `config.yaml`，填好 `JWT_SECRET`（非空随机字符串）后重启。

### 4. 启动报错「bind: address already in use」

8080 端口被其他程序占用。检查并关闭占用程序，或编辑 `config.yaml` 把
`SERVER_PORT` 改成其他端口（例如 8090），然后重启，访问时用新端口。

查找占用 8080 的进程：

```bash
lsof -i :8080
```

### 5. 手机连不上

- 手机和 Mac 必须在**同一个 Wi-Fi / 局域网**；
- Mac 的「系统设置 → 网络 → 防火墙」必须允许本程序接受传入连接；
- 公司/公共场所 Wi-Fi 通常开启了「AP 隔离」，设备之间无法互访；建议使用店内
  专用 Wi-Fi 路由器。

---

## 红线

- **SQLite 数据库 `data/hair-salon.db` 必须放在 Mac 本机本地磁盘**，严禁放在
  SMB / NFS / NAS / iCloud Drive / 网络共享目录（会导致数据库损坏）。
- **`data/` 目录必须保留**：升级只替换程序，绝不删除或覆盖 `data/`。
- **不要把 8080 端口直接暴露到公网**（例如路由器直接端口映射）。如需外网访问，
  请另行配置 HTTPS 反向代理和访问控制。
- **金额一律整数分**，数据已有事务与流水保证，不要手工修改数据库文件。
