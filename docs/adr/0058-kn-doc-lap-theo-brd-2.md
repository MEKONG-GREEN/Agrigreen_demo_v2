# ADR-0058: Khuyến nông chạy độc lập theo BRD 2.0

**Trạng thái:** Chấp nhận — quyết định của người dùng ngày 07/11/2026; gói #261 KN-STANDALONE-0-GOV.
**Ngày:** 07/11/2026.
**Bổ sung:** [ADR-0055](0055-tach-he-thong-khuyen-nong.md). Các quyết định cũ về chủ sở hữu dữ liệu, tenant, quyền và sổ chỉ ghi thêm vẫn áp dụng; phạm vi kết nối và triển khai dưới đây được ưu tiên cho đợt KN độc lập.
**Liên quan:** [ADR-0056](0056-ho-chan-nuoi-cnts-v4.md) (hộ chăn nuôi), [ADR-0057](0057-tu-dat-lai-mat-khau-qua-email.md) (xác thực).

## Bối cảnh

BRD 2.0 đã chốt gồm Trồng trọt v7.7, CNTS v4.0, HCTH v4.0, Báo giá v2.0 và KHTC v2.3. Bề mặt KN hiện còn mục ngoài BRD và luồng phụ thuộc dữ liệu của hệ khác. Người dùng đã duyệt kế hoạch KN độc lập ngày 07/11/2026, gồm D1–D3 và mặc định Q2–Q10. ADR này ghi lại quyết định đã duyệt; việc có ADR không chứng minh các gói triển khai hoặc nghiệm thu đã hoàn thành.

## Quyết định

### 1. Nguồn đặc tả và bề mặt

Chỉ lập trình theo Function chi tiết và cấu trúc dữ liệu của năm BRD 2.0. Khi hai chỗ lệch nhau, bảng trường trong Function đang làm thắng bảng tổng hợp/phụ lục; ghi lựa chọn trong tài liệu gói. Không dùng BRD cũ, prototype hoặc tài liệu đối chiếu cũ làm căn cứ nghiệp vụ.

Mỗi khóa menu, route web KN (kể cả route ngoài `/kn/**` mà KN còn dùng), nhóm API KN và màn field-app KN phải có dòng trong `docs/kn/brd-surface.yaml`: FN của BRD 2.0, hoặc `HẠ_TẦNG` kèm FN cần nó, `GỠ` kèm gói, `HOÃN`. #262 lập bảng và kiểm ở chế độ báo cáo; #272 mới bật chặn CI. Bề mặt không thuộc BRD phải được gỡ cả menu, route, API/quyền và màn mobile tương ứng, với test âm 404/403.

### 2. Hạ tầng KN và hệ ngoài

| Loại | Thành phần |
|---|---|
| Module nền KN được dùng trong cùng tiến trình | vault, forms, contact, price, ledger, notify, masterdata, party, accounts, reports, sync, evidence, export, geo, workflow, metrics |
| Hạ tầng vận hành KN | Keycloak, outbox mail, PostgreSQL, hàng đợi, kho tệp |
| Hệ nghiệp vụ/nguồn ngoài chưa kết nối | HTX (`pf/cooperative`, `cropcycle`, `mrv`), CGH (`pf/mechanization`), MKG nội bộ, Public API (`pf/apiclient`, integration-gateway), website, Google Sheet và nguồn số liệu ngoài |

Hạ tầng phục vụ KN không tự trở thành một chức năng trên menu. Profile chỉ-KN có module KN và hạ tầng cần thiết; tắt module hệ ngoài. Tiêu chí nghiệm thu là tenant KN mới thực hiện các FN chưa hoãn khi không có kết nối hệ ngoài; vẫn giữ CI toàn stack để kiểm hồi quy các ứng dụng khác.

### 3. D1–D3

| Mã | Quyết định đã chốt |
|---|---|
| D1 | Dữ liệu KN do KN tự nhập, tự quản. Thửa ruộng và số máy của Trồng trọt FN-XA-03.04/03.05 chuyển về kho KN `pf_extension` trong #271; chọn HTX từ hồ sơ KN. Chuyển dữ liệu phải có đếm trước/sau, đối chiếu và diễn tập quay lui. |
| D2 | Chưa kết nối hệ khác. Hoãn Báo giá FN-TI-02.07 đăng bảng giá lên website, phần Google Sheet của Trồng trọt FN-TI-02.06 và cutover danh tính KNSPLIT-4B #87–#89 sang pha kết nối. Email và liên kết tải của Trồng trọt giữ theo BRD. Các gói kết nối #40/#238 tiếp tục hoãn. |
| D3 | Không đưa tính năng ngoài BRD 2.0 vào app KN. Gỡ bề mặt và quyền ghi, giữ dữ liệu cũ chỉ đọc; không DROP, không sửa migration đã áp dụng, không làm mất chuỗi băm của bản đóng băng. Mã còn phục vụ HTX/MKG giữ đúng chủ hệ đó. |

KN không ghi dữ liệu HTX, kể cả qua port, công bố dữ liệu hay lệnh tích hợp. Việc chuyển dữ liệu có thể tạo bản KN và bằng chứng chuyển nhưng không cấp quyền sửa bảng nguồn HTX/CGH, không xóa sổ nguồn hoặc thay lịch sử.

### 4. Mặc định Q2–Q10 đã được duyệt

| Mã | Áp dụng |
|---|---|
| Q2 | accounts, reports, sync, evidence, export, geo, workflow, metrics là hạ tầng KN khi phục vụ các FN của KN. |
| Q3 | Tên hiển thị theo BRD của module đang xét; tên phòng dùng chung theo BRD Báo giá. |
| Q4 | Gỡ “Quy trình, văn bản” khỏi phòng ngoài CNTS và “Báo cáo đột xuất” Trồng trọt; FN-20 CNTS chỉ áp dụng cho CNTS. |
| Q5 | Không làm màn tham số KN chung. Hạn sửa nhật ký CNTS, ranh giới tuần KSGM và quy tắc kỳ lấy đúng BRD trong cấu hình/seed có phiên bản. Chỉ giữ màn cấu hình BRD có, gồm Báo giá FN-AD-04.04–04.06. |
| Q6 | Website Báo giá và Google Sheet được hoãn theo D2. Nút website trong #268 ghi trạng thái “chưa kết nối” có log, không báo đã đăng thành công. |
| Q7 | API `pf/extension` mà App HTX còn dùng chuyển về chủ HTX; gỡ khỏi bề mặt KN, không xóa mã hệ khác. |
| Q8 | Profile chỉ-KN dùng realm `kn`, chỉ có client `kn-web` và `kn-field`. Bản triển khai chung giữ realm `agrigreen` tới pha kết nối; không thực hiện cutover #87–#89 trong đợt này. |
| Q9 | CNTS FN-10 HCN-09 hiển thị họ tên cũ → mới, chỉ che số điện thoại theo Function của BRD; vẫn đọc qua vault, đúng quyền và ghi log. Đây là phần điều chỉnh quy tắc hiển thị của ADR-0056, không chuyển PII ra khỏi kho định danh. |
| Q10 | Báo giá FN-CH-00.01 “Liên hệ quản trị viên” tạo yêu cầu gửi Admin KN và lưu trong KN (#274). |

### 5. Trình tự và kiểm chứng

Đợt 0 (#261, #262) lập quyết định và bảng ánh xạ. Sau đó chạy các gói gỡ bề mặt và lấp FN theo phụ thuộc; #271 chuyển thửa/máy sau #261. #272 dựng profile KN và bật cổng bề mặt khi các gói gỡ/thay nguồn đã gộp; #273 nghiệm thu năm phòng trên profile đó.

Mỗi gói tự kiểm, CI đúng SHA, tự review và gộp tuần tự qua helper ticket vào `dev`. Gói gỡ kiểm đủ lớp và parity menu; gói chuyển dữ liệu kiểm số dòng, tổng diện tích/số máy và quay lui. Không tăng baseline để làm xanh. Không coi ADR, bảng báo cáo, cổng khớp baseline hoặc các XFAIL lịch sử là bằng chứng hoàn tất KN độc lập.

## Phương án không chọn

- Giữ kết nối HTX/CGH/MKG hoặc nguồn ngoài làm điều kiện chạy KN: trái D1/D2 và không kiểm được KN độc lập trên tenant mới.
- Chỉ ẩn menu nhưng giữ route/API/quyền cũ: không đáp ứng D3 và vẫn cho phép gọi trực tiếp.
- Xóa bảng hoặc sửa sổ/migration lịch sử khi bỏ chức năng: làm mất bằng chứng và trái ranh giới dữ liệu đã chốt.

## Hệ quả

KN có nguồn dữ liệu và phạm vi bề mặt đo được theo BRD. Các gói cần chuyển thửa/máy và bỏ phụ thuộc cũ trước khi chứng nhận độc lập; website, Google Sheet và cutover danh tính tiếp tục chờ pha kết nối. Hạ tầng dùng chung vẫn cần hoạt động, và CI toàn stack vẫn phải bảo vệ hệ khác.

Việc gỡ route cần kiểm người gọi để không làm gãy App HTX/MKG. Việc chuyển dữ liệu cần đối chiếu và quay lui; dữ liệu cũ giữ chỉ đọc. Chỉ tuyên bố hoàn thành khi bảng bề mặt phủ đủ, cổng chặn xanh và UAT trên profile KN có bằng chứng.
