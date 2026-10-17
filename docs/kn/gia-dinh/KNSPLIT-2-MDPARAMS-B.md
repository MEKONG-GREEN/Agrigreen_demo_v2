## KNSPLIT-2-MDPARAMS-B — Gỡ tham số KN khỏi nguồn mặc định nền tảng

Nền c098c8bd đã có registry và 6 điểm lắp ráp KN (10 constructor calls). B xóa 16 mô tả extension.* / cnts.* và đúng 13 seed khỏi native masterdata; gỡ cache Defaults toàn cục không còn nơi gọi và ca capture có thể ghi golden. Golden cũ đóng băng, ca owner giữ đối chiếu byte.

Chuyển nguyên văn 11 khóa ParamCNTS* và IndicatorKey từ masterdata/port/cnts.go về KNport/masterdata_params.go, sửa đúng 4 nơi dùng KN. Giữ toàn bộ chuỗi khóa, hàm dựng khóa, giá trị/ngày/nguồn seed; không tạo alias từ capability sang KN. Native seed khác, kể cả mrv.* thuộc HTX/MKG, giữ nguyên dữ liệu. Chỉ prune baseline của 11 khai báo đã chuyển; không thay canonical paths hay thêm ngoại lệ.

Nil/empty registry không tự suy metadata/default KN; dữ liệu tham số đã duyệt trong DB vẫn đọc qua cổng generic như trước. Không đổi quyền, transaction, RLS, seed idempotency hay duyệt bốn mắt. Không đổi OpenAPI/UI/migration/D2/tenant/MRV/M1–M7. Đây mới xong phần tham số: catalog CNTS và typed lookup còn gói tách riêng.

Ca âm TestParametersRequireRegistration chạy trên A: exit1, cả nil/empty còn tự suy extension.task_sla_hours. Sau B, pure exit0 gồm missing-registration/owner frozen/invalid/crossowner/deepcopy/color/native-domain/alert-scope; KNapp compile, không có test. Full PG cần A16 + full KNhandler cho đổi khóa GPS trong schedule_dept; 17 package có test dự kiến. Chỉ tích hợp dev sau fullPG/static/helper8migration thật.
