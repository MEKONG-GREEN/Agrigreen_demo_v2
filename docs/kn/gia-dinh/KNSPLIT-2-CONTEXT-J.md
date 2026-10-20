## KNSPLIT-2-CONTEXT-J — lịch công tác dùng phiên owner KN

- Phiên đã chọn luôn giải bằng extension/port.RequireSession; legacy marker không phải nguồn quyền. Lỗi/mất payload không rơi về resolver cũ hoặc realm role. Phiên owner hợp lệ hoạt động khi không có Principal.KN.
- Ngữ cảnh lịch lấy role, department, communes, cấp quản lý, đơn vị nội bộ và chế độ chỉ xem từ owner. Tỉnh lấy qua cổng tổ chức từ UnitRef (OrgRef dự phòng), tối đa bốn tổ tiên như resolver trước; lỗi phụ thuộc được giữ.
- Quy tắc người theo dõi lấy Manager/duty từ owner; legacy manager hoặc raw platform_admin không cấp quyền thêm. Lưu, sao chép, duyệt đơn/lô và check-in kiểm chỉ xem trước I/O. Không thay lịch, hạn đăng ký/duyệt, địa điểm hay lưu/ẩn/xóa tọa độ GPS.
- Request thực sự chưa chọn phiên (không selected và không legacy marker) tạm giữ resolver/realm gốc để chuyển tiếp các consumer chưa dời. Không coi đây là hoàn tất bỏ fallback khỏi accounts/authz.
- Không đổi Principal identity/tenant/raw roles/MFA; không tạo Principal cho worker. Không SQL/ACL/OpenAPI/menu/golden hoặc chuyển tenant. MRV vẫn thuộc HTX/MKG; GPS lịch công tác này thuộc KN.
- Kiểm tra: app unit owner-policy/error/native compatibility; HTTP public module root với PG thật, chín trạng thái phiên và đếm giao dịch; toàn bộ extension handler (lịch có dữ liệu), contextdata và KN77/API consumers. Các cổng tĩnh/biên giới và migration helper phải đạt trước tích hợp.
- Trạng thái: đã hiện thực, đang chạy kiểm thử; chỉ root gộp dev bằng helper sau bằng chứng thật. G1 còn shared legacy/accounts/party, dữ liệu riêng KN và tenant move.


Kết quả thực (21/10/2026): source35581 actual0, app1.129s/extension-handler176.871s/contextdata148.320s/HTTP125.689s, đủ chín ca HTTP và không FAIL/SKIP/INFRA. PG60155/3f406438c6f5 đã dọn. Giữ nguyên log gốc PS5 trộn UTF8/UTF16; bản normalized tách NUL được kiểm marker và đủ chín lá PASS. Static65527 actual0/lint0/boundary314/fmt2fixture cũ.

Sau source/static đã kết thúc, commit99409f32 bổ sung readonlyfalse cho gợi ý duyệt theo MỌI quy tắc (ngoài chặn thực thi đã có); unit hiện tại95493 PASS1.368s; static-current40610 actual0/lint0/boundary314. Full5-verified28682 actual0: dbadmin7.137s/metrics164.783s/KN77517.773s/API160.645s/worker3.335s, không FAIL/SKIP/INFRA; PG61724/e5b450d45dff đã dọn. Guard33994 cũ dừng trước PG, không tính là suite đã chạy.

Toàn bộ tests/golden, migrations và contracts giữ nguyên nền2bfb. Chưa tích hợp: ticket #46 thuộc quy trình mới TWO-AGENT, cần review chéo và khóa qua ticket.ps1 merge; không đặt AG_INTEGRATION_LOCK thủ công để vượt guard.
