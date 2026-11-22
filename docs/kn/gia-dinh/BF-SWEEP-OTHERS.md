## BF-SWEEP-OTHERS — phần đầu: thông tin làm việc HCTH (#117)

Ngày 28/10/2026. Phạm vi phần đầu là luồng FN-08: danh sách → chi tiết → sửa thông tin làm việc. Theo PRD R-E1/R-A2/R-A4 và UI-KIT: cập nhật chuyển khỏi popup sang `/kn/nhan-su/ho-so/thong-tin-lam-viec/[id]/sua`, có nút Lưu/Hủy, liên kết về danh sách, lịch sử và lỗi theo trường. Không đổi API, kế hoạch ghi sổ append-only hay khóa idempotency. Không có ô số hay migration mới.

Không coi toàn bộ sweep đã hoàn thành. Phần còn lại tách thành #163 BF-SWEEP-HCTH-REST, #164 BF-SWEEP-KHTC và #165 BF-SWEEP-PRICE, sau phần đầu #117. Các mẫu KHTC giữ bố cục giấy; không thay mẫu nạp sẵn, M1–M7 hoặc quyết định PO. Không có câu hỏi PO mới trong phần đầu.
### Bằng chứng kiểm

- `go build ./...`, `go vet ./...`, `go run ./tools/modgen -check`, `go run ./tools/migratecheck -base origin/dev migrations`, `go run ./tools/archlint ./...`, `go run ./tools/knboundary -base origin/dev`, `go run ./tools/docscheck`: kiểm trên phần đầu; không đổi baseline 314.
- `scripts/pg-ephemeral.ps1 run go test -v -count=1 -p 1 -timeout 55m ./internal/kn/hrm/...`: 32 ca `--- PASS`, 0 SKIP; handler 417.772 giây; cụm tạm đã dọn.
- Vitest toàn thư mục `src/kn/hrm`, tsc, eslint file đổi, `npm run check:api`, `npm run build:mock`. Test riêng luồng thông tin làm việc có ca lưu thành công, chặn lỗi theo trường trước ghi, Hủy, cấp Xã và thiếu quyền màn/hồ sơ.
- Phép làm sai: bỏ `if (bad) return` trong bản tạm khiến test `validation guard` FAIL vì `addEvent` đã bị gọi một lần. Khôi phục nguyên byte mã rồi chạy lại test đạt. Không có ô số mới nên các probe số mũ không áp dụng cho phần này.