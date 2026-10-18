## KNSPLIT-2-PUB-READERS-B — Quyền đọc theo khai báo module

Nền14f266a2 (A đã đủ helper11, full5PG, static và remote verify). B xóa map PubReaders chứa cấu hình KN trong dbadmin. Bootstrap chỉ dùng catalog.PubReadOwners đã sinh từ consumes.publications/readpubview. Không có mặc định theo tên module. Metadata không thêm phụ thuộc gọi in-process. Generic fixture kiểm reader bất kỳ, thiếu khai báo kể cả tên reader cũ không sinh quyền, kết quả không sửa metadata; blackbox kiểm mọi runtime module role có đúng membership _pub đã khai (analytics giữ chính sách cũ riêng).

Giữ quyền OLD thực tế 26roleflags/41viewaccess/15membership trong tests/golden/pub-reader-access.json; không sinh lại hay ghi đè golden. Giữ mọi khai báo, source SQL, DDL, contract JSON và call depends của A. Không thay OpenAPI/UI/mobile/M1–M7/D2/tenant perCenter/MRV HTX-MKG. Không tuyên bố thu hồi grant từng cấp trước khi metadata đổi: Bootstrap vẫn cấp idempotent như cũ, không thêm cơ chế REVOKE mới ở B.

Kiểm: pure 7package (DBadmin PG ở full, không claim skip), static full/boundary chỉ giảm, full5PG (DBadmin/metrics/KN77/APIassembly/worker), helper11 có ACL actual. Chưa chạy, chưa tích hợp. G1 còn dữ liệu HTX/policy/auth/accounts/party/routes/tenant/client/KHTC; lịch/main/worktrees giữ.
