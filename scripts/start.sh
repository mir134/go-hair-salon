#!/bin/sh
# ============================================================
# 理发店客户管理系统 - macOS 启动脚本
# 放置位置：与 hair-salon-server-darwin-amd64 / -arm64 同一目录（部署目录）
#          web/dist 也必须位于本目录下（服务启动时自动托管静态页面）
#
# 首次使用（08-DEPLOYMENT.md:49 首次运行需要处理 macOS 执行权限）：
#   chmod +x start.sh stop.sh backup.sh
#   chmod +x hair-salon-server-darwin-amd64 hair-salon-server-darwin-arm64
#   若从网络下载/拷贝后被 Gatekeeper 拦截（未签名二进制）：
#     xattr -d com.apple.quarantine hair-salon-server-darwin-*
#   或在 Finder 中右键 -> 打开，并在“系统设置 -> 隐私与安全性”中允许。
#
# 停止：./stop.sh（发送 SIGTERM，服务优雅关闭并关闭数据库连接）
# ============================================================
set -eu

APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$APP_DIR"

case "$(uname -m)" in
  arm64|aarch64) BIN="hair-salon-server-darwin-arm64" ;;
  x86_64|amd64)  BIN="hair-salon-server-darwin-amd64" ;;
  *) echo "[错误] 不支持的 CPU 架构: $(uname -m)" >&2; exit 1 ;;
esac

if [ ! -f "$BIN" ]; then
  echo "[错误] 未找到 $BIN：请把本脚本与对应架构的可执行文件放在同一目录。" >&2
  exit 1
fi
if [ ! -x "$BIN" ]; then
  echo "[错误] $BIN 没有执行权限，请先运行：" >&2
  echo "       chmod +x $BIN start.sh stop.sh backup.sh" >&2
  exit 1
fi

if [ ! -f "config.yaml" ]; then
  if [ -f "config.example.yaml" ]; then
    cp config.example.yaml config.yaml
    echo "[提示] 已从 config.example.yaml 生成 config.yaml。"
    echo "[错误] 请先编辑 config.yaml 填写 JWT_SECRET，然后重新运行 ./start.sh。" >&2
    exit 1
  fi
  echo "[错误] 未找到 config.yaml，也没有 config.example.yaml 模板。" >&2
  exit 1
fi

if [ ! -f "web/dist/index.html" ]; then
  echo "[警告] 未找到 web/dist/index.html：网页界面不可用（仅 API）。"
  echo "        请把前端构建产物 web/dist 复制到本目录下，与可执行文件同级。"
fi

PID_FILE="hair-salon-server.pid"
if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "[错误] 服务已在运行，PID $(cat "$PID_FILE")；如需重启请先执行 ./stop.sh。" >&2
  exit 1
fi

mkdir -p logs data/backups data/uploads
echo "[提示] 首次启动前请确认 config.yaml 已填写 JWT_SECRET，留空时服务会拒绝启动并提示字段。"
echo "[信息] 数据目录 data/ 仅可放本机磁盘，禁止 SMB/NFS 网络共享；备份 data/backups/；日志 logs/"
echo "[信息] 本机访问 http://127.0.0.1:8080"
echo "[信息] 局域网访问 http://本机局域网IP:8080，首次运行需在“系统设置 -> 网络 -> 防火墙”放行 8080"
echo "[信息] 控制台输出重定向到 logs/server-console.log"

nohup "./$BIN" >> logs/server-console.log 2>&1 &
echo $! > "$PID_FILE"
echo "[信息] 服务已启动，PID $(cat "$PID_FILE")；停止请执行 ./stop.sh"
