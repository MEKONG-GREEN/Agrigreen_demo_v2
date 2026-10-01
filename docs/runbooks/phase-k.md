# Bật Phase K (G5): Public API cho bên thứ ba

Phụ lục A, cổng G5: "Phase K; `pf/metering` và Public API bật với bên thứ ba đầu tiên". R-01: chuyển giai đoạn **chỉ là đổi cấu hình và số bản sao**, không sửa mã, không build lại image. Quyết định: [ADR-0022](../adr/0022-public-api-ben-thu-ba.md) (Public API), [ADR-0024](../adr/0024-diem-kiem-soat-xuat-du-lieu-dlp.md) (điểm kiểm soát xuất), [ADR-0025](../adr/0025-ho-so-cap-phat-phase-k.md) (hồ sơ Phase K). Triển khai chung: [k8s-deploy.md](k8s-deploy.md).

| Thành phần | Đường dẫn |
|---|---|
| Lớp giá trị Phase K | `deploy/k8s/charts/agrigreen/values-phasek.yaml` (chồng lên `values-prod.yaml` / `values-staging.yaml`) |
| Kiểm hồ sơ | `deploy/k8s/check.ps1` (hồ sơ `prod-phasek`), `deploy/k8s/tests/check_flows.py` (`public_api_checks`, `phasek_overlay_checks`) |
| DNS + chứng thư `partner.<domain>` | `deploy/iac/opentofu`, biến `phase_k`, module `certificate` |
| Compose (Local/Dev) | `docker compose --profile phasek` (mục 6) |
| Luật cảnh báo hạ tầng | `deploy/observability/prometheus/agrigreen-public-api.rules.yml` |

## 1. Hồ sơ gốc (ĐTK 1 / G4) và hồ sơ Phase K

| Hạng mục | Hồ sơ gốc | Phase K (`values-phasek.yaml`) |
|---|---|---|
| Deployment `api-public` (`agrigreen-pf`) | có, **0 bản sao**, image vẫn build và ký | 2 bản sao, PDB `minAvailable: 1` (có sẵn ở `values.yaml`) |
| `AG_PUBLIC_API_ENABLED` | không đặt → tiến trình thoát lỗi nếu lỡ chạy | `1` |
| `AG_PUBLIC_OPERATOR_TENANT` | rỗng | tenant đơn vị vận hành — đặt bằng `--set` (chờ khách Q5-2). Rỗng → `api-public` không khởi động |
| Host `partner.<domain>` | không có (`ingress.optionalHosts.partner.enabled: false`) | có: TLS `agrigreen-partner-tls`, chỉ `websecure`, Middleware `public-api-ratelimit` (20 rps, burst 40 theo IP) + `public-api-body-limit` (1 MiB) |
| Bí mật | `pf-public-db-roles` (vai của `api-public`) | thêm `pf-apikey-pepper` (pepper khóa API) và `agrigreen-partner-tls` |
| `AG_EGRESS_MODE` (`api-platform`, `worker`, `api-public`) | `observe` | `enforce` |
| NetworkPolicy | F08 (Ingress → `api-public:8085`), F19 (`api-public` → PgBouncer pf) **có sẵn** — không có Pod nào khi 0 bản sao | không đổi. F26 (Valkey) mở khi `external.valkey` có dải |
| Valkey | — | tùy chọn (Q5-10): `external.valkey` + `AG_VALKEY_URL` |

`check_flows.py` kiểm cả hai hồ sơ:
- hồ sơ gốc: `api-public` 0 bản sao, không HPA, không host `partner`, không Middleware, không pepper;
- Phase K: 2 bản sao + PDB, `AG_PUBLIC_API_ENABLED=1`, host `partner` duy nhất chỉ tới `api-public:8085`, TLS + hai Middleware, pepper chỉ ở vùng pf, `enforce`;
- mọi hồ sơ: `api-public` chỉ nhận từ Ingress; chỉ ra PgBouncer pf + DNS + OTLP; **không** tới `identity-vault`, RabbitMQ, PostgreSQL pf trực tiếp, DB mkg/ts/vault/iam, ứng dụng khác, Internet, dải ngoài cụm;
- `values-phasek.yaml` chỉ chứa đường dẫn trong danh sách `PHASEK_ALLOWED` (số bản sao, HPA/PDB, `env`, `enabled`, `external.valkey`) — R-01.

## 2. Điều kiện trước (ngoài hệ thống, chờ khách)

1. Q5-1: bên thứ ba đầu tiên, sản phẩm, độ chi tiết. Hợp đồng dữ liệu `contracts/dataproduct/<sản phẩm>.json` chuyển `approved` (production chỉ phục vụ `approved`, ADR-0022 mục 9) — đó là một bản phát hành mã bình thường, **trước** ngày bật.
2. Q5-2: pháp nhân đơn vị vận hành → tenant UUIDv7 của đơn vị vận hành (`AG_PUBLIC_OPERATOR_TENANT`).
3. Q5-8: hợp đồng dữ liệu / DPA đã ký; kênh giao bí mật khóa đã chốt.
4. Q5-5: gói hạn mức và ngưỡng xuất theo kênh đã duyệt (tham số có phiên bản, người đặt ≠ người duyệt). Có thể bật với giá trị tạm của g5-conventions §1.2.
5. Image `api-public` của đúng `global.imageTag` đang chạy đã có trong kho image trong lãnh thổ và đã ký (CI `images`).

## 3. Bật Phase K (chỉ cấu hình)

Làm ở **staging trước** (diễn tập K-04), rồi production. Không build lại image: dùng nguyên `global.imageTag` đang chạy.

1. **Bí mật ở kho trong lãnh thổ** (`<remotePrefix>` = `agrigreen/staging` hoặc `agrigreen/prod`):
   - `<remotePrefix>/apikey/pf`: `AG_APIKEY_PEPPER_V1` = 32 byte ngẫu nhiên trở lên (vd `openssl rand -hex 32`, sinh **trong** kho/KMS, không qua máy cá nhân), `AG_APIKEY_PEPPER_CURRENT` = `1`;
   - `<remotePrefix>/db/pf`: có `AG_DB_PASSWORD_PUBLIC_API`, `AG_DB_PASSWORD_APICLIENT_APP`, `AG_DB_PASSWORD_METERING_APP` (vai đã có từ G5-0; Job bootstrap đặt mật khẩu ở `helm upgrade` release `data`);
   - `<remotePrefix>/tls/partner`: `tls.crt`, `tls.key` cho `partner.<domain>` (bước 2).
2. **DNS + chứng thư** (`deploy/iac/opentofu`): đặt `phase_k = true` trong `envs/<env>.tfvars`, rồi
   ```powershell
   tofu plan -var-file=envs/<env>.tfvars    # chỉ thêm bản ghi partner + module.partner_certificate[0]
   tofu apply -var-file=envs/<env>.tfvars
   ```
   Đầu ra `partner_fqdn` phải bằng `partner.<global.domain>` của values Helm.
3. **Kiểm chart** trên nhánh phát hành: `powershell -File deploy/k8s/check.ps1` — hồ sơ `prod-phasek` sạch.
4. **Release `data`** (vai DB và mật khẩu): `helm upgrade agrigreen-data … -f values-<env>.yaml` như [k8s-deploy.md](k8s-deploy.md) mục 7. Không cần `values-phasek.yaml` ở đây.
5. **Release `pf`** với lớp Phase K:
   ```powershell
   $c = "deploy/k8s/charts/agrigreen"
   helm upgrade agrigreen-pf $c -f "$c/values-<env>.yaml" -f "$c/values-phasek.yaml" --namespace kube-system `
     --set global.imageTag=<tag đang chạy> --set-json 'global.installZones=["pf"]' `
     --set apps.api-public.env.AG_PUBLIC_OPERATOR_TENANT=<uuid đơn vị vận hành> --wait
   ```
   Kết quả: `ExternalSecret` `pf-apikey-pepper`, `agrigreen-partner-tls` đồng bộ; `api-public` 2 bản sao; Ingress `api-public-8085` + Middleware; `api-platform` và `worker` khởi động lại cuốn chiếu với `AG_EGRESS_MODE=enforce` và pepper (Admin API cấp khóa).
6. **Release `edge`, `ops`**: không cần cài lại. Nửa egress của F08 ở vùng edge và luật `agrigreen-public-api` đã có từ hồ sơ gốc.
7. **Kiểm sau khi bật:**
   ```powershell
   kubectl -n agrigreen-pf get externalsecret pf-apikey-pepper agrigreen-partner-tls   # SecretSynced
   kubectl -n agrigreen-pf rollout status deploy/api-public
   kubectl -n agrigreen-pf get pdb api-public                                          # ALLOWED DISRUPTIONS 1
   curl -fsS https://partner.<domain>/healthz                                          # 200
   curl -s -o /dev/null -w '%{http_code}' https://partner.<domain>/v1/data-products    # 401 (chưa có khóa)
   ```
   Thêm: `kubectl get netpol -A -l agrigreen.vn/flow=F08` có hai nửa (`-in` ở `agrigreen-pf`, `-out` ở `agrigreen-edge-ingress`); cảnh báo `AgPublicAPIDown` không kêu.

Ghi vào biên bản diễn tập: revision Helm trước/sau, `global.imageTag` (không đổi), thời điểm, người làm, kết quả mục 7.

## 4. Cấp bên thứ ba (thủ tục, không sửa mã — ADR-0025 mục 2)

Runbook chi tiết của agent vận hành: `docs/runbooks/public-api-onboarding.md`. Tóm tắt thứ tự:

0. **Pháp lý:** hợp đồng dữ liệu / DPA (mục đích, sản phẩm, thời hạn, tái chia sẻ). Ghi số hợp đồng vào mục đích của cấp quyền.
1. **Tenant bên thứ ba:** tenant UUIDv7 mới, tổ chức gốc ở `pf/party` (như cấp tenant MKG, ADR-0020). Chỉ gọi API thì không cần tài khoản Keycloak.
2. **Cấp quyền chia sẻ của chủ dữ liệu:** `platform_admin` bên cấp `POST /v1/share-grants` (`grantee` = tenant bên thứ ba, `kinds` = `dp_<sản phẩm>`, mục đích, hạn); một `platform_admin` **khác** duyệt (R-56).
3. **Khách hàng API** (Admin API, MFA, tenant đơn vị vận hành): `api_client_admin` đăng ký khách hàng, đăng ký sản phẩm + scope, gói hạn mức; `api_client_approver` khác người duyệt.
4. **Khóa:** `api_client_admin` phát hành; bí mật hiện **một lần**; giao qua kênh riêng đã chốt (không email thường), có biên bản. Xoay ≤ 365 ngày và ngay khi nghi lộ.
5. **Thử chấp nhận:** `GET /v1/data-products` thấy đúng sản phẩm; một lời gọi mỗi sản phẩm → một `usage_event` + một `egress_event` ở tenant bên cấp; thu hồi cấp quyền → 403 ngay ở lời gọi kế tiếp.
6. **Hằng tháng:** `billing_officer` lập bảng kê, `billing_approver` duyệt, tải CSV lập hóa đơn ngoài hệ thống (ADR-0023).

Ngừng hợp tác: thu hồi khóa (lý do bắt buộc) → thu hồi cấp quyền ở bên cấp → đăng ký sản phẩm `revoked`. Sổ giữ nguyên (RB-1). Không cần đổi cấu hình hạ tầng.

Đơn vị thẩm định (VVB) chọn OAuth client credentials (Q5-7, ADR-0022 mục 5): thêm vào `values-phasek.yaml` `apps.api-public.oidc.enabled: true` (mở F28 tới Keycloak) và `apps.api-public.env.AG_PUBLIC_OIDC_ISSUER` / `AG_PUBLIC_OIDC_AUDIENCE`; client Keycloak tạo ở realm (K-06). Vẫn là cấu hình.

## 5. Vận hành

- **Xoay pepper** (ADR-0022 mục 4): thêm `AG_APIKEY_PEPPER_V2` vào `<remotePrefix>/apikey/pf`, đổi `AG_APIKEY_PEPPER_CURRENT=2`. `ExternalSecret` dùng `dataFrom.extract` nên **không sửa chart**; Reloader khởi động lại `api-public` và `api-platform`. Khóa cũ vẫn kiểm được theo `pepper_version` của nó. **Không xóa** `V1` khi còn khóa hiệu lực dùng `V1`.
- **Tăng/giảm bản sao:** `apps.api-public.replicas` hoặc bật HPA (`apps.api-public.hpa.enabled: true`, `minReplicas ≥ 2`). Giới hạn tần suất trong tiến trình chia hạn mức cho số bản sao — khi có từ 3 bản sao trở lên nên dùng Valkey (F26).
- **Giới hạn ở gateway:** sửa `ingress.middlewares.public-api-ratelimit` ở `values-<env>.yaml` nếu cần (cấu hình, không thẩm quyền — R-55). Hạn mức thẩm quyền là gói hạn mức ở `pf_apiclient`, không phải gateway.
- **Cảnh báo:** `agrigreen-public-api.rules.yml` (sẵn sàng của `api-public`, 5xx/429 ở gateway). Bất thường xuất, checkpoint lỗi, metering lỗi: tệp luật của `pf/metering` (agent M), nạp tự động khi có ở `deploy/observability/prometheus/` **và** bản sao ở `deploy/k8s/charts/agrigreen/files/prometheus/` (chart đọc mọi `*.rules.yml`; `check_flows.py` hỏng nếu hai bản lệch).

## 6. Compose (Local/Dev)

```powershell
./scripts/gen-dev-secrets.ps1          # sinh thêm AG_APIKEY_PEPPER_V1 trong deploy/compose/.env
# .env: AG_PUBLIC_OPERATOR_TENANT=<uuid tenant vận hành dev>; tùy chọn AG_EGRESS_MODE=enforce
docker compose -f deploy/compose/docker-compose.yml --profile phasek up -d --build --wait
curl -fsS http://localhost:8085/healthz
```

Không có `--profile phasek` thì dịch vụ `api-public` không chạy (như ĐTK 1). `.env` cũ thiếu `AG_APIKEY_PEPPER_V1` → chạy lại `gen-dev-secrets` (hoặc thêm tay 64 ký tự hex). Keycloak realm không đổi.

## 7. Tắt lại (rollback)

Rollback = **bỏ lớp Phase K** (về 0 bản sao), không phải rollback mã:

```powershell
helm upgrade agrigreen-pf $c -f "$c/values-<env>.yaml" --namespace kube-system `
  --set global.imageTag=<tag đang chạy> --set-json 'global.installZones=["pf"]' --wait
```

Kết quả: `api-public` 0 bản sao; Ingress `partner` và Middleware bị gỡ (bên thứ ba nhận lỗi ở gateway); `AG_EGRESS_MODE` về `observe`; `ExternalSecret` pepper/TLS bị gỡ (Secret do ESO sở hữu bị xóa theo; giá trị vẫn ở kho bí mật — **không** xóa ở kho, khóa đã phát hành cần pepper để bật lại). Hoặc nhanh hơn, chỉ ngắt lưu lượng: `helm rollback agrigreen-pf <revision trước khi bật>`.

- Sổ `pf_apiclient`, `pf_metering` giữ nguyên (RB-1). Bật lại = mục 3 bước 5; khóa cũ vẫn hợp lệ nếu chưa hết hạn/thu hồi.
- Cần ngắt **một** bên thứ ba thì không tắt Phase K: thu hồi khóa / cấp quyền (mục 4).
- DNS `partner` (OpenTofu `phase_k`) có thể giữ khi tắt tạm; tắt hẳn thì `phase_k = false` + `tofu apply`.

## 8. Sự cố thường gặp

| Triệu chứng | Kiểm |
|---|---|
| `api-public` `CrashLoopBackOff` ngay khi bật | log khởi động: `AG_PUBLIC_API_ENABLED` ≠ `1`, `AG_PUBLIC_OPERATOR_TENANT` rỗng, pepper thiếu hoặc < 32 byte (`pf-apikey-pepper` chưa `SecretSynced`) |
| `CreateContainerConfigError` | `pf-public-db-roles` / `pf-apikey-pepper` chưa đồng bộ: kho thiếu khóa `<remotePrefix>/db/pf` hoặc `apikey/pf` |
| `/readyz` 503 | PgBouncer pf (F19), mật khẩu `public_api`: chạy lại `helm upgrade` release `data` (Job bootstrap) |
| 404 ở `partner.<domain>` | `ingress.optionalHosts.partner.enabled`, Middleware `agrigreen-pf-public-api-*@kubernetescrd` tồn tại (Traefik cần provider `kubernetescrd`) |
| 429 nhiều ở gateway (`AgPublicAPIGatewayRateLimitedHigh`) | nhiều bên thứ ba sau cùng NAT, hoặc LB chèn IP: xem `sourceCriterion.ipStrategy.depth` |
| 503 `metering.unavailable` | sổ sử dụng không ghi được (đóng khi lỗi, ADR-0023) — DB pf, luật của `pf/metering` |
