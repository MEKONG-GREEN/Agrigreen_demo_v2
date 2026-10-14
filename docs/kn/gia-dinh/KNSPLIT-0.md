## KNSPLIT-0 — Kiểm kê, cổng ranh giới và ADR tách hệ Khuyến nông (không migration)

Gói chuẩn bị tách KN ([ADR-0055](../../adr/0055-tach-he-thong-khuyen-nong.md), [kiểm kê](../tach-he-thong/kiem-ke.md)). Không chuyển mã. Các lựa chọn dưới đây do nhóm phát triển đặt, chờ chủ sản phẩm (Q-KNS-01…06 ở ADR-0055).

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Module nào là "của KN" | `pf/extension`, `pf/price`, `pf/hrm`, `pf/contact` (theo yêu cầu PO), cộng không gian tên đích `internal/kn` | `tools/knboundary/boundary.yaml` `kn_modules`; `knModules` trong kiểm thử | Q-KNS-05: nếu `pf/contact`/`pf/price` là năng lực dùng chung thì bỏ khỏi `kn_modules`; baseline tự co (chạy `-prune`) |
| Module nào là "dữ liệu HTX" | `pf/cooperative`, `pf/cropcycle`, `pf/mechanization` (D2: dữ liệu canh tác, máy) | `htx_modules`; `htxModules` trong kiểm thử | Thêm module khi có dữ liệu HTX mới |
| Gốc lắp ráp được import KN | `cmd/api-platform`, `cmd/worker`, `cmd/formimport`, `cmd/seed-synthetic` (giai đoạn logic dùng chung binary) | `composition_roots` | KNSPLIT-5: chỉ `cmd/api-kn`, `cmd/worker-kn` |
| Phạm vi quét dấu KN | Tên tệp hoặc khai báo cấp gói có `KN`/`CNTS`/`KHTC` trong `internal/`, `lib/` (không gồm kiểm thử), cộng danh sách tay `kn_inventory_files` (15 tệp không mang dấu tên: menu, phòng ban, đơn vị nội bộ, vô hiệu, loại thông báo, `extensionsrc`) | `symbol_marker`, `file_marker`, `kn_inventory_files` | Thêm tệp vào danh sách tay khi phát hiện; danh sách chỉ co lại cùng baseline |
| Route bề mặt KN | Tiền tố `/v1/kn/`, `/v1/extension/`, `/v1/price/`, `/v1/hrm/`, `/v1/contacts`, `/v1/support-requests` (thao tác không tag tính là của KN); bề mặt dùng chung: menu, ngữ cảnh phiên, ủy quyền, đơn vị nội bộ, phòng ban, cán bộ KN, phiên hỗ trợ | `kn_route_prefixes`, `kn_shared_route_prefixes` | Đổi khi PO xác định phòng ban, ủy quyền là của nền tảng |
| Vùng web KN | `src/kn/`, `src/app/kn/`, mock `src/api/mock/kn-*`; KN được dùng `api`, `apiclient`, `auth`, `components`, `lib`, `shell`, `nav`, `config` | `web` | KNSPLIT-3 đổi sang `apps/web-kn` |
| Mục baseline từ migration cũ | Giữ (vĩnh viễn: migration không sửa được) — 000114 (`ag_ops_jobs` đọc cột `pf_extension`), 000124 (dựng view `pf_extension_pub`) | `baseline.yaml` | — |
| "Chủ thể KN" ở mức DB | (1) vai nhóm module KN; (2) phiên người dùng KN trên route KN do module HTX phục vụ (`r_cooperative`, Nút KN, mục đích `khuyen-nong`) | `migrations/kn_split_boundary_integration_test.go` | Thêm vai `r_kn_*` khi KNSPLIT-2 tạo module mới |
| Ca hỏng vì tenant lưu ký | Đánh dấu XFAIL (vẫn chạy, ghi lý do và gói gỡ KNSPLIT-4; hết vi phạm → XPASS làm hỏng kiểm thử) — 5 ca: đọc bảng HTX ngoài cấp phép, đọc ngoài phạm vi cấp phép, ghi sổ HTX, thu hồi không cắt, hết hạn không cắt | như trên | KNSPLIT-4 đổi thành khẳng định cứng |
| Số ADR | 0055 (số trống lớn nhất + 1; 0040 "hai hệ thống thông tin" vẫn giữ chỗ cho v4.3 ADR-22) | `docs/adr/README.md` | Đổi số nếu gói song song lấy 0055 trước (`docscheck`) |
