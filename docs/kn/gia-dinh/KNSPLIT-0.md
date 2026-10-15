## KNSPLIT-0 — Kiểm kê, cổng ranh giới và ADR tách hệ Khuyến nông (không migration mới)

Nguồn: CODEX-G1 mục 5–6 và trả lời PO 14–15/10/2026; [ADR-0055](../../adr/0055-tach-he-thong-khuyen-nong.md), [kiểm kê](../tach-he-thong/kiem-ke.md). Gói này chưa chuyển mã nghiệp vụ/tenant và chưa đóng G1.

| Mục | Quyết định / cách triển khai | Bằng chứng / gói tiếp |
|---|---|---|
| Q-KNS-01/02 | Hệ KN độc lập logic; trao đổi hệ khác chỉ qua nền tảng dữ liệu, không import port HTX/MKG; vật lý sau G1 | kn-import-htx, ADR-0055 |
| Q-KNS-03 | Mỗi Trung tâm tỉnh một tenant; chuyển dữ liệu KN cũ forward-only trong G1, có IDmap/bút toán/chạy thử/đối chiếu/rollback | KNSPLIT-4b |
| Q-KNS-04 | KN không ghi HTX; dữ liệu KN tạo về HTX là bản riêng KN. Không cấp đường ghi gián tiếp | kn-write-htx; D2-KN-01 trong ADR là đề xuất riêng chờ PO, chưa sửa D2 |
| Q-KNS-05 | extension, price, hrm, contact thuộc KN; giữ tên module/vai/schema vật lý | namespace KNSPLIT-1, ADR-0049/0054 |
| Q-KNS-06 | MRV thuộc HTX/MKG, không chuyển vào tenant KN | Phân loại chuyển 4b, client/role gói 3 |
| Client và frontend | G1 client kn-web ở realm agrigreen + runbook import. Web riêng/mobile KN riêng sau G1 | KNSPLIT-3 tối thiểu |
| Baseline ban đầu | Sinh từ dev `988febd9` sau W11.46:414 ghép nối,10 quy tắc (một quy tắc hiện0). Sau tích hợp chỉ giảm | tools/knboundary/baseline.yaml, CI -base |
| Đổi namespace cơ học | Bốn alias canonical_paths cố định giữ ID ghép nối trước/sau chuyển thư mục; không thay phân loại quyền/import, không làm mất import mới. Alias không đổi được so với nhánh đích; ref đích sai làm cổng lỗi | KNSPLIT-GATE-001/002/003 |
| Ba tệp dở khi tiếp nhận | Đã lưu patch và commit nguyên vẹn trước rebase; giữ quy tắc kn-write-htx | boundary.yaml/check.go/knboundary_test.go |
| Năm vi phạm tenant lưu ký | XFAIL vẫn chạy thật, ghi rõ vi phạm;4b phải chuyển thành PASS và fail-on-skip, không bỏ test | migrations/kn_split_boundary_integration_test.go |
| Ghép nối SQL mới phát hiện | cntsalert/app/lifecycle.go nhắc pf_forms.period trong tìm tenant job; phải chuyển sang cổng chung/gốc lắp ráp, không mở thêm quyền KN | kn-sql-foreign=1, KNSPLIT-2 |

Mẫu phiếu KHTC nạp sẵn đủ G1 (KH-UR-AD-02), M1–M7 đã chốt; không hỏi lại. Đề xuất D2 đã có bước KN đề nghị, MKG duyệt và tạo, tác động và phương án thay thế tại mục Ảnh hưởng ADR-0055; chưa triển khai phần phụ thuộc khi chưa có trả lời PO. Kết quả kiểm thử/cổng ghi tại [sổ hồi quy](../../REGRESSION.md).
