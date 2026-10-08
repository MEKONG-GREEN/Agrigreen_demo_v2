# Triển khai Kubernetes trong lãnh thổ (G4)

Phụ lục A, cổng G4: Kubernetes được quản trị trong lãnh thổ, tách đơn vị triển khai bằng cấu hình (RB-5, R-01). Kiến trúc mạng: [ADR-0008](../adr/0008-phan-vung-mang.md). Lựa chọn operator, PKI, bí mật: [ADR-0021](../adr/0021-ha-tang-kubernetes-g4.md).

| Thành phần | Đường dẫn |
|---|---|
| Chart Helm | `deploy/k8s/charts/agrigreen` (`values.yaml` + `values-dev/-staging/-prod.yaml`; lớp Phase K `values-phasek.yaml` — [phase-k.md](phase-k.md)) |
| Kiểm chart + bảng luồng | `deploy/k8s/check.ps1`, `make k8s-check`, `deploy/k8s/tests/check_flows.py` |
| Ảnh PostgreSQL CNPG | `deploy/k8s/images/postgres-cnpg/Dockerfile` |
| Bí mật dev | `deploy/k8s/scripts/dev-secrets.ps1` |
| OpenTofu | `deploy/iac/opentofu` (chờ chọn nhà cung cấp) |
| PKI | [pki.md](pki.md) · diễn tập tải: [load-drill.md](load-drill.md) |

Compose (`deploy/compose`, `docker-compose.vps.yml`) vẫn là Local/Dev/demo. Chart này dùng cho dev dùng chung, staging và production (mục 21).

## 1. Công cụ (cài mức người dùng, không cần admin)

| Công cụ | Giấy phép | Cài trên Windows |
|---|---|---|
| Helm ≥ 3.18 | Apache-2.0 | giải nén `helm-v3.18.x-windows-amd64.zip` từ get.helm.sh vào `%LOCALAPPDATA%\Programs\helm` |
| kubeconform 0.7 | Apache-2.0 | `kubeconform-windows-amd64.zip` (GitHub yannh/kubeconform) vào `%LOCALAPPDATA%\Programs\kubeconform` |
| OpenTofu ≥ 1.8 | MPL-2.0 | `tofu_<ver>_windows_amd64.zip` (GitHub opentofu/opentofu) vào `%LOCALAPPDATA%\Programs\tofu` |
| kubectl | Apache-2.0 | theo nhà cung cấp cụm |
| Python 3.12 + PyYAML | PSF / MIT | đã có trong môi trường dev (`python -m pip install --user pyyaml`) |

`deploy/k8s/check.ps1` tự tìm công cụ ở các thư mục trên, hoặc theo biến `HELM`, `KUBECONFORM`.

## 2. Điều kiện trước (một lần mỗi cụm)

1. **Hạ tầng** (mục 9): mạng theo vùng, cụm được quản trị, nhóm node gắn nhãn `agrigreen.vn/pool`, kho S3, KMS, DNS. Cụm phải có:
   - CNI thực thi NetworkPolicy, kể cả sau DNAT (Calico hoặc Cilium);
   - API server riêng, chỉ mở cho VPN quản trị;
   - etcd mã hóa bằng KMS trong lãnh thổ;
   - Pod Security Admission;
   - admission từ chối image chưa ký (mục 20).
2. **Operator.** Mỗi operator cài trong namespace riêng bằng chart chính thức. Ghim phiên bản đã kiểm ở staging.

   | Operator | Namespace | Dùng cho |
   |---|---|---|
   | CloudNativePG + plugin `pgbackrest.dalibo.com` | `cnpg-system` | `Cluster`, `Pooler`, `ScheduledBackup`, `Stanza` |
   | RabbitMQ Cluster Operator + Messaging Topology Operator | `rabbitmq-system` | `RabbitmqCluster`, `Vhost`, `User`, `Permission` |
   | cert-manager + trust-manager | `cert-manager` | `ClusterIssuer`, `Certificate`, `Bundle` |
   | External Secrets Operator | `external-secrets` | `ExternalSecret`; tạo `ClusterSecretStore agrigreen-vn-secrets` trỏ tới kho bí mật **trong lãnh thổ** |
   | Stakater Reloader | `reloader` | khởi động lại Pod khi chứng thư/bí mật đổi |
   | kube-prometheus-stack (Prometheus Operator) | `agrigreen-ops` | `ServiceMonitor`, `PodMonitor`, `PrometheusRule`. Nhãn chọn `release: prometheus` (`observability.selectorLabels`) |
   | Traefik | `agrigreen-edge-ingress` | IngressClass `traefik`. Nhãn Pod `app.kubernetes.io/name=traefik`, cổng 8000/8443 |
   | Keycloak (chart riêng) | `agrigreen-iam` | Service `keycloak:8080`, Pod `app.kubernetes.io/name=keycloak`, DB `pg-iam` (`agrigreen-data`) |

   Nhãn Pod của các thành phần ngoài chart phải khớp `systemSelectors` trong `values.yaml`. Kiểm thử luồng dựa vào giả định này: đổi nhãn thì đổi cả hai nơi.
3. **Ảnh.** Build và ký mọi image vào kho image trong lãnh thổ (`global.imageRegistry`):
   - dịch vụ Go: `Dockerfile` gốc với `--build-arg CMD=<lệnh>` cho `api-platform`, `api-internal`, `api-public` (G5 — build và ký dù 0 bản sao), `worker`, `worker-internal`, `outbox-publisher`, `ingest`, `identity-vault`, `migrate`;
   - `optimizer`, `web-ops`, `web-internal`;
   - `cnpg-postgis-pgrouting:17-3.5` (`deploy/k8s/images/postgres-cnpg`).

   `NEXT_PUBLIC_*` của web nhúng lúc build, nên build riêng cho từng môi trường.

## 3. Cài đặt theo vùng (production: một release mỗi vùng)

Một release mỗi vùng. Vì vậy người vận hành, quyền Argo CD project và lịch thay đổi của từng vùng tách được. `global.installZones` quyết định release render những gì. NetworkPolicy của một luồng có hai nửa: egress ở vùng nguồn, ingress ở vùng đích. Luồng chỉ thông khi **cả hai** release đã cài.

```powershell
$c = "deploy/k8s/charts/agrigreen"
$v = "$c/values-prod.yaml"
$tag = "<git sha đã ký>"
foreach ($z in "pki","data","vault","iam","ops","pf","mkg","edge") {
  helm upgrade --install "agrigreen-$z" $c -f $v --namespace kube-system `
    --set global.imageTag=$tag --set-json "global.installZones=[\"$z\"]" --wait --timeout 20m
}
```

Thứ tự và lý do:
1. `pki`: `ClusterIssuer` và `Bundle` phải có trước `Certificate` của các vùng.
2. `data`, rồi `vault`: cụm PostgreSQL, RabbitMQ, Job `db-migrate-<cụm>`.
3. `iam`: NetworkPolicy cho Keycloak. Cài chart Keycloak sau bước này.
4. `ops`: Collector, sql_exporter, luật.
5. `pf`, `mkg`: ứng dụng.
6. `edge`: Ingress controller có đường vào **sau cùng**.

Release metadata nằm ở `kube-system`. Tài nguyên tự ghi namespace của vùng. Namespace có `helm.sh/resource-policy: keep`: gỡ release không xóa dữ liệu.

Argo CD: một `Application` mỗi vùng, cùng chart và values. Hook `post-install,pre-upgrade` của Job migration được Argo CD ánh xạ sang PostSync/PreSync.

**Lần cài đầu.** Cụm CNPG cần vài phút để sẵn sàng. Job `db-migrate-<cụm>` (hook post-install) chạy `bootstrap` và tự thử lại kết nối. Trong lúc đó ứng dụng có thể khởi động lại vài lần vì `/readyz` lỗi DB, rồi tự ổn định. Kiểm:

```powershell
kubectl -n agrigreen-data get cluster,pooler,job
kubectl -n agrigreen-data logs job/db-migrate-pf -c up      # "đã áp migration: ag_pf"
kubectl -n agrigreen-data logs job/db-migrate-pf            # "cổng catalog: sạch" (migrate verify)
```

## 4. Bí mật

- **Không có giá trị bí mật trong repo.** Mỗi Secret là một `ExternalSecret` (`externalSecrets.secrets`, `externalSecrets.amqp`). Nguồn là kho bí mật trong lãnh thổ, khóa `<remotePrefix>/<remote>`, thuộc tính = tên khóa.

  | Kho (`remote`) | Nội dung | Namespace nhận |
  |---|---|---|
  | `db/pf` | `AG_DB_PASSWORD_<VAI>` mọi vai cụm pf, `ag_migrator`, `ag_outbox_publisher`, `ag_ops_jobs` | `agrigreen-pf` (vai module; `pf-db-outbox` chỉ outbox-publisher, `pf-db-ops` worker), `agrigreen-data` (Job) |
  | `db/mkg` | vai cụm mkg, `ag_outbox_publisher` và `ag_ops_jobs` dùng chung cho **mkg và ts**, `mkg_monitor` | `agrigreen-mkg`, `agrigreen-data`, `agrigreen-ops` (sql_exporter) |
  | `db/ts` | `ingest_ts`, `ts_reader`, `ts_monitor`, `ag_migrator` | `agrigreen-mkg`, `agrigreen-data` |
  | `db/vault` | `vault_app`, `ag_migrator`, `ag_outbox_publisher`, `ag_ops_jobs` | chỉ `agrigreen-vault` |
  | `db/foreign-data`, `db/foreign-vault` | `AG_DB_APP_PASSWORD` ngẫu nhiên, không dùng ở đâu. Là mật khẩu cho vai của vùng khác mà `dbadmin.Bootstrap` vẫn tạo trên mỗi cụm | `agrigreen-data`, `agrigreen-vault` |
  | `rabbitmq/<user>` | `username`, `password` của tài khoản dịch vụ | `agrigreen-data` (`rmq-<user>`) + URL dựng sẵn ở vùng dùng (`amqp-*`) |
  | `app/pf`, `app/mkg`, `app/vault` | `AG_SYNC_CURSOR_KEY` (≥ 32 byte), `AG_KC_CLIENT_SECRET`; `AG_MKG_SA_CLIENT_SECRET`, `AG_FIELDOPS_CURSOR_KEY`; `AG_VAULT_KEK`, `AG_VAULT_BLIND_KEY` | vùng tương ứng |
  | `s3/<vùng>`, `anchor/<vùng>` | khóa S3 **không có quyền xóa**, khóa ký neo Ed25519 | vùng tương ứng |
  | `backup/data`, `backup/vault` | khóa ghi pgBackRest. Vault có bucket **và** khóa riêng (mục 22) | `agrigreen-data`, `agrigreen-vault` |
  | `pki/service-ca` | `tls.crt`/`tls.key` CA trung gian dịch vụ | `cert-manager` |
  | `pki/device-ca` | PIN HSM của step-ca | `agrigreen-pki` |
  | `tls/public` | chứng thư TLS công khai (CA trong lãnh thổ) | `agrigreen-pf`, `-mkg`, `-iam` |
  | `db/pf` → `pf-public-db-roles` (G5) | chỉ vai của `api-public`: `public_api`, `apiclient_app`, `metering_app`, `cropcycle_app`, `geo_app`, `mrv_app`, `masterdata_app`, `party_app` | `agrigreen-pf` |
  | `apikey/pf` (G5, Phase K) | `AG_APIKEY_PEPPER_V<n>` (≥ 32 byte), `AG_APIKEY_PEPPER_CURRENT`. `dataFrom.extract`: thêm phiên bản pepper chỉ ở kho | chỉ `agrigreen-pf` (`api-public`, `api-platform`) |
  | `tls/partner` (G5, Phase K) | chứng thư `partner.<domain>` (OpenTofu `module.partner_certificate`) | `agrigreen-pf` |

- **Tách vùng.** Secret có phạm vi namespace. Pod vùng mkg vì vậy không thể nhận mật khẩu vai pf và ngược lại. `check_flows.py` kiểm thêm không Pod nào tham chiếu Secret có tiền tố của vùng khác.
- **Đổi mật khẩu vai DB:**
  1. đổi giá trị ở kho;
  2. chờ External Secrets làm mới (`refreshInterval`, mặc định 1 giờ, hoặc `kubectl annotate externalsecret … force-sync=$(date +%s)`);
  3. `helm upgrade` release `data`/`vault`: Job bootstrap (pre-upgrade) chạy `ALTER ROLE … PASSWORD`;
  4. Reloader khởi động lại Pod ứng dụng khi Secret đổi.

  PgBouncer (auth_query) tự dùng mật khẩu mới.

## 5. PostgreSQL và RabbitMQ

- Mỗi cụm logic là một cụm CNPG riêng: `pg-pf`, `pg-mkg`, `pg-ts`, `pg-iam` ở `agrigreen-data`; `pg-vault` ở `agrigreen-vault`. Mỗi cụm 3 bản, chống chung node bắt buộc.
- Tham số: `max_locks_per_transaction=256`, `max_connections=300`, `wal_level=logical`, `pg_stat_statements`.
- Ứng dụng nối PgBouncer transaction mode (`pg-<cụm>-pooler:5432`, R-30). Các kết nối cần khóa mức phiên nối thẳng `pg-<cụm>-rw:5432`: leader lịch worker, outbox, `hub_capacity_daily`, Job migration. Kết nối replication của engine đồng bộ (mục 15) cũng nối thẳng primary.
- Job `db-migrate-<cụm>` chạy bốn bước:
  1. `migrate bootstrap` — vai, database, extension, bằng superuser `pg-<cụm>-superuser`;
  2. `pooler-auth` — hàm `public.user_search` + `GRANT CONNECT` cho `cnpg_pooler_pgbouncer` trong database ứng dụng;
  3. `migrate up` (`AG_CLUSTERS=<cụm>`);
  4. `migrate verify`.
- CronJob theo cụm:
  - `maintain-<cụm>`: phân vùng tháng, mùng 1 lúc 02:00;
  - `verify-chains-<cụm>`: Chủ nhật lúc 03:00.

  Múi giờ Asia/Ho_Chi_Minh.
- Neo chuỗi + dọn idempotency:
  - cụm pf: trong lịch `worker` (`AG_CLUSTERS=pf`);
  - cụm mkg, ts: CronJob `anchor-mkg`, `purge-idempotency-mkg` ở `agrigreen-mkg`;
  - cụm vault: `anchor-vault`, `purge-idempotency-vault` ở `agrigreen-vault`.

  Xem [anchor.md](anchor.md). Mỗi vùng một khóa ký riêng (`anchor/<vùng>`, key id `<env>-<vùng>-1`). Diễn tập phục hồi phải có khóa công khai của cả ba.
- **Sao lưu** (mục 22): pgBackRest qua plugin Dalibo, `Stanza` `pg-<cụm>`, full Chủ nhật + diff hằng ngày + WAL liên tục. Kho S3 trong lãnh thổ có khóa đối tượng. **Đối chiếu tên trường của `Stanza`/`ScheduledBackup` với phiên bản plugin đã ghim** (schema kiểm offline ở `deploy/k8s/tests/schemas/stanza_v1.json`). Phục hồi và diễn tập: [backup.md](backup.md), [restore-drill.md](restore-drill.md).
- **RabbitMQ:**
  - `RabbitmqCluster rabbitmq` 3 bản, quorum queue mặc định, vhost `mkg.internal` (CR `Vhost`);
  - tài khoản theo dịch vụ (`rabbitmq.users`); chỉ tài khoản vùng mkg có Permission trên `mkg.internal`; tài khoản `mkg-outbox` trên vhost `/` chỉ khai báo exchange và ghi `ag.events` (van);
  - sau khi tạo vhost, kiểm `rabbitmqctl list_permissions -p mkg.internal` chỉ có tài khoản mkg (ADR-0018). Tài khoản mặc định của operator thường có quyền trên vhost nó tạo, nên gỡ quyền đó.

## 6. Dev (cụm dùng chung, dữ liệu tổng hợp)

```powershell
helm upgrade --install agrigreen deploy/k8s/charts/agrigreen -f deploy/k8s/charts/agrigreen/values-dev.yaml --namespace kube-system
powershell -File deploy/k8s/scripts/dev-secrets.ps1         # Secret ngẫu nhiên, không commit
sh scripts/gen-dev-certs.sh                                 # CA dịch vụ + CA thiết bị dev
kubectl -n cert-manager create secret tls agrigreen-service-ca --cert deploy/compose/.certs/ca.crt --key deploy/compose/.certs/ca.key
kubectl -n cert-manager create configmap agrigreen-device-ca-source --from-file=ca.crt=deploy/compose/.certs/device-ca.crt
```

Dev: 1 bản mỗi thứ, không sao lưu, không HSM, `AG_ENV=dev`. NetworkPolicy và tách vùng **giữ nguyên** để lỗi luồng lộ sớm.

## 7. Nâng cấp

1. Chạy `deploy/k8s/check.ps1` trên nhánh phát hành (mục 8). Image đã ký, SBOM đã quét (mục 20).
2. Staging trước:
   ```powershell
   helm upgrade agrigreen-<vùng> deploy/k8s/charts/agrigreen -f values-staging.yaml --set global.imageTag=<sha> --set-json "global.installZones=[\"<vùng>\"]" --wait
   ```
3. Thứ tự vùng như mục 3. Release `data`/`vault` chạy migration (pre-upgrade) trước. Release ứng dụng cập nhật cuốn chiếu (`maxUnavailable: 0`, PDB).
4. Theo dõi:
   - `kubectl -n agrigreen-pf rollout status deploy/api-platform`;
   - cảnh báo `http.server.request.duration` p95 > 2 s, DLQ, `ag.outbox.backlog` (README mục "Chỉ số và cảnh báo").

## 8. Lùi lại (rollback)

- **Ứng dụng:** `helm rollback agrigreen-<vùng> <revision>`.
- **Migration chỉ tiến** (R-23, R-25): không bao giờ lùi schema. Chỉ lùi ứng dụng khi migration của bản mới **tương thích ngược** với mã cũ (thêm cột/bảng, không đổi nghĩa). Quy ước của repo vốn đòi điều này.
- Không tương thích thì **sửa tiến**: phát hành bản vá, không lùi DB.
- Hỏng dữ liệu hoặc mất cụm: phục hồi từ pgBackRest theo [restore-drill.md](restore-drill.md), rồi `migrate verify-chains`.
- `helm rollback` không chạy lại hook migration (`pre-upgrade` chỉ chạy khi upgrade).

## 9. Kiểm chart (bắt buộc trước mỗi thay đổi `deploy/k8s`)

```powershell
powershell -File deploy/k8s/check.ps1            # helm lint --strict, helm template, kubeconform -strict cho dev/staging/prod/prod-phasek; check_flows.py
powershell -File deploy/k8s/check.ps1 -Offline   # không tải schema CRD từ mạng
make k8s-check                                   # Linux/CI: helm lint + check_flows.py
```

`check_flows.py` hỏng khi:
- một luồng mục 17 bị chặn, hoặc một luồng cấm lại được phép;
- một namespace thiếu default-deny;
- Pod không cứng hóa: non-root, rootfs chỉ đọc, bỏ capability, seccomp, requests/limits;
- Pod thiếu probe (trừ `worker`, `outbox-publisher-*`, `optimizer`: chưa có endpoint);
- Pod dùng Secret hoặc URL DB của vùng khác;
- tài khoản nền tảng có quyền trên `mkg.internal`;
- `max_locks_per_transaction` ≠ 256;
- luật Prometheus trong chart lệch bản gốc `deploy/observability/prometheus`. Sửa luật ở bản gốc rồi chép vào `deploy/k8s/charts/agrigreen/files/`;
- Public API (G5, hồ sơ `prod-phasek` = `values-prod.yaml` + `values-phasek.yaml`): `api-public` nhận từ nơi khác ngoài Ingress, ra nơi khác ngoài PgBouncer pf/DNS/OTLP (vault, RabbitMQ, DB mkg/ts/vault, Internet…); hồ sơ gốc không ở trạng thái tắt (0 bản sao, không host `partner`, không pepper); Phase K thiếu 2 bản sao/PDB/TLS/Middleware/`enforce`; `values-phasek.yaml` chứa thứ khác ngoài cấu hình và số bản sao (R-01).

## 10. OpenTofu (chờ chọn nhà cung cấp)

`deploy/iac/opentofu`:
- module trung lập nhà cung cấp: `network`, `cluster`, `node-pool`, `kms`, `object-storage`, `dns`;
- `envs/<env>.tfvars`;
- `provider_country` chỉ nhận `VN` (RB-7);
- `phase_k` (G5, mặc định `false`): bản ghi DNS `partner.<domain>` + module `certificate` (chứng thư ghi vào `<remotePrefix>/tls/partner`) — [phase-k.md](phase-k.md) mục 3.

Các module hiện chỉ ghi ý định (`terraform_data`). Khi đã chọn nhà cung cấp (VNPT, Viettel, FPT Cloud hoặc khác đạt R-82):
1. thêm `required_providers`, `backend "s3"` trỏ kho S3 trong lãnh thổ;
2. thay khối chú thích "chờ chọn nhà cung cấp" trong từng module bằng tài nguyên thật, giữ nguyên biến và đầu ra;
3. đưa đầu ra vào values Helm: `api_server_cidrs` → `external.kubeAPI`, `object_storage_*` → `common.s3.endpoint`/`external.objectStorage`, `kms_cidrs` → `external.kms`.

```powershell
cd deploy/iac/opentofu
tofu init -backend=false; tofu validate
tofu plan -var-file=envs/staging.tfvars
tofu plan -var-file=envs/staging.tfvars -var phase_k=true   # Phase K: thêm partner + chứng thư
```

## 11. Tách đơn vị triển khai bằng cấu hình (RB-5) — tóm tắt

| Đơn vị | Namespace | Cấu hình chọn vùng |
|---|---|---|
| `api-platform`, `worker` | `agrigreen-pf` | `AG_PF_DB_URL`/`AG_DB_URL` → `pg-pf-pooler`; worker `AG_CLUSTERS=pf`, `AG_DB_DIRECT_URL` → `pg-pf-rw` |
| `api-public` (G5) | `agrigreen-pf` | `AG_PF_DB_URL` → `pg-pf-pooler`; 0 bản sao ở hồ sơ gốc, 2 ở Phase K (`values-phasek.yaml`, [phase-k.md](phase-k.md)) |
| `outbox-publisher-pf` | `agrigreen-pf` | `AG_CLUSTERS=pf`, `AG_DB_URL` → `pg-pf-rw`, vhost `/` |
| `outbox-publisher-mkg` | `agrigreen-mkg` | `AG_CLUSTERS=mkg,ts`, `AG_MKG_DB_URL`/`AG_TS_DB_URL` → `pg-mkg-rw`/`pg-ts-rw`, van → `/`, nội bộ → `mkg.internal` |
| `api-internal`, `worker-internal`, `ingest` | `agrigreen-mkg` | `AG_MKG_DB_URL` → `pg-mkg-pooler`, ingest `AG_TS_DB_URL` → `pg-ts-pooler` |
| `identity-vault` | `agrigreen-vault` | `AG_VAULT_DB_URL` → `pg-vault-pooler` (namespace vault) |
| Job/CronJob DB | namespace của cụm | `AG_CLUSTERS=<cụm>`, `AG_<CỤM>_DB_URL` |

Biến mới:
- `AG_CLUSTERS` và `AG_<CỤM>_DB_URL` (`cmd/internal/clusterenv`): rỗng thì giữ hành vi Compose (mọi cụm, một URL);
- lệnh `migrate purge-idempotency`.

Tên biến `_URL` nên guard RB-7 kiểm đích ([residency.md](residency.md)). `global.residencyAllowlist` phải gồm `svc.cluster.local` và tên miền kho S3.

## 12. Sự cố thường gặp

| Triệu chứng | Kiểm |
|---|---|
| Pod `CreateContainerConfigError` | Secret chưa có: `kubectl get externalsecret -n <ns>` (trạng thái `SecretSynced`), ClusterSecretStore |
| `/readyz` 503 kéo dài sau cài đầu | Job `db-migrate-<cụm>` chưa xong hoặc lỗi. PgBouncer `auth_query` lỗi thì chạy lại Job (bước `pooler-auth`) |
| Kết nối bị treo giữa hai vùng | thiếu một nửa luồng: cài đủ release hai vùng; `kubectl get netpol -A -l agrigreen.vn/flow=<Fxx>` |
| `RB-7: đích ngoài danh mục` lúc khởi động | thêm hậu tố/dải vào `global.residencyAllowlist` **chỉ khi** đích ở trong lãnh thổ |
| ingest từ chối khởi động (CA) | `agrigreen-device-ca` phải chỉ chứa CA thiết bị ([pki.md](pki.md)) |
| probe của kubelet thất bại ngay sau khi bật NetworkPolicy | CNI phải cho phép lưu lượng từ node tới Pod trên cùng node (mặc định của Calico/Cilium); không mở bằng NetworkPolicy |
| `api-internal` không gọi được `api.<miền>` | luồng F25 qua DMZ; CNI phải thực thi policy sau DNAT, DNS của cụm phân giải được tên công khai |
| `api-public` thoát lỗi khi khởi động | `AG_PUBLIC_API_ENABLED`, `AG_PUBLIC_OPERATOR_TENANT`, pepper — [phase-k.md](phase-k.md) mục 8 |
