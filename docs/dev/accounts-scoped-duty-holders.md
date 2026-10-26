# Người nhận theo nhiệm vụ và nút phát hành

`accounts.Module.ScopedDutyHolders()` cung cấp truy vấn nội bộ cho worker với tenant tường minh. Vai, phòng ban, nhiệm vụ và nút hiệu lực phải thuộc cùng một assignment đang hoạt động tại một thời điểm lấy từ đồng hồ máy chủ. Nút hiệu lực ưu tiên UnitRef khác zero; chỉ khi thiếu mới dùng OrgRef. Kết quả khử trùng actor sau khi lọc assignment.

Worker không cần dựng Principal. Nếu context đã chứa danh tính hoặc tenant thì tenant phải khớp truy vấn. Tenant/nút zero hoặc tham số trống bị từ chối trước khi đọc DB; không có wildcard. Truy vấn dùng giao dịch chỉ đọc với r_accounts, không cấp quyền theo delegation hay sửa danh tính.

Đây là primitive shared của #63 (con #47), chưa phải job nhắc KHTC. Chủ ứng dụng chịu trách nhiệm xác định tenant, issuer, role, department và duty hợp lệ; port không phải API cho người dùng tùy chọn phạm vi. RoleHolders cũ giữ nguyên. Không đổi HTTP, schema, ACL hoặc migration.

Kiểm thử DB bao gồm nhiều Trung tâm cùng tenant, actor có nhiều assignment, ưu tiên UnitRef, hết hạn/thu hồi, tenant khác, danh tính không khớp, đầu vào rỗng và delegation không tạo duty. Factory/manifest không thay cấu hình Navigation.
