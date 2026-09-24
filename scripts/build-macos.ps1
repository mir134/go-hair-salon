<#
.SYNOPSIS
  打包 macOS 部署目录到 deploy/hair-salon-macos。
.DESCRIPTION
  在 Windows 上用 PowerShell 完成：前端构建 → 组装目录 → darwin/amd64 + darwin/arm64
  交叉编译 → 产物核验（Mach-O 魔数）。产物结构与 scripts/start.command 的约定一致
  （二进制、web/dist、config.yaml、data/、logs/ 同级）。
  仅生成构建产物；不改动源码、不接触生产数据库；deploy/ 已被 .gitignore 忽略，不入 git。
.PARAMETER OutDir
  输出目录，默认 <仓库根>/deploy/hair-salon-macos。
.PARAMETER ForceConfig
  用 config.example.yaml 覆盖已存在的 config.yaml（默认不覆盖，避免覆盖已填写的 JWT_SECRET）。
.EXAMPLE
  # 仓库根执行
  powershell -ExecutionPolicy Bypass -File scripts\build-macos.ps1
#>
[CmdletBinding()]
param(
  [string]$OutDir,
  [switch]$ForceConfig
)

$ErrorActionPreference = 'Stop'
function Info([string]$m) { Write-Host $m -ForegroundColor Cyan }
function Warn([string]$m) { Write-Host $m -ForegroundColor Yellow }

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $root 'deploy\hair-salon-macos' }

# 1) 前端构建
Info "[1/5] 前端构建：web/  (npm run build)"
Push-Location (Join-Path $root 'web')
try {
  npm run build
  if ($LASTEXITCODE -ne 0) { throw "npm run build 失败（exit=$LASTEXITCODE）" }
} finally { Pop-Location }

# 2) 组装目录
Info "[2/5] 组装目录：$OutDir"
$webDir = Join-Path $OutDir 'web'
$distDir = Join-Path $webDir 'dist'
New-Item -ItemType Directory -Force -Path $webDir, (Join-Path $OutDir 'data\backups'), (Join-Path $OutDir 'data\uploads'), (Join-Path $OutDir 'logs') | Out-Null
if (Test-Path $distDir) { Remove-Item -Recurse -Force $distDir }   # 清旧产物，避免残留过期 hash 资源
Copy-Item -Path (Join-Path $root 'web\dist') -Destination $distDir -Recurse -Force

$configPath = Join-Path $OutDir 'config.yaml'
if ($ForceConfig -or -not (Test-Path $configPath)) {
  Copy-Item -Path (Join-Path $root 'server\config.example.yaml') -Destination $configPath -Force
  Warn "  已生成 config.yaml —— 请填写 JWT_SECRET（留空会导致启动失败）"
} else {
  Write-Host "  保留既有 config.yaml（-ForceConfig 可强制覆盖）"
}

# 启动/停止/备份脚本需与二进制同目录（start.command 的约定；双击可运行）
foreach ($s in 'start.command', 'stop.command', 'backup.command') {
  Copy-Item -Path (Join-Path $root "scripts\$s") -Destination (Join-Path $OutDir $s) -Force
}
# 部署说明文档（README.macos.md 是源文件，部署目录重命名为 README.md，双击查看）
Copy-Item -Path (Join-Path $root 'scripts\README.macos.md') -Destination (Join-Path $OutDir 'README.md') -Force

# 3) 交叉编译双架构
Info "[3/5] 交叉编译 darwin/amd64 + darwin/arm64 (CGO_ENABLED=0)"
Push-Location (Join-Path $root 'server')
try {
  $env:CGO_ENABLED = '0'; $env:GOOS = 'darwin'
  $env:GOARCH = 'amd64'
  go build -o (Join-Path $OutDir 'hair-salon-server-darwin-amd64') ./cmd/server
  if ($LASTEXITCODE -ne 0) { throw "darwin/amd64 构建失败（exit=$LASTEXITCODE）" }
  $env:GOARCH = 'arm64'
  go build -o (Join-Path $OutDir 'hair-salon-server-darwin-arm64') ./cmd/server
  if ($LASTEXITCODE -ne 0) { throw "darwin/arm64 构建失败（exit=$LASTEXITCODE）" }
} finally {
  Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
  Pop-Location
}

# 4) 产物核验
Info "[4/5] 产物核验（Mach-O 魔数应为 CFFAEDFE）"
$machOMagic = 'CFFAEDFE'
foreach ($name in 'hair-salon-server-darwin-amd64', 'hair-salon-server-darwin-arm64') {
  $p = Join-Path $OutDir $name
  if (-not (Test-Path $p)) { throw "缺少产物：$name" }
  $bytes = [System.IO.File]::ReadAllBytes($p)
  $magic = '{0:X2}{1:X2}{2:X2}{3:X2}' -f $bytes[0], $bytes[1], $bytes[2], $bytes[3]
  if ($magic -ne $machOMagic) { throw "$name 魔数 $magic ≠ Mach-O（$machOMagic）" }
  Write-Host ("  {0}  {1,12:N0} B  magic={2}" -f $name, (Get-Item $p).Length, $magic)
}
if (-not (Test-Path (Join-Path $distDir 'index.html'))) { throw "缺少 web/dist/index.html" }

# 5) 完成
Info "[5/5] 完成：$OutDir"
Get-ChildItem $OutDir | Select-Object Name, Length | Format-Table -AutoSize | Out-String | Write-Host
Write-Host "在 macOS 上首次运行（详细说明见 README.md）：" -ForegroundColor Green
Write-Host "  chmod +x start.command stop.command backup.command hair-salon-server-darwin-*"
Write-Host "  xattr -d com.apple.quarantine start.command stop.command backup.command hair-salon-server-darwin-*"
Write-Host "  双击 start.command 启动（或 sh start.command）"
