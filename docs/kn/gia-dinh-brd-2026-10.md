# Giả định của đợt W11 (BRD Khuyến nông 10/2026)

Product owner **chưa trả lời** các quyết định QĐ-1…QĐ-10 và câu hỏi Q-0xx trong [đối chiếu BRD KN 10/2026](doi-chieu-brd-kn-2026-10.md) (§2, §4). Trong lúc chờ, các gói W11 làm theo cột **"Đề xuất của dev"** của tài liệu đó. Chỗ nào làm rẻ được thì lựa chọn được giữ thành tham số có phiên bản hoặc seed, để đổi khi PO trả lời mà không phải viết lại mã.

Mỗi gói ghi ở đây một mục gồm: câu hỏi (QĐ/Q), phương án đã chọn, chỗ nó nằm trong mã/tài liệu, và cần làm gì nếu PO chọn khác. Khi PO trả lời, sửa dòng tương ứng và ghi ngày.

Mặc định chung của đợt (theo hướng dẫn đợt W11):
- **QĐ-6:** tuần báo cáo Thứ Tư → Thứ Ba; KSGM 4 tuần/tháng chạy song song; số liệu nguồn sửa được tới khi chốt; KHTC đọc lũy kế của module nguồn.
- **QĐ-5:** phương án (a), G1 gồm Trồng trọt và vận hành Báo giá hằng ngày.
- **QĐ-7:** theo đề xuất của từng câu ở §4.5 và §4.7.

## W11.01 — Tài liệu KN

| Câu hỏi | Phương án đã chọn | Ở đâu | Nếu PO chọn khác |
|---|---|---|---|
| Q-049 (mã FN/UR trùng giữa các BRD) | Thêm tiền tố module (đề xuất của dev): `TT-` Trồng trọt, `HC-` HCTH, `CN-` CNTS, `KH-` KHTC, `BG-` Báo giá. Ma trận nghiệm thu Trồng trọt dùng `TT-` ở cột "Mã" và cột "FN" (mục 3–7). Lời văn, cột tham chiếu và tên kiểm thử `TestFN_…` giữ mã cũ, hiểu là `TT-` | `docs/uat/kn-v7.7-acceptance.md` (ghi chú đầu tài liệu); `docs/kn/loi-van-ban-brd.md` §5 | Nếu BA dùng tiền tố khác: thay chuỗi trong cột "Mã"/"FN" của ma trận. Tên kiểm thử không đổi |
| Q-052 (lỗi văn bản, mục thiếu) | Chưa có trả lời của BA. Dev làm theo cột "Cách hiểu tạm" trong danh sách lỗi (vd FN-TI-05 hiểu là FN-TI-02.03; "Mục VI" hiểu là Mục V; Báo giá Mục VII: mở duyệt 30 ngày, lịch T2–T6) | `docs/kn/loi-van-ban-brd.md` | Sửa dòng tương ứng và báo các gói bị ảnh hưởng (W11.02, W11.71–W11.76) |
| QĐ-6 (lịch tuần) — chỉ phần tài liệu | Danh sách lỗi ghi CNTS v3.7 (tuần Thứ Ba – Thứ Hai) là văn bản chưa cập nhật theo KHTC (Thứ Tư – Thứ Ba) | `docs/kn/loi-van-ban-brd.md` CN-1 | Nếu PO chọn Thứ Ba – Thứ Hai: chuyển CN-1 sang KHTC. Mã kỳ thuộc W11.03 |
| D3 T0 (menu mobile "đã có?") | Xác minh trong mã thay vì chờ: `kn_menu_service.dart` gọi `GET /v1/menu`, `kn_context_picker_screen.dart` chọn ngữ cảnh, có kiểm thử Flutter. UR-CH-01, UR-KT-02, FN-CH-00.01 chuyển sang **đạt** | `docs/uat/kn-v7.7-acceptance.md` | Nếu kiểm trên máy thật thấy thiếu: đưa về **một phần** và ghi phần thiếu |
| "Báo cáo đột xuất" | Thêm dòng `TT-FN-AD-04.06-DX` (**một phần**): menu và mobile Cấp Xã đã có, web thiếu trang `/kn/bao-cao/dot-xuat`. Màn hình thuộc W11.02 | `docs/uat/kn-v7.7-acceptance.md` mục 7 | — |
## W11.06 — Mở phòng ban + menu kn.menu/v3

| Câu hỏi | Phương án đã chọn | Đổi ở đâu nếu khách chọn khác |
|---|---|---|
| Q-051 (QĐ-7) tên và mã phòng ban | 5 phòng ban BRD nêu tên: `trong_trot`, `chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, `thong_tin_truyen_thong`, `hanh_chinh_tong_hop`. Tên "Phòng Thông tin Tuyên truyền" theo BRD của chính phòng đó (Báo giá v1.3) và KHTC v2.2; **mã giữ** `thong_tin_truyen_thong` (đã có từ v7.7, mã là định danh, không phải tên) | Tên: Admin sửa phòng ban hoặc sửa seed JSON. Mã: chỉ thêm bí danh mới trong `port.legacyDeptAliases`, không đổi mã cũ |
| Q-051 bỏ `phong_ban_5/6` | `phong_ban_5` → `hanh_chinh_tong_hop` (bí danh); `phong_ban_6` bỏ, không kế nhiệm (không BRD nào nêu phòng thứ 6) | `port.legacyDeptAliases`; nếu khách nêu phòng thứ 6, thêm mã mới vào seed và trỏ `phong_ban_6` về mã đó |
| Q-073 HCTH cùng app hay app riêng | Cùng app KN, cùng tenant (đề xuất của dev): HCTH là một phòng ban trong `web-ops` / app di động, menu theo phòng ban | Nếu tách app: thêm `app/HC`, khuôn `hanh_chinh_tong_hop` vẫn dùng được |
| Q-050 đăng nhập | Giữ luồng v7.7 (chọn vai trò + phòng ban sau xác thực); W11.06 không đổi đăng nhập | — |
| Trạng thái seed của 4 phòng ban mới | "Chưa mở" — Admin mở khi module của phòng sẵn sàng (FN-03 AC-01). Mở sớm thì người dùng thấy khung báo cáo theo kỳ chung đang chạy + nhóm "Nghiệp vụ" toàn "Sắp có" | Seed JSON (`status`) hoặc Admin đổi trạng thái |
| Module chưa có trong menu | Hiện mờ "Sắp có" (không ẩn sau cờ) | `domain.MenuItem.ComingSoon`; gói module bỏ cờ khi màn sẵn sàng |
| Nhóm "Dùng chung" | Cuối menu mọi đối tượng (kể cả Admin), hiện có Contact (HCTH FN-13) "Sắp có" | `domain.SharedGroup` |
| Khuôn của phòng ban mới | Khung báo cáo theo kỳ chung (giữ màn đang chạy và trang mặc định của vai) + nhóm "Nghiệp vụ" theo §VI.3 từng BRD | `domain/menu_departments.go`; gói module có thể thay khung chung bằng menu riêng của phòng |
## W11.71 — Khung `pf/price` (ADR-0049, pf 000138)

| Câu hỏi | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| QĐ-5 / Q-044 — Báo giá có vào G1 15/12? | (a) G1 = Trồng trọt + Báo giá vận hành ngày: `pf/price` ghép vào `app/KN`, `enable: per_tenant` mặc định bật | `app/KN/app.yaml`, `internal/platform/price/module.yaml` | (b): giữ module, tắt cho tenant qua cổng bật module (`pf_party.module_enablement_event`) — không sửa mã |
| Q-082 — Chủ danh mục mặt hàng giá | Một danh mục ở `cap/price` (Phòng TTTT), KHTC đọc qua `price/port.Catalog` | ADR-0049 §3 | Nếu KHTC là chủ: `pf/price` đọc danh mục KHTC qua port, giữ sổ của mình cho lịch sử |
| Mã phòng ban Phòng TTTT (Q-02, mâu thuẫn tên "Thông tin Tuyên truyền" / "Thông tin – Truyền thông") | `thong_tin_truyen_thong` trong `KNPriceProfile.Department` (chỉ thông tin, khung chưa lọc theo phòng ban) | `cmd/api-platform/assembly/price.go` | Sửa một hằng ở gốc lắp ráp |
| Ai ghi danh mục nền ở khung | Chỉ Admin KN (`ManageBy: [admin]`); cờ "Quản trị nghiệp vụ báo giá" của Cấp Tỉnh ở W11.72 | `KNPriceProfile.ManageBy` | W11.72 mở rộng `port.Profile` |
| Đơn vị tính mặc định (BRD không liệt kê) | kg, tạ, tấn, lít, con, trái, bao, chai | `price/domain.DefaultUnits` | Chủ danh mục thêm/ngưng mã (phiên bản mới `retired`); seed chỉ ghi mã chưa có |
| 5 nhóm hàng mặc định | Theo BRD FN-AD-04.03: `lua_gao`, `rau_mau`, `chan_nuoi`, `thuy_hai_san`, `vat_tu_nong_nghiep` | `price/domain.DefaultGroups` | Như trên |
| Ngày hiệu lực danh mục | `valid_from` = ngày báo giá theo giờ VN (BR-02: áp dụng từ ngày báo giá kế tiếp); seed mặc định hiệu lực từ hôm nay | pf 000138, `price/domain.BusinessDay` | Lịch báo giá thật (ngày nghỉ, Q-086) ở W11.74 |
## W11.03 — Lịch kỳ hệ thống dùng chung

| Câu hỏi | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Q-045 / QĐ-6 — tuần báo cáo | Tuần **Thứ Tư – Thứ Ba** (KHTC v2.2 GD-05, BR-DL-01), tuần 1 = tuần chứa 01/01 (năm 52/53 tuần), nhãn "Tuần 41/2026". CNTS (Thứ Ba – Thứ Hai) vẫn chọn được bằng `week_start=tue`. | Tham số `extension.report_period_rule` (Text = tag quy tắc), phiên bản mới có ngày hiệu lực |
| Q-045 — ngày áp dụng | Phiên bản nạp sẵn (masterdata `seeddata/params.json`) hiệu lực **04/11/2026** (Thứ Tư đầu tiên sau hạn chốt QĐ-6 31/10). Đợt bắt đầu trước ngày đó và mọi đợt đã phát hành giữ quy tắc v1 (tuần thứ Hai – Chủ nhật), không đánh số lại. | Phê duyệt phiên bản tham số khác (ngày hiệu lực khác) |
| Q-045 — KSGM 4 tuần/tháng | Giữ song song: loại kỳ `ksgm_week`, mặc định 1–7, 8–14, 15–21, 22–cuối tháng; Cấp Tỉnh sửa ranh giới từng tháng. | Tham số `extension.ksgm_week_starts:YYYY-MM` (vd "1,10,16,23") |
| BR-DL-01 — tháng/quý/năm | Gồm các tuần báo cáo có Thứ Ba (ngày cuối tuần) thuộc kỳ (`month=week_end`), áp cho mọi module. `month=calendar` giữ lựa chọn dương lịch. | Tag quy tắc |
| Hạn nộp mặc định v2 (BRD chưa thống nhất) | Tuần / tuần KSGM: 12:00 ngày sau ngày cuối kỳ (CNTS H04); ngày: 09:00 hôm sau (CN-06); tháng/quý/năm theo Thứ Ba cuối: 10:00 Thứ Tư kế tiếp (KHTC II.4); vụ/đột xuất: +7 ngày 17:00. Người phát hành vẫn chọn hạn khác. | `PeriodRule.DefaultDeadline` (mã) |
| Đợt vắt qua ngày hiệu lực | Cả đợt theo quy tắc có hiệu lực tại ngày đầu đợt (một quy tắc mỗi đợt, ghi ở `period_rule`). | — |

Chưa làm trong gói này: lưu `Key`/`WeekYear` của kỳ vào DB (không có migration); loại báo cáo ngày/quý/KSGM ở
"Phát hành biểu" Trồng trọt (chờ W11.05/W11.42).

## W11.02 — Đóng khoảng trống Trồng trọt (D3 T1)

| Câu hỏi / mục | Giả định đã lấy | Đổi ở đâu |
|---|---|---|
| Q-064 (Q-TT-3) vẽ thửa trên Web | **Hoãn**: chấp nhận "Web xem, Mobile vẽ" (đề xuất của dev). `PlotsScreen` web giữ danh sách + `GapNotice`; vẽ ở app di động (`boundary_screen.dart`) | Làm khi khách bắt buộc: màn vẽ web dùng `/v1/kn/plots` (máy chủ đã đủ: chồng lấn, tối thiểu 3 điểm, KML/GeoJSON) |
| Q-060 (Q-TT-2) lệch A1–A5 | Giữ nguyên các lệch nội bộ (khóa hẳn sau 3 lần khóa tạm, liên kết kích hoạt, liên kết tải 7 ngày, vẽ tối thiểu 3 điểm) | — |
| "Báo cáo đột xuất" | Một trang `/kn/bao-cao/dot-xuat` theo vai: Cấp Xã = công việc kỳ `adhoc`; Cấp Tỉnh/Admin = tổng hợp đợt `adhoc`; Cấp Quốc gia = chỉ xem. Màn phát hành thêm loại kỳ "Đột xuất" (một kỳ). Menu vẫn chỉ hiện khi phòng ban có mẫu loại `dot_xuat` | `apps/web-ops/src/app/kn/bao-cao/dot-xuat/page.tsx` |
| Nạp Tỉnh giao từ Excel (UR-TI-02) | Tệp "rộng": một dòng một xã, cột "Mã xã" + mỗi cột Tỉnh giao tiêu đề "<Mã biểu> <Chữ cột>" (một biểu thì chỉ chữ cột). Máy chủ chỉ đọc tệp (shared/tabular, ô công thức bị từ chối), trả giá trị cho Bước 4; phát hành gửi `provincial_source = excel`. Tệp mẫu CSV UTF-8 dựng ở trình duyệt từ danh sách xã của "Xem trước". Tối đa 500 dòng, 2 MB | `reportcycle/app/provincial_import.go` |
| Nạp Excel tài khoản nhiều vai trò (UR-AD-04) | Mỗi dòng = một lần gán vai trò; các dòng cùng Email = một tài khoản. Vai trò nhận mã hoặc nhãn ("Cấp Xã"…); vai Admin không nạp qua tệp. "Mã Nút" = mã tổ chức hoặc UUID. Phòng ban/xã phân tách ";". Một dòng lỗi → loại cả tài khoản (không tạo tài khoản thiếu vai). Tối đa 300 dòng | `accounts/app/kn_import.go` |
| Tab Phiên / Lịch sử đăng nhập | Đọc trực tiếp Keycloak Admin REST, không lưu ở pf. Realm bật lưu sự kiện `LOGIN`, `LOGIN_ERROR`, `LOGOUT`, `CODE_TO_TOKEN_ERROR`, giữ **1 năm** (theo N-13 "nhật ký truy cập ≥ 1 năm"); tài khoản dịch vụ thêm `view-events`. Realm production đã nhập phải áp tay (Realm settings › Events) | `deploy/compose/keycloak/agrigreen-realm.json`, `docs/brd/kn-p1-realm-patch.md` |
| Đếm theo phòng ban (FN-AD-04.03) | `record_count` = tổng bản ghi mang `department_code` trong tenant (mã phòng ban duy nhất trong tenant, không tách theo đơn vị): công việc báo cáo, lịch công tác, ghi nhận vật tư, mẫu báo cáo, lần gửi Chi cục, dòng máy móc HTX, thửa | `pty.UseDepartmentUsage(...)` ở `cmd/api-platform/assembly/modules.go` |
| X-12 dữ liệu cũ không phòng ban | Thửa `department_code IS NULL` (có trước v7.7) đọc qua KN là **Phòng Trồng trọt** (`store.LegacyKNDepartment`), không còn hiện ở mọi phòng ban; không UPDATE dữ liệu. Các bảng KN khác đều `NOT NULL` nên không có dữ liệu cũ | `cooperative/plot/store/kn.go` |
| X-02 PII ở ô tự do | Chỉ **cảnh báo** (không chặn) khi ô ghi chú/lý do/nội dung chứa SĐT, email, số CCCD/CMND — nhận dạng bằng mẫu ở web (`src/kn/pii.tsx`) và mobile (`lib/domain/pii_hint.dart`); máy chủ không quét nội dung | hai tệp trên |
| Di chuyển Nút xã (FN-AD-04.02 B3) | Dùng Admin API có sẵn `POST /v1/orgs/{ref}/move` (lý do ≥ 10 ký tự, giữ lịch sử, bỏ gán khu vực cũ). Xã sang tỉnh khác mà mã ĐVHC không thuộc tỉnh mới → nhập "Mã ĐVHC mới" (máy chủ kiểm ĐVHC trong địa bàn cha) | `OrgStructureScreen` |

## W11.74 — Lịch báo giá, giờ khóa, nhắc, ngưỡng (pf 000139)

| Câu hỏi | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Q-086 / Q-052 — lịch báo giá mặc định, ngày nghỉ, ngày bổ sung | T2–T6 (mẫu BRD FN-AD-04.04 BR-04, chưa xác nhận); không seed ngày nghỉ — Cấp Tỉnh/Admin thêm ở "Lịch ngoại lệ" | `KNPriceProfile.Schedule.Weekdays` (`cmd/api-platform/assembly/price.go`) | Tenant ghi phiên bản lịch mới (`POST /v1/price/calendar/schedule`), không sửa mã; đổi mặc định ở Profile |
| Giờ nhắc lần 1, lần 2 (BRD không nêu) | 07:30 và 11:00 giờ VN; cả hai kênh (trong ứng dụng, email); mẫu "Nhắc nhập giá ngày {ngày} cho {xã}: vui lòng gửi báo giá trước {giờ khóa}." | `KNPriceProfile.Schedule`, `price/domain.DefaultReminderTemplate` | Như trên |
| Ai cấu hình lịch/ngưỡng | Vai `ManageBy` của Profile (Admin KN). Cờ "Quản trị nghiệp vụ báo giá" của Cấp Tỉnh chưa có (W11.72) | `app/calendar.go`, `app/threshold.go` | W11.72 mở rộng kiểm quyền ghi |
| Người nhận nhắc | Chưa có xã báo giá/phân công (W11.72) và phiếu (W11.75): một thông báo chủ đề tới mọi cán bộ xã (`kn_officer`) của tenant; lần 2 không lọc được "xã đã gửi" | `port.ReminderTargets` (nil) | W11.72/W11.75 cài `ReminderTargets` và gọi `Module.UseReminderTargets` |
| Kênh email của nhắc | Cờ email ghi ở cấu hình và sự kiện; `pf/notify` CHƯA gửi email theo từng người nhận (chỉ email tóm tắt chỉ đạo) | `notify/domain/price.go` | Gói sau thêm kênh email theo người nhận ở `pf/notify` |
| Loại thông báo | Dùng lại `report_task_due` (CHECK `kind` của `pf_notify` chỉ migration của `pf/notify` sửa được; gói có một số migration) | `notifyport.KindPriceReminder` | Người tích hợp cấp số migration `pf_notify` thêm `price_reminder`, đổi hằng |
| Thêm ngày nghỉ cho ngày đã có phiếu gửi (BRD chặn) | Chỉ thêm/xóa ngoại lệ từ ngày mai (chưa có phiếu ở W11.74) | `app.AddException` | W11.75 cho phép hôm nay nếu chưa có phiếu |
| Đổi lịch trong ngày báo giá | Ngày áp dụng sớm nhất = ngày mai nếu hôm nay là ngày báo giá, hôm nay nếu không (422 `price.valid_from`) | `domain.EarliestValidFrom` | — |
| Ngưỡng mặc định | 30 %, N = 7, đi ngang 1 %, phiếu điều tra 40 % (BRD CT-02…CT-06); ngưỡng mặt hàng chỉ kiểm dạng mã (danh mục ở W11.73) | `KNPriceProfile.Schedule`, `app.SaveThreshold` | Tenant ghi phiên bản ngưỡng; W11.73 kiểm mã mặt hàng có thật |
| "Mốc duyệt bảng giá dự kiến" (FN-AD-04.04 BR-03: hạn phản hồi trước mốc duyệt) | Chưa có tham số mốc duyệt — chỉ kiểm hạn phản hồi sau giờ khóa | `domain.ValidateSchedule` | W11.76 (duyệt/công bố) thêm mốc duyệt vào lịch |
## W11.05 — `cap/forms` tổng quát đơn vị lá (pf 000140)

| Câu hỏi | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Q-081 — số lần gửi phiếu KHTC | Tối đa **3**, lần 3 tự khóa: không "yêu cầu bổ sung" được nữa, chỉ xác nhận (FN-TI-02.03/02.04) | `KNKHTCProfile.MaxSubmissions` (gốc lắp ráp); kỳ đặt riêng `max_submissions` khi mở kỳ (0 = không giới hạn) |
| Q-047 — sửa số kỳ cũ | `cap/forms` không đổi số đã chốt: ảnh chụp đóng băng giữ nguyên, sửa = mở lại kỳ (CNTS) hoặc ghi điều chỉnh ở phiếu kỳ sau (KHTC, W11.63). Lũy kế đọc từ nguồn (đề xuất "lũy kế của nguồn") — chưa làm ở gói này | W11.63, W11.10 |
| Q-045 — lịch kỳ của kỳ `cap/forms` | Kỳ native mang khóa kỳ theo lịch kỳ hệ thống W11.03 (`period.kind/key/from/to/rule`), lưu ở `pf_forms.period_spec`; bên gọi tự tính khóa bằng `reportcycle/domain` | Bên gọi (W11.42, W11.62) |
| Q-056 — ai định nghĩa biểu | Admin KN + `reports_admin` cho cả `kn_cnts`, `kn_khtc` (như `kn`) | `DefineBy` của Profile |
| Vòng đời CNTS III.3.0 (Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ) | Áp cho `cap/forms` (mọi profile — phép chiếu khi kỳ đã chốt với đơn vị) và cho vòng đời KN v1 chỉ ở phòng `chan_nuoi_thuy_san` (`reportcycle/domain.ExtendedLifecycle`); Trồng trọt giữ "Đã chốt" | `extendedLifecycle` (mã) |
| "Chờ mở kỳ" | Bản offline (`captured_at`) đến sau chốt được GIỮ (sổ `held_submission`, HTTP 202), không vào số liệu kỳ; Cấp Tỉnh mở lại kỳ rồi đơn vị nộp lại (không tự áp bản giữ) | `app.SubmitOffline` |
| Hủy kỳ (KHTC BR-DL-18) | Chỉ hủy kỳ đang mở (published/reopened); kỳ đã chốt không hủy (KHTC: "Đã đóng" là cuối); kỳ hủy không mở lại. Thu hồi đợt KN v1 ghi `cancelled` (mirror) cho mọi kỳ của đợt | `pf_forms.fn_transition` |
| Phạm vi đơn vị lá | Cơ sở GM / hộ-điểm: lọc theo mã xã cha (`unit_parents`); phòng ban: cấp lá chỉ phòng của phiên, cấp trên (Phòng KHTC) mọi phòng | `app.actor.allowsUnit` |
| Phòng ban được dùng Profile | `kn_cnts`: chỉ `chan_nuoi_thuy_san`; `kn_khtc`: mọi phòng (KHTC mở kỳ bằng khóa vai `kn_tinh@ke_hoach_tai_chinh`, phòng khác nộp) | `Departments`, `Roles` của Profile |
| Đơn vị = người, nhiều bước (HCTH, D3 §2.5) | **Chưa làm** — chừa chỗ: hằng `port.UnitPerson`, `port.ReviewFlowSteps`, cột `review_flow` (CHECK chỉ `single`). Theo đề xuất (b) của D3 §2.5, `pf/hrm` tự giữ máy trạng thái đợt; (a) thêm `person` + `steps` khi có bên dùng thứ hai | Migration mới + `domain.SupportedUnitKinds` |

Chưa làm trong gói này: mở loại kỳ ngày/quý/KSGM ở "Phát hành biểu" và lưu `period_key`/`week_year` vào
`pf_extension.report_period` — cần một migration của module `pf/extension` (archlint cấm migration `pf_forms` sửa schema
`pf_extension`; gói chỉ được cấp pf 000140). Ảnh chụp ghi song song KN đã mang `period_key` tính lại từ quy tắc lưu ở
đợt. "Chờ mở kỳ" của vòng đời KN v1 (đồng bộ offline sau chốt) để W11.42 (CN2).

## W11.73 — Mặt hàng theo thời kỳ (pf 000141)

| Câu hỏi | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Q-082 — Chủ danh mục mặt hàng giá | Một danh mục ở `cap/price` (Phòng TTTT/QTNV theo thời kỳ), KHTC đọc qua `price/port.Items` | `internal/platform/price/port/item.go` | Nếu KHTC là chủ: nạp danh mục KHTC vào sổ `pf_price.item` qua port, giữ lịch sử phiên bản |
| Cờ "Quản trị nghiệp vụ" (BR-06) chưa có | Mọi Cấp Tỉnh có quyền QTNV cho tới khi W11.72 nối `ItemHooks.Steward`; Admin KN (vai quản trị) luôn được | `app/item.go` (`steward`) | W11.72 gọi `Module.SetItemHooks` với `Steward` đọc cờ |
| "Ngày báo giá kế tiếp" (BR-02) | Ngày báo giá kế tiếp theo lịch W11.74 của bên dùng đầu tiên (`Service.NextQuoteDay`, nối ở gốc lắp ráp bằng `Module.WireItemCalendar`); không có kỳ trong 60 ngày → ngày mai | `cmd/api-platform/assembly/price.go` | — |
| "Đã có giá" / "đã có phiếu gửi hôm nay" khi chưa có phiếu | Chưa có giá, chưa có phiếu → không khóa ĐVT, cho hiệu lực từ hôm nay | `ItemHooks.HasPrice`, `HasSheets` | W11.75 nối sổ phiếu |
| Định danh mặt hàng (BR-07, AC-06) | Tên loại + giống/phẩm cấp (bắt buộc) + ĐVT; trùng = cùng nhóm, tên, giống/phẩm cấp, quy cách (bỏ hoa thường, khoảng trắng thừa) | `domain.IdentityKey` | — |
| Mã mặt hàng | `MH` + 5 chữ số tăng dần theo tenant | `domain.ItemCode`, `store.NextItemNumber` | Đổi định dạng cần migration (CHECK `^MH[0-9]{5}$`) |
| Khóa khi đã có giá (BR-04) | Khóa cả giống/phẩm cấp và loại (Cơ bản/Địa phương) ngoài ĐVT, quy cách, nhóm — chỉ sửa tên, nơi bán, thứ tự | `ItemService.ReviseItem` | — |
| "Thay thế cho" | Mặt hàng cũ tự ngưng từ ngày hiệu lực của mặt hàng mới (lý do "Thay thế bởi …"); xã của mặt hàng Địa phương cũ chuyển sang mặt hàng mới nếu không chọn xã | `ItemService.CreateItem` | — |
| Bộ theo vụ (BR-08) | Mặt hàng thuộc ≥ 1 bộ đang hiệu lực chỉ xuất hiện ở xã khi có bộ chứa nó đang bật; mặt hàng không thuộc bộ nào không bị ảnh hưởng; ngưng bộ = bỏ chi phối | `domain.Gate` | — |
| Xã báo giá (W11.72 song song) | `point_code` = mã xã báo giá, mặc định mã ĐVHC xã, không khóa ngoại; xã của Cấp Xã khi đề xuất do client gửi | `pf_price.item_assignment`, `item_proposal` | W11.72 kiểm mã và lấy xã của Cấp Xã từ phân công |
| Thay đổi nhóm hàng | Thêm/sửa tên/xóa nhóm hiệu lực từ hôm nay (không đổi danh sách mặt hàng của phiếu) | `ItemService.CreateGroup`, `groupVersion` | — |
| Nhập danh mục từ Excel, thông báo kết quả đề xuất | Chưa làm (chờ `cap/import`, nối `notify`) | — | Gói sau |

## W11.07 — Phân quyền trong vai + đơn vị nội bộ

| Câu hỏi | Phương án đã chọn | Ở đâu | Nếu PO chọn khác |
|---|---|---|---|
| Q-059 danh mục cấp quản lý chung | Một danh mục cho mọi module: Chuyên viên - Cán bộ (`chuyen_vien`), Phụ trách (`phu_trach`, mới), Phó trưởng phòng, Trưởng phòng, Lãnh đạo đơn vị (`lanh_dao_don_vi`, mới); giữ `pho_giam_doc`, `giam_doc` (dữ liệu v7.7). Nhãn "Chuyên viên" đổi thành "Chuyên viên - Cán bộ" (mã không đổi). "Lãnh đạo" (`Manager`) = mọi cấp ≥ phó trưởng phòng, kể cả Phụ trách | `accounts/port.MgmtLevels`, phiên bản `accounts.kn-roles/2` (`GET /v1/accounts/mgmt-levels`) | Sửa bảng trong mã, tăng phiên bản; mã đã ghi sổ không đổi |
| Q-059 "Lãnh đạo đơn vị xem mọi phòng ban, chỉ đọc" có áp cho Module 1? | **Có** (Q-059 không có đề xuất của dev; chọn theo BR-04 HCTH cho thống nhất): lần gán Cấp Tỉnh có `lanh_dao_don_vi` chọn được mọi phòng ban (Đang áp dụng) của đơn vị ở chế độ **chỉ xem**; phòng ban trong lần gán vẫn ghi được. Ngữ cảnh chỉ xem bị chặn mọi thao tác ghi ở middleware (`accounts.context_read_only`) và `KNContext.ReadOnly()` = true | `domain.ViewOnlyDepartment`, middleware `pf/accounts` | Nếu không áp cho Module 1: thêm điều kiện phòng ban vào `ViewOnlyDepartment` |
| 5 nhóm Cấp Tỉnh (D4 X3) | Thuộc tính của lần gán `kn_tinh` (không phải vai mới): `vien_chuc`, `tong_hop`, `chu_nhiem`, `lanh_dao_phong`, `ban_giam_doc`. Nhóm "Lãnh đạo phòng" = lãnh đạo dù cấp quản lý chưa đặt; "Ban Giám đốc" = **luôn chỉ xem** (kể cả phòng ban của mình) và đọc được mọi phòng ban như Lãnh đạo đơn vị | `port.ProvinceGroups`, sổ phụ `pf_accounts.assignment_attr_event` | Đổi danh mục trong mã (phiên bản) |
| Cờ QTNV báo giá, người phụ trách biểu | Cờ nhiệm vụ trên lần gán Cấp Tỉnh: `qtnv_bao_gia`, `phu_trach_bieu` → `KNContext.Duties`, `HasDuty`. "Người phụ trách biểu" ở đây là **đủ điều kiện được chọn**; việc gán người phụ trách cho từng đợt phát hành thuộc gói biểu (W11.42/W11.05). `pf/price` đọc cờ QTNV qua `port.Profile` ở W11.72 | `authz.DutyPriceAdmin`, `authz.DutyFormOwner` | — |
| Ủy quyền có hạn | Lãnh đạo phòng (ngữ cảnh Cấp Tỉnh, `Manager`, không chỉ xem) giao `mo_lai_ky` hoặc `duyet_bao_cao` cho cán bộ Cấp Tỉnh cùng đơn vị, cùng phòng ban, **tối đa 30 ngày** (tạm — BRD chỉ nói "có thời hạn"); hết hạn tự mất hiệu lực (không job); thu hồi bởi người giao hoặc Admin. Lãnh đạo phòng mặc định có cả hai quyền (`KNContext.Can`) | `POST /v1/me/delegations`, `domain.DelegationMaxDays`; DB trần cứng 366 ngày | Đổi `DelegationMaxDays` / danh mục `port.Powers` |
| Phạm vi Cấp Xã theo cơ sở/hộ | Giới hạn **thu hẹp** trong xã phụ trách: `facility_refs`, `household_refs` trên lần gán `kn_xa`; cả hai rỗng = cả xã. Không đổi `app.xa_ids`/chặn Nút (Cấp Xã vẫn bắt buộc Nút). Mã cơ sở chưa kiểm với danh mục (cơ sở giết mổ thuộc W11.41) | `KNContext.AllowsFacility/AllowsHousehold` | Gói CNTS kiểm thêm mã thuộc xã khi có danh mục |
| Q-067 lãnh đạo các phòng làm HCTH ở phiên nào | Đề xuất của dev: menu "Nhân sự của phòng" (`tinh.chung.nhan_su_phong`, nhóm "Dùng chung", "Sắp có") hiện ở **mọi** phòng ban khi ngữ cảnh là lãnh đạo; không gán thêm phòng ban HCTH | `domain/menu_departments.go` `sharedByAudience`, lọc `MenuItem.Needs` | Nếu chọn gán phòng ban HCTH: bỏ mục, dùng menu HCTH |
| Q-068 danh mục đơn vị nội bộ | Đề xuất của dev là "mở rộng `department` với loại tram/trai". **Hiện thực khác một chút:** danh mục đơn vị nội bộ riêng (`pf_accounts.internal_unit`: BGĐ, văn phòng, phòng, trạm, trại, tăng cường) mà đơn vị loại `phong` **trỏ mã phòng ban P2**. Lý do: chèn trạm/trại vào `pf_party.department` làm chúng hiện ở bước chọn phòng ban, menu, số tài khoản theo phòng ban (W11.06); gói chỉ có một số migration (pf 000142) nên đặt ở schema `pf_accounts` (archlint: một migration — một module). Seed: Ban Giám đốc + 5 phòng; trạm/trại Admin thêm khi có danh sách. Thứ tự NS01 = `sort_order` | `POST /v1/accounts/internal-units/seed`, màn Admin › Phòng ban › "Đơn vị nội bộ" | Nếu PO muốn trong `department`: chuyển dữ liệu sang `pf_party` ở gói sau (W11.21/W11.27), mã giữ nguyên |
| HS-37 "cán bộ xã được phân công" | Đơn vị nội bộ của lần gán Cấp Xã = **đơn vị quản lý**; phạm vi nhân sự của lãnh đạo (`port.KNPeople.PeopleInScope`) = người Cấp Tỉnh cùng đơn vị nội bộ + cán bộ Cấp Xã có đơn vị quản lý đó. HCTH (phòng ban `hanh_chinh_tong_hop`), Admin, Lãnh đạo đơn vị, Ban Giám đốc = toàn bộ | `accounts/app/kn_attrs.go` | — |
| `KNContext.UnitRef` (D3 H0) | `authz.KNContext.UnitRef` đã có nghĩa "Nút sở hữu phòng ban" (P1) — **không đổi nghĩa**; "UnitRef nội bộ" hiện thực là trường mới `KNContext.InternalUnit` (mã đơn vị nội bộ), cũng có ở `accounts/port.KNContext` | `internal/shared/authz/kn.go` | — |
| Vai khách "Phòng nhận báo cáo" (X3, "nếu giữ") | Chưa làm (chờ PO giữ hay bỏ) | — | Thêm vai/nhóm khi PO trả lời |
## W11.05b — Kỳ ngày / quý / tuần KSGM ở "Phát hành biểu" (pf 000143)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Tên giá trị `period_type` mới | `day`, `quarter`, `ksgm_week` — cùng dạng danh từ với `week`/`month`/`season`/`year`/`adhoc` đã có ở `report_batch.period_type` và với `reportcycle/domain.Period*`, `pf_forms.period_spec.period_kind`. KHÔNG thêm loại báo cáo `daily`/`quarterly`/`ksgm_weekly`: loại báo cáo do `pf/reports` gán mẫu (CHECK `report_type` của `pf_reports.form_assignment_event`, schema khác) | Migration `pf_reports` + `domain.ReportPeriodTypes` nếu PO muốn loại báo cáo riêng |
| Q-045 — loại kỳ nào đi với loại báo cáo nào | Báo cáo tuần: tuần báo cáo (mặc định) / tuần KSGM / ngày; tháng: tháng / quý; năm: năm / quý; vụ: một kỳ; đột xuất: một kỳ / ngày. Bỏ trống `period_type` = mặc định cũ (Trồng trọt không đổi) | `reportcycle/domain.ReportPeriodTypes` (Go), `REPORT_PERIOD_TYPES` (mock), `PERIOD_CHOICES` (màn Phát hành) |
| Kỳ mới với lịch kỳ v1 | Từ chối (422 `period_type`): ngày/KSGM/quý chỉ chia được khi tham số `extension.report_period_rule` tại ngày đầu đợt là v2 (nạp sẵn từ 04/11/2026) | `domain.PeriodRule.PeriodOf` |
| Ranh giới tuần KSGM (QĐ-6, CNTS H04) | Tham số `extension.ksgm_week_starts:YYYY-MM` có hiệu lực tại ngày 1 của tháng, tra LÚC phát hành; vắng/sai → 1, 8, 15, 22. Sửa tham số sau khi phát hành không đổi kỳ đã chia | Cấp Tỉnh ghi phiên bản tham số (pf/masterdata) trước khi phát hành tháng đó |
| Lưu khóa kỳ | Sổ phụ `pf_extension.report_period_key` (append-only, một dòng một kỳ, cùng giao dịch phát hành): `period_key`, `week_year`, loại kỳ + quy tắc chép từ đợt. Không có dòng với vụ/đột xuất (và năm v1). Kỳ phát hành trước pf 000143 không được nạp ngược (migration không UPDATE/tính khóa bằng SQL) → API trả `period_key = null`, ảnh chụp `cap/forms` tính lại từ `period_rule` như W11.05 | `forms_mirror.periodKey` |
| Hạn mặc định kỳ mới | Theo W11.03 v2: ngày → 09:00 hôm sau; tuần KSGM → 12:00 ngày sau ngày cuối kỳ; quý theo Thứ Ba cuối → 10:00 Thứ Tư kế tiếp | `PeriodRule.DefaultDeadline` |
| Số khe kỳ của biểu | Không đổi: đợt nhiều kỳ hơn số khe kỳ của mẫu bị từ chối khi phát hành (cảnh báo khi xem trước) — kỳ ngày dài cần mẫu không có khối kỳ hoặc đợt ngắn | Q-01 |

Chưa làm: app di động (`field-app`) chưa hiển thị `period_key`; lọc công việc theo `period_type` mới ở màn Cấp Xã (`TasksScreen`) chưa thêm lựa chọn.

## W11.10 — Hợp đồng nguồn module `_pub` (pf 000144)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| QĐ-6 — "nguồn đã chốt" là gì | Ảnh chụp đã chốt: lần tổng hợp các xã (bất biến) mới nhất của kỳ × biểu ghi không muộn hơn lần chốt gần nhất chạm biểu (chốt, tự chốt, chốt lại). Số tạm khi đang mở lại không lọt ra; nguồn sửa được tới khi chốt | View `pf_extension_pub.v1_consolidation_closed` (đổi ý nghĩa = `v2`) |
| Q-048 — "nguồn đã chốt cho tuần X" | Đề xuất dev: **mọi** biểu của **mọi** kỳ cùng (phòng, tỉnh, loại kỳ, khóa kỳ) đã chốt và không xã × biểu nào đang mở lại (`key_closed`). Có thêm cờ theo biểu (`form_closed`) và theo kỳ (`period_closed`) nếu PO chọn mức mịn hơn | Bên đọc W11.64 chọn cột cờ — không cần migration |
| Khóa tra cứu | `period_key` ĐÃ LƯU lúc phát hành (W11.05b). Kỳ không có khóa (vụ, đột xuất, phát hành trước pf 000143) không có dòng — không tính lại bằng SQL | Nạp ngược khóa kỳ cũ nếu KHTC cần kỳ trước 04/11/2026 |
| Q-047 — lũy kế nào là chuẩn | Lũy kế của nguồn: cột lũy kế nằm trong `rows/totals.cells` của ảnh chụp (KHTC không tự cộng) | — |
| Phạm vi CNTS | Cùng view, `department_code = chan_nuoi_thuy_san`: nếu CNTS dùng vòng đời `extension/reportcycle` thì có dòng ngay; nếu dùng kỳ native `cap/forms` thì thêm nhánh `UNION ALL` (giữ cột) bằng `CREATE OR REPLACE VIEW` — schema `pf_forms` thuộc module khác nên cần thêm view `pf_forms_pub` hoặc port đọc, quyết ở gói CNTS | W11.42/W11.44 |
| Giá (BG10 dữ liệu) | Chỉ hợp đồng (`planned`): bảng giá đã duyệt theo ngày min/max **toàn tỉnh**, ngày chưa duyệt không có dòng → bên đọc lấy ngày đã duyệt gần nhất ≤ ngày chốt (Q-084 đề xuất); số phiếu theo tuần báo cáo × xã, chỉ đếm phiếu của bảng giá đã duyệt, cờ tuần đã chốt = mọi ngày báo giá theo lịch đã duyệt | W11.76 dựng theo `contracts/pubview/README.md` |
| Bên đọc KHTC | KHTC nằm trong `pf/extension` (`ke_hoach_tai_chinh`); vai đọc là `r_extension_pub` (view) — W11.64 quyết cấp thành viên `r_extension_pub`/`r_price_pub` cho đường đọc của nó hay đọc qua port | W11.64 |
## W11.72 — Xã báo giá + phân công (pf 000144)
## W11.72 — Xã báo giá + phân công (pf 000145)

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Mã xã báo giá (FN-AD-04.02 BR-01 "hệ thống tự sinh") | `XBG` + 4 chữ số tăng dần theo tenant, không tái sử dụng — thay giả định "mã xã báo giá = mã ĐVHC" của W11.73 (mã ĐVHC đổi khi sắp xếp ĐVHC; mã xã phải ổn định) | `domain.PointCode`, `store.NextPointNumber` | Đổi định dạng cần migration (CHECK `^XBG[0-9]{4}$`) |
| `point_code` của W11.73 | Nay bắt buộc là xã báo giá có thật, đang hoạt động tại ngày hiệu lực (gán mặt hàng, thêm mặt hàng có xã, duyệt đề xuất); danh sách mặt hàng của xã ngưng/chưa hoạt động = rỗng | `app.requirePoints`, `pointItems` | — |
| Q-053 — nhóm vùng | Thuộc tính riêng của Báo giá (đề xuất "cả hai là thuộc tính riêng"): danh mục `pf_price.region_group`, mặc định An Giang, Kiên Giang; không dùng khu vực (`Regions`) của pf/party | pf 000145, `domain.DefaultRegions` | Thêm/sửa/ngưng nhóm vùng ở màn "Xã báo giá" (Admin) |
| Ai quản lý xã, nhóm vùng, phân công | Chỉ vai quản trị (`ManageBy` = Admin KN) — BRD FN-AD-04.01/04.02 "Admin · Quản trị hệ thống"; Cấp Tỉnh, Cấp Quốc gia xem (tên cán bộ chỉ Admin, Cấp Tỉnh) | `app/point.go` | Thêm cấp vào kiểm `c.manage` |
| Nơi phân công cán bộ (BRD đặt ở "Tài khoản & phân quyền" FN-AD-04.01) | Phân công là dữ liệu của `pf/price` (sổ `point_officer`), thao tác ở màn "Xã báo giá" › "Phân công" — **lệch vị trí màn** so với BRD; màn Tài khoản (pf/accounts) chưa hiện xã báo giá | `/kn/bao-gia/xa-bao-gia` | Gói sau thêm khối "Xã báo giá" vào màn Tài khoản gọi cùng API `/v1/price/points/{code}/officers` |
| Một cán bộ Xã nhiều xã? | Mỗi xã một cán bộ Xã phụ trách chính tại một thời điểm (BR-01); một cán bộ có thể phụ trách nhiều xã (không chặn — BRD "được gán vào một xã báo giá" coi là thông thường, không phải ràng buộc). Mỗi xã một cán bộ Cấp Tỉnh được phân công; một cán bộ Tỉnh nhiều xã (BR-02) | `domain.OfficerAt` | Thêm kiểm trùng ở `AssignOfficer` nếu PO yêu cầu |
| Ai chọn được khi phân công | Cán bộ có lần gán `kn_xa` / `kn_tinh` còn hiệu lực ở phòng ban `thong_tin_truyen_thong` (Profile.Department). Không kiểm mã xã của lần gán `kn_xa` khớp ĐVHC của xã báo giá | `assembly.PriceOfficers` | CTV chưa gán phòng TTTT → không chọn được: Admin thêm phòng ban cho lần gán |
| Cờ QTNV (FN-AD-04.01, BR-05) | `port.Profile.StewardDuty = authz.DutyPriceAdmin` (cờ W11.07 trên lần gán Cấp Tỉnh): lịch, ngưỡng, gửi thử nhắc (W11.74), thao tác Quản trị nghiệp vụ của mặt hàng (W11.73 — trước đây mặc định mọi Cấp Tỉnh). Không kiểm thêm phòng ban của phiên là TTTT (BR-05 do pf/accounts kiểm khi gán cờ) | `app.Service.resolve` | Profile rỗng `StewardDuty` → chỉ Admin |
| Ngày hiệu lực của xã/phân công | Vắng → ngày báo giá kế tiếp (lịch W11.74); hôm nay được; ngày đã qua → 422 `price.valid_from`. Ngưng xã hôm nay khi xã đã gửi phiếu → từ ngày báo giá kế tiếp (W11.75 nối `PointHooks.Submitted`; chưa nối = chưa gửi) | `Service.pointFrom`, `pointVersion` | — |
| ĐVHC hết hiệu lực (sắp xếp ĐVHC) | Không tự chuyển: danh sách báo "ĐVHC hết hiệu lực → mã kế thừa"; Admin "Sửa" chuyển sang mã ĐVHC mới từ ngày hiệu lực (giữ mã xã, lịch sử). Xã báo giá chọn ĐVHC cấp xã có hiệu lực tại ngày hiệu lực | `app.checkPoint`, `PointView.AdminUnitValid` | Gói sau có thể tự đề xuất chuyển theo `successor_codes` |
| Người nhận nhắc nhập giá (W11.74) | Cán bộ Xã phụ trách từng xã đang hoạt động; xã "Chưa có cán bộ Xã" không nhắc (FN-TI-02.01); tenant chưa khai xã báo giá → **không còn** thông báo chủ đề tới mọi `kn_officer`. Lần 2 nhắc lại mọi xã cho tới khi W11.75 nối `PointHooks.Submitted` | `app.PointReminders` (mặc định trong `price.New`) | `Module.UseReminderTargets(nil)` quay về thông báo chủ đề |
| Menu | Admin › Phòng ban › Phòng TTTT: "Xã báo giá" (`/kn/bao-gia/xa-bao-gia`), "Kỳ báo giá & giờ khóa" (`/kn/phong-ban/{mã}/bao-gia-lich`) mở; "Mặt hàng" Sắp có. Cấp Tỉnh có cờ QTNV: "Cấu hình báo giá" (`/kn/bao-gia/cau-hinh`, cùng màn lịch W11.74) mở. Menu `/kn/bao-gia/*` thuộc module per_tenant `pf/price` | `party/domain/menu_departments.go`, `modules.go` | — |

## W11.41 — Danh mục CNTS (pf 000146)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Nơi lưu danh mục CNTS (D4 CN1 "enum `catalog.go`") | Cùng bảng `pf_masterdata.catalog_item` + sổ `catalog_change` với danh mục Trồng trọt, tiền tố `cnts_` (12 danh mục); thuộc tính kiểm ở `domain/cnts.go`. Mũi tiêm để trong `attrs.doses` (`<vaccine>/<lần>@<ngày tuổi>`), không cần bảng mũi tiêm | `masterdata/domain/cnts.go`, `port/cnts.go` |
| FN-06 BR-03 "lưu theo hiệu lực từ ngày – đến ngày" | Áp cho **mọi** danh mục CNTS (không chỉ thuộc tính tổng hợp): sổ phụ `catalog_effective`, mỗi lần tạo/sửa/ngừng/dùng lại ghi một giá trị có `valid_from` (mặc định hôm nay; ngừng/dùng lại = hôm nay); "đến ngày" = `valid_from` của giá trị kế tiếp. Bảng hiện trạng giữ giá trị ghi sau cùng (kể cả ngày áp dụng tương lai). Mục nạp sẵn có hiệu lực từ 01/01/2000; mục nạp trước khi có sổ (không có dòng) → đọc hiện trạng | `store/effective.go`; sửa ngày áp dụng lùi về quá khứ được phép (báo cáo đã gửi giữ giá trị lúc gửi — BR-04, do gói biểu chụp) |
| Danh sách danh mục (~10) | Vật nuôi, Giống, Vaccine, Quy trình tiêm, Bệnh, Hóa chất, Cơ sở giết mổ, Đối tượng nuôi thủy sản, Nhóm khai thác, Chỉ tiêu theo dõi, Loại mô hình, Loại văn bản (FN-06) | `domain.CNTSCatalogs` |
| Dữ liệu nạp sẵn | Tạm theo BRD (chờ khách gửi danh mục chuẩn): 8 loài CN-01, 13 bệnh, 9 vaccine cột CN-02 (mẫu 2026, "Dịch tả – THT heo" một cột) + 6 vaccine của ví dụ quy trình gà thương phẩm, quy trình gà thương phẩm (9 mũi), 4 nhóm khai thác, 5 chỉ tiêu, 4 loại văn bản, 4 đối tượng thủy sản, 4 loại mô hình. **Không** nạp cơ sở giết mổ (Admin nạp Excel) | `seeddata/catalogs.json` |
| Ánh xạ vaccine – loài (Mục III.5) | Thuộc tính bắt buộc `species` của vaccine = loài SO SÁNH ("Trâu + Bò", "Vịt + Gà"…); `report_group` (heo/bo/cum_gia_cam/dai/khac) = nhóm cộng báo cáo tuần. Cột CN-02 của biểu vẫn do cấu hình biểu (UR-TINH-07, W11.42) chọn | `port.CNTSCatalogs.VaccineSpecies` |
| Cơ sở giết mổ: định danh, mộc dấu | `facility_ref` (UUIDv7) do máy chủ cấp, không đổi qua lần sửa (khóa của W11.07 `facility_refs`, BM-01). Mã mộc dấu GS và GC là danh sách; một mã chỉ thuộc **một** cơ sở (kể cả cơ sở đã ngừng — mã là định danh) và không vừa GS vừa GC. Xã phải là cấp `commune` có hiệu lực tại ngày áp dụng (BR-02) | 409 `masterdata.cnts_stamp_taken`, 422 `masterdata.cnts_commune_invalid` |
| Ngưỡng mặc định | Tham số có phiên bản `cnts.*` (người đặt ≠ người duyệt): biến động tổng đàn 15 %, khai thác 50 %, KSGM ×3 / ≥ 7 ngày, tìm lùi 4 kỳ, 200 m, GPS 50 m, quá hạn +1 ngày, pH 7,5–8,5, oxy ≥ 4 mg/l (`cnts.indicator_min/max:<chỉ tiêu>[:<loại mô hình>]`), tỷ lệ chết `cnts.mortality_max_pct:<loại mô hình>` chưa có giá trị (Cấp Tỉnh đặt). Màn Danh mục CNTS chỉ hiển thị; sửa ở "Tham số" | `seeddata/params.json`, `domain/registry.go` |
| Quyền | FN-06: Admin (`masterdata_admin`) thêm/sửa/ngừng/nạp Excel qua Admin API; Cấp Tỉnh (và mọi người dùng đã xác thực) chỉ đọc qua Application API — mục menu mới `tinh.cnts.danh_muc` (Cấp Tỉnh) và `admin.pb.cnts_danh_muc` bỏ "Sắp có" | `party/domain/menu_departments.go` |
| Phạm vi Cấp Xã theo cơ sở (W11.07) | `facility_refs` phải là cơ sở giết mổ **đang dùng hôm nay** (`accounts.facility_invalid`); chưa kiểm cơ sở thuộc xã của lần gán (Nút xã không mang mã ĐVHC ở pf/accounts) | `accounts.UseFacilityCatalog`; gói W11.42 kiểm thêm theo xã |
| Đồng bộ thiết bị | Danh mục CNTS **chưa** đồng bộ xuống app (entity `catalog_item` giữ 10 danh mục cũ) — mobile CNTS là W11.42 | `domain.SyncCatalogs` |

## W11.21 — Danh mục nhân sự (pf 000147)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Danh mục FN-19 ở `cap/masterdata` (bản đối chiếu H1) hay module của chủ | **Module mới `pf/hrm` (`dom/hrm`) dựng ngay ở W11.21**, schema `pf_hrm` — một bên dùng, thuộc tính và tham chiếu chéo riêng ([ADR-0050](../adr/0050-dom-hrm-pf-hrm.md)) | ADR-0050 |
| Q-070 bảng hệ số, thời gian giữ bậc, quy chế NLTH, % TNVK, tuổi nghỉ hưu, số ngày nhắc | Chưa có dữ liệu → cài **mẫu** `placeholder = true` (nhãn "Mẫu" trên màn): bảng hệ số theo NĐ 204/2004 (A2.1 4,40 + 0,34 × 8 bậc; A1 2,34 + 0,33 × 9 bậc; B 1,86 + 0,20 × 12 bậc; giữ bậc A 36 / B 24 tháng; hiệu lực 01/10/2004); quy tắc nâng bậc: xếp loại tối thiểu HTNV, 3 năm xếp loại, 5 năm thành tích, kéo dài giữ bậc khiển trách/cảnh cáo 6, cách chức 12 tháng, chỉ tiêu NLTH 10 %, NLTH nghỉ hưu ≤ 12 tháng trong 12 tháng, TNVK 5 % + 1 %/12 tháng sau 36 tháng ở bậc cuối, nhắc 1/3/6 tháng, tuổi nghỉ hưu 2026 nam 61 tuổi 6 tháng (738 tháng), nữ 57 tuổi (684 tháng) — mỗi năm lộ trình một phiên bản; quy đổi thành tích: Bằng khen Bộ/UBND tỉnh, CSTĐ cấp tỉnh 12 tháng, CSTĐ cơ sở, Giấy khen Sở 9 tháng | Admin ghi phiên bản mới (`POST /v1/hrm/grade-scales/{grade}`, `/v1/hrm/rules/{key}`) — mẫu bị thay, không xóa |
| Danh sách BRD không liệt kê (chức vụ, chuyên ngành, dân tộc/tôn giáo khác Kinh/"Không", khen thưởng, kỷ luật, biến động, lý do nghỉ) | Cài mẫu ngắn (`placeholder`); hệ số PCCV của chức vụ để trống (không đoán) | Admin sửa trên màn; `domain/defaults.go` |
| Tên ngạch 06.031 và V.03.09.27 (BRD chỉ ghi mã) | "Kế toán viên" (CV), "Kỹ thuật viên khuyến nông hạng IV" (CS, loại B — khớp hệ số chênh lệch 0,20 của AC-02 FN-09), cả hai `placeholder` | Admin sửa tên |
| Chuẩn hóa mã (AC-03) | Mã ngạch: bỏ MỌI khoảng trắng, xuống dòng (kể cả giữa mã), giữ chữ hoa; mã danh mục khác: bỏ khoảng trắng, về chữ thường, `[a-z][a-z0-9_]*` | `domain.NormalizeCode` |
| Quyền | Admin thêm/sửa/ngừng; Cấp Tỉnh có ngữ cảnh ở phòng `hanh_chinh_tong_hop` chỉ xem (BRD §VI.4 "Master data: Admin Có, Cấp Tỉnh – HCTH Xem"); vai khác 403. Không duyệt bốn mắt (BRD: HCTH không có luồng phê duyệt) | `assembly.KNHRMAccess` |
| BR-05 quan hệ ngạch ↔ chức vụ | Chưa mô hình (chờ HCTH xác nhận); chức vụ có `mgmt_level` tùy chọn nối với cấp quản lý tài khoản (W11.07) | — |
| Bảng hệ số sửa ngày quá khứ | Cho phép (chuyển đổi dữ liệu cần bảng cũ); cùng ngày hiệu lực thì bản ghi sau thắng. Quyết định nâng bậc (W11.25) phải chụp hệ số lúc ban hành để BR-02 đúng cả khi sửa lùi | W11.25 |
| Đơn vị công tác (HS-16, NS05) | Không ở danh mục này — dùng danh mục đơn vị nội bộ W11.07 (`pf_accounts.internal_unit`) | — |
| Nạp danh mục/bảng hệ số từ Excel (luồng phụ FN-19) | Chưa làm (chờ dữ liệu Q-070; `cap/import`/`shared/tabular` ở W11.30) | W11.30 |

## W11.75 — Nhập báo giá + xác minh (pf 000148)

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Q-054 — Cấp Tỉnh sửa số gốc (Báo giá cho sửa giá, sửa ngày, xóa phiếu) | Chấp nhận theo phòng (đề xuất): Báo giá cho hiệu chỉnh / sửa ngày / xóa mềm — mọi thay đổi là phiên bản mới có lý do; giá Xã gửi giữ nguyên (in phiếu), bảng giá và giá tham chiếu dùng giá hiệu chỉnh | `app/ticket_review.go` | — |
| Q-085 — phiếu in File 2 có ký số? | Không ký số (đề xuất): phiếu in có ô ký tay "Người lập phiếu", "Xác nhận của Phòng TTTT"; bố cục File 2 dựng lại dạng bảng (chưa có tệp mẫu gốc trong repo) | `PrintView.tsx`, `TicketService.PrintTickets` | Khi có File 2 gốc: chỉnh bố cục màn in / bộ dựng PDF |
| "Đã duyệt" trong giá tham chiếu (CT-02) khi chưa có duyệt bảng giá (W11.76) | Chưa nối `TicketHooks.Board` → mọi phiếu Đã gửi / Nộp muộn của N ngày báo giá trước là "đã duyệt"; mọi ngày là "Chưa duyệt" (Cấp Tỉnh sửa được mọi ngày đã khóa) | `app.TicketHooks` | W11.76 nối `Module.SetTicketHooks` |
| "Một phiếu chỉ một người sửa tại một thời điểm — Đang sửa bởi …" (BR-06 FN-TI-02.04) | Khóa lạc quan: mọi lần ghi gửi `version` đã đọc; người thứ hai nhận 409 `price.ticket_stale` và tải lại (không giữ khóa phiên) | `checkVersion` | Thêm khóa thuê (lease) có tên người nếu PO cần hiển thị "Đang sửa bởi" |
| Trạng thái Không nộp / Đã khóa / Chờ rà soát / Quá hạn phản hồi | Chiếu lúc đọc theo `DayPlan.Locked` và `respond_by` (không job ghi lúc 12:00); "thông báo tổng hợp cho Cấp Tỉnh lúc 12:00" (B4 FN-TI-02.03) chưa gửi | `domain.TicketView`, `AlertView` | Job `price_calendar` (W11.74) phát thông báo khi nối `pf/notify` |
| Yêu cầu xác minh gửi sau hạn phản hồi của ngày (phiếu nhập thay buổi chiều) | Hạn = giờ gửi + (hạn phản hồi − giờ khóa), tối thiểu 30 phút (mặc định +1 giờ 30) | `app.respondBy` | Tham số lịch W11.74 nếu PO chốt khác |
| Ai nhập / sửa | Cấp Xã: xã mình phụ trách chính TẠI NGÀY báo giá, chỉ ngày hôm nay trước giờ khóa; Cấp Tỉnh: xã được phân công hoặc chưa phân công (`Point.HandledBy`); Admin: như Cấp Tỉnh mọi xã (hỗ trợ — BR-01 FN-TI-02.04); Cấp TW không đọc phiếu | `app/ticket.go` | — |
| Lưu nháp sau khi đã gửi | Không cho (409 `conflict`) — sửa rồi "Gửi lại"; tránh phiếu Đã gửi trở về Bản nháp (BRD: Đã gửi → gửi lại) | `SaveEntry` | — |
| Sửa ngày phiếu | Ngày mới phải là ngày báo giá đã qua giờ khóa, xã hoạt động, chưa có phiếu (AC-04), bảng giá chưa duyệt; cảnh báo giữ nguyên (không tính lại theo ngày mới) | `Redate` | — |
| Giá nhập trên web "13.000" | Nhóm 3 chữ số sau dấu chấm (không dấu phẩy) = phân cách nghìn (VND); "12,5" = 12.5 | `kn/price/tickets/api.ts priceOf` | — |
| Thông báo chuông / email (yêu cầu xác minh, phản hồi, kết quả xử lý, nhập thay) | Chưa gửi — cần sự kiện mới và consumer `pf/notify` (CHECK `kind` của `pf_notify` chỉ migration của `pf/notify` sửa) | — | Gói sau: sự kiện `pf.price.verification.*` + `pf/notify` |
| Tệp minh chứng khi nhập thay (FN-TI-02.09 B3, tùy chọn) | Chưa làm — chỉ lý do | — | Nối `cap/files` |
| Lịch sử báo giá dạng Lịch (FN-XA-03.03) | Chỉ dạng Danh sách | `HistoryScreen` | Gói sau |
| Nhập trên điện thoại (UR-XA-01) | Web responsive (thẻ theo mặt hàng, gom nhóm) — `apps/field-app` chưa có mẫu KN báo giá; hoãn app di động | `globals.css .pb-line` | Gói sau nếu PO cần offline |

## W11.42 — Biểu kỳ Cấp Xã BM-01/04/05/17 (pf 000149)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu |
|---|---|---|
| Đường kỳ: `cap/forms` native hay `extension/reportcycle` | **Native `cap/forms` (profile `kn_cnts`)** — đề xuất "chuyển kỳ CNTS thẳng sang kỳ native" của bản đối chiếu (D4 §X4); công việc reportcycle là mẫu × XÃ × kỳ còn BM-01 cần đơn vị = cơ sở giết mổ; kỳ native đã có đơn vị lá tổng quát, Không nộp / Chờ mở kỳ, hủy kỳ, ảnh chụp (W11.05) và W11.04 sẽ cắt KN sang native. `pf/extension` chỉ tính lịch kỳ + định nghĩa biểu, phát hành qua cổng mới `formsport.Publisher` | `reportcycle/domain/cnts_forms.go` |
| Q-045 — tuần của BM-04/BM-17 | Quy tắc W11.03 v2 (Thứ Tư – Thứ Ba, không phải Thứ Ba – Thứ Hai của BRD CNTS); lô luôn mở rộng tới tuần đầy đủ. **Phòng CNTS chưa có kỳ cũ nên dùng v2 ngay** kể cả trước ngày hiệu lực 04/11/2026 của tham số (phiên bản tham số v2 khác, vd `week_start=tue`, được tôn trọng) | `app.cntsRule`, tham số `extension.report_period_rule` |
| Hạn riêng từng biểu | BM-01 09:00 D+1; BM-04/BM-17 12:00 ngày sau ngày cuối tuần (W11.03 `DefaultDeadline`); BM-05 Phần I 17:00 Thứ Hai sau tuần làm việc, Phần II 17:00 Thứ Sáu trước tuần (giờ BRD "do Cấp Tỉnh đặt" — mặc định 17:00 tạm). Người phát hành đổi giờ/offset ở mỗi lô | `domain.CNTSForms[].Deadline` |
| BM-05 — "mỗi phần là một kỳ riêng", H01 là cán bộ | Hai biểu `CNTS-BM05-I`, `CNTS-BM05-II`, kỳ = tuần làm việc Thứ Hai–Thứ Sáu (khóa `YYYY-LVww` theo tuần ISO, thẻ `cnts.work_week/v1`). **Đơn vị = xã phụ trách** của cán bộ tăng cường (đơn vị = người của `cap/forms` còn CHỪA CHỖ chờ HCTH); người nhập lưu ở `submitter_ref` | Thêm `person` vào CHECK `period_spec.unit_kind` khi HCTH chọn (a) |
| Khóa kỳ BM-01 | `period_kind = day`, khóa `YYYY-MM-DD`; tuần KSGM (BM-02) do W11.44 cộng từ kỳ ngày | — |
| Danh sách cột tiêm CN-02 (UR-TINH-07) | 9 cột mẫu 2026 cố định trong phiên bản biểu v1; Cấp Tỉnh/Admin đổi cột bằng phiên bản biểu mới (`/v1/forms/templates`), kỳ đã phát hành giữ phiên bản cũ | Màn cấu hình cột riêng: gói sau |
| Quy tắc chặn | Số liều tuần của một cột > tổng đàn loài so sánh → Chặn (heo; trâu + bò; vịt + gà; chó mèo — ánh xạ TĨNH theo cột mẫu 2026, chưa đọc `port.CNTSCatalogs.VaccineSpecies`); số âm → Chặn (`non_negative`); BM-05 ô văn bản ≤ 500 ký tự, "Công tác khác" phải ghi rõ. Cảnh báo (±15 %, 50 %, lệch KSGM, 3× trung bình) để W11.43 | `domain.ValidateCNTS`, `formsport.Validator` |
| "Không phát sinh" (UR-XA-13, BM-01 KS-05, BM-17 TS-09) | Cờ `nil_report` của lần gửi (sổ phụ `submission_meta`): mọi trường số = 0 (số khác 0 → lỗi), ô trống tự điền 0; BM-05 không có | `domain.NilValues` |
| Khóa lạc quan (III.3.0) | `base_version` = phiên bản người nhập đã thấy; khác → 409 `forms.submission_conflict` "Đã có người cập nhật lúc …"; DB kiểm lại `base_version = seq − 1` (AGF04). Thiếu `base_version` (API cũ) = không kiểm | `app.SubmitWith` |
| Trễ hạn / lý do trễ (H05, H09) | Trạng thái `overdue` là phép chiếu (chưa gửi, kỳ còn nhận, quá hạn); gửi sau hạn với profile `require_late_reason` (chỉ `kn_cnts`) phải có `explanation`. `late` tính theo `captured_at` của bản offline (giờ thiết bị — có thể sai giờ máy) | `Profile.RequireLateReason` |
| Sao chép kỳ trước | Bài cuối của đơn vị ở kỳ gần nhất trước đó (theo `period_from`) của cùng profile + mã biểu (mọi phiên bản biểu); mobile sao chép từ công việc đã có trên máy (ngoại tuyến) | `GET /v1/forms/periods/{id}/previous` |
| Cơ sở thuộc xã (mục mở W11.41) | Lần gán Cấp Xã: mọi `facility_refs` phải thuộc một xã của lần gán (danh mục hôm nay) → 422 `accounts.facility_commune_mismatch`. Phát hành BM-01: cơ sở phải đang dùng tại ngày đầu lô, `unit_parents` điền/kiểm theo xã của cơ sở → 422 `forms.facility_invalid`. Cơ sở đổi xã sau phát hành: kỳ đã phát hành giữ xã cha cũ | `port.CNTSCatalogs.FacilityCommunes` |
| Lưu nháp (H06 "Lưu nháp") | **Chỉ trên máy** (web: `localStorage`, mobile: `catalog_cache`) — không có sổ nháp ở máy chủ nên Cấp Tỉnh không thấy "Lưu nháp" | Sổ phụ nháp `pf_forms` ở gói sau nếu PO cần |
| Người phụ trách biểu (`phu_trach_bieu`) | Chưa gán theo đợt; rà soát/yêu cầu điều chỉnh vẫn theo cấp `review_by` (Cấp Tỉnh) | W11.43 (vòng đời cảnh báo) |
| Nguồn `_pub` cho W11.44 | Chưa thêm view `pf_forms_pub` (một migration một schema); W11.44 đọc ảnh chụp/kỳ qua port hoặc thêm view | W11.44 |
| Đồng bộ thiết bị | `forms_task` / `forms_submission` (mẫu KN v7.7 P7), con trỏ `pf_forms.sync_feed`; 12 danh mục CNTS thêm vào `SyncCatalogs` (entity `catalog_item`, giá trị hiện hành) | `docs/dev/sync-contract.md` |

Chưa làm trong gói này: nhập Excel BM-04; lũy kế (CN-03/05/07, TS-07/08) và "Tuần trước liền kề" (W11.44); gợi ý CN-06 từ tổng BM-01; lịch trực KSGM (KS-04, cảnh báo người nhập không trong lịch); chốt kỳ tự động theo lịch và mở lại có thời hạn cho kỳ native; check-in BM-05 (CV-06/07); "Lịch sử sửa" từng ô (H07).
## W11.22 — Hồ sơ nhân sự (pf 000150, vault 000004)

| Câu hỏi / mục | Phương án đã chọn | Đổi ở đâu / nếu PO chọn khác |
|---|---|---|
| Q-005 DPIA (hồ sơ có dân tộc/tôn giáo/Đảng) | **Mã được, chưa bật dữ liệu thật:** cờ `AG_HRM_RECORDS` — vắng ở production = TẮT (mọi route hồ sơ 403 `hrm.records_disabled`, `/v1/hrm/access` báo `enabled=false`, màn hiện thông báo); ngoài production bật (dữ liệu tổng hợp). Cổng module `per_tenant` của `pf/hrm` giữ mặc định bật vì danh mục W11.21 đã dùng | Có DPIA → đặt `AG_HRM_RECORDS=1` ở production |
| Q-069 "ngày xếp bậc hiện tại", "HS bảo lưu" | **Lưu trong hồ sơ** (đề xuất của dev) — trường `step_since` (mặc định = ngày hiệu lực của dòng ngạch/bậc) và `reserved_coeff` của sự kiện ngạch/bậc. BRD BR-06 "không lưu quá trình ngạch, bậc": bậc vẫn là PHÉP CHIẾU của các sự kiện ngạch/bậc (để truy vấn as-of đúng — NS01–06 tại ngày chốt), màn chỉ hiện bậc/hệ số hiện hưởng; dòng cũ là "lịch sử sửa" | Bỏ hai trường: chỉ ẩn ở màn, dữ liệu đã ghi giữ nguyên |
| Cột nguồn dữ liệu ban đầu (Q-069 phần 2) | Chưa làm — W11.30 chuyển đổi Excel | W11.30 |
| PII ở đâu | Họ tên, SĐT, địa chỉ → `vault.identity` (chủ thể `person`, địa bàn `hrm` — không phân phối khóa xuống thiết bị); ngày sinh (+ mức chính xác), giới tính, email, ghi chú (HS-32) → ngăn `hrm.profile`; TK-01..06 → ngăn `hrm.sensitive`. Mỗi ngăn có khóa dữ liệu riêng (sinh mới mỗi lần ghi, bọc KEK), mục đích gắn cứng; xóa định danh xóa luôn ngăn. `pf_hrm` chỉ có mã tham chiếu, mã danh mục, ngày, số quyết định, TÊN trường đã đổi | — |
| Giới tính, ngày sinh cho báo cáo NS (W11.28) | Ở kho (PII) — báo cáo NS01–06 phải đọc qua kho (chưa có API đọc theo lô) | W11.28: thêm đọc theo lô ở kho, hoặc PO chấp nhận lưu năm sinh/giới tính ở `pf_hrm` (DPIA) |
| Vai kho cho người dùng HCTH | Dùng vai realm có sẵn `vault_registrar` (như A1 cho Cấp Xã) — Admin cấp cho Cấp Tỉnh – HCTH và lãnh đạo phòng cần xem chi tiết. Thiếu vai: danh sách vẫn chạy (nhãn họ tên rỗng), chi tiết lỗi 403 của kho | Gói sau: vai kho riêng `vault_hrm` chỉ nhận mục đích `hrm.*` |
| Mục đích của thẻ TK | `hrm.sensitive` — chỉ đọc/ghi ngăn `hrm.sensitive`, kho từ chối ở mọi thao tác khác; đọc thẻ đòi MFA khi kho bật `AG_VAULT_REQUIRE_MFA` | — |
| Tìm theo họ tên (FN-07 B1) | **Chưa có** — họ tên không ở `pf_hrm`; tìm theo mã hồ sơ (tiền tố) hoặc SĐT (chỉ mục mù của kho). Danh sách hiện nhãn che bớt ("Trần T. M.") | Cần chỉ mục mù theo tên ở kho (gói sau) |
| Trùng email (quy tắc V.2.0 mục 4) | Chưa chặn (kho chỉ có chỉ mục mù SĐT/CCCD); trùng SĐT → 409 `hrm.duplicate_person`; trùng họ tên + ngày sinh → chưa cảnh báo | Thêm chỉ mục mù email ở kho |
| Mã hồ sơ (HS-01, BRD ví dụ "VC-HCTH-011") | `VC-nnnnn` / `HD-nnnnn` / `XA-nnnnn` theo loại — không gắn mã đơn vị vì đơn vị đổi theo quá trình | Đổi `domain.StaffCode` |
| Kỷ luật | Lưu như cờ có hiệu lực (sự kiện `discipline`, có `valid_to`) cho W11.25 (Q-075 đề xuất "cờ nhập tay"); không quản lý quy trình | — |
| Khen thưởng | Năm, hình thức (mã danh mục), danh hiệu, cấp quyết định, số QĐ — không số tiền (BR-03); tệp QĐ ở W11.24 | — |
| Ma trận FN-20 mặc định | Admin: mọi quyền toàn Trung tâm (cố định, không chỉnh — "chặn gỡ Admin cuối"); Cấp Tỉnh – HCTH: mọi quyền toàn Trung tâm; lãnh đạo các phòng (`KNContext.Manager`): xem + xuất "đơn vị mình" (đơn vị nội bộ + cán bộ xã có đơn vị quản lý đó — HS-37); Cấp Tỉnh khác, Cấp Xã, Cấp TW: không (BR-04 — chỉ Contact). Thẻ TK chỉ cấp được cho nhóm HCTH hoặc từng người (BR-03). Ngữ cảnh chỉ xem (Ban Giám đốc, Lãnh đạo đơn vị ở phòng ban khác) = nhóm lãnh đạo theo đơn vị của họ, không ghi | `domain.DefaultMatrix`; Admin chỉnh ở "Phân quyền hồ sơ" |
| "Nhân sự của phòng" (Q-067) | Mở — cùng màn danh sách, máy chủ giới hạn phạm vi "đơn vị mình" | — |
| Nhật ký xem/xuất ≥ 1 năm | Sổ `pf_hrm.record_access` chỉ ghi thêm (chuỗi băm), mỗi lần xem chi tiết/xem thẻ TK/xuất; giữ theo chính sách sổ chung (≥ 13 tháng). Xem danh sách KHÔNG ghi ở `pf_hrm` (kho ghi mỗi lần đọc nhãn) | — |
| Xuất Excel (FN-07 B5) | CSV UTF-8 có BOM (Excel mở được), cột không PII ngoài nhãn che bớt; tối đa 5.000 hồ sơ. Xuất họ tên đầy đủ cần đọc kho theo lô (hạn mức Resolve 600/giờ) — chưa làm | W11.28 (renderer xlsx + đọc kho theo lô) |
| Thông tin làm việc (FN-08) | Sự kiện `work_start`/`work_end` (lý do bắt buộc, mã danh mục `leave_reason`); trạng thái chỉ "Đang công tác"/"Đã nghỉ việc" (thêm "chưa vào làm" khi chưa có ngày vào làm); không tệp QĐ (W11.24) | — |
| Đơn vị công tác | Mã đơn vị nội bộ W11.07 (`pf_accounts.internal_unit`) qua `port.Units` — kiểm "đang dùng" khi người gọi có Nút (Admin không Nút: chỉ kiểm dạng mã) | — |
