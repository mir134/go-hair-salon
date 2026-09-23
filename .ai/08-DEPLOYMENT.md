# 部署目标

系统运行在美发店自己的 Windows 11 或 macOS 电脑上，电脑作为局域网服务器，手机和其他电脑通过同一 Wi-Fi/LAN 访问。

# 1. 推荐部署模型

单一 Go 可执行程序：Go 后端、Vue 前端静态文件、SQLite 数据库、uploads、logs、config。

推荐目录（构建产物目录，与源码目录 server/ + web/ 区分）：

```
hair-salon/
├── hair-salon-server(.exe)   # 单一可执行程序
├── web/dist                  # 前端构建产物（由 server 托管）
├── data/
│   ├── hair-salon.db         # SQLite 数据库
│   ├── backups/
│   └── uploads/
├── logs/
└── config.yaml
```

Windows 使用 hair-salon-server.exe；macOS 使用对应架构的可执行文件。

# 2. 网络

默认监听 0.0.0.0:8080。

本机访问：http://127.0.0.1:8080

局域网访问：http://<服务器局域网IP>:8080

手机必须与服务器电脑处于同一局域网，或通过明确配置的网络通道访问。

# 3. Windows 11

MVP 支持直接双击/命令行启动。

建议提供 start.bat、stop.bat、backup.bat。

生产使用时可选注册为 Windows 服务，但不作为 MVP 必需项。

防火墙只允许局域网访问 8080，不建议默认暴露公网。

# 4. macOS

提供 start.sh、stop.sh、backup.sh。

首次运行需要处理 macOS 执行权限。

可选使用 launchd 自动启动，但 MVP 不强制。

# 5. SQLite 规则

- 数据库必须位于本机磁盘。
- 不允许把 SQLite 主数据库放在 SMB/NAS/网络共享目录。
- 启用 WAL。
- 设置 busy_timeout。
- 程序启动时执行必要的数据库迁移。
- 正常退出尽量关闭数据库连接。

# 6. 数据目录

配置项至少包括：DB_PATH、UPLOAD_DIR、BACKUP_DIR、LOG_DIR、SERVER_HOST、SERVER_PORT、JWT_SECRET。

配置文件统一命名为 `config.yaml`（对应 05-TASKS.md P0 工程骨架）。

敏感配置不要硬编码进源码。

# 7. 备份

默认自动备份：每天至少 1 次，保留最近 7 份。

备份内容：SQLite 数据库、uploads、必要配置。

备份文件必须带时间戳。

执行恢复前：停止业务写入 → 自动创建当前数据的安全备份 → 验证待恢复文件 → 用户明确确认 → 恢复 → 启动并执行数据检查。

# 8. 升级

程序文件可以替换，但 data/ 必须保留。

升级步骤：备份 → 停止服务 → 替换程序 → 执行数据库迁移 → 启动 → 健康检查。

数据库迁移必须向前兼容当前版本，不允许开发者直接手工修改生产 SQLite 文件。

# 9. 日志与排障

至少记录：启动/停止、数据库错误、HTTP 500、认证失败、账务操作、备份/恢复、数据库迁移。

日志不得记录：JWT 完整 token、密码、不必要的敏感客户信息。

提供 GET /health 用于判断程序是否正常运行。

# 10. 安全边界

MVP 默认定位为局域网系统：JWT 鉴权、密码哈希存储、后端 RBAC、关键操作日志、8080 默认不直接暴露公网。

如果未来需要互联网访问，应增加 HTTPS、反向代理、访问控制、备份策略和更严格的安全设计，而不是直接把 8080 映射到公网。

# 11. 交付验收

在 Windows 11 和 macOS 至少分别验证：程序启动、浏览器访问、管理员登录、手机同 Wi-Fi 访问、新增客户、消费、充值、查询余额、查看流水、备份、恢复、重启后数据仍存在。

# 12. 非 MVP 运维能力

暂不强制：Docker、Kubernetes、Redis、PostgreSQL、云监控、自动在线升级、公网部署、多门店同步