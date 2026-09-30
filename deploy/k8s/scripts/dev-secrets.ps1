# Sinh Secret NGẪU NHIÊN cho cụm Dev (values-dev.yaml tắt External Secrets). Không ghi gì vào repo.
# Chỉ cho Dev/dữ liệu tổng hợp (mục 21) — staging/production dùng External Secrets từ kho trong lãnh thổ.
#   powershell -File deploy/k8s/scripts/dev-secrets.ps1 [-Context <kubectl context>]
# Đọc danh mục secret từ values.yaml của chart (externalSecrets.secrets / .amqp / rabbitmq.users) nên
# luôn khớp chart. Mật khẩu cùng "remote" dùng chung giá trị (vd db/pf ở agrigreen-pf và agrigreen-data).
# Secret đã có thì giữ nguyên (chạy lại an toàn). Cần: kubectl, python + PyYAML.
param([string]$Context = "")
$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$py = @'
import json, secrets, string, sys, yaml
v = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
ns = v["namespaces"]; es = v["externalSecrets"]; mq = v["rabbitmq"]
alpha = string.ascii_letters + string.digits
pool = {}
def val(remote, key):
    k = (remote, key)
    if k not in pool:
        if key in ("AG_ANCHOR_SIGNING_KEY", "AG_VAULT_KEK", "AG_VAULT_BLIND_KEY"):
            import base64, os
            pool[k] = base64.b64encode(os.urandom(32)).decode()
        else:
            pool[k] = "".join(secrets.choice(alpha) for _ in range(40))
    return pool[k]
out = []
for zone, items in es["secrets"].items():
    for name, s in items.items():
        if s.get("type") == "kubernetes.io/tls" or name in ("pg-backup-s3", "step-ca-hsm-pin"):
            continue  # TLS công khai, sao lưu, HSM: không dùng ở dev
        data = {k: val((s.get("keyRemotes") or {}).get(k, s["remote"]), k) for k in s["keys"]}
        out.append({"ns": ns[zone], "name": name, "data": data})
host = "%s.%s.svc.%s" % (mq["name"], ns["data"], v["global"]["clusterDomain"])
for u in mq["users"]:
    out.append({"ns": ns["data"], "name": "rmq-" + u, "data": {"username": u, "password": val(("rabbitmq", u), "password")}})
for name, a in es["amqp"].items():
    pw = val(("rabbitmq", a["user"]), "password"); u = a["user"]
    data = {}
    for key, kind in a["urls"].items():
        data[key] = {"platform": f"amqp://{u}:{pw}@{host}:5672/", "internal": f"amqp://{u}:{pw}@{host}:5672/mkg.internal",
                     "mgmt": f"http://{u}:{pw}@{host}:15672"}[kind]
    out.append({"ns": ns[a["zone"]], "name": name, "data": data})
dsn = lambda role, c: "postgres://%s:%s@pg-%s-pooler.%s.svc.%s:5432/ag_%s?sslmode=require" % (role, val("db/" + c, "AG_DB_PASSWORD_" + role.upper()), c, ns["data"], v["global"]["clusterDomain"], c)
cfg = ("global: { scrape_timeout: 10s, min_interval: 60s }\ncollector_files: [\"/etc/sql_exporter/collectors/*.collector.yml\"]\njobs:\n"
       "  - { job_name: agrigreen-mkg, collectors: [agrigreen_mkg_measurement], static_configs: [{ targets: { mkg: \"%s\" } }] }\n"
       "  - { job_name: agrigreen-ts, collectors: [agrigreen_ts_weigh], static_configs: [{ targets: { ts: \"%s\" } }] }\n") % (dsn("mkg_monitor", "mkg"), dsn("ts_monitor", "ts"))
out.append({"ns": ns["ops"], "name": "sql-exporter-config", "data": {"sql_exporter.yml": cfg}})
print(json.dumps(out))
'@
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "agrigreen-dev-secrets.py"
Set-Content -Path $tmp -Value $py -Encoding utf8
$json = python $tmp (Join-Path $root "deploy\k8s\charts\agrigreen\values.yaml")
Remove-Item $tmp
if ($LASTEXITCODE -ne 0) { throw "không dựng được danh mục secret" }
$kargs = @(); if ($Context) { $kargs = @("--context", $Context) }
foreach ($s in ($json | ConvertFrom-Json)) {
  & kubectl @kargs get secret $s.name -n $s.ns 2>$null | Out-Null
  if ($LASTEXITCODE -eq 0) { Write-Host "giữ nguyên $($s.ns)/$($s.name)"; continue }
  $lits = @()
  foreach ($p in $s.data.PSObject.Properties) { $lits += "--from-literal=$($p.Name)=$($p.Value)" }
  & kubectl @kargs create secret generic $s.name -n $s.ns @lits | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "tạo secret $($s.ns)/$($s.name) lỗi" }
  Write-Host "tạo $($s.ns)/$($s.name)"
}
Write-Host "Xong. Còn lại (tay): Secret agrigreen-service-ca ở cert-manager và ConfigMap agrigreen-device-ca-source — k8s-deploy.md mục 6."
