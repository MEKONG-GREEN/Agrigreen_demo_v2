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
# Giả định khi chưa có trả lời của khách — BRD KN 10/2026

Mỗi gói W11.x ghi các câu hỏi (§2 QĐ, §4 Q-0xx của tài liệu đối chiếu v4.3/MKG v1.4/BRD KN) mà gói đã
làm theo phương án đề xuất. Khi khách trả lời khác, sửa ở chỗ được nêu.

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
# Giả định khi hiện thực BRD Khuyến nông 10/2026 (chủ sản phẩm chưa trả lời)

Nguồn: đối chiếu v4.3 / MKG v1.4 / BRD KN (09/10/2026) — §2 quyết định QĐ-1…10, §4 câu hỏi Q-0xx. Mỗi gói ghi lựa chọn mặc định đã
dùng ("Đề xuất của dev"); đổi khi chủ sản phẩm trả lời. Mỗi gói một mục.

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
# Giả định khi chưa có trả lời của PO — đợt BRD KN 10/2026

Mỗi gói W11.x ghi các câu hỏi đang chặn/chờ và phương án dev đã chọn làm mặc định (theo §2, §4 của tài liệu đối
chiếu v4.3 / BRD KN). Đổi phương án = đổi tham số/seed, không sửa mã, trừ khi ghi khác.

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

## W11.74 — Lịch báo giá, giờ khóa, nhắc, ngưỡng (pf 000146)

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
