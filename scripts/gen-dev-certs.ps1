# Sinh chứng thư mTLS cho bề mặt Internal ở Docker Compose Local/Dev (mục 11) — bản
# PowerShell của scripts/gen-dev-certs.sh, cùng tệp đầu ra trong deploy/compose/.certs/
# (đã gitignore — không bao giờ commit):
#   ca.crt / ca.key            CA dev tự ký (ca.key không gắn vào container nào)
#   api-platform.crt / .key    máy chủ listener Internal (SAN api-platform, localhost, 127.0.0.1)
#   optimizer.crt / .key       client của optimizer (CN = SAN = optimizer)
# Dùng: .\scripts\gen-dev-certs.ps1 [-Force]   (gen-dev-secrets.ps1 tự gọi)
# Cần openssl: trong PATH hoặc đi kèm Git for Windows. CHỈ cho Local/Dev.
param([switch]$Force)
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$dir = Join-Path $root 'deploy\compose\.certs'
$need = 'ca.crt', 'api-platform.key', 'optimizer.key'
if (-not $Force -and -not ($need | Where-Object { -not (Test-Path (Join-Path $dir $_)) })) {
    Write-Host "Đã có chứng thư dev trong $dir — giữ nguyên (dùng -Force để sinh lại)."
    exit 0
}

$openssl = (Get-Command openssl -ErrorAction SilentlyContinue).Source
if (-not $openssl) {
    $git = (Get-Command git -ErrorAction SilentlyContinue).Source
    $cands = @()
    if ($git) { $gitRoot = Split-Path -Parent (Split-Path -Parent $git); $cands += (Join-Path $gitRoot 'mingw64\bin\openssl.exe'), (Join-Path $gitRoot 'usr\bin\openssl.exe') }
    $cands += "$env:ProgramFiles\Git\mingw64\bin\openssl.exe", "$env:ProgramFiles\Git\usr\bin\openssl.exe"
    $openssl = $cands | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $openssl) { throw 'cần openssl (PATH hoặc Git for Windows)' }

function Invoke-OpenSSL {
    # PowerShell 5.1: stderr của lệnh ngoài thành ErrorRecord — chỉ xét mã thoát.
    $ErrorActionPreference = 'Continue'
    & $openssl @args 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "openssl $($args[0]) thất bại (mã $LASTEXITCODE)" }
}

New-Item -ItemType Directory -Force $dir | Out-Null
$tmp = Join-Path $dir ".tmp.$PID"
New-Item -ItemType Directory -Force $tmp | Out-Null
Push-Location $dir
try {
    Invoke-OpenSSL genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out ca.key
    Invoke-OpenSSL req -x509 -new -key ca.key -sha256 -days 3650 -subj '/O=AgriGreen Local-Dev/CN=agrigreen-dev-internal-ca' `
        -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' -addext 'keyUsage=critical,keyCertSign,cRLSign' -out ca.crt
    $t = Split-Path -Leaf $tmp
    foreach ($leaf in @(
            @{ Name = 'api-platform'; Usage = 'serverAuth'; San = 'DNS:api-platform,DNS:localhost,IP:127.0.0.1' },
            @{ Name = 'optimizer'; Usage = 'clientAuth'; San = 'DNS:optimizer' })) {
        $n = $leaf.Name
        Invoke-OpenSSL genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$n.key"
        Invoke-OpenSSL req -new -key "$n.key" -subj "/O=AgriGreen Local-Dev/CN=$n" -out "$t/$n.csr"
        $ext = "basicConstraints=critical,CA:FALSE`nkeyUsage=critical,digitalSignature`nextendedKeyUsage=$($leaf.Usage)`nsubjectAltName=$($leaf.San)`n"
        [System.IO.File]::WriteAllText((Join-Path $tmp "$n.ext"), $ext, (New-Object System.Text.UTF8Encoding $false))
        Invoke-OpenSSL x509 -req -in "$t/$n.csr" -CA ca.crt -CAkey ca.key -CAcreateserial -CAserial "$t/ca.srl" `
            -days 825 -sha256 -extfile "$t/$n.ext" -out "$n.crt"
    }
}
finally {
    Pop-Location
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
Write-Host "Đã ghi chứng thư mTLS dev vào $dir (khóa không in ra)."
