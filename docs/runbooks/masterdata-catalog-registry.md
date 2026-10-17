# Registry danh mục có hiệu lực theo ngày

Thay đổi này mở ràng buộc tên danh mục của sổ pf_masterdata.catalog_effective cho các danh mục đã được ứng dụng đăng ký. Role, RLS, ledger trigger, cột và tên vật lý giữ nguyên. Registry/HTTP vẫn kiểm danh mục có đăng ký; một tên đúng cú pháp không tự cấp quyền ghi.

Áp bằng bộ chạy migration chuẩn, đúng thứ tự ba tệp timestamp của gói MDCATALOG-A. Không gộp chúng vào một transaction thủ công: bước thêm CHECK NOT VALID cần commit trước bước quét xác minh; bước bỏ CHECK tiền tố cũ chỉ chạy sau khi CHECK mới validated. Bộ chạy v2 thực hiện mỗi tệp trong transaction riêng (internal/shared/migrator/runner.go).

Hai bước DDL ngắn có lock_timeout5s. Nếu không lấy được khóa, để lệnh thất bại rồi chạy lại bằng bộ chạy chuẩn sau khi xử lý giao dịch dài. Bước VALIDATE cần quét các phân vùng; thời gian phụ thuộc lượng dữ liệu và I/O. Cần đo trên bản sao dữ liệu đích trước triển khai. Cách tách bước dựa trên [PostgreSQL17 ALTER TABLE](https://www.postgresql.org/docs/17/sql-altertable.html): NOT VALID hoãn quét; VALIDATE dùng khóa nhẹ hơn bước ADD.

Chỉ private PostgreSQL được sử dụng trong phát triển. Kiểm thử upgrade của gói tạo 1.000 dòng sổ trước thay đổi, so số dòng/checksum nội dung sau từng bước, kiểm chuỗi băm rồi thêm tên không có tiền tố CNTS. Không coi fixture là tải production.

Khôi phục ứng dụng về phiên bản trước vẫn giữ CHECKgeneric đã valid: tập tên cũ là tập con nên mã cũ tiếp tục dùng dữ liệu cũ. Không Down migration, không sửa tenant_id/hàng sổ, không xóa tên mới để ép lại CHECK tiền tố. Nếu cần hạn chế một danh mục, gỡ đăng ký ứng dụng hoặc viết migration tiến riêng sau kiểm kê dữ liệu; giữ nguyên lịch sử và hash. Tệp migration cũ bất biến.
