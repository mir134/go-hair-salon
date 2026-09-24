<#
.SYNOPSIS
  打包 Windows 部署目录到 deploy/hair-salon。
.DESCRIPTION
  在 Windows 上完成：前端构建 → 组装目录 → windows/amd64 编译 → 产物核验（PE 魔数 MZ）。
  产物结构与 scripts/start.bat 的约定一致
  （hair-salon-server.exe、web\dist、config.yaml、data\、logs\ 同级）。
  仅生成构建产物；不改动源码、不接触生产数据库；deploy/ 已被 .gitignore 忽略，不入 git。
.PARAMETER OutDir
  输出目录，默认 <仓库根>/deploy/hair-salon。
.PARAMETER ForceConfig
  用 config.example.yaml 覆盖已存在的 config.yaml（默认不覆盖，避免覆盖已填写的 JWT_SECRET）。
.EXAMPLE
  # 仓库根执行
  powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
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
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $root 'deploy\hair-salon' }

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
# 部署目录附一份示例配置（start.bat 在缺 config.yaml 时用它生成）
Copy-Item -Path (Join-Path $root 'server\config.example.yaml') -Destination (Join-Path $OutDir 'config.example.yaml') -Force

# 启停/备份脚本需与 exe 同目录（start.bat 的约定）
foreach ($s in 'start.bat', 'stop.bat', 'backup.bat') {
  Copy-Item -Path (Join-Path $root "scripts\$s") -Destination (Join-Path $OutDir $s) -Force
}

# 3) 编译 windows/amd64
Info "[3/5] 编译 windows/amd64 (CGO_ENABLED=0)"
Push-Location (Join-Path $root 'server')
try {
  $env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
  go build -o (Join-Path $OutDir 'hair-salon-server.exe') ./cmd/server
  if ($LASTEXITCODE -ne 0) { throw "windows/amd64 构建失败（exit=$LASTEXITCODE）" }
} finally {
  Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
  Pop-Location
}

# 4) 产物核验
Info "[4/5] 产物核验（PE 魔数应为 4D5A = MZ）"
$exe = Join-Path $OutDir 'hair-salon-server.exe'
if (-not (Test-Path $exe)) { throw "缺少产物：hair-salon-server.exe" }
$bytes = [System.IO.File]::ReadAllBytes($exe)
$magic = '{0:X2}{1:X2}' -f $bytes[0], $bytes[1]
if ($magic -ne '4D5A') { throw "hair-salon-server.exe 魔数 $magic ≠ PE（4D5A）" }
Write-Host ("  {0}  {1,12:N0} B  magic={2}" -f 'hair-salon-server.exe', (Get-Item $exe).Length, $magic)
if (-not (Test-Path (Join-Path $distDir 'index.html'))) { throw "缺少 web/dist/index.html" }

# 5) 完成
Info "[5/5] 完成：$OutDir"
Get-ChildItem $OutDir | Select-Object Name, Length | Format-Table -AutoSize | Out-String | Write-Host
Write-Host "运行：" -ForegroundColor Green
Write-Host "  1) 编辑 config.yaml 填写 JWT_SECRET（空值会导致启动失败）"
Write-Host "  2) 双击 start.bat 启动（停止用 stop.bat）"
Write-Host "  3) 首次按需放行防火墙 TCP 8080（仅专用网络）"
