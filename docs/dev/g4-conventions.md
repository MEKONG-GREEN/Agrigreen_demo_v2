# Quy ước phát triển G4

Đọc cùng [g1-conventions.md](g1-conventions.md), [g2-conventions.md](g2-conventions.md) và [g3-conventions.md](g3-conventions.md). Mọi quy ước trước vẫn áp dụng: cấu trúc module, RLS, sổ chỉ ghi thêm, PII chỉ ở vault, kiểm thử bắt buộc, tiền tố mã story, lệnh kiểm tra trước khi báo xong. Tài liệu này chỉ ghi phần **thêm** cho G4.

Cổng G4 (tài liệu kiến trúc v3.0 Phụ lục A, Q2/2028; G4 v4.1 cùng mốc nhưng khác nội dung — [roadmap](../roadmap.md#cổng-v41--cổng-repo); bảng đổi mục v3.0 → v4.1 ở [g1-conventions](g1-conventions.md)) gồm:
- bốn module còn lại của vùng mkg: `mkg/logistics`, `mkg/orders`, `mkg/costing` (đóng kỳ theo mẫu §11 v4.1, mục 12 v3.0), `mkg/acctbridge` ([mkg.md §5.4–5.7](../design/mkg.md#54-mkglogistics--chuyến-chặng-đường-thủy-và-đường-bộ));
- diễn tập dựng lại bảng chiếu từ sổ trên **bản sao dữ liệu thật** (R-22) và diễn tập phục hồi;
- tách đơn vị triển khai **bằng cấu hình** (RB-5);
- diễn tập tải ĐTK 2 (gồm hiệu năng RLS ở khối lượng ĐTK 2, §32 v4.1, mục 20 v3.0);
- Kubernetes được quản trị trong lãnh thổ, phân vùng mạng theo [ADR-0008](../adr/0008-phan-vung-mang.md) (namespace theo vùng, NetworkPolicy mặc định từ chối — R-81), PKI nội bộ cho mTLS trong cụm.

Phần hạ tầng (Kubernetes, NetworkPolicy, PKI nội bộ) do **agent hạ tầng làm song song**; tài liệu này chỉ tham chiếu. Hai dòng "ngoài G3 → G4" của [g3-acceptance.md](../uat/g3-acceptance.md) (proto FM-05, phiếu nhận ↔ đơn mua > 3 %) thuộc logistics và orders.

G4-0 (30/09/2026) dựng nền và **đóng băng hợp đồng** để agent module (logistics, orders, costing, acctbridge), web/app và E2E làm song song. Bảng nghiệm thu: [g4-acceptance.md](../uat/g4-acceptance.md).

## 1. Quyết định đã chốt (30/09/2026)

| Chủ đề | Quyết định | Căn cứ |
|---|---|---|
| Phạm vi `mkg/orders` | Gồm **cả chiều mua**: hợp đồng mua rơm với HTX (theo tấn nhà máy cân hoặc theo cuộn, proto CT-01..03), đơn mua + phiếu nhận hàng (kiểm ↔ đơn mua > 3 % chỉ cảnh báo, cần biên bản chênh lệch), và chiều bán: đơn bán, giao nhận có biên bản liên kết phiếu `CUSTOMER_IN` | prototype `procurement`, `sales`, `warehouse` (createGoodsReceipt), `straw/contracts` |
| Quyết toán HTX | Thuộc `mkg/costing` (sổ `settlement_event`), theo prototype "phiếu mua rơm" PM-01..04: một lệnh gom một quyết toán; theo cuộn → số cuộn × giá ngay; theo tấn → chờ cân đủ mọi ghe; không hợp đồng → người xác nhận nhập giá (không đoán); duyệt ghi **đúng một** khoản phải trả, hạn 15 ngày, đóng băng. Người lập `cost_accountant` ≠ người duyệt `chief_accountant` (R-56) | prototype `straw/tickets.ts` |
| Một nguồn khoản phải trả | Prototype ghi phải trả hai lần (phiếu nhận + đề nghị thanh toán). G4: phải trả mua rơm chỉ từ `settlement.approved`; phải trả đơn mua khác chỉ từ phiếu nhận đã duyệt | lỗi prototype (three-way match) |
| Chuyến | Trạng thái **chỉ tiến**, Go cưỡng chế (prototype không kiểm): `planned → departed → arrived → completed`, `planned/departed → cancelled` có lý do. Mỗi lượt xuống ghe (đếm cuộn công đoạn `boat_load`) sinh một chuyến đường thủy `planned` với tấn ước theo cuộn; lỗi lập chuyến **không** chặn ghi đếm cuộn (FM-05). Phiếu cân đóng chuyến với tấn thật; cước = đơn giá thuê ghe (GH-03) nếu có, không thì đơn giá tham số | proto FM-05, GH-01..03 |
| Định tuyến đường thủy | Loại cấp quyền chia sẻ mới **`waterway_routes`** (pf 000046) + bề mặt đối tác `GET/POST /v1/partner/waterway/route` (vai `mkg_sync`), **đã hiện thực phía nền tảng ở G4-0** (`pf/waterway` + `pf/party`). MKG lưu **nguyên văn** phản hồi + SHA-256 vào `route_snapshot` của chặng (§27 v4.1, mục 16 v3.0: kết quả định tuyến đã dùng lưu bền vững) | ADR-0020, mkg.md §5.4 |
| Định tuyến đường bộ | Chưa có OSRM trong lãnh thổ: `RoadRoutes` = Haversine × hệ số #10 đường bộ (1,3), nhãn `haversine`. Điều phối nhập km tay có lý do (`manual`). OSRM / loại `road_routes` để sau (pf 000047 dự phòng) | RB-7 |
| Đóng kỳ | Kỳ **tháng** (`YYYY-MM`). `POST …/periods/{id}/close` → 202 + job (mẫu §11 v4.1, mục 12 v3.0), ảnh chụp đầu vào bất biến có băm. Kỳ đã đóng **không mở lại**; sai sót = bút toán điều chỉnh ở kỳ đang mở, `supersedes_id` trỏ kết quả cũ. Người đóng `period_closer` ≠ người lập bút toán điều chỉnh của kỳ (R-56, app/ + CHECK DB). Đẩy bút toán thành công → kỳ `posted` | §11, §27 v4.1 (mục 12, 16 v3.0); mkg.md §5.6 |
| Event sourcing | `costing` mức B (sổ là nguồn, `POST /v1/costing/projections/rebuild`); mọi kết quả ghim `rule_version`, tính lại = kết quả mới `supersedes_id`. `logistics`, `orders`, `acctbridge`: sổ + bảng chiếu cùng giao dịch (R-21), có hàm dựng lại bảng chiếu để diễn tập R-22 | ADR-0003 |
| Phần mềm kế toán | **Chưa biết nhà cung cấp.** `acctbridge/port.LedgerAdapter`; G4 hiện thực bộ **xuất tệp** (CSV + XML, tất định byte-cho-byte, ký SHA-256, lưu kho đối tượng trong lãnh thổ) và bộ **giả**. Bảng ánh xạ (nhóm chi phí × chiều) → tài khoản có phiên bản (`accounting_bridge_operator` đề xuất ≠ `chief_accountant` duyệt). Chế độ kế toán và hệ thống tài khoản chờ khách | §27 v4.1 (mục 16 v3.0); RB-4, RB-7 |
| Doanh thu carbon | Tham số có phiên bản: MKG giữ 45 %; 55 % còn lại chia 70 % nông hộ/HTX, 15 % cơ quan, 15 % VVB (VVB nhận phần dư làm tròn). costing chỉ **chia** khi có doanh thu carbon được ghi; không tính tCO2e (pf/mrv, chờ C-1) | prototype `carbonRevenueShare` |
| Ẩm độ | Không có công thức quy đổi/khấu trừ ẩm ở đâu (C-4). Tham số `costing.moisture.base_pct` mặc định **không quy đổi**; trần ẩm hợp đồng chỉ lưu | C-4 |
| Telemetry phương tiện | Tùy chọn trong G4: schema `ts_fleet` ở cụm ts (ts 000004), quyền cho `ingest_ts` (ghi) và `ts_reader` (đọc) — **không** thêm vai ts mới | ADR-0017 |
| Tách đơn vị triển khai | Không đổi mã: chọn job/consumer bằng biến môi trường. `api-internal` đã có `AG_MKG_JOBS`; `worker-internal` thêm `AG_WORKER_INTERNAL_CONSUMERS` (danh sách tên hàng đợi hoặc `all`, mặc định `all`) — việc của agent tích hợp/hạ tầng | RB-5 |

### 1.1 Đặc tả rút từ prototype `mekong-green`

Không có BRD hay backlog nào của khách nói về vận tải nội bộ, đơn hàng, giá thành, quyết toán hay kế toán của MKG (GIS BRD FN-08 ghi rõ chọn xe/tài xế, lập lịch, tối ưu điều phối thuộc TMS; BRD CGH loại trừ giao dịch/thanh toán). Vì vậy mọi quy tắc G4 hoặc là **quyết định của khách**, hoặc chép quy tắc (không chép mã) từ prototype. Prototype **không có kiểm thử** cho đơn bán, đơn mua, three-way match, dung sai 3 %, chia carbon, định tuyến TMS, chênh cước — agent G4 viết kiểm thử theo quy tắc, ghi `proto <mã>`.

| Quy tắc prototype | Nội dung | Module |
|---|---|---|
| proto FM-05 | Mỗi lượt xuống ghe một chuyến TMS đường thủy (số hiệu ghe bắt buộc, tấn ước theo cuộn); không xác định được đích → không lập chuyến, chỉ cảnh báo; thông báo hàng đến (`cho_den → da_den`) mang mã chuyến; cảnh báo ghe ngoài danh mục/ngừng/hết hạn đăng kiểm/tấn ước > 1,1 × tải | logistics |
| proto GH-01..03 | Số hiệu ghe chuẩn hóa (bỏ khoảng trắng, chữ hoa), duy nhất; loại `ghe`/`sa_lan`; đăng kiểm hết hạn **chỉ cảnh báo** (≤ 30 ngày: sắp hết hạn); cước theo đơn giá thuê `per_ton` / `per_trip` / `per_ton_km` | logistics |
| proto plant weighing đóng chuyến | Cân nhà máy đóng chuyến: tấn thật = tịnh / 1000; lệch > 5 % so với tấn ước → cờ | logistics (+ measurement đã có) |
| proto nhắc ghe chưa cân | Xuống ghe > 48 giờ chưa cân → cảnh báo vận hành, điều phối, kho | logistics |
| proto decideMode (FN-08 BR-04) | Mặc định đường bộ; gợi ý đường thủy khi xa hơn `roadFeasibleDistanceKm` (#27) **và** có kênh tiếp cận; người dùng đổi được theo chặng | logistics |
| proto createTrip / completeTrip | Cước kế hoạch = tấn kế hoạch × km × đơn giá phương thức; cước thực = nhập tay hoặc tấn thật × km × đơn giá; ghi `cost_source` | logistics |
| proto ePOD / e-bill | Chứng từ chuyến có người ký, thời điểm ký | logistics, orders |
| proto CT-01..03 | Một hợp đồng hiệu lực mỗi HTX mỗi kỳ (chồng lấn → từ chối); giá > 0, tấn cam kết > 0, kết thúc ≥ bắt đầu; cách tính `per_tonne` / `per_bale`; trần ẩm lưu, chưa cưỡng chế | orders |
| proto createGoodsReceipt | Nhận hàng lệch đơn mua > **3 %** → cần biên bản chênh lệch + ghi chú kiểm toán, **không chặn** | orders |
| proto createPurchaseOrder | Tấn > 0, giá > 0; `draft → approved` (thêm `cancelled`) | orders |
| proto createSalesOrder / advance | Tấn > 0 (Go thêm đơn giá > 0); thiếu tồn chỉ cảnh báo; trạng thái chỉ tiến `draft → confirmed → delivering → delivered → completed` | orders |
| proto approveGoodsIssue | Kiểm tồn lại lúc duyệt trong cùng giao dịch, theo lô, ưu tiên rủi ro cao rồi cũ nhất (đã có ở `mkg/hub`); Σ phân bổ = tấn xuất | hub (đã có) + orders |
| proto PM-01..04 | Quyết toán HTX (bảng trên) | costing |
| proto FM-09 / baleKg | Tấn = cuộn × kg/cuộn / 1000; kg/cuộn học theo HTX (30 lần cân gần nhất) → hệ thống → cấu hình → 20 | costing (đọc `measurement/port.RollTotals`) |
| proto buildCostChain | Giá thành = gom + kho + bốc xếp + vận chuyển (ruộng→hub + hub→nhà máy); đ/t = tổng / tấn giao; tấn giao = 0 → "không xác định"; không gồm khấu hao; giá mua rơm tính riêng | costing |
| proto loss #19 / #56 | Hao hụt dự kiến 5 % (#19) + hao hụt sơ chế hub 2 % (#56) | costing (đối soát đã ở measurement) |
| proto carbonRevenueShare | 45 % / 70-15-15 (bảng trên) | costing |
| proto D03 | Phải trả là nợ phải trả, **không** là chi phí trong báo cáo lãi lỗ | costing, acctbridge |
| proto params/actuals | Số đo thực chỉ là **đề xuất** tham số, đi qua duyệt; cước thực đo chỉ lấy chuyến `manual` / `vessel_rate`; tối thiểu 3 mẫu; lệch ≤ 5 % khớp, > 15 % lệch lớn | logistics, costing |

### 1.2 Tham số tạm (tạm, chờ khách)

Mọi số dưới đây là **tham số có phiên bản** của module (bảng `param_version`/`param_decision`/`param_current` như `mkg/hub`, `mkg/fieldops` — không dùng `pf/masterdata`, R-03), người đặt ≠ người duyệt (R-56), áp theo **thời điểm sự việc**. Mặc định `#0` mang nhãn "tạm, chờ khách". Khóa tham số là hằng trong `port/`.

| Khóa | Mặc định | Nguồn | Người duyệt |
|---|---|---|---|
| `logistics.rate.road_vnd_per_t_km` | 2 200 (khoảng 1 800–2 900) | proto #22 | `dispatcher` đặt ≠ `chief_accountant` duyệt |
| `logistics.rate.waterway_vnd_per_t_km` | 900 (650–1 250) | proto #23 | như trên |
| `logistics.mode.road_feasible_km` | chờ khách (#27) | proto decideMode | như trên |
| `logistics.vessel.straw_payload_tons` | 90 (50–140) | proto #51 | như trên |
| `logistics.vessel.payload_warn_factor` | 1,1 | proto FM-05 | như trên |
| `logistics.vessel.expiry_warn_days` | 30 | proto GH-02 | như trên |
| `logistics.trip.unweighed_alert_hours` | 48 | proto notify | như trên |
| Hệ số Haversine đường bộ | 1,3 (#10) | proto | như trên |
| `orders.receipt.po_variance_warn_pct` | 3 | proto `RECEIPT_VARIANCE_TOLERANCE_PCT`, g3-conventions §1.1 | `sales_officer` đặt ≠ `sales_approver` duyệt |
| `orders.sales.price_approval_delta_pct`, `orders.sales.discount_approval_pct` | chờ khách (không có trong prototype) | mkg.md §5.5 | như trên |
| `orders.payment_term_days` | 15 | proto | như trên |
| `costing.allocation.key` | `mass` (theo khối lượng lô trên chặng) | đề xuất G4-0 | `cost_accountant` đặt ≠ `chief_accountant` duyệt |
| `costing.moisture.base_pct` | không quy đổi | C-4 | như trên |
| `costing.loss.expected_pct` | 5 (3–10) | proto #19 | như trên |
| `costing.loss.hub_processing_pct` | 2 (0–6) | proto #56 | như trên |
| `costing.rate.hub_processing_vnd_per_t` | 120 000 (70 000–200 000) | proto #55 | như trên |
| `costing.settlement.payment_term_days` | 15 | proto `PAYMENT_TERM_DAYS` | như trên |
| `costing.carbon.retained_pct` / `split.farmers_pct` / `split.authority_pct` / `split.verifier_pct` | 45 / 70 / 15 / 15 | proto `CARBON-SHARE` | như trên |
| `acctbridge.account_mapping` | chờ khách (chế độ kế toán, hệ thống tài khoản) | §27 v4.1 (mục 16 v3.0) | `accounting_bridge_operator` đề xuất ≠ `chief_accountant` duyệt |
| `acctbridge.push.retry_max` | 8 (giãn cách mũ) | R-62 | như trên |

## 2. Số migration

Chỉ dùng số đã cấp. golang-migrate **không** áp số nhỏ hơn số đã chạy: database kiểm thử luôn dựng mới nên thứ tự merge không ảnh hưởng kiểm thử, nhưng database dev/Compose đã chạy số cao hơn phải dựng lại. Người tích hợp merge theo thứ tự số.

| Cụm | Số | Nội dung | Schema | Vai DB |
|---|---|---|---|---|
| mkg | 000011 | **bỏ trống** (dự phòng G3 không dùng — 000012, 000013 đã chạy) | | |
| mkg | **000014** | G4-0 (**đã có**): `CREATE SCHEMA` bốn module + `USAGE` cho đúng vai, không bảng | `mkg_logistics`, `mkg_orders`, `mkg_costing`, `mkg_acctbridge` | 4 vai dưới |
| mkg | 000015 | logistics: sổ chuyến `trip_event` (`trip:<id>`), chặng `leg_event`, `route_snapshot` (nguyên văn + SHA-256), danh mục ghe có phiên bản, chứng từ chuyến, tham số, bảng chiếu `trip_current`, `vessel_current` | `mkg_logistics` | `logistics_app` |
| mkg | 000016 | orders: hợp đồng mua rơm có phiên bản, đơn mua, phiếu nhận hàng (+ kiểm ↔ đơn mua), đơn bán, giao nhận, duyệt giá (R-56), tham số, bảng chiếu | `mkg_orders` | `orders_app` |
| mkg | 000017 | costing (1/2): sổ `cost_entry` (`lot:<id>`), `conversion_result` (cuộn → tấn, ẩm), `allocation_result`, tham số có phiên bản, bảng chiếu `lot_cost_current` | `mkg_costing` | `costing_app` |
| mkg | 000018 | costing (2/2): `period_event`, job đóng kỳ + ảnh chụp đầu vào bất biến, `period_summary` (kết quả đóng kỳ), `settlement_event` + bảng chiếu quyết toán (R-56 CHECK), chia doanh thu carbon | `mkg_costing` | `costing_app` |
| mkg | 000019 | acctbridge: bảng ánh xạ tài khoản có phiên bản, `summary_journal` (`period:<id>`), duyệt (R-56 CHECK), job, `push_attempt` (bảng vận hành) | `mkg_acctbridge` | `acctbridge_app` |
| mkg | **000020** | G4 tích hợp (**đã có**, 01/10/2026): sổ `mkg_fieldops.roll_count_vessel` (số hiệu ghe của bản ghi xếp ghe), sổ `mkg_logistics.trip_milestone_fix` (accuracy_m của mốc), bảng vận hành chỉ ghi thêm `mkg_logistics.alert_notice` (L-08), nới CHECK mốc `departed`/`arrived` của `trip_event` (lat/lng NULL khi không GPS, CHECK cặp lat/lng), `ag_outbox_publisher` đọc `(tenant_id, status)` của `trip_current`/`vessel_current` | `mkg_fieldops`, `mkg_logistics` | `fieldops_app`, `logistics_app`, `ag_outbox_publisher` |
| mkg | **000021** | G4 đóng khoảng trống (**đã có**, 01/10/2026): sổ `mkg_hub.arrival_event` + bảng chiếu `arrival_current` (thông báo hàng đến FM-05), bảng chỉ ghi thêm `mkg_orders.file_object` / `mkg_logistics.file_object` (tệp chứng từ ở kho đối tượng), sổ `mkg_logistics.trip_document` (ePOD / e-bill có người ký, L-11) | `mkg_hub`, `mkg_orders`, `mkg_logistics` | `hub_app`, `orders_app`, `logistics_app` |
| mkg | 000022 | **dự phòng G4** — xin người tích hợp | | |
| ts | 000004 | **tùy chọn** — telemetry phương tiện `ts_fleet` (logistics): phân vùng tháng như `ts_weigh`, `apply_append_only`, RLS; `ingest_ts` INSERT + SELECT, `ts_reader` SELECT | `ts_fleet` | `ingest_ts`, `ts_reader` |
| ts | 000005+ | dự phòng `ts_env` (cảm biến kho, `hub.risk.v2`) — **chuyển** từ ts 000004 của g3-conventions | `ts_env` | xin khi làm |
| pf | 000044 | **bỏ trống** (không dùng; 000045 đã chạy) | | |
| pf | **000046** | G4-0 (**đã có**): loại cấp quyền `waterway_routes` (CHECK `share_grant_event_kinds_check`) | `pf_party` | `party_app` |
| pf | 000047 | dự phòng phía nền tảng của G4 (vd loại `road_routes` khi có OSRM trong lãnh thổ) — xin người tích hợp | | |

Migration cụm `ts` **không** tham chiếu `pf_*`, `mkg_*`, `vault` (R-07), không dùng `timescaledb` — `migratecheck` cưỡng chế.

## 3. Vai

### 3.1 Vai DB

Đã khai trong `internal/shared/dbadmin/dbadmin.go` và userlist PgBouncer (CI `.github/workflows/ci.yml` + `deploy/compose/pgbouncer/entrypoint.sh`). Mật khẩu dùng `AG_DB_APP_PASSWORD` (hoặc `AG_DB_PASSWORD_<VAI>`), không cần sửa `scripts/dev-env*.ps1`. Agent module **không** sửa các tệp đó.

| Vai DB | Cụm | Của | Quyền |
|---|---|---|---|
| `logistics_app` | mkg | `mkg/logistics` | `USAGE mkg_logistics` (000014); bảng do 000015 |
| `orders_app` | mkg | `mkg/orders` | `USAGE mkg_orders` (000014); bảng do 000016 |
| `costing_app` | mkg | `mkg/costing` | `USAGE mkg_costing` (000014); bảng do 000017–000018 |
| `acctbridge_app` | mkg | `mkg/acctbridge` | `USAGE mkg_acctbridge` (000014); bảng do 000019 |

Cả bốn là `LOGIN`, thành viên `ag_shared_user` (outbox, inbox, idempotency), chỉ `CONNECT ag_mkg`. **Không** có quyền xuyên schema: costing đọc hub/measurement/logistics/orders qua `port/` bằng pool vai của module chủ dữ liệu (như `mkgwire.Rolls` ở G3), không `GRANT SELECT` chéo.

### 3.2 Vai nghiệp vụ

Hằng trong `port/`, đã có trong realm Keycloak (`TestKeycloakRealmCoversRolesAndClaims` đạt). Phê duyệt ở vùng mkg dùng vai riêng của MKG, không dùng `param_setter`/`param_approver`.

| Vai | Module (hằng) | Làm gì | R-56 |
|---|---|---|---|
| `dispatcher` | `logistics/port.RoleDispatcher` | lập/hủy chuyến, gán phương tiện/tài xế, chọn phương thức theo chặng, xác nhận cước thực, danh mục ghe | tham số cước: đặt ≠ `chief_accountant` duyệt |
| `driver` | `logistics/port.RoleDriver` | mốc chuyến trên app | — |
| `sales_officer` | `orders/port.RoleSalesOfficer` | đơn bán, đơn mua, hợp đồng mua rơm, giao nhận, phiếu nhận | lập ≠ duyệt |
| `sales_approver` | `orders/port.RoleSalesApprover` | duyệt đơn/giá/chiết khấu vượt ngưỡng, hợp đồng | không duyệt của mình |
| `cost_accountant` | `costing/port.RoleCostAccountant` | chi phí, phân bổ, lập quyết toán, bút toán điều chỉnh, đề xuất tham số | lập ≠ duyệt |
| `period_closer` | `costing/port.RolePeriodCloser` | yêu cầu đóng kỳ | ≠ người lập điều chỉnh của kỳ |
| `chief_accountant` | `costing/port.RoleChiefAccountant` (= `acctbridge/port.RoleChiefAccountant`) | duyệt quyết toán HTX, tham số giá thành, bảng ánh xạ tài khoản, bút toán tổng hợp; bề mặt quản trị MFA | không duyệt việc của mình |
| `accounting_bridge_operator` | `acctbridge/port.RoleAccountingBridgeOperator` | dựng bút toán tổng hợp, đề xuất ánh xạ, đẩy lại từ DLQ | dựng ≠ duyệt |

Tài xế, chủ ghe, nhân viên MKG: định danh ở vault tenant MKG, trong mkg chỉ `*_ref` (RB-3). Prototype lưu tên tài xế văn bản tự do — **không** làm thế.

**`driver_ref` = `sub` của token** (quyết định tích hợp 01/10/2026): tài xế là tài khoản Keycloak của tenant MKG (vai `driver`); `driver_ref` của chuyến là `sub` (= `authz.Principal.ActorRef`), như `member_refs` của tổ gom fieldops. Tên, điện thoại ở vault tenant MKG, khóa theo cùng `sub` — mkg không giữ PII. Người **chỉ** có vai `driver` (không `dispatcher`): danh sách chuyến tự lọc `driver_ref = sub`, đọc chuyến khác → 404, ghi mốc chuyến khác → 403 `logistics.not_assigned_driver`, không thấy cước (chuyến, chặng). Chủ ghe thuê ngoài không có tài khoản: `driver_ref` rỗng, điều phối ghi mốc thay.

## 4. Sự kiện G4

Tất cả là **sự kiện nội bộ** (`contracts/events-internal/<tên>.v1.json` + struct `//ag:internal-event` trong `port/`, phát với `outbox.KindInternal` lên vhost `mkg.internal`). Không có hợp đồng van mới ở G4: chuyến, cước, giá, giá thành, quyết toán, bút toán **không bao giờ** qua van.

| Sự kiện | Phát | Nhận (hàng đợi) | Nội dung chính |
|---|---|---|---|
| `mkg.logistics.trip.completed` v1 | logistics | costing (`mkg.costing.trip`) | chặng (phương thức, km, nguồn tính, `route_snapshot_ref`, phiên bản mạng), lô trên chuyến + khối lượng + phiếu cân, cước thực + nguồn, bản bù `supersedes_id` |
| `mkg.orders.delivery.accepted` v1 | orders | costing (`mkg.costing.delivery`) | đơn, khách (`*_ref`), tuyến HUB/DIRECT, **phiếu `CUSTOMER_IN`**, lô + khối lượng, đơn giá, số tiền, bản bù |
| `mkg.costing.period.closed` v1 | costing | acctbridge (`mkg.acctbridge.period_closed`) | kỳ, `close_run_id`, băm ảnh chụp đầu vào, `result_set_sha256` (= `source_hash`) |
| `mkg.costing.settlement.approved` v1 | costing | — (chưa có consumer bắt buộc) | HTX, cách tính, cuộn/khối lượng, đơn giá, số tiền, hạn, người lập ≠ người duyệt |
| `mkg.acctbridge.journal.pushed` v1 | acctbridge | costing (`mkg.costing.journal_pushed`: kỳ → `posted`) | bút toán, bộ điều hợp, `external_ref`, Σ Nợ = Σ Có, lần thử |
| `mkg.costing.period_close.requested` v1 | costing | costing (`mkg.costing.period_close`) | job đóng kỳ (mẫu §11 v4.1, mục 12 v3.0) |
| `mkg.acctbridge.journal_build.requested` v1 | acctbridge | acctbridge (`mkg.acctbridge.journal_build`) | job dựng bút toán |
| `mkg.acctbridge.journal_push.requested` v1 | acctbridge | acctbridge (`mkg.acctbridge.journal_push`) | job đẩy bút toán đã duyệt |
| `mkg.logistics.trip.milestone_recorded` v1 (**thêm 01/10/2026**, G4 đóng khoảng trống) | logistics | hub (`mkg.hub.trip_milestone`) | mốc chuyến có đích là cơ sở (chặng cuối `to_kind = facility`): `departed`, `rerouted` (lập lại chặng khi đang chạy), `arrived`, `completed`, `cancelled`; mã chuyến, cơ sở đích, số hiệu phương tiện, lô + tấn ước — **không** cước, tài xế. Hub ghi thông báo hàng đến proto FM-05 `cho_den` (`expected`) → `da_den` (`arrived`) |

Sự kiện G3 mà module G4 nhận:

| Sự kiện G3 | Hàng đợi G4 | Việc |
|---|---|---|
| `mkg.measurement.weigh_ticket.recorded` | `mkg.costing.weigh_ticket` | khối lượng chuẩn lô → giá thành đ/t, quyết toán theo tấn (chờ cân đủ) |
| | `mkg.logistics.weigh_ticket` | đóng chuyến với tấn thật (plant weighing) |
| | `mkg.orders.weigh_ticket` | `CUSTOMER_IN` → gắn vào giao nhận chờ chấp nhận; `HUB_IN` của lô có đơn mua → kiểm ↔ đơn mua > 3 % |
| `mkg.measurement.reconciliation.completed` | `mkg.costing.reconciliation` | quy kết hao hụt bất thường về lô nguồn theo tỷ lệ nguồn (`hub/port.LotOrigins`) |
| `mkg.fieldops.roll_count.recorded` | `mkg.costing.roll_count` | quyết toán theo cuộn (PM-02) |
| | `mkg.logistics.roll_count` | công đoạn `boat_load` → chuyến đường thủy `planned` (FM-05) |

- Consumer idempotent qua `shared.inbox` (R-61; kiểm thử giao lặp); DLQ = tên + `.dlq` (R-62).
- Consumer chỉ ghi sổ đầu vào / đánh dấu cần tính lại; **không** tự đóng kỳ. Ảnh chụp đầu vào của đóng kỳ đọc lại qua `port/` với `asOf`.
- `roll_count.recorded` v1 có trường **tùy chọn** `vessel_code` (tích hợp 01/10/2026 — thêm trường tùy chọn là tương thích ngược theo `contractcheck -base`, không lên v2): số hiệu ghe/sà lan người thu gom nhập khi xếp ghe (`boat_load`), chuẩn hóa như GH-01. Logistics gán phương tiện của chuyến FM-05 khi số hiệu khớp danh mục; không khớp → cờ `vessel_unregistered` + `no_vehicle`; vắng (app cũ) → `no_vehicle`, điều phối gán sau. API app hiện trường (`POST …/roll-counts`, `sync/push`) nhận `vessel_code`; app `apps/mkg-field-app` có ô nhập tùy chọn ở công đoạn xếp ghe (G4 đóng khoảng trống, chuẩn hóa như máy chủ).
- Tên hằng: `logistics/port` (`EventTripCompleted`, `RollCountConsumer`, `WeighTicketConsumer`), `orders/port` (`EventDeliveryAccepted`, `WeighTicketConsumer`), `costing/port` (`EventPeriodClosed`, `EventSettlementApproved`, `EventPeriodCloseRequested`, sáu `*Consumer`, `PeriodCloseConsumer`), `acctbridge/port` (`EventJournalPushed`, `EventJournalBuildRequested`, `EventJournalPushRequested`, `PeriodClosedConsumer`, `JournalBuildConsumer`, `JournalPushConsumer`). `cmd/worker-internal/assembly` `TestG4FrozenInternalContracts` kiểm schema và tên hàng đợi.

## 5. Vùng và cửa

Không đổi so với g3-conventions §5. Bổ sung:

| Chiều | G4 |
|---|---|
| Nền tảng → MKG | `GET /v1/partner/waterway/route?grantor=&from=lat,lng&to=lat,lng&min_load_tons=&version=` và `POST /v1/partner/waterway/route?grantor=` (thân `{points[2..25], min_load_tons?, version?}`, `Idempotency-Key` bắt buộc — MKG dùng SHA-256 của thân), vai `mkg_sync`, cấp quyền `waterway_routes`. Nền tảng định tuyến ở tenant **bên cấp** sau kiểm cấp quyền (tham số #10, bán kính tiếp cận của bên cấp), ghi `partner_read_log` trước khi trả. Phản hồi `PartnerWaterwayRoute` (`contracts/openapi/application.yaml`). Kiểm thử `TestPartnerWaterwayRoute_ADR0020`, `TestShareGrantWaterwayRoutesKind_G4`. Phía MKG: `logistics/port.WaterwayRoutes` (hiện thực ở `logistics/adapter`, cùng tài khoản dịch vụ `AG_MKG_SA_*` và `AG_PARTNER_GRANTOR_TENANT` của fieldops) |
| MKG → nền tảng | Không có van mới. Nếu sau này cần (vd CO2 vận chuyển theo bậc cho MRV) → hợp đồng van mới, PR riêng, `proposed_by` ≠ `approved_by` |
| Trong MKG | `port/` đồng bộ cùng tiến trình (mặc định) + sự kiện nội bộ mục 4 |
| Kế toán pháp định | Một chiều, ra ngoài, tháng; đích trong lãnh thổ (RB-7). Bộ xuất tệp ghi kho đối tượng trong lãnh thổ |

## 6. Hợp đồng đóng băng ở G4-0

Đổi chữ ký hoặc ngữ nghĩa phải qua người tích hợp. Thêm phương thức → interface **mới**; thêm trường DTO/sự kiện chỉ khi tương thích ngược (trường tùy chọn, schema cập nhật cùng lúc).

| Hợp đồng | Nơi | Nội dung |
|---|---|---|
| Cổng logistics | `internal/mkg/logistics/port` | `Trips` (`Trip`, `TripsForLot`, `Completed(from, to, asOf, cursor, limit)`), `WaterwayRoutes.Route`, `RoadRoutes.Route` (`RouteRequest` → `RouteResult` kèm `Raw` + `RawSHA256` để lưu nguyên văn), `TripView`/`LegView`/`CargoLine`, trạng thái chuyến, `CostSource`, `RouteSource`, lỗi `Err*`, khóa tham số |
| Cổng orders | `internal/mkg/orders/port` | `Deliveries` (`Delivery`, `Accepted(from, to, asOf, …)`), `Contracts.ContractFor(htx, on)` (`ErrNoContract` → không đoán giá), `ReceiptCheck`, `PricingBasis`, trạng thái giao nhận |
| Cổng costing | `internal/mkg/costing/port` | `ClosedPeriods.ClosedPeriod` → `PeriodSummary{Lines []SummaryLine}` tất định, `CostCategory`, trạng thái kỳ và quyết toán, khóa tham số |
| Cổng acctbridge | `internal/mkg/acctbridge/port` | `LedgerAdapter{Name(); Push(ctx, SummaryJournal) (ExternalRef, error)}` — idempotent theo `IdempotencyKey`, `ErrAdapterUnavailable` (thử lại) / `ErrAdapterRejected` (DLQ); `SummaryJournal`, `JournalLine`, `Side` |
| Sự kiện nội bộ | `contracts/events-internal/*.v1.json` + struct `//ag:internal-event` | tám sự kiện mục 4 |
| Bề mặt đối tác | `contracts/openapi/application.yaml` `/v1/partner/waterway/route`; `pf/party/port.KindWaterwayRoutes` | mục 5 |

Cổng G3 mà G4 đọc (đã có, không đổi): `hub/port.Lots`, `hub/port.LotOrigins` (nguồn + tỷ lệ Σ = 1), `measurement/port.LotMass`, `measurement/port.RollTotals` / `RollTotalsAsOf`, `measurement/port.CollectionLinks`, `fieldops/port`.

Gốc lắp ráp: **chưa** nối module G4 (chưa có module). Chỗ nối ghi chú ở `cmd/api-internal/assembly.Build` và `cmd/worker-internal/assembly.Consumers`; mỗi agent module chỉ thêm dòng của mình (xung đột merge nhỏ, người tích hợp gộp).

## 7. Kiểm thử và diễn tập

- **Bắt buộc cho mọi module G4** (g1-conventions §5 + G3): cách ly tenant (kể cả song song qua chế độ PgBouncer — `Test…TenantLeakParallel`), sáu phép thử UPDATE/DELETE/TRUNCATE trên mọi sổ mới, idempotent POST, giao lặp sự kiện (R-61), sửa sai bằng bản ghi bù, tự duyệt bị chặn ở app/ **và** CHECK DB (R-56), `agnofloat` (không float ở bất kỳ package nào của bốn module).
- **costing:** tính tất định (cùng ảnh chụp + cùng `rule_version` → cùng kết quả byte-cho-byte); tính lại → kết quả mới `supersedes_id`, kết quả cũ không đổi; tham số theo thời điểm sự việc; đóng kỳ 202 + job, giao lặp job → một kết quả; `POST /v1/costing/projections/rebuild` = dựng thẳng từ sổ.
- **acctbridge:** Σ Nợ = Σ Có; cùng kỳ + cùng phiên bản ánh xạ → cùng tệp xuất (băm); đẩy lại → cùng `ExternalRef`; lỗi tạm thời → thử lại, lỗi vĩnh viễn → DLQ.
- **logistics:** kết quả định tuyến lưu nguyên văn, đọc chặng cũ không gọi lại nền tảng; nền tảng lỗi / thiếu cấp quyền → chuyến vẫn lập được (km tay có lý do hoặc Haversine), không chặn đếm cuộn.
- **Diễn tập R-22 trên bản sao dữ liệu thật:** chạy trên **staging** với bản sao ẩn danh hóa không đảo ngược (§33 v4.1, mục 21 v3.0, R-95); so bảng chiếu dựng lại với bảng chiếu hiện hành của mọi module có `projections/rebuild` (hub, measurement, fieldops, costing, logistics, orders, acctbridge) — mở rộng `cmd/drill` (việc của agent E2E/vận hành). Máy dev chỉ chạy được trên dữ liệu tổng hợp (`cmd/seed-synthetic -plan mkg`, cần mở rộng cho chuyến/đơn/kỳ).
- **Diễn tập tải ĐTK 2** và **phục hồi**: kịch bản và số liệu mục tiêu do agent hạ tầng/E2E lập; kết quả ghi ở g4-acceptance.
- Mã story ghi tiền tố: `proto FM-05`, `proto GH-02`, `proto PM-03`, `proto CT-01`, `GIS FN-08 BR-04`…
- Máy dev (Windows, PostgreSQL 17 cục bộ, không Docker/RabbitMQ/Kubernetes): như g3-conventions §7. Kubernetes, NetworkPolicy, PKI nội bộ chỉ kiểm được trên cụm — ghi trong báo cáo.

## 8. Quyết định tích hợp G4 (01/10/2026)

| Chủ đề | Quyết định |
|---|---|
| worker-internal ↔ costing | Mọi consumer costing dựng qua `assembly.CostingDeps`: `Origins` (pool `hub_app`), `Contracts` + `Deliveries` (pool `orders_app`), `Trips` (pool `logistics_app`) — chỉ đọc, tenant tường minh, không GRANT chéo schema. `buildAcctbridge` đọc `ClosedPeriods` trên pool `costing_app` (kiểm thử đầu–cuối `TestWorkerInternalCostingToAcctbridgeWiring_G4`) |
| L-08 thông báo | MKG không có kênh thông báo riêng (pf/notify ở vùng nền tảng, R-03). Job `logistics_alerts` (api-internal, `AG_MKG_JOBS`, mỗi `AG_LOGISTICS_ALERT_INTERVAL`, mặc định 15 phút) ghi **một** thông báo mỗi điều kiện vào `mkg_logistics.alert_notice` (chống lặp: theo chuyến / theo (ghe, hạn đăng kiểm)), đọc qua `GET /v1/logistics/alert-notices`, và đặt gauge `ag_logistics_alerts_open{kind}` → Alertmanager gửi (luật `agrigreen-mkg-logistics.rules.yml`). Đẩy tới điện thoại (FCM) để sau — cần kênh thông báo vùng mkg |
| Bù phiếu HUB_IN sau duyệt | Phiếu nhận đã duyệt không mở lại: ghi bản bù `corrected` (vẫn `approved`, người duyệt giữ nguyên, kiểm ↔ đơn mua lại, chỉ cảnh báo) + bản bù khoản phải trả đơn mua khác (`supersedes_id`, số lượng + tiền mới, giữ hạn + điều khoản đã chốt lúc duyệt). Khoản phải trả hiệu lực = dòng mới nhất của stream `payable:<receipt>`. Rơm: không phải trả ở orders (O-11) |
| O-05 thiếu tồn | Cổng đọc thêm `hub/port.Stock.StockByProduct` (tồn sổ `lot_balance` theo dạng hàng / cơ sở, tenant tường minh — chưa trừ phiếu xuất chờ duyệt). Orders tính `stock_check` lúc lập / xác nhận / đọc đơn draft|confirmed, không lưu; thiếu → `warnings: [orders.stock_shortage]`, không chặn; `orders.sales.stock_shortage_blocking` giữ 0 (giá trị 1 hiện chỉ hiển thị `blocking: true`, chưa chặn — chờ khách) |
| Mốc chuyến không GPS | `lat`/`lng` tùy chọn (NULL, không bao giờ 0,0; app cũ gửi 0,0 → coi như không có); `accuracy_m` tùy chọn ở sổ riêng `trip_milestone_fix`. Delta `updated_since` theo `trip_current.recorded_at` (sự kiện cuối) |
| Tách cụm | `migrate bootstrap` theo `AG_CLUSTERS` (`dbadmin.BootstrapClusters`, `RolesFor`): chỉ vai + database của cụm chọn, vai nhóm NOLOGIN luôn có; rỗng = mọi cụm |
| Sức khỏe | `worker` (:8087) và `outbox-publisher` (:8088) có `/healthz`, `/readyz` qua `AG_HEALTH_ADDR` (`cmd/internal/healthsrv`), `-healthcheck <url>`; chart có probe |
| Diễn tập R-22 | `cmd/rebuilddrill` (gốc lắp ráp vùng mkg): băm → `Module.RebuildTenant` → băm, báo cáo Markdown + JSON ([rebuild-projection.md](../runbooks/rebuild-projection.md#diễn-tập-g4)). `mkg/fieldops` có hàm dựng lại từ G4 đóng khoảng trống (sáu bảng chiếu; `sync_seq` giữ giá trị cũ; hai bảng đệm nguồn ngoài không dựng lại) |

### 8.1 Đóng khoảng trống mã (G4-F, 01/10/2026)

| Chủ đề | Quyết định |
|---|---|
| Thông báo hàng đến (FM-05) | Sự kiện nội bộ mới `mkg.logistics.trip.milestone_recorded` v1 (mục 4) phát trong cùng giao dịch với mốc chuyến / lập lại chặng khi đang chạy, chỉ khi chặng cuối của kế hoạch hiện hành tới `facility`. Hub: sổ `arrival_event` (`arrival:<trip_id>`) + `arrival_current`, trạng thái chỉ tiến `expected → arrived`, `cancelled` chỉ từ `expected`, mốc lệch thứ tự / lặp bị bỏ qua; idem theo dòng nguồn logistics. `GET /v1/hub/arrivals?facility_id=&status=&cursor=` (vai hub); `proto_status` = `cho_den`/`da_den`/`huy`. Hub `RebuildTenant` dựng cả `arrival_current` |
| Tệp chứng từ vùng mkg | Không có module tệp riêng (cần vai DB mới): mỗi module chủ dữ liệu có bảng chỉ ghi thêm `file_object` và route riêng — `POST /v1/orders/files` (mục đích `receipt_variance_report` / `delivery_evidence`), `POST /v1/logistics/files` (`trip_document`). Kiểm tệp dùng chung `internal/shared/docfile` (≤ 15 MB, PDF/JPEG/PNG/WebP theo nội dung, SHA-256 khai báo, tên an toàn — cùng mẫu tệp đính kèm pf/content, không import nền tảng). Nội dung ở `shared/objstore` (`AG_S3_*` / `AG_OBJSTORE_DIR`), khóa `mkg/<module>/<tenant>/<sha256>`, tải về qua API có kiểm vai (không URL có chữ ký). Chưa cấu hình kho → 503 `*.file_store_unavailable`. Phiếu nhận / giao nhận: `file_ref` đã đăng ký phải đúng mục đích (422 `orders.file_purpose`); `file_ref` chưa đăng ký vẫn nhận (tương thích ngược). **Chưa mã hóa phong bì** (cần dịch vụ khóa vùng mkg) — mục mở, chỉ dựa SSE + khóa đối tượng của kho trong lãnh thổ |
| Chứng từ chuyến (L-11) | Sổ `trip_document`: `kind` epod/ebill/minutes, `file_ref` + `file_sha256` (chép từ `file_object`), `signer_role`, `signer_ref` (*_ref, không tên), `signed_at`; tài xế được giao hoặc điều phối; bản bù `supersedes_id` + lý do |
| Danh sách cho web | `GET /v1/costing/lots`, `/v1/costing/carbon-revenue`, `/v1/costing/period-close-jobs`, `/v1/acctbridge/jobs`, `/v1/logistics/vessels/{id}/versions` — con trỏ `cursor` = khóa cuối trang (`next_cursor`), mới nhất trước (phiên bản phương tiện: version tăng dần) |
| fieldops dựng lại | `POST /v1/fieldops/projections/rebuild` (quản trị, `field_supervisor`) + `Module.RebuildTenant` cho `cmd/rebuilddrill` |

## 9. Giả định tạm (ghi trong báo cáo nếu bám vào)

- Một tenant MKG, một bên cấp (tenant chương trình) cho `waterway_routes`; nhiều bên cấp → tham số `grantor` (`AG_PARTNER_GRANTOR_TENANT`).
- Tiền tệ duy nhất VND. Kỳ theo tháng dương lịch, giờ Việt Nam.
- Cước chuyến phân bổ về lô **theo khối lượng** lô trên chặng; chi phí hub phân bổ theo khối lượng lô qua hub; lô đầu ra của mẻ nhận giá thành theo tỷ lệ nguồn (`hub/port.LotInfo.Sources`).
- Doanh thu giao nhận = khối lượng `CUSTOMER_IN` được chấp nhận × đơn giá đơn bán (không trừ chênh tranh chấp cho tới khi hồ sơ tranh chấp đóng).
- Chưa có OSRM; đường bộ = Haversine × 1,3.
- Bộ điều hợp kế toán mặc định = xuất tệp CSV (UTF-8, `;`, số thập phân dấu chấm) và XML; tên tệp tất định `<period_code>_<journal_id>.<ext>`.

## 10. Mục mở (rà E2E, 01/10/2026)

Kịch bản `TestG4TripToLedger` đạt; bảng nghiệm thu: 85 yêu cầu — 59 đạt, 13 một phần, 13 chưa làm ([g4-acceptance.md](../uat/g4-acceptance.md)).

Đã sửa khi rà E2E (không đổi hợp đồng):
- `pf/waterway` đọc tải lớp phương tiện ở `capacity_t` (lược đồ `vessel_class` của `pf/masterdata`), giữ `tons` tương thích — trước đó mọi lần dựng mạng trên dữ liệu chủ đã nạp đều 422 `waterway.params_invalid`.
- `mkg/costing`: `trip.completed` của lệnh gom làm mới quyết toán chưa duyệt (PM-02) — không còn phụ thuộc thứ tự giữa hàng đợi `mkg.costing.weigh_ticket` và `mkg.logistics.weigh_ticket`.
- `mkg/measurement`: `RebuildLotMass` đặt `as_of` / `last_seq` từ sổ phiếu (dựng lại = bảng chiếu hiện hành, R-22).

Còn mở:

| Loại | Việc |
|---|---|
| mã | Chứng từ chuyến ePOD / e-bill (L-11); thông báo hàng đến mang mã chuyến cho hub/kho (T-1); ô nhập số hiệu ghe trên app Flutter (nếu chưa gộp); costing đọc ước lượng kg/cuộn của fieldops (C-04); hàm dựng lại bảng chiếu `mkg/fieldops` (R-22); phần G4 của `cmd/drill` (A-5); đăng ký nguồn `ag.projection.lag` cho module G4 + chỉ số / luật job đóng kỳ lỗi (K-12); `cmd/seed-synthetic -plan mkg` cho chuyến, đơn, kỳ, quyết toán (K-14); telemetry `ts_fleet` (tùy chọn) |
| chờ khách | Q-1..Q-7 (phần mềm kế toán + chế độ + hệ thống tài khoản, khóa phân bổ, công thức giá quyết toán + hạn, tỷ lệ carbon, ngưỡng duyệt giá, quy đổi ẩm, đơn giá cước / ngưỡng đường thủy / tải ghe); khối lượng ĐTK 2 của diễn tập tải |
| hạ tầng | Cụm Kubernetes được quản trị trong lãnh thổ; NetworkPolicy, PKI / mTLS, xoay chứng thư, admission ký image trên cụm; CA thiết bị production + VPN biên; pgBackRest + diễn tập phục hồi; diễn tập tải và R-22 trên staging (bản sao ẩn danh); PgBouncer thật; `promtool`; CI; máy ảo app |
