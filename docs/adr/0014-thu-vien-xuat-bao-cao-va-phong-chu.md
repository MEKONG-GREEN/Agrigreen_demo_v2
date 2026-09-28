# ADR-0014: Thư viện xuất Excel/PDF và giấy phép phông chữ nhúng

**Trạng thái:** Đề xuất — chờ người tích hợp chấp nhận giấy phép phông · **Ngày:** 28/09/2026

## Bối cảnh
pf/reports (KN US-DASH-02/03, FR-27; CGH FN-10) phải xuất báo cáo ra Excel và PDF có tiếng Việt đầy đủ dấu. Cần:
- thư viện Go thuần (không cgo, không dịch vụ ngoài — RB-7), giấy phép thuộc `deploy/policies/allowed-licenses.txt`;
- một phông chữ có đủ glyph tiếng Việt (Latin Extended Additional U+1EA0–U+1EF9, ₫) được phép **nhúng và phân phối lại** trong tệp PDF gửi cho người dùng.

Phông là **tài sản** (tệp dữ liệu nhúng bằng `go:embed`), không phải dependency Go, nên `go-licenses` không kiểm. Vì vậy cần quyết định tường minh.

## Quyết định
- Excel: `github.com/xuri/excelize/v2` (BSD-3-Clause). Ô số ghi nguyên chuỗi decimal đã lưu (`SetCellDefault`) — không qua float — với định dạng số `#,##0.00`; Excel hiển thị dấu ngăn cách theo máy người xem.
- PDF: `github.com/go-pdf/fpdf` (MIT), phông UTF-8 nhúng tập con. Số in theo quy ước Việt Nam (`domain.FormatVN`: `1.234.567,89`) tính trên decimal.
- Phông: **Roboto Regular + Bold** (Google), giấy phép **Apache-2.0** — thuộc danh sách giấy phép cho phép. Tệp nằm tại `internal/platform/reports/render/fonts/` kèm `LICENSE-Roboto.txt` (toàn văn Apache-2.0). Đã kiểm cmap có đủ 134 chữ cái tiếng Việt có dấu và ký hiệu ₫.
- Phương án thay thế đã cân nhắc, cũng chấp nhận được nếu cần đổi nhận diện: Noto Sans hoặc Be Vietnam Pro (SIL OFL-1.1 — cho phép nhúng và phân phối lại cùng phần mềm, cấm bán riêng phông). OFL-1.1 **chưa** có trong `allowed-licenses.txt`; muốn dùng thì bổ sung "OFL-1.1 (chỉ cho tài sản phông)" vào chính sách qua người tích hợp.
- DejaVu (đi kèm fpdf) không dùng: giấy phép Bitstream Vera riêng, không thuộc danh sách.

## Hệ quả
- Tệp PDF có kích thước tăng thêm phần tập con phông (~30–60 KB).
- Đổi phông chỉ cần thay tệp trong `render/fonts/` và cập nhật ADR này; bố cục không phụ thuộc số đo riêng của Roboto.
- `go-licenses` vẫn kiểm các dependency Go (excelize và phụ thuộc: mscfb/msoleps Apache-2.0, efp/nfp BSD-3, go-deepcopy MIT; fpdf MIT).
