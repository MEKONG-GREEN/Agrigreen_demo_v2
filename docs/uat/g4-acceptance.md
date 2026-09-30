# Nghiệm thu cổng G4: yêu cầu → kiểm thử

**Khung** lập ngày 30/09/2026 ở G4-0 (nhánh `sub/g4`). Chưa có mã module G4 nên mọi dòng đang **chưa làm**. Agent module, web/app, hạ tầng và E2E điền cột kiểm thử và trạng thái khi làm; người tích hợp rà lại toàn bảng trước khi đóng cổng (như [g3-acceptance.md](g3-acceptance.md)). Mỗi yêu cầu có đúng một dòng. Tên kiểm thử ghi vào bảng phải tra được trong mã.

Yêu cầu lấy từ:
- kiến trúc v3.0: Phụ lục A dòng G4, mục 12 (luồng bất đồng bộ), 16 (`mkg/costing`, `mkg/acctbridge`, `pf/waterway`), 17 (phân vùng mạng), 18, 19, 20 (kiểm thử định kỳ), 21, 22 (sao lưu, phục hồi);
- [docs/design/mkg.md](../design/mkg.md) §5.4–5.8, §6, §7 và [g4-conventions.md](../dev/g4-conventions.md);
- hai dòng **"ngoài G3 → G4"** của [g3-acceptance.md](g3-acceptance.md): proto FM-05, phiếu nhận hàng ↔ đơn mua > 3 %;
- quy tắc của prototype `mekong-green` (`src/erp/tms`, `sales`, `procurement`, `warehouse`, `straw`, `finance`, `field` FM-05/09/10, `params/actuals`) — prototype là đặc tả chạy được, không chép mã. Prototype **không có kiểm thử** cho đơn bán, đơn mua, three-way match, dung sai 3 %, chia carbon, định tuyến TMS, chênh cước;
- **BRD GIS v1.3**: FN-08 (BR-04, BR-28..33), FN-09 (BR-34..41), FN-17 (TMS dùng bản đồ). Các BRD/backlog khác (HTX, Khuyến nông, CGH) **không có** yêu cầu về vận tải, đơn hàng, giá thành, quyết toán, kế toán của MKG (CGH loại trừ giao dịch/thanh toán; GIS FN-08 giao chọn xe/tài xế, lập lịch cho TMS);
- quyết định: [ADR-0003](../adr/0003-event-sourcing-muc-b.md), [ADR-0008](../adr/0008-phan-vung-mang.md), [ADR-0018](../adr/0018-su-kien-noi-bo-mkg.md), [ADR-0020](../adr/0020-lien-thong-platform-mkg-khac-tenant.md).

Bốn trạng thái:
- **đạt**: có kiểm thử tự động chứng minh tiêu chí chấp nhận (kiểm thử Go `Test…`, bước E2E-G4-xx, WEB-E2E `*.spec.ts`, Vitest, Flutter, hoặc báo cáo diễn tập có lưu). Tham số **tạm, chờ khách** vẫn **đạt** nếu cơ chế (phiên bản, người đặt ≠ người duyệt, ghim `rule_version`) đã chứng minh;
- **một phần**: ghi rõ phần còn thiếu và loại — **mã**, **chờ khách**, **hạ tầng**;
- **chưa làm**: chưa có mã / chưa diễn tập;
- **ngoài G4 → G5/…**: chỉ cho hạng mục mà kiến trúc hoặc lộ trình đặt ở cổng sau.

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | chưa làm | ngoài G4 |
|---|---|---|---|---|---|
| 3. Phụ lục A (G4) | 5 | 0 | 0 | 5 | 0 |
| 4. Chuyển từ G3 | 2 | 0 | 0 | 2 | 0 |
| 5. `mkg/logistics` | 13 | 0 | 0 | 13 | 0 |
| 6. `mkg/orders` | 11 | 0 | 0 | 11 | 0 |
| 7. `mkg/costing` | 16 | 14 | 2 | 0 | 0 |
| 8. `mkg/acctbridge` | 9 | 0 | 0 | 9 | 0 |
| 9. Cổng kiến trúc G4 (RB, R-xx, vùng, hạ tầng) | 14 | 0 | 0 | 14 | 0 |
| 10. Client và kịch bản đầu–cuối | 4 | 0 | 0 | 4 | 0 |
| 11. Câu hỏi khách | 7 | 0 | 0 | 7 | 0 |
| **Tổng** | **81** | **14** | **2** | **65** | **0** |

Đã có ở G4-0 (nền móng, không tính là đạt dòng nào): bề mặt đối tác `/v1/partner/waterway/route` phía nền tảng (`TestPartnerWaterwayRoute_ADR0020`, `TestShareGrantWaterwayRoutesKind_G4`), hợp đồng sự kiện nội bộ (`TestG4FrozenInternalContracts`), vai trong realm (`TestKeycloakRealmCoversRolesAndClaims`), `agnofloat` cho bốn module (`TestNoFloat`).

## 1. Cách chạy

Như [g3-acceptance.md §1](g3-acceptance.md#1-cách-chạy). Kịch bản E2E G4 (mục 2) và các diễn tập ghi lệnh chạy ở đây khi có.

## 2. Kịch bản đầu–cuối (chưa có)

Đề xuất `TestG4FieldToLedger` (`tests/e2e/g4_scenario_test.go`), hai tenant (chương trình ≠ MKG):

| Bước | Nội dung |
|---|---|
| E2E-G4-01 | Tenant chương trình duyệt cấp quyền `waterway_routes` cho tenant MKG; mạng đường thủy đã phát hành |
| E2E-G4-02 | Hợp đồng mua rơm HTX (theo tấn) được duyệt (người lập ≠ người duyệt) |
| E2E-G4-03 | Lệnh gom → đếm cuộn → xuống ghe (`boat_load`) → chuyến đường thủy `planned`, tấn ước (FM-05), định tuyến qua `/v1/partner/waterway/route`, snapshot lưu nguyên văn |
| E2E-G4-04 | Cân HUB_IN → chuyến `completed` với tấn thật, cước thuê ghe (GH-03) → `trip.completed` |
| E2E-G4-05 | Phiếu nhận hàng ↔ đơn mua lệch > 3 % → cảnh báo + biên bản, không chặn |
| E2E-G4-06 | Mẻ sấy/nén ở hub → xuất → chuyến đường bộ → CUSTOMER_IN → giao nhận chấp nhận → `delivery.accepted` |
| E2E-G4-07 | Quyết toán HTX theo tấn (chờ cân đủ) → duyệt `chief_accountant` → một khoản phải trả |
| E2E-G4-08 | Đóng kỳ tháng (202 + job) → `period.closed`; tính lại không đổi kết quả đã đóng |
| E2E-G4-09 | Bút toán tổng hợp → duyệt → bộ xuất tệp → `journal.pushed` → kỳ `posted` |
| E2E-G4-10 | Dựng lại mọi bảng chiếu từ sổ = bảng chiếu hiện hành (R-22) |

## 3. Phụ lục A (G4)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| A-1 | Kubernetes được quản trị, trong lãnh thổ (RB-7, R-82 bằng chứng tách logic nếu đám mây) | — | **chưa làm** (hạ tầng) |
| A-2 | Diễn tập tải ĐTK 2, gồm hiệu năng RLS ở khối lượng ĐTK 2 (mục 20) | — | **chưa làm** |
| A-3 | Tách đơn vị triển khai bằng cấu hình, không sửa mã (RB-5): `api-internal` `AG_MKG_JOBS`, `worker-internal` chọn consumer (`AG_WORKER_INTERNAL_CONSUMERS`), `api-platform`/`worker` tương tự | — | **chưa làm** |
| A-4 | Dựng lại bảng chiếu từ sổ trên **bản sao dữ liệu thật** ẩn danh hóa (R-22, mục 20–21): mọi module có `projections/rebuild` | — | **chưa làm** |
| A-5 | Diễn tập phục hồi hàng quý trên môi trường sạch (mục 22): số dòng, băm tham số, `verify_chain`, mẫu tính lại, checksum kho đối tượng — mở rộng `cmd/drill` cho cụm mkg G4 (một kỳ costing mẫu tính lại) | `cmd/drill` (G1, chưa có phần G4) | **chưa làm** |

## 4. Chuyển từ G3

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| T-1 | proto FM-05: mỗi lượt xuống ghe sinh một chuyến vận tải đường thủy (số hiệu ghe, tấn ước theo cuộn; lỗi TMS / không xác định đích → chỉ cảnh báo, đếm cuộn vẫn ghi; thông báo hàng đến mang mã chuyến) | — (phía nền tảng định tuyến: `TestPartnerWaterwayRoute_ADR0020`) | **chưa làm** (`mkg/logistics`) |
| T-2 | proto phiếu nhận hàng ↔ đơn mua: lệch > 3 % → cần biên bản chênh lệch + ghi chú kiểm toán, **không chặn** | — | **chưa làm** (`mkg/orders`) |

## 5. `mkg/logistics` (mkg.md §5.4)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| L-01 | Chuyến, chặng, điểm dừng là sổ (`trip:<id>`) + bảng chiếu `trip_current` cùng giao dịch; dựng lại được | — | **chưa làm** |
| L-02 | Trạng thái chuyến chỉ tiến `planned → departed → arrived → completed`, hủy có lý do (Go cưỡng chế — prototype không) | — | **chưa làm** |
| L-03 | Kết quả định tuyến đã dùng lưu nguyên văn + SHA-256 vào chặng; đọc chặng cũ không gọi lại nền tảng; ghim phiên bản mạng (mục 16) | — | **chưa làm** |
| L-04 | Định tuyến đường thủy chỉ qua bề mặt đối tác + cấp quyền `waterway_routes` (ADR-0020); thiếu cấp quyền / nền tảng lỗi → km tay có lý do hoặc Haversine, không chặn | — | **chưa làm** |
| L-05 | Đường bộ: Haversine × hệ số #10 (1,3) tới khi có OSRM trong lãnh thổ; nhãn nguồn tính | — | **chưa làm** |
| L-06 | proto decideMode (GIS FN-08 BR-04): mặc định đường bộ; gợi ý đường thủy khi xa hơn #27 và có kênh tiếp cận; điều phối đổi theo chặng | — | **chưa làm** |
| L-07 | proto GH-01: danh mục ghe có phiên bản, số hiệu chuẩn hóa duy nhất, loại `ghe`/`sa_lan`, tải > 0, đơn giá > 0 nếu có loại đơn giá | — | **chưa làm** |
| L-08 | proto GH-02: đăng kiểm hết hạn / sắp hết hạn (≤ 30 ngày) chỉ **cảnh báo**, không chặn xếp hàng; quét định kỳ | — | **chưa làm** |
| L-09 | proto GH-03 + plant weighing: phiếu cân đóng chuyến với tấn thật; cước = đơn giá thuê ghe (`per_ton`/`per_trip`/`per_ton_km`) nếu có, không thì đơn giá tham số (#22/#23); ghi `cost_source`; lệch > 5 % so với tấn ước → cờ | — | **chưa làm** |
| L-10 | Cảnh báo tấn ước > 1,1 × tải thực chở của ghe (#51) và ghe chưa cân sau 48 giờ | — | **chưa làm** |
| L-11 | Chứng từ chuyến ePOD / e-bill có người ký, thời điểm ký (tệp mã hóa ở kho đối tượng) | — | **chưa làm** |
| L-12 | Sự kiện `mkg.logistics.trip.completed` v1 (bản bù `supersedes_id`); tài xế/chủ ghe là `*_ref`, không dữ liệu cá nhân | — | **chưa làm** |
| L-13 | (tùy chọn) Telemetry phương tiện `ts_fleet` (ts 000004): chỉ ghi thêm, phân vùng tháng, R-07 | — | **chưa làm** |

## 6. `mkg/orders` (mkg.md §5.5)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| O-01 | proto CT-01: một hợp đồng mua rơm hiệu lực mỗi HTX mỗi kỳ; chồng lấn → từ chối | — | **chưa làm** |
| O-02 | proto CT-02/03: giá > 0, tấn cam kết > 0, kết thúc ≥ bắt đầu; cách tính `per_tonne` / `per_bale`; trần ẩm lưu, chưa cưỡng chế (C-4) | — | **chưa làm** |
| O-03 | Hợp đồng, đơn mua, đơn bán: người lập `sales_officer` ≠ người duyệt `sales_approver` ở app/ **và** CHECK DB (R-56) | — | **chưa làm** |
| O-04 | Đơn mua: tấn > 0, giá > 0; `draft → approved`, hủy có lý do | — | **chưa làm** |
| O-05 | Đơn bán: tấn > 0, đơn giá > 0; trạng thái chỉ tiến; thiếu tồn chỉ cảnh báo | — | **chưa làm** |
| O-06 | Giá / chiết khấu vượt ngưỡng (tham số có phiên bản, chờ khách) cần `sales_approver` | — | **chưa làm** |
| O-07 | Giao nhận: biên bản liên kết phiếu `CUSTOMER_IN` (khối lượng chuẩn từ measurement, không nhập tay); tuyến HUB/DIRECT; tranh chấp liên kết hồ sơ measurement | — | **chưa làm** |
| O-08 | Sự kiện `mkg.orders.delivery.accepted` v1 (bản bù); giá, khách không bao giờ qua van | — | **chưa làm** |
| O-09 | Tiền `numeric(18,2)`, đơn giá `numeric(18,4)` qua `shared/money` (R-10) | — | **chưa làm** |
| O-10 | `Contracts.ContractFor`: không có hợp đồng → `ErrNoContract` (quyết toán không đoán giá) | — | **chưa làm** |
| O-11 | Một nguồn khoản phải trả đơn mua (phiếu nhận đã duyệt) — không ghi trùng như prototype | — | **chưa làm** |

## 7. `mkg/costing` (mkg.md §5.6, mục 16)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| C-01 | Event sourcing mức B: sổ là nguồn; `POST /v1/costing/projections/rebuild` = dựng thẳng từ sổ (R-22) | `TestCostingPeriodLifecycle_C01_C09_C10_C16` (dựng lại = hiện hành, hai lần), `TestParamsRecomputeSupersedes_C02_C03`, `TestSettlementPerBaleApprove_PM02_PM04_C11_C13` (settlement_current) | **đạt** |
| C-02 | Mỗi phép tính ghim `rule_version`; tính lại dùng `recorded_at` (R-13), ghi kết quả mới `supersedes_id`, không sửa | `TestParamsRecomputeSupersedes_C02_C03` (ghim `rule_version` + `param_pins`, tính lại `as_of` trước quyết định → không đổi, tính lại → kết quả mới `supersedes_id`, kết quả cũ không đổi) | **đạt** |
| C-03 | Tham số theo **thời điểm sự việc**, có phiên bản, `cost_accountant` đặt ≠ `chief_accountant` duyệt | `TestParamsRecomputeSupersedes_C02_C03` (tự duyệt 403 + CHECK DB, tham số theo thời điểm sự việc), `TestParamsDefaultsProvisional_C03` | **đạt** |
| C-04 | Quy đổi cuộn → tấn (proto FM-09): tấn = cuộn × kg/cuộn / 1000; kg/cuộn học theo HTX → hệ thống → cấu hình → 20, ghi nguồn | `TestConvertRollsToTonnes_C04_C05`, `TestCostingPeriodLifecycle_C01_C09_C10_C16` (yield 22,000 kg/cuộn từ HUB_IN / số cuộn qua consumer) | **một phần** (mã: chuỗi dự phòng kg/cuộn học theo HTX → hệ thống → cấu hình → 20 ở fieldops `RollTotals`; costing ghi kg/cuộn THỰC từ HUB_IN, chưa đọc ước lượng) |
| C-05 | Quy đổi ẩm độ — cơ chế tham số, mặc định không quy đổi (C-4) | `TestConvertRollsToTonnes_C04_C05` (tham số `costing.moisture.base_pct` = none, từ chối giá trị khác) | **một phần** (chờ khách C-4: công thức quy đổi ẩm) |
| C-06 | Phân bổ chi phí đa chặng (gom, vận chuyển đường bộ/thủy, sấy/nén #55, lưu kho, bốc xếp) về lô theo khóa phân bổ có phiên bản (mặc định khối lượng) | `TestSplitAmountByMass_C06`, `TestCostingPeriodLifecycle_C01_C09_C10_C16` (cước 60/40 theo khối lượng), `TestParamsRecomputeSupersedes_C02_C03` (bản bù chuyến) | **đạt** (khóa phân bổ mặc định `mass` — chờ khách Q-2) |
| C-07 | Quy kết hao hụt về lô nguồn theo **tỷ lệ nguồn của lô** (`hub/port.LotOrigins`, Σ = 1); hao hụt dự kiến #19/#56 vs bất thường (từ đối soát measurement) | `TestAttributeLossBySourceProportions_C07`, `TestCostingPeriodLifecycle_C01_C09_C10_C16` (0,6/0,4 → 300/200 kg) | **đạt** |
| C-08 | proto buildCostChain: đ/t = tổng / tấn giao; tấn giao = 0 → "không xác định"; không gồm khấu hao; giá mua rơm tách nhóm | `TestCostPerTonne_C08`, `TestCostingPeriodLifecycle_C01_C09_C10_C16` (đ/t = 149 066,67; lô chưa giao → không xác định) | **đạt** |
| C-09 | Đóng kỳ theo mẫu mục 12: `POST …/periods/{id}/close` → 202 + job; ảnh chụp đầu vào bất biến có băm; worker-internal tính; giao lặp → một kết quả (R-61) | `TestCostingPeriodLifecycle_C01_C09_C10_C16` (202 + job, outbox, ảnh chụp đọc lại → cùng băm, giao lặp job → một kết quả), `TestPeriodCloseGoldenDeterministic_C09` (golden số + băm), `TestPeriodCloserNotAdjuster_C10` (idempotent POST close) | **đạt** |
| C-10 | Kỳ đã đóng không mở lại; điều chỉnh ở kỳ mở, trỏ kết quả cũ; `period_closer` ≠ người lập bút toán điều chỉnh (R-56 app/ + CHECK DB) | `TestCostingPeriodLifecycle_C01_C09_C10_C16` (không mở lại: 409 + trigger; điều chỉnh sau đóng ở kỳ mở), `TestPeriodCloserNotAdjuster_C10` (403 + trigger + CHECK DB) | **đạt** |
| C-11 | proto PM-01/02: một lệnh gom một quyết toán; theo cuộn → ngay; theo tấn → chờ cân đủ mọi ghe, làm mới sau mỗi lần cân tới khi duyệt | `TestSettlementPerBaleApprove_PM02_PM04_C11_C13`, `TestSettlementPerTonneAwaitsWeighing_PM02_C11` | **đạt** |
| C-12 | proto PM-03: không hợp đồng → không có số tiền, người xác nhận nhập giá | `TestSettlementNoContract_PM03`, `TestComputeSettlement_PM02_PM03` | **đạt** |
| C-13 | proto PM-04: duyệt (`chief_accountant` ≠ người lập) ghi **một** khoản phải trả, hạn 15 ngày (tạm), đóng băng; đã trả không hủy (quyết toán điều chỉnh); đã duyệt chỉ hoàn bằng bút toán đảo; `settlement.approved` | `TestSettlementPerBaleApprove_PM02_PM04_C11_C13` (tự duyệt 403 + CHECK DB, hạn 15 ngày, một `settlement.approved`, đóng băng, hủy sau duyệt/trả 409, quyết toán điều chỉnh) | **đạt** (hạn 15 ngày tạm, chờ khách Q-3) |
| C-14 | Chia doanh thu carbon 45 % / 70-15-15 (VVB nhận phần dư) — tham số có phiên bản, tạm chờ khách | `TestSplitCarbon_C14`, `TestCarbonRevenueSplit_C14` | **đạt** (tỷ lệ tạm, chờ khách Q-4) |
| C-15 | proto D03: khoản phải trả là nợ phải trả, không cộng vào chi phí | `TestSettlementPerBaleApprove_PM02_PM04_C11_C13` (không có bút toán chi phí lô từ quyết toán), `TestPeriodCloseGoldenDeterministic_C09` (straw_purchase × htx một lần) | **đạt** |
| C-16 | Sự kiện `mkg.costing.period.closed` v1; nhận `journal.pushed` → kỳ `posted`; consumer G3 (`weigh_ticket`, `reconciliation`, `roll_count`) và G4 (`trip`, `delivery`) idempotent | `TestCostingPeriodLifecycle_C01_C09_C10_C16` (period.closed, journal.pushed → posted, giao lặp mọi consumer), `TestCostingTenantLeakParallel` | **đạt** |

## 8. `mkg/acctbridge` (mkg.md §5.7, mục 16)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| B-01 | Chỉ bút toán **tổng hợp** theo kỳ, một chiều, tần suất tháng; không giao dịch chi tiết | — | **chưa làm** |
| B-02 | `period.closed` → job dựng bút toán (mẫu mục 12, ảnh chụp = `PeriodSummary` + ánh xạ tài khoản) — idempotent theo `(period_id, close_run_id)` | — | **chưa làm** |
| B-03 | Σ Nợ = Σ Có > 0; `source_hash` = `result_set_sha256` của kỳ; cùng đầu vào → cùng dòng | — | **chưa làm** |
| B-04 | `chief_accountant` duyệt ≠ người dựng (R-56 app/ + CHECK DB) | — | **chưa làm** |
| B-05 | Bảng ánh xạ (nhóm × chiều) → tài khoản có phiên bản, đề xuất ≠ duyệt | — | **chưa làm** |
| B-06 | `LedgerAdapter` bộ xuất tệp CSV + XML tất định byte-cho-byte, SHA-256, kho đối tượng trong lãnh thổ (RB-7) + bộ giả | — | **chưa làm** |
| B-07 | Đẩy idempotent (đẩy lại → cùng `ExternalRef`); lỗi tạm thời → thử lại giãn cách; lỗi vĩnh viễn → DLQ + cảnh báo (R-62) | — | **chưa làm** |
| B-08 | Sự kiện `mkg.acctbridge.journal.pushed` v1 | — | **chưa làm** |
| B-09 | Nhà cung cấp phần mềm kế toán thật: bộ điều hợp + kiểm giấy phép (RB-4) | — | **chưa làm** (chờ khách) |

## 9. Cổng kiến trúc G4 (RB, R-xx, vùng, hạ tầng)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| K-01 | RB-1: mọi sổ mới chỉ ghi thêm — sáu phép thử UPDATE/DELETE/TRUNCATE, `v_missing_immutability` rỗng, `verify_chain` | — | **chưa làm** |
| K-02 | RB-2: cách ly tenant mọi bảng G4, kể cả song song qua PgBouncer (R-32) | — | **chưa làm** |
| K-03 | R-10: `agnofloat` phủ toàn bộ bốn module (không float ở bất kỳ package con nào) | `TestNoFloat` (luật, G4-0) | **chưa làm** (chờ mã module) |
| K-04 | R-03/R-05: bốn module không import `platform/*`, mỗi module một vai chỉ quyền trên schema của nó, không đọc chéo schema | — | **chưa làm** |
| K-05 | ADR-0018: tám sự kiện nội bộ chỉ đi `KindInternal`; giao lặp → như một lần (R-61); DLQ (R-62) | `TestG4FrozenInternalContracts` (hợp đồng, G4-0) | **chưa làm** |
| K-06 | Không có dữ liệu G4 nào qua van (chuyến, cước, giá, giá thành, quyết toán, bút toán) — `contractcheck` | — | **chưa làm** |
| K-07 | ADR-0020: `waterway_routes` — kiểm cấp quyền, tenant bên cấp tường minh, sổ đọc trước khi trả, trường tối thiểu | `TestPartnerWaterwayRoute_ADR0020`, `TestShareGrantWaterwayRoutesKind_G4` (phía nền tảng, G4-0) | **chưa làm** (phía MKG) |
| K-08 | R-80/R-81 (ADR-0008): mỗi vùng một namespace, NetworkPolicy mặc định từ chối, chỉ mở cặp trong bảng mục 17; vùng dữ liệu không đi ra | — | **chưa làm** (hạ tầng) |
| K-09 | PKI nội bộ: mTLS giữa các dịch vụ trong cụm (vùng ứng dụng ↔ dữ liệu, ↔ định danh), xoay chứng thư | — | **chưa làm** (hạ tầng) |
| K-10 | Biên OT chỉ tới `ingest` qua VPN + mTLS (CA thiết bị riêng production — dòng mở của G3) | — | **chưa làm** (hạ tầng) |
| K-11 | Mục 18: pod non-root, bỏ capability, seccomp, hệ tệp gốc chỉ đọc; admission từ chối image chưa ký (mục 20) | — | **chưa làm** (hạ tầng) |
| K-12 | Mục 19: cảnh báo độ trễ bảng chiếu so với sổ, DLQ, job đóng kỳ / đẩy bút toán lỗi | — | **chưa làm** |
| K-13 | Mục 22: sao lưu cụm mkg G4 (pgBackRest trong lãnh thổ), credential ghi ≠ phục hồi | — | **chưa làm** (hạ tầng) |
| K-14 | R-95: bộ sinh dữ liệu tổng hợp mở rộng cho chuyến, đơn, kỳ, quyết toán (`cmd/seed-synthetic -plan mkg`) | — | **chưa làm** |

## 10. Client và kịch bản đầu–cuối

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| W-01 | Web nội bộ MKG (`apps/web-internal`): chuyến + danh mục ghe, đơn mua/bán, hợp đồng, giao nhận, kỳ + đóng kỳ, quyết toán, bút toán tổng hợp; màn duyệt tách vai (R-56) | — | **chưa làm** |
| W-02 | App tài xế / chủ ghe: mốc chuyến (ngoại tuyến như app hiện trường MKG) | — | **chưa làm** |
| W-03 | Kịch bản đầu–cuối `TestG4FieldToLedger` (mục 2) | — | **chưa làm** |
| W-04 | CI: job `test` qua PgBouncer có vai G4; `compose-smoke` có consumer G4 trên `mkg.internal` | userlist CI/Compose có vai G4 (G4-0) | **chưa làm** (CI chưa chạy được — GitHub Actions bị chặn) |

## 11. Câu hỏi khách

| Mã | Câu hỏi | Chặn | Trạng thái |
|---|---|---|---|
| Q-1 | Nhà cung cấp phần mềm kế toán pháp định, chế độ kế toán (TT200/TT133), hệ thống tài khoản, định dạng nhận | B-05, B-09 | **chưa làm** (chờ khách) |
| Q-2 | Khóa phân bổ chi phí đa chặng (khối lượng, tấn·km, doanh thu…) | C-06 | **chưa làm** (chờ khách) |
| Q-3 | Công thức giá quyết toán HTX; thời hạn thanh toán (tạm 15 ngày) | C-11..C-13 | **chưa làm** (chờ khách) |
| Q-4 | Tỷ lệ chia doanh thu carbon (tạm 45 % / 70-15-15) | C-14 | **chưa làm** (chờ khách) |
| Q-5 | Ngưỡng duyệt giá / chiết khấu đơn bán | O-06 | **chưa làm** (chờ khách) |
| Q-6 | Quy đổi / khấu trừ ẩm độ, trạng thái ẩm khi cân (C-4) | C-05, O-02 | **chưa làm** (chờ khách) |
| Q-7 | Đơn giá cước và ngưỡng chọn đường thủy (#22, #23, #27), tải thực chở của ghe (#51) | L-06, L-09, L-10 | **chưa làm** (chờ khách) |

## 12. Giới hạn đã biết của bộ kiểm thử

Điền khi rà nghiệm thu. Đã biết từ G4-0: máy dev không có Docker, RabbitMQ thật, Kubernetes — các dòng hạ tầng (A-1, K-08..K-11, K-13) và diễn tập tải chỉ kiểm được trên cụm.
