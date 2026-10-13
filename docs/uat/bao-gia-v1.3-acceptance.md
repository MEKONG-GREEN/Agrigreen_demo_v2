# Nghiệm thu KN Báo giá nông sản v1.3: yêu cầu → kiểm thử

**Khung** lập ngày 13/10/2026 ở gói KHTCQA (nhánh `khtcqa`, từ `origin/dev`). Định dạng giống [kn-v7.7-acceptance.md](kn-v7.7-acceptance.md):
- Mỗi yêu cầu có đúng một dòng.
- Mã mang tiền tố `BG-` (Q-049).
- Tên kiểm thử ghi vào bảng tra được trong mã. Tên ghi *(dự kiến)* là tên đề nghị, chưa có trong mã.

Báo giá vào **G1 (15/12/2026) với dữ liệu thật** theo QĐ-5 (a). Mã có ở W11.71–W11.76 (`pf/price`, ADR-0049):
- W11.71–W11.76 đã gộp vào `dev`. W11.76 (duyệt / mở duyệt bảng giá, pf 000151–000152) có ở `dev` từ mốc `1425a53c` (13/10/2026).

Kiểm thử được tra bằng `git grep "func Test" -- internal/platform/price` và tên `it(...)` của Vitest `apps/web-ops/src/kn/price/**`. Giả định đã dùng nằm ở [gia-dinh-brd-2026-10.md](../kn/gia-dinh-brd-2026-10.md) (mục W11.71–W11.75) và ADR-0049 mục "Hiện thực".

Yêu cầu lấy từ BRD Báo giá v1.3:
- 31 UR (Mục III, cột "Đợt");
- tiêu chí chấp nhận (AC) của 28 FN (Mục IV);
- **AC chung**: dữ liệu tạo/sửa được lưu và có truy vết.

Có thêm một dòng `BG-KHTC`: nguồn số liệu cho KHTC. Nguồn này chưa có trong BRD Báo giá (Q-084) nhưng KHTC cần ở G1.

Cột **Phạm vi G1**:
- **trong G1**: gói W11.71–W11.76;
- **sau G1**: W11.77 báo cáo giá; W11.78 điều tra giá thành; W11.81 website (Đợt 2 của BRD).

Trạng thái:
- **đạt**: có kiểm thử tự động chứng minh **mọi** AC của dòng; tham số tạm, chờ khách vẫn **đạt** nếu cơ chế đã được chứng minh;
- **một phần**: ghi rõ phần thiếu và loại (**mã**, **chờ khách**, **hạ tầng**);
- **chưa làm**.

## Tóm tắt

| Mục | Số dòng | trong G1 | sau G1 | đạt | một phần | chưa làm |
|---|---|---|---|---|---|---|
| 2. UR | 31 | 20 | 11 | 6 | 14 | 11 |
| 3. FN | 28 | 19 | 9 | 5 | 14 | 9 |
| 4. Nguồn cho KHTC | 1 | 1 | 0 | 0 | 1 | 0 |
| **Tổng** | **60** | **40** | **20** | **11** | **29** | **20** |

Phần **mã** còn thiếu ở các dòng trong G1:
- **Thông báo chuông/email theo từng người nhận.** Áp cho yêu cầu xác minh, phản hồi, kết quả xử lý, và email nhắc nhập giá. Cần sự kiện mới và consumer ở `pf/notify`.
- **"Đang sửa bởi …"** (FN-TI-02.04 AC-02). Hiện dùng khóa lạc quan, trả 409.
- **Dạng Lịch** của Lịch sử báo giá.
- **Kiểm thử thiếu.** Mã đã có nhưng chưa có kiểm thử cho: FN-TI-02.05 AC-01 (còn cảnh báo → nút Duyệt vô hiệu) và FN-XA-03.05 AC-03 (giải trình bắt buộc).

**Rủi ro G1.** Các UR Cao · Đợt 1 sau đây cần màn của **W11.77**, gói xếp ở đợt 2 (12/2026–02/2027): UR-TW-01 (Bảng giá tổng hợp cho TW), UR-TI-08 (Thống kê phiếu), dashboard FN-TI-02.01, UR-TI-17. Khi chạy dữ liệu thật ở G1:
- Cấp Tỉnh theo dõi tiến độ trên màn "Kiểm duyệt & công bố";
- TW chưa có màn xem bảng giá;
- KHTC đọc bảng giá qua view `_pub` của W11.76.

Đề nghị: kéo phần "Bảng giá tổng hợp" (FN-TW-01.01) và "Thống kê phiếu" (FN-TI-02.08) của W11.77 về G1, hoặc PO chấp nhận thiếu hai màn này ở G1.

## 1. Cách chạy

Như [kn-v7.7-acceptance.md §1](kn-v7.7-acceptance.md#1-cách-chạy):

```
go test -count=1 ./internal/platform/price/... ./internal/platform/notify/domain/   # cần PostgreSQL cho *_integration_test
cd apps/web-ops; npx vitest run src/kn/price
```

## 2. UR

| Mã | Yêu cầu (BRD, ưu tiên · đợt) | FN | Gói | Phạm vi G1 | Tiêu chí chấp nhận | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|---|
| BG-UR-CH-01 | Đăng nhập bằng email/SĐT từ đường dẫn nội bộ, Web và Mobile; chọn vai trò, phòng ban (Cao · Đợt 1) | BG-FN-CH-00.01 | P1 (có), W11.06, W11.72 | trong G1 | Như BG-FN-CH-00.01 | `TestFN_CH_00_01_MultiRoleContextsAndSwitch`, `TestW1106_MenuV3DepartmentTemplates` | **một phần** (như BG-FN-CH-00.01) |
| BG-UR-TI-01 | Dashboard xã chưa hoàn thiện báo giá trong ngày; hệ thống tự nhắc cán bộ Xã (Cao · Đợt 1) | BG-FN-TI-02.01 | W11.74, W11.75, W11.77 | trong G1 phần trạng thái và nhắc; màn dashboard sau G1 | Như BG-FN-TI-02.01 | `TestTicketView`, `TestPriceCalendarHTTP_W1174`, `TestPriceTicketsHTTP_W1175` | **một phần** (**mã**: màn dashboard W11.77) |
| BG-UR-TI-02 | Xem chi tiết toàn bộ báo giá, mặc định các xã được phân công, kèm lịch sử hiệu chỉnh, trao đổi xác minh, nhật ký (Cao · Đợt 1) | BG-FN-TI-02.02 | W11.75 | trong G1 | Như BG-FN-TI-02.02 | `TestPriceTicketsHTTP_W1175` | **đạt** |
| BG-UR-TI-03 | Cảnh báo giá bất thường ngay khi Xã gửi; yêu cầu xác minh; chốt xử lý từng cảnh báo (Cao) | BG-FN-TI-02.03 | W11.75 | trong G1 | Như BG-FN-TI-02.03 | `TestEvaluate`, `TestAlertView`, `TestPriceTicketsHTTP_W1175`; Vitest `tickets.test.tsx` | **một phần** (**mã**: thông báo trong 1 phút) |
| BG-UR-TI-04 | Kiểm tra, sửa số liệu (sửa ngày phiếu), xóa phiếu sai, có lịch sử (Cao · Đợt 1) | BG-FN-TI-02.04 | W11.75 | trong G1 | Như BG-FN-TI-02.04 | `TestEffective`, `TestPriceTicketsHTTP_W1175` | **một phần** (**mã**/**chờ khách**: "Đang sửa bởi …") |
| BG-UR-TI-05 | Duyệt bảng giá đầu giờ chiều (Cao · Đợt 1) | BG-FN-TI-02.05 | W11.76 | trong G1 | Như BG-FN-TI-02.05 | `TestPriceBoardsHTTP_W1176`, `TestBoardView_W1176` | **một phần** (**mã**: kiểm thử AC-01 "còn n cảnh báo"; AC-02 phần Bảng giá tổng hợp cần W11.77) |
| BG-UR-TI-06 | Mở duyệt bảng giá đã duyệt để sửa và cập nhật lại (Cao · Đợt 1) | BG-FN-TI-02.06 | W11.76 | trong G1 | Như BG-FN-TI-02.06 | `TestPriceBoardsHTTP_W1176`, `TestReopenWithin_W1176` | **một phần** (**mã**: AC-04 màn TW ở W11.77) |
| BG-UR-TI-07 | Đưa bảng giá đã công bố lên website (TB · Đợt 2) | BG-FN-TI-02.07 | W11.81 | sau G1 | Như BG-FN-TI-02.07 | *(dự kiến)* `TestBG_FN_TI_02_07_WebsiteMatchesApprovedVersion` | **chưa làm** (**chờ khách** A2) |
| BG-UR-TI-08 | Thống kê gửi báo giá theo cán bộ Xã: đúng hạn, nộp muộn, không nộp (Cao · Đợt 1) | BG-FN-TI-02.08 | W11.77 | sau G1 (rủi ro) | Như BG-FN-TI-02.08 | *(dự kiến)* `TestBG_FN_TI_02_08_TicketStatsPerOfficer` | **chưa làm** (**chờ khách** Q-085 khóa tháng) |
| BG-UR-TI-09 | Nhập thay cho xã không gửi kịp, có lý do, nhãn Nộp muộn (Cao · Đợt 1) | BG-FN-TI-02.09 | W11.75 | trong G1 | Như BG-FN-TI-02.09 | `TestPriceTicketsHTTP_W1175` | **một phần** (**mã**: AC-03 cần W11.77) |
| BG-UR-TI-10 | Tạo, quản lý đợt điều tra giá thành theo vụ (Cao · Đợt 1) | BG-FN-TI-02.10 | W11.78 | sau G1 | Như BG-FN-TI-02.10 | *(dự kiến)* `TestBG_FN_TI_02_10_SurveyRoundAssignOfficers` | **chưa làm** (**chờ khách** Q-083, Q-005) |
| BG-UR-TI-11 | Nhập dữ liệu điều tra từ file, kể cả Google Form, chuẩn hóa về danh mục (Cao · Đợt 1) | BG-FN-TI-02.11 | W11.78 | sau G1 | Như BG-FN-TI-02.11 | *(dự kiến)* `TestBG_FN_TI_02_11_ImportNormalizesItems` | **chưa làm** |
| BG-UR-TI-12 | Tự phát hiện phiếu điều tra bất thường, xác minh, duyệt/loại, chốt đợt (Cao · Đợt 1) | BG-FN-TI-02.12 | W11.78 | sau G1 | Như BG-FN-TI-02.12 | *(dự kiến)* `TestBG_FN_TI_02_12_MedianOutliersAndClose` | **chưa làm** |
| BG-UR-TI-13 | QTNV quản lý mặt hàng theo thời kỳ, nhóm, thứ tự, gán xã, xử lý đề xuất (Cao · Đợt 1) | BG-FN-AD-04.03 | W11.73 | trong G1 | Như BG-FN-AD-04.03 | `TestPriceItemsHTTP_W1173`, `TestValidateItem_W1173`, `TestItemStates_W1173`, `TestPointItems_W1173`; Vitest `items.test.tsx` | **đạt** (nạp danh mục từ Excel là luồng phụ, chưa làm — chờ `cap/import`) |
| BG-UR-TI-14 | QTNV cấu hình lịch báo giá, ngoại lệ, giờ khóa 12:00, hạn phản hồi (Cao · Đợt 1) | BG-FN-AD-04.04 | W11.74 | trong G1 | Như BG-FN-AD-04.04 | `TestPlanMonFriLock_W1174`, `TestExceptions_W1174`, `TestValidateSchedule_W1174`, `TestScheduleVersions_W1174`, `TestPriceCalendarHTTP_W1174`; Vitest `price.test.ts` | **đạt** (lịch T2–T6 tạm, chờ khách Q-086) |
| BG-UR-TI-15 | QTNV cấu hình lịch và nội dung nhắc nhập giá (TB · Đợt 1) | BG-FN-AD-04.05 | W11.74 | trong G1 | Như BG-FN-AD-04.05 | `TestValidateSchedule_W1174`, `TestPriceCalendarHTTP_W1174`, `TestPriceReminderNotifications_W1174`; Vitest `price.test.ts` | **đạt** |
| BG-UR-TI-16 | QTNV cấu hình ngưỡng biến động, đi ngang, số ngày tìm giá tham chiếu (Cao · Đợt 1) | BG-FN-AD-04.06 | W11.74, W11.75 | trong G1 | Như BG-FN-AD-04.06 | `TestThresholds_W1174`, `TestEvaluate`, `TestPriceCalendarHTTP_W1174`; Vitest `price.test.ts` | **đạt** (ngưỡng 30 %, N = 7, 1 % tạm) |
| BG-UR-TI-17 | Xem, xuất bảng giá tổng hợp, biến động, dashboard vùng, báo cáo giá thành (Cao · Đợt 1) | BG-FN-TW-01.01…01.04 | W11.77, W11.78 | sau G1 (rủi ro) | Như BG-FN-TW-01.01…01.04 | nền: `TestAggregateBoard_W1176` (CT-04) | **chưa làm** |
| BG-UR-XA-01 | Cập nhật đủ mọi mặt hàng (giá hoặc "Không có hàng") trước giờ khóa; lấy giá kỳ trước, lưu nháp, gửi, ghi chú khi vượt ngưỡng; máy tính hoặc điện thoại (Cao · Đợt 1) | BG-FN-XA-03.01 | W11.75 | trong G1 | Như BG-FN-XA-03.01; điện thoại = web responsive (giả định W11.75) | `TestPriceTicketsHTTP_W1175`, `TestValidateLine`, `TestEvaluate`; Vitest `tickets.test.tsx` | **một phần** (như BG-FN-XA-03.01) |
| BG-UR-XA-02 | In phiếu báo giá theo mẫu, lẻ hoặc hàng loạt (Cao · Đợt 1) | BG-FN-XA-03.02 | W11.75 | trong G1 | Như BG-FN-XA-03.02 | Vitest `tickets.test.tsx` ("in phiếu…"); `TestPriceTicketsHTTP_W1175` | **một phần** (**chờ khách**: File 2 gốc, Q-085) |
| BG-UR-XA-03 | Xem lịch sử báo giá dạng lịch hoặc danh sách (TB · Đợt 1) | BG-FN-XA-03.03 | W11.75 | trong G1 | Như BG-FN-XA-03.03 | `TestPriceTicketsHTTP_W1175` | **một phần** (**mã**: dạng Lịch) |
| BG-UR-XA-04 | Nhận nhắc nhập giá buổi sáng (Cao · Đợt 1) | BG-FN-XA-03.04 | W11.74, W11.72 | trong G1 | Như BG-FN-XA-03.04 | `TestPriceCalendarHTTP_W1174`, `TestPriceReminderNotifications_W1174`, `TestPriceTicketsHTTP_W1175` | **một phần** (**mã**: email theo người nhận) |
| BG-UR-XA-05 | Nhận và phản hồi yêu cầu xác minh, kể cả sau giờ khóa trong hạn (Cao · Đợt 1) | BG-FN-XA-03.05 | W11.75 | trong G1 | Như BG-FN-XA-03.05 | `TestAlertView`, `TestPriceTicketsHTTP_W1175`; Vitest `tickets.test.tsx` | **một phần** (**mã**: kiểm thử AC-03; thông báo) |
| BG-UR-XA-06 | Đề xuất mặt hàng mới phát sinh tại địa phương (TB · Đợt 1) | BG-FN-XA-03.01, BG-FN-AD-04.03 | W11.73 | trong G1 | AC-05 FN-AD-04.03: đề xuất được duyệt → mặt hàng gán cho xã đề xuất; Cấp Xã chỉ thấy đề xuất của mình | `TestPriceItemsHTTP_W1173`; Vitest `items.test.tsx` ("Cấp Xã đề xuất…") | **đạt** (thông báo kết quả đề xuất chưa gửi — ngoài AC) |
| BG-UR-XA-07 | Xem bảng giá tổng hợp và báo cáo biến động đã công bố (TB · Đợt 1) | BG-FN-TW-01.01, 01.02 | W11.77 | sau G1 | Như BG-FN-TW-01.01, 01.02 (Cấp Xã chỉ xem) | *(dự kiến)* `TestBG_FN_TW_01_01_SummaryBoard` | **chưa làm** |
| BG-UR-TW-01 | Cấp TW xem bảng giá tổng hợp đã công bố theo mặt hàng, xã, nhóm vùng, kèm tăng/giảm (Cao · Đợt 1) | BG-FN-TW-01.01 | W11.77 | sau G1 (rủi ro) | Như BG-FN-TW-01.01 | nền: `TestAggregateBoard_W1176` | **chưa làm** (**chờ khách** Q-055, Q-027) |
| BG-UR-TW-02 | Cấp TW xem báo cáo tăng giảm, biến động (TB · Đợt 2) | BG-FN-TW-01.02 | W11.77 | sau G1 | Như BG-FN-TW-01.02 | *(dự kiến)* `TestBG_FN_TW_01_02_TrendFlatThreshold` | **chưa làm** |
| BG-UR-TW-03 | Cấp TW dashboard giá theo vùng (TB · Đợt 2) | BG-FN-TW-01.03 | W11.77 | sau G1 | Như BG-FN-TW-01.03 | *(dự kiến)* `TestBG_FN_TW_01_03_RegionDashboard` | **chưa làm** |
| BG-UR-TW-04 | Cấp TW xem báo cáo giá thành theo vụ, mặt hàng, vùng (TB · Đợt 2) | BG-FN-TW-01.04 | W11.78 | sau G1 | Như BG-FN-TW-01.04 | *(dự kiến)* `TestBG_FN_TW_01_04_CostMedianNoPII` | **chưa làm** (**chờ khách** Q-083) |
| BG-UR-AD-01 | Admin tạo tài khoản, gán vai trò, gán cán bộ Xã vào xã báo giá, phân công xã cho Cấp Tỉnh, gán QTNV (Cao · Đợt 1) | BG-FN-AD-04.01 | P1 (có), W11.07, W11.72 | trong G1 | Như BG-FN-AD-04.01 | `TestPricePointsHTTP_W1172`, `TestOfficerAt` | **một phần** (như BG-FN-AD-04.01) |
| BG-UR-AD-02 | Admin quản lý danh mục xã báo giá, liên kết mã ĐVHC (Cao · Đợt 1) | BG-FN-AD-04.02 | W11.72 | trong G1 | Như BG-FN-AD-04.02 | `TestPricePointsHTTP_W1172`, `TestPointStates`, `TestAdminUnitClash`, `TestValidatePoint`; Vitest `points.test.tsx` | **một phần** (như BG-FN-AD-04.02) |

## 3. FN (tiêu chí chấp nhận)

| Mã | Chức năng | Gói | Phạm vi G1 | Tiêu chí chấp nhận (BRD) | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|
| BG-FN-CH-00.01 | Đăng nhập, chọn vai trò và phòng ban | P1, W11.06, W11.72 | trong G1 | AC-01: chỉ có vai Cấp Xã – Phòng TTTT → vào thẳng Nhập báo giá. AC-02: Cấp Tỉnh + Cấp Xã → màn chọn; đổi vai đổi menu. AC-03: Admin vào Tổng quan. AC-04: sai mật khẩu lần 5 → khóa 15 phút. | `TestFN_CH_00_01_MultiRoleContextsAndSwitch` (AC-02), `TestW1106_MenuV3DepartmentTemplates`; *(dự kiến)* `TestBG_FN_CH_00_01_XaDefaultRouteEntry` | **một phần** (**mã**: AC-01 trang mặc định chưa có kiểm thử; AC-04 theo chính sách Keycloak A3 của KN — **chờ khách**) |
| BG-FN-TW-01.01 | Bảng giá tổng hợp chung | W11.77 (CT-04 ở W11.76) | sau G1 (rủi ro) | AC-01: ba xã Dưa leo 8.000–10.000, 9.000–11.000, 12.000–14.000 → Thấp nhất 8.000, Cao nhất 14.000, Trung bình 10.667, 3 xã. AC-02: trùng bảng giá đã duyệt mới nhất. AC-03: ngày đang mở duyệt hiện bản cũ, nhãn "Đang cập nhật". | nền: `TestAggregateBoard_W1176` (min/max toàn tỉnh, trung bình P); *(dự kiến)* `TestBG_FN_TW_01_01_SummaryBoard` | **chưa làm** (**mã** W11.77) |
| BG-FN-TW-01.02 | Báo cáo tăng giảm, biến động | W11.77 | sau G1 | AC-01: trùng bảng giá đã công bố. AC-02: đi ngang 1 %, 10.000 → 10.080 "Đi ngang", → 10.100 "Tăng". AC-03: TW, Xã không có nút sửa/xuất; Tỉnh xuất Excel/PDF đúng bộ lọc. | *(dự kiến)* `TestBG_FN_TW_01_02_TrendFlatThreshold` | **chưa làm** |
| BG-FN-TW-01.03 | Dashboard giá theo vùng | W11.77 | sau G1 | AC-01: trùng FN-TW-01.01 cùng ngày, cùng nhóm vùng. AC-02: bấm An Giang → đúng các xã của nhóm vùng có giá trong ngày. | *(dự kiến)* `TestBG_FN_TW_01_03_RegionDashboard` | **chưa làm** |
| BG-FN-TW-01.04 | Báo cáo giá thành theo vụ | W11.78 | sau G1 | AC-01: 5.274, 5.580, 5.384 → bình quân 5.413, trung vị 5.384. AC-02: phiếu Loại, Đã xóa không tính. AC-03: TW không thấy họ tên, SĐT nông hộ. | *(dự kiến)* `TestBG_FN_TW_01_04_CostMedianNoPII` | **chưa làm** |
| BG-FN-TI-02.01 | Dashboard xã chưa hoàn thiện báo giá | W11.74, W11.75, W11.77 | trong G1 phần trạng thái + nhắc; màn sau G1 | AC-01: trạng thái xã cập nhật ngay khi lưu nháp/gửi. AC-02: thẻ và biểu đồ khớp bảng. AC-03: cột "Hệ thống đã nhắc" đúng các lần nhắc trong ngày. AC-04: Mỹ Chánh Bản nháp lúc 11:59 → ngay sau 12:00 "Không nộp". | `TestTicketView` (AC-04, trạng thái chiếu theo `DayPlan.Locked`), `TestPriceCalendarHTTP_W1174` (nhật ký nhắc), `TestPriceTicketsHTTP_W1175`; *(dự kiến)* `TestBG_FN_TI_02_01_ProgressDashboard` | **một phần** (**mã**: màn dashboard, AC-02, AC-03) |
| BG-FN-TI-02.02 | Xem chi tiết toàn bộ báo giá | W11.75 | trong G1 | AC-01: mặc định các xã được phân công; đổi bộ lọc xem mọi xã. AC-02: đủ dòng giá theo bộ lọc. AC-03: dòng đã hiệu chỉnh hiện cũ → mới, người sửa, lý do, trao đổi xác minh. | `TestPriceTicketsHTTP_W1175` (lọc `mine=true`, nhật ký, lịch sử, cảnh báo + phản hồi) | **đạt** |
| BG-FN-TI-02.03 | Cảnh báo biến động bất thường, yêu cầu xác minh | W11.74, W11.75 | trong G1 | AC-01: ngưỡng 30 %, tham chiếu 9.000, gửi 13.000–15.000 (+55,6 %) → trong 1 phút cán bộ Tỉnh nhận thông báo, dòng "Tạm tính". AC-02: tham chiếu 10.000, 13.000 (+30,0 %) có cảnh báo, 12.990 (+29,9 %) không. AC-03: sửa về 9.500–10.500 gửi lại trước 12:00 → cảnh báo tự hủy. | `TestEvaluate`, `TestThresholds_W1174` (AC-02), `TestPriceTicketsHTTP_W1175` (Tạm tính +55,56 %, tự hủy); Vitest `tickets.test.tsx` | **một phần** (**mã**: thông báo chuông/email trong 1 phút — cần `pf/notify`) |
| BG-FN-TI-02.04 | Kiểm tra, sửa số liệu, xóa phiếu | W11.75 | trong G1 | AC-01: hiệu chỉnh/xóa không lý do → lỗi. AC-02: phiếu đang được người khác sửa → chỉ xem, thấy tên người sửa. AC-03: mọi hiệu chỉnh, xóa có nhật ký. AC-04: sửa ngày sang ngày xã đã có phiếu → chặn. AC-05: hiệu chỉnh Heo hơi 38.000 → 56.000 → bảng giá dùng 56.000. | `TestEffective` (AC-05), `TestPriceTicketsHTTP_W1175` (AC-01, AC-03, AC-04, AC-05) | **một phần** (AC-02: khóa lạc quan 409 `price.ticket_stale` thay "Đang sửa bởi …" — **chờ khách** chấp nhận lệch hoặc **mã** khóa thuê) |
| BG-FN-TI-02.05 | Duyệt bảng giá | W11.76 | trong G1 | AC-01: còn 4 cảnh báo → nút Duyệt vô hiệu, hiện "còn 4 cảnh báo". AC-02: duyệt → "Đã duyệt", hiện ngay ở Bảng giá tổng hợp. AC-03: người khác thấy "Đang duyệt bởi …", không duyệt trùng. AC-04: Admin không thấy nút Duyệt. | `TestPriceBoardsHTTP_W1176` (AC-03, AC-04, phiên bản 1, CT-04), `TestBoardView_W1176`; Vitest `tickets.test.tsx` ("W11.76 bảng giá ngày…") | **một phần** (**mã**: kiểm thử AC-01 "còn n cảnh báo"; AC-02 phần Bảng giá tổng hợp cần W11.77) |
| BG-FN-TI-02.06 | Mở duyệt để sửa và cập nhật lại | W11.76 | trong G1 | AC-01: mở duyệt không lý do → không xác nhận được. AC-02: sau mở duyệt Tỉnh sửa được, Xã không. AC-03: duyệt lại → phiên bản mới, nhật ký có cả mở duyệt và duyệt lại. AC-04: trong khi mở duyệt, TW thấy bản cũ "Đang cập nhật". AC-05: giới hạn 30 ngày. | `TestPriceBoardsHTTP_W1176` (AC-01, AC-02, AC-03 phiên bản 2, AC-05), `TestReopenWithin_W1176` | **một phần** (**mã**: AC-04 màn TW ở W11.77) |
| BG-FN-TI-02.07 | Đăng bảng giá lên website | W11.81 | sau G1 | AC-01: website trùng bảng giá đã duyệt (cùng phiên bản). AC-02: lỗi → "Lỗi đăng website" + "Đăng lại". | *(dự kiến)* `TestBG_FN_TI_02_07_WebsiteMatchesApprovedVersion` | **chưa làm** |
| BG-FN-TI-02.08 | Thống kê phiếu báo giá | W11.77 | sau G1 (rủi ro) | AC-01: 22 ngày, đúng hạn 19, muộn 1, không nộp 2 → Phải nộp 22, Đúng hạn 19, Nộp muộn 1, Không nộp 2. AC-02: khớp trạng thái phiếu ở Chi tiết. | nền: `TestTicketView`, `TestOfficerAt` (phiếu thống kê cho cán bộ phụ trách tại ngày); *(dự kiến)* `TestBG_FN_TI_02_08_TicketStatsPerOfficer` | **chưa làm** |
| BG-FN-TI-02.09 | Nhập thay báo giá nộp muộn | W11.75 | trong G1 | AC-01: Hà Tiên "Không nộp", bảng chưa duyệt, nhập thay đủ 38 mặt hàng + lý do → "Nộp muộn", hiện ở Tiến độ và Chi tiết. AC-02: không lý do → không lưu. AC-03: hiện ở cột Nộp muộn của Thống kê. AC-04: xã đã có phiếu Đã khóa → không có "Nhập thay". | `TestPriceTicketsHTTP_W1175` (lý do bắt buộc, Nộp muộn, nhập thay trước giờ khóa → 409) | **một phần** (**mã**: AC-03 cần W11.77; AC-01 phần "Tiến độ" cần màn dashboard) |
| BG-FN-TI-02.10 | Quản lý đợt điều tra giá thành | W11.78 | sau G1 | AC-01: tên cán bộ điều tra trong tệp khớp → gán đúng cán bộ, xã. AC-02: hết ngày kết thúc → "Đang rà soát". AC-03: tiến độ = phiếu đã nhập / chỉ tiêu. | *(dự kiến)* `TestBG_FN_TI_02_10_SurveyRoundAssignOfficers` | **chưa làm** |
| BG-FN-TI-02.11 | Nhập dữ liệu điều tra từ file | W11.78 | sau G1 | AC-01: "Dưa leo", "dưa leo", "DƯA LEO" → một mặt hàng. AC-02: diện tích 0 → lỗi dòng đó. AC-03: trùng SĐT + mặt hàng trong đợt → bỏ dòng sau, có cảnh báo. | *(dự kiến)* `TestBG_FN_TI_02_11_ImportNormalizesItems` | **chưa làm** |
| BG-FN-TI-02.12 | Rà soát phiếu điều tra, chốt đợt | W11.78 | sau G1 | AC-01: 41 phiếu Xoài keo trung vị 12.000, ngưỡng 40 %, 20.000 (+66,7 %) → "Lệch trung vị". AC-02: Mít Thái 25.000 khi trung vị 10.000 → "Cao bất thường". AC-03: còn phiếu "Cần xác minh" → "Chốt đợt" vô hiệu. | *(dự kiến)* `TestBG_FN_TI_02_12_MedianOutliersAndClose` | **chưa làm** |
| BG-FN-XA-03.01 | Nhập/sửa báo giá theo ngày, trước giờ khóa | W11.75 | trong G1 | AC-01: 38 mặt hàng, nhập 36, "Không có hàng" 1, trống 1 → "Gửi" báo thiếu 1; lưu nháp được. AC-02: nhập nốt, gửi trước 12:00 → "Đã gửi", hiện ngay ở FN-TI-02.01. AC-03: kỳ trước 9.000, nhập 13.000–15.000 → tô đỏ "▲ Cao bất thường +55,6 %"; chưa ghi chú thì không gửi được. | `TestPriceTicketsHTTP_W1175` (AC-01, khóa 12:00), `TestValidateLine`, `TestEvaluate` (AC-03); Vitest `tickets.test.tsx` ("AC-03 FN-XA-03.01…", "Xã lưu nháp, gửi đủ mặt hàng…") | **một phần** (**mã**: AC-02 phần FN-TI-02.01 cần màn dashboard) |
| BG-FN-XA-03.02 | In phiếu báo giá | W11.75 | trong G1 | AC-01: đúng mẫu File 2: xã, cán bộ, ngày, mặt hàng theo nhóm, giá, ô ký. AC-02: Heo hơi gửi 38.000, hiệu chỉnh 56.000 → in 38.000 kèm "Tỉnh hiệu chỉnh: 56.000". AC-03: 5 ngày in hàng loạt → 1 tệp PDF 5 phiếu. | Vitest `tickets.test.tsx` ("in phiếu: giá Xã gửi kèm chú thích hiệu chỉnh…" — AC-02); `TestPriceTicketsHTTP_W1175` (in/xuất; PDF qua `UseTablePDF` nối ở gốc lắp ráp); *(dự kiến)* `TestBG_FN_XA_03_02_BatchPDF` | **một phần** (**chờ khách**: File 2 gốc, Q-085; **mã**: kiểm thử AC-03 với bộ PDF thật) |
| BG-FN-XA-03.03 | Lịch sử báo giá của xã | W11.75 | trong G1 | AC-01: đủ ngày báo giá trong khoảng, đúng trạng thái. AC-02: chọn nhiều phiếu, "In phiếu" → một tệp. AC-03: không xem được phiếu xã khác. | `TestPriceTicketsHTTP_W1175` (lịch sử, nhật ký; Cấp Xã xã khác → 403 `price.point_not_assigned`) | **một phần** (**mã**: dạng Lịch; kiểm thử AC-02 gộp tệp) |
| BG-FN-XA-03.04 | Nhắc nhập giá buổi sáng | W11.74, W11.72 | trong G1 | AC-01: lịch nhắc 07:30 và 10:30; lúc 07:30 cán bộ Xã mọi xã có ngày báo giá nhận thông báo trong ứng dụng **và email**. AC-02: lúc 10:30 chỉ xã Chưa nhập / Bản nháp nhận lần 2. AC-03: bấm thông báo mở đúng màn Nhập báo giá ngày đó. | `TestPriceCalendarHTTP_W1174` (nhắc một lần mỗi ngày qua outbox), `TestPriceReminderNotifications_W1174` (trong ứng dụng), `TestPriceTicketsHTTP_W1175` (lần 2 bỏ xã đã gửi — AC-02) | **một phần** (**mã**: email theo người nhận ở `pf/notify`; AC-03 liên kết) |
| BG-FN-XA-03.05 | Phản hồi yêu cầu xác minh | W11.75 | trong G1 | AC-01: yêu cầu Dưa leo 02/10 hạn 13:30; 12:15 "Đề xuất giá đúng" 9.000–10.000 + giải trình → "Xã đã phản hồi"; Tỉnh thấy giá đề xuất; giá trên phiếu không đổi. AC-02: 13:31 không gửi được. AC-03: không giải trình → không gửi được. | `TestAlertView` (AC-02), `TestPriceTicketsHTTP_W1175` (AC-01, AC-02); Vitest `tickets.test.tsx` | **một phần** (**mã**: kiểm thử AC-03 — mã đã chặn ở `app/ticket_review.go` `TextRequired(in.Explanation…)`) |
| BG-FN-AD-04.01 | Tài khoản và phân quyền | P1, W11.07, W11.72 | trong G1 | AC-01: tài khoản mới đăng nhập, đúng menu, xã. AC-02: Mỹ Chánh có cán bộ A, gán B từ 05/10 → từ 05/10 B nhập; phiếu trước 05/10 vẫn thống kê cho A. AC-03: bật QTNV → thấy nhóm menu "Cấu hình". AC-04: trùng email → không lưu. | `TestOfficerAt`, `TestPricePointsHTTP_W1172` (AC-02); `TestW1106_MenuV3DepartmentTemplates` | **một phần** (phân công ở màn "Xã báo giá", không ở màn Tài khoản như BRD — lệch vị trí, **chờ khách**; AC-03, AC-04 chưa có kiểm thử riêng cho Báo giá) |
| BG-FN-AD-04.02 | Danh mục xã báo giá | W11.72 | trong G1 | AC-01: xã mới xuất hiện ở Tiến độ nhập giá từ ngày báo giá kế tiếp. AC-02: không chọn được ĐVHC đã gắn với xã báo giá khác đang hoạt động. | `TestPointStates`, `TestAdminUnitClash` (AC-02), `TestPricePointsHTTP_W1172` (ĐVHC trùng → 409); Vitest `points.test.tsx` | **một phần** (**mã**: AC-01 phần màn Tiến độ W11.77) |
| BG-FN-AD-04.03 | Danh mục mặt hàng, nhóm hàng theo thời kỳ | W11.73 | trong G1 | AC-01: Dưa leo đã có giá, đổi ĐVT → chặn. AC-02: ngưng Lúa Jasmine 85 đến 30/09 → từ 01/10 không còn ở Nhập báo giá; báo cáo tháng 9 vẫn có. AC-03: thêm Cơ bản từ 05/10 → mọi xã. AC-04: danh sách đúng xã, ngày, thứ tự. AC-05: duyệt đề xuất. AC-06: định danh mặt hàng. | `TestPriceItemsHTTP_W1173` (AC-01…06), `TestValidateItem_W1173`, `TestItemStates_W1173`, `TestPointItems_W1173`; Vitest `items.test.tsx` | **đạt** |
| BG-FN-AD-04.04 | Cấu hình kỳ báo giá, giờ khóa, hạn phản hồi | W11.74 | trong G1 | AC-01: lịch T2–T6, khóa 12:00, mọi màn Nhập chỉ xem (lệch ≤ 1 phút). AC-02: T7, CN không có ngày báo giá, không nhắc. AC-03: đổi giờ khóa áp dụng từ ngày áp dụng. AC-04: ngày nghỉ 02/09/2026 không có kỳ, không nhắc, không tính phải nộp. AC-05: hạn phản hồi 11:30 khi khóa 12:00 → không lưu. | `TestPlanMonFriLock_W1174` (AC-01, AC-02), `TestScheduleVersions_W1174` (AC-03), `TestExceptions_W1174` (AC-04), `TestValidateSchedule_W1174` (AC-05), `TestPriceCalendarHTTP_W1174` (job khóa ≤ 60 s); Vitest `price.test.ts` | **đạt** (lịch tạm, chờ khách Q-086) |
| BG-FN-AD-04.05 | Cấu hình nhắc nhập giá | W11.74 | trong G1 | AC-01: giờ nhắc lần 2 không trước giờ khóa → không lưu. AC-02: "Gửi thử" gửi tới tài khoản đang đăng nhập. AC-03: thay đổi áp dụng từ ngày báo giá tiếp theo. | `TestValidateSchedule_W1174` (AC-01), `TestPriceCalendarHTTP_W1174` (gửi thử `reminder-test`, AC-03), `TestPriceReminderNotifications_W1174` (gửi thử khi kênh tắt); Vitest `price.test.ts` | **đạt** |
| BG-FN-AD-04.06 | Ngưỡng cảnh báo giá bất thường | W11.74, W11.75 | trong G1 | AC-01: mặc định 30 %, nhóm Rau màu 40 %: Rau màu +35 % không đánh dấu; Heo hơi −34,5 % "Thấp bất thường". AC-02: +30,0 % Cao, +29,9 % không. AC-03: đổi ngưỡng 10:00 không đổi cảnh báo đã sinh trước. AC-04: không xóa được ngưỡng mặc định. | `TestThresholds_W1174` (AC-01, AC-02), `TestPriceCalendarHTTP_W1174` (AC-04), `TestPriceTicketsHTTP_W1175` (ngưỡng chụp lúc gửi — AC-03); Vitest `price.test.ts` | **đạt** |

## 4. Nguồn cho KHTC

| Mã | Yêu cầu | Gói | Phạm vi G1 | Tiêu chí chấp nhận | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|
| BG-KHTC | Bảng giá đã duyệt theo ngày (min/max toàn tỉnh) và số phiếu theo tuần báo cáo cho KHTC Mẫu 1 A.III, đọc qua view `_pub`, không nhập lại (KHTC v2.2 §3.4, Q-084) | W11.10 (hợp đồng), W11.76 (view) | trong G1 | Theo ca golden G02B ([khtc-week-mapping.md](khtc-week-mapping.md)): ngày chưa duyệt không có dòng; hai cột Mẫu 1 là hai ngày đã duyệt gần nhất không sau ngày chốt; số phiếu tuần chỉ đếm bảng đã duyệt; tuần "đã chốt" khi mọi ngày báo giá theo lịch đã duyệt. Khóa tuần theo lịch kỳ W11.03 (`TestDefaultWeekOf_W1176`: 04/11/2026 → 2026-W45). | `TestPriceBoardsHTTP_W1176` (view `_pub` đúng số), `TestDefaultWeekOf_W1176`; `TestKHTCQA_GoldenDatasetConsistent` (G02B) | **một phần** (**mã**: bên đọc KHTC W11.64; view đã có) |
