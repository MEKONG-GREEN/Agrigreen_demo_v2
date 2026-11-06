## CI-G5-CLOCK — Đồng hồ kiểm thử bảng kê G5

### Phạm vi

Gói [#187](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/187) chỉ sửa [kiểm thử G5](../../../apps/web-ops/src/apiclient/g5.test.tsx).
Quy tắc kỳ tháng theo Việt Nam, API, mock, dữ liệu, quyền duyệt và CSV không đổi; không tạo Function nghiệp vụ mới.
Date được cố định trước khi reset/seed mock và được phục hồi ở afterEach, kể cả test lỗi.
Chỉ giả lập Date; timer bất đồng bộ và các timeout/worker giữ nguyên.

### Hợp đồng kiểm thử

Dùng kỳ đang mở theo Việt Nam và kiểm độc lập kỳ đang mở/kỳ vừa kết thúc ở tám mốc:
trước/đúng ranh giới tháng VN; khoảng tháng UTC khác tháng VN; sau ranh giới tháng UTC;
trước/đúng ranh giới năm VN; khoảng năm UTC khác năm VN; sau ranh giới năm UTC.
Mỗi mốc giữ đủ 409 kỳ đang mở, HTTP 202/job hoàn tất, người yêu cầu không tự duyệt,
bảng kê đã duyệt đóng băng, CSV/hash/verify tất định và sổ xuất.
Phép làm sai đổi lựa chọn kỳ về tháng UTC phải làm flow FAIL; sau đó phục hồi nguyên byte và chạy đối chứng.
Kiểm cả UTC và Asia/Bangkok; không đổi logic runtime, assertion theo hướng nới lỏng, timeout, workflow, dependency hoặc baseline.
Oracle bí mật base64url thuộc #231, không thay trong gói này.

### Cách chạy và kiểm

Từ gốc repo, kiểm backend liên quan qua PostgreSQL tạm riêng:

```powershell
$env:GOTOOLCHAIN = (Select-String go.mod '^toolchain\s+(\S+)').Matches[0].Groups[1].Value
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/platform/apiclient/... ./internal/platform/metering/... ./migrations
```

Từ `apps/web-ops`, `npm ci`, `npm run dev:mock` để mở web; `npx vitest run src/apiclient/g5.test.tsx`
để chạy đủ file G5. Kiểm gói còn gồm check:api, lint, tsc, Vitest đầy đủ, build:mock, cổng Go và CI đúng SHA.
