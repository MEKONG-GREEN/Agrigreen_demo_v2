# Van công bố (MKG → nền tảng)

Mục 5 của tài liệu kiến trúc: dữ liệu từ vùng nội bộ MKG sang nền tảng chỉ đi qua các hợp đồng trong thư mục này. **Mặc định là từ chối.**

- Mỗi hợp đồng là một tệp `<tên>.json` có dạng `{"name": "<tên>", "fields": ["..."], "owner": "...", "approved_by": "...", "approved_at": "YYYY-MM-DD"}`.
- Struct Go xuất ra van phải có chú thích `//ag:publication <tên>`.
- `go run ./tools/contractcheck` làm hỏng build khi:
  - struct có trường JSON không nằm trong `fields`;
  - struct được chú thích mà không có hợp đồng tương ứng.
- Thêm trường vào hợp đồng cần một PR riêng, có người phê duyệt khác người đề xuất (R-56).

Ở G0 chưa có hợp đồng nào. Các hợp đồng đầu tiên thuộc G2, cùng với thiết kế chi tiết `mkg/*`.
