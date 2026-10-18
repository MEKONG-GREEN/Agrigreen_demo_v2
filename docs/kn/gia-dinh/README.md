# Giả định theo gói — đợt W11 (BRD Khuyến nông 10/2026)

Phần chung (vì sao có giả định, mặc định chung QĐ-5/6/7) ở [../gia-dinh-brd-2026-10.md](../gia-dinh-brd-2026-10.md). Mỗi gói một tệp ở thư mục này:

- Tên tệp: mã gói, vd `W11.42.md`, `W11.05b.md`.
- Dòng đầu: `## <mã gói> — <tên gói> (<migration nếu có>)`; sau đó bảng như các tệp có sẵn: câu hỏi (QĐ/Q), phương án đã chọn, ở đâu trong mã/tài liệu, đổi thế nào nếu PO chọn khác.
- Khi PO trả lời: sửa dòng trong tệp của gói và ghi ngày.
- Gói làm lại / gói con dùng tệp của mình (vd `W11.72.md`), không sửa tệp gói khác.

`go run ./tools/docscheck` (CI) kiểm tên tệp, dòng đầu và liên kết tương đối. Không có danh sách tệp ở đây (danh sách là chính thư mục), nên thêm gói không sửa README này. Tách từ tài liệu một tệp ngày 13/10/2026 (WH-B), nội dung giữ nguyên byte; thứ tự cũ ghi ở cuối [../gia-dinh-brd-2026-10.md](../gia-dinh-brd-2026-10.md).
