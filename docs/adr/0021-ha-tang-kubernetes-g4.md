# ADR-0021: Hạ tầng Kubernetes G4 — operator, PKI, bí mật và tách vùng bằng cấu hình

**Trạng thái:** Đề xuất · **Ngày:** 30/09/2026 · **Thành phần:** `deploy/k8s/**`, `deploy/iac/opentofu/**`, `cmd/internal/clusterenv`, `cmd/migrate`, `cmd/outbox-publisher`, `cmd/worker`, `tools/loadgen`

## Bối cảnh

- Phụ lục A, G4 (Q2/2028): Kubernetes được quản trị trong lãnh thổ; diễn tập tải ĐTK 2; tách đơn vị triển khai bằng cấu hình (RB-5, R-01); dựng lại bảng chiếu; phục hồi.
- Mục 4 chốt OpenTofu, Helm, Argo CD; mục 22 chốt pgBackRest; mục 17/R-81 chốt mỗi vùng một namespace, NetworkPolicy mặc định từ chối (ADR-0008).
- ADR-0018 và ADR-0019 để lại cho G4: vhost và tài khoản RabbitMQ do IaC tạo; CA nội bộ production trong lãnh thổ.
- RB-4: thành phần mới phải thuộc `deploy/policies/allowed-licenses.txt` (MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0, PostgreSQL). RB-7: mọi thứ lưu dữ liệu ở trong lãnh thổ.
- Mã G0–G3 giả định **một** URL gốc PostgreSQL cho mọi cụm ở ba lệnh vận hành: `migrate`, `outbox-publisher`, lịch của `worker`. Với cụm vật lý tách theo vùng, các lệnh này không chạy được theo vùng nếu không có cấu hình chọn cụm.

## Quyết định

1. **PostgreSQL: CloudNativePG** (Apache-2.0), mỗi `migrations.Cluster` một cụm vật lý: `pg-pf`, `pg-mkg`, `pg-ts` ở `agrigreen-data`; `pg-vault` ở `agrigreen-vault` (vùng Định danh, "cụm riêng" mục 5). Thêm `pg-iam` cho Keycloak. PgBouncer transaction mode là `Pooler` của CNPG (auth_query; Job `db-migrate-<cụm>` tạo hàm `user_search`). `max_locks_per_transaction = 256`.
   - Sao lưu: **pgBackRest** (MIT) qua plugin CNPG-I của Dalibo `cnpg-plugin-pgbackrest` (Apache-2.0). Không dùng barman-cloud mặc định của CNPG vì Barman là GPL-3.0, ngoài danh sách RB-4, và mục 22 đã chốt pgBackRest. Vault có bucket và khóa sao lưu riêng.
   - Loại bỏ: StatefulSet tự viết (không có chuyển đổi dự phòng tự động cho diễn tập mục 20); Crunchy PGO (Apache-2.0 nhưng image hỗ trợ theo điều khoản riêng của nhà cung cấp).
2. **RabbitMQ: Cluster Operator + Messaging Topology Operator** (MPL-2.0). Vhost `mkg.internal`, User/Permission theo dịch vụ là CR do Helm quản lý. Không tài khoản nền tảng nào có Permission trên `mkg.internal`. Tài khoản van của vùng mkg chỉ khai báo exchange và ghi `ag.events` trên vhost `/`.
3. **PKI:**
   - **CA dịch vụ:** cert-manager (Apache-2.0), `ClusterIssuer` kiểu CA. CA trung gian nằm trong kho bí mật trong lãnh thổ, CA gốc ngoại tuyến. Chứng thư lá 90 ngày, tự gia hạn. trust-manager (Apache-2.0) phát bó CA công khai. Stakater Reloader (Apache-2.0) khởi động lại Pod khi chứng thư đổi, vì dịch vụ Go chỉ đọc chứng thư lúc khởi động.
   - **CA thiết bị:** step-ca (Apache-2.0), khóa trong HSM qua PKCS#11. Namespace riêng `agrigreen-pki`, chỉ nhận kết nối từ VPN quản trị. Người vận hành ký CSR sau khi đối chiếu ngoài băng (device-pki.md). Không tự cấp qua API cho thiết bị. Tên, khóa và người vận hành đều khác CA dịch vụ.
   - Loại bỏ: HashiCorp Vault PKI (BUSL-1.1, ngoài RB-4). CA công cộng (RB-7, device-pki.md).
4. **Bí mật: External Secrets Operator** (Apache-2.0) đồng bộ từ kho bí mật trong lãnh thổ (OpenBao, MPL-2.0, tự vận hành; hoặc dịch vụ của nhà cung cấp trong nước — chờ chọn). Mỗi Secret chỉ tồn tại trong namespace của vùng dùng nó, nên Pod vùng mkg không thể gắn bí mật của vùng pf. Repo không có giá trị bí mật nào.
5. **Biên/DMZ: Traefik** (MIT) qua `Ingress` chuẩn. ingress-nginx đã lưu trữ (03/2026) nên không chọn. Đổi bộ điều khiển chỉ cần đổi `ingress.className` và `systemSelectors.ingressController`.
6. **Tách vùng bằng cấu hình (RB-5).** Thêm package `cmd/internal/clusterenv`, tương thích ngược: rỗng thì giữ hành vi Compose.
   - `AG_CLUSTERS`: chọn cụm cho `migrate` (up, verify, maintain, verify-chains, anchor, lệnh mới `purge-idempotency`), `outbox-publisher` và lịch neo/dọn idempotency của `worker`.
   - `AG_<CỤM>_DB_URL`: URL gốc riêng của từng cụm.
   - Bản `outbox-publisher-pf` chạy `AG_CLUSTERS=pf`. Bản `outbox-publisher-mkg` chạy `AG_CLUSTERS=mkg,ts`: van lên `/`, sự kiện nội bộ lên `mkg.internal`. Hàng đợi pf chỉ do bản có cụm pf khai báo.
   - `worker` chạy `AG_CLUSTERS=pf`. Neo và dọn idempotency của cụm mkg/ts/vault chạy bằng CronJob `migrate anchor|purge-idempotency` trong vùng đó.
7. **Diễn tập tải: `tools/loadgen` (Go).** k6 là AGPL-3.0, ngoài RB-4. Công cụ tự viết dùng lại hợp đồng dây `edgewire` nên lô tải ký giống hệt agent thật.
8. **IaC: OpenTofu** (MPL-2.0). Bộ khung trung lập nhà cung cấp: module `network`, `cluster`, `node-pool`, `kms`, `object-storage`, `dns` dùng `terraform_data` ghi ý định. Phần gọi API nhà cung cấp đánh dấu "chờ chọn nhà cung cấp". `provider_country` chỉ nhận `VN` (RB-7).

Giấy phép công cụ và thành phần mới kiểm ngày 30/09/2026 qua GitHub API (SPDX): cloudnative-pg Apache-2.0, dalibo/cnpg-plugin-pgbackrest Apache-2.0, pgbackrest MIT, cert-manager/trust-manager Apache-2.0, smallstep/certificates Apache-2.0, rabbitmq cluster-operator/messaging-topology-operator MPL-2.0, external-secrets Apache-2.0, prometheus-operator Apache-2.0, traefik MIT, opentofu MPL-2.0, sql_exporter MIT, opentelemetry-collector-contrib Apache-2.0, helm/kubeconform Apache-2.0 (công cụ kiểm, không phân phối). Không cần thêm giấy phép vào `allowed-licenses.txt`.

## Hệ quả

- **Chưa hiện thực, cần G4-0:** `dbadmin.Bootstrap` chưa lọc theo cụm. Trên mỗi cụm vật lý nó vẫn tạo mọi vai và mọi database.
  - Tạm thời: Job bootstrap đặt mật khẩu ngẫu nhiên, không dùng ở đâu (`db-bootstrap-foreign`), cho vai của vùng khác.
  - Hệ quả: database rỗng `ag_<cụm khác>` xuất hiện trên mỗi cụm. Vai của vùng khác chỉ CONNECT được vào database rỗng đó.
  - Đề xuất: thêm lọc cụm vào `Bootstrap` (cùng `AG_CLUSTERS`).
- Trong một vùng, cùng tên vai dùng chung mật khẩu giữa các cụm của vùng, vì `PasswordFor` không theo cụm. Ví dụ `ag_outbox_publisher` của `pg-mkg` và `pg-ts` (secret `mkg-db-outbox`).
- `worker`, `outbox-publisher`, `optimizer` chưa có endpoint sức khỏe nên không có probe. Nên thêm `AG_HEALTH_ADDR` như `worker-internal`.
- **identity-vault có hai đường ra** ngoài KMS mà bảng mục 17 không liệt kê:
  - JWKS của Keycloak (R-54);
  - OTLP tới Quản trị (không có dữ liệu cá nhân, R-91).

  CronJob neo chuỗi của cụm vault ghi manifest (chỉ băm) ra kho S3. Ba luồng này ghi rõ ở ADR-0008 "Hiện thực".
- MKG gọi nền tảng qua DMZ (hairpin về bộ điều khiển Ingress), không Pod-tới-Pod. Cần CNI đánh giá NetworkPolicy sau DNAT: Calico, Cilium đạt.
- Tên trường của CR plugin pgBackRest và step-ca PKCS#11 phải đối chiếu lại với phiên bản được ghim khi dựng staging. `deploy/k8s/tests/schemas/stanza_v1.json` là schema CRD `Stanza` lấy từ repo plugin (nhánh main, 30/09/2026) để kubeconform kiểm offline.
- Grafana, Loki, Tempo (mục 4 "Quan sát") là AGPL-3.0, ngoài RB-4. Chart này không cài chúng. Cần ADR riêng: dùng nguyên bản như dịch vụ tách rời, hoặc thay bằng thành phần Apache-2.0.
