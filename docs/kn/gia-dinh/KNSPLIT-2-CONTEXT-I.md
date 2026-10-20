## KNSPLIT-2-CONTEXT-I — Phiên KN cho Báo giá, HCTH và Contact

Phạm vi: ba module `internal/kn/price`, `hrm`, `contact` đọc ảnh chụp quyền của chủ KN qua callback cấu hình. Module capability không nhập ngược domain extension; adapter nằm tại gốc lắp ráp extension và chỉ ánh xạ trường quyền cần thiết. Actor, tenant, realm roles và MFA giữ nguyên từ principal đã xác thực. Không giải payload từ header hoặc tạo principal hệ thống.

- Cấu hình KN tại API assembly bắt buộc bộ giải owner; không có hoặc hỏng binding bị từ chối, không dùng `Principal.KN` hay vai realm thay thế.
- Cấu hình native với callback nil giữ chính sách realm riêng; không suy ứng dụng từ tên profile hoặc vai token. Q-072 native vẫn được cung cấp qua cấu hình native, còn cấu hình KN yêu cầu phiên KN.
- Báo giá giữ vai, cờ QTNV, các phép tính decimal, lịch và duyệt. HasDuty không cấp quyền trong phiên chỉ xem; middleware accounts vẫn chặn yêu cầu ghi trong phiên chỉ xem/hỗ trợ trước khi vào module.
- HCTH giữ phòng HCTH, nhóm ma trận Admin/HCTH/quản lý/Tỉnh/Xã/TW, đơn vị nội bộ, ViewOnly và phạm vi bản thân. Hồ sơ/kho tài liệu vẫn trả 403 khi thiếu phiên; danh mục KN dùng lỗi owner 428 khi chưa chọn phiên.
- Contact giữ bộ trường basic/full, quyền quản lý theo phòng, read-only và lọc dữ liệu cá nhân. Nhãn đơn vị KN lấy UnitRef của owner.

Không đổi schema, ACL 26/59/24, migration, OpenAPI hoặc 35 golden. Không đổi M1–M7, tenant theo Trung tâm tỉnh, MRV HTX/MKG, mẫu KHTC nạp sẵn hoặc D2 chỉ hồ sơ KN trong G1.

Kiểm thử: các suite handler của ba module và domain, TestContactOverHRM tại API assembly, owner HTTP/PG mới tại tests/kn77; root chạy full5 và helper tích hợp tuần tự vào dev sau khi có exit thực và không FAIL/SKIP/INFRA. Kết quả thực ghi tại log TEMP và phần trạng thái cuối; không coi compile-only là bằng chứng PG.

### Bằng chứng trước tích hợp — 20/10/2026

Core `6984d4d4`, fixture/doc fix `a1157185`; production không đổi sau core. Log tại TEMP prefix `agrigreen-codex-knsplit2contexti`.

- Source ban đầu exit1 đúng bốn lỗi fixture: hai mong đợi 403 cần cập nhật thành lỗi bắt buộc chọn phiên 428 và hai direct Contact.DisplayNames chưa bind registry. Giữ nguyên log; không gọi aggregate này xanh. HRM handler (191.943s), các domain price/HRM/contact (0.747/0.947/0.783s) đã PASS thực trên cùng production và được giữ.
- `source-pg-verified` exit0 không FAIL/SKIP/INFRA: price handler 472.302s, contact handler 222.262s, contextdata 208.542s, KN77 hai test mới 196.513s, API assembly ContactOverHRM 205.992s. Tổng 34 ca mới: 24 owner + 4 phòng/read-only + 6 native. Positive không có legacy là đọc tenant rỗng qua PostgreSQL; các luồng có dữ liệu được bảo vệ bởi bộ nguồn đã nêu.
- `static-verified` exit0 đủ build/vet/modgen/migratecheck/archlint/contractcheck/docscheck/mfaflagcheck/knboundary; boundary314, đúng hai fixture gofmt cũ. `lint` exit0, 0 issues. Static ban đầu chỉ lỗi định dạng roadmap và được giữ log.
- Guarded `db-full` session89833 thực exit0: dbadmin6.063s, metrics192.798s, full KN77/KHTC368.873s, full API assembly199.183s, worker3.646s; không FAIL/SKIP/INFRA. Guard kiểm composite nguồn cùng production và đúng 24 owner/6 native leaves. Cả hai private PG đã dọn (nguồn 59369/e4f69762e953, full5 56981/6a0b0cf380cd). PG trực tiếp; không coi đây là kiểm chứng PgBouncer.
- Migrations/contracts/35 golden giữ nguyên theo diff với base `a4dbc7e2`. Chưa rebase/helper/push; root gộp tuần tự dev sau helper với full KN77, API assembly và 9 migration/ACL gates. Shared legacy/account ownership và adapter khác vẫn là gói riêng; chưa xóa Principal.KN toàn hệ thống.
