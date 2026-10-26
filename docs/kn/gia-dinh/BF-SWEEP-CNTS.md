## BF-SWEEP-CNTS — Áp UI-KIT cho màn Chăn nuôi – Thủy sản (#101)

- Nguồn: PRD `kn-bugfix-brd-alignment` nhóm A (CN-01..03) và D (CN-15), quy tắc ở [ui-kit.md](../ui-kit.md). Chỉ sửa web (`apps/web-ops`), không đổi API, OpenAPI, Go hay migration.
- Phạm vi: các màn trong `kn/cnts` (Danh mục, Biểu kỳ, Báo cáo kỳ, KSGM BM-02, Phụ lục 2, Cảnh báo, Dịch bệnh, Lịch tiêm, Lịch trực, Hộ mô hình, Chương trình dự án, Cấp quyền) và route `app/kn/cnts/**`, `app/kn/phong-ban/[dept]/*cnts*`. Không đụng lưới nhập kiểu Excel (`kn/grid/FormGrid`) và lịch công tác (`kn/schedule`, gói khác).

**Pop-up → trang riêng (R-A2, dựng bằng `DetailPage`, Hủy về danh sách/chi tiết, Lưu là nút chính):**
- Danh mục CNTS có 12 loại danh mục nhưng dùng chung một màn, nên chuyển bằng một route chung cho mọi loại: `…/danh-muc/[type]/them-moi`, `…/[type]/[code]` (chi tiết, kèm bảng các giá trị theo ngày hiệu lực thay pop-up "Hiệu lực"), `…/[type]/[code]/sua`. Có ở cả `/kn/cnts/danh-muc` và `/kn/phong-ban/[dept]/danh-muc-cnts`. Danh sách mở đúng tab theo `?tab=`.
- Pop-up còn giữ, vì chỉ nhập lý do ngắn (`ReasonDialog`, kiểm độ dài như máy chủ trước khi gọi API): Ngừng dùng mục danh mục; Mở lại / Hủy kỳ biểu; Mở lại tuần KSGM và Phụ lục 2; Kết thúc / Mở lại ổ dịch; Bác bỏ tin báo; Kết thúc / Mở lại dự án; Rút hộ mô hình; Thu hồi quyền (kèm người nhận việc); Hủy lịch tiêm. Các thao tác này trước đây dùng `window.prompt` hoặc ô lý do nằm trên trang.
- Các màn CNTS khác không có pop-up thêm/sửa. Biểu nhập của chúng là khối mở ngay trên trang (BM-10, tin báo, báo cáo kỳ, điểm/hộ mô hình, dự án, cấp quyền, lịch tiêm, nhắc nhở). Gói này giữ bố cục đó và chỉ áp ô số, trường bắt buộc, bảng STT. Việc tách thành trang riêng để cho gói nghiệp vụ của từng màn (xem mục Bỏ qua).

**Ô số (R-A1, CN-01):** dùng `NumberField`/`parseNumberInput`. Nhận dấu phẩy thập phân kiểu vi-VN và gửi chuỗi đã chuẩn hóa. Quy tắc lấy theo máy chủ:
- Danh mục: "Thứ tự cột" nguyên 0–999. Lần tiêm nguyên 1–20, ngày tuổi nguyên 0–3650.
- Báo cáo kỳ BM-01/04/05/17: mọi ô số không âm (máy chủ khai báo `non_negative` cho mọi trường), nguyên hoặc tối đa 6 số lẻ theo trường.
- KSGM: số điều chỉnh **được âm** vì là dòng cộng/trừ vào số tự cộng, tối đa 3 số lẻ (`allowNegative`, có ghi lý do trong mã). Phụ lục 2: diện tích và sản lượng nuôi trồng không âm, tối đa 4 số lẻ.
- Cảnh báo: nhắc trước hạn nguyên 0–168 giờ, trễ liên tiếp 0–12 kỳ, hạn xử lý nguyên 1–720 giờ (trước đây là `type="number"`).
- Dịch bệnh BM-10: quy mô đàn nguyên > 0, số con chết / tiêu hủy / ngày tuổi nguyên ≥ 0, kg / lít / sai số ≥ 0 và tối đa 3 số lẻ. Tin báo: số con nguyên ≥ 0.
- Lịch tiêm: tuổi lúc nhận nguyên 0–3650, số con nguyên > 0. Điểm và hộ mô hình: quy mô > 0, tối đa 2 số lẻ. Dự án: kinh phí nguyên ≥ 0.

**Trường bắt buộc (R-A4):** bấm Lưu thì báo lỗi dưới từng trường và không gọi API khi còn lỗi. Trước đây có màn khóa nút Lưu và gộp lỗi thành một dòng. Chỉ áp cho trường máy chủ đã bắt buộc:
- Danh mục: Mã, Tên, Ngày áp dụng và các thuộc tính bắt buộc của từng loại.
- Phát hành biểu kỳ: Từ/Đến ngày, đơn vị nhận. Đến ngày không trước Từ ngày. Giờ hạn nộp theo dạng HH:MM.
- KSGM: Tuần, Cơ sở, Lý do điều chỉnh. Cảnh báo: Kết quả xử lý ≥ 10 ký tự.
- Tin báo: Xã, Địa bàn, Mô tả. Lịch tiêm: Xã, Loại mô hình, Địa bàn, Ngày nhận.
- Điểm trình diễn: Xã, Tên, Loại mô hình. Hộ mô hình: Họ tên chủ hộ, Cán bộ ghi nhật ký khi cán bộ ghi. Dự án: Mã, Tên, Loại, Bắt đầu. Cấp quyền: Tài khoản, Xã, điểm/hộ theo vai.
- Lỗi theo trường từ máy chủ (`errors[].field`) được gắn vào ô tương ứng.

**Bảng (R-A5):** các bảng danh sách tự viết (`<table className="dt">`, `className="table"`) đã chuyển sang `DataTable` có STT. Có 26 bảng, gồm cả bảng các giá trị theo ngày và bảng mũi tiêm của danh mục.

Giữ nguyên bố cục biểu báo cáo, vì đây không phải danh sách:
- BM-02 có dòng cộng theo xã và dòng tổng.
- Phụ lục 2 có dòng nhóm và dòng khu vực, kèm ô nhập nuôi trồng trong bảng.
- Lưới ca trực kiểu bảng tính.

**Font (R-A3, CN-03):** đã rà các màn trong phạm vi. Tiêu đề thẻ và khối đều đã là h1–h3. Chỉ đổi khối "Nạp … từ Excel/CSV" của danh mục từ chữ đậm sang h3.

**Mock:** máy chủ giả lập dùng cùng quy tắc với máy chủ:
- Dự án: từ chối kinh phí âm hoặc thập phân.
- Điểm và hộ mô hình: từ chối quy mô ≤ 0.
- Lứa tiêm: từ chối tuổi lúc nhận ngoài 0–3650 và số con < 1.

**Bỏ qua, để gói khác làm (đã để `TODO(#…)` trong mã):**
- CN-15 (#111): biểu "Bản đồ vùng dịch" (Tên vùng, Loại, Xã/phường bắt buộc; diện tích > 0) chưa có trên web. Hiện chỉ có điểm ổ dịch BM-10. Chuyển biểu nhập BM-10 và tin báo sang trang riêng cũng để cho #111.
- #108: cấu hình phát hành đầy đủ (cán bộ phụ trách, nhóm xã, xem trước số nhiệm vụ, trạng thái Nháp / Trả lại, nhắc theo từng biểu). Ô "Mã xã nhận" vẫn nhập mã, chỉ thêm kiểm bắt buộc.
- #109: nhật ký BM-08/BM-09 theo hộ mô hình. Biểu hộ và điểm vẫn mở ngay trên trang.
- #29: Mẫu KNQG 01/03/04/05/07 và bước xác nhận của TW cho chương trình, dự án.
- #110 (báo cáo tuần) và #112 (khai thác): không thêm trường nghiệp vụ. Gói này chỉ áp ô số cho BM-17 và Phụ lục 2 đang có.
