# BF-SWEEP-KHTC — lát Danh mục KHTC (#164)

Đối chiếu **BRD KHTC v2.3 ngày 08/10/2026**, FN-AD-04.01, các đoạn P1649–P1674 trong DOCX, SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25` (khớp [ma trận nguồn](../doi-chieu-brd-bugfix-2026-10.md)). Bản trích `memory/brd/2.txt` và DOCX v2.2 ở thư mục V1.0 chỉ là bản cũ; không dùng chúng thay v2.3. Yêu cầu UI: PRD R-E1/R-A1–A5 và [UI-KIT](../ui-kit.md).

## Phạm vi hoàn chỉnh của lát này

Chuẩn hóa cả 7 loại trong Danh mục KHTC: Loại báo cáo, ĐVT, Lĩnh vực, Nguồn kinh phí, Người ký, Nơi nhận, Căn cứ.

- Danh sách dùng DataTable tự sinh STT, giữ số lượng theo loại và bộ lọc ngừng dùng; loại/bộ lọc lưu trong URL.
- Thêm, sửa, xem có route riêng dưới menu Danh mục KHTC; DetailPage có liên kết danh sách, Lưu/Hủy và Log. Đoạn `/muc/[code]` tránh nhầm mã hợp lệ `them-moi` với trang tạo mới.
- Log lấy từ API lịch sử hiện có, sắp phiên bản, hiển thị người/thời điểm và giá trị cũ → mới. Không dựng lịch sử giả từ trạng thái giao diện.
- TextField/NumberField + validateForm: lỗi bắt buộc, mã 1–40 ký tự đúng quy tắc, tên tối đa 500, thuộc tính tối đa 300; Người ký bắt buộc Chức vụ. Lỗi máy chủ gắn về trường, kể cả trùng mã và xung đột người ký mặc định.
- Thứ tự là số nguyên 0–100000, rỗng thành 0 như API hiện tại. Chặn độ dài thô trên 32 ký tự và số mũ **trước** bộ chuẩn hóa/Decimal của UI-KIT, cả khi nhập/blur và khi Lưu. Giá trị bị từ chối không thay bằng số cũ để gửi; người dùng phải sửa lại. Guard chỉ ở feature này, không sửa `components/form.tsx` đang thuộc #163.
- Giữ API, idempotency và nghiệp vụ cũ: Admin có quyền ghi; phiên chỉ xem/cấp khác không mở form ghi; ngừng dùng/dùng lại tạo phiên bản, không xóa; mặc định chỉ ảnh hưởng bản soạn mới, một mẫu chỉ một người ký mặc định. Không thay quyền native/phạm vi, không ghi dữ liệu HTX.

## Phần còn lại

**Không coi #164 đã phủ toàn bộ KHTC.** [#180 BF-SWEEP-KHTC-REST](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/180) giữ PlanScreen, periods, inbox, reports và forms: thay popup, bảo toàn bản nháp, kiểm nhập, Log/STT phù hợp. #163 chỉ HCTH, không trùng với #164/#180. Các thiếu hụt nghiệp vụ đã có (nhập Excel danh mục, mục giá dùng cap/price…) không được mở rộng trong lát UI này. Không đổi M1–M7, FormGrid hoặc mẫu giấy nạp sẵn.

## Kiểm chứng

Các ca mới ở `catalog.test.tsx` kiểm trang riêng, 7 loại, lỗi từng trường, quyền chỉ xem, lịch sử, trạng thái và thử lại cùng Idempotency-Key. Probe số gồm `1e100000`, `1e-100000`, chuỗi 100000 chữ số, âm/lẻ/vượt biên; xác nhận chuỗi cực trị không vào Decimal.parse và không gọi API. E2E `kn-khtc-catalog.spec.ts` đi qua danh sách → thêm → chi tiết → sửa → ngừng dùng/dùng lại, giữ bộ lọc, Log và xung đột người ký mặc định. Bằng chứng cổng/DB/web/mutation được ghi trong bình luận ready; không coi có mã là đã được khách UAT.
