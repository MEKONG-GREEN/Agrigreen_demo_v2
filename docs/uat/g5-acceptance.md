# Nghiệm thu cổng G5: yêu cầu → kiểm thử

**Khung** lập ngày 01/10/2026 ở G5-0, nhánh `sub/g5`. Đợt G5-1 A (`pf/apiclient` + `cmd/api-public`) đã điền các dòng của mình. Cách điền:
- Agent module, hạ tầng, web và E2E điền cột kiểm thử và trạng thái khi làm.
- Người tích hợp rà lại toàn bảng trước khi đóng cổng, như [g4-acceptance.md](g4-acceptance.md).
- Mỗi yêu cầu có đúng một dòng.
- Tên kiểm thử ghi vào bảng phải tra được trong mã.

Yêu cầu lấy từ:
- **Kiến trúc v3.0:** Phụ lục A dòng G5; mục 2 (ĐTK 2: bên thứ ba, đơn vị thẩm định; Phase K); mục 11 (bề mặt Public, R-50..R-55); mục 12; mục 16 (`pf/metering`); mục 17–18 (phân vùng, DLP); mục 19; mục 21; R-01, RB-1..RB-7.
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
| 3. Phụ lục A (G5) | 4 | 0 | 2 | 2 | 0 |
| 4. Public API (ADR-0022) | 19 | 12 | 6 | 1 | 0 |
| 5. Sản phẩm dữ liệu | 5 | 0 | 1 | 4 | 0 |
| 6. `pf/metering` (ADR-0023) | 10 | 1 | 1 | 8 | 0 |
| 7. Điểm kiểm soát xuất — DLP (ADR-0024) | 11 | 0 | 0 | 11 | 0 |
| 8. Phase K và cấp phát bên thứ ba (ADR-0025) | 6 | 0 | 0 | 6 | 0 |
| 9. Cổng kiến trúc G5 (RB, R-xx, vùng) | 10 | 1 | 4 | 5 | 0 |
| 10. Client và kịch bản đầu–cuối | 4 | 0 | 0 | 4 | 0 |
| 11. Câu hỏi khách | 10 | 0 | 0 | 10 | 0 |
| **Tổng** | **79** | **14** | **14** | **51** | **0** |

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

## 2. Kịch bản đầu–cuối (chưa có)

Đề xuất `TestG5ThirdPartyToStatement` (`tests/e2e/g5_scenario_test.go`), với ba tenant: chương trình (chủ dữ liệu), đơn vị vận hành, bên thứ ba (nhà máy).

| Bước | Nội dung |
|---|---|
| E2E-G5-01 | Bên cấp duyệt cấp quyền `dp_collection_volume` + `dp_hub_capacity` cho tenant bên thứ ba (đề xuất ≠ duyệt) |
| E2E-G5-02 | `api_client_admin` đăng ký khách hàng + đăng ký sản phẩm + gói hạn mức; `api_client_approver` khác người duyệt; tự duyệt → 403 |
| E2E-G5-03 | Phát hành khóa (bí mật một lần, không có trong log/DB); khóa sai → 401, không dòng sổ |
| E2E-G5-04 | `GET /v1/data/collection-volumes`: chỉ ô ≥ k nông hộ, `meta.suppressed_cells`, `AG-Data-Contract`, `RateLimit-*`; một `usage_event` + một `egress_event` ở tenant bên cấp + `partner_read_log` |
| E2E-G5-05 | Xuất 202 + job → worker → `GET /v1/jobs/{id}/file` (đơn vị theo dòng); gọi lặp `Idempotency-Key` → cùng phản hồi, 0 đơn vị |
| E2E-G5-06 | Vượt hạn mức → 429 `metering.quota_exceeded` + `Retry-After`; dòng sổ `status = 429`, `units = 0` |
| E2E-G5-07 | Thu hồi cấp quyền → 403 ngay; thu hồi khóa → 401 |
| E2E-G5-08 | DLP: xuất báo cáo + gói MRV + phong bì van đi qua checkpoint (observe); vượt ngưỡng → đúng một `pf.metering.egress.anomaly_detected`; `enforce` → từ chối, van vào DLQ |
| E2E-G5-09 | Bảng kê kỳ: 202 + job, ảnh chụp băm, duyệt ≠ yêu cầu, CSV tất định; bảng kê bù |
| E2E-G5-10 | `verify_chain` mọi sổ G5; RB-1 sáu phép thử; cách ly tenant |

## 3. Phụ lục A (G5)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| A-1 | Phase K = thay cấu hình và số bản sao, **không sửa mã** (R-01, RB-5): `values-phasek.yaml` chỉ có cấu hình, bật bằng `helm upgrade` không build lại image (ADR-0025 §1) | — | **chưa làm** |
| A-2 | `pf/metering` bật: mỗi lời gọi Public API một dòng sổ chứng từ (mục 11, 16) | — | **chưa làm** |
| A-3 | Public API bật với bên thứ ba đầu tiên: khóa API lưu băm, scope, quota (mục 11) | `TestKeyRotationOverlapRevoke_ADR0022`, `TestScopeAndGrantDenied_ADR0022`, `TestQuotaExhausted429_ADR0022` | **một phần** — cơ chế đạt; chờ khách Q5-1 (bên thứ ba đầu tiên, hợp đồng `approved`) |
| A-4 | Public API **tắt ở ĐTK 1**: 0 bản sao, không host ingress, `AG_PUBLIC_API_ENABLED` ≠ `1` → `api-public` thoát lỗi | `TestDisabledFlagExits_ADR0022`, `TestEnabledRequiresOperatorTenant_ADR0022` | **một phần** — mã đạt (thoát lỗi, thiếu tenant vận hành → không khởi động); hạ tầng: 0 bản sao, không host ingress (đợt I) |

## 4. Public API (ADR-0022)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| P-01 | Tiến trình riêng `cmd/api-public` chỉ có route Public; không import `internal/mkg/**` (R-03) và kho định danh, kể cả client (RB-3) | `TestBoundary`; tiến trình `cmd/api-public` chỉ có route `/v1` Public + `/healthz` `/readyz` | **đạt** |
| P-02 | Khóa API: chỉ lưu `key_prefix` + HMAC-SHA-256 với pepper theo phiên bản (≥ 32 byte, từ trình quản lý bí mật); so khớp thời gian hằng; xoay pepper không làm hỏng khóa cũ | `TestPepperVersionsAndRotation_ADR0022`, `TestPeppersFromEnv_ADR0022`, `TestKeyHashNeverStoresPlaintext_ADR0022` | **đạt** |
| P-03 | Bí mật khóa hiện **một lần** (`Cache-Control: no-store`); không có trong log, lỗi, DB, URL, sự kiện (R-14, R-92) — kiểm thử quét chuỗi | `TestKeyHashNeverStoresPlaintext_ADR0022` (quét bảng + log), `TestAdminIssueKeySecretOnce_ADR0022` (no-store, phát lại không bí mật, quét `shared.idempotency_key`), `TestUnauthenticatedNoLedger_ADR0022` | **đạt** |
| P-04 | Vòng đời khóa là sổ (`issued`/`rotated`/`revoked`/`expired`, RB-1); tối đa 2 khóa hiệu lực; xoay có hạn chồng lấn; thu hồi có lý do, hiệu lực ngay | `TestKeyRotationOverlapRevoke_ADR0022`, `TestKeyUsable_ADR0022` | **đạt** |
| P-05 | Scope cấp qua đăng ký sản phẩm: `api_client_admin` đề xuất ≠ `api_client_approver` duyệt (R-56, `app/` + CHECK DB) | `TestSelfApprovalBlocked_R56` (app/ + CHECK DB), `TestAdminIssueKeySecretOnce_ADR0022` | **đạt** |
| P-06 | Truy cập cần cấp quyền `dp_<sản phẩm>` đang hiệu lực của bên cấp (ADR-0020, `GrantLookup`); thu hồi → 403 ở lời gọi kế tiếp; `grantor` chỉ chọn bên cấp | `TestScopeAndGrantDenied_ADR0022`, `TestExportJobFlow_ADR0022` (thu hồi → 403 ở lần tải kế tiếp) — với `GrantLookup` giả | **một phần** — mã: `GrantLookup` thật (đợt D) nối ở `cmd/api-public` `integrationPorts` |
| P-07 | Tenant của bên gọi lấy từ bản ghi khóa, không bao giờ từ path/query/body (tương đương R-16) | `TestReadCollectionVolumes_ADR0022` | **đạt** |
| P-08 | Gateway: giới hạn thô theo IP + giới hạn thân trên `partner.<domain>`, không thẩm quyền (R-55) | — | **chưa làm** (hạ tầng) |
| P-09 | Backend: giới hạn tần suất theo khách hàng (Valkey nếu có, không thì trong tiến trình) → 429 `apiclient.rate_limited` + `Retry-After` | `TestRateLimit_ADR0022` (token bucket trong tiến trình, chia theo `AG_PUBLIC_REPLICAS`) | **một phần** — hạ tầng: adapter Valkey chưa có (Q5-10), interface `RateLimiter` sẵn |
| P-10 | Hạn mức thẩm quyền ngày/tháng theo đơn vị từ bộ đếm `pf_metering` cùng giao dịch → 429 `metering.quota_exceeded` | `TestQuotaExhausted429_ADR0022` (kiểm sớm `Quotas` + thẩm quyền `Ledger.Record`, đơn vị đếm đúng) — với metering giả | **một phần** — mã: bộ đếm `pf_metering` thật (đợt M) |
| P-11 | Header `RateLimit-Limit`/`-Remaining`/`-Reset`, `AG-Quota-Remaining` ở mọi phản hồi | `TestReadCollectionVolumes_ADR0022`, `TestRateLimit_ADR0022` | **đạt** |
| P-12 | R-50: `/v1`, header `AG-Data-Contract: <sản phẩm>.v<N>`; hợp đồng mới chạy song song, `Deprecation`/`Sunset` | `TestReadCollectionVolumes_ADR0022`, `TestExportJobFlow_ADR0022` (`AG-Data-Contract`) | **một phần** — mã: `Deprecation`/`Sunset` khi có hợp đồng v2 (chưa có) |
| P-13 | R-51: POST thiếu `Idempotency-Key` → 400; gọi lặp → đúng phản hồi đầu, 0 đơn vị | `TestExportJobFlow_ADR0022` (đúng byte phản hồi đầu, `replayed`, 0 đơn vị; khác thân → 422), `TestExportRequiresIdempotencyKey_ADR0022` | **đạt** |
| P-14 | R-52: xuất → 202 + `Location: /v1/jobs/{id}` (mẫu mục 12, ảnh chụp bất biến); tệp phát luồng qua API, **không** URL có chữ ký | `TestExportJobFlow_ADR0022` (202 + `Location`, đầu vào bất biến, tệp phát luồng qua API + `Digest`) | **đạt** |
| P-15 | R-53: phân trang cursor ở mọi danh sách | con trỏ của nguồn chuyển tiếp (`next_cursor`); Admin `GET /v1/api-clients` cursor | **một phần** — mã: con trỏ do nguồn (đợt S/D) hiện thực |
| P-16 | Lỗi RFC 9457 + `trace_id` (R-15, R-63), mã ở `docs/errors.md` (`apiclient.*`, `metering.*`) | `TestLedgerFailureNoBody_ADR0023`, `TestScopeAndGrantDenied_ADR0022`; mã `apiclient.*` ở `docs/errors.md` | **đạt** |
| P-17 | Lúc chạy: `dataproduct.Get` + `Validate` từng phần tử — trường lạ → không trả, `apiclient.contract_violation` | `TestDefaultDenyExtraField_ADR0022` | **đạt** |
| P-18 | Hợp đồng `draft` không phục vụ ở production (`residency.Production`) | `TestDraftNotServedInProduction_ADR0022` | **đạt** |
| P-19 | Lời gọi chưa xác thực không ghi sổ (chỉ log `key_prefix` + `ag_public_unauthenticated_total`); OAuth client credentials tùy chọn (R-54) | `TestUnauthenticatedNoLedger_ADR0022` (không dòng sổ, log chỉ prefix, metric `ag.public.unauthenticated`) | **một phần** — mã: `TokenAuthenticator` (OAuth client credentials) chưa hiện thực, chờ khách Q5-7 |

## 5. Sản phẩm dữ liệu (g5-conventions §6)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| D-01 | `collection_volume`: ô xã × vụ × tháng chỉ khi ≥ k nông hộ phân biệt (`publicdata.k_min`, tạm 10, C-3); ô dưới k **không bao giờ** xuất hiện, kể cả trong tệp xuất; `suppressed_cells` | — | **chưa làm** |
| D-02 | `hub_capacity`: hub đã công bố, bậc sử dụng thay tấn tồn; không lô/giá | — | **chưa làm** (chờ khách Q5-6) |
| D-03 | `mrv_package_status`: đơn vị thẩm định chỉ xem trạng thái, băm, kiểm lại, số bằng chứng tổng hợp; không ảnh/tọa độ/nông hộ (KN US-MRV-05) | — | **chưa làm** |
| D-04 | Xuất `collection_volume` CSV/JSON tất định (SHA-256), cùng ngưỡng k | `TestExportJobFlow_ADR0022`, `TestExportTooLargeAndDeterministic_ADR0022` (CSV/JSON tất định, SHA-256, `max_export_rows`) | **một phần** — mã: ngưỡng k trong nguồn `CollectionVolumeSource` (đợt S) |
| D-05 | `contractcheck`: struct `//ag:dataproduct` khớp hợp đồng, tag OpenAPI, `personal_data: false`, người đề xuất ≠ người phê duyệt; chuyển `approved` khi khách ký | (`TestDataProductContracts_G5` có từ G5-0) | **chưa làm** (chờ khách Q5-1) |

## 6. `pf/metering` (ADR-0023, mục 16)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| M-01 | Sổ `usage_event` theo mẫu §8.1 (chuỗi băm, phân vùng tháng, RLS); một dòng mỗi lời gọi đã xác thực, kể cả 4xx sau xác thực | — | **chưa làm** |
| M-02 | Ghi sổ **trước** khi gửi byte; ghi lỗi → 503 `metering.unavailable`, không có dữ liệu | `TestLedgerFailureNoBody_ADR0023`, `TestQuotaExhausted429_ADR0022` | **đạt** |
| M-03 | Đơn vị `metering.units.v1` ghim `rule_version`; đổi quy tắc → v2, dòng cũ không đổi | — | **chưa làm** |
| M-04 | Bộ đếm `usage_counter_current` cùng giao dịch, khóa dòng, giờ VN; `RebuildTenant` dựng lại = hiện hành (R-22) | — | **chưa làm** |
| M-05 | `GET /v1/usage`: chỉ của chính khách hàng | `TestScopeAndGrantDenied_ADR0022` (`usage:read`, `client_id` từ khóa) | **một phần** — mã: `Usage.UsageOf` (đợt M) |
| M-06 | Bảng kê kỳ tháng: 202 + job, ảnh chụp bất biến (first/last seq, băm cuối chuỗi, SHA-256); tất định | — | **chưa làm** |
| M-07 | Duyệt bảng kê `billing_approver` ≠ `billing_officer` yêu cầu (R-56, `app/` + CHECK DB); đã duyệt → 409; sửa = bảng kê bù | — | **chưa làm** |
| M-08 | Giá tùy chọn, có phiên bản (`shared/money`, `numeric(18,4)`/`(18,2)`, không float — `agnofloat`); chưa có giá → chỉ đơn vị | — | **chưa làm** (chờ khách Q5-4) |
| M-09 | CSV bảng kê tất định byte-cho-byte, tải qua checkpoint kênh `report_export` | — | **chưa làm** |
| M-10 | `verify_chain`, neo hằng ngày (job `anchor`), lưu chứng từ chuyển tầng không xóa | — | **chưa làm** (chờ khách Q5-9) |

## 7. Điểm kiểm soát xuất — DLP (ADR-0024, mục 18)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| L-01 | Một điểm kiểm soát `Checkpoint.Pass`: sổ `egress_event` (tenant = chủ dữ liệu, chuỗi băm), idempotent theo (kênh, nguồn, bên nhận) | — | **chưa làm** |
| L-02 | Public API: dòng egress cùng giao dịch với sổ sử dụng, luôn đóng khi lỗi | — | **chưa làm** |
| L-03 | `pf/mrv`: yêu cầu xuất gói + `PackageURL` qua `Guard` kênh `mrv_package` | — | **chưa làm** |
| L-04 | `pf/reports`: ba lối (yêu cầu, lịch, tải) qua `Guard` kênh `report_export`; điểm kiểm soát cục bộ + `pf.reports.export.anomaly` giữ nguyên (kiểm thử G1 không đổi) | — | **chưa làm** |
| L-05 | Van: ba consumer gọi `Guard` kênh `valve` sau `ValveTargets`, mỗi bên cấp một lần; giao lặp không thêm dòng; `enforce` từ chối → DLQ (R-62) | — | **chưa làm** |
| L-06 | `/v1/partner/*` (ADR-0020): `RecordRead` qua `Guard` kênh `partner_share` (P2) | — | **chưa làm** |
| L-07 | Hạn mức kênh `metering.egress.<kênh>.daily_records_quota` (tham số có phiên bản) | — | **chưa làm** (chờ khách Q5-5) |
| L-08 | Bất thường: × trung vị 28 ngày / ngưỡng tuyệt đối; lần đầu trong ngày → đúng một `pf.metering.egress.anomaly_detected` → `pf.notify.egress_anomaly` | — | **chưa làm** |
| L-09 | `AG_EGRESS_MODE`: `observe` không bao giờ từ chối (kể cả checkpoint lỗi); `enforce` từ chối đúng mã | (`TestGuardModes_ADR0024` cho `Guard`) | **chưa làm** |
| L-10 | Metric `ag_egress_*` nhãn cardinality thấp (R-91); luật `agrigreen-egress.rules.yml` | — | **chưa làm** |
| L-11 | `GET /v1/metering/egress`, `/anomalies` (Admin API): chỉ tenant chủ dữ liệu thấy dòng của mình | — | **chưa làm** |

## 8. Phase K và cấp phát bên thứ ba (ADR-0025)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| K-01 | `values-phasek.yaml`: `api-public` 2 bản sao + PDB, host `partner.<domain>`, F08/F19; luồng cấm (`api-public` → vault/MQ/DB mkg-ts/Internet) bị chặn — `check_flows.py` | — | **chưa làm** (hạ tầng) |
| K-02 | Pepper khóa API qua ExternalSecret (KMS trong lãnh thổ), xoay theo phiên bản | — | **chưa làm** (hạ tầng) |
| K-03 | Runbook `public-api-onboarding.md`: tenant bên thứ ba → cấp quyền `dp_*` → khách hàng + đăng ký → khóa → thử chấp nhận → bảng kê; ngừng hợp tác | — | **chưa làm** |
| K-04 | Diễn tập bật Phase K trên staging chỉ bằng `helm upgrade -f values-phasek.yaml` | — | **chưa làm** (hạ tầng) |
| K-05 | Đơn vị thẩm định: tenant riêng, chỉ xem, mọi truy cập có nhật ký (sổ sử dụng + egress + `partner_read_log`) — KN US-MRV-05 | — | **chưa làm** (chờ khách Q5-7) |
| K-06 | Keycloak client `agrigreen-public` + client bí mật cho từng đơn vị thẩm định, khi khách chọn | — | **chưa làm** (chờ khách Q5-7) |

## 9. Cổng kiến trúc G5 (RB, R-xx, vùng)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| X-01 | RB-1: sáu phép thử UPDATE/DELETE/TRUNCATE trên mọi sổ mới; `v_missing_immutability` rỗng | `TestAppendOnlySixWays_G5Apiclient` (sổ `pf_apiclient`, `v_missing_immutability`), `verify_chain` trong `TestKeyRotationOverlapRevoke_ADR0022` | **một phần** — `pf_apiclient` đạt; `pf_metering` (đợt M) |
| X-02 | RB-2: cách ly tenant ở mọi bảng mới, kể cả song song qua PgBouncer transaction mode | `TestTenantIsolation_G5` (`pf_apiclient`) | **một phần** — `pf_metering` (đợt M); song song qua PgBouncer chưa có |
| X-03 | RB-3: Public API không trả dữ liệu cá nhân (hợp đồng `personal_data: false`, archlint không vault, kiểm thử quét phản hồi) | `TestBoundary`, `TestDefaultDenyExtraField_ADR0022`, hợp đồng `personal_data: false` | **một phần** — mã: kiểm thử quét phản hồi đầu–cuối (đợt E) |
| X-04 | R-10: `agnofloat` phủ `internal/platform/metering/**` | (`TestNoFloat` có từ G5-0) | **chưa làm** |
| X-05 | R-14/R-92: không log khóa, bí mật, URL có chữ ký (`agpiilog` với `IssuedKey.Secret pii:"true"`) | `TestKeyHashNeverStoresPlaintext_ADR0022`, `TestUnauthenticatedNoLedger_ADR0022`, `TestAdminIssueKeySecretOnce_ADR0022`; `IssuedKey.Secret pii:"true"` (agpiilog) | **đạt** |
| X-06 | R-61: ba consumer G5 idempotent (giao lặp) | `TestExportJobFlow_ADR0022` (giao lặp `pf.apiclient.export` → không làm gì) | **một phần** — `pf.apiclient.export` đạt; hai hàng đợi metering (đợt M) |
| X-07 | R-62: DLQ + cảnh báo cho ba hàng đợi G5 | — | **chưa làm** |
| X-08 | R-64: ba schema sự kiện G5 tương thích ngược (`contractcheck -base`) | — | **chưa làm** |
| X-09 | R-80/R-81: `api-public` ở vùng ứng dụng, chỉ PgBouncer pf; NetworkPolicy mặc định từ chối | — | **chưa làm** (hạ tầng) |
| X-10 | RB-7: Valkey, kho tệp xuất, nhật ký Public API trong lãnh thổ | — | **chưa làm** (hạ tầng) |

## 10. Client và kịch bản đầu–cuối

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| W-01 | `apps/web-ops`: khách hàng API, đăng ký sản phẩm, gói hạn mức, khóa (hiện bí mật một lần, xoay, thu hồi) — R-56 trên UI chỉ phản ánh `app/` | — | **chưa làm** |
| W-02 | `apps/web-ops`: bảng kê (yêu cầu, duyệt, tải CSV) | — | **chưa làm** |
| W-03 | `apps/web-ops`: nhật ký xuất và cảnh báo bất thường theo tenant | — | **chưa làm** |
| E-01 | Kịch bản `TestG5ThirdPartyToStatement` (mục 2) đạt trên máy dev | — | **chưa làm** |

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
