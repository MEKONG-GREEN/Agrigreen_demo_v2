# Truy vết và khoảng cách: BRD KN Phòng Trồng trọt v7.7 ↔ mã hiện tại

> Đầu vào: [kn-trong-trot-v7.7.md](kn-trong-trot-v7.7.md) (tóm tắt BRD v7.7), mã ở nhánh `kn-0` (= `sub/kn-v77` = main @ 0819839). Mã hiện tại xây theo BRD App Khuyến nông v1.0 (FR-01…FR-32), Backlog US v1.0 và prototype `App_Khuyen_Nong_v5.0.html` (nghiệm thu: `docs/uat/g1-acceptance.md`, `g2-acceptance.md`).
> Kế hoạch thực hiện: [kn-v7.7-plan.md](kn-v7.7-plan.md).

> **Cập nhật W11.01 (10/10/2026).** Cột "Mã hiện tại" của ma trận dưới đây là ảnh chụp trước P1–P8 và giữ nguyên làm lịch sử. Từ đó mã đã đổi chỗ như sau (trạng thái nghiệm thu hiện hành ở [kn-v7.7-acceptance.md](../uat/kn-v7.7-acceptance.md), đối chiếu đầy đủ ở [doi-chieu-brd-kn-2026-10.md](../kn/doi-chieu-brd-kn-2026-10.md) Phụ lục C):
> - **W3.3 (pf 000131–000133):** HTX, nông hộ, thửa, vụ, nhật ký canh tác chuyển từ `party` (cooperative/household), `geo` (plot), `cropcycle` sang module `pf/cooperative` (`internal/platform/cooperative/{member,plot,season}`, schema `pf_coop`). Dòng UR-XA-16, UR-XA-18, UR-KT-01 và mục 3 (HTX, bản đồ) đọc theo vị trí mới. `/v1/kn/cooperatives*`, `/v1/kn/households*` vẫn ở `party/handler/kn_collect.go` (qua `cooperativeport`); `/v1/kn/plots*` ở `cooperative/plot/handler/kn.go`.
> - **W6.2 (pf 000121–000122):** chốt/mở lại kỳ KN ghi song song vào `pf/forms` (`cap/forms`); vòng đời báo cáo KN vẫn chạy trên `extension/reportcycle` đến W11.04.
> - **Menu mobile** từ `GET /v1/menu` và chọn ngữ cảnh trên mobile đã có (P7, `apps/field-app/lib/application/kn_menu_service.dart`, `ui/kn/kn_context_picker_screen.dart`).
> - **Còn thiếu, bản đầu không ghi:** trang web "Báo cáo đột xuất" (`/kn/bao-cao/dot-xuat` có trong menu nhưng rơi vào `ComingSoon`), làm ở gói W11.02.
> - Mã FN/UR trùng giữa các BRD KN: ma trận nghiệm thu dùng tiền tố module (`TT-`, `HC-`, `CN-`, `KH-`, `BG-`); mã không tiền tố trong tài liệu này là `TT-`.

**Trạng thái:** **Đáp ứng** = dùng được gần như nguyên trạng · **Cần sửa** = đã có nền nhưng phải đổi hành vi, mô hình hoặc giao diện · **Mới** = chưa có gì đáng kể · **Bỏ khỏi phạm vi module** = v7.7 chuyển sang module/hệ thống khác.

## 1. Tóm tắt khác biệt lớn nhất

| # | v7.7 đòi hỏi | Hiện tại | Hệ quả |
|---|---|---|---|
| D1 | Một tài khoản **nhiều vai trò × Nút × phòng ban × xã phụ trách**; phiên theo 1 vai trò + 1 phòng ban, đổi không đăng nhập lại | Mỗi người **một `org_ref`** (thuộc tính Keycloak) + realm roles; phạm vi suy từ loại tổ chức (`party/app/registry.go Scope()`) | Mô hình phân quyền mới: bảng gán vai trò trong `pf_accounts`, ngữ cảnh phiên, `Principal`/`Scope` mới (P1) |
| D2 | **Phòng ban là module**, bật bằng cấu hình; mọi dữ liệu nghiệp vụ lọc theo phòng ban | Không có khái niệm phòng ban | Danh mục phòng ban + gắn `department_code` vào mọi bản ghi KN mới (P2) |
| D3 | **14 mẫu báo cáo đúng Excel** do Admin thiết kế (cột/dòng/gộp ô/công thức, phiên bản) | `field_report` cố định 2 loại (`sowing_progress`, `growth_pest`), kỳ `YYYY-MM`; `reports.template_version` chỉ là bố cục xuất | Engine mẫu báo cáo + trình thiết kế + công thức tương thích Excel (P3) |
| D4 | Phát hành biểu → **công việc theo xã × biểu × kỳ**, theo dõi nộp, nhắc, rà soát, yêu cầu điều chỉnh, **chốt/mở lại kỳ**, tổng hợp có nút “Cập nhật” + nhật ký ô | Chỉ có `period_close` (không mở lại), `program_report` duyệt; không có công việc nộp, nhắc, tổng hợp | Vòng đời báo cáo mới (P4) |
| D5 | Báo cáo Chi cục: Excel/PDF/**Google Sheet**, gửi email **kèm tệp** tới địa chỉ ngoài | `reports/mail/smtp.go` gửi tới `actor_ref` (≤ 50), **không đính kèm**; không có Google | Bộ gửi mới + vấn đề RB-7 (P4, §4) |
| D6 | Lịch công tác tuần đăng ký/duyệt | Không có (chỉ `duty_status`, `duty_shift` – trực) | Mới (P5) |
| D7 | Giao diện theo Design Sample: sidebar 3 nhóm, Noto Serif + Roboto, List/Detail(Log)/Dashboard | Sidebar 6 nhóm đánh số theo prototype v5.0, font Be Vietnam Pro, mẫu trang một bảng + Dialog | Dựng lại khung web-ops và các màn (P6) |
| D8 | Menu Phòng Trồng trọt chỉ 25 FN; thư viện, GIS, MRV, chỉ đạo, danh bạ, đề án 1 triệu ha… ra khỏi module | Đều nằm trong menu KN | Ẩn khỏi menu Phòng Trồng trọt, giữ mã cho module khác (§3) |
| D9 | Đăng nhập email **hoặc SĐT**; khóa **tạm** 15 phút sau 5 lần sai, hiện số lần còn lại; phiên Web 4 giờ không thao tác, Mobile 24 giờ | Keycloak: chỉ username/email; `permanentLockout: true`; `ssoSessionIdleTimeout` 3600; trang đăng nhập Keycloak mặc định | Đổi realm + theme đăng nhập (P1, §4) |

## 2. Ma trận truy vết UR/FN

### 2.1 Chức năng chung, Cấp Quốc gia, Cấp Tỉnh

| UR | FN | Hiện có (module / route / màn / kiểm thử) | Trạng thái | Thay đổi cần làm | Gói |
|---|---|---|---|---|---|
| UR-CH-01 | FN-CH-00.01 Đăng nhập | Keycloak realm `deploy/compose/keycloak/agrigreen-realm.json` (PKCE, brute force 5 lần/900 s nhưng **khóa vĩnh viễn**, idle 3600 s, TOTP); web `apps/web-ops/src/auth/oidc.ts`, `/login`, `/login/callback`; `principal.ts buildPrincipal` (claims + `/v1/me`); mobile `packages/agrigreen_mobile/lib/data/auth/session_service.dart` (offline 72 giờ), `pin_service.dart` (PIN 6 số, 5 lần → khóa 15 phút); accounts `POST /v1/accounts/users/:id/unlock`; UAT US-ADM-02/03 | Cần sửa | (1) Đăng nhập bằng SĐT: đặt `username` = SĐT chuẩn hóa, giữ `loginWithEmailAllowed`; (2) `permanentLockout:false`, `maxFailureWaitSeconds`/`waitIncrementSeconds` 900, `maxTemporaryLockouts` > 0; (3) theme đăng nhập Keycloak theo Design Sample, banner “còn n lần thử”; (4) bắt buộc đổi mật khẩu lần đầu (UPDATE_PASSWORD đã có); (5) client `agrigreen-web-ops` idle 4 giờ, client mobile phiên tối đa 24 giờ (thuộc tính client session); (6) màn **Chọn vai trò và phòng ban** + ngữ cảnh phiên (D1); (7) đổi vai trò/phòng ban trong menu tài khoản không đăng nhập lại; (8) trang mặc định theo vai trò; (9) Admin → Tổng quan hệ thống | P1, P6, P7 |
| UR-KT-02 | FN-CH-00.01, 04.03, 04.06 | — | Mới | Menu sinh từ danh mục phòng ban + cấu hình module; bật phòng ban không cần cập nhật ứng dụng (menu đọc từ API, không cứng trong `nav.ts`) | P2, P6 |
| UR-TW-01 | FN-TW-01.01 Dashboard TW | `extension` `GET /v1/extension/dashboard`, `GET /v1/dashboard` (unified, 4 KPI), web `/` (`src/app/page.tsx`), `metrics` daily_value; UAT US-DASH-01 | Cần sửa | Nguồn số = **lần cập nhật gần nhất** của tổng hợp (P4) thay cho `field_report`; chọn Tỉnh/Vụ/Kỳ; thẻ: DT xuống giống/KH, thu hoạch + năng suất, sâu bệnh (nặng), xã đã nộp; drill-down tới xã; Làm mới; Xuất Excel; < 5 s/500 người (bảng tóm tắt dựng sẵn khi “Cập nhật”) | P4, P6 |
| UR-TW-02 | FN-TW-01.02 Báo cáo TW | `reports` exports (`/v1/reports/exports`, xlsx/pdf), web `/bao-cao`; dữ liệu nguồn `extension.sowing`, `growth_pest` | Cần sửa | Màn xem bảng **đúng bố cục mẫu** (dựng từ phiên bản mẫu), tab theo biểu, gom Khu vực, dòng tổng Khu vực/Toàn tỉnh, Đã chốt/Tạm tính, so sánh cùng kỳ năm trước, năng suất bình quân gia quyền; Xuất Excel đúng mẫu/PDF; chỉ xem; thêm kỳ **năm** (Q-07) | P3, P4, P6 |
| UR-TI-01 | FN-TI-02.01 Dashboard tỉnh | như trên | Cần sửa | Như TW + lọc Khu vực, nút “Nhắc nhở” ở khung xã chưa nộp; số khớp FN-TI-02.05 | P4, P6 |
| UR-TI-02 | FN-TI-02.02 Phát hành biểu | Không có (gần nhất: `extension` directives, `reports/schedule`) | Mới | Đợt phát hành: nhiều mẫu cùng loại kỳ, phạm vi xã (toàn tỉnh/khu vực/từng xã), tự chia kỳ, hạn nộp mỗi kỳ, số ngày chốt sau hạn, giá trị cột Tỉnh giao (nhập tay/Excel/lấy đợt trước), xem trước số công việc, thu hồi đợt chưa ai nộp, sửa hạn chỉ cho kỳ chưa đến hạn; sinh **công việc** + thông báo (`notify` kind mới) | P4 |
| UR-TI-03 | FN-TI-02.03 Trạng thái nộp | `notify` (notification, push FCM data-only, nudge); không có bộ nhắc định kỳ | Mới | Bảng trạng thái thời gian thực theo đợt/biểu/kỳ; 4 bộ đếm; gửi nhắc chọn xã/tất cả; **tự nhắc trước hạn và khi trễ** (job theo tham số); lịch sử nhắc; nhắc tới tay < 1 phút | P4 |
| UR-TI-04 | FN-TI-02.04 Rà soát, chốt kỳ | `extension.period_close` (000006, chỉ chốt, **không mở lại**), `POST /v1/extension/report-periods/close`; `field_report` corrections; `program_report_approval` (000016) | Cần sửa (gần như mới) | Trạng thái công việc thêm Yêu cầu điều chỉnh/Đã xác nhận; lý do bắt buộc; chốt thủ công + **tự chốt theo lịch**; **mở lại kỳ theo xã/biểu, có lý do và thời hạn** (sự kiện bù, §4.1); cảnh báo khi báo cáo Chi cục đã gửi; Detail có khung Chốt kỳ + Log | P4 |
| UR-TI-05 | FN-TI-02.05 Tổng hợp các xã | `reports` aggregation (national/province/commune/cooperative) chỉ cho export | Mới | **Lần cập nhật** bất biến (ảnh chụp số toàn tỉnh theo mẫu), so khác ô với lần trước, nhật ký lần cập nhật + chi tiết ô (cột Excel, cũ → mới, người sửa, loại thay đổi), tab “Thay đổi chờ cập nhật”, tự cập nhật lúc chốt kỳ, xuất Excel theo mẫu/PDF/xuất nhật ký | P4 |
| UR-TI-06 | FN-TI-02.06 Báo cáo Chi cục | `reports/render/xlsx.go`, `render/pdf.go` (Roboto, ADR-0014); `reports/mail/smtp.go` (TLS, **không đính kèm**, người nhận là `actor_ref`) | Cần sửa | Danh bạ người nhận Chi cục (địa chỉ ngoài hệ thống), đính kèm tệp, giới hạn dung lượng → chỉ gửi link, log gửi/gửi lại/gửi lỗi; tài khoản SMTP công vụ TTKN An Giang; **bộ chuyển Google Sheets/Drive** (RB-7, §4.3); in “Tạm tính” + danh sách xã chưa nộp; mẫu CC01 (Q-08) | P4 |
| UR-TI-07 | FN-TI-02.07 Duyệt lịch | Không có | Mới | Xem P5 | P5 |

### 2.2 Cấp Xã

| UR | FN | Hiện có | Trạng thái | Thay đổi | Gói |
|---|---|---|---|---|---|
| UR-XA-01 | FN-XA-03.01 Công việc cần làm | `extension` tasks (`task_event`/`task_current`, nhiệm vụ hỗ trợ SLA 24 giờ, `/v1/extension/tasks`), web `/nhiem-vu`, field-app `tasks_screen.dart` | Mới (khái niệm khác) | “Công việc” v7.7 = nghĩa vụ nộp biểu, **không phải** nhiệm vụ hỗ trợ. Bảng mới (P4) + màn List web và mobile; số đếm menu; nhắc nhở và lý do điều chỉnh hiện trong dòng; đẩy thông báo mobile; “Trễ hạn” vẫn nộp đến chốt kỳ | P4, P6, P7 |
| UR-XA-02…15 | FN-XA-03.02 Nhập 14 biểu | `extension.field_report` (2 loại cố định), web không có màn nhập; field-app `field_report_screen.dart` (form tự do), hàng đợi offline `agrigreen_mobile` outbox | Mới | Màn nhập **dựng từ phiên bản mẫu**: hàng chữ cái cột, tiêu đề gộp, mỗi xã một tab, chỉ cột kỳ đang mở nhập được, màu ô theo loại cột, cố định cột định danh + cuộn ngang + “Đi tới”; công thức tính tại chỗ; kiểm “yêu cầu của mẫu” tại ô; Lưu nháp (tự lưu khi chuyển tab), Gửi, sửa và gửi lại trước chốt; Tải mẫu Excel (chỉ dòng của xã), Nhập từ Excel; Lịch sử sửa; offline trên mobile | P3, P4, P6, P7 |
| UR-XA-16 | FN-XA-03.03 HTX & nông hộ | `party` cooperative/household (000003), `/v1/cooperatives`, `/v1/households`, import Excel (admin), deactivation-preview/deactivate; web `/htx`, `/htx/[id]`; PII ở vault; UAT US-HTX-01…05 | Cần sửa | Phạm vi theo **xã phụ trách** (không theo `org_ref`); Cấp Xã được thêm/sửa trên app (hiện import ở Admin API); người đại diện + SĐT qua vault, **che một phần**; số nông hộ; Detail theo Design Sample với tab Nông hộ/Máy móc/Thửa ruộng + Log; chặn xóa khi có liên kết (đã có vô hiệu hóa) | P7, P6 |
| UR-XA-17 | FN-XA-03.04 Máy móc HTX | `mechanization` owner/machine (000007), `/v1/mechanization/*`, web `/cgh` | Cần sửa | Màn khai báo theo HTX (loại máy từ danh mục 14 loại PL3 CGH, số lượng nguyên ≥ 0, ghi chú), tổng theo xã = tổng HTX, gợi ý khi trùng loại, nạp Excel, lịch sử số lượng | P7 |
| UR-XA-18, UR-KT-01 | FN-XA-03.05 Vẽ thửa | `geo` plot (000004, PostGIS, chồng lấn, diện tích geodesic), `/v1/plots`, import KML/Shapefile/GeoJSON (admin); field-app `boundary_screen.dart`, `plots_screen.dart` (offline, cảnh báo chồng lấn); UAT US-PLOT-01…04 | Cần sửa (nhỏ) | Thêm công cụ **hình tròn, hình vuông**; trường HTX, **tên nông dân** (vault), tên lô, ghi chú; đối chiếu diện tích khai báo; **chặn lưu khi chồng lấn** (hiện là cảnh báo — kiểm lại), web vẽ thửa cho Cấp Xã; tải tệp KML/GeoJSON từ màn xã | P7 |
| UR-XA-19 | FN-XA-03.06 Vật tư | Không có (không có danh mục vật tư) | Mới | Danh mục `material` (masterdata) + bản ghi ghi nhận vật tư theo xã/thời điểm; tổng hợp theo xã cho Cấp Tỉnh | P2 (danh mục), P7 |
| UR-XA-20 | FN-XA-03.07 Báo cáo của xã | Không có | Mới | List báo cáo đã gửi theo tab xã + Thống kê của xã (DT XG/KH, sâu bệnh, nộp đúng hạn) | P4, P6 |
| UR-XA-21 | FN-XA-03.08 Lịch công tác | Không có | Mới | Xem P5 | P5, P7 |

### 2.3 Admin

| UR | FN | Hiện có | Trạng thái | Thay đổi | Gói |
|---|---|---|---|---|---|
| UR-AD-01 | FN-AD-04.01 ĐVHC | `masterdata.admin_unit` (000002, có hiệu lực theo thời gian, `successor_codes`, chặn sửa/xóa), seed `seeddata/admin_units_dbscl.json` (6 tỉnh, 591 xã), `/v1/masterdata/admin-units*`, `/admin-units/import`; `geo.admin_boundary`; UAT US-ADM | Đáp ứng (giao diện cần sửa) | Màn List theo Design Sample (mờ ĐVHC hết hiệu lực kèm mã mới), khung Sắp xếp ĐVHC, Ranh giới hành chính; kiểm thiếu sót: tọa độ trung tâm | P6 |
| UR-AD-02 | FN-AD-04.02 Cơ cấu 3 cấp | `party.org` (kinds `ttkn_national`, `ttkn_province`, `extension_team`…), `/v1/orgs`, `/orgs/:ref/move`, import/export Excel; web `/quan-tri/to-chuc`; UAT US-ADM-05/06 | Cần sửa | Thêm thuộc tính **Khu vực** cho Nút xã (danh mục khu vực theo tỉnh); Nút xã = `extension_team` gắn mã ĐVHC xã (1 Nút/xã); bảng Ảnh hưởng (tài khoản, HTX, thửa, kỳ chưa chốt); chặn di chuyển cấp 1, 2; số đã chốt giữ cấu trúc lúc chốt (ảnh chụp lưu `khu_vuc` tại thời điểm cập nhật) | P2 |
| UR-AD-03 | FN-AD-04.03 Phòng ban | Không có | Mới | Danh mục phòng ban theo đơn vị (mã, tên, mô tả, thứ tự, trạng thái Đang áp dụng/Chưa mở/Ngưng), số tài khoản; seed 6 phòng ban An Giang | P2 |
| UR-AD-04 | FN-AD-04.04 Tài khoản | `accounts` (000011, 000025): tạo/khóa/mở/gửi lại hành động Keycloak, import, bulk, `/users/:id/events`; web `/quan-tri/tai-khoan`; `port/grants.go RoleOrgKinds`; UAT US-ADM-01 | Cần sửa lớn | Bảng **gán vai trò** (vai trò × Nút × phòng ban[] × cấp quản lý × xã phụ trách[]) dạng sổ; màn Detail + Log chỉnh sửa (cũ → mới), tab Thông tin hệ thống / Phiên đang hoạt động / Lịch sử đăng nhập (Keycloak sessions + events); khóa thu hồi mọi phiên (đã có qua Keycloak logout — kiểm); chặn “Tạo tài khoản” khi chưa có Nút/phòng ban; cảnh báo Cấp Xã chưa gán xã; trùng SĐT/email → từ chối | P1 |
| UR-AD-05 | FN-AD-04.05 Danh mục nông nghiệp | `masterdata.catalog_item` (catalog `rice_variety`, `pest`, `machine_type`, `season`, `farm_stage`…), `/v1/masterdata/catalogs*`, import; web `/quan-tri/danh-muc`; UAT | Cần sửa | Thêm danh mục **cây trồng** (phân cấp nhóm › cây), **vật tư** (ĐVT), **mô hình sản xuất**; `pest` gắn cây trồng bị ảnh hưởng + ngưỡng mức độ (không bắt buộc); `machine_type` = 14 loại PL3 CGH; cột “biểu đang dùng” (đếm tham chiếu từ mẫu); đặt dưới menu Phòng ban › Phòng Trồng trọt | P2, P3 |
| UR-AD-06 | FN-AD-04.06 Mẫu báo cáo | `reports.template_version` (000013, bố cục xuất jsonb, mẫu hệ thống trong mã v0), `/v1/reports/templates*` (admin), web `/quan-tri/mau-bao-cao` | Mới (tái dùng phiên bản hóa) | Mẫu nhập liệu dạng lưới Excel: cột/dòng/gộp ô/loại cột/công thức/danh mục chọn, phiên bản nháp → phát hành, áp dụng từ kỳ, gán loại báo cáo của phòng ban × cấp; nhân bản, nạp từ Excel, khôi phục; xem trước màn nhập; seed 14 mẫu | P3 |
| UR-AD-07 | FN-AD-04.07 Menu các cấp | Không có (Admin hiện có vai `platform_admin` thấy hết, không ghi “thay cho ai”) | Mới | Chế độ hỗ trợ “xem như” (vai trò + tài khoản/xã), dải cảnh báo, lý do khi lưu, nhật ký hỗ trợ không xóa; yêu cầu MFA (`amr`) | P1, P6 |

### 2.4 Phi chức năng

| NFR | Hiện có | Trạng thái | Ghi chú |
|---|---|---|---|
| < 3 s trên 3G; dashboard, bản đồ < 5 s / 500 người | Có OTLP, chưa có kiểm tải KN | Cần sửa | Kiểm tải k6 trên dashboard dựng từ bảng tóm tắt lần cập nhật (P8) |
| ATTT cấp 3, mã hóa, MFA Admin, nhật ký truy cập ≥ 1 năm | MFA Admin API (`RequireMFA`), TOTP; `vault.access_log`; nhật ký truy cập HTTP chưa rõ thời hạn | Cần sửa | Chính sách lưu nhật ký truy cập ≥ 1 năm (runbook), Keycloak event store |
| Offline nhập báo cáo + vẽ thửa | `agrigreen_mobile` outbox UUIDv7, `idem_key`, SQLCipher; vẽ thửa offline có | Cần sửa | Thêm lược đồ offline cho mẫu + bản nháp báo cáo (P7) |
| Tương thích Excel (nhập/xuất đúng file mẫu, công thức trùng Excel) | `render/xlsx.go` (excelize) | Mới | Bộ tính công thức con của Excel + kiểm thử đối chiếu với tệp Excel gốc (P3) |
| Tích hợp: Excel/PDF/**Word**; SMTP công vụ; Google Sheets | xlsx, pdf; SMTP | Cần sửa | Word chưa có (Q-10); Google ngoài lãnh thổ (§4.3) |
| Truy vết (người/thời điểm tạo, sửa, phiên bản; cũ → mới) | Sổ chỉ ghi thêm khắp nơi (RB-1) | Đáp ứng về dữ liệu | Cần API “log” chuẩn cho khung Log của mọi màn Detail (P6 dùng, mỗi gói cung cấp) |
| ≥ 99,5 %; sao lưu hằng ngày 30 ngày; RPO 24 giờ; RTO 4 giờ | Runbook sao lưu, diễn tập phục hồi G1 | Đáp ứng (xác nhận) | Đối chiếu runbook với số mới |

## 3. Tính năng KN hiện có mà v7.7 bỏ khỏi Module Phòng Trồng trọt

**Không xóa mã.** Giữ nguyên module, API, kiểm thử; chỉ **ẩn khỏi menu Phòng Trồng trọt** (điều kiện hiển thị theo phòng ban/module) và ghi “giữ cho module khác”.

| Tính năng hiện có | Mã / route | Nguồn cũ | Xử lý v7.7 |
|---|---|---|---|
| Thư viện số, quy trình kỹ thuật, soạn bài viết, theo dõi phổ biến | `content` (000005, 000014), `/v1/documents`, `/v1/library`; web `/thu-vien`, `/quan-tri/bai-viet`, `/quan-tri/quy-trinh`; field-app `library_screen.dart` | FR-12…16, US-LIB-01…06 | Giữ cho **Module dùng chung / Phòng Thông tin - Truyền thông**; ẩn khỏi menu Trồng trọt |
| Chỉ đạo khẩn (directive + xác nhận đã đọc) | `extension` directive (000006), `/v1/extension/directives`; web `/chi-dao`; field-app `directives_screen.dart` | FR-19, US-DIR-01…03 | Không có trong 25 FN → ẩn, giữ cho module điều hành chung (Q-11). Nhắc nhở v7.7 là cơ chế riêng gắn công việc |
| Nhiệm vụ hỗ trợ SLA 24 giờ, yêu cầu hỗ trợ từ App HTX | `extension` task (000006), support_request (000026), `/v1/extension/tasks`, `/v1/support-requests`; web `/nhiem-vu`, `/htx/yeu-cau-ho-tro`; field-app `tasks_screen.dart` | FR-17/18, US-TASK-01…04 | Liên thông App HTX là **tích hợp ngoài** → ẩn khỏi menu Trồng trọt; giữ. Không dùng lại tên “Nhiệm vụ” cho việc này để tránh nhầm với nhóm menu “Nhiệm vụ” mới |
| Danh bạ trực, trạng thái trực, ca trực | `extension` staff/duty (000006), `/roster*`; web `/danh-ba`; field-app `roster_screen.dart` | FR-31 | “Hồ sơ cán bộ” thuộc **module dùng chung** → ẩn |
| Bản đồ khuyến nông, GIS nhiều lớp, phát lại | `geo`, `metrics` (000036/37), web `/ban-do`, `/ban-do-gis`, `/quan-tri/ban-do`, `/quan-tri/chi-so` | FR-09/10/11, BRD GIS | **Bản đồ GIS vùng trồng & dịch bệnh** → module dùng chung → ẩn. Bản đồ dùng cho **vẽ thửa** vẫn ở Trồng trọt |
| MRV (hồ sơ, đối chiếu, phương pháp) | `mrv`, web `/mrv*`, `/quan-tri/mrv-phuong-phap`; field-app `evidence_form_screen.dart` | FR-06/07/08 | **Hệ thống ngoài** → ẩn |
| Báo cáo Đề án 1 triệu ha (đăng ký/triển khai/hoàn thành, duyệt) | `extension.program_report` (000016), `/program-reports`, `/program-progress` | FR-03 | Không có trong v7.7 (có thể thành mẫu “Báo cáo đột xuất” nếu khách cần — Q-12) → ẩn |
| Báo cáo hiện trường tự do (gieo sạ, sinh trưởng – sâu bệnh) | `extension.field_report` (000006), `/field-reports`; field-app `field_report_screen.dart` | FR-01/02/04 | **Thay bằng** T01/T05… theo mẫu. Giữ bảng và API (dữ liệu G1), ẩn khỏi menu; không di trú số liệu sang mẫu mới trừ khi khách yêu cầu |
| Dashboard 4 KPI hợp nhất | `extension/handler/unified.go`, `GET /v1/dashboard`; web `/` | FR-26, US-DASH-01 | Thay bằng Dashboard v7.7 (FN-TW-01.01/TI-02.01). Giữ API cho bề mặt khác |
| Xuất báo cáo “theo mẫu Bộ”, lịch gửi báo cáo email | `reports` exports, schedules; web `/bao-cao`, `/quan-tri/mau-bao-cao`, `/htx/lich-bao-cao` | FR-05/27, US-DASH-02/03 | Bộ render xlsx/pdf **tái dùng** cho P3/P4; màn cũ ẩn khỏi menu Trồng trọt; lịch gửi HTX giữ cho App HTX |
| HTX: nhật ký canh tác, phê duyệt nhật ký, mùa vụ, sản lượng, tài khoản nông dân, thu gom rơm MKG | `cropcycle`, `accounts` farmer, web `/htx/*` | BRD App HTX, MKG | **Liên thông App HTX = tích hợp ngoài**; ẩn khỏi menu Trồng trọt. Danh sách HTX/nông hộ v7.7 vẫn giữ (FN-XA-03.03) |
| Cơ giới hóa: bản đồ CGH, cân đối, dự báo; mô phỏng Hub; đường thủy; kho/hub | `mechanization`, web `/cgh`, `/mo-phong`, `/quan-tri/thuy-he`, `/quan-tri/kho-hub` | BRD Bản đồ CGH, MKG | Không thuộc module; ẩn. Bảng `machine` dùng lại cho FN-XA-03.04 |
| Tham số & phê duyệt, quy chuẩn hiển thị bản đồ, xung đột đồng bộ, giám sát đồng bộ | `masterdata` params, `sync`; web `/quan-tri/tham-so`, `/hien-thi`, `/xung-dot`, `/giam-sat-dong-bo` | US-SYNC, R-56 | Công cụ vận hành nền tảng; chỉ hiện cho Admin trong nhóm “Quản trị hệ thống” (mở rộng) hoặc bề mặt quản trị nền tảng riêng (Q-13) |
| Cảnh báo sớm thiên tai & dịch hại, đào tạo, tin tức, sự kiện, hỏi đáp | Một phần trong notify/content | FR-17/18/20–25/28 | Module khác |

## 4. Xung đột với ràng buộc kiến trúc và đề xuất xử lý

### 4.1 RB-1 (chỉ ghi thêm) ↔ “sửa trước chốt kỳ”, “mở lại kỳ”, “sửa mẫu”, nhật ký cập nhật
- **Xung đột:** BRD dùng ngôn ngữ “sửa”, “mở lại kỳ”, “gửi lại”, “khôi phục phiên bản”, “thu hồi đợt”, “sửa hạn nộp”; RB-1 cấm UPDATE/DELETE trên sổ; `extension.period_close` hiện **không cho mở lại**.
- **Đề xuất:**
  - Mỗi lần **Lưu nháp/Gửi/Gửi lại** = một **phiên bản bài nộp** bất biến (`submission_version`: công việc, phiên bản mẫu, giá trị ô jsonb theo chữ cái cột, loại = nháp/gửi, `supersedes_id`). “Sửa” = phiên bản mới; lịch sử sửa = chuỗi phiên bản (đúng AC “giá trị cũ – mới”). Bản nháp có thể tách sang bảng phiên bản nháp có giới hạn (giữ N bản gần nhất trên thiết bị, server chỉ nhận bản khi lưu) để không phình sổ.
  - **Chốt kỳ** và **mở lại kỳ** là sự kiện trên sổ `period_event` (closed / reopened{xã, biểu, lý do ≥ 10 ký tự, hạn mở} / auto_closed / reclosed). Trạng thái “đang chốt” là phép chiếu. Không sửa `extension.period_close` cũ; bảng mới thay thế cho vòng đời v7.7 (kỳ cũ `YYYY-MM` giữ nguyên).
  - **Lần cập nhật tổng hợp** là sổ (`consolidation_run` + `consolidation_cell_change`) — khớp tự nhiên với “nhật ký không xóa”. “Phiên bản điều chỉnh” sau mở lại = lần cập nhật mới.
  - **Mẫu báo cáo:** phiên bản đã phát hành bất biến; nháp là phiên bản trạng thái `draft` thay thế bằng bản nháp mới (sổ) hoặc bảng nháp có cột `updated_by/updated_at` (không phải sổ, chấp nhận vì chưa có số liệu dựa vào) — chọn khi làm P3. “Khôi phục” = tạo nháp mới sao từ phiên bản cũ.
  - **Thu hồi đợt** = sự kiện `batch_withdrawn` (chỉ khi chưa xã nào nộp); **sửa hạn** = sự kiện `deadline_changed` chỉ áp cho kỳ chưa đến hạn.
  - Các trường “người sửa/thời điểm sửa” trên màn hình lấy từ sự kiện cuối, không phải cột UPDATE.

### 4.2 RB-3 (dữ liệu cá nhân trong vault)
- **Dữ liệu cá nhân mới trong v7.7:** họ tên, SĐT người đại diện HTX; chủ hộ; **tên nông dân** khi vẽ thửa; họ tên, email, SĐT cán bộ (đã ở Keycloak – giữ); **email người nhận Chi cục, CC** (có thể là địa chỉ cá nhân công vụ); nội dung lịch công tác có thể chứa tên người (thành phần phối hợp).
- **Đề xuất:** HTX/nông hộ/tên nông dân → `vault/identity` (`*_ref`), hiển thị qua `display` có mục đích, SĐT **che một phần** ở API hiển thị (không chỉ ở UI); danh bạ Chi cục → lưu địa chỉ dưới dạng chủ thể vault (loại `contact`) hoặc, nếu khách xác nhận chỉ dùng hộp thư đơn vị (vd `trongtrot.chicuc@…gov.vn`), coi là dữ liệu tổ chức (Q-09); khung Log hiển thị tên người sửa bằng tra `actor_ref` lúc đọc, **không** chép tên vào sổ; tệp xuất Excel danh sách HTX ghi `export_audit` (đã có DLP) và che SĐT; **tệp/Google Sheet gửi Chi cục chỉ chứa số tổng hợp theo xã, không có PII**. “Thành phần phối hợp” của lịch là văn bản tự do → cảnh báo người dùng không nhập SĐT/CCCD, hoặc chọn từ danh sách cán bộ (`actor_ref`).

### 4.3 RB-7 (lưu trong lãnh thổ) ↔ Google Sheet + tài khoản Google + email gửi đi
- **Xung đột:** FN-TI-02.06 tạo Google Sheet bằng tài khoản Google của TTKN An Giang (Drive/Sheets API – máy chủ ngoài lãnh thổ); ADR-0007 chỉ cho nguồn ngoài lãnh thổ khi **không có định danh**, qua allowlist + ADR riêng. Email công vụ An Giang qua SMTP (thường là hệ thống thư công vụ của tỉnh, trong lãnh thổ) — nếu “email công vụ” thực ra là Gmail/Google Workspace thì cũng ngoài lãnh thổ.
- **Đề xuất:**
  1. **ADR mới “Xuất bản báo cáo tổng hợp sang Google Sheets”** (đề xuất ADR-0022): chỉ dữ liệu **tổng hợp không định danh** (số liệu cấp xã/khu vực/tỉnh theo mẫu; không tên cán bộ, không SĐT); bộ chuyển `reports/gsheet` tắt mặc định, bật bằng `AG_NONPII_ALLOWLIST` + `AG_EXTDATA_GSHEET_ADR` theo cơ chế ADR-0007; kiểm thử van chặn trường lạ (như `TestValveRuntimeDefaultDeny`).
  2. **Thông tin xác thực Google** (OAuth refresh token/service account của TTKN An Giang) lưu trong KMS/bí mật trong lãnh thổ, chỉ Admin kết nối; nhật ký mỗi lần tạo/chia sẻ tệp.
  3. **Đường dự phòng mặc định:** đính kèm Excel/PDF; khi vượt giới hạn dung lượng mà Google tắt → gửi **liên kết tải có hạn** từ kho đối tượng trong lãnh thổ (MinIO/S3 VN) thay cho link Google.
  4. SMTP: dùng relay thư công vụ của An Giang; nếu là Google Workspace → cùng ADR, nội dung email không PII ngoài địa chỉ người nhận (Q-09, Q-14).
  5. “Chia sẻ quyền xem cho người nhận” trên Google → gửi địa chỉ email người nhận cho Google: là PII ra ngoài lãnh thổ → khách phải chấp thuận bằng văn bản; phương án thay: link “ai có liên kết đều xem” (không gửi email người nhận cho Google) — rủi ro lộ link, cần khách chọn.

### 4.4 Mật khẩu tạm
- **Xung đột:** BRD: “tạo mật khẩu tạm và gửi kích hoạt”, nút “Gửi lại mật khẩu”, log “Gửi lại mật khẩu tạm”; hiện `accounts` **không bao giờ gửi mật khẩu tạm** (Keycloak execute-actions email: VERIFY_EMAIL, UPDATE_PASSWORD, CONFIGURE_TOTP).
- **Đề xuất:** giữ cơ chế liên kết kích hoạt (an toàn hơn, đáp ứng “lần đầu bắt buộc đổi mật khẩu”); đổi nhãn nút thành “Gửi lại kích hoạt / đặt lại mật khẩu”, log ghi “Gửi lại liên kết đặt mật khẩu”. Nếu cán bộ xã **không có email** (chỉ SĐT) → cần kênh SMS (chưa có, nhà cung cấp SMS trong lãnh thổ) hoặc Admin đặt mật khẩu tạm hiển thị một lần trên màn Admin (Keycloak `temporary: true`), không gửi qua kênh nào, ghi log — chờ khách chọn (Q-03).

### 4.5 Nhiều vai trò trong token Keycloak
- **Xung đột:** realm chỉ có **một** `org_ref`, roles là realm roles phẳng; v7.7 cần nhiều (vai trò, Nút, phòng ban[], cấp quản lý, xã[]) và đổi ngữ cảnh **không đăng nhập lại**; phòng ban bật/tắt có hiệu lực ngay.
- **Đề xuất (phương án A, khuyến nghị):** Keycloak chỉ **xác thực** (danh tính, MFA, khóa, phiên). Gán vai trò KN lưu ở `pf_accounts.role_assignment` (sổ + chiếu `role_assignment_current`). Sau đăng nhập, client gọi `GET /v1/me/contexts` → danh sách cặp (vai trò, phòng ban); chọn → `POST /v1/me/context` trả `context_id`; mọi request gửi `X-AG-Context: <context_id>`; middleware kiểm context thuộc người dùng, còn hiệu lực, phòng ban “Đang áp dụng”, rồi dựng `Principal{Roles: [vai KN của context], Department, Scope}`. Đổi ngữ cảnh = đổi header, không cần token mới. Thu hồi tức thời (khóa tài khoản đã logout Keycloak; gỡ vai trò/phòng ban có hiệu lực ở request kế tiếp). Mobile lưu danh sách context trong phiên offline 72 giờ.
- **Phương án B:** mỗi context một token (Keycloak token exchange/“organizations” + claim `ctx`) — chuẩn hơn về OIDC nhưng phức tạp, phụ thuộc tính năng Keycloak bản mới, khó offline. Không khuyến nghị cho G-kế tiếp.
- Tương thích: realm roles hiện có (`kn_national_admin`, `kn_province_admin`, `kn_officer`) giữ cho module khác; P1 thêm ánh xạ: vai KN v7.7 `kn_tw` (Cấp Quốc gia, chỉ đọc), `kn_tinh`, `kn_xa`, Admin = `platform_admin` (bắt buộc MFA). `authz.Require` của module KN mới kiểm vai trong context, không trong token.

### 4.6 Các xung đột khác
| Vấn đề | Đề xuất |
|---|---|
| Khóa tạm 15 phút vs `permanentLockout: true` (quyết định ATTT trước đây) | Đổi realm theo BRD (khóa tạm), giữ `maxTemporaryLockouts` (vd 3 lần tạm → khóa vĩnh viễn) để vẫn đạt ATTT cấp 3; chủ sản phẩm xác nhận |
| Phiên Web **4 giờ không thao tác** vs hiện 1 giờ | Đặt ở client `agrigreen-web-ops` (client session idle 14400 s); Admin (MFA) có thể giữ ngắn hơn |
| Trang đăng nhập riêng theo Design Sample vs Keycloak hosted login | Theme Keycloak `agrigreen` (FreeMarker) dựng theo Design Sample; **không** làm form mật khẩu trong web-ops (giữ PKCE). “Còn n lần thử” cần authenticator/theme đọc số lần sai — kiểm khả năng, nếu không được thì ghi “sau 5 lần sai sẽ bị khóa 15 phút” |
| Đăng nhập bằng SĐT | `username` = SĐT chuẩn hóa `0xxxxxxxxx` (đã có validator `phone_number`), email qua `loginWithEmailAllowed`; tài khoản không có SĐT dùng username sinh |
| Admin “vào menu mọi cấp, nhập liệu thay” vs RB-3/maker-checker và nguyên tắc “Tỉnh không nhập đè” | Bản nộp do Admin hỗ trợ mang `on_behalf_of` + lý do; nhật ký hỗ trợ là sổ riêng; chặn Admin tự xác nhận/chốt bản do chính mình nhập thay |
| Scope hiện suy từ **loại tổ chức** của `org_ref`; Cấp Xã v7.7 gắn Nút **tỉnh** + danh sách xã phụ trách (Hình 25b) | `Scope` mới = Nút của context (xuống dưới) ∩ phòng ban; với Cấp Xã = danh sách xã phụ trách. Giữ `ScopeResolver` cũ cho module khác |
| Dữ liệu cũ không có phòng ban | Mặc định coi là “Phòng Trồng trọt” khi đọc qua module KN mới; không di trú bằng UPDATE |
| Sửa ĐVHC/di chuyển Nút sau chốt (“số đã chốt giữ theo cấu trúc lúc chốt”) | Lần cập nhật tổng hợp lưu khu vực, mã ĐVHC, tên xã tại thời điểm chạy |
| Engine công thức Excel trong Go | Chỉ hỗ trợ tập hàm cần cho 14 mẫu (SUM, cộng trừ nhân chia, IF, ROUND, tham chiếu cột cùng dòng, tổng nhóm); thư viện phải thuộc `allowed-licenses.txt` (excelize BSD-3 đã có; bộ tính công thức của excelize có thể dùng) |
| Thông báo đẩy qua FCM ngoài lãnh thổ (ADR-0015) | Giữ data-only: đẩy “có công việc mới”, app kéo nội dung qua API |

## 5. Kiểm thử hiện có bị ảnh hưởng (không xóa)
- `tests/e2e/g1_scenario_test.go` (TestG1DongXuanCanTho) dùng field-report, nhiệm vụ, chỉ đạo — giữ; chỉ chạy lại sau P1 (đổi Principal).
- `internal/platform/accounts/port/grants_test.go`, `handler/accounts_integration_test.go` — phải mở rộng cho gán nhiều vai trò.
- `apps/web-ops/src/nav/nav.test.tsx`, `tests/e2e/navigation.spec.ts` — viết lại theo menu v7.7; các spec màn bị ẩn (roster, sop, gis, mrv…) chuyển sang chạy với “bề mặt đầy đủ/module khác” (cờ cấu hình) thay vì xóa.
- `apps/field-app/test/task_service_test.dart`, `widget_smoke_test.dart` — cập nhật khi màn chính đổi sang Công việc cần làm.
