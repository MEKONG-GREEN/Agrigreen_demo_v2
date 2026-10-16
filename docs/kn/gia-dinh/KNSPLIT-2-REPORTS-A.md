## KNSPLIT-2-REPORTS-A — Đăng ký policy phiên cho cap/reports

Nền dev0237b812. Bước A thêm port.SessionResolver, không thay API/schema hoặc nhóm realm roles cũ cho báo cáo HTX/CGH. Đường tương thích legacy còn để chuyển consumer tuần tự; bước B mới gỡ. Chưa coi toàn bộ reports đã độc lập.

KN sở hữu ReportSessionResolver: vai nào đọc layout, vai nào đọc/gửi Chi cục, phòng/tỉnh, che hộp thư, on-behalf. Core giữ actor/tenant từ Principal; projection không có trường để ghi đè hai danh tính đó. handled denial/error không fallback; thiếu grant/filter hoặc phạm vi dispatch không đầy đủ thì từ chối. Realm đọc mẫu hiện hành giữ ưu tiên như trước. Phiên ViewOnly bị chặn ghi ngay trong policy bên cạnh middleware hiện có.

Đã nối policy thật ở api-platform assembly, worker reports factory và fixture KN77. Không thay gửi SMTP/tích hợp bên ngoài, không bật Google Sheet; báo giá/MRV/tenant/D2 và M1–M7 giữ nguyên. Error dependency dùng mã reports.dependency_unavailable đã có.

Kiểm chứng: ca thuần identity, phạm vi phòng/tỉnh, mask địa chỉ, on-behalf copy, deny/error không fallback, realm/legacy compatibility và policy KN gồm ViewOnly. Cần chạy PG thật reports/... + extension root + assembly + worker + KN77, cổng CODEX-G1/helper trước tích hợp. Không lặp các suite không đổi UI/API/SQL; bước B phải có test âm không tự suy KN khi chưa đăng ký.
