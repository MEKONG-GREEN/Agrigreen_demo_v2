## KNSPLIT-2-CONTEXT-I — Phiên KN cho Báo giá, HCTH và Contact

Phạm vi: ba module `internal/kn/price`, `hrm`, `contact` đọc ảnh chụp quyền của chủ KN qua callback cấu hình. Module capability không nhập ngược domain extension; adapter nằm tại gốc lắp ráp extension và chỉ ánh xạ trường quyền cần thiết. Actor, tenant, realm roles và MFA giữ nguyên từ principal đã xác thực. Không giải payload từ header hoặc tạo principal hệ thống.

- Cấu hình KN tại API assembly bắt buộc bộ giải owner; không có hoặc hỏng binding bị từ chối, không dùng `Principal.KN` hay vai realm thay thế.
- Cấu hình native với callback nil giữ chính sách realm riêng; không suy ứng dụng từ tên profile hoặc vai token. Q-072 native vẫn được cung cấp qua cấu hình native, còn cấu hình KN yêu cầu phiên KN.
- Báo giá giữ vai, cờ QTNV, các phép tính decimal, lịch và duyệt. HasDuty không cấp quyền trong phiên chỉ xem; middleware accounts vẫn chặn yêu cầu ghi trong phiên chỉ xem/hỗ trợ trước khi vào module.
- HCTH giữ phòng HCTH, nhóm ma trận Admin/HCTH/quản lý/Tỉnh/Xã/TW, đơn vị nội bộ, ViewOnly và phạm vi bản thân. Hồ sơ/kho tài liệu vẫn trả 403 khi thiếu phiên; danh mục KN dùng lỗi owner 428 khi chưa chọn phiên.
- Contact giữ bộ trường basic/full, quyền quản lý theo phòng, read-only và lọc dữ liệu cá nhân. Nhãn đơn vị KN lấy UnitRef của owner.

Không đổi schema, ACL 26/59/24, migration, OpenAPI hoặc 35 golden. Không đổi M1–M7, tenant theo Trung tâm tỉnh, MRV HTX/MKG, mẫu KHTC nạp sẵn hoặc D2 chỉ hồ sơ KN trong G1.

Kiểm thử: các suite handler của ba module và domain, TestContactOverHRM tại API assembly, owner HTTP/PG mới tại tests/kn77; root chạy full5 và helper tích hợp tuần tự vào dev sau khi có exit thực và không FAIL/SKIP/INFRA. Kết quả thực ghi tại log TEMP và phần trạng thái cuối; không coi compile-only là bằng chứng PG.
