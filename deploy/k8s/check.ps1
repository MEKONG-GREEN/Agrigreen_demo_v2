# Kiểm chart Kubernetes G4 (docs/runbooks/k8s-deploy.md mục 8):
#   1. helm lint --strict cho dev/staging/prod
#   2. helm template → kubeconform -strict (nếu có; schema CRD từ catalog datreeio + schema Stanza cục bộ)
#   3. deploy/k8s/tests/check_flows.py — bảng luồng ADR-0008, luồng cấm, cứng hóa, bí mật theo vùng
# Dùng: powershell -File deploy/k8s/check.ps1 [-Offline]
#   -Offline: kubeconform chỉ dùng schema có sẵn, bỏ qua CRD không có schema (không tải mạng).
param([switch]$Offline)
$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$chart = Join-Path $root "deploy\k8s\charts\agrigreen"
$helm = $env:HELM
if (-not $helm) { $c = Get-Command helm -ErrorAction SilentlyContinue; if ($c) { $helm = $c.Source } }
if (-not $helm) { $helm = Join-Path $env:LOCALAPPDATA "Programs\helm\helm.exe" }
if (-not (Test-Path $helm)) { throw "không tìm thấy helm — cài theo docs/runbooks/k8s-deploy.md mục 1" }
$kc = $env:KUBECONFORM
if (-not $kc) { $c = Get-Command kubeconform -ErrorAction SilentlyContinue; if ($c) { $kc = $c.Source } }
if (-not $kc) { $kc = Join-Path $env:LOCALAPPDATA "Programs\kubeconform\kubeconform.exe" }

$out = Join-Path ([System.IO.Path]::GetTempPath()) "agrigreen-k8s-check"
New-Item -ItemType Directory -Force $out | Out-Null
$rendered = @()
foreach ($e in "dev", "staging", "prod") {
  $vf = Join-Path $chart "values-$e.yaml"
  & $helm lint $chart -f $vf --strict
  if ($LASTEXITCODE -ne 0) { throw "helm lint hỏng ($e)" }
  $f = Join-Path $out "$e.yaml"
  & $helm template agrigreen $chart -f $vf | Out-File -Encoding utf8 $f
  if ($LASTEXITCODE -ne 0) { throw "helm template hỏng ($e)" }
  $rendered += "$e=$f"
}

if (Test-Path $kc) {
  $schemas = Join-Path $root "deploy\k8s\tests\schemas"
  foreach ($r in $rendered) {
    $f = $r.Split("=", 2)[1]
    $kargs = @("-strict", "-summary", "-kubernetes-version", "1.31.0", "-schema-location", "default", "-schema-location", "$schemas/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json")
    if ($Offline) { $kargs += "-ignore-missing-schemas" }
    else { $kargs += @("-schema-location", "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json") }
    & $kc @kargs $f
    if ($LASTEXITCODE -ne 0) { throw "kubeconform hỏng ($r)" }
  }
} else {
  Write-Warning "không có kubeconform — bỏ qua kiểm schema (cài theo k8s-deploy.md mục 1)"
}

$pyargs = @()
foreach ($r in $rendered) { $pyargs += @("--rendered", $r) }
python (Join-Path $root "deploy\k8s\tests\check_flows.py") @pyargs
if ($LASTEXITCODE -ne 0) { throw "check_flows hỏng" }
Write-Host "k8s-check: sạch"
