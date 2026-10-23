## BF-SWEEP-TT — Áp UI-KIT cho màn Trồng trọt (#100)

- Nguồn: PRD `kn-bugfix-brd-alignment` nhóm A (TT-01..03) và C, quy tắc ở [ui-kit.md](../ui-kit.md). Chỉ sửa web (`apps/web-ops`), không đổi API, OpenAPI, Go hay migration.
- Phạm vi: các màn trong `kn/cycle/screens` (Dữ liệu xã, Phát hành, Trạng thái nộp, Rà soát & chốt kỳ, Báo cáo tổng hợp, Quản trị người dùng/phòng ban/cơ cấu). Không đụng lưới nhập kiểu Excel (`kn/grid/FormGrid`) và lịch công tác (`kn/schedule`, gói khác).

**Pop-up → trang riêng (R-A2, dựng bằng `DetailPage`, Hủy về danh sách/chi tiết, Lưu là nút chính):**
- Thêm HTX → `/kn/du-lieu/htx/them-moi`; Thêm nông hộ → `/kn/du-lieu/htx/[id]/them-nong-ho`.
- Tạo tài khoản → `/kn/quan-tri/nguoi-dung/them-moi`; Thêm vai trò → `…/[id]/vai-tro/them-moi`; Sửa vai trò → `…/[id]/vai-tro/[aid]/sua`; Phân quyền trong vai → `…/[id]/vai-tro/[aid]/phan-quyen`.
- Đợt đã phát hành → `/kn/nhiem-vu/phat-hanh/dot-da-phat-hanh` (bảng có STT). Thu hồi đợt nhập lý do trong pop-up ngắn thay `window.prompt`.
- Pop-up còn giữ, vì chỉ nhập lý do ngắn: Vô hiệu hóa HTX, Mở lại kỳ, Khóa/Mở khóa tài khoản, Xóa vai trò, Thu hồi ủy quyền, Đặt lại TOTP. Các pop-up này đã kiểm lý do bắt buộc và độ dài tối thiểu như máy chủ trước khi gọi API.

**Ô số (R-A1) và trường bắt buộc (R-A4):**
- HTX: Tên, Xã/phường, Người đại diện, Điện thoại bắt buộc (TT-08). Diện tích ≥ 0, tối đa 4 chữ số lẻ. Nông hộ: Chủ hộ, SĐT bắt buộc. Diện tích canh tác ≥ 0.
- Máy móc: số lượng nguyên > 0 (TT-10), kiểm khi rời ô và trước khi Lưu.
- Vật tư: thời điểm, loại, số lượng bắt buộc. Số lượng ≥ 0. Bẫy đèn (cái) chỉ nhận số nguyên như máy chủ.
- Phát hành: Từ/Đến ngày, Tên vụ/đợt bắt buộc. Đến ngày không trước Từ ngày. "Chốt kỳ sau hạn" là số nguyên 0–60, trước đây là `type="number"` nhận được số âm. Ô "Tỉnh giao" không âm và được chuẩn hóa thành chuỗi thập phân trước khi gửi.
- Rà soát: lý do yêu cầu điều chỉnh bắt buộc. Quản trị: Họ tên, Email bắt buộc khi tạo hoặc sửa tài khoản. Mỗi vai trò phải có ít nhất một phòng ban. Mã, Tên, Loại bắt buộc khi thêm đơn vị nội bộ. Lý do hỗ trợ bắt buộc ở Menu các cấp.
- Lỗi theo trường từ máy chủ (`errors[].field`) được gắn vào ô tương ứng qua `fieldErrorMap`.

**Bảng (R-A5):** các bảng `<table className="dt">` tự viết đã chuyển sang `DataTable` có STT. Gồm: khai báo máy, chọn biểu và Tỉnh giao khi phát hành, nhật ký cập nhật và chi tiết lần cập nhật của Báo cáo tổng hợp, vai trò và ủy quyền của tài khoản, phiên và lịch sử đăng nhập, ảnh hưởng khi di chuyển Nút, đơn vị nội bộ.

**Font (R-A3):** đã rà các màn trong phạm vi. Tiêu đề thẻ và khối đều đã là h1–h3 (hoặc `SectionTitle`/`LabelValueBlock`, cũng là h2), nên không phải đổi gì.

**Mock:** `POST /v1/kn/cooperatives/{ref}/households` của máy chủ giả lập từ chối `cultivated_area_ha` âm, giống `area_ha` của HTX.

**Bỏ qua, để gói khác làm (đã để `TODO(#…)` trong mã):**
- #102: trường "Loại" của HTX (HTX / Nông hộ / Tổ hợp tác) và việc tách ba danh sách (TT-08, TT-09). Form và API hiện chưa có trường này.
- #103: Năm sản xuất (≤ năm hiện tại) và HTX/Chủ sở hữu bắt buộc cho máy móc (TT-10). Form và API hiện chưa có.
- #104: sửa thửa, các trường bắt buộc của thửa, chặn chồng lấn (TT-11). Màn Vẽ thửa trên web hiện chỉ có danh sách.
- #105: vật tư làm lại dạng bảng nhập nhiều dòng (TT-12). Gói này chỉ áp ô số và trường bắt buộc vào form ghi nhận một dòng đang có.
- #106: Phát hành 5 bước (TT-14). Gói này giữ bố cục hiện tại, chỉ thêm kiểm số và trường bắt buộc.
- Mẫu biểu (`kn/forms`) và các màn dùng chung khác ngoài `kn/cycle`: không thuộc màn Trồng trọt trong gói này.
- Khối "Thêm đơn vị nội bộ" ở màn Phòng ban và các ô của màn Cơ cấu tổ chức là form ngay trên trang, không phải pop-up, nên vẫn giữ tại chỗ.
- Đổi trạng thái phòng ban và ngừng đơn vị nội bộ vẫn hỏi lý do bằng `window.prompt`. Phần này chưa đổi để diff gọn, có thể chuyển sang pop-up lý do ở #117 khi rà Quản trị.
