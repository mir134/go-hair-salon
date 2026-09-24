#!/bin/sh
# ============================================================
# 理发店客户管理系统 - macOS 手动备份脚本（调用后端 API）
# 备份 API 仅管理员可用，需要登录令牌：
#   export HAIR_SALON_TOKEN=<管理员登录响应中的 data.token>
#   ./backup.command
# 可用 HAIR_SALON_PORT 覆盖端口（默认 8080）。
# 未设置令牌时不执行任何操作，只打印手动备份的替代路径。
# 说明：系统每天 BACKUP_TIME（默认 23:00）自动备份一次，保留最近 7 份，
#      备份文件位于 data/backups/。
# 首次使用：chmod +x backup.command
# ============================================================
set -u

APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$APP_DIR"

PORT="${HAIR_SALON_PORT:-8080}"

if [ -z "${HAIR_SALON_TOKEN:-}" ]; then
  echo "[提示] 未设置 HAIR_SALON_TOKEN，未执行 API 调用。"
  echo "        替代路径：登录 Web 界面，在“备份”页面点击“手动备份”。"
  echo "        或执行 export HAIR_SALON_TOKEN=<令牌> 后重试。"
  echo "        自动备份：每天 BACKUP_TIME 时间点备份一次，保留最近 7 份。"
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "[错误] 未找到 curl，请改用 Web 界面的手动备份。" >&2
  exit 1
fi

echo "[信息] 正在请求 http://127.0.0.1:$PORT/api/v1/backups ..."
if curl -fsS -X POST -H "Authorization: Bearer $HAIR_SALON_TOKEN" "http://127.0.0.1:$PORT/api/v1/backups"; then
  echo
  echo "[信息] 备份完成，文件位于 data/backups/。"
  exit 0
fi
echo
echo "[错误] 备份请求失败：服务未启动、令牌无效或账号非管理员。" >&2
exit 1
