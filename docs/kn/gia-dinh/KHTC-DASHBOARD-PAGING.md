## KHTC-DASHBOARD-PAGING — lát phân trang W11.69-DASHBOARD-FULL #189

Căn cứ BRD KHTC v2.3 FN-TI-02.01, bản gốc SHA256 `15859351F90574BF667190B1A12160465BA03F8CA9E408B94C90D285A3502E25`; prototype chỉ là đặc tả. Sau #166/#190. Phần còn lại ở #202; chưa công bố hoàn tất FN hay toàn G1.

- Dashboard quét năm bằng cursor UUID giảm dần, mỗi truy vấn tối đa100 kỳ, lọc năm/phòng/quyền adhoc trước LIMIT. Không còn trả422 ở100 kỳ và không tăng thành trần lớn khác để che thiếu dữ liệu. Events/active adhoc requests đọc theo lô trong TX RLS; SourceStates chia lô tối đa100, Crosswalk I/O ngoài TX.
- Terminal đóng/hủy không gọi Progress/Drafts. Kỳ week/month/program/adhoc ngoài quý không chiếu tiến độ. Ngoại lệ các kỳ quarter/year chưa terminal vẫn chiếu để giữ trạng thái chính xác của bảng báo cáo thường niên; không đổi collecting/complete thành trạng thái giả. Không sửa List API100 hiện hữu của màn khác.
- Progress/Drafts native cho kỳ thực còn liên quan dùng cổng hiện hành sau TX. Chưa có cổng native batch; không truy vấn SQL trực tiếp vào Forms hay giả quyền để tối ưu. Không có yêu cầu mở kỳ/bản soạn mới khi chỉ đọc dashboard.
- Kỳ adhoc được tính giống server: tỉnh được ghi hoặc Admin đã MFA và không readonly; TW/readonly không được thấy. Không giả context/phòng. Mock sửa cùng semantics, vẫn lọc tenant và phòng. Phiếu supplement/returned không sent; số nguồn closed/open/unknown và big.Rat/decimal trước rounding giữ nguyên.
- Kỳ được tạo thêm giữa các trang (UUID mới lớn hơn cursor) xuất hiện ở lần làm mới; không hứa snapshot nguyên tử xuyên các service. Thời điểm nguồn có thể đổi trong request như trước. Mỗi trang bounded; response vẫn chứa đủ các dòng hợp lệ, không cắt ngầm.
- Các ô số/bộ lọc hiện hữu giữ validation; không thêm ô số, không đổi OpenAPI/menu/migration. Test API kiểm1e±100000/chuỗi dài qua hồi quy cũ. Không HTX, baseline hay core shared.

Còn lại #202: live Mẫu5 đến ngày chọn kể cả chưa mở kỳ; lãnh đạo/BGĐ từ phòng gốc; badge; nhắc có idempotency/audit/outbox; lọc lĩnh vực đầy đủ theo PO. Lĩnh vực hiện vẫn chỉ lọc tiến độ chương trình.

Kiểm chứng: `TestDashboard189PagesAndNativeProjectionScope` mở105 kỳ qua KN/native Forms thật, thêm closed/cancelled/ngoài quý và phòng khác; kiểm qua nhiều trang, đúng số gọi native và TW/403/tenant. Hồi quy dashboard cũ giữ returned và nguồn closed/open/unknown, numeric hostile. Vitest mock >100/tenant/phòng; Playwright mở kỳ phát sinh rồi dashboard cập nhật trong cùng SPA. Kết quả chạy thực tế được ghi trong issue ready đúng SHA; tên test không thay bằng chứng PASS.
