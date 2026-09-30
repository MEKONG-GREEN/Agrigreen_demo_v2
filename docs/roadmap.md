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
- **Chờ khách:** C-4, C-5, C-6, C-7, định mức hao hụt sấy, phương pháp tCO2e + C-1, RB-2 cấp quyền chia sẻ (Q-MKG-12), số ngày lưu FN-21 — bảng dưới và [tong-hop.md](requirements/tong-hop.md#câu-hỏi-mở-g3-30092026).

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
| RB-2 | Cấp quyền chia sẻ: tài khoản dịch vụ tenant MKG đọc thửa/HTX thuộc tenant chương trình như thế nào (cấp quyền tường minh hay vai trong tenant chương trình) | A4 nguồn thửa/HTX |
| — | Số ngày lưu, nén, lưu trữ của nhật ký lớp GIS và ảnh chụp (GIS FN-21; HTML prototype ghi 365 ngày sự kiện, 90 ngày ảnh chụp) và của số đo thô | A6, A8 |
| Q2-05 | Ngưỡng sản lượng theo giống | A6 chỉ số, cảnh báo cropcycle |

## G4–G6
Theo Phụ lục A của tài liệu kiến trúc:
- **G4 (Q2/2028):** Kubernetes trong lãnh thổ.
- **G5 (Q4/2028):** Public API và metering.
- **G6 (Q1/2029):** cấu hình Phase 1, không sửa mã.
