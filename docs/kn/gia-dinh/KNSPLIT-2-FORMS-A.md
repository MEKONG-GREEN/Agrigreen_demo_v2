## KNSPLIT-2-FORMS-A — Đăng ký ngữ cảnh ứng dụng cho biểu mẫu

Bước A của chuyển hai bước: forms/port.Profile nhận ActorResolver do ứng dụng cung cấp; ứng dụng KN sở hữu cấu hình ba profile và quy tắc ngữ cảnh, phòng ban, cơ sở/hộ. Gốc lắp ráp truyền cấu hình từ extension.FormProfiles. ActorRef và TenantID luôn lấy Principal đã xác thực, resolver không được thay danh tính/tenant; callback chỉ cấu hình server và không serialize.

Giữ fallback cũ để các consumer chuyển dần, chưa coi là đã gỡ KN khỏi forms/shared-authz. Bước B sẽ chuyển fixture/consumer còn lại sang resolver, gỡ nhánh KNContext và prune baseline có bằng chứng. Không thêm import KN trong forms, không đổi migration/API/tenant hay quyết định PO. Hạn chế quyền từ chối phải trả lỗi thẳng, không rơi về quyền realm. Adapter không tham gia D2/MRV.

Đang kiểm chứng PostgreSQL và consumers trước tích hợp; bằng chứng tại docs/REGRESSION.md.
