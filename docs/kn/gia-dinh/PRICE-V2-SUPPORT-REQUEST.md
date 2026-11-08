## PRICE-V2-SUPPORT-REQUEST — Q10 / Báo giá v2 FN-CH-00.01

Nguồn duy nhất: BRD Báo giá v2.0, Drive `1fLQ5AYVOPTC8keXtbDPxvZn3QFkIwjeX`, bản sửa 05/11/2026;
text cache SHA256 `784b0af862668ba0a3edddb4c94f0c5d29c35fc2dac5df29d4ad96926d16a569`.
Theo Function chi tiết: “Liên hệ quản trị viên” tạo yêu cầu hỗ trợ; tài khoản chưa có vai trò/phòng ban đang áp dụng cần liên hệ Admin.
Q10 của kế hoạch standalone đã duyệt: lưu trong KN và Admin KN xem/xử lý, không gọi API support HTX cũ.

Quyết định triển khai tối thiểu: họ tên (1–100), email hoặc SĐT liên hệ (tối đa 254), nội dung (10–2000 ký tự).
Hai trạng thái Chưa xử lý/Đã xử lý; Admin xem nội dung và đánh dấu xử lý. Không thêm SLA, mức ưu tiên, file, chat, gửi email,
reset mật khẩu, PIN, chọn vai trò hoặc thay đổi phân quyền tài khoản. Nội dung không dùng để dò tài khoản.

Một Application API deployment tiếp nhận cho một tenant KN do operator cấu hình; public không nhận tenant/actor/role/key từ request.
Người chưa đăng nhập hoặc không có assignment dùng cùng public endpoint; sự kiện gửi ghi actor NULL (ẩn danh thực).
Kho giữ ciphertext; DEK bọc bằng khóa công khai của chủ thể vault `kn_support`, AAD ràng buộc tenant + request + purpose.
Admin chỉ mở khóa đúng purpose `kn.account_support`, đúng chủ thể `kn_support`, bằng token platform_admin + MFA thật.
Purpose và subject area khóa hai chiều; không cấp thêm vai vault chung. Các purpose/chủ thể hiện hữu khác giữ chính sách cũ.

Queue/detail/resolution kiểm realm Admin, MFA và phiên chủ KN hiện tại. Resolution dùng PUT với CAS và ledger replay gắn actor,
không đi cache POST chung. Audit vault và audit KN phải commit trước plaintext; sự kiện xử lý cùng transaction với CAS.
Public retry cùng UUID/nội dung chỉ một yêu cầu, success sau commit. Không có API đọc của người gửi/public.

Seam tích hợp: giữ `/login/lien-he` trong login-family allowlist của S2/#263; không lấy source chưa gộp #255/#263.
S10 profile KN cung cấp ba biến deployment theo [runbook](../../runbooks/kn-account-support.md); thiếu tất cả thì intake 503,
cấu hình một phần/sai thì startup fail closed. Không tự thay realm/account thật. Surface rows bổ sung sau BRD-SURFACE-MAP gộp.

Fingerprint chống retry khác nội dung là HMAC của payload theo UUID thao tác ngẫu nhiên; DB chỉ lưu băm UUID, không lưu khóa gốc hay hash PII không khóa.
