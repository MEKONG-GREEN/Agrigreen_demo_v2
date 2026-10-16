# KNSPLIT-2-REPORTS-B — Gỡ adapter phiên cũ

Nền dev18dd40f9 đã đăng ký ReportSessionResolver tại assembly, worker và KN77. Bước B bỏ legacyDispatchActor/RequireKN và fallback Principal.KN trong formReader. Dispatch chỉ nhận scope từ resolver đã đăng ký; không có resolver hoặc không xử lý thì Forbidden, thiếu Principal thì Unauthenticated. Identity actor/tenant vẫn lấy context, không cho callback thay thế. Native realm đọc form giữ nguyên, role groups KN/HTX/CGH và source ports chưa thuộc bước B.

Test âm trước sửa: TestReportSessionRequiresRegistration đỏ cả hai trường hợp missing/unhandled, đọc/ghi dispatch và đọc mẫu đều từng suy quyền từ KNContext. Sau sửa pure.exit0, gồm anonymous không tới resolver, identity/phòng/tỉnh/mask/on-behalf, denial/error không fallback, native realm. Cần đầy đủ PG/consumer và gates/helper trước tích hợp. Không thay API/SQL/UI, không bật Google Sheet hoặc gửi SMTP thật. Không đổi tenant/D2, MRV, M1–M7 và phạm vi G1 đã chốt.
