## HCTH-V4-PROMOTION-REGULAR — HC-3: nâng lương thường xuyên (không có migration)

Gói #278, nguồn duy nhất là Function chi tiết và bảng trường của
[BRD HCTH v4.0](https://docs.google.com/document/d/1r0TqvDU8yRZSdm5BwabvWkgElm8ynC7Q/edit).
Áp dụng FN-09, UR-HC-12/13, BR-01..07, AC-01..05, NL02-01..15; seam NB-06/NB-07 của FN-07.
Function FN-09 BR-06 thắng ghi chú ngày chốt chung: tính từ hồ sơ hiện hành khi mở màn;
năm/tháng chỉ lọc ngày đến kỳ, không chiếu hồ sơ về quá khứ hay tương lai.

### Hiện thực theo bước 1–5

1. Không thêm dependency. Giữ module HRM, tách trang xuất `nang-bac/xuat` khỏi màn danh sách. Lựa chọn chỉ ở React layout trong bộ nhớ tab; không đưa casekeys vào URL/storage. Tenant/subject/contextID/readonly/support đổi thì remount để xóa lựa chọn; xuất kiểm lại contextStore trước POST, bỏ lựa chọn khi thành công/hủy/rời màn. Reload thiếu state báo quay lại chọn. Batch500 vẫn dùng URL ngắn và gửi đủ keys trong body.
2. Không DDL, backfill, thay migration hoặc sửa event cũ. NB-07 là `promotion_months` tùy chọn
   trong payload EventGrade/EventStep và Snapshot hiện có. Kiểm cú pháp số nguyên tối đa 3 chữ số,
   phạm vi 1..120 trước parse; không dùng HoldMonths của ngạch thay dữ liệu cá nhân.
   Khi event bậc không ghi NB-07 thì giữ giá trị cá nhân trước đó; sửa/hủy vẫn dùng lịch sử append-only.
3. Compute chỉ tạo NLTX, ngày đến kỳ = NB-06 + NB-07 (ngày vượt cuối tháng chặn tại cuối tháng).
   Không truy vấn rule promotion/achievement_conversion hoặc xếp loại để tải FN-09;
   không dùng kỷ luật, thành tích, quota hoặc điều kiện xếp loại. Bậc cuối bị loại;
   hồ sơ thiếu dữ liệu xuất hiện trong cảnh báo tên. Job nhắc hiện hữu đọc tham số nhắc riêng,
   không đặt điều kiện cho màn tương tác.
4. API lọc năm 1..9999, tháng, ba trạng thái, đơn vị, họ tên; không nhận ngày chốt làm mốc chiếu.
   Gỡ include/exclude, nlth-pool, proposals/withdraw ở routing, handler, service, contract và mock.
   Xuất trực tiếp chỉ Mẫu1, đủ20 cột và cột19 trống; hỗ trợ tháng xét, người lập/ký, HS bảo lưu,
   số tháng hưởng và ghi chú. Số mũ/chuỗi dài bị chặn trước parse ở server và UI.
5. Danh sách dùng DataTable/NumberField, chỉ có Chưa xử lý, Chờ quyết định, Đã có quyết định;
   không khối chỉ số, biểu đồ, quota hay danh sách trước hạn. Trang chi tiết hiện NB06/NB07 và lịch sử.
   Chọn người → trang xuất → xem trước → tải Mẫu1; useAction giữ khóa idempotency khi thử lại.
   Dashboard FN-06 tiêu thụ ngày thường xuyên hiện hành, không tạo cảnh báo điều kiện xếp loại.

### Bảo toàn và seam cho gói sau

- Listed/include cũ ánh xạ Chưa xử lý; export cũ ánh xạ Chờ quyết định, không ghi lại sổ.
  Return cũ vẫn đọc được; luồng lưu quyết định mới chỉ đổi người có tên, không ghi Return cho người vắng.
- FN-10 vẫn giữ cả NLTX/NLTH/TNVK, nhập độc lập hiện hữu, sổ, tệp đính kèm và hủy.
  Quyết định thường xuyên không qua xuất vẫn hiện Đã có quyết định trong FN-09.
- Giữ decoder/renderer và ExportItem cũ cho Mẫu2/4; tải lại từ snapshot gốc,
  so hash, không chuyển đổi payload hay cập nhật chuỗi băm cũ. Không ghi PII trong sổ HRM.
- HC-4 tiếp tục NB06/NB07 bắt buộc, gợi ý theo trình độ khi tạo, nhập dữ liệu đầu và gỡ HoldMonths khỏi danh mục ngạch.
  HC-3 chỉ mở seam tùy chọn qua API và ô số của dòng Ngạch/Bậc hiện có.
- HC-5 tiếp tục toàn bộ màn/quy trình nhập quyết định độc lập và tệp .csv/.xlsx,
  cập nhật thời gian xét kỳ tới. HC-3 không chứng nhận FN-10 đầy đủ.
- Quyền chung profile.edit/export còn cần cho hồ sơ và FN-10; các thao tác FN-09 bị bỏ
  không có quyền riêng cần thu hồi. Không có migration quyền hoặc thay baseline ranh giới.

### Chạy và kiểm

Đặt GOTOOLCHAIN theo `go.mod`, GOTMPDIR/TEMP/TMP theo môi trường đã cấu hình.
Không có migration mới; chạy API theo profile KN hiện hữu và dùng tenant KN độc lập.

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/hrm/... ./cmd/hrmimport ./cmd/api-platform/assembly ./tests/hc30
cd apps/web-ops
npm ci
npm run gen:api
npm run check:api
npm run lint
npm run typecheck
npm test
npm run build:mock
npm run e2e -- tests/e2e/hrm-promotion-v4.spec.ts
```

RED trên production gốc: KHTNV/kỷ luật kéo dài và chặn NLTX, bậc cuối tự tạo TNVK,
proposal tự tạo NLTH. Test v4 xanh sau sửa. Phép làm sai có kiểm soát cộng thừa một tháng
trong Compute làm test FAIL; khôi phục đúng byte và control PASS.
Native Windows không hỗ trợ Go -race trong môi trường CGO=0 không có gcc;
Linux race/full CI đúng SHA vẫn bắt buộc trước tích hợp, sau khi barrier hạ tầng và ROOT release.
