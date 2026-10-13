# ADR-0049: `cap/price` = module năng lực `pf/price` — chuỗi giá có hiệu lực, danh mục nền, công bố là quyền riêng

**Trạng thái:** Đề xuất — khung hiện thực ở W11.71 (10/10/2026); phạm vi G1 theo QĐ-5 mặc định (a), chờ chủ sản phẩm (Q-044) · **Ngày:** 10/10/2026 (gói W11.71) · **Yêu cầu:** kiến trúc v4.1 §26 (`cap/price`), §5.1 (ma trận KN ●, HTX ○, MKG ●, NH ●), R-CODE-01, R-MOD-04, R-MOD-09, R-DB-10, Phụ lục A (G1 cần `cap/price` tối thiểu) · **Nguồn:** BRD Báo giá nông sản v1.3 (07/10/2026), đối chiếu v4.3/MKG v1.4/BRD KN Phụ lục D mục 2.3, BG1 · **Thành phần:** `internal/platform/price`, `cmd/api-platform/assembly/price.go`, `migrations/pf/000138_pf_price.up.sql`, `tools/archlint/nofloat.go` · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md)

## Bối cảnh

- v4.1 §26 định nghĩa `cap/price`: sở hữu chuỗi giá có hiệu lực và ngưỡng; port `Series`, `Publish`; sự kiện `price.published`; điểm mở rộng "Nguồn giá"; công bố là quyền riêng. ADR-0027 bảng mục 2 ghi "Chưa có module".
- BRD Báo giá nông sản v1.3 (Phòng TTTT): 26 xã báo giá, 30–40 mặt hàng/xã, 5 nhóm hàng, 2 nhóm vùng; phiếu xã hằng ngày khóa 12:00, cảnh báo biến động, duyệt/công bố bảng giá ngày có phiên bản, điều tra giá thành theo vụ (phiếu có PII nông hộ). Không module domain nào khác của repo cần quy trình phiếu xã.
- Bên dùng: KN (ngay), MKG và NH (đọc bảng giá qua cổng tích hợp), HTX (○) — đạt quy tắc ba (R-MOD-09) cho một module năng lực.
- R-CODE-01 nêu đích danh `cap/price` trong danh sách cấm float.

## Quyết định

### 1. Một module năng lực `pf/price`

`pf/price` (`internal/platform/price`, `kind: capability`, `aka: [cap/price]`, `zones: [pf]`, `enable: per_tenant`) sở hữu schema `pf_price` (nội bộ) và `pf_price_pub` (view công bố). Vai nhóm `r_price` (ADR-0028; `app_platform` là thành viên), vai đọc view `r_price_pub` (thành viên: `r_metrics`, `r_agg`). Module khác chỉ import `price/port` (R-02).

Quy trình phiếu xã hằng ngày nằm TRONG `pf/price` như **nguồn giá nội tại** (không module domain nào khác cần nó). Điều tra giá thành là nguồn giá thứ hai, cũng trong `pf/price`; PII nông hộ đi `vault/identity` (chỉ giữ `household_ref`/băm SĐT).

### 2. Cấu hình theo bên dùng (Profile), không so tên ứng dụng

`port.Profile` (tên, phòng ban chủ, vai → cấp TW/TINH/XA, vai quản trị danh mục) khai ở gốc lắp ráp (`cmd/api-platform/assembly/price.go`, KN = `kn`), như `forms.Profile` (R-MOD-04). Các gói sau mở rộng Profile (cờ Quản trị nghiệp vụ, giờ khóa, hạn phản hồi) thay vì rẽ nhánh theo ứng dụng. Phòng TTTT là cấu hình phòng ban của `dom/extension` cho menu; dữ liệu giá do `pf/price` sở hữu.

### 3. Dữ liệu: sổ chỉ ghi thêm, hiệu lực theo ngày báo giá, decimal

- Mọi bảng nghiệp vụ là sổ chỉ ghi thêm (`shared.apply_ledger`), RLS `p_tenant` (`shared.apply_tenant_rls`). Danh mục có phiên bản: mỗi dòng là một phiên bản của một mã (`version = seq` của chuỗi `<loại>:<mã>`), hiệu lực theo `valid_from` (date, giờ VN) — BRD BR-02 "mọi thay đổi áp dụng từ ngày báo giá kế tiếp"; ngưng là phiên bản `retired`, không xóa (BR-04).
- Giá, %, ngưỡng là `numeric` ở DB và decimal ở Go. `internal/platform/price` nằm trong `floatFreeZones` của agnofloat (cấm cả trong kiểm thử).
- Một danh mục mặt hàng duy nhất ở `pf/price`; KHTC và module khác đọc qua `port.Catalog` (đề xuất Q-082).

### 4. Điểm mở rộng cho các gói sau

| Gói | Thêm vào | Ghi chú |
|---|---|---|
| W11.72 xã báo giá + phân công | bảng `pf_price.collection_point` (↔ mã ĐVHC có hiệu lực ADR-0032, nhóm vùng), phân công CTV/Tỉnh theo ngày; `Profile` thêm cờ QTNV | migration riêng, tiền tố `pf_price` |
| W11.73 mặt hàng theo thời kỳ | sổ mặt hàng tham chiếu `unit.code`, `commodity_group.code`; gán xã, bộ theo vụ, "thay thế cho" | dùng mẫu phiên bản + `valid_from` của pf 000138 |
| W11.74 lịch, giờ khóa, ngưỡng | lịch + ngoại lệ, `Profile` thêm giờ khóa/hạn phản hồi, ngưỡng decimal | job khóa ở `cmd/worker` |
| W11.75 phiếu báo giá + xác minh | sổ phiếu/dòng giá (min/max numeric), cảnh báo, hiệu chỉnh/xóa mềm | |
| W11.76 duyệt/công bố | bảng giá ngày có phiên bản, port `Series`, `Publish`, sự kiện `price.published`, view `pf_price_pub.v1_board_daily` (`//ag:pubview`) | công bố là quyền riêng |
| W11.81 website | tập dữ liệu `price_board_daily` qua cổng tích hợp (ADR-0033, feed cursor) | không DB chung |

### 5. Ngưỡng k

Bảng giá xã là giá thị trường, không phải dữ liệu hộ → không áp k. Giá thành điều tra (theo nông hộ) rời tenant phải là tập tổng hợp có k theo `household` (ADR-0035, mặc định `publicdata.k_min` = 10); trong tenant giữ nhãn "Mẫu nhỏ" của BRD (đề xuất Q-083).

## Phương án đã cân nhắc

- **Đặt Báo giá trong `pf/extension` (dom/extension) như một phòng ban.** Loại: v4.1 §26 đã tách `cap/price`; MKG, NH cũng cần chuỗi giá; R-CODE-01 cần ranh giới module để cấm float.
- **Danh mục mặt hàng ở `pf/masterdata`.** Loại ở khung: danh mục giá có quy tắc riêng (hiệu lực theo ngày báo giá, khóa ĐVT khi đã có giá, "thay thế cho") và chủ là Phòng TTTT; `pf/masterdata` giữ tham số chung. Xem lại nếu Q-082 chọn KHTC làm chủ.
- **Một module nguồn giá riêng (domain) cho phiếu xã.** Loại: chỉ một bên dùng, chưa đạt quy tắc ba.

## Hệ quả

- Thêm một schema, hai vai nhóm (`r_price`, `r_price_pub`) — `cmd/migrate bootstrap` phải chạy trước pf 000138.
- KN có route `/v1/price/*`; bật/tắt theo tenant qua cổng module (`enable: per_tenant`, mặc định bật).
- Các gói W11.72–W11.76 song song được trên cùng schema nhờ mỗi gói một migration và bảng riêng; xung đột chỉ ở `port.Profile` và `docs/errors.md`.

## Hiện thực

### W11.10 hợp đồng view giá (12/10/2026, không migration `pf_price`)

- Cột của `pf_price_pub.v1_board_daily` (bảng giá đã duyệt theo ngày, min/max toàn tỉnh — Q-084) và `pf_price_pub.v1_ticket_week` (số phiếu theo tuần báo cáo + cờ tuần đã chốt) chốt ở `contracts/pubview/*.json`, trạng thái `planned`; W11.76 dựng view đúng cột, khai `//ag:pubview`, đổi `published` (việc cụ thể: [contracts/pubview/README.md](../../contracts/pubview/README.md#việc-của-w1176-để-dựng-hai-view-giá)).

### W11.71 khung (10/10/2026, pf 000138)

- `internal/platform/price`: `module.yaml` (sinh: `ports: [Catalog]`, `owns_schema: [pf_price, pf_price_pub]`), `port` (Profile, Catalog, PubSchema, `price.profile_not_found`), `domain` (mã, `valid_from` theo ngày VN, 5 nhóm hàng BRD + 8 đơn vị tính mặc định), `store`, `app`, `handler`.
- pf 000138: schema `pf_price`, `pf_price_pub` (chưa view); sổ `pf_price.unit`, `pf_price.commodity_group`; quyền `r_price` SELECT/INSERT + `k_ref`; `r_price_pub` USAGE schema công bố. Xã báo giá KHÔNG có ở khung (W11.72: cần ĐVHC có hiệu lực, không đơn giản).
- HTTP (`contracts/openapi/application.yaml`, mục pf/price): `GET /v1/price/status`, `GET /v1/price/catalog?at=`, `POST /v1/price/catalog/defaults` (Admin KN; chỉ ghi mã chưa có).
- `app/KN/app.yaml` thêm `pf/price`; agnofloat `floatFreeZones` thêm `/internal/platform/price`; `dbadmin` kiểm thử: `laterGroups`, số vai nhóm, `r_price_pub`.
- Giả định (docs/kn/gia-dinh-brd-2026-10.md): QĐ-5 (a), Q-082 (một danh mục ở `cap/price`), mã phòng ban `thong_tin_truyen_thong`, danh sách đơn vị tính mặc định.

### W11.74 lịch báo giá, giờ khóa, nhắc, ngưỡng (10/10/2026, pf 000139)

- `port.Profile.Schedule` (`ScheduleDefaults`): lịch/ngưỡng MẶC ĐỊNH của bên dùng (KN: T2–T6, khóa 12:00, hạn phản hồi 13:30, nhắc 07:30/11:00, 30 %, N = 7, đi ngang 1 %, phiếu điều tra 40 %) — không rẽ nhánh theo tên ứng dụng; `module.New` kiểm cấu hình lúc khởi động.
- pf 000139: sổ `pf_price.calendar_rule` (phiên bản lịch, `valid_from`), `calendar_exception` (ngày nghỉ/bổ sung, xóa = `retired`), `alert_threshold` (`numeric(5,2)`, phạm vi default/group/item; default mang N, đi ngang, phiếu điều tra; không `retired` được), `calendar_run` (nhật ký job, CHECK `seq = 1`). `ag_ops_jobs` đọc cột `tenant_id` của `unit`, `calendar_rule`.
- Port: `Calendar.Day` (kế hoạch ngày, `DayPlan.Locked(now)` — W11.75 chặn ghi theo đồng hồ, không đợi job), `Thresholds` (ngưỡng mặt hàng → nhóm → mặc định, tham số chung), `ReminderTargets` (W11.72/W11.75 cài, `Module.UseReminderTargets`).
- Job `price_calendar` (`cmd/worker`, `schedule.Every{1 phút}` ở leader, `RunOnStart`): ghi mốc khóa (độ trễ = `event_time − scheduled_at`, kiểm thử ≤ 60 s) và nhắc lần 1/2 (một lần mỗi ngày; ngày nghỉ/ngoài lịch không nhắc), phát `pf.price.reminder.issued` → consumer `pf.notify.price` (loại thông báo tạm dùng `report_task_due`).
- HTTP: `GET /v1/price/calendar`, `POST /v1/price/calendar/schedule`, `POST /v1/price/calendar/exceptions`, `POST /v1/price/calendar/exceptions/{day}/retire`, `GET /v1/price/calendar/runs`, `POST /v1/price/calendar/reminder-test`, `GET|POST /v1/price/thresholds`, `POST /v1/price/thresholds/{scope}/{code}/retire`. Web-ops: `/kn/phong-ban/[dept]/bao-gia-lich` (chưa có mục menu — menu do người tích hợp bật).
### W11.73 mặt hàng theo thời kỳ (10/10/2026, pf 000141)

- Sổ `pf_price.item` (chuỗi `item:<code>`, mã `MH#####` do hệ thống sinh dưới khóa cố vấn theo tenant, không tái sử dụng — BR-01), `item_assignment` (`assign:<point>:<item>`), `item_proposal` (`proposal:<id>`), `item_set` (`set:<code>`, `item_codes text[]`, `enabled`); cùng mẫu phiên bản + `valid_from` của pf 000138. Nhóm hàng ghi tiếp vào `pf_price.commodity_group` (thêm, sửa tên, xóa = phiên bản `retired`).
- Phép chiếu ở `domain` (thuần, có kiểm thử): phiên bản có hiệu lực tại ngày, trạng thái Hoạt động / Chờ hiệu lực / Ngưng + "hiệu lực đến"; mặt hàng của xã = Cơ bản ∪ Địa phương đã gán, trừ mặt hàng thuộc bộ theo vụ mà không bộ nào chứa nó đang bật (BR-08).
- "Thay thế cho": mặt hàng mới mang `replaces_code`; mặt hàng cũ ngưng từ ngày hiệu lực của mặt hàng mới; mặt hàng Địa phương không chọn xã nhận các xã đang gán mặt hàng cũ; nhãn "Giá mới" = thay thế + chưa có giá.
- `port.Items` (`ItemsAt`, `PointItems`) — KHTC đọc (Q-082), phiếu W11.75 đọc. `app.ItemHooks` là điểm nối của gói song song: `HasPrice`/`HasSheets` (W11.75), `NextDay` (lịch W11.74; mặc định ngày mai), `Steward` (cờ QTNV — W11.72: nil = `Profile.StewardDuty`) — nối bằng `price.Module.SetItemHooks`.
- Nối với W11.74: `ItemHooks.NextDay` = `Service.NextQuoteDay` (lịch báo giá, `Module.WireItemCalendar` ở gốc lắp ráp). Ngưỡng theo mặt hàng (`alert_threshold` scope `item`) dùng mã `MH#####` và phải có trong `pf_price.item` (pf 000141 nới CHECK `scope_code` cho chữ hoa).
- Xã báo giá: `point_code` là mã xã báo giá (mặc định mã ĐVHC xã), KHÔNG khóa ngoại — W11.72 dựng `collection_point` và kiểm mã khi nối.
- Chưa làm: nhập danh mục từ Excel (chờ `cap/import`), thông báo cho Cấp Xã khi đề xuất được xử lý (chờ nối `notify`), đối chiếu xã của Cấp Xã với phân công (W11.72).

### W11.72 xã báo giá + phân công (12/10/2026, pf 000145)

- Sổ `pf_price.region_group` (nhóm vùng — cùng cột với `commodity_group`, chuỗi `region:<code>`; mặc định An Giang, Kiên Giang ghi cùng `POST /v1/price/catalog/defaults`), `collection_point` (xã báo giá, chuỗi `point:<code>`, mã `XBG####` hệ thống sinh dưới khóa cố vấn theo tenant; `admin_unit_code` + tên xã chụp theo ĐVHC tại ngày hiệu lực; `region_code`; ngưng = `retired` kèm lý do), `point_officer` (chuỗi `<role>:<point>`; `xa` = cán bộ Xã phụ trách chính, `tinh` = cán bộ Cấp Tỉnh; `officer_ref` NULL = bỏ phân công).
- ĐVHC có hiệu lực (ADR-0032): đọc bảng chiếu `k_ref.admin_unit` (`valid_to` không bao gồm) — chỉ ĐVHC cấp xã có hiệu lực tại ngày hiệu lực; một ĐVHC chỉ gắn một xã báo giá đang/sắp hoạt động (`domain.AdminUnitClash`); sắp xếp ĐVHC = `POST /v1/price/points/{code}/revise` với mã ĐVHC mới (giữ mã xã, lịch sử); danh sách báo `admin_unit_valid = false` + mã kế thừa khi ĐVHC đã hết hiệu lực.
- `port.Profile.StewardDuty` = cờ nhiệm vụ trên lần gán Cấp Tỉnh cho quyền Quản trị nghiệp vụ (KN: `authz.DutyPriceAdmin` của W11.07, đọc `KNContext.HasDuty`). Lịch, ngưỡng, gửi thử nhắc (W11.74) và mặt hàng (W11.73 `ItemHooks.Steward` nil → cờ của Profile) dùng cờ này; xã báo giá, nhóm vùng, phân công chỉ vai `ManageBy` (Admin — FN-AD-04.01/04.02). `GET /v1/price/status` thêm `can_steward`.
- `port.Points` (`PointsAt`, `PointAt`; `Point.HandledBy` = xã được phân công cho người đó hoặc chưa phân công) cho W11.75/W11.77; `port.Officers` (danh bạ cán bộ — gốc lắp ráp nối `PriceOfficers` từ `pf/accounts` `Assignments` + `ActorNames`, phòng ban = `Profile.Department`).
- `point_code` của W11.73 nay phải là xã báo giá có thật và đang hoạt động tại ngày hiệu lực (gán mặt hàng, thêm mặt hàng có xã, duyệt đề xuất); Cấp Xã chỉ đề xuất cho xã mình phụ trách chính (vắng mã + phụ trách một xã → xã đó); mặt hàng của xã ngưng/không có = rỗng.
- `ReminderTargets` mặc định (`price.New`) = `app.PointReminders`: mỗi xã đang hoạt động có cán bộ Xã phụ trách → một nhóm người nhận; xã "Chưa có cán bộ Xã" không nhắc; lần 2 bỏ xã đã gửi khi W11.75 nối `PointHooks.Submitted` (`Module.SetPointHooks`).
- HTTP: `GET|POST /v1/price/regions`, `POST /v1/price/regions/{code}/revise|retire`, `GET|POST /v1/price/points`, `GET /v1/price/points/{code}`, `POST /v1/price/points/{code}/revise|retire|officers`, `GET /v1/price/admin-units`, `GET /v1/price/officers`. Web-ops: `/kn/bao-gia/xa-bao-gia` (Admin › Phòng ban › Phòng TTTT › "Xã báo giá"), menu "Kỳ báo giá & giờ khóa" (Admin, `/kn/phong-ban/{department}/bao-gia-lich`) và "Cấu hình báo giá" (Cấp Tỉnh có cờ QTNV, `/kn/bao-gia/cau-hinh` — cùng màn W11.74) đã mở.

### W11.75 phiếu báo giá + xác minh (12/10/2026, pf 000148)

- Sổ `pf_price.ticket` (đầu phiếu, chuỗi `ticket:<ticket_id>`; `status` draft / submitted / late / deleted, `source` xa / proxy, `op`, lý do, `officer_ref` chụp tại ngày), `ticket_line` (chuỗi `line:<ticket>:<item>`; `min_price`/`max_price` `numeric(14,2)`, `no_goods`, ghi chú biến động; `adj_*` = giá hiệu chỉnh — giữ cả giá Xã gửi, BR-05 FN-TI-02.04), `price_alert` (chuỗi `alert:<ticket>:<item>`; provisional → requested → responded → adjusted / kept / excluded, cancelled = tự hủy, reopened; chụp P_ref, P, Δ%, ngưỡng; nội dung yêu cầu, hạn phản hồi, phản hồi + giá đề xuất + giải trình). Một xã một phiếu còn hiệu lực / ngày: khóa cố vấn theo tenant + xã (không UNIQUE trên sổ chỉ ghi thêm — sửa ngày là phiên bản mới). Khóa lạc quan: `version` = `seq` đầu phiếu (409 `price.ticket_stale`) thay cho khóa "Đang sửa bởi …" (BR-06 FN-TI-02.04).
- Không job ghi trạng thái lúc 12:00: "Không nộp" (chưa có phiếu / bản nháp sau giờ khóa), "Đã khóa", "Chờ rà soát" (Tạm tính sau giờ khóa), "Quá hạn phản hồi" là phép chiếu `domain.TicketView` / `AlertView` theo `DayPlan.Locked(now)` và `respond_by` lúc đọc; ghi của Cấp Xã bị chặn đúng giờ khóa theo đồng hồ máy chủ.
- Giá tham chiếu (CT-02): P (giá hiệu chỉnh nếu có, bỏ dòng Đã loại, Không có hàng) của phiếu Đã gửi / Nộp muộn gần nhất trong N ngày báo giá liền trước (N từ ngưỡng mặc định, W11.74). Cảnh báo sinh / cập nhật / tự hủy khi Xã gửi hoặc gửi lại, ngưỡng mặt hàng → nhóm → mặc định đang hiệu lực lúc gửi.
- Điểm nối: `app.TicketService` cài `port.Tickets.Day` (tóm tắt kỳ + dòng giá tính vào bảng giá — W11.76/W11.77); `TicketHooks.Board` (trạng thái bảng giá ngày: open / approving / approved / reopened — nil = Chưa duyệt; khi nối, giá tham chiếu chỉ lấy ngày đã duyệt, Cấp Tỉnh chỉ sửa khi Chưa duyệt / Đang mở duyệt, "Mở lại" cảnh báo chỉ khi Đang mở duyệt); `price.New` nối `PointHooks.Submitted` (nhắc lần 2 bỏ xã đã gửi; ngưng xã hôm nay khi đã gửi) và `ItemHooks.HasPrice` / `HasSheets` (khóa ĐVT mặt hàng đã có giá; thay đổi danh mục hôm nay khi đã có phiếu gửi). `port.TablePDF` ← `pf/reports` (`Module.UseTablePDF`) cho "Tải PDF" phiếu.
- HTTP (`handler/ticket.go`): `GET /v1/price/tickets`, `GET|POST /v1/price/tickets/sheet`, `POST /v1/price/tickets/proxy`, `GET /v1/price/tickets/history|export|print`, `GET /v1/price/tickets/{id}`, `GET /v1/price/tickets/{id}/log`, `POST /v1/price/tickets/{id}/correct|redate|delete`, `GET /v1/price/alerts`, `POST /v1/price/alerts/request|respond|decide`. Web-ops: `/kn/bao-gia/nhap` (Xã, thẻ theo mặt hàng trên điện thoại), `/kn/bao-gia/xac-minh`, `/kn/bao-gia/lich-su`, `/kn/bao-gia/kiem-duyet` (Cấp Tỉnh), `/kn/bao-gia/in` (phiếu File 2 khổ A4); menu Cấp Xã "Nhập báo giá", "Yêu cầu xác minh", "Lịch sử báo giá" và Cấp Tỉnh "Kiểm duyệt & công bố" mở.
- Chưa làm: thông báo chuông / email cho yêu cầu xác minh, phản hồi, kết quả xử lý (cần sự kiện mới + consumer `pf/notify`); tệp minh chứng khi nhập thay (`cap/files`); dạng Lịch của Lịch sử báo giá; nhập trên `apps/field-app` (web responsive thay thế).

### W11.76 duyệt / mở duyệt bảng giá ngày (13/10/2026, pf 000151, 000152)

- pf 000151 (chỉ `pf_price`): sổ `pf_price.board` (chuỗi `board:<profile>:<ngày>`; thao tác lock / unlock / approve / reopen / cancel_reopen; trạng thái open / approving / approved / reopened; `board_version` = phiên bản đã duyệt hiện hành; khóa "Đang duyệt" có `lock_until` + `base_status` — hết hạn là phép chiếu lúc đọc, không job), `board_line` (dòng CT-04 của MỘT phiên bản: min / max toàn tỉnh `numeric(14,2)`, trung bình P `numeric(16,4)`, số xã; tên / nhóm / ĐVT chụp tại ngày), `board_ticket` (phiếu Đã gửi / Nộp muộn của phiên bản, `ticket_seq`, mã ĐVHC xã chụp tại ngày). Phiên bản `approve` LƯU khóa tuần (`week_key`, `week_year`, `week_from`, `week_to` — lịch kỳ hệ thống W11.03 qua `port.Weeks` ← `pf/extension.ReportWeekOf` ở gốc lắp ráp; không nối → QĐ-6 mặc định) và `days_scheduled` (lịch W11.74 của tuần).
- pf 000152 (chỉ `pf_price_pub`): `v1_board_daily`, `v1_ticket_week` đúng cột hợp đồng W11.10 (đã `published`), `GRANT SELECT … TO r_price_pub`; `//ag:pubview` trong `port/port.go`. RLS bảng nguồn là `p_tenant` chuẩn của `apply_ledger` (hàm `k_ref.ctx_*` đã cấp `r_agg` ở pf 000137) — không cấp thêm.
- `app` (board.go): `LockBoard` ("Duyệt bảng giá" — Đang duyệt 10 phút, một người: khóa cố vấn theo tenant + bên dùng + ngày, 409 `price.board_busy`), `UnlockBoard`, `ApproveBoard` (đã qua giờ khóa, 0 cảnh báo chưa xử lý, có dòng giá; khóa lạc quan `seq`; ảnh chụp dòng + phiếu; outbox `pf.price.board.published` v1), `ReopenBoard` (lý do, trong `Profile.Board.ReopenDays` = 30 — Q-086 tạm), `CancelReopen` (chỉ khi chưa có phiên bản phiếu / dòng / cảnh báo nào sau lúc mở duyệt), `GetBoard`, `ListBoards`; quyền: chỉ Cấp Tỉnh có cờ QTNV báo giá (`Profile.StewardDuty` — D11, CEO quyết; khác → 403 `price.board_steward_required`) duyệt (Admin chỉ khi `Profile.Board.AdminApproves`), TW / Xã chỉ thấy phiên bản đã duyệt. `port.Boards.Published` cho W11.77.
- `price.New` nối `TicketHooks.Board` (Cấp Tỉnh sửa / nhập thay / xác minh chỉ khi Chưa duyệt / Đang mở duyệt; Đang duyệt chặn sửa) và `TicketHooks.Approved` (mới — giá tham chiếu CT-02 chỉ từ ngày đã có phiên bản duyệt, kể cả đang mở duyệt: ngày chưa duyệt dùng giá mới — BR-05 FN-TI-02.06). `port.Tickets.Day` tách thành `daySummary` dùng chung trong giao dịch duyệt.
- HTTP: `GET /v1/price/boards`, `GET /v1/price/boards/{day}`, `POST /v1/price/boards/{day}/lock|unlock|approve|reopen|cancel-reopen`. Web-ops: khối "Bảng giá ngày" trên `/kn/bao-gia/kiem-duyet` (nhãn trạng thái, tóm tắt kỳ, Xem trước, Duyệt — vô hiệu kèm lý do / số cảnh báo, Xác nhận / Hủy, Mở duyệt có lý do, Hủy mở duyệt, Nhật ký); giả lập `src/api/mock/kn-price-boards.ts`. Không đổi menu.
- Chưa làm: Bảng giá tổng hợp / dashboard cho Cấp TW (W11.77), đăng website (W11.81 — nghe sự kiện), thông báo khi duyệt / mở duyệt.
- Gói NOTIF (13/10/2026, pf 20261013070025 — `pf_notify`): pf/price phát sự kiện vòng đời cho thông báo (`port/events.go`: `pf.price.alert.raised`, `verification.requested|responded|decided`, `lock.summarized`, `board.reopened`) trong cùng giao dịch với dòng sổ; chỉ mã tham chiếu cán bộ Xã / Cấp Tỉnh của xã tại ngày báo giá, không văn bản tự do. pf/notify chọn người nhận (thêm QTNV qua `PriceDesk`), chuông + email theo người nhận. Thông báo khi duyệt / mở duyệt đã có.

### W11.77 báo cáo giá (13/10/2026, pf 20261013133657, 20261013133703)

- Báo cáo chỉ đọc bảng giá ĐÃ DUYỆT qua `port.Boards` — thêm `PublishedBetween` (khoảng ngày) và `PublishedBoard.PointLines` (giá từng xã của CHÍNH phiên bản). pf 20261013133657 (`pf_price`): sổ `board_point` (chuỗi `bpoint:<profile>:<ngày>:v<N>:<xã>:<mặt hàng>`) ghi cùng giao dịch `ApproveBoard`; nhóm vùng chụp tại ngày báo giá. Ngày đang mở duyệt vẫn trả phiên bản cũ ("Đang cập nhật"); phiên bản duyệt trước migration không có ảnh chụp → dựng lại từ phiếu khi ngày không mở duyệt (bảng giá Đã duyệt chặn mọi sửa phiếu nên số liệu khớp).
- `app/report.go`: Bảng giá tổng hợp (FN-TW-01.01; CT-04 theo nhóm vùng qua `domain.AggregateBoard`, tăng/giảm so bảng công bố liền trước, ngưỡng đi ngang = `AlertParams.FlatPct`), Biến động (FN-TW-01.02; kỳ ngày / tuần — `port.Weeks` / tháng, giá kỳ = trung bình Giá trung bình các ngày công bố, số lần vượt ngưỡng = cảnh báo đã chốt), Dashboard vùng (FN-TW-01.03). `app/report_ticket.go`: Tiến độ nhập giá (FN-TI-02.01; trạng thái chiếu như Chi tiết báo giá, lần nhắc dựng lại từ `calendar_run`), Thống kê phiếu theo cán bộ Xã (FN-TI-02.08) + khóa tháng (Q-085: sổ `stat_month`, ảnh chụp `stat_month_line`; chỉ QTNV — D11).
- Xuất Excel qua `tabular.WriteXLSX`, PDF biến động qua `port.TablePDF`; mọi lần xuất qua điểm kiểm soát xuất (`meter.Guard`, kênh `report_export`, Internal — `Module.UseEgress`).
- Sự kiện mới `pf.price.ticket.proxied` v1 (nhập thay — FN-TI-02.09 hậu điều kiện); pf/notify loại `price_proxy_entry` (pf 20261013133703), chuông cho cán bộ Xã phụ trách, không email.
- HTTP `GET /v1/price/reports/{progress,summary,movement,dashboard,stats}[/export]`, `GET /v1/price/reports/stats/detail`, `POST /v1/price/reports/stats/{close,reopen}`. Web-ops: `/kn/bao-gia/{tien-do,bang-gia,bien-dong,dashboard-vung,thong-ke}`; menu Go + web mở các mục tương ứng.
- Chưa làm: báo cáo giá thành (FN-TW-01.04 — W11.78), đăng website (W11.81); "Nhập thay" từ Tiến độ chỉ dẫn sang Kiểm duyệt & công bố (chưa mở sẵn hộp nhập thay).

