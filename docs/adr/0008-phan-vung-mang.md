# ADR-0008: Phân vùng mạng và bảng luồng

**Trạng thái:** Chấp nhận — hiện thực từ G4 · **Ngày:** 27/09/2026 · **Hiện thực K8s:** 30/09/2026 ([ADR-0021](0021-ha-tang-kubernetes-g4.md)) · **Public API (G5):** 01/10/2026 ([ADR-0025](0025-ho-so-cap-phat-phase-k.md))

## Quyết định
Theo bảng ở mục 17: Biên → DMZ → Ứng dụng → Dữ liệu / Định danh; Quản trị qua VPN + MFA; Biên OT chỉ tới `ingest`.
- Kubernetes: mỗi vùng một namespace, NetworkPolicy mặc định từ chối (R-81).
- Compose ở ĐTK 1 **chỉ cho Local/Dev**. Mọi cổng chỉ bind `127.0.0.1`.
- Mức DB đã cưỡng chế từ G0: mỗi database chỉ cấp CONNECT cho đúng các vai của nó. `party_app` không vào được `ag_vault` (R-04).

## Hệ quả / hiện thực (G4)

Chart `deploy/k8s/charts/agrigreen` (vận hành: [k8s-deploy.md](../runbooks/k8s-deploy.md)).

**Vùng → namespace:**

| Vùng mục 17 | Namespace | Chứa |
|---|---|---|
| Biên + DMZ | `agrigreen-edge-ingress` | bộ điều khiển Ingress (Traefik); WAF/chống DDoS của nhà cung cấp đứng trước LB |
| Ứng dụng — nền tảng | `agrigreen-pf` | `api-platform`, `worker`, `outbox-publisher-pf`, `optimizer`, `web-ops`, `api-public` (G5, 0 bản sao trừ Phase K) |
| Ứng dụng — nội bộ MKG | `agrigreen-mkg` | `api-internal`, `worker-internal`, `outbox-publisher-mkg`, `ingest`, `web-internal`, CronJob neo/dọn cụm mkg + ts |
| Dữ liệu | `agrigreen-data` | PostgreSQL `pg-pf`, `pg-mkg`, `pg-ts`, `pg-iam` + PgBouncer (CNPG Pooler), RabbitMQ, Job migration |
| Định danh | `agrigreen-vault` | `identity-vault`, cụm PostgreSQL **riêng** `pg-vault` + PgBouncer, Job migration/neo |
| Định danh người dùng | `agrigreen-iam` | Keycloak (chart riêng; chart này chỉ đặt NetworkPolicy) |
| Quản trị | `agrigreen-ops` | Prometheus, Alertmanager, Grafana, Argo CD, OTel Collector, sql_exporter |
| Quản trị (PKI) | `agrigreen-pki` | CA thiết bị step-ca + HSM, chỉ qua VPN quản trị |

**Cơ chế:**
- Mỗi namespace có `default-deny-all` (Ingress + Egress, `podSelector: {}`) và `allow-dns-egress`.
- Mọi luồng khác sinh từ **một** bảng `networkPolicy.flows` trong `values.yaml`, mã `F01`…`F99`, mỗi luồng ghi hàng mục 17 mà nó hiện thực. Một luồng gồm hai nửa: egress ở namespace nguồn và ingress ở namespace đích.
- Dải ngoài cụm (kho S3, KMS, SMTP, VPN OT, VPN quản trị, API server, HSM) lấy từ `external.*`. Danh sách rỗng thì luồng không mở.

| Hàng mục 17 | Luồng (mã) |
|---|---|
| Biên → DMZ | Internet → Ingress (F01) |
| DMZ → Ứng dụng, chỉ cổng API | Ingress → `api-platform` 8080/8082, `web-ops`, `api-internal` 8081/8083, `web-internal`, Keycloak (F02–F06); Ingress → `api-public` 8085 qua host `partner.<domain>` (F08, G5). **Không** tới 8084 (Internal), `identity-vault`, `ingest` |
| Ứng dụng → Dữ liệu, chỉ cổng DB/MQ | pf → PgBouncer/PostgreSQL **cụm pf** + RabbitMQ (F10–F13); `api-public` → **chỉ** PgBouncer cụm pf (F19, G5); mkg → PgBouncer/PostgreSQL **cụm mkg, ts** + RabbitMQ (F14–F18). Không có đường pf → DB mkg/ts hay mkg → DB pf |
| Ứng dụng → Định danh, chỉ API vault, mTLS | `api-platform` → `identity-vault` 8090 (F20). Chỉ `identity-vault` tới DB vault (F70) |
| Dữ liệu: không đi ra | Pod DB/MQ chỉ ra: sao chép trong **cùng** cụm, API server (operator), kho sao lưu trong lãnh thổ (F60–F69, F90–F92) |
| Định danh: không đi ra, trừ KMS | `identity-vault` → KMS (F33). Ngoại lệ ghi rõ: JWKS Keycloak (F24, R-54), OTLP (F52, không có dữ liệu cá nhân — R-91), sao lưu riêng của `pg-vault` (F75), manifest neo của cụm vault — chỉ băm (F77) |
| Quản trị → mọi vùng, qua VPN + MFA | VPN quản trị → `agrigreen-ops`, CA thiết bị (F58, F59). Prometheus → cổng số đo (F53–F56). sql_exporter → PgBouncer mkg/ts bằng vai giám sát (F57) |
| Biên OT → chỉ `ingest`, VPN + mTLS | dải VPN OT → `ingest` 8443 (F40), qua LoadBalancer nội bộ với `loadBalancerSourceRanges` |

- Cửa nền tảng → MKG (mục 5, ADR-0020): `api-internal` gọi Application API công khai **qua DMZ** (F25), không có luồng Pod-tới-Pod mkg → pf.
- Mức DB giữ nguyên, nay tách thêm về vật lý. Mỗi cụm là một cụm PostgreSQL riêng. Pod của một vùng chỉ nhận Secret trong namespace của nó, nên Pod mkg không có mật khẩu vai pf và ngược lại.
- RabbitMQ: tài khoản theo dịch vụ. Chỉ tài khoản vùng mkg có quyền trên `mkg.internal` (ADR-0018).
- **Kiểm thử:** `deploy/k8s/tests/check_flows.py` (chạy qua `deploy/k8s/check.ps1` hoặc `make k8s-check`) render dev/staging/prod rồi mô phỏng ngữ nghĩa NetworkPolicy. Nó kiểm:
  - mọi luồng mục 17 được phép;
  - các luồng cấm bị chặn: mkg → DB pf, pf → DB mkg/ts, mọi thứ trừ vault → DB vault, Internet ra từ DB/vault/ứng dụng, Internet thẳng vào ứng dụng/ingest/DB/MQ, DB khác cụm;
  - mặc định từ chối ở mọi namespace;
  - Secret và URL DB đúng vùng;
  - quyền RabbitMQ đúng.
- **Public API cho bên thứ ba (G5, [ADR-0022](0022-public-api-ben-thu-ba.md), [ADR-0025](0025-ho-so-cap-phat-phase-k.md)).** `api-public` là đơn vị riêng ở `agrigreen-pf`, 0 bản sao ở hồ sơ gốc; Phase K bật bằng lớp `values-phasek.yaml` (chỉ cấu hình và số bản sao, R-01). Luồng:

  | Mã | Hàng mục 17 | Luồng | Khi nào |
  |---|---|---|---|
  | F08 | DMZ → Ứng dụng | Ingress → `api-public` 8085 (host `partner.<domain>`, TLS, giới hạn IP + thân 1 MiB ở gateway, R-55) | luôn có policy; có Pod khi Phase K |
  | F19 | Ứng dụng → Dữ liệu | `api-public` → PgBouncer cụm pf 5432 | như F08 |
  | F26 | Ứng dụng → dịch vụ trong lãnh thổ | `api-public` → Valkey 6379 (giới hạn tần suất dùng chung, không phải nguồn sự thật) | khi `external.valkey` có dải (Q5-10) |
  | F27 | Quản trị → Ứng dụng (mở rộng F53) | Prometheus → `api-public` 9464 | khi `apps.api-public.metrics.enabled` (mặc định số đo đi OTLP qua F50) |
  | F28 | Ứng dụng → Định danh người dùng (R-54) | `api-public` → Keycloak 8080 (JWKS, client credentials đơn vị thẩm định) | khi `apps.api-public.oidc.enabled` (Q5-7) |

  Luồng **cấm** (`check_flows.py`, `public_api_checks`, mọi hồ sơ kể cả `prod-phasek`): mọi nguồn khác Ingress → `api-public`; `api-public` → `identity-vault`, RabbitMQ (outbox ghi DB, `outbox-publisher-pf` phát), PostgreSQL pf trực tiếp, DB/PgBouncer mkg/ts/vault/iam, ứng dụng khác, Internet, dải ngoài cụm; host `partner.<domain>` → bất cứ gì ngoài `api-public:8085` (Internet không tới `api-platform` qua host này). Pepper khóa API chỉ có Secret ở `agrigreen-pf`.- **Còn phụ thuộc hạ tầng:**
  - CNI phải thực thi NetworkPolicy, kể cả sau DNAT cho luồng hairpin F25 (Calico/Cilium);
  - nhà cung cấp phải có bằng chứng tách logic theo R-82;
  - dải thật thay chỗ giữ RFC 5737 trong `values-prod.yaml` — chờ chọn nhà cung cấp.
