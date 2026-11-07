# PRICE-V2-WEEKLY-PROGRAM

FN-TI-02.13 Báo giá v2.0: hai báo cáo tuần theo chương trình, chốt/chỉ xem, cộng dồn và nhập tay thay thế kinh phí lũy kế, điều chỉnh quy mô, nhóm/con/thêm/xóa, Excel partial rows/template/export, quyền Tỉnh TTTT và Admin hỗ trợ.

1. Dependency/cấu trúc: dùng clock/calendar/decimal/excelize/ledger và port session hiện hữu, không thêm dependency.
2. Data: migration mới `pf_price_weekly_program`, RLS theo tenant và ledger append-only; catalog kế hoạch chính thức, tombstone giữ lịch sử chốt bất biến.
3. Service: arithmetic, tuần đầu/tuần kế, CAS/idempotency/audit, từng dòng Excel; parse/render ngoài DB transaction.
4. API: `/v1/price/weekly-programs/{information|training}` và actions/imports/export/template; quyền kiểm trước idempotency.
5. UI: `/kn/chuong-trinh-khuyen-nong/thong-tin`, `/kn/chuong-trinh-khuyen-nong/dao-tao-hop-tac`, menu Tỉnh TTTT, bảng hai tầng, lỗi theo ô, dialog có xác nhận/hủy, tuần cũ chỉ xem.

Kiểm: native `TestWeekly*284` phủ AC01–07, decimal guard, closed-state direct writes, tenant/support/RLS, concurrent CAS một người thắng, override sang tuần sau, partial import/blank/ignore cumulative/error export và OOXML. Chạy hồi quy cả price, các consumer session/menu/assembly và migrations trên pg-ephemeral riêng; đủ static gates, full web và production browser. Bằng chứng kiểm chi tiết, controlled fault và manifest source/artifact được giữ theo gói trong packet điều phối.

Chạy lại từ gốc worktree với toolchain trong go.mod:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./internal/kn/extension/menu/... ./internal/platform/party/domain/... ./internal/platform/party/handler/... ./cmd/api-platform/assembly/... ./migrations/...
```

Migration dùng runner chuẩn của repo (`go run ./cmd/migrate --help` để chọn profile/DSN của môi trường được cấp); không áp vào DB dùng chung khi kiểm gói. Web: trong `apps/web-ops`, `npm ci`, `npm run gen:api`, `npm run check:api`, `npm run lint`, `npm run typecheck`, `npm test`, `npm run build:mock`. Chạy API bằng lắp ráp `cmd/api-platform` hiện có; production web bằng `npm start` sau build và ngữ cảnh Tỉnh TTTT/hỗ trợ.

Điều phối local preparation: giữ barrier actual dev #287/#286/#261/#262 và ROOT release trước whole CI/public self review/PR/merge. Sau release chạy Linux race/whole CI trên SHA cuối, đồng bộ surface rows và regenerate từ dev thật.
