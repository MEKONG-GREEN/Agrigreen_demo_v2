# CHỈ Local/Dev: tạo người dùng demo trong realm agrigreen của Keycloak Docker Compose.
#
# Dùng: .\scripts\dev-users.ps1 [-TenantId <uuid>] [-OrgRef <uuid>] [-KeycloakUrl http://localhost:8180]
#
# - Gọi Keycloak Admin REST bằng tài khoản bootstrap "admin" (mật khẩu KEYCLOAK_ADMIN_PASSWORD
#   trong deploy/compose/.env). Không có mật khẩu nào nằm trong repo hay trong tệp realm.
# - Mỗi người dùng nhận mật khẩu NGẪU NHIÊN, in ra màn hình MỘT LẦN duy nhất. Lưu ngay vào
#   trình quản lý mật khẩu; chạy lại script không in lại (người dùng đã có thì bỏ qua).
# - Thuộc tính tenant_id (mặc định sinh mới) và org_ref (tùy chọn: UUID tổ chức trong pf/party).
# - Vai [MFA] → required action CONFIGURE_TOTP: đăng nhập lần đầu qua trình duyệt
#   (http://localhost:3000) để quét mã TOTP trước khi dùng Admin API hoặc client agrigreen-seed.
# Không bao giờ chạy với Keycloak staging/production.
param(
    [string]$TenantId = ([guid]::NewGuid().ToString()),
    [string]$OrgRef = '',
    [string]$KeycloakUrl = 'http://localhost:8180',
    [string]$Realm = 'agrigreen'
)
$ErrorActionPreference = 'Stop'

if ($env:AG_ENV -match '^(prod|production)$') { throw 'AG_ENV=production — từ chối tạo người dùng demo.' }
if ($KeycloakUrl -notmatch '^http://(localhost|127\.0\.0\.1)(:\d+)?$') { throw 'Chỉ chạy với Keycloak Local/Dev (http://localhost:<cổng>).' }

$root = Split-Path -Parent $PSScriptRoot
$envFile = Join-Path $root 'deploy\compose\.env'
if (-not (Test-Path $envFile)) { throw "Không thấy $envFile — chạy .\scripts\gen-dev-secrets.ps1 trước." }
$line = Get-Content $envFile | Where-Object { $_ -match '^KEYCLOAK_ADMIN_PASSWORD=' } | Select-Object -First 1
if (-not $line) { throw 'deploy/compose/.env thiếu KEYCLOAK_ADMIN_PASSWORD' }
$adminPw = $line.Substring('KEYCLOAK_ADMIN_PASSWORD='.Length).Trim("'", '"')

$tok = Invoke-RestMethod -Method Post -Uri "$KeycloakUrl/realms/master/protocol/openid-connect/token" `
    -ContentType 'application/x-www-form-urlencoded' `
    -Body @{ grant_type = 'password'; client_id = 'admin-cli'; username = 'admin'; password = $adminPw }
$h = @{ Authorization = "Bearer $($tok.access_token)" }
$api = "$KeycloakUrl/admin/realms/$Realm"

$mfaRoles = @('platform_admin', 'param_setter', 'param_approver', 'masterdata_admin', 'registry_admin', 'cgh_admin',
    'geo_admin', 'device_admin', 'sync_conflict_resolver', 'vault_eraser', 'account_admin', 'account_admin_province')
$demo = @(
    @{ u = 'dev-admin'; first = 'Quản trị'; last = 'Dev'; roles = @('platform_admin', 'masterdata_admin', 'registry_admin', 'vault_registrar', 'kn_province_admin', 'cgh_admin', 'account_admin') },
    @{ u = 'dev-accounts'; first = 'Tài khoản'; last = 'Dev'; roles = @('account_admin') },
    @{ u = 'dev-viewer'; first = 'Cục KTHT'; last = 'Dev'; roles = @('cgh_viewer') }
)

$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
function New-Password {
    $chars = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
    $b = New-Object byte[] 20; $rng.GetBytes($b)
    return (-join ($b | ForEach-Object { $chars[$_ % $chars.Length] })) + '#7'
}

Write-Host "tenant_id = $TenantId"
foreach ($d in $demo) {
    $exists = Invoke-RestMethod -Headers $h -Uri "$api/users?username=$($d.u)&exact=true"
    if ($exists.Count -gt 0) { Write-Host "$($d.u): đã có — bỏ qua (mật khẩu không in lại)"; continue }
    $pw = New-Password
    $attrs = @{ tenant_id = @($TenantId) }
    if ($OrgRef) { $attrs.org_ref = @($OrgRef) }
    $actions = @()
    if ($d.roles | Where-Object { $mfaRoles -contains $_ }) { $actions += 'CONFIGURE_TOTP' }
    $body = @{
        username = $d.u; email = "$($d.u)@dev.agrigreen.local"; emailVerified = $true; enabled = $true
        firstName = $d.first; lastName = $d.last; attributes = $attrs; requiredActions = $actions
        credentials = @(@{ type = 'password'; value = $pw; temporary = $false })
    } | ConvertTo-Json -Depth 5
    $res = Invoke-WebRequest -UseBasicParsing -Method Post -Headers $h -Uri "$api/users" -ContentType 'application/json; charset=utf-8' `
        -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
    $id = ($res.Headers['Location'] -split '/')[-1]
    $roleReps = @($d.roles | ForEach-Object { Invoke-RestMethod -Headers $h -Uri "$api/roles/$_" | Select-Object id, name })
    Invoke-RestMethod -Method Post -Headers $h -Uri "$api/users/$id/role-mappings/realm" -ContentType 'application/json' `
        -Body (ConvertTo-Json -InputObject $roleReps -Depth 3) | Out-Null
    $totp = ''
    if ($actions -contains 'CONFIGURE_TOTP') { $totp = ' — cần cấu hình TOTP ở lần đăng nhập đầu' }
    Write-Host "$($d.u) / $pw  (vai: $($d.roles -join ', '))$totp"
}
$rng.Dispose()
Write-Host 'Mật khẩu trên chỉ in MỘT LẦN. Không dán vào issue, chat hay commit.'
