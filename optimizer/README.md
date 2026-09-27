# optimizer (Python 3.12, G2)

Đây là dịch vụ tối ưu hóa và mô phỏng nặng cho bài toán chọn hub, dùng OR-Tools và HiGHS/Pyomo. Nó là ngoại lệ bắt buộc về ngôn ngữ (mục 4).

Quy ước:
- Nhận việc qua mẫu bất đồng bộ ở mục 12. Đầu vào là **ảnh chụp bất biến** gồm tham số, phiên bản đồ thị, kết quả định tuyến đã dùng và `algorithm_version`.
- Chỉ nhận dữ liệu **không chứa định danh** (RB-7).
- Engine mô phỏng và bộ 49 tham số của prototype `mekong-green` (`src/erp/simulation`, `src/erp/params`) là đặc tả để chuyển sang đây.
