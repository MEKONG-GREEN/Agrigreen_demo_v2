## BF-CI-G5-KEY-SECRET-ORACLE — Kiểm nguyên payload bí mật API key

Gói [#231](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/231) sửa oracle trong
[kiểm thử G5](../../../apps/web-ops/src/apiclient/g5.test.tsx). Đây là việc kiểm chứng hợp đồng kỹ thuật
API key hiện hữu, không tạo hoặc thay đổi Function nghiệp vụ trong BRD 2.0.

### Thay đổi theo trình tự triển khai

| Bước | Tệp/phạm vi | Hợp đồng được kiểm |
|---|---|---|
| 1 — Dependency/cấu trúc | Giữ dependency và cấu trúc hiện hữu | Vitest/jsdom và Web Crypto sẵn có; không thêm thư viện |
| 2 — Data | Không đổi schema, model, migration hoặc dữ liệu | Snapshot, sessionStorage và danh sách khóa không chứa payload bí mật đầy đủ |
| 3 — Core | `apps/web-ops/src/apiclient/g5.test.tsx`: fixture cục bộ | Hai mảng 32 byte tổng hợp tạo secret base64url có `_` ở giữa/gần cuối; phục hồi RNG trong `finally` |
| 4 — API | Cùng tệp kiểm thử; API/runtime/mock không đổi | Định dạng `agk_<prefix12>_<secret43>`, secret chỉ xuất hiện ở 201 đầu; replay cùng khóa idempotency không trả secret; giới hạn hai khóa vẫn trả 409 |
| 5 — UI/kiểm chứng | Cùng tệp kiểm thử; không đổi component | Cắt đúng prefix đã xác minh, kiểm đủ 43 ký tự và ba nguồn không lưu bí mật; assertion lỗi chỉ hiện boolean/độ dài |

Base64url cho phép `_` bên trong secret nên `split("_").pop()` không xác định được payload.
Oracle lấy phần sau `agk_${first.key_prefix}_` đã được kiểm định dạng/prefix, giữ nguyên mọi kiểm tra
secret-once, replay, non-persistence và giới hạn khóa. Fixture chỉ thay lần rút mảng `Uint8Array(32)`
trong từng test, các độ dài khác chuyển tới RNG gốc; kiểm đúng một lần sinh secret. Không ghi/in key,
suffix hoặc snapshot vào log. Không đổi entropy/storage sản phẩm, timeout, workflow hay baseline.

### Kiểm chứng và cách chạy

Phép làm sai có kiểm soát thay duy nhất extraction đầy đủ bằng oracle `split` cũ: cả hai ca xác định
phải FAIL. Phục hồi nguyên byte, chạy đối chứng và toàn bộ G5; các test khác bị lọc trong lượt làm sai
chỉ là diagnostic, không tính thay cho bộ kiểm thử đầy đủ. Lỗi đồng hồ độc lập thuộc #187.

Từ `apps/web-ops`:

```powershell
npm ci
npx vitest run src/apiclient/g5.test.tsx
npm run typecheck
npm run lint
npm run check:api
npm test
npm run build:mock
npm run dev:mock
```

Từ gốc worktree, kiểm backend liên quan trên PostgreSQL tạm riêng:

```powershell
$env:GOTOOLCHAIN = (Select-String go.mod '^toolchain\s+(\S+)').Matches[0].Groups[1].Value
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/platform/apiclient/... ./internal/platform/metering/... ./migrations
```

Chỉ tự review/gộp sau đủ cổng Go, full web, PG thật 0 SKIP, mutation/restore và CI đúng SHA xanh.
Chỉ tích hợp bằng `ticket.ps1 merge`; gộp #231 trước lượt kiểm cuối của #187 do cùng tệp test.
