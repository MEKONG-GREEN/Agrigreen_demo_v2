# Lộ trình theo cổng

Lộ trình theo Phụ lục A của tài liệu kiến trúc v3.0. Kế hoạch lập ngày 27/09/2026. Sprint dài 2 tuần.

## G0: nền móng (mốc gốc 30/09/2026, đề xuất 10/10/2026)

- [x] Monorepo theo mục 6, cấu trúc module `port/domain/app/store/handler/module.go`
- [x] `shared/*`: tenant, ledger, outbox, inbox, idempotency, money, mass, clock, authz, problem, telemetry, ids
- [x] Mẫu bảng sổ gồm 3 lớp bảo vệ, chuỗi băm, verify, neo hằng ngày, phân vùng
- [x] `WithTenantTx` + kiểm thử rò rỉ tenant R-32 (chạy thẳng DB trên máy dev; qua PgBouncer trong CI)
- [x] Module mẫu `pf/party` (đồng ý theo mục đích) chạy end-to-end qua Application API
- [x] Công cụ cổng: archlint (R-02/03/04/10/12/14), migratecheck (R-23/25), contractcheck (R-64, van công bố)
- [x] CI: lint → test (PostgreSQL + PgBouncer) → govulncheck, go-licenses, gitleaks, trivy → build, SBOM, quét image, ký cosign
- [x] Docker Compose ĐTK 1, realm Keycloak, cấu hình PgBouncer
- [x] ADR 0001–0013
- [x] CI xanh toàn bộ trên GitHub, kể cả R-32 qua PgBouncer và build/ký 6 image (27/09/2026)
- [x] Chạy thử Compose trên máy có Docker (job CI `compose-smoke`: dựng toàn bộ stack, kiểm healthz/readyz)
- [ ] MKG ký các ADR đề xuất: 0006, 0007, 0009, 0010, 0011

## G1: ĐTK 1, ngày 15/12/2026

Người dùng là cán bộ khuyến nông, TTKNQG và Cục Kinh tế hợp tác; một tenant.

| Sprint | Thời gian | Nội dung |
|---|---|---|
| S1 | 12–25/10 | `pf/masterdata`: đơn vị hành chính có hiệu lực theo sáp nhập 2025, giống lúa, tham số có phiên bản với `param_setter`/`param_approver`. `vault/identity`: API mục 10.3, khóa X25519 theo hộ, blind index. Bề mặt Admin với MFA |
| S2 | 26/10–8/11 | `pf/geo`: thửa PostGIS, chồng lấn, diện tích geodesic. Cây phân cấp KN. HTX và nông hộ (định danh cá nhân ở vault). `apps/web-ops` Next.js. Chốt engine đồng bộ (23.1) |
| S3 | 9–22/11 | `apps/field-app` Flutter: đăng nhập offline 72 giờ, SQLCipher, hàng đợi UUIDv7 + idem_key, vẽ ranh giới offline, mã hóa ảnh ngay khi chụp |
| S4 | 23/11–6/12 | `platform/extension` (nhiệm vụ SLA 24 giờ), thư viện quy trình, dashboard 4 KPI. `platform/mechanization` (CGH: cân đối QT-01/02). Danh mục trường MRV (C-1). OTLP (ADR-0013) |
| S5 | 7–15/12 | Cứng hóa, bộ sinh dữ liệu tổng hợp (R-95), UAT theo ca kiểm thử trích từ prototype `mekong-green`, diễn tập sao lưu và phục hồi |

### Trạng thái G1 (29/09/2026 — chốt mã)

Mã đã có đủ các phần của kế hoạch S1–S5, sớm hơn mốc 15/12/2026. Đối chiếu yêu cầu → kiểm thử: [docs/uat/g1-acceptance.md](uat/g1-acceptance.md) — **94 yêu cầu: 80 đạt, 5 một phần, 9 ngoài G1** (G2/G3). CI trên GitHub xanh toàn bộ (14 job).

**Đã có:**
- Backend 11 module pf: masterdata, party, geo, content, extension, mechanization (CGH), mrv, sync, notify (hộp thư + đẩy FCM chỉ dữ liệu), accounts (Keycloak Admin REST), reports (xuất Excel/PDF bất đồng bộ, lịch gửi email). Kho định danh `identity-vault` tách database.
- Web `apps/web-ops` (Next.js): điều hành, bản đồ khuyến nông, nhiệm vụ, chỉ đạo khẩn, danh bạ trực, báo cáo, HTX & nông hộ, thư viện, cổng CGH cho Cục (bản đồ tỉnh + ghim HTX gom cụm, panel máy, tra cứu), 8 màn quản trị. Vitest 119 ca, Playwright 27 ca.
- App `apps/field-app` (Flutter): ngoại tuyến 72 giờ + PIN, SQLCipher, hàng đợi đúng một lần, vẽ ranh giới ngoại tuyến, ảnh mã hóa ngay khi chụp, hộp thư/đẩy, thư viện "Đã lưu", chỉ đạo, đồng ý + watermark, bản đồ ngoại tuyến, danh bạ trực gọi/nhắn một chạm. 143 ca + kiểm thử tích hợp trên emulator.
- Danh mục hành chính **chính thức** sau sáp nhập 2025 (6 tỉnh/thành ĐBSCL và Tây Ninh, 591 xã mới); nhập/xuất Excel (.xlsx) cho nông hộ, tài khoản, cây tổ chức, máy; `cmd/seed-synthetic`; `cmd/drill` + `objsync` (diễn tập phục hồi).
- Kịch bản E2E 14 bước, 39 ca UAT từ prototype; Compose Local/Dev (SeaweedFS thay MinIO) có job `compose-smoke`.

**Còn lại của G1** (không chặn mã; chi tiết ở mục Tóm tắt của g1-acceptance):
- **Chờ khách:** danh mục trường MRV chính thức (C-1); mẫu biểu báo cáo chính thức của Bộ (US-DASH-02); DCRD xác nhận phương pháp dự báo CGH (FN-09); chuyển VN-2000 khi nhập (X9).
- **Mã:** chọn nhóm dữ liệu khi đồng bộ ngay (US-SYNC-02, cùng liên thông App HTX ở G2). Bản đồ CGH cấp HTX (FN-08: ghim, gom cụm, panel máy, tra cứu) đã xong 29/09.
- **Hạ tầng:** workflow `restore-drill` đã chạy xanh (29/09/2026); còn chạy thử trên Keycloak/SMTP/FCM thật trong lãnh thổ; MKG ký các ADR đề xuất.
- Chuyển sang G2 theo lộ trình: App HTX (US-HTX-05, US-SYNC-01, US-TASK-01 tự động, CGH FN-07), Hub/kho và lớp GIS nền (GIS FN-04/06/07). Sang G3: đối soát đo lường RB-6, số đo của R-95.
## G2: 31/03/2027
- App HTX:
  - `platform/cropcycle`: nhật ký canh tác dạng sổ, sản lượng decimal.
  - Liên thông KN ↔ HTX bằng sự kiện.
- `pf/waterway` (pgRouting, có phiên bản), `pf/simulation`, `optimizer/`.
- Thiết kế chi tiết `mkg/*` và các hợp đồng van công bố đầu tiên.

Quy ước và quyết định đã chốt: [docs/dev/g2-conventions.md](dev/g2-conventions.md). Phạm vi từ backlog: App HTX 48 story (13 đã có ở G1, 17 một phần, 18 mới), GIS 38 story.

| Đợt | Nội dung |
|---|---|
| Hạ tầng (29/09) | pgRouting ở cụm pf (image `deploy/compose/postgres`), vai `cropcycle_app`, `waterway_app`, `simulation_app`, `hub_app`, cấp số migration |
| Đợt 1 — backend và lõi | `platform/cropcycle` (SOP, vụ, nhật ký, duyệt, thu hoạch, dashboard) · `pf/geo` G2 (ranh giới HTX, kho/cơ sở, đường bộ, log GPS) · `pf/waterway` · `pf/simulation` + `optimizer/` (Python) · thiết kế mkg + `mkg/hub` + van `hub_facility`/`hub_capacity` + siết contractcheck · tài khoản nông dân (SĐT, QR kích hoạt) + yêu cầu hỗ trợ → nhiệm vụ tự động + đồng bộ theo nhóm dữ liệu · tách `packages/agrigreen_mobile` từ field-app |
| Đợt 2 — client và liên thông | `apps/htx-app` (Flutter, ngoại tuyến) · web-ops: màn HTX (vụ, duyệt, sản lượng, dashboard, SOP), lớp GIS, waterway, mô phỏng · nối sự kiện (CGH FN-07, cảnh báo tới nông dân, van → mô hình đọc hub) · UAT G2 |

**Chờ khách** (không chặn đợt 1): nhà cung cấp thời tiết/độ mặn và tile vệ tinh trong lãnh thổ (RB-7), nguồn dữ liệu thủy hệ (VIWA) và bản đồ địa chính (VN-2000), ngưỡng k của bộ lọc dữ liệu công khai (C-3), chọn hub là tối ưu (MILP) hay đánh giá kịch bản, bộ 57 tham số mô phỏng.

### Trạng thái G2 (29/09/2026)

Đợt 1 và đợt 2 đã có mã, sớm hơn mốc 31/03/2027. Đối chiếu yêu cầu → kiểm thử: [docs/uat/g2-acceptance.md](uat/g2-acceptance.md). **137 yêu cầu: 96 đạt, 31 một phần, 10 ngoài G2** (G3 hoặc chờ nhà cung cấp). **CI trên GitHub chưa xác nhận** cho các commit G2, vì GitHub Actions đang bị chặn do lỗi thanh toán. Trạng thái dựa trên lần chạy kiểm thử ở máy dev.

**Đã có:**
- Backend: `platform/cropcycle` (SOP có phiên bản, vụ, nhật ký dạng sổ, duyệt, sản lượng decimal, bảng điều hành, bản đồ giai đoạn). `pf/geo` G2: ranh giới HTX, kho/Hub + sức chứa, đường bộ, log GPS, mô hình đọc hub qua van. `pf/waterway` (pgRouting, phiên bản, dựng bất đồng bộ, phát hành tách vai). `pf/simulation` + `optimizer/` Python (5 ca golden tái lập prototype, ảnh chụp bất biến không định danh). Tài khoản nông dân (SĐT, QR/liên kết một lần), yêu cầu hỗ trợ → nhiệm vụ tự động, sự kiện nông hộ, đồng bộ theo nhóm dữ liệu, CGH FN-07 (diện tích), mẫu báo cáo sản lượng HTX.
- mkg: thiết kế chi tiết [docs/design/mkg.md](design/mkg.md); `mkg/hub`; van `hub_facility`/`hub_capacity` v1; `contractcheck` và `archlint` siết cho vùng mkg; `cmd/api-internal`.
- Client: `packages/agrigreen_mobile` (lõi chung), `apps/htx-app` (nông dân và quản lý HTX, ngoại tuyến). web-ops: màn App HTX, bản đồ GIS nhiều lớp, biên tập bản đồ, kho/Hub, thủy hệ, mô phỏng Hub.
- Hạ tầng: listener Internal mTLS (8084), vòng nhận optimizer (hàng đợi quorum + DLQ), khai báo trước hàng đợi RabbitMQ, job CI cho optimizer, lõi di động và App HTX.

**Còn lại của G2** (chi tiết ở mục Tóm tắt của g2-acceptance):
- **Mã:** App HTX chưa có bản đồ và vẽ thửa cho Quản lý HTX (HTX US-PLOT-01/02/05). Chưa có nhập và sao chép HTX, sao chép nông hộ, tìm nông hộ, gọi nhanh nông hộ (HTX US-HTX-02/03, US-HH-01/02/03). Chưa có màn lập lịch báo cáo cho Quản lý HTX (US-DASH-03), chuyên mục tin trên App HTX (US-NEWS-01), xem trước bài dạng mobile (US-CMS-01), ô chọn nhóm dữ liệu ở web `/dong-bo` (KN US-SYNC-02). GIS còn thiếu: cấu hình dải màu/HEX và khôi phục mặc định (US-CFG-01/02), lớp CGH trên GIS (US-INT-01), API cho TMS (US-INT-02), màn giám sát đồng bộ và cảnh báo DLQ (US-SYNC-01/02), job công bố sức chứa định kỳ (FN-06, Q-MKG-02). CGH FN-07 còn thiếu nhật ký đồng bộ và cảnh báo Admin.
- **Hạ tầng:** phần mở rộng Keycloak phát liên kết hành động (`AG_KC_ACTION_LINK_URL`) cho kích hoạt QR của nông dân; chạy lại toàn bộ CI khi GitHub Actions hoạt động lại; đo hiệu năng lớp GIS (≤ 1,5 s) và TMS (≤ 500 ms).
- **Chờ khách:** các câu hỏi G2 ở [tong-hop.md](requirements/tong-hop.md#câu-hỏi-mở-g2-29092026): nhà cung cấp thời tiết/độ mặn/tile (RB-7), nguồn thủy hệ, VN-2000, bộ 57 tham số và MILP, ngưỡng k (C-3), quy tắc duyệt, ngưỡng sản lượng, cổng thu hoạch, chính sách lưu (GPS, FN-21), khóa đăng nhập GIS. Ký ADR-0016 (giấy phép Python).
- **Chuyển sang G3 theo lộ trình:** lớp tự nhiên và thời tiết GIS (khi có nhà cung cấp), snapshot/replay/lưu trữ GIS (FN-19..21, cùng lịch sử `pf/metrics`), RB-6 và C-4 (`mkg/measurement`), số đo của R-95.

## G3–G6
Theo Phụ lục A của tài liệu kiến trúc:
- **G3 (Q3/2027):** metrics, MRV, measurement, edge-agent. Mẫu cân C-6 phải chốt trước Q1/2027.
- **G4 (Q2/2028):** Kubernetes trong lãnh thổ.
- **G5 (Q4/2028):** Public API và metering.
- **G6 (Q1/2029):** cấu hình Phase 1, không sửa mã.
