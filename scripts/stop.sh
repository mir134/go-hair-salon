#!/bin/sh
# ============================================================
# 理发店客户管理系统 - macOS 停止脚本
# 只停止本目录下启动的 hair-salon-server：
#   1) 优先读取 start.sh 写入的 hair-salon-server.pid，并校验该 PID 的
#      可执行文件名确实是本程序的二进制（防止 PID 复用误杀无关进程）；
#   2) PID 文件缺失/过期时，按“可执行文件名 + 完整路径”匹配本目录下的二进制。
# 先发送 SIGTERM（服务优雅关闭、关闭数据库连接），10 秒未退出再 SIGKILL。
# 前台运行（Ctrl+C 启动）的场景请直接在该终端按 Ctrl+C。
# 首次使用：chmod +x stop.sh
# ============================================================
set -u

APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$APP_DIR"
PID_FILE="hair-salon-server.pid"

case "$(uname -m)" in
  arm64|aarch64) BIN="hair-salon-server-darwin-arm64" ;;
  x86_64|amd64)  BIN="hair-salon-server-darwin-amd64" ;;
  *) BIN="" ;;
esac

# pid_belongs_to_us 判断 PID 的可执行文件名是否为本程序二进制（拒绝误杀）。
pid_belongs_to_us() {
  [ -n "$BIN" ] || return 1
  COMM=$(ps -o comm= -p "$1" 2>/dev/null || true)
  case "$COMM" in
    "$APP_DIR/$BIN"|*"/$BIN"|"./$BIN"|"$BIN") return 0 ;;
    *) return 1 ;;
  esac
}

PIDS=""
if [ -f "$PID_FILE" ]; then
  PID=$(cat "$PID_FILE" 2>/dev/null || true)
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null && pid_belongs_to_us "$PID"; then
    PIDS="$PID"
  else
    echo "[提示] PID 文件已过期或指向其他程序，已清理：$PID_FILE"
    rm -f "$PID_FILE"
  fi
fi

if [ -z "$PIDS" ] && [ -n "$BIN" ]; then
  # 兜底：按可执行文件名匹配（二进制名唯一），路径含空格也可正确处理。
  PIDS=$(ps -ax -o pid=,comm= 2>/dev/null | awk -v base="$BIN" '{ line=$0; sub(/^[ \t]*[0-9]+[ \t]+/, "", line); n=split(line, a, "/"); if (a[n] == base) printf "%s ", $1 }')
fi

if [ -z "$PIDS" ]; then
  echo "[信息] hair-salon-server 未在运行，无需停止。"
  exit 0
fi

for PID in $PIDS; do
  echo "[信息] 正在停止 PID $PID ..."
  kill -TERM "$PID" 2>/dev/null || true
done

WAIT=10
while [ "$WAIT" -gt 0 ]; do
  ALIVE=""
  for PID in $PIDS; do
    if kill -0 "$PID" 2>/dev/null; then ALIVE="$ALIVE $PID"; fi
  done
  [ -z "$ALIVE" ] && break
  WAIT=$((WAIT - 1))
  sleep 1
done

for PID in $PIDS; do
  if kill -0 "$PID" 2>/dev/null; then
    echo "[提示] PID $PID 未在 10 秒内退出，发送 SIGKILL。"
    kill -KILL "$PID" 2>/dev/null || true
  fi
done

rm -f "$PID_FILE"
echo "[信息] 已停止。"
exit 0
