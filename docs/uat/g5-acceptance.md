# Nghiệm thu cổng G5: yêu cầu → kiểm thử

**Khung** lập ngày 01/10/2026 ở G5-0, nhánh `sub/g5`; điền ở G5-1 và tích hợp; rà toàn bảng ở E2E G5 (02/10/2026). Cách điền:
- Agent module, hạ tầng, web và E2E điền cột kiểm thử và trạng thái khi làm.
- Người tích hợp rà lại toàn bảng trước khi đóng cổng, như [g4-acceptance.md](g4-acceptance.md).
- Mỗi yêu cầu có đúng một dòng.
- Tên kiểm thử ghi vào bảng phải tra được trong mã.

Yêu cầu lấy từ:
- **Kiến trúc v3.0:** Phụ lục A dòng G5; mục 2 (ĐTK 2: bên thứ ba, đơn vị thẩm định; Phase K); mục 11 (bề mặt Public, R-50..R-55); mục 12; mục 16 (`pf/metering`); mục 17–18 (phân vùng, DLP); mục 19; mục 21; R-01, RB-1..RB-7. Mục tương ứng ở v4.1 (W0.3, 04/10/2026): §2, §10, §11, §27, §29–§30, §31, §33. Các dòng dưới giữ số mục v3.0 vì là bằng chứng nghiệm thu đã lập; bảng đổi mục ở [g1-conventions](../dev/g1-conventions.md).
- **Tài liệu G5:** [g5-conventions.md](../dev/g5-conventions.md); [ADR-0022](../adr/0022-public-api-ben-thu-ba.md), [ADR-0023](../adr/0023-so-do-luong-su-dung-metering.md), [ADR-0024](../adr/0024-diem-kiem-soat-xuat-du-lieu-dlp.md), [ADR-0025](../adr/0025-ho-so-cap-phat-phase-k.md); [ADR-0020](../adr/0020-lien-thong-platform-mkg-khac-tenant.md) (cấp quyền chia sẻ).
- **Backlog KN** US-MRV-05 AC (chuyên gia thẩm định chỉ xem, mọi lần truy cập có nhật ký). Các BRD/backlog khác **không có** yêu cầu về Public API, đo lường, thu phí.
- **Prototype `mekong-green`:**
  - đối tác `customer`/`vendor`/`vvb`/`buyer`, vai `vvb_auditor`;
  - `SUB-DATA` Data Licensing (450 000 000 đ);
  - phản mẫu `mrv/sync-lakehouse`.

  Prototype là đặc tả chạy được, không chép mã. Prototype **không có** khóa API, sổ sử dụng, hạn mức theo khách hàng hay ngưỡng k.

Bốn trạng thái:
- **đạt**: có kiểm thử tự động chứng minh tiêu chí chấp nhận. Kiểm thử hợp lệ là: kiểm thử Go `Test…`, bước E2E-G5-xx, WEB-E2E `*.spec.ts`, Vitest, hoặc báo cáo diễn tập có lưu. Tham số **tạm, chờ khách** vẫn **đạt** nếu cơ chế (phiên bản, người đặt ≠ người duyệt) đã chứng minh.
- **một phần**: ghi rõ phần còn thiếu và loại — **mã**, **chờ khách**, **hạ tầng**.
- **chưa làm**: chưa có mã, chưa diễn tập.
- **ngoài G5 → G6/…**: chỉ cho hạng mục mà kiến trúc hoặc lộ trình đặt ở cổng sau.

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | chưa làm | ngoài G5 |
|---|---|---|---|---|---|
| 3. Phụ lục A (G5) | 4 | 2 | 2 | 0 | 0 |
| 4. Public API (ADR-0022) | 19 | 14 | 5 | 0 | 0 |
| 5. Sản phẩm dữ liệu | 5 | 2 | 3 | 0 | 0 |
| 6. `pf/metering` (ADR-0023) | 10 | 9 | 1 | 0 | 0 |
| 7. Điểm kiểm soát xuất — DLP (ADR-0024) | 12 | 11 | 1 | 0 | 0 |
| 8. Phase K và cấp phát bên thứ ba (ADR-0025) | 6 | 2 | 2 | 2 | 0 |
| 9. Cổng kiến trúc G5 (RB, R-xx, vùng) | 13 | 8 | 5 | 0 | 0 |
| 10. Client và kịch bản đầu–cuối | 4 | 4 | 0 | 0 | 0 |
| 11. Câu hỏi khách | 10 | 0 | 0 | 10 | 0 |
| **Tổng** | **83** | **52** | **19** | **12** | **0** |

Rà E2E G5 (02/10/2026, nhánh `g5-e`): trước rà 79 yêu cầu — 44 đạt, 19 một phần, 16 chưa làm. Sau rà 82 yêu cầu (thêm X-11..X-13 cho RB-4, RB-5, RB-6) — 51 đạt, 19 một phần, 12 chưa làm. Mọi tên kiểm thử trong bảng đã tra được trong mã (`func Test…`, WEB-E2E `g5-public-api.spec.ts`, Vitest `src/apiclient/g5.test.tsx`). Phần còn lại theo loại:
- **mã:** P-12 `Deprecation`/`Sunset` (khi có hợp đồng v2), P-15 con trỏ, P-19 OAuth client credentials (cũng chờ Q5-7);
- **chờ khách:** A-3 (Q5-1, Q5-8), D-01 (C-3), D-02 (Q5-6), D-05 (Q5-1), M-10 (Q5-9), P-09 / X-10 (Q5-10), K-06 + Q5-1..Q5-10;
- **hạ tầng:** A-4, P-08, K-01, K-02, K-04 (diễn tập staging), L-10 (`promtool`), X-02 (PgBouncer), X-07 (luật cảnh báo DLQ vùng nền tảng), X-09, X-11 (cổng giấy phép CI).

Đã có ở G5-0. Đây là nền móng, không tính là đạt dòng nào:

| Hạng mục | Kiểm thử |
|---|---|
| Hợp đồng sản phẩm dữ liệu mặc định từ chối | `TestEmbeddedProductsLoad_G5`, `TestValidateDefaultDeny_G5`, `TestMetaProblems_G5`, `TestDataProductContracts_G5` |
| Hợp đồng đóng băng | `TestG5FrozenDataProducts`, `TestParseKey_ADR0022`, `TestUnitsV1_ADR0023`, `TestGuardModes_ADR0024`, `TestParseMode_ADR0024` |
| Loại cấp quyền `dp_*` (DB thật) | `TestShareGrantDataProductKinds_G5` |
| archlint cho `cmd/api-public` (R-03, RB-3) và `agnofloat` cho `metering` | `TestBoundary`, `TestNoFloat` |
| Vai trong realm | `TestKeycloakRealmCoversRolesAndClaims` |

## 1. Cách chạy

Như [g4-acceptance.md §1](g4-acceptance.md#1-cách-chạy). Kịch bản E2E G5 (mục 2) và diễn tập bật Phase K ghi lệnh chạy ở đây khi có.

Hạ tầng Phase K: `powershell -File deploy/k8s/check.ps1` (online) và `-Offline` — helm lint + kubeconform cho dev/staging/prod và `prod-phasek` (= `values-prod.yaml` + `values-phasek.yaml`), rồi `check_flows.py`. OpenTofu: `tofu plan -var-file=envs/<env>.tfvars [-var phase_k=true]`. Thủ tục bật/tắt: [phase-k.md](../runbooks/phase-k.md).

## 2. Kịch bản đầu–cuối

`TestG5ThirdPartyToStatement` (`tests/e2e/g5_scenario_test.go`, E2E G5 02/10/2026) — **đạt** trên máy dev. Năm tenant: chương trình (chủ dữ liệu), MKG (van), đơn vị vận hành, nhà máy (bên thứ ba đầu tiên đề xuất), đơn vị thẩm định. Nền tảng dựng bằng gốc lắp ráp thật `cmd/api-platform/assembly` (harness), Public API bằng gốc lắp ráp thật `cmd/api-public/assembly` (tách khỏi `package main` ở E2E G5 để kịch bản dùng đúng mã của tiến trình). Bơm outbox thay outbox-publisher + RabbitMQ, giao tới đúng consumer của `cmd/worker` qua `shared.inbox`. Cấu hình Phase K bằng biến môi trường: pepper, `AG_PUBLIC_OPERATOR_TENANT`, `AG_EGRESS_MODE=enforce`.

Chạy:

```powershell
. .\scripts\dev-env.ps1
go test -count=1 -timeout 40m ./tests/e2e/ -run TestG5ThirdPartyToStatement -v
# ghi lại golden bảng kê: thêm -g5-update
```

| Bước | Bước con của kiểm thử | Nội dung | Dòng nghiệm thu |
|---|---|---|---|
| E2E-G5-01 | `01_du_lieu_chuong_trinh_va_van_MKG` | Seed dữ liệu chủ, cây tổ chức, danh mục MRV duyệt ≠ đặt; 10 thửa + vụ; cấp quyền van (đề xuất ≠ duyệt); 10 bảng kê HTX + 2 hub + sức chứa qua consumer van THẬT → một `egress_event` kênh `valve` mỗi phong bì ở tenant MKG; minh chứng AWD → xuất gói MRV 202 + job → completed, egress `mrv_package` | L-03, L-05, X-12 |
| E2E-G5-02 | `02_tenant_ben_thu_ba_cap_quyen_dp` | Tổ chức gốc loại `partner` cho tenant nhà máy + đơn vị thẩm định; cấp quyền `dp_collection_volume`, `dp_hub_capacity`, `dp_mrv_package_status` (tự duyệt → 403, người khác duyệt) | K-03, P-06 |
| E2E-G5-03 | `03_khach_hang_dang_ky_goi_han_muc_R56` | Khách hàng, đăng ký sản phẩm + scope, gói hạn mức: tự duyệt → 403 `apiclient.self_approval`, `api_client_approver` khác duyệt | P-05, K-03 |
| E2E-G5-04 | `04_khoa_bi_mat_mot_lan` | Bí mật một lần, `no-store`; gọi lặp không bí mật; khóa sai / thiếu → 401, không dòng sổ | P-03, P-19, X-05 |
| E2E-G5-05 | `05_doc_san_pham_k_an_danh_bac_hub_trang_thai_MRV` | Danh mục; `collection_volume` k = 10 (mọi ô ẩn) → `publicdata.k_min = 5` qua pf/masterdata (R-56) → một ô ≥ k; `hub_capacity` chỉ bậc; `mrv_package_status` danh sách + một gói + kiểm lại; 403 khi sai sản phẩm / thiếu scope / `grantor` lạ; header hợp đồng, `RateLimit-*`, `AG-Quota-Remaining` | D-01, D-02, D-03, P-06, P-07, P-11, P-12 |
| E2E-G5-06 | `06_xuat_202_tep_Digest` | Thiếu `Idempotency-Key` → 400; 202 + `Location`; gọi lặp đúng byte; worker → tệp; tải qua API, `Digest` khớp, ô dưới k không có | D-04, P-13, P-14 |
| E2E-G5-07 | `07_so_su_dung_egress_so_doc` | `usage_event` = lời gọi đã xác thực (kể cả 403); egress `public_api` + `partner_read_log` (K-05) = lần đọc / tải 2xx, ở tenant bên cấp; gọi lặp 0 đơn vị; `/v1/usage` của chính mình; `metering_viewer` bên cấp | A-2, M-01, M-05, L-02, K-05 |
| E2E-G5-08 | `08_het_han_muc_429_goi_moi` | Hết `daily_units` → 429 `metering.quota_exceeded` + `Retry-After`, dòng sổ 429 0 đơn vị; gói v2 duyệt → đọc lại | P-10, M-04 |
| E2E-G5-09 | `09_DLP_enforce_bat_thuong_thong_bao` | Tham số kênh (R-56): xuất báo cáo lần 2 → 429 `metering.egress_quota_exceeded` (egress `denied`, không `export_audit`); tải gói MRV vượt hạn mức kênh → 429; đúng một bất thường + một sự kiện → `pf.notify.egress_anomaly` → `metering_viewer` + `platform_admin` bên cấp nhận cảnh báo khẩn, tenant khác không | L-04, L-07, L-08, L-09, L-11 |
| E2E-G5-10 | `10_xoay_khoa_thu_hoi_401_403` | Xoay: khóa mới + chồng lấn; thu hồi thiếu lý do → 422; thu hồi khóa cũ → 401 không dòng sổ; thu hồi cấp quyền hub → 403 ngay, cấp quyền khác vẫn 200 | P-04, P-06 |
| E2E-G5-11 | `11_bang_ke_cuoi_ky_R56_CSV_golden` | Đồng hồ thật → 409 `period_not_ended`; "đầu tháng sau": 202 + job (gọi lặp cùng job) → draft; tự duyệt → 403; người khác duyệt; người duyệt thứ ba → 409 `statement_frozen`; kiểm băm khớp; CSV tất định = golden; tải qua `report_export`; bảng kê bù, bảng kê cũ giữ nguyên | M-06, M-07, M-09 |
| E2E-G5-12 | `12_R61_giao_lap_moi_su_kien` | Giao lặp mọi sự kiện → inbox bỏ qua, mọi bảng G5 không đổi | X-06, L-01 |
| E2E-G5-13 | `13_cach_ly_tenant_RB3` | Quét mọi phản hồi Public API + tệp xuất (PII, mã nội bộ, khóa đối tượng, ô dưới k), `Validate` từng phần tử; sổ đúng tenant; bí mật khóa không ở log / bảng nào | X-02, X-03, X-05 |
| E2E-G5-14 | `14_RB1_verify_chain_so_G5` | `verify_chain` mọi stream của chín sổ G5 ở mọi tenant; UPDATE/DELETE bị chặn | X-01, M-10 |

Không có trong kịch bản: job dựng tệp báo cáo (`pf.reports.export.requested`, chỉ kiểm lối yêu cầu qua checkpoint), RabbitMQ / PgBouncer / Valkey / Ingress thật (mục 12).

## 3. Phụ lục A (G5)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| A-1 | Phase K = thay cấu hình và số bản sao, **không sửa mã** (R-01, RB-5): `values-phasek.yaml` chỉ có cấu hình, bật bằng `helm upgrade` không build lại image (ADR-0025 §1) | `check_flows.py` `phasek_overlay_checks` (mọi lá của `values-phasek.yaml` thuộc `PHASEK_ALLOWED`: số bản sao, HPA/PDB, `env`, `enabled`, `external.valkey` — không image, không luồng) + `public_api_checks` hồ sơ `prod-phasek`; `deploy/k8s/check.ps1` (online và `-Offline`: helm lint --strict + kubeconform -strict cho dev/staging/prod/prod-phasek, `check_flows.py` 5 020 kiểm đạt, 02/10/2026 sau tích hợp G5 — thêm F29); [phase-k.md](../runbooks/phase-k.md) ; Phase K là chuyển cấu hình: cùng mã chạy cả hồ sơ gốc lẫn `prod-phasek` (`check_flows.py` hồ sơ dev/staging/prod/prod-phasek, `deploy/k8s/check.ps1 -Offline` 02/10/2026 sau E2E G5); `TestG5ThirdPartyToStatement` chạy `api-public` + `api-platform` chỉ bằng biến môi trường Phase K (`AG_APIKEY_PEPPER_V1`, `AG_PUBLIC_OPERATOR_TENANT`, `AG_EGRESS_MODE=enforce`), không cờ mã | **đạt** (cơ chế chỉ cấu hình; diễn tập `helm upgrade` trên staging thật theo dõi ở K-04) |
| A-2 | `pf/metering` bật: mỗi lời gọi Public API một dòng sổ chứng từ (mục 11, 16) | `TestRecordOneRowPerCall_EgressInDataTenant_ADR0023`, `TestMeteringLedgersAppendOnlySixWays_R20`; `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): mỗi lời gọi đã xác thực (200, 202, 429, 403) một dòng `usage_event`; ghi sổ lỗi → 503 ; `TestG5ThirdPartyToStatement` (E2E-G5-07): `usage_event` = số lời gọi đã xác thực của từng khách hàng (kể cả 403, 429), chỉ ở tenant đơn vị vận hành; bảng kê cuối kỳ từ cùng sổ (E2E-G5-11) | **đạt** |
| A-3 | Public API bật với bên thứ ba đầu tiên: khóa API lưu băm, scope, quota (mục 11) | `TestKeyRotationOverlapRevoke_ADR0022`, `TestScopeAndGrantDenied_ADR0022`, `TestQuotaExhausted429_ADR0022` ; `TestG5ThirdPartyToStatement` (E2E-G5-02..10): bên thứ ba đầu tiên (nhà máy) + đơn vị thẩm định cấp theo runbook, khóa HMAC bí mật một lần, scope theo đăng ký duyệt, hạn mức 429, xoay / thu hồi | **một phần** — mã đạt đầu–cuối; **chờ khách** Q5-1 (bên thứ ba đầu tiên, hợp đồng dữ liệu `approved`) và pháp lý Q5-8 (DPA, kênh giao bí mật) |
| A-4 | Public API **tắt ở ĐTK 1**: 0 bản sao, không host ingress, `AG_PUBLIC_API_ENABLED` ≠ `1` → `api-public` thoát lỗi | `check_flows.py` `public_api_checks` hồ sơ dev/staging/prod: Deployment `api-public` có (image build + ký, CI `images`) nhưng 0 bản sao, không HPA, không host `partner`, không Middleware, không pepper, `AG_PUBLIC_API_ENABLED` không đặt; Compose chỉ chạy với `--profile phasek`; OpenTofu `phase_k = false`; `TestDisabledFlagExits_ADR0022`, `TestEnabledRequiresOperatorTenant_ADR0022` | **một phần** — mã đạt (thoát lỗi khi tắt, thiếu tenant vận hành → không khởi động; 0 bản sao, không host ở hồ sơ gốc); **hạ tầng**: chưa chạy trên cụm thật |

## 4. Public API (ADR-0022)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| P-01 | Tiến trình riêng `cmd/api-public` chỉ có route Public; không import `internal/mkg/**` (R-03) và kho định danh, kể cả client (RB-3) | `TestBoundary`; tiến trình `cmd/api-public` chỉ có route `/v1` Public + `/healthz` `/readyz` | **đạt** |
| P-02 | Khóa API: chỉ lưu `key_prefix` + HMAC-SHA-256 với pepper theo phiên bản (≥ 32 byte, từ trình quản lý bí mật); so khớp thời gian hằng; xoay pepper không làm hỏng khóa cũ | `TestPepperVersionsAndRotation_ADR0022`, `TestPeppersFromEnv_ADR0022`, `TestKeyHashNeverStoresPlaintext_ADR0022` | **đạt** |
| P-03 | Bí mật khóa hiện **một lần** (`Cache-Control: no-store`); không có trong log, lỗi, DB, URL, sự kiện (R-14, R-92) — kiểm thử quét chuỗi | `TestKeyHashNeverStoresPlaintext_ADR0022` (quét bảng + log), `TestAdminIssueKeySecretOnce_ADR0022` (no-store, phát lại không bí mật, quét `shared.idempotency_key`), `TestUnauthenticatedNoLedger_ADR0022` | **đạt** |
| P-04 | Vòng đời khóa là sổ (`issued`/`rotated`/`revoked`/`expired`, RB-1); tối đa 2 khóa hiệu lực; xoay có hạn chồng lấn; thu hồi có lý do, hiệu lực ngay | `TestKeyRotationOverlapRevoke_ADR0022`, `TestKeyUsable_ADR0022` | **đạt** |
| P-05 | Scope cấp qua đăng ký sản phẩm: `api_client_admin` đề xuất ≠ `api_client_approver` duyệt (R-56, `app/` + CHECK DB) | `TestSelfApprovalBlocked_R56` (app/ + CHECK DB), `TestAdminIssueKeySecretOnce_ADR0022` | **đạt** |
| P-06 | Truy cập cần cấp quyền `dp_<sản phẩm>` đang hiệu lực của bên cấp (ADR-0020, `GrantLookup`); thu hồi → 403 ở lời gọi kế tiếp; `grantor` chỉ chọn bên cấp | `TestGrantLookupActiveGrant_ADR0022` (cổng `GrantLookup`: chỉ `dp_*`, duyệt, hạn, thu hồi, `grantor` chỉ chọn bên cấp); handler `api-public` → 403 `apiclient.forbidden` ở đợt A; `TestScopeAndGrantDenied_ADR0022`, `TestExportJobFlow_ADR0022` (thu hồi → 403 ở lần tải kế tiếp) — với `GrantLookup` giả; `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): `GrantLookup` thật (`party/port/grantlookup`), cấp quyền duyệt qua Admin API pf/party, thu hồi → 403 ở lời gọi kế tiếp ; `TestG5ThirdPartyToStatement` (E2E-G5-05, E2E-G5-10): bên thứ ba khác tenant, `grantor` không cấp quyền → 403, thu hồi `dp_hub_capacity` → 403 ngay, `dp_collection_volume` vẫn 200 | **đạt** |
| P-07 | Tenant của bên gọi lấy từ bản ghi khóa, không bao giờ từ path/query/body (tương đương R-16) | `TestReadCollectionVolumes_ADR0022` ; `TestG5ThirdPartyToStatement` (E2E-G5-05: `?grantor=` của tenant không cấp → 403) | **đạt** |
| P-08 | Gateway: giới hạn thô theo IP + giới hạn thân trên `partner.<domain>`, không thẩm quyền (R-55) | Chart `ingress.optionalHosts.partner` + `ingress.middlewares`: Traefik `Middleware` `public-api-ratelimit` (20 rps, burst 40 theo IP nguồn — tạm, chờ khách) và `public-api-body-limit` (`buffering.maxRequestBodyBytes` 1 MiB), chỉ `websecure`; `check_flows.py` hồ sơ `prod-phasek` kiểm Ingress tham chiếu đủ hai Middleware; kubeconform online (schema CRD Traefik) | **một phần** — **hạ tầng**: chưa áp trên cụm thật; `ipStrategy.depth` chờ chọn LB nhà cung cấp |
| P-09 | Backend: giới hạn tần suất theo khách hàng (Valkey nếu có, không thì trong tiến trình) → 429 `apiclient.rate_limited` + `Retry-After` | `TestRateLimit_ADR0022` (token bucket trong tiến trình, chia theo `AG_PUBLIC_REPLICAS`) | **một phần** — adapter Valkey **không làm** ở tích hợp G5: chưa có máy chủ Valkey cục bộ để kiểm thử tích hợp và chưa chọn nhà cung cấp trong lãnh thổ (**chờ khách** Q5-10); client ứng viên `valkey-go` (Apache-2.0) / `go-redis` (BSD-2-Clause) đều trong `deploy/policies/allowed-licenses.txt`; interface `RateLimiter` sẵn |
| P-10 | Hạn mức thẩm quyền ngày/tháng theo đơn vị từ bộ đếm `pf_metering` cùng giao dịch → 429 `metering.quota_exceeded` | `TestQuotaExactUnderConcurrency_ADR0023` (bộ đếm `pf_metering` cùng giao dịch, song song không vượt); `TestQuotaExhausted429_ADR0022` (kiểm sớm `Quotas` + thẩm quyền `Ledger.Record`, đơn vị đếm đúng) — với metering giả; `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): hết `daily_units` → 429 `metering.quota_exceeded` + `Retry-After` với `Quotas`/`Ledger` thật (pf/metering) ; `TestG5ThirdPartyToStatement` (E2E-G5-08: hết `daily_units` gói v1 → 429 + `Retry-After`, dòng sổ 429 0 đơn vị; gói v2 duyệt ≠ đề xuất → đọc lại) | **đạt** |
| P-11 | Header `RateLimit-Limit`/`-Remaining`/`-Reset`, `AG-Quota-Remaining` ở mọi phản hồi | `TestReadCollectionVolumes_ADR0022`, `TestRateLimit_ADR0022` | **đạt** |
| P-12 | R-50: `/v1`, header `AG-Data-Contract: <sản phẩm>.v<N>`; hợp đồng mới chạy song song, `Deprecation`/`Sunset` | `TestReadCollectionVolumes_ADR0022`, `TestExportJobFlow_ADR0022` (`AG-Data-Contract`) | **một phần** — mã: `Deprecation`/`Sunset` khi có hợp đồng v2 (chưa có) |
| P-13 | R-51: POST thiếu `Idempotency-Key` → 400; gọi lặp → đúng phản hồi đầu, 0 đơn vị | `TestExportJobFlow_ADR0022` (đúng byte phản hồi đầu, `replayed`, 0 đơn vị; khác thân → 422), `TestExportRequiresIdempotencyKey_ADR0022` ; `TestG5ThirdPartyToStatement` (E2E-G5-06: thiếu khóa → 400; gọi lặp → đúng byte, `Idempotent-Replayed`, `units = 0` ở sổ) | **đạt** |
| P-14 | R-52: xuất → 202 + `Location: /v1/jobs/{id}` (mẫu mục 12, ảnh chụp bất biến); tệp phát luồng qua API, **không** URL có chữ ký | `TestExportJobFlow_ADR0022` (202 + `Location`, đầu vào bất biến, tệp phát luồng qua API + `Digest`) ; `TestG5ThirdPartyToStatement` (E2E-G5-06: worker nền tảng qua bơm outbox, tải qua `api-public`, `Digest` khớp, không URL có chữ ký) | **đạt** |
| P-15 | R-53: phân trang cursor ở mọi danh sách | con trỏ của nguồn chuyển tiếp (`next_cursor`); Admin `GET /v1/api-clients` cursor | **một phần** — mã: con trỏ do nguồn (đợt S/D) hiện thực |
| P-16 | Lỗi RFC 9457 + `trace_id` (R-15, R-63), mã ở `docs/errors.md` (`apiclient.*`, `metering.*`) | `TestLedgerFailureNoBody_ADR0023`, `TestScopeAndGrantDenied_ADR0022`; mã `apiclient.*` ở `docs/errors.md` | **đạt** |
| P-17 | Lúc chạy: `dataproduct.Get` + `Validate` từng phần tử — trường lạ → không trả, `apiclient.contract_violation` | `TestDefaultDenyExtraField_ADR0022` | **đạt** |
| P-18 | Hợp đồng `draft` không phục vụ ở production (`residency.Production`) | `TestDraftNotServedInProduction_ADR0022` | **đạt** |
| P-19 | Lời gọi chưa xác thực không ghi sổ (chỉ log `key_prefix` + `ag_public_unauthenticated_total`); OAuth client credentials tùy chọn (R-54) | `TestUnauthenticatedNoLedger_ADR0022` (không dòng sổ, log chỉ prefix, metric `ag.public.unauthenticated`) | **một phần** — mã: `TokenAuthenticator` (OAuth client credentials) chưa hiện thực, chờ khách Q5-7 |

## 5. Sản phẩm dữ liệu (g5-conventions §6)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| D-01 | `collection_volume`: ô xã × vụ × tháng chỉ khi ≥ k nông hộ phân biệt (`publicdata.k_min`, tạm 10, C-3); ô dưới k **không bao giờ** xuất hiện, kể cả trong tệp xuất; `suppressed_cells` | `TestCollectionVolumeKAnonymity_G5_D01` (nguồn pf/cropcycle: ô ≥ k, ẩn sơ cấp + bổ sung, `Suppressed` trên cả bộ lọc, bộ lọc không lộ ô ẩn, cách ly tenant, chỉ trường hợp đồng), `TestSuppressPrimary_G5_D01`, `TestSuppressComplementaryRow_G5_D01`, `TestSuppressComplementaryFixedPoint_G5_D01`, `TestSuppressNothing_G5_D01` (quy tắc `publicdata.kanon.v1`, g5-conventions §11.3); `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): ô dưới k không có trong phản hồi lẫn tệp xuất, `meta.suppressed_cells`, `k_applied`; `TestG5ThirdPartyToStatement` (E2E-G5-05): k mặc định 10 → mọi ô ẩn; `publicdata.k_min = 5` đặt qua pf/masterdata (người đặt ≠ người duyệt, tự duyệt → 403) → `k_applied = 5`, chỉ ô ≥ k; ô dưới k không có trong mọi phản hồi và tệp xuất (quét E2E-G5-13) | **một phần** — mã đạt (cả tham số k khác mặc định qua `api-public` thật); **chờ khách** C-3 / Q5-3 (giá trị k) |
| D-02 | `hub_capacity`: hub đã công bố, bậc sử dụng thay tấn tồn; không lô/giá | `TestHubCapacityProductAndValveEgress_G5` (nguồn pf/geo: bậc thay tấn tồn, không used/lô/giá, khớp hợp đồng, con trỏ, cách ly tenant), `TestUtilizationBand_G5_D02`; `TestG5ThirdPartyToStatement` (E2E-G5-05): handler `api-public` thật, hub qua van MKG thật → chỉ `utilization_band` (`unknown` khi chưa có số), không `used_tons`/`available_tons`/lô | **một phần** — mã đạt; **chờ khách** Q5-6 (MKG đồng ý mở dữ liệu hub) |
| D-03 | `mrv_package_status`: đơn vị thẩm định chỉ xem trạng thái, băm, kiểm lại, số bằng chứng tổng hợp; không ảnh/tọa độ/nông hộ (KN US-MRV-05) | `TestMRVPackageProductAndEgress_G5` (nguồn pf/mrv: trạng thái, băm, số bằng chứng, kiểm lại; không người yêu cầu/khóa đối tượng/tọa độ; 404 tenant khác); `TestG5ThirdPartyToStatement` (E2E-G5-05, -07, -13): đơn vị thẩm định (tenant riêng) đọc danh sách + một gói qua `api-public` thật — trạng thái, băm, `last_verify_match`; mỗi lần đọc một dòng `partner_read_log` ở bên cấp; không người yêu cầu / khóa đối tượng / tọa độ (quét RB-3); không có scope `export` | **đạt** |
| D-04 | Xuất `collection_volume` CSV/JSON tất định (SHA-256), cùng ngưỡng k | `TestExportJobFlow_ADR0022`, `TestExportTooLargeAndDeterministic_ADR0022` (CSV/JSON tất định, SHA-256, `max_export_rows`); `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): tệp xuất CSV qua nguồn pf/cropcycle thật, cùng ngưỡng k, `Digest` khớp ; `TestG5ThirdPartyToStatement` (E2E-G5-06) | **đạt** |
| D-05 | `contractcheck`: struct `//ag:dataproduct` khớp hợp đồng, tag OpenAPI, `personal_data: false`, người đề xuất ≠ người phê duyệt; chuyển `approved` khi khách ký | (`TestDataProductContracts_G5` có từ G5-0) | **một phần** — cơ chế đạt (`TestDataProductContracts_G5`); **chờ khách** Q5-1 (ký hợp đồng → `approved`) |

## 6. `pf/metering` (ADR-0023, mục 16)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| M-01 | Sổ `usage_event` theo mẫu §8.1 (chuỗi băm, phân vùng tháng, RLS); một dòng mỗi lời gọi đã xác thực, kể cả 4xx sau xác thực | `TestRecordOneRowPerCall_EgressInDataTenant_ADR0023`, `TestMeteringLedgersAppendOnlySixWays_R20` | **đạt** |
| M-02 | Ghi sổ **trước** khi gửi byte; ghi lỗi → 503 `metering.unavailable`, không có dữ liệu | `TestRecordFailsClosed_ADR0023` (`Ledger.Record` lỗi → `metering.unavailable`); `TestLedgerFailureNoBody_ADR0023`, `TestQuotaExhausted429_ADR0022` | **đạt** |
| M-03 | Đơn vị `metering.units.v1` ghim `rule_version`; đổi quy tắc → v2, dòng cũ không đổi | `TestUnitsV1_ADR0023`, `TestRecordOneRowPerCall_EgressInDataTenant_ADR0023` | **đạt** |
| M-04 | Bộ đếm `usage_counter_current` cùng giao dịch, khóa dòng, giờ VN; `RebuildTenant` dựng lại = hiện hành (R-22) | `TestQuotaExactUnderConcurrency_ADR0023`, `TestRebuildProjectionsEqualsLive_R22`, `TestWindowsVietnamTime_ADR0023` | **đạt** |
| M-05 | `GET /v1/usage`: chỉ của chính khách hàng | `TestRecordOneRowPerCall_EgressInDataTenant_ADR0023` (`port.Usage` chỉ của chính khách hàng); `TestScopeAndGrantDenied_ADR0022` (`usage:read`, `client_id` từ khóa); `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): `GET /v1/usage` qua `Usage.UsageOf` thật, chỉ của chính khách hàng | **đạt** |
| M-06 | Bảng kê kỳ tháng: 202 + job, ảnh chụp bất biến (first/last seq, băm cuối chuỗi, SHA-256); tất định | `TestStatementLifecycle_SoD_Deterministic_ADR0023`, `TestSnapshotCanonicalHash_ADR0023` | **đạt** |
| M-07 | Duyệt bảng kê `billing_approver` ≠ `billing_officer` yêu cầu (R-56, `app/` + CHECK DB); đã duyệt → 409; sửa = bảng kê bù | `TestStatementLifecycle_SoD_Deterministic_ADR0023` (tự duyệt → 403 ở `app/` và CHECK 23514; đã duyệt → 409; bảng kê bù) | **đạt** |
| M-08 | Giá tùy chọn, có phiên bản (`shared/money`, `numeric(18,4)`/`(18,2)`, không float — `agnofloat`); chưa có giá → chỉ đơn vị | `TestStatementOptionalPricing_ADR0023`, `TestStatementCSVGolden_ADR0023`, `TestNoFloat` | **đạt** (giá trị chờ khách Q5-4; cơ chế tham số pf/masterdata setter ≠ approver) |
| M-09 | CSV bảng kê tất định byte-cho-byte, tải qua checkpoint kênh `report_export` | `TestStatementCSVGolden_ADR0023`, `TestStatementLifecycle_SoD_Deterministic_ADR0023` (CSV byte-cho-byte, tải ghi `egress_event` kênh `report_export`) | **đạt** |
| M-10 | `verify_chain`, neo hằng ngày (job `anchor`), lưu chứng từ chuyển tầng không xóa | `TestMeteringLedgersAppendOnlySixWays_R20` (`verify_chain` ba sổ; neo hằng ngày qua job `anchor` sẵn có — `shared.chain_head`) | **một phần** (chờ khách Q5-9: thời hạn lưu, chuyển tầng) |

## 7. Điểm kiểm soát xuất — DLP (ADR-0024, mục 18)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| L-01 | Một điểm kiểm soát `Checkpoint.Pass`: sổ `egress_event` (tenant = chủ dữ liệu, chuỗi băm), idempotent theo (kênh, nguồn, bên nhận) | `TestCheckpointObserveEnforce_ADR0024` (mỗi kênh một dòng, giao lặp không thêm), `TestMeteringTenantIsolationParallel_RB2` | **đạt** |
| L-02 | Public API: dòng egress cùng giao dịch với sổ sử dụng, luôn đóng khi lỗi | `TestRecordOneRowPerCall_EgressInDataTenant_ADR0023`, `TestCheckpointObserveEnforce_ADR0024` (Public API luôn enforce), `TestRecordFailsClosed_ADR0023` | **đạt** |
| L-03 | `pf/mrv`: yêu cầu xuất gói + `PackageURL` qua `Guard` kênh `mrv_package` | `TestMRVPackageProductAndEgress_G5` (yêu cầu xuất + cấp URL tải gói qua `Guard` kênh `mrv_package`; observe / enforce / checkpoint lỗi / nil); `TestExportEndToEnd` không đổi | **đạt** |
| L-04 | `pf/reports`: ba lối (yêu cầu, lịch, tải) qua `Guard` kênh `report_export`; điểm kiểm soát cục bộ + `pf.reports.export.anomaly` giữ nguyên (kiểm thử G1 không đổi) | `TestReportExportEgress_ADR0024` (yêu cầu, tải), `TestScheduledExportEgress_ADR0024` (lịch); `TestQuotaAndAnomaly`, `TestScheduleFiresOncePerPeriodAndDelivers` không đổi | **đạt** |
| L-05 | Van: ba consumer gọi `Guard` kênh `valve` sau `ValveTargets`, mỗi bên cấp một lần; giao lặp không thêm dòng; `enforce` từ chối → DLQ (R-62) | `TestHubCapacityProductAndValveEgress_G5` (`pf.geo.hub_valve`: mỗi bên cấp một lần, giao lặp không gọi thêm, enforce → lỗi, không ghi), `TestCollectionVolumeKAnonymity_G5_D01` (`pf.cropcycle.htx_collection_valve`), `TestMRVPackageProductAndEgress_G5` (`pf.mrv.collection_evidence`); kiểm thử van G3/G4 không đổi | **đạt** |
| L-06 | `/v1/partner/*` (ADR-0020): `RecordRead` qua `Guard` kênh `partner_share` (P2) | `TestPartnerShareEgress_ADR0024` (kênh `partner_share`; enforce từ chối → không ghi sổ đọc); `TestShareGrantLifecycle_SoD_Audit_ADR0020` không đổi; `TestRecordReadAPIClient_ADR0022` (khách hàng Public API không đếm hai lần) | **đạt** |
| L-07 | Hạn mức kênh `metering.egress.<kênh>.daily_records_quota` (tham số có phiên bản) | `TestCheckpointObserveEnforce_ADR0024` (tham số có phiên bản ở pf/masterdata; mặc định §1.2) | **đạt** (giá trị chờ khách Q5-5) |
| L-08 | Bất thường: × trung vị 28 ngày / ngưỡng tuyệt đối; lần đầu trong ngày → đúng một `pf.metering.egress.anomaly_detected` → `pf.notify.egress_anomaly` | `TestEgressAnomalyFirstOfDayOnly_ADR0024` (tuyệt đối + 3 × trung vị 28 ngày; đúng một sự kiện/ngày); `TestEgressAnomalyNotifiesDLPRecipients_ADR0024` (consumer `pf.notify.egress_anomaly`: người nhận DLP `metering_viewer` + `platform_admin` của tenant chủ dữ liệu — tạm, chờ khách Q5-5; tenant khác không nhận; giao lặp bỏ qua; payload hỏng → lỗi/DLQ; migration pf 000083) | **đạt** |
| L-09 | `AG_EGRESS_MODE`: `observe` không bao giờ từ chối (kể cả checkpoint lỗi); `enforce` từ chối đúng mã | `TestGuardModes_ADR0024`, `TestParseMode_ADR0024`, `TestEgressModeFromEnv_ADR0024` (gốc lắp ráp), `TestCheckpointObserveEnforce_ADR0024` (checkpoint pf/metering thật); từng lối: `TestReportExportEgress_ADR0024`, `TestMRVPackageProductAndEgress_G5`, `TestHubCapacityProductAndValveEgress_G5`, `TestPartnerShareEgress_ADR0024` | **đạt** |
| L-10 | Metric `ag_egress_*` nhãn cardinality thấp (R-91); luật `agrigreen-egress.rules.yml` | `TestCheckpointObserveEnforce_ADR0024`, `TestEgressAnomalyFirstOfDayOnly_ADR0024` (metric `ag_egress_*`); `deploy/observability/prometheus/agrigreen-egress.rules.yml` + bản sao chart (`check_flows.py`) | **một phần** (hạ tầng: `promtool check rules` chỉ chạy trên CI, chưa chạy cục bộ) |
| L-11 | `GET /v1/metering/egress`, `/anomalies` (Admin API): chỉ tenant chủ dữ liệu thấy dòng của mình | `TestMeteringTenantIsolationParallel_RB2`, `TestEgressAnomalyFirstOfDayOnly_ADR0024` | **đạt** |
| L-12 | Xuất nội bộ KN (quyết định A5, 03/10/2026): xuất lịch công tác và tải tệp thửa KN qua `Guard` kênh `report_export` với `Egress.Internal` → luôn `observe` (ghi `egress_event`, đếm, bất thường), không bao giờ bị chặn kể cả `AG_EGRESS_MODE=enforce`; kênh ngoài vẫn bị chặn | `TestGuardInternalNeverDenied_A5`, `TestCheckpointInternalObserveUnderEnforce_A5` (checkpoint thật, enforce + vượt hạn mức), `TestFN_TI_02_07_UnregisteredBulkRemindersExport`, `TestFN_XA_03_05_CreateListGetUpdateCorrectLog` (handler: checkpoint giả từ chối vẫn 200, nhận `Internal`) | **đạt** |

## 8. Phase K và cấp phát bên thứ ba (ADR-0025)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| K-01 | `values-phasek.yaml`: `api-public` 2 bản sao + PDB, host `partner.<domain>`, F08/F19; luồng cấm (`api-public` → vault/MQ/DB mkg-ts/Internet) bị chặn — `check_flows.py` | `check_flows.py` `public_api_checks` (mọi hồ sơ: F08 Ingress → `api-public:8085`, F19 → PgBouncer pf, DNS, OTLP được phép; mọi nguồn khác → `api-public` bị chặn; `api-public` → `identity-vault`, RabbitMQ 5672/15672, PostgreSQL pf trực tiếp, DB mkg/ts/vault/iam, ứng dụng khác, Internet, dải ngoài cụm bị chặn; host `partner` chỉ tới `api-public:8085`; Phase K: 2 bản sao, PDB ≥ 1, TLS `agrigreen-partner-tls`, `AG_EGRESS_MODE=enforce`); luồng tùy chọn F26 (Valkey) / F27 (scrape) / F28 (Keycloak) chỉ mở khi cấu hình; `deploy/k8s/check.ps1` (online và `-Offline`: helm lint --strict + kubeconform -strict cho dev/staging/prod/prod-phasek, `check_flows.py` 5 020 kiểm đạt, 02/10/2026 sau tích hợp G5 — thêm F29); ADR-0008 mục hiện thực | **một phần** — **hạ tầng**: chưa áp trên cụm thật (CNI thực thi NetworkPolicy) |
| K-02 | Pepper khóa API qua ExternalSecret (KMS trong lãnh thổ), xoay theo phiên bản | ExternalSecret `pf-apikey-pepper` (`dataFrom.extract` của `<remotePrefix>/apikey/pf`: thêm `AG_APIKEY_PEPPER_V<n+1>` + đổi `CURRENT` chỉ ở kho, không sửa chart), bật ở Phase K, chỉ vùng pf (`api-public` + `api-platform` tùy chọn); vai DB `pf-public-db-roles` đúng tập g5-conventions §3.1, `pg-pf-roles` có `public_api`/`apiclient_app`/`metering_app` — `check_flows.py`; không giá trị bí mật trong repo; Compose `gen-dev-secrets` sinh `AG_APIKEY_PEPPER_V1` | **một phần** — **hạ tầng**: chưa có kho bí mật trong lãnh thổ thật; **mã**: kiểm khóa theo `pepper_version` (đợt A, P-02) |
| K-03 | Runbook `public-api-onboarding.md`: tenant bên thứ ba → cấp quyền `dp_*` → khách hàng + đăng ký → khóa → thử chấp nhận → bảng kê; ngừng hợp tác | —; [public-api-onboarding.md](../runbooks/public-api-onboarding.md) (pháp lý → tenant → cấp quyền `dp_*` → khách hàng + đăng ký + gói → khóa + giao bí mật → thử chấp nhận → bảng kê; xoay, thu hồi, ngừng hợp tác); các bước 2–5 chạy thật trong `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026) ; `TestG5ThirdPartyToStatement` chạy đầu–cuối các bước 1–8 của runbook (tenant + tổ chức gốc `partner`, cấp quyền, khách hàng, đăng ký, gói, khóa, thử chấp nhận, bảng kê, xoay, thu hồi khóa và cấp quyền); sửa runbook: `verify` là `GET` | **đạt** |
| K-04 | Diễn tập bật Phase K trên staging chỉ bằng `helm upgrade -f values-phasek.yaml` | — | **chưa làm** (hạ tầng) |
| K-05 | Đơn vị thẩm định: tenant riêng, chỉ xem, mọi truy cập có nhật ký (sổ sử dụng + egress + `partner_read_log`) — KN US-MRV-05 | `TestRecordReadAPIClient_ADR0022` (lần đọc của khách hàng API → một dòng `partner_read_log` ở tenant bên cấp, người đọc = mã khách hàng); sổ sử dụng + egress ở đợt A/M; **K-05 đóng ở E2E G5:** `api-public` ghi `partner_read_log` qua bộ điều hợp không vault `party/port/grantlookup` (`port.APIReadLog`), ghi TRƯỚC sổ đo lường, lỗi → 503 `party.partner_audit_unavailable` không dữ liệu — `TestAPIReadLogAdapter_K05`, `TestPublicAPIRealWiring_G5Integration` (sổ đọc + 503), `TestG5ThirdPartyToStatement` (E2E-G5-07: số dòng sổ đọc = số lần đọc / tải 2xx của từng khách hàng, đơn vị thẩm định 2/2) | **đạt** (danh tính VVB và client credentials theo dõi ở K-06 / Q5-7) |
| K-06 | Keycloak client `agrigreen-public` + client bí mật cho từng đơn vị thẩm định, khi khách chọn | — | **chưa làm** (chờ khách Q5-7) |

## 9. Cổng kiến trúc G5 (RB, R-xx, vùng)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| X-01 | RB-1: sáu phép thử UPDATE/DELETE/TRUNCATE trên mọi sổ mới; `v_missing_immutability` rỗng | `TestMeteringLedgersAppendOnlySixWays_R20` (pf_metering; `v_missing_immutability` rỗng); `TestAppendOnlySixWays_G5Apiclient` (sổ `pf_apiclient`, `v_missing_immutability`), `verify_chain` trong `TestKeyRotationOverlapRevoke_ADR0022` ; `TestG5ThirdPartyToStatement` (E2E-G5-14): `verify_chain` trên TỪNG stream của chín sổ G5 (`pf_apiclient` bốn, `pf_metering` ba, `pf_party` hai) ở mọi tenant có dữ liệu; UPDATE/DELETE bằng vai ứng dụng bị chặn | **đạt** |
| X-02 | RB-2: cách ly tenant ở mọi bảng mới, kể cả song song qua PgBouncer transaction mode | `TestMeteringTenantIsolationParallel_RB2` (pf_metering); `TestTenantIsolation_G5` (`pf_apiclient`) ; `TestG5ThirdPartyToStatement` (E2E-G5-13): sổ sử dụng chỉ ở tenant vận hành, egress `public_api` + sổ đọc chỉ ở bên cấp, `metering_viewer` mỗi tenant chỉ thấy dòng của mình, khóa của khách hàng khác → 404 job | **một phần** — cả `pf_apiclient` và `pf_metering` đạt trực tiếp PostgreSQL; song song qua PgBouncer chưa chạy (**hạ tầng**) |
| X-03 | RB-3: Public API không trả dữ liệu cá nhân (hợp đồng `personal_data: false`, archlint không vault, kiểm thử quét phản hồi) | Nguồn chỉ điền trường DTO, kiểm `Validate` + quét chuỗi: `TestCollectionVolumeKAnonymity_G5_D01`, `TestHubCapacityProductAndValveEgress_G5`, `TestMRVPackageProductAndEgress_G5`; `TestBoundary`, `TestDefaultDenyExtraField_ADR0022`, hợp đồng `personal_data: false`; `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public` thật — `Build` + `integrationPorts` thật trên DB thật, tích hợp G5 02/10/2026): quét phản hồi đầu–cuối (không mã xã dưới k, `household_ref`/`plot_ref`/`cooperative_ref`); `TestNoVaultTransitiveDeps_RB3` (nhị phân không kéo vault/mkg/handler pf/party kể cả gián tiếp) ; `TestG5ThirdPartyToStatement` (E2E-G5-13): quét MỌI phản hồi Public API + tệp xuất (mã thửa / nông hộ / vụ / HTX, mã người dùng, `vault`, khóa đối tượng, SĐT, email, ô dưới k) và `Validate` từng phần tử + khóa gốc; archlint `noVaultRoots` phủ cả `cmd/api-public/assembly` | **đạt** |
| X-04 | R-10: `agnofloat` phủ `internal/platform/metering/**` | `TestNoFloat`; mọi package `internal/platform/metering/**` (kể cả kiểm thử) không float | **đạt** |
| X-05 | R-14/R-92: không log khóa, bí mật, URL có chữ ký (`agpiilog` với `IssuedKey.Secret pii:"true"`) | `TestKeyHashNeverStoresPlaintext_ADR0022`, `TestUnauthenticatedNoLedger_ADR0022`, `TestAdminIssueKeySecretOnce_ADR0022`; `IssuedKey.Secret pii:"true"` (agpiilog) ; `TestG5ThirdPartyToStatement` (E2E-G5-04, -13): bí mật mọi khóa (kể cả khóa xoay) không có trong log ba server và trong mọi bảng `pf_apiclient`/`pf_metering`/`pf_party`/`shared`; gọi lặp phát hành không trả bí mật | **đạt** |
| X-06 | R-61: ba consumer G5 idempotent (giao lặp) | `TestStatementLifecycle_SoD_Deterministic_ADR0023` (giao lặp `pf.metering.statement.requested` → bỏ qua); `TestExportJobFlow_ADR0022` (giao lặp `pf.apiclient.export` → không làm gì); `TestEgressAnomalyNotifiesDLPRecipients_ADR0024` (giao lặp `pf.notify.egress_anomaly` → bỏ qua) ; `TestG5ThirdPartyToStatement` (E2E-G5-12): giao lặp MỌI sự kiện đã giao (van MKG collection/hub, xuất MRV, `pf.apiclient.export`, `pf.metering.statement`, `pf.notify.egress_anomaly`) → inbox bỏ qua, mọi bảng G5 không đổi | **đạt** |
| X-07 | R-62: DLQ + cảnh báo cho ba hàng đợi G5 | DLQ `<consumer>.dlq` khai báo cho mọi hàng đợi của worker (`internal/shared/inbox/rabbit.go`), ba hàng đợi G5 có trong `assembly.Queues` (`TestWorkerConsumersDeclaredByPublisher`), gauge `ag.rabbitmq.dlq.depth` (`TestDLQDepth`) | **một phần** — **hạ tầng**: chưa có luật Prometheus cảnh báo DLQ cho vhost nền tảng (chỉ có `AgrigreenMkgInternalDLQNotEmpty` cho `mkg.internal`); RabbitMQ thật chưa chạy cục bộ |
| X-08 | R-64: ba schema sự kiện G5 tương thích ngược (`contractcheck -base`) | `go run ./tools/contractcheck -base 3ca36b3` (G5-0) sạch, 02/10/2026 (E2E G5): ba schema `contracts/events/pf.apiclient.export.requested.v1.json`, `pf.metering.statement.requested.v1.json`, `pf.metering.egress.anomaly_detected.v1.json` không đổi từ khi đóng băng | **đạt** |
| X-09 | R-80/R-81: `api-public` ở vùng ứng dụng, chỉ PgBouncer pf; NetworkPolicy mặc định từ chối | `api-public` ở `agrigreen-pf` (default-deny), cứng hóa Pod (non-root, rootfs chỉ đọc, seccomp, không token SA) và Secret đúng vùng — `check_flows.py` `structure_checks` + `public_api_checks` (xem K-01) | **một phần** — **hạ tầng**: chưa áp trên cụm thật |
| X-10 | RB-7: Valkey, kho tệp xuất, nhật ký Public API trong lãnh thổ | Valkey chỉ qua `external.valkey` (dải trong lãnh thổ, F26; rỗng → không mở, `check_flows.py`); `api-public` không ra Internet; log/metric qua OTLP tới `agrigreen-ops` (F50); `global.residencyAllowlist` như mọi dịch vụ Go; kho tệp xuất: luồng F29 `api-public` → `external.objectStorage` 443 + khóa S3 chỉ đọc `pf-public-s3` (Phase K) — `check_flows.py` (tích hợp G5) | **một phần** — **chờ khách** Q5-10 (nhà cung cấp Valkey); **hạ tầng**: chưa áp trên cụm thật |
| X-11 | RB-4: giấy phép phụ thuộc cho phép dùng trong nền tảng thu phí | G5 không thêm phụ thuộc: `git diff 3ca36b3..` (G5-0) của `go.mod`, `go.sum`, `apps/web-ops/package.json` rỗng (E2E G5 02/10/2026); ứng viên Valkey trong `deploy/policies/allowed-licenses.txt` (P-09) | **một phần** — **hạ tầng**: cổng rà giấy phép chạy ở CI (GitHub Actions bị chặn), chưa chạy cục bộ |
| X-12 | RB-5 / R-01: mã chịu cấu hình ĐTK 2, chuyển giai đoạn không sửa mã; ranh giới module | archlint (`go run ./tools/archlint ./...` sạch): `zoneRoots` + `noVaultRoots` phủ cả cây `cmd/api-public` (kể cả `cmd/api-public/assembly` tách ở E2E G5), `TestBoundary`; `TestNoVaultTransitiveDeps_RB3` (`go list -deps`); `TestDisabledFlagExits_ADR0022`; `check_flows.py` `phasek_overlay_checks` + `public_api_checks` mọi hồ sơ (A-1); `TestG5ThirdPartyToStatement` bật Phase K chỉ bằng biến môi trường | **đạt** |
| X-13 | RB-6: số đo ở biên giữa hai bên có đối soát chéo | G5 không thêm số đo ở biên. Đối soát xuất qua van: số dòng `egress_event` kênh `valve` ở tenant MKG = số phong bì đã nhận × bên cấp (`TestG5ThirdPartyToStatement` E2E-G5-01; `TestHubCapacityProductAndValveEgress_G5`); đối soát cân G3/G4 không đổi | **đạt** |

## 10. Client và kịch bản đầu–cuối

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| W-01 | `apps/web-ops`: khách hàng API, đăng ký sản phẩm, gói hạn mức, khóa (hiện bí mật một lần, xoay, thu hồi) — R-56 trên UI chỉ phản ánh `app/` | Màn `/quan-tri/khach-hang-api` (mock backend `src/api/mock/g5.ts`). WEB-E2E `g5-public-api.spec.ts`: “G5 W-01: vòng đời khách hàng…”, “G5 W-01: phát hành khóa — bí mật hiện MỘT lần, đóng/tải lại là mất…”, “G5 W-01: xoay khóa…”. Vitest `src/apiclient/g5.test.tsx` (`<SecretOnceDialog>`, bảng khóa không lộ bí mật vào DOM/storage/log, phát lại Idempotency-Key không có `api_key`, `apiclient.self_approval`, quy tắc hiển thị) | **đạt** (trên mock; nối API thật cùng hợp đồng `admin.yaml`) |
| W-02 | `apps/web-ops`: bảng kê (yêu cầu, duyệt, tải CSV) | Màn `/quan-tri/bang-ke` + `/quan-tri/su-dung-api` (tổng theo kỳ, sổ sử dụng cursor, tham số). WEB-E2E “G5 W-02: bảng kê — A yêu cầu (202 + job), không tự duyệt; B duyệt, kiểm băm, tải CSV”. Vitest: tiền VND chuỗi thập phân (BigInt, không float), `metering.self_approval`, `metering.period_not_ended`, CSV cùng định dạng `domain.StatementCSV` | **đạt** (trên mock) |
| W-03 | `apps/web-ops`: nhật ký xuất và cảnh báo bất thường theo tenant | Màn `/quan-tri/nhat-ky-xuat` (lọc kênh, ngày, bên nhận; tab cảnh báo). WEB-E2E “G5 W-03: nhật ký xuất và cảnh báo bất thường — lọc kênh, bên nhận”, “G5: phân quyền màn hình…”. Vitest sổ xuất (kênh lạ → 422, vai không đọc → 403) | **đạt** (trên mock) |
| E-01 | Kịch bản `TestG5ThirdPartyToStatement` (mục 2) đạt trên máy dev | `TestG5ThirdPartyToStatement` (`tests/e2e/g5_scenario_test.go`, 14 bước — mục 2) đạt trên máy dev 02/10/2026; golden `tests/e2e/testdata/g5_statement.golden.csv` | **đạt** |

## 11. Câu hỏi khách

| Mã | Câu hỏi | Kiểm thử | Trạng thái |
|---|---|---|---|
| Q5-1 | Bên thứ ba đầu tiên, sản phẩm, độ chi tiết; `SUB-DATA` có thật không | — | **chưa làm** (chờ khách) |
| Q5-2 | Pháp nhân đơn vị vận hành Public API, ai lập hóa đơn | — | **chưa làm** (chờ khách) |
| Q5-3 | C-3 / Q-MKG-11: giá trị k; phạm vi áp | — | **chưa làm** (chờ khách) |
| Q5-4 | Mô hình tính phí, đơn giá, hoàn đơn vị | — | **chưa làm** (chờ khách) |
| Q5-5 | Hạn mức, ngưỡng bất thường theo kênh; có chặn van; người nhận cảnh báo | — | **chưa làm** (chờ khách) |
| Q5-6 | MKG đồng ý mở dữ liệu hub cho bên thứ ba | — | **chưa làm** (chờ khách) |
| Q5-7 | Đơn vị thẩm định: danh tính, client credentials / mTLS, nhận gói bằng chứng (C-1, DPA) | — | **chưa làm** (chờ khách) |
| Q5-8 | Mẫu hợp đồng dữ liệu / DPA; kênh giao bí mật | — | **chưa làm** (chờ khách) |
| Q5-9 | Thời hạn lưu chứng từ | — | **chưa làm** (chờ khách) |
| Q5-10 | Nhà cung cấp Valkey trong lãnh thổ | — | **chưa làm** (chờ khách) |

## 12. Giới hạn đã biết của bộ kiểm thử

- Máy dev (Windows, PostgreSQL 17 cục bộ) không có Docker, RabbitMQ thật, Kubernetes hay Valkey. Ingress, NetworkPolicy, ExternalSecret và giới hạn tần suất dùng chung chỉ kiểm được trên cụm.
- GitHub Actions bị chặn, nên kiểm tra chạy cục bộ theo danh sách ở g2-common.
