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
- [x] Chấp nhận các ADR đề xuất 0006, 0007, 0009, 0010, 0011 (30/09/2026, chủ sản phẩm quyết định trong phiên làm việc). Đối chiếu cam kết → mã → kiểm thử: [trien-khai-g1.md](adr/trien-khai-g1.md). ADR 0014–0019 vẫn ở trạng thái Đề xuất

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
- **Hạ tầng:** workflow `restore-drill` đã chạy xanh (29/09/2026); còn chạy thử trên Keycloak/SMTP/FCM thật trong lãnh thổ. Năm ADR G1 đã được chấp nhận ngày 30/09/2026 ([trien-khai-g1.md](adr/trien-khai-g1.md)); còn thêm luật Prometheus cho `ag_sync_conflict_overdue` và `ag_db_standby_replication_lag_seconds` vào `deploy/observability`.
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

## G3: Q3/2027

Phụ lục A yêu cầu ba phần:
- `pf/metrics` có lịch sử;
- `mkg/measurement` có đường nhận số liệu cân của khách;
- agent biên theo mẫu cân đã chốt.

Thêm vào đó là MRV và fieldops G3. Quy ước, số migration, vai, sự kiện, luật vùng: [docs/dev/g3-conventions.md](dev/g3-conventions.md). Thiết kế mkg: [docs/design/mkg.md](design/mkg.md) §5.

### G3-0: nền móng và hợp đồng đóng băng (30/09/2026, xong)

- [x] ADR-0017: cụm `ts` (`ag_ts`) — PostgreSQL 17 phân vùng tháng, không dùng TimescaleDB TSL. `ts/000001` tạo `ts_weigh.reading_batch` và `ts_weigh.raw_reading`, chỉ ghi thêm, RLS; `migrate maintain` tạo phân vùng tiếp theo; `migratecheck` bắt R-07 và cấm timescaledb.
- [x] ADR-0018: vhost `mkg.internal`. Có `shared.outbox.kind`, outbox-publisher định tuyến theo `kind`, `//ag:internal-event` được `contractcheck` kiểm, và khung `cmd/worker-internal` (có trong CI images và Compose, `rabbitmq-init` gỡ quyền tài khoản nền tảng).
- [x] ADR-0019: định danh thiết bị và đồng hồ. Hợp đồng dây `contracts/edge/batch.v1.json` kèm bộ vector, gói `edgewire` (Ed25519, dạng chuẩn); archlint giới hạn import của `cmd/edge-agent`.
- [x] Port đóng băng: `mkg/measurement/port` (`Integrator`, `TicketWriter`, `LotMass`, `DeviceRegistry`), `mkg/fieldops/port`, `pf/metrics/port` (`Source.AsOf`, `Definition`, `LayerLog`). Sự kiện nội bộ: `weigh_ticket.recorded`, `reconciliation.completed`, `roll_count.recorded`. Van dự thảo: `collection_schedule`, `htx_collection_statement`, `mrv_collection_evidence`.
- [x] Vai DB `ingest_ts`, `ts_reader`, `measurement_app`, `ingest_mkg`, `worker_internal`, `metrics_app` (dbadmin + PgBouncer CI/Compose); vai nghiệp vụ G3 trong realm.

### G3-1: lõi backend (song song, theo hợp đồng G3-0)

| Đợt | Nội dung | Migration |
|---|---|---|
| **A1** `mkg/measurement` | Sổ thiết bị: đăng ký, duyệt (R-56), thu hồi, kỳ đăng ký. Sổ phiếu cân (`TicketWriter`, phiếu bù) và bảng chiếu `lot_mass_current`. Bảng dung sai và định mức hao hụt sấy có phiên bản, **mặc định lấy từ prototype: cờ 5 %, nghiêm trọng 15 %**. CUSTOMER_IN nhập tay kèm bằng chứng. Đối soát tách hao hụt dự kiến và bất thường; hồ sơ tranh chấp. Van `mrv_collection_evidence` | mkg 000007–000008 |
| **A2** `cmd/ingest` | Listener mTLS, `edgewire.Verify` bằng khóa lấy từ `DeviceRegistry.Resolve`, ghi `ts_weigh` trong một giao dịch. Gửi lặp: cùng `payload_sha256` là idempotent, khác là báo động. `Integrator` `weigh.integrate.v1` (tạm, chờ C-6) → `TicketWriter` → sự kiện nội bộ. Kiểm thử bằng bộ vector | — |
| **A3** `cmd/edge-agent` | Đệm đĩa ≥ 72 giờ, đầy thì ngừng và báo động. Interface bộ điều hợp mẫu cân (bộ mô phỏng cho tới khi có C-6). Ký lô, đẩy qua mTLS chỉ đi ra, xóa khỏi đệm chỉ khi được xác nhận. Cập nhật có ký và lùi lại được (khung). Import chỉ theo ADR-0019 | — |
| **A4** `mkg/fieldops` G3 | Lệnh gom có duyệt. Công đoạn đếm cuộn → tập kết → xếp (proto FM-04). kg/cuộn học theo HTX. Xác nhận của HTX. Sự kiện `roll_count.recorded`. Van `collection_schedule` và `htx_collection_statement`. Nguồn thửa/HTX là interface; dùng dữ liệu tổng hợp cho tới khi chốt cấp quyền chia sẻ RB-2 | mkg 000006 |
| **A5** `mkg/hub` G3 | Khối lượng lô lấy từ phiếu cân (consumer worker-internal, Q-MKG-04). Kiểm kê (Q-MKG-06). Chuyển kho (Q-MKG-10). Chỗ nối `hub.risk.v2` với cảm biến (`ts_env` để dành) | mkg 000009 |
| **A6** `pf/metrics` | Định nghĩa chỉ số là tệp khai báo có phiên bản. Giá trị theo ngày theo `AsOf`/`Cutoff`; lịch sử KPI (KN US-DASH-01/02, CGH FN-09..11). Nhật ký lớp GIS, ảnh chụp và watermark, `ReplayLayer` (GIS US-EVL-01, US-SNAP-01, US-REPLAY-01, US-RETAIN-01) | pf 000036–000037 |
| **A7** `pf/mrv` G3 | Nhận van `mrv_collection_evidence`. Đối chiếu nhật ký HTX ↔ KN (KN US-MRV-02). Chỉ tính hồ sơ đã duyệt (KN US-MRV-05). Hệ số CO2 là tham số có phiên bản, gắn cờ "chờ ký phương pháp". Package có băm ảnh chụp | pf 000038 |
| **A8** Tích hợp và vận hành | Nối consumer `worker-internal`. Bộ sinh dữ liệu cân, cuộn, độ ẩm theo R-95 (dữ liệu golden 5800/5700 cuộn, 96,8 t). Chỉ số nghiệp vụ: độ đầy đệm agent, tỷ lệ phiếu vượt dung sai. Khung runbook tầng lạnh (R-71, chờ C-7). R-32 cho cụm ts | dự phòng pf 000039–000042, mkg 000010–000012 |

### G3-2: client và liên thông

| Đợt | Nội dung |
|---|---|
| **B1** | App hiện trường MKG (biến thể `apps/field-app` qua `packages/agrigreen_mobile`): đếm cuộn ngoại tuyến theo công đoạn, lệnh gom được giao |
| **B2** | `apps/htx-app`: lịch gom (van `collection_schedule`), xác nhận bảng kê số cuộn (van `htx_collection_statement` → API công khai) |
| **B3** | Web vận hành MKG (bề mặt `api-internal`): thiết bị cân, phiếu cân, đối soát, tranh chấp, dung sai/định mức |
| **B4** | `apps/web-ops`: bảng chỉ số có lịch sử, thanh trượt phát lại GIS theo ngày (GIS US-REPLAY-01) |
| **B5** | `apps/web-ops`: MRV — bằng chứng thu gom, đối chiếu HTX ↔ KN, xuất package |
| **B6** | Công cụ đăng ký thiết bị: sinh khóa và chứng thư dev (`scripts/gen-dev-certs`), CLI đăng ký, bộ mô phỏng đầu cân |
| **B7** | Compose và CI: dịch vụ `ingest` và bộ mô phỏng edge trong `compose-smoke` (mTLS, lô đi hết tới phiếu cân) |
| **B8** | UAT G3: bảng nghiệm thu yêu cầu → kiểm thử, kịch bản E2E cân → phiếu → đối soát → MRV |

### Trạng thái G3 (30/09/2026)

G3-0, G3-1 và G3-2 đã có mã, sớm hơn mốc Q3/2027. Đối chiếu yêu cầu → kiểm thử: [docs/uat/g3-acceptance.md](uat/g3-acceptance.md). **78 yêu cầu: 56 đạt, 20 một phần, 2 ngoài G3** (FM-05 vận tải thủy và phiếu nhận ↔ đơn mua, cả hai sang G4). Kịch bản đầu–cuối `TestG3FieldToCustomer` (lệnh gom → đếm cuộn → bảng kê HTX xác nhận trên nền tảng → cân HUB_IN qua ingest mTLS → nhận hub → HUB_OUT → CUSTOMER_IN tranh chấp → van MRV → phát lại GIS, chỉ số ngày) đạt trên máy dev. **CI trên GitHub chưa xác nhận** (GitHub Actions bị chặn do thanh toán): `compose-smoke` có ingest + edge-sim, PgBouncer cho bảng G3 (R-32), emulator App hiện trường MKG, image Docker, build tĩnh edge-agent, `promtool`.

**Đã có:** cụm `ts` + `cmd/ingest` (R-70, R-71), `cmd/edge-agent` (đệm ≥ 72 giờ, Ed25519 + bộ đếm, mTLS đi ra, cập nhật có ký), `mkg/measurement` (ba điểm cân, dung sai/định mức có phiên bản, đối soát, tranh chấp, nhập số cân khách, van MRV), `mkg/fieldops` G3 (lệnh gom có duyệt, FM-03/04/09, kg/cuộn học theo HTX, xác nhận HTX qua nền tảng), `mkg/hub` G3, `worker-internal` trên vhost `mkg.internal`, `pf/metrics` (lịch sử chỉ số, nhật ký lớp GIS, ảnh chụp, phát lại), `pf/mrv` G3, seed MKG (R-95); client: App hiện trường MKG, lịch gom/bảng kê trên App HTX và web-ops, web nội bộ MKG, màn chỉ số, phát lại GIS, MRV.

**Còn lại của G3** (chi tiết ở mục Tóm tắt của g3-acceptance):
- **Mã:** proto FM-01/02 (ràng buộc ngày gặt — cần đọc ngày thu hoạch qua API công khai), FM-06/07/08 (năng lực, trạng thái tổ gom, phân công tự động); KN US-MRV-04 (nhãn cảnh báo khẩn sâu bệnh), US-MRV-06 (leo thang hồ sơ MRV quá hạn), FR-08 (cờ AWD thiếu hạ tầng, thông báo KNV).
- **Hạ tầng:** chạy lại toàn bộ CI; vai giám sát `mkg_monitor`/`ts_monitor` cho cảnh báo SQL; CA thiết bị riêng cho production; job tầng lạnh `ts_weigh` (sau C-7).
- **Chờ khách:** C-4, C-5, C-6, C-7, định mức hao hụt sấy, phương pháp tCO2e + C-1, số ngày lưu FN-21. RB-2 (Q-MKG-12) đã chốt 30/09/2026 — [ADR-0020](adr/0020-lien-thong-platform-mkg-khac-tenant.md) — bảng dưới và [tong-hop.md](requirements/tong-hop.md#câu-hỏi-mở-g3-30092026).

### Câu hỏi khách còn mở cho G3

| # | Câu | Chặn |
|---|---|---|
| **C-4 (quá hạn — hạn G2)** | Bảng dung sai cân; trạng thái độ ẩm khi cân (hợp đồng prototype có trần ẩm 18 % nhưng không khấu trừ) | A1 đối soát; đang dùng mặc định từ prototype |
| C-5 | Đường nhận số liệu cân của khách (tích hợp hệ cân hay nhập tay) | A1 CUSTOMER_IN; G3 bắt đầu bằng nhập tay kèm bằng chứng |
| C-6 | Mẫu cân và giao thức — **hạn Q1/2027, có thời gian dẫn phần cứng** | A2 thuật toán tích phân, A3 bộ điều hợp; hợp đồng dây không đổi |
| C-7 | Cửa sổ kiểm toán số đo thô trước khi chuyển tầng lạnh | R-71, A8 runbook tầng lạnh |
| — | Định mức hao hụt sấy (hao hụt dự kiến HUB_IN → HUB_OUT) | A1 |
| — | Trả tiền theo cuộn hay theo tấn nhà máy cân (prototype có cả hai; theo tấn thì đối chiếu cuộn ↔ khối lượng là kiểm RB-6) | A1, A4, van `htx_collection_statement` |
| C-1 | Danh mục trường MRV **và** phạm vi, phương pháp tCO2e (baseline, leakage, chất khô; hệ số 1460 kgCO2e/t, 0,062 / 0,021 kgCO2e/t·km mới là tham số tạm) | A7, van `mrv_collection_evidence` |
| RB-2 | ~~Cấp quyền chia sẻ: tài khoản dịch vụ tenant MKG đọc thửa/HTX thuộc tenant chương trình như thế nào~~ — **đã chốt** (MKG tenant riêng, cấp quyền chia sẻ tường minh, ADR-0020) | A4 nguồn thửa/HTX |
| — | Số ngày lưu, nén, lưu trữ của nhật ký lớp GIS và ảnh chụp (GIS FN-21; HTML prototype ghi 365 ngày sự kiện, 90 ngày ảnh chụp) và của số đo thô | A6, A8 |
| Q2-05 | Ngưỡng sản lượng theo giống | A6 chỉ số, cảnh báo cropcycle |

## G4: Q2/2028

Phụ lục A: Kubernetes được quản trị; diễn tập tải ĐTK 2; tách đơn vị triển khai bằng cấu hình; dựng lại bảng chiếu; phục hồi. Thêm vào đó là bốn module còn lại của vùng mkg (`mkg/logistics`, `mkg/orders`, `mkg/costing` có đóng kỳ, `mkg/acctbridge`) và hai dòng "ngoài G3 → G4" (proto FM-05, phiếu nhận ↔ đơn mua > 3 %). Quy ước, số migration, vai, sự kiện, tham số tạm: [docs/dev/g4-conventions.md](dev/g4-conventions.md). Nghiệm thu: [docs/uat/g4-acceptance.md](uat/g4-acceptance.md).

### G4-0: nền móng và hợp đồng đóng băng (30/09/2026, xong)

- [x] Số migration: mkg 000014 (schema bốn module, đã có), 000015 logistics, 000016 orders, 000017–000018 costing, 000019 acctbridge, 000020–000021 dự phòng; ts 000004 `ts_fleet` (tùy chọn; `ts_env` chuyển sang 000005+); pf 000046 `waterway_routes` (đã có), 000047 dự phòng.
- [x] Vai DB `logistics_app`, `orders_app`, `costing_app`, `acctbridge_app` (dbadmin + PgBouncer CI/Compose); vai nghiệp vụ `dispatcher`, `driver`, `sales_officer`, `sales_approver`, `cost_accountant`, `period_closer`, `chief_accountant`, `accounting_bridge_operator` (hằng `port/` + realm).
- [x] Port đóng băng: `logistics/port` (`Trips`, `WaterwayRoutes`, `RoadRoutes`), `orders/port` (`Deliveries`, `Contracts`), `costing/port` (`ClosedPeriods`), `acctbridge/port` (`LedgerAdapter`). Tám sự kiện nội bộ: `trip.completed`, `delivery.accepted`, `period.closed`, `settlement.approved`, `journal.pushed` và ba sự kiện job.
- [x] Bề mặt đối tác `GET/POST /v1/partner/waterway/route` (loại cấp quyền `waterway_routes`, ADR-0020) — **đã hiện thực phía nền tảng** (`pf/waterway` + `pf/party`, sổ đọc).
- [x] `agnofloat` phủ `mkg/logistics` (costing, orders, acctbridge đã phủ theo tên).

### G4-1: module và client (song song, theo hợp đồng G4-0)

| Đợt | Nội dung | Migration |
|---|---|---|
| **L** `mkg/logistics` | Chuyến, chặng, mốc; FM-05 (xuống ghe → chuyến), GH-01..03 (danh mục ghe, đăng kiểm chỉ cảnh báo, cước thuê ghe); định tuyến qua `/v1/partner/waterway/route` lưu nguyên văn; đóng chuyến bằng phiếu cân; `trip.completed` | mkg 000015 (+ ts 000004) |
| **O** `mkg/orders` | Hợp đồng mua rơm CT-01..03, đơn mua + phiếu nhận (> 3 % cảnh báo), đơn bán, giao nhận ↔ `CUSTOMER_IN`, duyệt giá R-56; `delivery.accepted` | mkg 000016 |
| **C** `mkg/costing` | Sổ giá thành mức B, quy đổi cuộn → tấn, phân bổ đa chặng, quy kết hao hụt theo tỷ lệ nguồn, kỳ + đóng kỳ (202 + job), quyết toán HTX PM-01..04, chia carbon; `period.closed`, `settlement.approved` | mkg 000017–000018 |
| **A** `mkg/acctbridge` | Bút toán tổng hợp theo kỳ, ánh xạ tài khoản có phiên bản, duyệt R-56, bộ xuất tệp CSV/XML + bộ giả, thử lại + DLQ; `journal.pushed` | mkg 000019 |
| **W** web nội bộ + app | `apps/web-internal`: chuyến, đơn, kỳ, quyết toán, bút toán; app tài xế/chủ ghe (mốc chuyến) | — |
| **H** hạ tầng (song song) | Kubernetes trong lãnh thổ, namespace theo vùng + NetworkPolicy mặc định từ chối (ADR-0008, R-81), PKI nội bộ mTLS; tách đơn vị triển khai bằng cấu hình | — |
| **E** E2E + diễn tập | Kịch bản ruộng → ghe → hub → khách → giá thành → đóng kỳ → bút toán; diễn tập R-22 trên bản sao ẩn danh; diễn tập tải ĐTK 2; diễn tập phục hồi | dự phòng mkg 000020–000021 |

### Trạng thái G4 (01/10/2026)

G4-0 và G4-1 đã có mã trên nhánh `sub/g4` (sớm hơn mốc Q2/2028): bốn module `mkg/logistics`, `mkg/orders`, `mkg/costing` (đóng kỳ 202 + job), `mkg/acctbridge` (bộ xuất tệp CSV/XML), web nội bộ (W-01), app tài xế (W-02), chart Kubernetes theo vùng + NetworkPolicy + PKI + OpenTofu, `tools/loadgen`, `cmd/rebuilddrill`. Đối chiếu yêu cầu → kiểm thử: [docs/uat/g4-acceptance.md](uat/g4-acceptance.md). **85 yêu cầu: 59 đạt, 13 một phần, 13 chưa làm, 0 ngoài G4** (trước khi rà E2E: 51 / 5 / 29). Kịch bản đầu–cuối `TestG4TripToLedger` (hợp đồng mua rơm → xếp ghe có số hiệu ghe → chuyến thủy định tuyến qua bề mặt đối tác sau cấp quyền `waterway_routes` → mốc tài xế → HUB_IN qua ingest đóng chuyến + phiếu nhận ↔ đơn mua → giao nhận CUSTOMER_IN → quyết toán HTX → đóng kỳ → bút toán tổng hợp duyệt + đẩy → kỳ `posted`; nhánh sửa sai HUB_IN bù sau duyệt + điều chỉnh trễ vào kỳ mở kế tiếp; R-22, R-61, cách ly tenant, không dữ liệu G4 qua van) đạt trên máy dev. Rà E2E sửa ba lỗi: `pf/waterway` không đọc được danh mục lớp phương tiện của `pf/masterdata` (`capacity_t`), quyết toán theo tấn kẹt "chờ cân" khi phiếu cân tới costing trước khi chuyến đóng, dựng lại `lot_mass_current` lệch `as_of`.

Còn lại:
- **mã**: chứng từ ePOD / e-bill (L-11); thông báo hàng đến mang mã chuyến (T-1); costing đọc ước lượng kg/cuộn (C-04); hàm dựng lại bảng chiếu `mkg/fieldops`; phần G4 của `cmd/drill` (A-5); nguồn độ trễ bảng chiếu + chỉ số job đóng kỳ (K-12); bộ sinh dữ liệu tổng hợp cho chuyến / đơn / kỳ (K-14); telemetry `ts_fleet` (tùy chọn).
- **chờ khách**: nhà cung cấp phần mềm kế toán + chế độ kế toán + hệ thống tài khoản (Q-1, B-09); khóa phân bổ chi phí (Q-2); công thức giá quyết toán, hạn thanh toán (Q-3); tỷ lệ chia doanh thu carbon (Q-4); ngưỡng duyệt giá / chiết khấu (Q-5); quy đổi ẩm (Q-6, C-4); đơn giá cước, ngưỡng đường thủy, tải ghe (Q-7); khối lượng ĐTK 2 cho diễn tập tải.
- **hạ tầng**: cụm Kubernetes được quản trị trong lãnh thổ (chưa chọn nhà cung cấp), áp NetworkPolicy / PKI / mTLS / admission ký image trên cụm, CA thiết bị production + VPN biên, sao lưu pgBackRest + diễn tập phục hồi, diễn tập tải ĐTK 2 và R-22 trên bản sao dữ liệu thật ở staging, PgBouncer thật, `promtool`, CI (GitHub Actions bị chặn do thanh toán), kiểm thử máy ảo app.

## KN Phòng Trồng trọt v7.7

**Trạng thái (02/10/2026, nhánh `sub/kn-v77` + P8 đợt 4 `kn-p8e`):** gói P1–P7 đã có mã (P1 tài khoản nhiều vai trò + ngữ cảnh phiên X-AG-Context, P2 phòng ban/khu vực/cây 3 cấp/menu theo cấu hình, P3 engine 14 mẫu, P4 vòng đời báo cáo + Chi cục, P5 lịch công tác, P6 khung + mọi màn web, P7 app Cán bộ xã ngoại tuyến + HTX/máy/thửa/vật tư); kịch bản đầu–cuối `TestKN77AnGiangTuan36` (`tests/kn77`, 13 bước trên module thật) đạt. Nghiệm thu [docs/uat/kn-v7.7-acceptance.md](uat/kn-v7.7-acceptance.md): **130 yêu cầu — 42 đạt, 48 một phần, 27 chưa làm, 13 ngoài module** (trước đợt 4: 38/36/43/13). Còn lại: **chờ khách** Q-01…Q-23 (chặn lớn nhất Q-01 — tệp Excel gốc của 14 mẫu, 13 biểu Cấp Xã đang là mẫu tạm); **mã** — menu/ngữ cảnh trên mobile, vài màn Admin (di chuyển Nút, nạp Excel nhiều vai trò, đếm theo phòng ban), vai kho định danh mặc định cho Cán bộ xã, cảnh báo PII ô tự do, SPI "còn n lần thử"; **hạ tầng** — sẵn sàng, sao lưu/RPO/RTO trên cụm thật, tải 500 người dùng, Android 3G, FCM thật.

## G5 — Phase K, `pf/metering`, Public API cho bên thứ ba (nhánh `sub/g5`, hợp nhất vào `main` 03/10/2026)

Phụ lục A: "Phase K; `pf/metering` và Public API bật với bên thứ ba đầu tiên" (Q4/2028), cộng mục 18 (một điểm kiểm soát cho mọi đường xuất). Quy ước, số migration, vai, sự kiện, sản phẩm dữ liệu, tham số tạm: [docs/dev/g5-conventions.md](dev/g5-conventions.md). Quyết định: ADR-0022..0025. Nghiệm thu: [docs/uat/g5-acceptance.md](uat/g5-acceptance.md) — rà E2E 02/10/2026: **82 yêu cầu, 51 đạt, 19 một phần, 12 chưa làm** (10 trong số đó là câu hỏi khách Q5-1..Q5-10).

### G5-0: nền móng và hợp đồng đóng băng (01/10/2026, xong)

- [x] ADR-0022 Public API:
  - tiến trình riêng `cmd/api-public`, tắt ở ĐTK 1 bằng cấu hình;
  - khóa API lưu HMAC + pepper, scope, cấp quyền `dp_*`;
  - giới hạn ở gateway + hạn mức thẩm quyền ở backend;
  - R-50..R-53.
- [x] ADR-0023 `pf/metering`: sổ sử dụng là chứng từ, đơn vị `metering.units.v1`, bảng kê tháng, R-56, giá tùy chọn.
- [x] ADR-0024 DLP: `Checkpoint` duy nhất ở `pf/metering`, `observe`/`enforce`, van kiểm ở phía nhận.
- [x] ADR-0025 hồ sơ Phase K: lớp giá trị, cấp tenant bên thứ ba, đơn vị thẩm định.
- [x] Số migration:
  - pf 000080: schema `pf_apiclient`, `pf_metering`, loại cấp quyền `dp_*` (**đã có**);
  - pf 000081 apiclient;
  - pf 000082 metering;
  - pf 000083 kind thông báo `egress_anomaly`;
  - (trên `sub/g5` là 000047–000050; đánh số lại khi hợp nhất vào `main` 03/10/2026, sau KN 000060–000079);
  - mkg 000022 không dùng.
- [x] Vai DB `apiclient_app`, `metering_app`, `public_api` (dbadmin + PgBouncer CI/Compose). Vai nghiệp vụ `api_client_admin`, `api_client_approver`, `metering_viewer`, `billing_officer`, `billing_approver` (hằng `port/` + realm).
- [x] Port đóng băng:
  - `apiclient/port`: `Authenticator`, `TokenAuthenticator`, `Plans`, ba nguồn sản phẩm, DTO `//ag:dataproduct`;
  - `metering/port`: `Ledger`, `Quotas`, `Usage`, `Checkpoint`, `Guard`, `UnitsV1`;
  - `party/port`: `GrantLookup`, `DataProductKind`.
  
  Ba sự kiện: `pf.apiclient.export.requested`, `pf.metering.statement.requested`, `pf.metering.egress.anomaly_detected`.
- [x] Hợp đồng sản phẩm dữ liệu `contracts/dataproduct/*` (dự thảo, chờ khách): `collection_volume` (k nông hộ), `hub_capacity`, `mrv_package_status`. `contractcheck` kiểm struct `//ag:dataproduct` và tag của `contracts/openapi/public.yaml` (khung).
- [x] archlint: `cmd/api-public` không import `mkg/**` (R-03), không import vault kể cả client (RB-3); `agnofloat` cho `metering` có testdata.

### G5-1: module, hạ tầng, client (song song, theo hợp đồng G5-0)

Theo [g5-conventions §8](dev/g5-conventions.md#8-đợt-g5-1-song-song-theo-hợp-đồng-g5-0):

| Đợt | Phạm vi |
|---|---|
| **A** | `pf/apiclient` + `cmd/api-public` |
| **M** | `pf/metering` |
| **S** | nguồn sản phẩm ở `pf/cropcycle`, `pf/geo`, `pf/mrv`, `pf/party` |
| **D** | tích hợp DLP vào báo cáo / MRV / van |
| **H** | hạ tầng Phase K |
| **W** | `apps/web-ops` |
| **E** | E2E + diễn tập bật Phase K |

Trạng thái (02/10/2026): A, M, S + D, H, W đã hợp nhất vào `sub/g5` (tích hợp `g5-int`: nối cổng thật cho `api-public`, kho tệp xuất F29, cảnh báo DLP, job khóa hết hạn, runbook cấp bên thứ ba).

### G5 E2E (02/10/2026, nhánh `g5-e`)

- [x] `TestG5ThirdPartyToStatement` (`tests/e2e/g5_scenario_test.go`) đạt: 14 bước, 5 tenant (chương trình, MKG, đơn vị vận hành, nhà máy, đơn vị thẩm định), gốc lắp ráp thật của `api-platform` và `api-public`, consumer thật của worker qua bơm outbox, `AG_EGRESS_MODE=enforce`. Phủ:
  - runbook cấp bên thứ ba bước 1–8;
  - ba sản phẩm qua `api-public` thật, `publicdata.k_min` = 5 đặt qua pf/masterdata (người đặt ≠ người duyệt);
  - xuất 202 → tệp `Digest`; sổ sử dụng, egress, sổ đọc khớp số lời gọi;
  - 429 hạn mức, gói v2; DLP enforce (báo cáo, gói MRV) → bất thường → thông báo;
  - xoay / thu hồi khóa, thu hồi cấp quyền;
  - bảng kê cuối kỳ R-56 + CSV golden + kiểm băm + bảng kê bù;
  - giao lặp mọi sự kiện, cách ly tenant, quét RB-3, `verify_chain` chín sổ.
- [x] Đóng K-05: `api-public` ghi `partner_read_log` ở tenant bên cấp cho mỗi lần đọc / tải dữ liệu 2xx, qua bộ điều hợp không vault `party/port/grantlookup` (`port.APIReadLog`, ghi trước sổ đo lường, lỗi → 503 không dữ liệu). `TestAPIReadLogAdapter_K05`.
- [x] Gốc lắp ráp `api-public` tách sang `cmd/api-public/assembly` để E2E dùng đúng mã của tiến trình (archlint vẫn phủ cả cây).
- [ ] Còn lại:
  - **mã:** `Deprecation`/`Sunset` (P-12), OAuth client credentials (P-19), con trỏ (P-15);
  - **chờ khách:** Q5-1..Q5-10 (bên thứ ba đầu tiên, giá trị k, Q5-6 hub, giá, thời hạn lưu, Valkey, VVB);
  - **hạ tầng:** diễn tập `helm upgrade -f values-phasek.yaml` trên staging (K-04), NetworkPolicy / Ingress / ExternalSecret trên cụm thật, PgBouncer song song, luật cảnh báo DLQ vùng nền tảng (X-07), `promtool`, cổng giấy phép CI.

## Cổng v4.1 ↔ cổng repo

Thêm ngày 04/10/2026 (gói W0.3, X-19). **Có hai bộ tên cổng trùng nhau.**
- Các mục G0–G6 ở trên là cổng **của repo**. Chúng theo Phụ lục A v3.0 và đã có mã đến G5.
- Phụ lục A **v4.1** dùng lại đúng tên G0–G6, với cùng mốc ngày, nhưng giao **việc khác**: chuyển repo sang module bốn tầng, cổng tích hợp, `k/access`…
- Vì vậy "G2" trong một ADR v4.1 (ví dụ "van RabbitMQ giữ đến G2", ADR-0033 §6) **không** phải G2 của repo, vốn đã xong mã.

Cho đến khi MKG chọn tên mới (H-7, đề xuất "M0…M6" hoặc "G0′…G6′"), tài liệu repo viết **"G<n> v4.1"** cho cổng v4.1. Chữ "G<n>" không kèm hậu tố luôn là cổng repo.

| Cổng v4.1 | Mốc | Giao theo v4.1 (Phụ lục A) | Cổng repo cùng mốc và trạng thái | Gói chuyển đổi ([kế hoạch](kien-truc-v4.1-ke-hoach.md)) |
|---|---|---|---|---|
| **G0 v4.1** | 30/09/2026 ("cần cập nhật trạng thái") | Khung module, registry, kiểm đồ thị; `k/ledger` mẫu DDL đã sửa; `k/access` tối thiểu; `k_ref`; vai đăng nhập theo đơn vị triển khai; CI chạy DDL tài liệu | G0 repo: nền móng, xong. Phần v4.1 **đang làm**: W1.1, W1.2, W1.3, W1.4, W2.1–W2.6 xong | Giai đoạn 1–2 (W1.x, W2.x) |
| **G1 v4.1** | 15/12/2026 | App KN chạy trên module dùng chung; `dom/cooperative` làm chủ duy nhất (hợp nhất ba hệ UAT); lớp định danh theo SAD v1.1 | G1 repo (ĐTK 1): **mã xong** 29/09/2026. KN v7.7: P1–P7 có mã. Chủ duy nhất HTX **chưa** | Giai đoạn 3 (W3.x, trước 15/12/2026). Lớp định danh: KN P1 là hiện thực ĐTK 1 (ADR-0037); W7.2–W7.3 chờ SAD v1.1 |
| **G1+ v4.1** | Song song G1 | Cổng tích hợp tối thiểu: feed cursor, nạp theo lô, xác thực máy, hạn mức theo đối tác; ADR luồng song phương và cấp phép tổng hợp theo xã | Không có cổng repo tương ứng. Mầm là `cmd/api-public` (G5 repo) | Giai đoạn 5 (W5.x); ADR-0033…0035 |
| **G2 v4.1** | 31/03/2027 | App HTX, CGH, SIM lên module dùng chung; `share_grant` chạy thật; tổng quát `cap/forms`, `cap/task`; ký Danh mục Ràng buộc Nền tảng | G2 repo (`mkg/*`, `pf/waterway`, `pf/simulation`): **mã xong**. Hạn gỡ van RabbitMQ (ADR-0033 §6) | Giai đoạn 4, 6 (W4.x, W6.x) |
| **G3 v4.1** | Q3/2027 | Vùng nội bộ MKG `mkg/*`, cụm `mkg`, `mkg-ts`; App NH; chỉ số qua `r_agg` | G3 repo (measurement, edge-agent, cụm `ts`): **mã xong**. `mkg-ts` = cụm `ts` (ADR-0017, ghi lệch) | Giai đoạn 8 (W8.x, chờ MKG v1.3); W6.3–W6.4 (`r_agg`) |
| **G4 v4.1** | Q2/2028 | Kubernetes được quản trị; diễn tập tải ĐTK 2; tách đơn vị triển khai bằng cấu hình; dựng lại bảng chiếu; phục hồi đủ thứ tự | G4 repo: **mã xong** (chart, diễn tập trên máy dev); còn hạ tầng thật | W7.4 (cụm `idp` riêng; bản đối chiếu §2.4: tách ở G4) |
| **G5 v4.1** | Q4/2028 | Phase K; Public API và khóa API với bên thứ ba đầu tiên; bật module theo đơn vị | G5 repo: **mã xong**, hợp nhất 03/10/2026. Sau ADR-0033, Public API thành cổng tích hợp, bật từ ĐTK 1 | W5.2 (đổi tên cổng) |
| **G6 v4.1** | Q1/2029 | Cấu hình Phase 1 không sửa mã; build bản lite | G6 repo: như v4.1 | — |

Nhận xét:
- Hai bộ cổng **cùng mốc ngày**. Khác nhau ở chỗ cổng repo là "tính năng nào chạy" (đã làm trước lịch), còn cổng v4.1 là "kiến trúc đã chuyển tới đâu".
- Thứ tự gói chuyển đổi **không** theo số cổng: G1+ v4.1 (giai đoạn 5) chạy song song G1 v4.1, trước G2 v4.1 (giai đoạn 4).
- G0 v4.1 cần MKG cập nhật trạng thái (H-7). Đề xuất của repo: mốc mới là khi W1.x và W2.1–W2.5 xong.

## Chuyển đổi kiến trúc v4.1 ([kế hoạch](kien-truc-v4.1-ke-hoach.md))
- [x] W0.4 (04/10/2026): R-OPS-04 — bản v4.1 trong `docs/kien-truc/`, `go run ./tools/docsql` chạy 9 khối ```sql trên PostgreSQL trong CI (9/9 PASS) ([docs/dev/docsql.md](dev/docsql.md)).
- [x] W0.3 (04/10/2026): ghi lệch ADR-0006 (C-1 không chặn), ADR-0012 (`chain_head` chung, idempotency 30 ngày qua W2.1), ADR-0017 (không TimescaleDB); ADR-0037 (tách xác thực và phân quyền, không mạo danh — Đề xuất, chờ MKG ký); `docs/design/mkg.md` bốn cửa và bảng kê song phương; dẫn chiếu v3.0 → v4.1 trong `docs/dev/g*-conventions.md`; bảng cổng v4.1 ↔ cổng repo ở trên.
- [x] W0.2 (04/10/2026): ADR-0032…0036 — đơn vị hành chính có hiệu lực + kế thừa, cổng tích hợp `integration-gateway` (MKG như mọi đối tác), luồng song phương, cấp phép tổng hợp theo xã có k, engine ba chế độ + `lib/optcore` ([docs/adr](adr/README.md)); Đề xuất, chờ MKG ký (A7), ADR-0034 chờ thêm C-12.
- [x] W1.1 (04/10/2026): `module.yaml` cho 26 module, `internal/shared/modreg`, `go run ./tools/modgen -check` ([docs/dev/modules.md](dev/modules.md)).
- [x] W1.2 (04/10/2026): analyzer `agmodgraph` trong `tools/archlint` (chu trình, tầng, `depends`, phiên bản, `owns_schema` ↔ migration) + 118 ngoại lệ có chủ trong `tools/archlint/modgraph_exceptions.yaml` (chỉ co lại); `modgen -check` và `agmodgraph` vào job lint CI ([docs/dev/modules.md](dev/modules.md#đồ-thị-module-agmodgraph-w12)).
- [x] W1.3 (04/10/2026): `app/{KN,HTX,CGH,SIM,MKG,NH,PF}/app.yaml` (module có phiên bản, vai realm, trang đích, đơn vị triển khai); `modgen -check` kiểm ghép `cmd/*` ⊆ hợp `app.yaml` ([docs/dev/modules.md](dev/modules.md#ứng-dụng-appyaml-w13)).
- [x] W1.4 (04/10/2026): bật module theo đơn vị (R-MOD-08, pf 000088) — sổ + chiếu `pf_party.module_enablement_event`/`_current`, mặc định bật; route của module tắt trả 403 `module_not_enabled`, vai của module tắt không cấp được, menu Go và web-ops lọc ([docs/dev/modules.md](dev/modules.md#bật-module-theo-đơn-vị-w14)).
- [x] W2.1 (04/10/2026): DB-11 (shared 000006) — `shared.ledger_idem.expires_at` 30 ngày + chỉ mục; chống trùng `idem_key` của sổ chỉ bảo đảm trong 30 ngày, quá hạn ghi lại được nhận như mới; job `idempotency` của worker và `migrate purge-idempotency` dọn theo cụm ([ADR-0012](adr/0012-duy-nhat-tren-bang-so-phan-vung.md#hạn-30-ngày-của-sharedledger_idem-w21-db-11)).
- [x] W2.2 (05/10/2026): DB-06 (pf 000089) — `btree_gist` + `EXCLUDE` chống chồng hiệu lực trên `pf_masterdata.admin_unit` (giữ trigger), bảng chỉ ghi thêm `admin_unit_succession` (`effective_on`, `area_share`, `legal_basis`, `basis_status`) điền từ `successor_codes` dạng `pending_c9`, port `AdminUnitHistory` (`AdminUnitAt`, `SuccessorsAt`, `RollForward`); nạp danh mục chính thức chờ C-9 (W3.5) ([ADR-0032](adr/0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md#hiện-thực)).
- [x] W2.3 (05/10/2026): vai DB theo module — khung (DB-04, R-DB-02/03, pf 000090) — `r_<module>` NOLOGIN cho 17 module pf (quyền chép từ `<module>_app`), `app_platform`/`app_worker`/`app_public`/`app_sync` LOGIN NOINHERIT là thành viên `r_*` của module đơn vị ghép, `db.WithContextTx`/`db.ForModule` đặt `SET LOCAL ROLE`, lint `agnosessionset` + `migratecheck`; module mẫu `pf/content`, `pf/notify`; vai cũ chạy song song tới W2.6 ([ADR-0028](adr/0028-vai-dang-nhap-theo-don-vi-trien-khai.md#hiện-thực)).
- [x] W2.4 (05/10/2026): biến ngữ cảnh trong DB (DB-13, pf 000091) — `db.ForModule` đặt vai + `app.node_id`, `app.dept`, `app.xa_ids`, `app.purpose`, `app.actor_ref`, `app.on_behalf_of_ref` (từ Principal/KN, `authz.WithPurpose`) trong một câu `set_config(…, true)`; `k_ref.ctx_uuid`/`ctx_text` (rỗng/sai dạng → NULL, chỉ `app.*`, inline), EXECUTE cho `r_*`; chưa đổi chính sách ([hướng dẫn](dev/modules.md#biến-ngữ-cảnh-trong-db-w24)).
- [x] W6.5 (08/10/2026): `cap/evidence` — module mới `pf/evidence` sở hữu `pf_files` (chuyển từ `pf/sync`, route `/v1/files` giữ ở sync gọi port `Files`); pf 000123: bộ ba `(subject_module, subject_type, subject_id)` NULL được trên `pf_files.file`, sổ chỉ ghi thêm `pf_files.access_log`, vai `r_evidence`; `Attach` phân giải chủ thể qua `Resolver` của module chủ (nối `pf/cooperative`), `Open(purpose)` kiểm chủ thể + phạm vi + đồng ý và ghi sổ mọi quyết định ([hướng dẫn](dev/modules.md#tệp-minh-chứng-capevidence-w65)).
- [x] W6.1 (08/10/2026): `cap/workflow` — module `pf/workflow` (pf 000118): sổ `pf_workflow.request`/`decision`, bốn mắt ở DB (CHECK người duyệt ≠ người lập ≠ người được hưởng, trigger gắn quyết định vào yêu cầu, một yêu cầu một quyết định), ghi qua hàm `SECURITY DEFINER` trong giao dịch bên gọi; duyệt tham số (`pf/masterdata`) và cấp quyền chia sẻ (`pf/party`) ghi song song vào workflow, API không đổi ([hướng dẫn](dev/modules.md#duyệt-bốn-mắt-capworkflow-w61)).
- [x] W6.2 (08/10/2026, bật production chờ Q-01): `cap/forms` — module `pf/forms` (pf 000121–000122, vai `r_forms`): biểu có phiên bản, phát hành/nộp/rà soát bốn mắt (qua `cap/workflow`)/chốt kỳ kèm ảnh chụp đóng băng (băm SHA-256 do DB tính)/mở lại là sự kiện mới, máy trạng thái kỳ ở DB; cấu hình theo bên dùng (KN: TW/TINH/XA, Tỉnh phát hành và chốt); `/v1/forms/*`; route v1 KN ghi song song chốt/mở lại + ảnh chụp sang `pf_forms`; chỉ có biểu mẫu thử, 14 biểu thật chờ Q-01 ([hướng dẫn](dev/modules.md#biểu-theo-kỳ-capforms-w62)).
- [x] W2.5 (05/10/2026): `k_ref` trong cụm pf và mkg (DB-02, DB-05, R-DB-05, pf 000092, mkg 000022) — module lõi `pf/kref` sở hữu `k_ref` (`org`, `node_closure`, `admin_unit`, `share_grant_active`, `share_scope` rỗng), RLS theo tenant, mọi `r_*` SELECT, chỉ `r_kref` ghi; worker `pf.kref` từ `pf.party.org.created`/`moved` (mới), `deactivated`/`reactivated`, `pf.masterdata.admin_unit.changed`, `pf.party.share_grant.approved`/`revoked` (mới); `worker -kref-rebuild`; chỉ số `ag.k_ref.lag` + cảnh báo p99 > 5 s; `drill kref` trong diễn tập phục hồi ([ADR-0029](adr/0029-k-ref-ban-chieu-loi-moi-cum.md#hiện-thực), [hướng dẫn](dev/modules.md#bản-chiếu-k_ref-w25)).
- [x] W3.1 (06/10/2026): thiết kế ranh giới `pf/cooperative` ([docs/design/cooperative.md](design/cooperative.md)) — kiểm kê 22 bảng/9 sổ chuyển sang `pf_coop`, 6 port `cooperativeport` + ánh xạ bên tiêu thụ, `module.yaml` (`shares: [household, plot, season, diary_entry]`, vai `r_coop`), kế hoạch pf 000094–000096 (giữ tên sổ, chuyển phân vùng riêng), `p_scope` W3.4, giả định H-1 (a); câu hỏi H-17.
- [x] W5.1 (08/10/2026): hợp đồng cổng tích hợp `contracts/integration/` — feed `harvest_progress_by_commune`, `collection_volume_by_commune` (k tham số, tạm 10, chờ C-4/K-8), `season_harvest_dates`, `waterway_routes`, `admin_units`; nạp song phương `collection_schedule`, `roll_count_statement`, `htx_roll_confirmation` (chờ C-12); phong bì, mẫu, `contractcheck` mở rộng; không migration ([ADR-0033](adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md#hiện-thực)).
- [x] W5.2 (09/10/2026): cổng tích hợp `cmd/integration-gateway` từ `cmd/api-public` — giữ khóa API HMAC, hạn mức, metering, DLP; listener mTLS tùy chọn cho đối tác ghi lớn (chứng chỉ gắn khách hàng API qua `MachineIdentity`, kiểm cả khóa lẫn chứng chỉ); vai `app_public` giữ nguyên; `api-public` thành bí danh một kỳ (main mỏng); image CI, compose, chart + NetworkPolicy (F09); không migration ([ADR-0033](adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md#hiện-thực), [runbook](runbooks/integration-gateway.md)).
- [x] W5.3 (09/10/2026): feed theo cursor + tập dữ liệu dựng sẵn ở `pf/apiclient` (pf 000126–000127): kho feed chỉ ghi thêm giữ 30 ngày (job dọn, trigger chặn sửa/xóa sớm), phiên bản bất biến có `k_used` + tổng kiểm = băm ảnh chụp NDJSON ở kho đối tượng, quy tắc k ADR-0035 (ẩn ô bù, trễ ẩn k + 2) áp một lần trong job; `GET /v1/feeds/{feed}`, `GET /v1/snapshots/{dataset}/{version}` (URL ký lúc yêu cầu); hợp đồng `proposed` chỉ phục vụ ngoài production với cờ; bật production chờ A7/C-4/K-8 ([ADR-0035](adr/0035-cap-phep-tong-hop-theo-xa-nguong-k.md#hiện-thực), [runbook](runbooks/integration-gateway.md)).
- [x] W5.4 (09/10/2026): nạp theo lô `POST /v1/batches/{contract}` → 202 + mã lô, `GET /v1/batches/{id}` kết quả từng bản ghi; hàng đợi quorum + DLQ riêng từng đối tác, 429 `integration.queue_saturated` + `Retry-After`; inbox; hợp đồng mặc định từ chối (`proposed` chỉ dev/test); port module chủ do bên tiêu thụ khai — ba hợp đồng song phương vào sổ đệm `pf_apiclient.ingest_staging` tới W5.6; migration pf 000125 ([ADR-0033](adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md#hiện-thực), [runbook](runbooks/integration-batches.md)).
- [x] W8.1 (08/10/2026): `lib/optcore` — lõi tối ưu dùng chung (mạng lưới, mô hình cung, hàm chi phí, bộ giải HiGHS, độ nhạy) tách nguyên trạng khỏi `agrigreen_optimizer`, golden byte chụp trước khi tách (35 đầu ra lõi + 20 phong bì) trùng byte sau khi tách; image optimizer cài lõi qua ngữ cảnh đặt tên `optcore`; CI job `optcore` ([ADR-0036](adr/0036-engine-ba-che-do-vung-noi-bo-optcore.md#hiện-thực), [lib/optcore](../lib/optcore/README.md)).
- [x] Rà soát mã (08/10/2026, gộp sau W2.5): bằng chứng đổi tenant `tenant.Grant` (H4) + archlint `agtenantgate`; giới hạn thời gian/kích thước (statement_timeout, hạn xử lý inbox, nhập tệp); bản sao danh bạ fieldops (mkg 000033); cứng hóa identity-vault (hạn mức, MFA, `AG_ENV` bắt buộc), CI ghim SHA; cổng biến build web; vai `ag_ops_jobs` (shared 000009, pf 000114, mkg 000034), xả phân vùng DEFAULT (shared 000010), chỉ mục FK (pf 000115–000117), quy tắc khóa `migratecheck`; đảo phụ thuộc port + `consumes.events` — ngoại lệ `agmodgraph` từ 118 còn 22.
- [x] W4.1 (08/10/2026, bật production chờ C-10/H-1): cấp phép chia sẻ v2 (pf 000120) — `grant_class` (`intra_platform` | `partner`), `module_table` (chỉ bảng khai `shares`, kiểm theo danh mục `modreg`), `scope_node_ref`, `purpose_code`, `basis_ref` ở sổ phụ `share_grant_terms` + bảng chiếu; chỉ quản trị đơn vị chủ tạo; rà soát định kỳ R-SEC-04 (sổ `share_grant_review`, API đến hạn/ghi quyết định); thu hồi tăng epoch ngữ cảnh của đơn vị xem (`pf_party.context_epoch`, `pf.party.context_epoch.bumped`); cột `k_ref.share_grant_active` chuyển W4.2 ([ADR-0030](adr/0030-doc-tai-cho-co-cap-phep.md#hiện-thực)).
- [x] W3.2 (08/10/2026): module `pf/cooperative` — 6 port `cooperativeport` + bộ kiểm hợp đồng `porttest`, mặt tiền không DB trên port cũ của party/geo/cropcycle (bộ chuyển `cmd/internal/coopwire`); `mechanization`, `extension`, `cropcycle`, assembly (simulation, notify, party) đổi sang `cooperativeport`; không migration, không đổi ngoại lệ `agmodgraph` ([ghi chú](design/cooperative.md#16-ghi-chú-hiện-thực-w32), [hướng dẫn](dev/modules.md#module-pfcooperative--mặt-tiền-w32)).
- [x] fx2 (08/10/2026): sửa đứt chuỗi băm giả sau `ADD COLUMN` trên sổ (mkg 000006 `roll_count`, pf 000079 `plot_boundary`/`plot_event`) — `shared 000011`: sổ đăng ký cột thêm, `verify_chain` bỏ khóa null của đợt thêm khi băm lại dòng cũ, `register_ledger_columns`, quy tắc `migratecheck` ([ADR-0012](adr/0012-duy-nhat-tren-bang-so-phan-vung.md#hiện-thực--cột-thêm-vào-sổ-sau-khi-đã-có-dòng-fx2-shared-000011-08102026)).
- [x] W2.6 (08/10/2026): vai DB — chuyển hết (DB-04, R-DB-02, pf 000119, mkg 000035, ts 000004, vault 000003) — mọi module pf/mkg/vault chạy dưới `r_<module>` qua một pool vai đăng nhập mỗi đơn vị (`app_platform`, `app_worker`, `app_public`, `app_sync`, `app_internal`, `app_worker_internal`, `app_ingest`, `app_vault`); vai cũ mất quyền trực tiếp (giữ tên + membership một kỳ cho lùi bản); vai không theo module gộp `r_ops`/`r_measurement`; bí mật mỗi cụm 4–7 (gồm `ag_ops_jobs`); cổng CI "truy vấn ngoài giao dịch của module thất bại" ([ADR-0028](adr/0028-vai-dang-nhap-theo-don-vi-trien-khai.md#hiện-thực), [runbook](runbooks/db-roles-w26.md)).
- [x] W9.1 (08/10/2026): cổng kiến trúc — analyzer `agnoappname` (module capability/kernel không so sánh với mã ứng dụng trong `app/*/app.yaml`); cổng tích hợp quyền chéo schema mọi cụm (`r_*` chỉ được `k_ref`, `*_pub`; PUBLIC không có quyền trên schema module; ngoại lệ chỉ co lại `migrations/schema_isolation_exceptions.yaml`: `r_masterdata`, `r_party` → `pf_workflow`, gỡ ở W6.3); cổng `EXPLAIN` chung 360 bảng RLS (DB-14, không vi phạm); `agnofloat` phủ `mkg/fieldops`, `pf/mechanization` (mã sản phẩm). Không migration ([hướng dẫn](dev/modules.md#cổng-kiến-trúc-w91))
- [x] W6.3 (09/10/2026): view công bố `_pub` + `cap/analytics` (R-DB-10, pf 000124) — `pf_{cropcycle,extension,geo,mechanization}_pub` (11 view `v1_*`, `provides.publications` qua `//ag:pubview`), vai `r_<module>_pub` ∋ `r_metrics`; `pf/metrics` chỉ đọc `_pub` bằng vai của mình, đối chứng golden với nguồn cũ; bản sao chỉ đọc `AG_DB_REPLICA_URL` có kiểm độ trễ ([modules.md](dev/modules.md#view-công-bố-_pub-w63)).

## G6
Theo Phụ lục A của tài liệu kiến trúc:
- **G6 (Q1/2029):** cấu hình Phase 1, không sửa mã. Cơ chế giống Phase K: một lớp giá trị mới (ADR-0025).
