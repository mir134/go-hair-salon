#!/usr/bin/env sh
# 打包 macOS 部署目录到 deploy/hair-salon-macos（在 macOS / Linux 上运行）。
# 与 scripts/build-macos.ps1 等价：前端构建 → 组装 → darwin amd64/arm64 编译 → 核验。
#
# 用法：
#   sh scripts/build-macos.sh            # 输出到 <仓库根>/deploy/hair-salon-macos
#   sh scripts/build-macos.sh /tmp/out   # 自定义输出目录
#
# 说明：仅生成构建产物，不改动源码、不接触生产数据库；deploy/ 已被 .gitignore 忽略，不入 git。
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT="${1:-$ROOT/deploy/hair-salon-macos}"

echo "[1/5] 前端构建：web/"
(cd "$ROOT/web" && npm run build)

echo "[2/5] 组装目录：$OUT"
mkdir -p "$OUT/web" "$OUT/data/backups" "$OUT/data/uploads" "$OUT/logs"
rm -rf "$OUT/web/dist"
cp -R "$ROOT/web/dist" "$OUT/web/dist"
if [ ! -f "$OUT/config.yaml" ]; then
  cp "$ROOT/server/config.example.yaml" "$OUT/config.yaml"
  echo "  已生成 config.yaml —— 请填写 JWT_SECRET（留空会导致启动失败）"
else
  echo "  保留既有 config.yaml"
fi
# 启动/停止/备份脚本需与二进制同目录（start.command 的约定；双击可运行）
cp "$ROOT/scripts/start.command" "$ROOT/scripts/stop.command" "$ROOT/scripts/backup.command" "$OUT/"
# 部署说明文档（README.macos.md 是源文件，部署目录重命名为 README.md，双击查看）
cp "$ROOT/scripts/README.macos.md" "$OUT/README.md"

echo "[3/5] 编译 darwin/amd64 + darwin/arm64 (CGO_ENABLED=0)"
(
  cd "$ROOT/server"
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$OUT/hair-salon-server-darwin-amd64" ./cmd/server
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$OUT/hair-salon-server-darwin-arm64" ./cmd/server
)

echo "[4/5] 核验（Mach-O 魔数应为 cffaedfe）"
for b in hair-salon-server-darwin-amd64 hair-salon-server-darwin-arm64; do
  f="$OUT/$b"
  [ -f "$f" ] || { echo "缺少产物：$b" >&2; exit 1; }
  magic=$(od -An -tx1 -N4 "$f" | tr -d ' \n')
  [ "$magic" = "cffaedfe" ] || { echo "$b 魔数 $magic ≠ Mach-O" >&2; exit 1; }
  echo "  $b  $(wc -c < "$f") B  magic=$magic"
done
[ -f "$OUT/web/dist/index.html" ] || { echo "缺少 web/dist/index.html" >&2; exit 1; }

echo "[5/5] 完成：$OUT"
ls -la "$OUT"
echo
echo "在 macOS 上首次运行（详细说明见 README.md）："
echo "  chmod +x start.command stop.command backup.command hair-salon-server-darwin-*"
echo "  xattr -d com.apple.quarantine start.command stop.command backup.command hair-salon-server-darwin-*"
echo "  双击 start.command 启动（或 sh start.command）"
