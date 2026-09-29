# Sinh deploy/compose/.env với bí mật ngẫu nhiên cho Docker Compose Local/Dev.
# Dùng: .\scripts\gen-dev-secrets.ps1 [-Force]
# Không bao giờ commit .env (đã có trong .gitignore). Không in bí mật ra màn hình.
param([switch]$Force)
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$target = Join-Path $root 'deploy\compose\.env'
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
function New-Bytes([int]$n) { $b = New-Object byte[] $n; $rng.GetBytes($b); return ,$b }
# Mật khẩu dạng hex: an toàn khi ghép vào URL (postgres://…, amqp://…).
function New-Hex([int]$n) { return (-join ((New-Bytes $n) | ForEach-Object { $_.ToString('x2') })) }
function New-B64([int]$n) { return [Convert]::ToBase64String((New-Bytes $n)) }

if ((Test-Path $target) -and -not $Force) {
    # Bổ sung bí mật mới thêm sau (không đổi bí mật cũ, không cần down -v).
    $have = [System.IO.File]::ReadAllText($target)
    if ($have -notmatch '(?m)^AG_KC_CLIENT_SECRET=') {
        $add = "AG_KC_CLIENT_SECRET=$(New-Hex 32)`n"
        if (-not $have.EndsWith("`n")) { $add = "`n" + $add }
        [System.IO.File]::AppendAllText($target, $add, (New-Object System.Text.UTF8Encoding $false))
        Write-Host "Đã bổ sung AG_KC_CLIENT_SECRET vào $target (giá trị không in ra). Tạo lại container keycloak để nhập lại realm."
    }
    $rng.Dispose()
    Write-Host "Đã có $target — giữ nguyên (dùng -Force để sinh lại; khi đó chạy 'docker compose down -v' vì mật khẩu DB đã đổi)."
    # Chứng thư mTLS của bề mặt Internal (api-platform ↔ optimizer): sinh nếu còn thiếu.
    & (Join-Path $PSScriptRoot 'gen-dev-certs.ps1')
    exit 0
}

$lines = @(
    '# Sinh bởi scripts/gen-dev-secrets.ps1 — CHỈ cho Local/Dev. KHÔNG commit.'
    "PG_SUPERUSER_PASSWORD=$(New-Hex 24)"
    "AG_DB_APP_PASSWORD=$(New-Hex 24)"
    "AG_DB_PASSWORD_VAULT_APP=$(New-Hex 24)"
    "AMQP_PASSWORD=$(New-Hex 24)"
    "KEYCLOAK_ADMIN_PASSWORD=$(New-Hex 24)"
    "MINIO_ROOT_PASSWORD=$(New-Hex 24)"
    "S3_APP_SECRET_KEY=$(New-Hex 24)"
    "AG_SYNC_CURSOR_KEY=$(New-Hex 32)"
    "AG_VAULT_KEK='$(New-B64 32)'"
    "AG_VAULT_BLIND_KEY='$(New-B64 32)'"
    "AG_ANCHOR_SIGNING_KEY='$(New-B64 32)'"
    "AG_KC_CLIENT_SECRET=$(New-Hex 32)"
    '# Sao lưu (docker-compose.backup.yml): mã hóa kho pgBackRest, kho S3 thứ hai với danh tính ghi/đọc tách biệt.'
    "PGBACKREST_CIPHER_PASS=$(New-Hex 32)"
    "BACKUP_S3_ROOT_SECRET=$(New-Hex 24)"
    "S3_BACKUP_WRITER_SECRET=$(New-Hex 24)"
    "S3_RESTORE_READER_SECRET=$(New-Hex 24)"
)
$rng.Dispose()
# LF, UTF-8 không BOM — Docker Compose đọc được trên mọi nền tảng.
[System.IO.File]::WriteAllText($target, (($lines -join "`n") + "`n"), (New-Object System.Text.UTF8Encoding $false))
Write-Host "Đã ghi $target (giá trị không in ra)."
# Chứng thư mTLS của bề mặt Internal (api-platform ↔ optimizer) vào deploy/compose/.certs.
& (Join-Path $PSScriptRoot 'gen-dev-certs.ps1') -Force:$Force
