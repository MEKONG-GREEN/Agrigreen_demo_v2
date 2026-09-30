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
| 3. Phụ lục A (G4) | 5 | 1 | 1 | 3 | 0 |
| 4. Chuyển từ G3 | 2 | 1 | 1 | 0 | 0 |
| 5. `mkg/logistics` | 16 | 14 | 0 | 2 | 0 |
| 6. `mkg/orders` | 12 | 12 | 0 | 0 | 0 |
| 7. `mkg/costing` | 16 | 14 | 2 | 0 | 0 |
| 8. `mkg/acctbridge` | 9 | 8 | 0 | 1 | 0 |
| 9. Cổng kiến trúc G4 (RB, R-xx, vùng, hạ tầng) | 14 | 0 | 0 | 14 | 0 |
| 10. Client và kịch bản đầu–cuối | 4 | 1 | 1 | 2 | 0 |
| 11. Câu hỏi khách | 7 | 0 | 0 | 7 | 0 |
| **Tổng** | **85** | **51** | **5** | **29** | **0** |

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
| A-3 | Tách đơn vị triển khai bằng cấu hình, không sửa mã (RB-5): `api-internal` `AG_MKG_JOBS`, `worker-internal` chọn consumer (`AG_WORKER_INTERNAL_CONSUMERS`), `api-platform`/`worker` tương tự | `TestSelectConsumersByConfig_A3` (`AG_WORKER_INTERNAL_CONSUMERS`: all / danh sách, tên lạ → lỗi khởi động), `TestParseDefaultIsAllClusters`, `TestParseKeepsCanonicalOrder`, `TestParseRejectsUnknownAndDuplicate`, `TestBaseURLOverride` (`AG_CLUSTERS` + `AG_<CỤM>_DB_URL` cho migrate / outbox-publisher / job worker), `TestRolesForSelectedClusters_G4`, `TestBootstrapClustersOnlySelected_G4` (`migrate bootstrap` theo `AG_CLUSTERS`), `TestHealthAndReadiness_G4` (`AG_HEALTH_ADDR` của worker / outbox-publisher); chart dựng bản theo vùng chỉ bằng values (`deploy/k8s/check.ps1`: helm lint + kubeconform + `check_flows.py` 3 240 kiểm đạt) | **đạt** (`api-internal` `AG_MKG_JOBS`, `worker` `-modules`/`-jobs` đã có từ G1–G3) |
| A-4 | Dựng lại bảng chiếu từ sổ trên **bản sao dữ liệu thật** ẩn danh hóa (R-22, mục 20–21): mọi module có `projections/rebuild` | `cmd/rebuilddrill` `TestRebuildDrillChecksumsBeforeAfter_R22` (băm mọi `*_current` + `lot_balance` trước / sau `Module.RebuildTenant` của hub, measurement, logistics, orders, costing, acctbridge; bảng chiếu bị sửa tay → báo LỆCH, lần sau khớp), `TestRebuildDrillRequiresConfirmCopy`; quy trình [rebuild-projection.md "Diễn tập G4"](../runbooks/rebuild-projection.md#diễn-tập-g4) | **một phần** — **hạ tầng**: chưa chạy trên bản sao dữ liệu thật (staging / môi trường phục hồi); **mã**: `mkg/fieldops` chưa có hàm dựng lại bảng chiếu (công cụ báo "chưa có"); cụm pf ngoài công cụ (R-03) |
| A-5 | Diễn tập phục hồi hàng quý trên môi trường sạch (mục 22): số dòng, băm tham số, `verify_chain`, mẫu tính lại, checksum kho đối tượng — mở rộng `cmd/drill` cho cụm mkg G4 (một kỳ costing mẫu tính lại) | `cmd/drill` (G1, chưa có phần G4) | **chưa làm** |

## 4. Chuyển từ G3

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| T-1 | proto FM-05: mỗi lượt xuống ghe sinh một chuyến vận tải đường thủy (số hiệu ghe, tấn ước theo cuộn; lỗi TMS / không xác định đích → chỉ cảnh báo, đếm cuộn vẫn ghi; thông báo hàng đến mang mã chuyến) | `TestLogisticsT1FM05TripFromBoatLoad` (một chuyến planned / lượt xuống ghe, tấn ước = cuộn × kg/cuộn, giao lặp, bản bù đếm cuộn), `TestLogisticsT1FM05VesselCodeAssignsVehicle` (số hiệu ghe khớp danh mục → gán phương tiện + cảnh báo quá tải; ngoài danh mục → `vessel_unregistered` + `no_vehicle`; không số hiệu → `no_vehicle`), `TestRollCountVesselCodeOptional_FM05` (fieldops nhận `vessel_code` qua POST roll-counts và sync push, chuẩn hóa GH-01, 422 ngoài `boat_load`, bản bù giữ số hiệu, payload `roll_count.recorded` v1 tùy chọn), `TestLogisticsL10OverloadAndUnweighedAlert`, `TestG4LogisticsConsumersRegistered`; phía nền tảng `TestPartnerWaterwayRoute_ADR0020` | **một phần** (mã): lỗi lập chuyến không chặn đếm cuộn (consumer bất đồng bộ); thiếu đích → chuyến cờ `no_route` thay vì không lập (quyết định G4); **ô nhập số hiệu ghe trên app Flutter chưa có** (API đã nhận — việc agent app); **chưa có** thông báo hàng đến (`cho_den → da_den`) mang mã chuyến cho hub/kho |
| T-2 | proto phiếu nhận hàng ↔ đơn mua: lệch > 3 % → cần biên bản chênh lệch + ghi chú kiểm toán, **không chặn** | `TestReceiptVsPOVariancePerTonne_T2` (HUB_IN qua consumer, trước/sau phiếu nhận), `TestReceiptVsPOVariancePerBale_T2` (theo cuộn, bù có lý do), `TestCheckReceiptThresholdBothUnits_T2`, `TestReceiptVarianceParamVersioned` (ngưỡng có phiên bản) | **đạt** (ngưỡng 3 % tạm, chờ khách) |

## 5. `mkg/logistics` (mkg.md §5.4)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| L-01 | Chuyến, chặng, điểm dừng là sổ (`trip:<id>`) + bảng chiếu `trip_current` cùng giao dịch; dựng lại được | `TestLogisticsL01ProjectionRebuildEqualsLive` (`POST /v1/logistics/projections/rebuild` = bảng chiếu sống, gồm bản bù, hủy, kế hoạch chặng lần 2), `TestLogisticsTenantLeakParallel` | **đạt** |
| L-02 | Trạng thái chuyến chỉ tiến `planned → departed → arrived → completed`, hủy có lý do (Go cưỡng chế — prototype không) | `TestLogisticsL02ForwardOnlyStatus` (409 + trigger DB `trip_forward_only`), `TestLogisticsL02CanTransitionForwardOnly` | **đạt** |
| L-03 | Kết quả định tuyến đã dùng lưu nguyên văn + SHA-256 vào chặng; đọc chặng cũ không gọi lại nền tảng; ghim phiên bản mạng (mục 16) | `TestLogisticsL03RouteSnapshotStoredWithHash` (CHECK DB `raw_sha256 = sha256(raw)`), `TestLogisticsWaterwayHTTPPartnerClient` | **đạt** |
| L-04 | Định tuyến đường thủy chỉ qua bề mặt đối tác + cấp quyền `waterway_routes` (ADR-0020); thiếu cấp quyền / nền tảng lỗi → km tay có lý do hoặc Haversine, không chặn | `TestLogisticsL04FallbackWhenRoutingDown`, `TestLogisticsWaterwayHTTPPartnerClient` (403 → `ErrRouteGrantMissing`) | **đạt** |
| L-05 | Đường bộ: Haversine × hệ số #10 (1,3) tới khi có OSRM trong lãnh thổ; nhãn nguồn tính | `TestLogisticsL05L06RoadHaversineAndModeSuggestion`, `TestLogisticsL05HaversineDecimal` (không float) | **đạt** |
| L-06 | proto decideMode (GIS FN-08 BR-04): mặc định đường bộ; gợi ý đường thủy khi xa hơn #27 và có kênh tiếp cận; điều phối đổi theo chặng | `TestLogisticsL05L06RoadHaversineAndModeSuggestion` (cờ `waterway_suggested`; #27 = 120 km tạm, chờ khách) | **đạt** |
| L-07 | proto GH-01: danh mục ghe có phiên bản, số hiệu chuẩn hóa duy nhất, loại `ghe`/`sa_lan`, tải > 0, đơn giá > 0 nếu có loại đơn giá | `TestLogisticsL07L08VesselRegisterAndExpiry`, `TestLogisticsL07VesselValidation` | **đạt** |
| L-08 | proto GH-02: đăng kiểm hết hạn / sắp hết hạn (≤ 30 ngày) chỉ **cảnh báo**, không chặn xếp hàng; quét định kỳ | `TestLogisticsL07L08VesselRegisterAndExpiry`, `TestLogisticsL08ExpiryStatus`, `TestLogisticsL08AlertJobNotices` (job `logistics_alerts`: một thông báo mỗi điều kiện, không gửi lại ở lượt sau, metric `ag_logistics_alerts_open{kind}`, `GET /v1/logistics/alert-notices` chỉ điều phối, tenant qua `ag_outbox_publisher`, `alert_notice` chỉ ghi thêm, RB-2); luật `deploy/observability/prometheus/agrigreen-mkg-logistics.rules.yml` (+ bản chart, `check_flows.py`) | **đạt** (promtool chưa chạy cục bộ — không Docker; đẩy tới điện thoại cần kênh thông báo vùng mkg — sau G4) |
| L-09 | proto GH-03 + plant weighing: phiếu cân đóng chuyến với tấn thật; cước = đơn giá thuê ghe (`per_ton`/`per_trip`/`per_ton_km`) nếu có, không thì đơn giá tham số (#22/#23); ghi `cost_source`; lệch > 5 % so với tấn ước → cờ | `TestLogisticsInternalEventTripCompletedOnWeighTicket`, `TestLogisticsCorrectionByCompensatingRecord` (`param_rate`, `manual` có chứng từ), `TestLogisticsL09Costs` | **đạt** |
| L-10 | Cảnh báo tấn ước > 1,1 × tải thực chở của ghe (#51) và ghe chưa cân sau 48 giờ | `TestLogisticsL10OverloadAndUnweighedAlert`, `TestLogisticsL10OverloadWarning`, `TestLogisticsL08AlertJobNotices` (nhắc ghe chưa cân > 48 giờ qua job định kỳ + Alertmanager) | **đạt** |
| L-11 | Chứng từ chuyến ePOD / e-bill có người ký, thời điểm ký (tệp mã hóa ở kho đối tượng) | — | **chưa làm** (mã: sổ chứng từ + `shared/objstore`; migration 000015 chưa có bảng) |
| L-12 | Sự kiện `mkg.logistics.trip.completed` v1 (bản bù `supersedes_id`); tài xế/chủ ghe là `*_ref`, không dữ liệu cá nhân | `TestLogisticsInternalEventTripCompletedOnWeighTicket` (outbox `kind = internal`, chặng + ảnh chụp + phiên bản mạng, phiếu bù → `supersedes_id`), `TestLogisticsTripsPortCompletedAsOf` (`port.Trips.Completed` với `asOf`) | **đạt** |
| L-13 | (tùy chọn) Telemetry phương tiện `ts_fleet` (ts 000004): chỉ ghi thêm, phân vùng tháng, R-07 | — | **chưa làm** (tùy chọn, không làm ở G4 logistics) |
| L-14 | App tài xế (G4 tích hợp): "chuyến của tôi" (`driver_ref` = `sub` của token), chỉ tài xế được giao / điều phối ghi mốc, tài xế không thấy cước | `TestLogisticsDriverScopeMilestonesAndDelta_G4` (lọc tự động với người chỉ có vai driver, `assigned_to_me`, 404 chuyến người khác, 403 `logistics.not_assigned_driver`, cước chuyến + chặng vắng với tài xế, điều phối thấy), `TestLogisticsL02ForwardOnlyStatus` (tài xế được giao) | **đạt** |
| L-15 | Mốc chuyến không GPS: `lat`/`lng` NULL (không bao giờ 0,0), `accuracy_m` tùy chọn — tương thích app gửi `{kind, event_time, lat, lng}` | `TestLogisticsDriverScopeMilestonesAndDelta_G4` (không GPS → NULL, 0,0 → NULL, chỉ lat → 422, accuracy_m lưu sổ `trip_milestone_fix` và đọc ở `/events`); mkg 000020 nới CHECK `trip_event` | **đạt** |
| L-16 | Đồng bộ chuyến theo delta cho app (`updated_since`, con trỏ theo `recorded_at`) | `TestLogisticsDriverScopeMilestonesAndDelta_G4` (delta chỉ chuyến có sự kiện mới, gồm chuyến của tài xế, phân trang `limit=1` + `next_cursor`, 422 mốc sai) | **đạt** |

## 6. `mkg/orders` (mkg.md §5.5)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| O-01 | proto CT-01: một hợp đồng mua rơm hiệu lực mỗi HTX mỗi kỳ; chồng lấn → từ chối | `TestContractLifecycle_CT01_CT02_SoD` (409 `orders.contract_overlap` khi lập và kiểm lại khi duyệt), `TestValidateContract_CT02_CT03` (chồng lấn theo ngày, hai đầu tính) | **đạt** |
| O-02 | proto CT-02/03: giá > 0, tấn cam kết > 0, kết thúc ≥ bắt đầu; cách tính `per_tonne` / `per_bale`; trần ẩm lưu, chưa cưỡng chế (C-4) | `TestContractLifecycle_CT01_CT02_SoD`, `TestValidateContract_CT02_CT03` (+ CHECK mkg 000016); hợp đồng có phiên bản (sửa = phiên bản mới, duyệt → cũ `superseded`) | **đạt** (quy đổi ẩm chờ khách — Q-6) |
| O-03 | Hợp đồng, đơn mua, đơn bán: người lập `sales_officer` ≠ người duyệt `sales_approver` ở app/ **và** CHECK DB (R-56) | `TestContractLifecycle_CT01_CT02_SoD` (403 + CHECK 23514), `TestPurchaseOrderRules_O04`, `TestSalesOrderPriceApproval_O06` (403 + CHECK 23514), `TestReceiptVarianceParamVersioned` (tham số); phiếu nhận: CHECK `receipt_event` | **đạt** |
| O-04 | Đơn mua: tấn > 0, giá > 0; `draft → approved`, hủy có lý do | `TestPurchaseOrderRules_O04` | **đạt** |
| O-05 | Đơn bán: tấn > 0, đơn giá > 0; trạng thái chỉ tiến; thiếu tồn chỉ cảnh báo | `TestSalesOrderForwardOnly_O05`, `TestSalesOrderForwardOnly_O05Domain`, `TestOrdersStockShortageWarningFromHub_O05` (`cmd/api-internal/assembly`: orders nối `hub/port.Stock` của mkg/hub — đủ tồn không cảnh báo; vượt tồn → `warnings: [orders.stock_shortage]` + `stock_check.shortage_kg`, lập và xác nhận vẫn được; dạng hàng không tồn; RB-2 tồn tenant khác) | **đạt** (`orders.sales.stock_shortage_blocking` = 0, tạm chờ khách) |
| O-06 | Giá / chiết khấu vượt ngưỡng (tham số có phiên bản, chờ khách) cần `sales_approver` | `TestSalesOrderPriceApproval_O06` (403 `orders.price_approval_required`, `self_approval_forbidden`), `TestCheckPriceApproval_O06`; ngưỡng ghim vào `price_rule` | **đạt** (ngưỡng tạm 0 — chờ khách Q-5) |
| O-07 | Giao nhận: biên bản liên kết phiếu `CUSTOMER_IN` (khối lượng chuẩn từ measurement, không nhập tay); tuyến HUB/DIRECT; tranh chấp liên kết hồ sơ measurement | `TestDeliveryCustomerInAccepted_O07_O08` (consumer `mkg.orders.weigh_ticket` gắn phiếu; 409 `orders.customer_in_missing`; CHECK `accepted_kg = ticket_kg`; `dispute_ref`), `TestDeliveryCompensatingCorrection_RB1` (DIRECT) | **đạt** |
| O-08 | Sự kiện `mkg.orders.delivery.accepted` v1 (bản bù); giá, khách không bao giờ qua van | `TestDeliveryCustomerInAccepted_O07_O08` (`KindInternal`, đúng trường hợp đồng), `TestDeliveryCompensatingCorrection_RB1` (phiếu bù → chấp nhận bù tự động + `supersedes_id`; `Deliveries.Accepted` theo `asOf`), `contractcheck` | **đạt** |
| O-09 | Tiền `numeric(18,2)`, đơn giá `numeric(18,4)` qua `shared/money` (R-10) | `TestLineAmountDecimalRounding_O09`, `TestNoFloat` (archlint phủ `orders`) | **đạt** |
| O-10 | `Contracts.ContractFor`: không có hợp đồng → `ErrNoContract` (quyết toán không đoán giá) | `TestContractForErrNoContract_O10` (+ đơn mua rơm không hợp đồng không giá → 422 `orders.price_required`) | **đạt** |
| O-11 | Một nguồn khoản phải trả đơn mua (phiếu nhận đã duyệt) — không ghi trùng như prototype | `TestOtherPurchasePayableSingleSource_O11` (đúng một khoản, duyệt lặp không ghi thêm), `TestReceiptVsPOVariancePerTonne_T2` (rơm: 0 khoản — chỉ từ quyết toán costing) | **đạt** |
| O-12 | RB-1: phiếu HUB_IN bù ở measurement **sau khi** phiếu nhận đã duyệt → bản bù phiếu nhận + bản bù khoản phải trả (không sửa bản ghi đã có) | `TestHubInCorrectionAfterReceiptApproved_RB1` (phiếu nhận `corrected` vẫn `approved`, người duyệt giữ nguyên, kiểm ↔ đơn mua lại; khoản phải trả bù `supersedes_id` = khoản cũ, số tiền theo tấn mới, giữ hạn; khoản gốc nguyên; giao lặp không ghi thêm; rơm: không phải trả ở orders) | **đạt** |

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
| B-01 | Chỉ bút toán **tổng hợp** theo kỳ, một chiều, tần suất tháng; không giao dịch chi tiết | `TestAcctbridgeFlow_B01_B08`, `TestBuildBalanced_B03` (chỉ tổng theo tài khoản × nhóm × chiều; không sự kiện nền tảng) | **đạt** |
| B-02 | `period.closed` → job dựng bút toán (mẫu mục 12, ảnh chụp = `PeriodSummary` + ánh xạ tài khoản) — idempotent theo `(period_id, close_run_id)` | `TestAcctbridgeFlow_B01_B08` (giao lặp + sự kiện khác cùng kỳ → một job), `TestAcctbridgeBuildFailures_B02` (thiếu ánh xạ, băm lệch, kỳ chưa đóng, costing chưa nối → giao lại), `TestWorkerInternalCostingToAcctbridgeWiring_G4` (đầu–cuối qua sổ `Consumers` của worker-internal: job đóng kỳ costing với cổng orders/logistics/hub thật → `period.closed` → acctbridge đọc `ClosedPeriods` trên pool `costing_app` → bút toán `source_hash` = `result_set_sha256`), `TestWorkerInternalConsumersBuildWithDeps_G4` | **đạt** |
| B-03 | Σ Nợ = Σ Có > 0; `source_hash` = `result_set_sha256` của kỳ; cùng đầu vào → cùng dòng | `TestBuildBalanced_B03`, `TestBuildDeterministic_B03`, `TestAcctbridgeSourceHashStable_B03` (+ CHECK DB `total_debit = total_credit > 0`) | **đạt** |
| B-04 | `chief_accountant` duyệt ≠ người dựng (R-56 app/ + CHECK DB) | `TestAcctbridgeSoD_B04` (403 app/ + CHECK sổ và bảng chiếu, MFA bề mặt quản trị) | **đạt** |
| B-05 | Bảng ánh xạ (nhóm × chiều) → tài khoản có phiên bản, đề xuất ≠ duyệt | `TestAcctbridgeMappingVersions_B05`, `TestValidateRules_B05`, `TestAcctbridgeSoD_B04`, `TestAcctbridgeCorrectionBeforePushOnly` (dựng lại theo phiên bản mới). Mặc định VAS kiểu TT200 **tạm, chờ khách** | **đạt** |
| B-06 | `LedgerAdapter` bộ xuất tệp CSV + XML tất định byte-cho-byte, SHA-256, kho đối tượng trong lãnh thổ (RB-7) + bộ giả | `TestExportGolden_B06` (golden), `TestFileExportIdempotent_B06`, `TestFakeAndUnconfigured`, `TestAcctbridgeFlow_B01_B08` (tệp tải về = tệp ở kho); hợp đồng: [acctbridge-adapter.md](../dev/acctbridge-adapter.md) | **đạt** |
| B-07 | Đẩy idempotent (đẩy lại → cùng `ExternalRef`); lỗi tạm thời → thử lại giãn cách; lỗi vĩnh viễn → DLQ + cảnh báo (R-62) | `TestAcctbridgePushIdempotent_B07` (giao dịch hỏng sau khi đích nhận → một `ExternalRef`), `TestAcctbridgeRetryVersusDLQ_R62`, `TestBackoff_R62`; luật `deploy/observability/prometheus/agrigreen-mkg-acctbridge.rules.yml` (promtool chưa chạy cục bộ — không có Docker) | **đạt** |
| B-08 | Sự kiện `mkg.acctbridge.journal.pushed` v1 | `TestAcctbridgeFlow_B01_B08` (outbox `internal`, payload đúng hợp đồng), `TestG4FrozenInternalContracts` | **đạt** |
| B-09 | Nhà cung cấp phần mềm kế toán thật: bộ điều hợp + kiểm giấy phép (RB-4) | — | **chưa làm** (chờ khách) |

## 9. Cổng kiến trúc G4 (RB, R-xx, vùng, hạ tầng)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| K-01 | RB-1: mọi sổ mới chỉ ghi thêm — sáu phép thử UPDATE/DELETE/TRUNCATE, `v_missing_immutability` rỗng, `verify_chain` | orders: `TestOrdersLedgersAppendOnlySixWays_R20` (8 sổ, `verify_chain`), `TestCatalogGates` | **chưa làm** (còn logistics, costing, acctbridge) |
| K-02 | RB-2: cách ly tenant mọi bảng G4, kể cả song song qua PgBouncer (R-32) | orders: `TestOrdersTenantLeakParallel` (15 bảng) | **chưa làm** (còn logistics, costing, acctbridge) |
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
| W-01 | Web nội bộ MKG (`apps/web-internal`): chuyến + danh mục ghe, đơn mua/bán, hợp đồng, giao nhận, kỳ + đóng kỳ, quyết toán, bút toán tổng hợp; màn duyệt tách vai (R-56) | WEB-E2E `logistics.spec.ts` (6: chặng đường thuỷ + ảnh chụp băm khớp nguyên văn, km tay cần lý do, huỷ có lý do, hoàn tất cờ lệch cân + bản bù, tài xế chỉ ghi mốc, danh mục ghe chuẩn hoá + phiên bản, cảnh báo 48 giờ / đăng kiểm, tham số dispatcher ≠ chief_accountant), `orders.spec.ts` (7: hợp đồng tự duyệt bị chặn + chồng lấn CT-01, phiếu nhận lệch +7,56 % cần biên bản không chặn, một khoản phải trả, đơn bán `orders.price_approval_required` + tự duyệt, giao nhận theo CUSTOMER_IN / `orders.customer_in_missing`, tham số, phân quyền), `costing.spec.ts` (6: đóng kỳ R-56 + `period_not_ended` + 202 → job → đã đóng, đ/t + điều chỉnh + tính lại phân bổ, quyết toán theo cuộn tự duyệt bị chặn → duyệt → đã trả, không hợp đồng → nhập giá, theo tấn chờ cân `settlement_not_ready`, chia carbon, tham số bề mặt quản trị), `acctbridge.spec.ts` (4: đóng kỳ → bút toán cân, người đóng kỳ không tự duyệt → người khác duyệt → đẩy → kỳ posted, DLQ → đẩy lại, tải CSV/XML kiểm X-Content-SHA256, ánh xạ đề xuất ≠ duyệt + dựng lại → bản cũ bị thay, phân quyền); Vitest `src/lib/g4.test.ts` (tiền VND BigInt, SHA-256, trạng thái chỉ tiến, mọi mã `logistics.*`/`orders.*`/`costing.*`/`acctbridge.*` của docs/errors.md có nhãn tiếng Việt), `src/api/mock/g4mock.test.ts`, `src/components/g4kit.test.tsx` | **đạt** (trên backend giả lập theo hợp đồng `mkg-internal.yaml`; gọi api-internal thật còn chờ CORS — khoảng trống ghi ở `apps/web-internal/README.md`) |
| W-02 | App tài xế / chủ ghe: mốc chuyến (ngoại tuyến như app hiện trường MKG) | `apps/mkg-field-app`, vai `driver`: `trips_domain_test.dart` (máy trạng thái chỉ tiến `nextMilestone`/`checkMilestone`/`effectiveStatus` — phản chiếu L-02; bản lưu không cước/lô), `trip_service_test.dart` (chỉ vai driver mới kéo `GET /v1/logistics/trips` theo con trỏ, con trỏ hỏng → đọc lại; lọc `driver_ref` = `sub`; mốc ngoại tuyến vào hàng `api_action` với `Idempotency-Key` `act:<id>`, `event_time` đồng hồ máy, GPS + sai số; phát lại khi mất phản hồi → một bản ghi; 409 `logistics.trip_transition` / `logistics.trip_closed` → xung đột, không thử lại; thiếu GPS → không xếp hàng), `http_logistics_test.dart` (HTTP thật của lõi + MockClient: header, thân đúng hợp đồng, 503 thử lại, mất mạng, problem+json 409), `trip_screens_test.dart` (danh sách / chi tiết: chặng, điểm đến, tấn ước; chỉ nút mốc kế tiếp; xung đột hiển thị ở chi tiết, danh sách, hàng đợi; màn 320×568) | **một phần** (app xong, kiểm thử máy ảo/CI chưa chạy — CI bị chặn). Khoảng trống phía máy chủ: chưa có lọc "chuyến của tôi" (app lọc `driver_ref` = `sub` — cần chốt `driver_ref` là `sub` Keycloak hay ref vault RB-3), máy chủ không kiểm người ghi mốc là tài xế được gán, `GET /v1/logistics/trips` trả cả cước cho vai driver, `lat`/`lng` bắt buộc nhưng thiếu thì thành (0, 0) và không có trường sai số GPS, không có con trỏ thay đổi (`updated_since`) |
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
