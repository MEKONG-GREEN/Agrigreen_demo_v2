# Đặt biến môi trường cho kiểm thử tích hợp trên máy Windows dùng PostgreSQL
# cài cục bộ (không Docker). Dùng: . .\scripts\dev-env.ps1
#
# Mật khẩu superuser đọc từ tệp ngoài repo (mặc định %LOCALAPPDATA%\agrigreen-pg\superuser.pw).
# Không commit mật khẩu thật vào repo.
param(
  [string]$PgRoot = "$env:LOCALAPPDATA\agrigreen-pg",
  [int]$Port = 5432
)
$goBin = "$env:LOCALAPPDATA\Programs\go\bin"
if (Test-Path $goBin) { $env:Path = "$goBin;$env:Path" }
$pwFile = Join-Path $PgRoot "superuser.pw"
if (-not (Test-Path $pwFile)) { throw "Không thấy $pwFile — xem docs/dev-setup.md" }
$pw = [IO.File]::ReadAllText($pwFile).Trim()
$env:AG_TEST_PG_ADMIN_URL = "postgres://postgres:$pw@localhost:$Port/postgres?sslmode=disable"
if (-not $env:AG_DB_APP_PASSWORD) { $env:AG_DB_APP_PASSWORD = "agtest-local-only" }
Write-Host "Đã đặt AG_TEST_PG_ADMIN_URL (localhost:$Port) và AG_DB_APP_PASSWORD cho kiểm thử."
