# Nghiệm thu Phòng Trồng trọt theo BRD 2.0 (Trồng trọt v7.7)

Gói KN-STANDALONE-11-UAT-DOCS (#273). Tài liệu này thay cho [kn-v7.7-acceptance.md](kn-v7.7-acceptance.md) khi nghiệm thu với
người dùng; tệp cũ giữ lại làm bảng bằng chứng kiểm thử tự động, kịch bản đầu–cuối "An Giang, Tuần 36" và phần phi chức năng.

## 1. Phạm vi và căn cứ

- Căn cứ duy nhất: BRD 2.0 – Module Phòng Trồng trọt **v7.7**, Mục IV "Chi tiết yêu cầu chức năng", đủ 25 chức năng:
  FN-CH-00.01; FN-TW-01.01, 01.02; FN-TI-02.01 … 02.07; FN-XA-03.01 … 03.08; FN-AD-04.01 … 04.07.
- BRD Trồng trọt không đánh số từng quy tắc và tiêu chí như BRD CNTS. Cột "Mã" dùng ký hiệu theo dòng của bảng chức năng trong BRD:
  **B** = bước của luồng xử lý chính, **LP** = luồng phụ, ngoại lệ, **QT** = quy tắc nghiệp vụ, **AC** = tiêu chí chấp nhận;
  mã UR (UR-XA-02 … UR-XA-15) giữ nguyên.
- **AC-CHUNG** (áp dụng cho mọi FN): dữ liệu tạo mới, thay đổi từ giao diện được lưu vào cơ sở dữ liệu; mọi thay đổi có truy vết
  (ai, khi nào, giá trị cũ → mới). Cách kiểm chung: sau mỗi thao tác ghi, tải lại trang và mở khung Log/Lịch sử của bản ghi.
  Trạng thái trên dev: Đạt (sổ chỉ ghi thêm, kiểm ở bước 13 của `TestKN77AnGiangTuan36`).
- Cột **Trạng thái trên dev** ghi theo mã trên nhánh `dev` ngày 11/11/2026:
  - **Đạt**: đã thấy mã và kiểm thử tự động (tên ở dòng "Kiểm thử tự động" cuối mỗi FN).
  - **Đạt (cơ chế)**: mã có, con số thời gian/tải của tiêu chí phải đo khi UAT.
  - **Thiếu**: chưa thấy mã; ghi rõ thiếu gì và issue (chưa có thì ghi "chưa có issue").
  - **Hoãn pha kết nối**: cần hệ ngoài, không nghiệm thu ở pha KN độc lập.
- Dòng bước, luồng phụ, ngoại lệ ghi Đạt dựa trên kiểm thử của FN; ý nào kiểm thử không nêu đích danh thì người kiểm xác nhận lại khi UAT.
- Cột **Kết quả UAT** để trống cho người kiểm điền (Đạt / Không đạt + ghi chú, ngày, người kiểm).
- Tài liệu sẽ chạy trên **profile chỉ-KN** (Compose profile `kn`, realm Keycloak `kn`, tenant KN mới, module HTX/CGH/MKG/Public
  API tắt) khi gói KN-STANDALONE-10B-PROFILE-CI (#272) gộp. Các FN dùng dữ liệu HTX, máy móc, thửa ruộng (FN-XA-03.03 … 03.05,
  03.08) dùng dữ liệu KN tự quản: thửa ruộng và lựa chọn HTX ở lịch công tác đã về kho KN (#271 đã gộp); máy móc và việc ẩn ứng viên HTX
  đã công bố còn ở #306.

## 2. Chuẩn bị dữ liệu trên một tenant KN mới

KN tự nhập toàn bộ dữ liệu, không lấy từ hệ ngoài. Trình tự theo Mục IV.4 của BRD:

| Bước | Người làm | Việc | Màn | FN |
|---|---|---|---|---|
| 1 | Admin | Nạp danh mục ĐVHC An Giang | `/kn/quan-tri/don-vi-hanh-chinh` | FN-AD-04.01 |
| 2 | Admin | Dựng cây TTKNQG › TTKN An Giang › Nút xã; gán Khu vực cho Nút xã | `/kn/quan-tri/co-cau-to-chuc` | FN-AD-04.02 |
| 3 | Admin | Khai báo phòng ban, bật Phòng Trồng trọt "Đang áp dụng" | `/kn/quan-tri/phong-ban` | FN-AD-04.03 |
| 4 | Admin | Tạo tài khoản kiểm (bảng dưới) | `/kn/quan-tri/nguoi-dung` | FN-AD-04.04 |
| 5 | Admin | Nạp danh mục nông nghiệp: giống lúa, vật tư, cây trồng, sâu bệnh, 14 loại máy, mô hình | `/kn/phong-ban/trong_trot/danh-muc` | FN-AD-04.05 |
| 6 | Admin | Khởi tạo 14 mẫu báo cáo, gán cho Phòng Trồng trọt, phát hành phiên bản (người phát hành khác người đề nghị) | `/kn/phong-ban/trong_trot/mau-bao-cao` | FN-AD-04.06 |
| 7 | Cấp Tỉnh | Phát hành gói tuần T01, T03, T04, T05, T06 cho 3 xã, nhập cột Tỉnh giao | `/kn/nhiem-vu/phat-hanh` | FN-TI-02.02 |
| 8 | Cấp Xã | Thêm 2 HTX, 3 nông hộ, khai máy, vẽ 2 thửa, ghi vật tư ở xã A | `/kn/du-lieu/htx`, `/kn/du-lieu/may-moc`, `/kn/du-lieu/ve-thua`, `/kn/du-lieu/vat-tu` | FN-XA-03.03 … 03.06 |
| 9 | Cấp Tỉnh | Khai báo email Chi cục (người nhận, CC) | `/kn/bao-cao/chi-cuc` | FN-TI-02.06 |

Tài khoản kiểm tối thiểu (mật khẩu do Admin đặt khi tạo, không ghi vào tài liệu):

| Ký hiệu | Vai trò | Phạm vi |
|---|---|---|
| AD1, AD2 | Admin | Toàn hệ thống (hai người để kiểm tách người đề nghị và người phát hành mẫu) |
| TW | Cấp Quốc gia · Phòng Trồng trọt | TTKNQG |
| T-TP | Cấp Tỉnh · Phòng Trồng trọt | Trưởng phòng (người duyệt lịch) |
| T-CB | Cấp Tỉnh · Phòng Trồng trọt | Cán bộ |
| X-A | Cấp Xã · Phòng Trồng trọt; thêm vai Cấp Quốc gia để kiểm nhiều vai | Xã A, xã B |
| X-C | Cấp Xã · Phòng Trồng trọt | Xã C (khác Khu vực) |
| X-K | Cấp Xã · phòng khác | Xã A (kiểm cách ly phòng ban) |

## 3. Chức năng hoãn, chờ đầu vào và khoảng trống

| Loại | Nội dung | FN | Issue |
|---|---|---|---|
| Hoãn pha kết nối | Tạo đường link Google Sheet và gửi link cho Chi cục. Email kèm tệp Excel/PDF và link tải vẫn nghiệm thu | FN-TI-02.06 (phần Google Sheet) | #40 |
| Chờ đầu vào | Cấu trúc 14 mẫu biểu theo tệp Excel gốc: T02, T03, T07, V03, V06, T05, T06, V01, V02 còn lệch; các mẫu còn lại chưa đối chiếu | FN-XA-03.02, FN-TI-02.05, FN-AD-04.06 | #157, #39 |
| Đang làm | Máy móc HTX về kho KN tự quản; ẩn ứng viên HTX đã công bố từ hệ ngoài (thửa ruộng và HTX ở lịch công tác đã xong ở #271) | FN-XA-03.04, FN-XA-03.03 | #306 |
| Đang làm | Menu từng phòng chỉ còn FN của phòng đó ("Quy trình, văn bản", "Trang chủ phòng", "Báo cáo đột xuất", "Dùng chung" ở Trồng trọt đang gỡ) | FN-CH-00.01 | #265 |
| Khoảng trống, chưa có issue | Đăng nhập nhanh PIN 6 số trên Mobile và dòng "số lần thử còn lại" (FN-CH-00.01); sắp xếp ĐVHC có ánh xạ mã và nạp ranh giới trên màn (FN-AD-04.01); "Lấy từ đợt trước" cho cột Tỉnh giao (FN-TI-02.02); tải tệp KML/Shapefile/GeoJSON khi vẽ thửa (FN-XA-03.05); nạp/xuất Excel danh sách HTX (FN-XA-03.03); số liệu so sánh cùng kỳ năm trước và báo cáo năm (FN-TW-01.02); đo tải 500 người dùng (FN-TW-01.01) | — | chưa có issue |

Chức năng #221 và #173 thuộc Phòng CNTS, xem [cnts-v2-acceptance.md](cnts-v2-acceptance.md).

## 4. Kịch bản theo chức năng

### FN-CH-00.01 — Đăng nhập, chọn vai trò & phòng ban

- Vai: Admin, Cấp Quốc gia, Cấp Tỉnh, Cấp Xã. Điều kiện đầu: tài khoản "Hoạt động" (FN-AD-04.04).
- Màn: `/login` → `/chon-vai-tro` → trang mặc định (`/kn/tong-quan` cho Admin). Mobile: màn đăng nhập, `kn_context_picker_screen`, `kn_home_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Nhập email hoặc số điện thoại, mật khẩu; lần đầu bắt buộc đổi mật khẩu | Đăng nhập X-C lần đầu bằng SĐT | Bị yêu cầu đổi mật khẩu rồi mới vào | Đạt | |
| B3 | 1 vai trò và 1 phòng ban → vào thẳng; nhiều → màn "Chọn vai trò và phòng ban"; Admin không chọn phòng ban | Đăng nhập X-C, X-A, AD1 | X-C vào thẳng; X-A thấy màn chọn; AD1 vào Tổng quan hệ thống | Đạt | |
| B4 | Phiên gắn Vai trò + Phòng ban, hiển thị menu tương ứng | X-A chọn Cấp Xã · Trồng trọt | Menu Cấp Xã (Hình 3f) | Đạt (còn mục ngoài BRD đang gỡ ở #265) | |
| LP | Đổi vai trò / phòng ban trong menu tài khoản mà không đăng nhập lại | X-A đổi sang Cấp Quốc gia | Menu đổi, không hỏi mật khẩu | Đạt | |
| LP | Mobile: đăng nhập nhanh bằng PIN 6 số | Mở lại app | Hỏi PIN | Thiếu: App KN không dùng PIN (ghi chú trong mã `apps/field-app/lib/ui/providers.dart`) — chưa có issue | |
| LP | Mobile: đăng nhập offline với phiên đã có, tối đa 72 giờ | Tắt mạng, mở app trong 72 giờ; rồi sau 72 giờ | Trong hạn vào được; quá hạn bắt đăng nhập | Đạt | |
| LP | Sai thông tin → banner lỗi, ghi số lần thử còn lại; sai 5 lần → khóa tạm 15 phút | Nhập sai 5 lần | Lần 6 bị khóa 15 phút | Đạt phần khóa (5 lần, 900 giây). Thiếu: "số lần thử còn lại" (chưa có issue) | |
| LP | Không có vai trò/phòng ban đang áp dụng hoặc bị khóa → thông báo liên hệ Admin | Đăng nhập tài khoản đã khóa | Thông báo liên hệ Admin | Đạt | |
| QT | Phiên hết hạn sau 24 giờ trên Mobile, 4 giờ không thao tác trên Web | Để web 4 giờ không thao tác | Bị đăng xuất | Đạt | |
| QT | Menu Admin: nhóm "Phòng ban" gom menu từng phòng; phòng chưa mở hiện mờ; nhóm "Quản trị hệ thống" | AD1 xem sidebar | Đúng 3 nhóm | Đạt | |
| AC | Vào đúng menu và phạm vi dữ liệu; đổi vai trò hiển thị đúng menu mới; Admin vào thẳng Tổng quan hệ thống | Như B3, LP | Như trên | Đạt | |
| AC | Phòng ban chuyển "Đang áp dụng" hiện ngay, không cần cập nhật ứng dụng | AD1 bật một phòng; X-A tải lại màn chọn | Thẻ phòng mới sáng | Đạt | |

Kiểm thử tự động: `TestFN_CH_00_01_MultiRoleContextsAndSwitch`, `TestFN_CH_00_01_ContextHeaderSpoofingRejected`, `TestFN_CH_00_01_AdminNoDepartment`,
`TestFN_CH_00_01_DefaultRouteMatchesMenuContract`, `TestUR_KT_02_MenuByConfigDepartmentToggle`; Flutter `kn_context_test.dart`, `kn_widgets_test.dart`.

### FN-TW-01.01 — Dashboard trồng trọt (Cấp Quốc gia)

- Vai: Cấp Quốc gia (chỉ xem). Điều kiện đầu: có báo cáo đã gửi của các xã.
- Màn: Dashboard — `/kn/dashboard`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Chọn tỉnh, vụ, kỳ; xem thẻ chỉ số, biểu đồ; bấm để xem chi tiết tới xã | TW chọn An Giang, tuần hiện tại | Có thẻ xuống giống, thu hoạch, sâu bệnh, số xã đã nộp | Đạt | |
| LP | Làm mới thủ công; xuất Excel; chưa có dữ liệu → "Chưa có dữ liệu" | Bấm Làm mới, Xuất Excel; chọn kỳ trống | Có tệp; kỳ trống hiện câu trống | Đạt | |
| QT | Chỉ đọc; mỗi thẻ ghi mốc cập nhật; số cấp trên bằng tổng cấp dưới; chỉ tính báo cáo đã gửi | So số dashboard với Báo cáo tổng hợp các xã | Bằng nhau; không có nút sửa | Đạt | |
| AC | Tải Dashboard dưới 5 giây với 500 người dùng đồng thời | Chạy kiểm tải trên môi trường triển khai | < 5 giây | Thiếu: chưa đo tải (hạ tầng; chưa có issue) | |

Kiểm thử tự động: `TestFN_TI_02_KN77FullCycle`, `TestFN_TW_01_01_LoadNote`, `TestFN_TI_02_01_ExtractMetrics`, `TestKN77AnGiangTuan36` (bước 8).

### FN-TW-01.02 — Xem báo cáo tổng hợp của Tỉnh và Xã

- Vai: Cấp Quốc gia. Điều kiện đầu: có báo cáo đã gửi.
- Màn: Báo cáo tuần / tháng / hàng vụ / năm — `/kn/bao-cao/tuan`, `/kn/bao-cao/thang`, `/kn/bao-cao/vu`, `/kn/bao-cao/nam`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B4 | Chọn loại, tỉnh, vụ, kỳ; chọn biểu (tab); xem bảng; bấm tên xã; xuất Excel/PDF | TW mở Báo cáo tuần, tab T01, xuất Excel | Bảng đủ xã, dòng tổng Khu vực và Toàn tỉnh; có tệp | Đạt | |
| LP | So sánh cùng kỳ năm trước | Bật "So sánh cùng kỳ năm trước" | Hiện số cùng kỳ | Thiếu: có ô chọn, chưa có số liệu lịch sử để so (chưa có issue) | |
| LP | Kỳ chưa chốt → nhãn "Tạm tính", liệt kê xã chưa nộp | Mở kỳ chưa chốt | Nhãn Tạm tính + danh sách xã | Đạt | |
| QT | Chỉ xem; toàn tỉnh = tổng các xã; năng suất dòng tổng = sản lượng / diện tích thu hoạch | Tính tay 1 dòng tổng | Khớp | Đạt | |
| AC | Số liệu toàn tỉnh bằng tổng các xã đã gửi; không có nút sửa | TW thử gọi cập nhật/gửi | Bị 403 chỉ đọc | Đạt | |
| Menu | Báo cáo năm | Mở `/kn/bao-cao/nam` | Có báo cáo năm theo mẫu | Thiếu: chưa thấy kiểm thử cho báo cáo năm (chưa có issue) | |

Kiểm thử tự động: `TestFN_TI_02_05_TotalsWeighted`, `TestFN_TI_02_KN77FullCycle`; Playwright `kn-p6s.spec.ts`.

### FN-TI-02.01 — Dashboard trồng trọt toàn tỉnh

- Vai: Cấp Tỉnh. Màn: Dashboard — `/kn/dashboard`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn Khu vực, vụ, kỳ; xem chỉ số; drill-down tới xã | T-CB lọc Khu vực của xã C | Chỉ số chỉ của Khu vực đó | Đạt | |
| B3 | Bấm "Nhắc nhở" ở khung xã chưa nộp | Bấm Nhắc nhở | Mở màn Trạng thái nộp / gửi nhắc | Đạt | |
| QT | Chỉ đọc; chỉ thấy dữ liệu Phòng Trồng trọt của tỉnh mình | Đăng nhập Cấp Tỉnh phòng khác | Không thấy số Trồng trọt | Đạt | |
| AC | Số liệu trùng với báo cáo tổng hợp FN-TI-02.05 cùng kỳ | So với `/kn/bao-cao/tong-hop` | Trùng | Đạt | |

Kiểm thử tự động: `TestFN_TI_02_KN77FullCycle`, `TestKN77AnGiangTuan36` (bước 8).

### FN-TI-02.02 — Phát hành biểu mẫu báo cáo

- Vai: Cấp Tỉnh. Điều kiện đầu: mẫu đã được Admin gán cho Phòng Trồng trọt.
- Màn: Điều hành › Nhiệm vụ › Phát hành biểu mẫu — `/kn/nhiem-vu/phat-hanh`; đợt đã phát hành — `/kn/nhiem-vu/phat-hanh/dot-da-phat-hanh`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Chọn biểu; phạm vi xã (Toàn tỉnh / Khu vực / từng xã); loại kỳ, từ–đến, hạn nộp, số ngày chốt; hệ thống tự chia kỳ | Chọn T01, T04; 3 xã; 2 tuần | Xem trước: 2 biểu × 3 xã × 2 kỳ = 12 công việc | Đạt | |
| B4 | Nhập giá trị cột "Tỉnh giao" cho từng xã (trực tiếp, nhập Excel hoặc lấy từ đợt trước) | Nhập trực tiếp; nạp tệp Excel | Giá trị hiện ở cột Tỉnh giao của xã | Đạt cho nhập trực tiếp và nạp Excel. Thiếu: "Lấy từ đợt trước" (chưa có issue) | |
| B5 | Xem trước → "Phát hành" | Bấm Phát hành | Có đợt mới; xã nhận việc | Đạt | |
| LP | Thu hồi đợt chưa có xã nào nộp; sửa hạn nộp chỉ áp dụng cho kỳ chưa đến hạn | Thu hồi đợt mới; sửa hạn kỳ sau | Thu hồi được; kỳ đã đến hạn không đổi | Đạt | |
| LP | Xã chưa có cán bộ phụ trách → cảnh báo; mẫu chưa có cấu trúc cột → chặn | Chọn xã chưa có cán bộ | Có cảnh báo | Đạt | |
| QT | Chỉ phát hành mẫu thuộc phòng mình; cột "Tỉnh giao" Cấp Xã chỉ xem | X-A mở biểu | Cột Tỉnh giao màu xám, không sửa được | Đạt | |
| AC | Cán bộ xã nhận công việc và thông báo ngay; cán bộ xã khác phòng ban không nhận | X-A và X-K mở Công việc cần làm | X-A có việc; X-K không | Đạt | |

Kiểm thử tự động: `TestFN_TI_02_KN77FullCycle`, `TestFN_TI_02_02_SplitWeeks`, `TestFN_TI_02_02_ProvincialImport`, `TestFN_TI_02_02_LayoutPeriodLabels`,
`TestFN_TI_02_04_AutoCloseAndReminders`; vitest `cycle/screens/PublishWizard.test.tsx`.

### FN-TI-02.03 — Theo dõi trạng thái nộp và nhắc nhở

- Vai: Cấp Tỉnh. Màn: Trạng thái nộp — `/kn/nhiem-vu/trang-thai-nop`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn đợt, biểu, kỳ; xem số xã Đã nộp / Lưu nháp / Chưa nhập / Trễ hạn | Mở kỳ có 1 xã nộp, 1 nháp, 1 chưa nhập | 4 thẻ đếm đúng | Đạt | |
| B3 | Chọn xã → "Gửi nhắc nhở" kèm nội dung; nhắc hiện ở "Công việc cần làm" | Nhắc xã C | X-C thấy nhắc | Đạt | |
| LP | Tự nhắc trước hạn và khi trễ hạn; xuất Excel; lịch sử nhắc | Xem lịch sử nhắc sau hạn | Có dòng nhắc tự động | Đạt | |
| LP | Xã chưa có cán bộ → "Chưa có cán bộ", không gửi được nhắc | Xem xã chưa gán cán bộ | Nhãn và nút nhắc khóa | Đạt | |
| AC | Số xã theo trạng thái đúng thời gian thực; cán bộ xã nhận nhắc trong 1 phút | X-A gửi; tải lại màn | Đếm đổi ngay; nhắc tới trong ≤ 1 phút | Đạt (cơ chế); đo 1 phút khi UAT | |

Kiểm thử tự động: `TestFN_TI_02_03_ReportCycleInbox`, `TestFN_TI_02_04_AutoCloseAndReminders`, `TestKN77AnGiangTuan36` (bước 6).

### FN-TI-02.04 — Rà soát báo cáo, yêu cầu điều chỉnh, chốt kỳ

- Vai: Cấp Tỉnh. Màn: Rà soát & chốt kỳ — `/kn/nhiem-vu/ra-soat`, `/kn/nhiem-vu/ra-soat/[id]`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Lọc; mở báo cáo đúng mẫu, chỉ xem; "Yêu cầu điều chỉnh" (bắt buộc lý do) hoặc "Xác nhận" | Yêu cầu điều chỉnh không lý do, rồi có lý do | Lần 1 bị chặn; lần 2 xã nhận yêu cầu | Đạt | |
| B4 | "Chốt kỳ ngay" hoặc hệ thống chốt tự động theo lịch | Chốt kỳ ngay | Kỳ Đã chốt; có lần tổng hợp "Hệ thống" | Đạt | |
| B5 | Mở lại kỳ đã chốt: kỳ, xã/biểu, lý do, thời hạn mở | Mở lại cho xã A | Xã A gửi lại được; xã khác vẫn chốt | Đạt | |
| LP | Báo cáo Chi cục đã gửi của kỳ được mở lại → cảnh báo "số liệu đã thay đổi" | Mở lại kỳ đã gửi Chi cục | Có cảnh báo ở màn Chi cục | Đạt | |
| QT | Cấp Tỉnh không sửa số liệu gốc của xã; mọi thao tác lưu vết | Tìm ô nhập ở màn rà soát; xem Log | Không có ô nhập; Log đủ | Đạt | |
| AC | Cán bộ xã nhận yêu cầu điều chỉnh kèm lý do; số liệu sau chốt kỳ không sửa được nếu chưa mở lại | X-A gửi sau chốt | Bị từ chối | Đạt | |

Kiểm thử tự động: `TestFN_TI_02_KN77FullCycle`, `TestFN_TI_02_04_AutoCloseAndReminders`, `TestKN77AnGiangTuan36` (bước 7, 9).

### FN-TI-02.05 — Báo cáo tổng hợp các xã (Báo cáo Xã)

- Vai: Cấp Tỉnh. Màn: Báo cáo › Báo cáo tổng hợp các xã — `/kn/bao-cao/tong-hop`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Hiện dữ liệu lần cập nhật gần nhất; có báo cáo mới → "Có n thay đổi mới từ m xã" | X-A gửi lại; T-CB mở màn | Dải thông báo vàng | Đạt | |
| B3–B4 | Bấm "Cập nhật" → tổng hợp lại, đánh dấu ô thay đổi, ghi nhật ký; rê chuột xem giá trị cũ, người sửa | Bấm Cập nhật | Ô đổi viền vàng ●; có dòng nhật ký | Đạt | |
| B5–B6 | Xem "Nhật ký cập nhật"; "Xuất Excel theo mẫu" hoặc PDF | Xuất Excel | Tệp đúng số trên bảng | Đạt | |
| LP | Không có thay đổi mới → "Dữ liệu đã mới nhất"; xã chưa nộp → "chưa nộp", nhãn "Tạm tính" | Bấm Cập nhật lần hai | Thông báo đã mới nhất | Đạt | |
| QT | Chỉ lấy báo cáo "Đã gửi"; Khu vực, Toàn tỉnh = tổng các xã; nhật ký không xóa; không tự cập nhật định kỳ | Lưu nháp ở xã rồi Cập nhật | Nháp không vào bảng | Đạt | |
| AC | Sau "Cập nhật" bảng đúng số liệu mới nhất; ô thay đổi có trong nhật ký; toàn tỉnh = tổng các xã | Cộng tay 3 xã | Khớp dòng Toàn tỉnh | Đạt | |
| AC | File Excel xuất ra đúng bố cục mẫu | So tệp xuất với tệp Excel gốc theo chữ cái cột | Trùng | Thiếu: 9 mẫu còn lệch tệp gốc, các mẫu khác chưa đối chiếu — #157, #39 (chờ đầu vào) | |

Kiểm thử tự động: `TestFN_TI_02_05_Diff`, `TestFN_TI_02_05_TotalsWeighted`, `TestFN_TI_02_KN77FullCycle`, `TestKN77AnGiangTuan36` (bước 7).

### FN-TI-02.06 — Xuất file và gửi báo cáo Chi cục

- Vai: Cấp Tỉnh. Điều kiện đầu: có dữ liệu tổng hợp của kỳ; đã khai báo email Chi cục; email công vụ đã cấu hình (hạ tầng KN).
- Màn: Báo cáo › Báo cáo Chi cục — `/kn/bao-cao/chi-cuc`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn kỳ, phạm vi, biểu, định dạng Excel theo mẫu / PDF → "Xuất file" | Xuất Excel + PDF | Có 2 tệp trong danh sách | Đạt | |
| B2 | Định dạng "Tạo đường link Google Sheet" | Chọn Google Sheet | Ô chọn tắt, có ghi chú chưa kết nối | Hoãn pha kết nối (#40); máy chủ trả 409 `reports.gsheet_disabled` | |
| B3–B4 | Nhập/chọn email Chi cục, CC; tiêu đề, nội dung tự điền; "Gửi email" kèm file, ghi log | Gửi email tới hộp thư kiểm | Thư có 2 tệp đính kèm + CC; trạng thái Đã gửi | Đạt | |
| LP | Chỉ tải file về máy; xem lịch sử gửi; gửi lại | Tải tệp; Gửi lại | Có dòng log mới | Đạt | |
| LP | Kỳ chưa chốt → file ghi "Tạm tính"; còn xã chưa nộp → in danh sách kèm file | Xuất kỳ chưa chốt | Tệp có "Tạm tính" và danh sách xã | Đạt | |
| LP | File vượt giới hạn đính kèm → chỉ gửi link | Gửi tệp lớn | Thư có link tải (hiệu lực 7 ngày) thay tệp | Đạt với link tải của hệ thống; link Google Sheet: hoãn (#40) | |
| LP | Gửi lỗi → ghi "Gửi lỗi", cho gửi lại | Nhập email sai tên miền | Trạng thái Gửi lỗi, có nút Gửi lại | Đạt | |
| QT | Email Chi cục lưu làm mặc định; log gửi lưu thời điểm, người gửi, người nhận, CC, kỳ, file/link, trạng thái | Mở lại màn; xem Log | Email điền sẵn; log đủ cột | Đạt | |
| AC | Số liệu trong file bằng số liệu tổng hợp của kỳ; Chi cục nhận email kèm file hoặc link mở được; mọi lần gửi có trong log | Mở tệp nhận được, so với bảng tổng hợp | Khớp | Đạt (không tính Google Sheet) | |

Kiểm thử tự động: `TestFN_TI_02_06_ChiCucExportAndEmail`, `TestFN_TI_02_06_SMTPAttachmentAndCC`, `TestFN_TI_02_06_GSheetDisabledByDefaultAndValve`,
`TestKN77AnGiangTuan36` (bước 10); vitest `cycle/screens/dispatch-resend.test.tsx`.

### FN-TI-02.07 — Lịch công tác (đăng ký và duyệt lịch Cấp Xã)

- Vai: Cấp Tỉnh (người duyệt: Trưởng phòng Trồng trọt). Màn: Lịch công tác — `/kn/nhiem-vu/lich-cong-tac`, `/kn/nhiem-vu/lich-cong-tac/[id]`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Tab "Chờ duyệt", chọn tuần; "Duyệt" hoặc "Yêu cầu chỉnh sửa" (bắt buộc lý do) | T-TP yêu cầu chỉnh sửa không lý do, rồi duyệt | Lần 1 bị chặn; duyệt xong X-A nhận thông báo | Đạt | |
| B3 | Tab "Lịch của tôi": đăng ký lịch tuần → "Gửi đăng ký" | T-CB đăng ký lịch | Lịch ở Chờ duyệt (người khác duyệt) | Đạt | |
| LP | Duyệt hàng loạt; xuất Excel/PDF; tab "Chưa đăng ký"; lịch chờ duyệt quá hạn → nhắc người duyệt | Duyệt hàng loạt 2 lịch; xuất Excel | Cả 2 Đã duyệt; có tệp | Đạt | |
| QT | Chỉ Cấp Tỉnh cùng phòng ban, cùng tỉnh xem, duyệt; lịch tuần đã kết thúc bị khóa; lưu lịch sử duyệt | X-C thử duyệt; sửa lịch tuần trước | 403; bị khóa | Đạt | |
| AC | Lịch cán bộ xã gửi hiện ngay ở "Chờ duyệt"; không xem được lịch phòng ban hoặc tỉnh khác | X-A gửi; T-TP tải lại | Hiện ngay | Đạt | |

Kiểm thử tự động: `TestFN_TI_02_07_SelfApprovalBlockedAndProvinceOwnSchedule`, `TestFN_TI_02_07_ScopeAndTenantIsolation`,
`TestFN_TI_02_07_UnregisteredBulkRemindersExport`, `TestFN_TI_02_07_EndedWeekLocked`; Playwright `kn-lich-cong-tac.spec.ts`.

### FN-XA-03.01 — Công việc cần làm

- Vai: Cấp Xã. Màn: Công việc cần làm (trang mặc định) — `/kn/nhiem-vu/cong-viec`, `/kn/nhiem-vu/cong-viec/[id]`. Mobile: `kn_tasks_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Liệt kê việc của mọi xã phụ trách: xã, biểu, kỳ, hạn, trạng thái; lọc; "Nhập" mở biểu đúng tab xã và kỳ | X-A bấm Nhập ở T01 xã B | Mở T01, tab xã B, đúng kỳ | Đạt | |
| LP | Thông báo đẩy trên Mobile; không có việc → "Chưa có công việc"; quá hạn → "Trễ hạn", vẫn nộp được đến khi chốt kỳ | Để quá hạn rồi nộp | Nhãn Trễ hạn; nộp được | Đạt (thông báo đẩy thật đo khi UAT) | |
| QT | Trạng thái do hệ thống cập nhật; chỉ thấy việc của xã và phòng ban được gán | X-K mở danh sách | Không thấy việc Trồng trọt | Đạt | |
| AC | Công việc mới hiện ngay sau phát hành; trạng thái chuyển "Đã nộp" ngay sau khi gửi | Gửi T01 | Đã nộp | Đạt | |

Kiểm thử tự động: `TestFN_XA_03_01_TaskStatus`, `TestFN_TI_02_KN77FullCycle`; Flutter `kn_widgets_test.dart` ("FN-XA-03.01 Công việc cần làm").

### FN-XA-03.02 — Nhập báo cáo theo biểu mẫu

- Vai: Cấp Xã (Admin nhập thay khi hỗ trợ). Điều kiện đầu: biểu đã phát hành cho xã; tài khoản đã gán xã.
- Màn: Báo cáo tuần — `/kn/bao-cao/tuan`; tháng — `/kn/bao-cao/thang`; hàng vụ — `/kn/bao-cao/vu`; hoặc mở từ Công việc cần làm.
  Mobile: `kn_report_entry_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn biểu, kỳ; chọn tab xã; chỉ hiện dòng của xã trong bảng đúng mẫu | X-A mở T01 | 2 tab (xã A, xã B), mỗi tab 1 dòng | Đạt | |
| B3 | Mẫu nhiều kỳ trên một dòng (PL 01, 01B, 02, 07): chỉ cột/khối của kỳ đang mở được nhập | Nhập ô của tuần khác | Ô khóa | Đạt | |
| B4–B5 | "Lưu nháp" hoặc "Gửi báo cáo"; trước chốt kỳ được sửa và gửi lại | Lưu nháp, gửi, sửa, gửi lại | Lịch sử có 2 phiên bản | Đạt | |
| LP | "Tải mẫu Excel" (chỉ dòng của xã) và "Nhập từ Excel" | Tải mẫu, điền, nhập lại | Số vào đúng ô | Đạt | |
| LP | Nhập offline trên Mobile, tự đồng bộ; tự lưu nháp khi chuyển tab | Tắt mạng, nhập, bật mạng | Một phiên bản, không trùng | Đạt | |
| LP | Vi phạm yêu cầu của mẫu → báo tại ô, không cho gửi; gửi sau chốt kỳ → từ chối | T03: tổng các trà khác số đã xuống giống | Lỗi tại ô, không gửi được | Đạt | |
| QT | Cột STT, Khu vực, Xã, Tỉnh giao cố định; cột công thức không sửa tay; mỗi lần gửi lưu lịch sử; Cấp Tỉnh không nhập đè | Sửa ô công thức | Ô khóa, tự tính | Đạt | |
| AC | Cán bộ xã không thấy dòng của xã khác; số liệu gửi hiện ngay trên báo cáo tổng hợp Cấp Tỉnh | X-A tìm xã C; T-CB bấm Cập nhật | Không có xã C; số mới vào bảng | Đạt | |
| AC | Bố cục bảng trùng sheet mẫu (đối chiếu theo chữ cái cột) | So từng biểu với tệp Excel gốc | Trùng | Thiếu: xem các dòng UR dưới — #157, #39 (chờ đầu vào) | |
| UR-XA-02 | T01 Tiến độ xuống giống lúa (PL 01): số tăng thêm, cột tuần trên cùng dòng | Nhập 2 tuần liên tiếp | Lũy kế cộng dồn | Đạt | |
| UR-XA-03 | T02 Xuống giống, phân trà lúa mùa (PL 01B) | Nhập T02 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-04 | T03 Phân trà lúa (PL 02): tổng các trà = số đã xuống giống ở PL 01 | Nhập T03 | Quy tắc tổng chạy đúng | Quy tắc tổng: Đạt. Cấu trúc cột: Thiếu — #157 | |
| UR-XA-05 | T04 Rau màu (PL 03): số lũy kế; 54 cây × 4 cột | Nhập T04 | Đủ 54 cây | Thiếu: chưa đối chiếu tệp gốc — #39 | |
| UR-XA-06 | T05 Sâu bệnh hại lúa (PL 04): 20 đối tượng × 3 mức | Nhập T05 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-07 | T06 Sâu bệnh hại cây trồng khác (PL 05): 70 cặp × 3 mức | Nhập T06 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-08 | T07 Tiến độ thu hoạch lúa (PL 07): khối 4 cột mỗi tuần | Nhập T07 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-09 | M01 Cây ăn trái, cây công nghiệp: 30 cây × 6 cột, trước ngày 10 | Nhập M01 | Cột "hiện có" tự tính | Thiếu: chưa đối chiếu tệp gốc — #39 | |
| UR-XA-10 | V01 Áp dụng 3 giảm 3 tăng (PL1) | Nhập V01 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-11 | V02 Áp dụng 1 phải 5 giảm (PL2) | Nhập V02 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-12 | V03 Trang thiết bị cơ giới hóa (PL3 CGH): 14 loại máy | Nhập V03 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |
| UR-XA-13 | V04 Cơ cấu giống lúa (PL4 CCG): 40 giống; DTXG = tổng các giống | Nhập V04 | Đúng cột mẫu | Thiếu: chưa đối chiếu tệp gốc — #39 | |
| UR-XA-14 | V05 Lượng giống gieo sạ (PL5): 4 nhóm lượng giống | Nhập V05 | Đúng cột mẫu | Thiếu: chưa đối chiếu tệp gốc — #39 | |
| UR-XA-15 | V06 Diện tích tham gia mô hình (PL6) | Nhập V06 | Đúng cột mẫu | Thiếu: cấu trúc lệch tệp gốc — #157 | |

Kiểm thử tự động: `TestFN_XA_03_02_T01Layout`, `TestFN_XA_03_02_T03TraEqualsSown`, `TestFN_XA_03_02_OnlyOpenPeriodEditable`, `TestFN_XA_03_02_RequiredAndRange`,
`TestFN_XA_03_02_OfflineReportSync`, `TestFN_AD_04_06_SeedsValid`; vitest `cycle/screens/XaScreens.test.tsx`; Flutter `kn_report_test.dart`, `kn_widgets_test.dart`.

### FN-XA-03.03 — Quản lý HTX & nông hộ

- Vai: Cấp Xã (thêm, sửa); Cấp Tỉnh, Cấp Quốc gia (xem); Admin (nạp ban đầu). Điều kiện đầu: danh mục ĐVHC.
- Màn: Dữ liệu › Thu thập dữ liệu › Danh sách HTX — `/kn/du-lieu/htx` (`…/them-moi`, `…/[id]`, `…/[id]/sua`, `…/[id]/them-nong-ho`,
  `/kn/du-lieu/htx/nong-ho`). Mobile: `kn_cooperatives_screen`. Dữ liệu do KN tự nhập (hồ sơ KN), không lấy từ hệ HTX.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn xã; xem danh sách, tìm, lọc; thêm / sửa / vô hiệu hóa HTX | Thêm HTX ở xã A; sửa diện tích | HTX có trong danh sách; Log ghi cũ → mới | Đạt | |
| B3 | Mở HTX → tab "Nông hộ" để thêm, sửa nông hộ | Thêm 1 nông hộ | Số nông hộ tăng 1 | Đạt | |
| B4 | Xuất Excel | Bấm Xuất Excel | Có tệp | Thiếu: chưa thấy xuất Excel ở màn HTX của KN (chưa có issue) | |
| LP | Nạp hàng loạt từ Excel; dòng lỗi → chỉ nạp dòng hợp lệ | Nạp tệp có 1 dòng lỗi | Báo dòng lỗi, dòng khác vào | Thiếu: chưa thấy nạp Excel ở màn HTX của KN (chưa có issue) | |
| LP | Vô hiệu hóa HTX có thửa ruộng, máy móc → cảnh báo | Vô hiệu hóa HTX đã khai máy | Cảnh báo số máy, thửa; bắt lý do | Đạt | |
| QT | Chỉ thao tác trong xã phụ trách; vô hiệu hóa thay xóa cứng; SĐT che một phần | X-A thêm HTX ở xã C; xem cột SĐT | 403; SĐT che | Đạt | |
| AC | Không thao tác được ngoài phạm vi; không xóa được HTX đã có dữ liệu liên kết | Tìm nút Xóa | Không có, chỉ Vô hiệu hóa | Đạt | |
| Độc lập | Chạy khi tenant KN tắt module HTX; không hiện ứng viên HTX đã công bố từ hệ ngoài | Chạy trên profile chỉ-KN | Mọi bước trên vẫn chạy | Đạt phần HTX, nông hộ là hồ sơ KN. Thiếu: ẩn ứng viên HTX đã công bố — #306 | |

Kiểm thử tự động: `TestFN_XA_03_03_DeactivateReasonMinMatchesLedgerCheck`, `TestKN77AnGiangTuan36` (bước 12);
vitest `cycle/screens/tt-htx.test.tsx`, `producer.test.tsx`; Flutter `kn_widgets_test.dart` ("FN-XA-03.03 danh sách HTX").

### FN-XA-03.04 — Quản lý máy móc của HTX

- Vai: Cấp Xã (thêm, sửa); Cấp Tỉnh, Cấp Quốc gia (xem). Điều kiện đầu: HTX đã có; danh mục loại máy.
- Màn: Máy móc HTX — `/kn/du-lieu/may-moc`. Mobile: `kn_machines_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn HTX; thêm / sửa / xóa dòng: loại máy, số lượng, ghi chú | Khai 2 máy gặt, 1 máy cày | Lưu được | Đạt | |
| B3 | Xem tổng số máy theo loại của xã | Xem tổng xã A | = tổng các HTX | Đạt | |
| LP | Số lượng âm hoặc không nguyên → chặn; trùng loại máy trong HTX → gợi ý cập nhật dòng đã có | Nhập −1; 1,5; thêm trùng loại | Bị chặn; có gợi ý | Đạt | |
| QT | Loại máy chọn từ danh mục; lưu lịch sử thay đổi số lượng | Sửa số lượng; xem Log | Có cũ → mới | Đạt | |
| AC | Tổng số máy theo loại của xã bằng tổng các HTX | Cộng tay | Khớp | Đạt | |
| Độc lập | Số máy lưu ở kho KN, chạy khi tắt module CGH/HTX | Chạy trên profile chỉ-KN | Mọi bước trên vẫn chạy | Thiếu: #306 | |

Kiểm thử tự động: `TestFN_XA_03_04_MachineCountsDeclareTotalsLog`, `TestFN_XA_03_04_MachineCountsScopeReadOnlyTenant`; vitest `cycle/screens/tt-mach.test.tsx`.

### FN-XA-03.05 — Vẽ ranh giới thửa ruộng

- Vai: Cấp Xã; Admin. Điều kiện đầu: tài khoản đã gán xã; Mobile đã tải bản đồ nền offline.
- Màn: Vẽ thửa ruộng — `/kn/du-lieu/ve-thua`, `/kn/du-lieu/ve-thua/them-moi`, `/kn/du-lieu/ve-thua/[id]`. Mobile: `kn_plots_screen`, `boundary_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Mở bản đồ xã và danh sách thửa; "Vẽ lô mới": chọn HTX, tên nông dân, tên lô, ghi chú | Vẽ lô mới ở xã A | Mở màn vẽ | Đạt | |
| B3–B4 | Vẽ bằng chấm điểm, hình tròn hoặc hình vuông, tối thiểu 3 điểm; hệ thống khép vùng, tính diện tích, đối chiếu diện tích khai báo → "Lưu ranh giới" | Chấm 4 điểm, lưu | Diện tích tự tính, có so sánh với khai báo | Đạt | |
| LP | Tải tệp KML/Shapefile/GeoJSON | Tải 1 tệp GeoJSON | Thửa hiện trên bản đồ | Thiếu: chưa thấy tải tệp ở màn vẽ thửa của KN (chưa có issue) | |
| LP | Chưa đủ 3 điểm → chặn; chồng lấn thửa đã lưu → cảnh báo, hiển thị phần chồng lấn | Lưu 2 điểm; vẽ đè thửa cũ | Bị chặn; báo chồng lấn | Đạt | |
| LP | Mất mạng → lưu tạm, tự đồng bộ | Vẽ trên app khi mất mạng | Đồng bộ đủ khi có mạng | Đạt | |
| QT | Diện tích luôn tự tính, không nhập tay đè; chỉ vẽ trong địa bàn được phân công | Vẽ ở xã C | 403 | Đạt | |
| AC | Không lưu được ranh giới không khép kín hoặc chồng lấn; vẽ offline đồng bộ đủ 100% | Như LP | Như trên | Đạt | |
| Độc lập | Thửa ruộng lưu ở kho KN, chạy khi tắt module HTX | Chạy trên profile chỉ-KN | Mọi bước trên vẫn chạy | Đạt (#271: thửa ở kho KN, chạy khi tắt module HTX) | |

Kiểm thử tự động: `TestFN_XA_03_05_StandaloneWithHTXModulesDisabled`, `TestFN_XA_03_05_OverlapAndGeometry`, `TestFN_XA_03_05_RolesScopeTenantDepartment`,
`TestFN_XA_03_05_OfflineSync`; vitest `cycle/screens/tt-plot.test.tsx`.

### FN-XA-03.06 — Nhập thông tin vật tư

- Vai: Cấp Xã (thêm, sửa); Cấp Tỉnh (xem). Điều kiện đầu: danh mục Vật tư.
- Màn: Vật tư — `/kn/du-lieu/vat-tu`. Mobile: `kn_materials_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Chọn xã, thời điểm; thêm dòng: loại vật tư, số lượng, đơn vị tính, ghi chú; xem lịch sử | Ghi 2 dòng vật tư | Có trong lịch sử ghi nhận | Đạt | |
| LP | Nạp từ Excel; giá trị không hợp lệ theo đơn vị tính → chặn | Nhập số âm, chuỗi `1e100000` | Bị chặn | Đạt | |
| QT | Loại vật tư chọn từ danh mục | Gõ tên tự do | Không cho | Đạt | |
| AC | Cấp Tỉnh xem được tổng hợp vật tư theo xã | T-CB mở màn Vật tư | Có tổng theo xã, không có nút sửa | Đạt | |

Kiểm thử tự động: `TestFN_XA_03_06_QuantityUnitAndValidation`, `TestFN_XA_03_06_RecordCorrectLogIdempotent`, `TestFN_XA_03_06_ScopeRolesDepartmentTenantSummary`;
vitest `cycle/screens/MaterialsScreen.test.tsx`.

### FN-XA-03.07 — Báo cáo của xã

- Vai: Cấp Xã. Màn: Báo cáo › Báo cáo của xã — `/kn/bao-cao/cua-xa`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn tab xã, loại kỳ, vụ, biểu; xem danh sách báo cáo (biểu, kỳ, trạng thái, gửi lúc, chốt kỳ) | X-A mở tab xã A | Đủ báo cáo đã gửi | Đạt | |
| B3 | "Xem" mở chỉ đọc; "Sửa" nếu chưa chốt; "Xuất Excel" | Xem 1 báo cáo đã chốt; xuất Excel | Không có Sửa ở báo cáo đã chốt; có tệp | Đạt | |
| QT | Chỉ báo cáo của xã được phân công | Tìm xã C | Không có | Đạt | |
| AC | Số liệu thống kê khớp báo cáo đã gửi | So 1 chỉ tiêu | Khớp | Đạt | |

Kiểm thử tự động: `TestFN_TI_02_KN77FullCycle`; vitest `cycle/screens/XaScreens.test.tsx`; Playwright `kn-p6s.spec.ts`.

### FN-XA-03.08 — Lịch công tác (đăng ký hằng tuần)

- Vai: Cấp Xã. Màn: Lịch công tác — `/kn/nhiem-vu/lich-cong-tac`. Mobile: `work_schedule_screen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn tuần (mặc định tuần kế tiếp); thêm mục công tác: nội dung, địa điểm (xã, HTX), thành phần phối hợp, ghi chú | Thêm 3 mục, 1 mục chọn HTX | Lưu nháp được | Đạt (HTX chọn từ hồ sơ KN, #271) | |
| B3–B4 | "Gửi duyệt" → "Chờ duyệt"; nhận kết quả; "Yêu cầu chỉnh sửa" thì sửa và gửi lại | Gửi; T-TP yêu cầu chỉnh sửa; sửa, gửi lại | Trạng thái đi đúng vòng | Đạt | |
| LP | Sao chép tuần trước; điều chỉnh lịch đã duyệt → phần điều chỉnh chờ duyệt lại | Sửa lịch đã duyệt | Về Chờ duyệt | Đạt | |
| LP | Trùng thời gian → cảnh báo; quá hạn đăng ký → nhãn "Đăng ký muộn" | Thêm 2 mục trùng giờ; gửi sau hạn | Có cảnh báo; nhãn Đăng ký muộn | Đạt | |
| QT | Nháp → Chờ duyệt → Đã duyệt / Yêu cầu chỉnh sửa; lịch tuần đã kết thúc bị khóa | Sửa lịch tuần trước | Bị khóa | Đạt | |
| AC | Cán bộ xã nhận thông báo kết quả duyệt kèm lý do | Xem chuông sau khi T-TP trả lời | Có thông báo + lý do | Đạt | |

Kiểm thử tự động: `TestFN_XA_03_08_RegisterSubmitReviewFlow`, `TestFN_XA_03_08_AmendmentVersioning`, `TestFN_XA_03_08_CopyPreviousAndValidation`, `TestFN_XA_03_08_ScheduleHTXFromKNRecords`,
`TestFN_XA_03_08_WeekRules`; vitest `schedule/schedule.test.tsx`; Playwright `kn-lich-cong-tac.spec.ts`.

### FN-AD-04.01 — Danh mục đơn vị hành chính

- Vai: Admin. Màn: Quản trị hệ thống › Đơn vị hành chính — `/kn/quan-tri/don-vi-hanh-chinh`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1 | Nạp danh mục: mã, tên, cấp, loại, mã cấp trên, ngày hiệu lực | Nạp danh mục An Giang | Bảng có Mã, Tên, Cấp, Hiệu lực từ, Trạng thái | Đạt (nạp qua Admin API; màn hiện danh mục) | |
| B2 | Bổ sung tọa độ trung tâm và ranh giới (GeoJSON/Shapefile) | Tải ranh giới 1 xã | Xã có ranh giới | Thiếu: chưa thấy nạp ranh giới trên màn KN (chưa có issue) | |
| B3 | Sắp xếp ĐVHC: khai báo ĐVHC mới, chọn ĐVHC cũ hợp nhất; lưu ánh xạ mã, chuyển ĐVHC cũ "Hết hiệu lực" | Hợp nhất 2 xã thử | Có ánh xạ; xã cũ Hết hiệu lực | Thiếu: chưa thấy mã ánh xạ/hợp nhất (chưa có issue) | |
| LP | Dòng lỗi khi nạp → chỉ nạp dòng hợp lệ; vô hiệu hóa ĐVHC đang liên kết dữ liệu khi chưa có ánh xạ → chặn | Nạp tệp có dòng lỗi | Dòng hợp lệ vào | Đạt phần nạp. Thiếu: chặn theo ánh xạ (đi cùng B3) | |
| QT | Mã ĐVHC duy nhất, không tái sử dụng; không xóa, chỉ "Hết hiệu lực" | Tìm nút Xóa | Không có | Đạt | |
| AC | Hiển thị đúng tỉnh An Giang và các xã/phường; không chọn được ĐVHC ngoài danh mục ở các chức năng khác | Thêm HTX với mã xã lạ (API) | Bị chặn | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_01_Catalog`, `TestFN_AD_04_01_NormalizeCatalog`.

### FN-AD-04.02 — Cơ cấu tổ chức 3 cấp

- Vai: Admin. Màn: Cơ cấu tổ chức 3 cấp — `/kn/quan-tri/co-cau-to-chuc`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Thêm Nút: Nút cha, tên, mã, cấp, mã ĐVHC, Khu vực; sửa Nút | Gán Khu vực cho Nút xã A | Cây hiện Khu vực | Đạt | |
| B3 | Di chuyển Nút xã sang Nút tỉnh khác: xem bảng ảnh hưởng → lý do → xác nhận | Di chuyển 1 Nút xã thử | Hiện bảng ảnh hưởng; bắt lý do | Đạt | |
| B4 | Vô hiệu hóa Nút khi sáp nhập/giải thể | Vô hiệu hóa Nút thử | Nút mờ, dữ liệu cũ còn | Đạt | |
| LP | Xóa Nút có Nút con, tài khoản, dữ liệu → chặn, gợi ý vô hiệu hóa | Xóa Nút xã A | Bị chặn | Đạt | |
| QT | Xem xuống, không xem ngang, không xem lên; không di chuyển Nút cấp 1, cấp 2; số liệu đã chốt giữ theo cấu trúc lúc chốt | Đổi Khu vực xã C sau chốt; mở bảng đã chốt | Bảng đã chốt giữ Khu vực cũ | Đạt | |
| AC | Cây hiển thị trực quan; di chuyển Nút cập nhật ngay phạm vi tài khoản; báo cáo nhóm đúng theo Khu vực | Mở Báo cáo tổng hợp sau khi gán Khu vực | Dòng tổng theo Khu vực đúng | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_02_ImpactAndTreeScope`, `TestFN_AD_04_02_RegionAssignmentCompensating`, `TestFN_AD_04_02_SumByRegion`,
`TestFN_AD_04_02_AnGiangCommunesRegionSums`, `TestKN77AnGiangTuan36` (bước 1, 9).

### FN-AD-04.03 — Danh mục phòng ban

- Vai: Admin. Màn: Quản trị hệ thống › Phòng ban — `/kn/quan-tri/phong-ban`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Xem danh sách: mã, tên, trạng thái, thứ tự, số tài khoản; thêm/sửa; đổi trạng thái Đang áp dụng / Chưa mở / Ngưng hoạt động | Thêm phòng thử, đổi trạng thái | Có log thay đổi | Đạt | |
| LP | Trùng mã trong đơn vị → chặn; ngưng phòng ban có tài khoản/dữ liệu → cảnh báo, dữ liệu cũ giữ nguyên | Thêm trùng mã; ngưng phòng có dữ liệu | Bị chặn; có cảnh báo số bản ghi | Đạt | |
| QT | Chỉ phòng ban "Đang áp dụng" chọn được khi đăng nhập; không xóa phòng ban đã có tài khoản hoặc dữ liệu | Tìm nút Xóa ở Phòng Trồng trọt | Không có | Đạt | |
| AC | Chuyển "Đang áp dụng" thì hiện ngay ở bước 2 đăng nhập | Bật phòng; tải lại `/chon-vai-tro` | Thẻ sáng | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_03_DepartmentLifecycleAuditLog`, `TestFN_AD_04_03_NormalizeDepartment`, `TestUR_KT_02_MenuByConfigDepartmentToggle`.

### FN-AD-04.04 — Tài khoản và phân quyền

- Vai: Admin. Màn: Quản trị hệ thống › Người dùng — `/kn/quan-tri/nguoi-dung`, `…/them-moi`, `…/[id]`, `…/[id]/vai-tro/them-moi`,
  `…/[id]/vai-tro/[aid]/sua`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Danh sách tài khoản, lọc theo vai trò, Nút, phòng ban, trạng thái; "Tạo tài khoản" hoặc mở Detail | Tạo X-A | Mở màn Detail | Đạt | |
| B3–B4 | Sửa thông tin; thêm, sửa, xóa vai trò (vai trò, cấp quản lý, Nút, phòng ban, xã phụ trách); "Lưu thay đổi" ghi Log; tài khoản mới nhận kích hoạt | Thêm vai Cấp Quốc gia cho X-A | Log có dòng mới: người làm, thời điểm, cũ → mới | Đạt | |
| B5 | "Gửi lại mật khẩu", "Khóa tài khoản" / "Mở khóa"; mỗi thao tác ghi log | Khóa rồi mở X-C | 2 dòng log | Đạt | |
| LP | Nạp tài khoản từ Excel; trùng tên đăng nhập/SĐT/email → từ chối | Nạp tệp 2 dòng (1 trùng) | 1 vào, 1 báo lỗi | Đạt | |
| LP | Cấp Xã chưa gán xã → cho lưu, cảnh báo, chưa xem được dữ liệu nghiệp vụ | Tạo Cấp Xã không gán xã | Lưu được + cảnh báo | Đạt | |
| QT | Chỉ Admin quản lý tài khoản; khóa tài khoản thu hồi ngay mọi phiên; xóa vai trò không xóa dữ liệu đã nhập | Khóa X-C khi đang đăng nhập | Thao tác kế tiếp của X-C bị từ chối | Đạt | |
| AC | Vai trò khác Admin không thấy menu "Người dùng"; hai cán bộ xã khác phòng ban trên cùng xã chỉ thấy dữ liệu phòng mình | X-A và X-K mở Công việc cần làm | Mỗi người chỉ thấy việc phòng mình | Đạt | |
| AC | Mọi lần lưu, khóa, gửi lại mật khẩu, thêm/xóa vai trò hiện trong Log với người thực hiện, thời điểm, cũ → mới | Mở tab Log của X-A | Đủ các dòng | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_04_CreateAccountRules`, `TestFN_AD_04_04_AssignmentLog`, `TestFN_AD_04_04_ImportMultiRoleAccounts`,
`TestFN_AD_04_04_RevocationAndDepartmentStatusTakeEffect`, `TestFN_AD_04_04_ScopeIsolationPerContext`, `TestFN_AD_04_04_TenantIsolation`.

### FN-AD-04.05 — Danh mục nông nghiệp

- Vai: Admin. Màn: Phòng ban › Phòng Trồng trọt › Danh mục nông nghiệp — `/kn/phong-ban/trong_trot/danh-muc`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Chọn danh mục (tab): Giống lúa, Vật tư, Cây trồng, Sâu bệnh, Loại máy móc, Mô hình sản xuất; xem mã, tên, nhóm, đơn vị tính, biểu đang dùng, trạng thái; thêm / sửa / vô hiệu hóa | Thêm 1 giống lúa | Có trong tab Giống lúa | Đạt | |
| LP | Nạp/xuất Excel; trùng mã/tên → chặn; vô hiệu hóa mục đã có số liệu → cho phép, số liệu cũ giữ nguyên | Thêm trùng mã; vô hiệu hóa 1 vật tư đã dùng | Bị chặn; mục chuyển Ngừng dùng | Đạt | |
| QT | Vai trò khác chỉ chọn, không nhập tự do; không xóa cứng mục đã dùng; danh mục loại máy 14 loại theo PL3 CGH | Đếm tab Loại máy móc | 14 loại | Đạt | |
| AC | Mục mới hiện ngay trong lựa chọn của biểu và màn nhập liệu | X-A mở `/kn/du-lieu/vat-tu` | Chọn được vật tư mới | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_05_AgriCatalogs`.

### FN-AD-04.06 — Quản lý mẫu báo cáo

- Vai: Admin. Điều kiện đầu: danh mục phòng ban và danh mục nông nghiệp đã có.
- Màn: Phòng ban › Phòng Trồng trọt › Mẫu báo cáo — `/kn/phong-ban/trong_trot/mau-bao-cao`, `…/[id]`, `…/[id]/v/[v]`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1 | Chọn phòng ban, cấp, loại báo cáo, mẫu; hoặc "Tạo mẫu mới" (trống / nhân bản / từ Excel) | Nhân bản T01; tạo mẫu từ tệp Excel | Có 2 mẫu nháp | Đạt | |
| B2–B3 | Chỉnh cấu trúc: thêm/xóa cột, dòng tiêu đề, dòng chỉ tiêu, gộp/tách ô, công thức; thuộc tính cột (Cố định / Tỉnh giao / Nhập / Cột theo kỳ / Công thức) | Thêm 1 cột Nhập và 1 cột Công thức | Lưới đổi theo; công thức tính đúng | Đạt | |
| B5 | "Xem trước màn hình nhập liệu" → "Lưu nháp" hoặc "Phát hành phiên bản" | AD1 đề nghị; AD1 tự phát hành; AD2 phát hành | AD1 bị từ chối; AD2 phát hành được | Đạt | |
| B6 | Gán mẫu vào loại báo cáo của phòng ban | Gán mẫu mới cho Trồng trọt | Mẫu hiện ở màn Phát hành của Cấp Tỉnh | Đạt | |
| LP | Lịch sử phiên bản; khôi phục phiên bản cũ thành nháp; sửa mẫu đã có số liệu → chỉ tạo phiên bản mới, áp dụng từ kỳ sau | Sửa T01 sau khi đã có số | Kỳ cũ giữ phiên bản cũ | Đạt | |
| LP | Công thức tham chiếu ô không tồn tại → báo lỗi, không lưu; mẫu chưa gán loại báo cáo → không phát hành được | Nhập công thức sai; phát hành mẫu chưa gán | Cả hai bị chặn | Đạt | |
| QT | Chỉ Admin chỉnh sửa và tạo mẫu; mỗi mẫu có phiên bản; mẫu thuộc đúng một phòng ban và một cấp nhập liệu | T-CB mở màn mẫu | Không có menu; 403 | Đạt | |
| QT | Khởi tạo 14 mẫu Phòng Trồng trọt từ bộ Excel đang dùng (các ghi chú PL 01, PL 02, PL 07, PL1 3G3T, PL2 1P5G, nhóm lượng giống) | So 14 mẫu khởi tạo với tệp Excel gốc | Trùng cấu trúc | Thiếu: 9 mẫu còn lệch, các mẫu khác chưa đối chiếu — #157, #39 (chờ đầu vào) | |
| QT | Loại "Báo cáo đột xuất": menu chỉ hiện khi phòng ban đã có mẫu loại này | Gán mẫu loại đột xuất; xem menu Cấp Xã | Có mục "Báo cáo đột xuất" `/kn/bao-cao/dot-xuat` | Đạt trên dev; mục menu này đang được xem lại ở #265 | |
| AC | Thêm/bớt cột, dòng hiện đúng trên màn nhập và bảng tổng hợp từ kỳ áp dụng; mẫu mới gán cho phòng ban hiện trong danh sách phát hành | Phát hành kỳ mới sau khi thêm cột; X-A mở biểu | Cột mới có ở màn nhập và bảng tổng hợp | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_06_DesignerOps`, `TestFN_AD_04_06_FormLifecycleSoD`, `TestFN_AD_04_06_FormulaGolden`, `TestFN_AD_04_06_ImportCustomerSheet`,
`TestFN_AD_04_06_UnassignedCannotPublish`, `TestFN_AD_04_06_SeedsValid`; Playwright `kn-forms.spec.ts`.

### FN-AD-04.07 — Menu các cấp (hỗ trợ người dùng)

- Vai: Admin. Điều kiện đầu: tài khoản Admin xác thực nhiều yếu tố.
- Màn: Quản trị hệ thống › Menu các cấp — `/kn/quan-tri/menu-cac-cap`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn vai trò và tài khoản/xã cần hỗ trợ; "Vào menu" → hiện menu của vai trò đó với dải cảnh báo đang hỗ trợ | AD1 chọn Cấp Xã, tài khoản X-C, nhập lý do, Vào menu | Menu Cấp Xã + dải vàng "Đang hỗ trợ" | Đạt | |
| B3 | Thao tác; khi lưu, nhập lý do | Nhập thay T01 xã C không lý do, rồi có lý do | Lần 1 bị chặn; lần 2 lưu, ghi "nhập thay cho X-C" | Đạt | |
| B4 | Xem nhật ký | Mở bảng Nhật ký thao tác Admin | Có thời điểm, Admin, thay cho ai, chức năng, nội dung, lý do | Đạt | |
| LP | Thoát chế độ hỗ trợ về menu Admin | Bấm thoát | Về menu Admin | Đạt | |
| QT | Không xóa nhật ký | Tìm nút xóa | Không có | Đạt | |
| AC | Mọi thao tác hỗ trợ đều có trong nhật ký | So số thao tác với số dòng nhật ký | Đủ | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_07_SupportModeAuditTrail`, `TestKN77AnGiangTuan36` (bước 5, 7).

## 5. Tổng hợp

| FN | Tên | Trạng thái trên dev |
|---|---|---|
| FN-CH-00.01 | Đăng nhập, chọn vai trò & phòng ban | Thiếu: PIN Mobile, số lần thử còn lại |
| FN-TW-01.01 | Dashboard (Cấp Quốc gia) | Thiếu: đo tải 500 người dùng |
| FN-TW-01.02 | Báo cáo tổng hợp của Tỉnh và Xã | Thiếu: số liệu so sánh cùng kỳ, báo cáo năm |
| FN-TI-02.01 | Dashboard toàn tỉnh | Đạt |
| FN-TI-02.02 | Phát hành biểu mẫu | Thiếu: "Lấy từ đợt trước" cho cột Tỉnh giao |
| FN-TI-02.03 | Trạng thái nộp, nhắc nhở | Đạt |
| FN-TI-02.04 | Rà soát, chốt kỳ | Đạt |
| FN-TI-02.05 | Báo cáo tổng hợp các xã | Thiếu: Excel đúng mẫu gốc (#157, #39) |
| FN-TI-02.06 | Xuất file, gửi Chi cục | Đạt; phần Google Sheet hoãn pha kết nối (#40) |
| FN-TI-02.07 | Duyệt lịch công tác | Đạt |
| FN-XA-03.01 | Công việc cần làm | Đạt |
| FN-XA-03.02 | Nhập báo cáo theo biểu mẫu | Thiếu: cấu trúc 13/14 mẫu theo tệp gốc (#157, #39) |
| FN-XA-03.03 | HTX & nông hộ | Thiếu: nạp/xuất Excel; ẩn ứng viên HTX đã công bố (#306) |
| FN-XA-03.04 | Máy móc của HTX | Thiếu: số máy về kho KN (#306) |
| FN-XA-03.05 | Vẽ ranh giới thửa ruộng | Thiếu: tải tệp KML/Shapefile/GeoJSON |
| FN-XA-03.06 | Vật tư | Đạt |
| FN-XA-03.07 | Báo cáo của xã | Đạt |
| FN-XA-03.08 | Đăng ký lịch công tác | Đạt |
| FN-AD-04.01 | Đơn vị hành chính | Thiếu: sắp xếp ĐVHC có ánh xạ, nạp ranh giới |
| FN-AD-04.02 | Cơ cấu tổ chức 3 cấp | Đạt |
| FN-AD-04.03 | Danh mục phòng ban | Đạt |
| FN-AD-04.04 | Tài khoản và phân quyền | Đạt |
| FN-AD-04.05 | Danh mục nông nghiệp | Đạt |
| FN-AD-04.06 | Quản lý mẫu báo cáo | Thiếu: 14 mẫu khởi tạo theo tệp gốc (#157, #39) |
| FN-AD-04.07 | Menu các cấp | Đạt |

Đạt trọn: 14/25. Còn thiếu ít nhất một ý: 11/25; lý do chính là 14 mẫu biểu chưa khớp tệp Excel gốc (#157, #39 — chờ đầu vào),
số máy và ứng viên HTX còn chờ #306 và các ý nhỏ chưa có issue (mục 3). Hoãn pha kết nối: phần Google Sheet
của FN-TI-02.06 (#40).
