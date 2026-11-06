# AUDIT-EXPORT-KN — lát 1: codec bộ lọc nhật ký (#83)

Lát này cung cấp codec thuần và kiểm chứng private snapshot qua backend reports của #82. **Nguồn export chưa được đăng ký trong production.** Phần quyền hiện hành, worker reader/receipt, catalog/schema/generated/mock/UI và Excel qua DLP thuộc [#259](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/259). #64 chưa hoàn thành bởi lát codec này.

Đây là hợp đồng kỹ thuật #83/#82: FN/UR/BR/AC nghiệp vụ **N/A**. Không suy ra chức năng nghiệp vụ từ prototype hoặc mô tả tổng quan. Khi thực hiện phần nghiệp vụ của gói tiếp theo, áp dụng Function chi tiết và cấu trúc dữ liệu BRD 2.0 hiện hành.

## Hợp đồng codec

- `extension.NewAuditExportFilterCodec` nhận chính `accounts/port.AuditRegistry` của chủ audit, không tạo registry hay journal mới. Constructor từ chối registry thiếu. Codec không truy cập DB, mạng, danh bạ, recorder hay cấp quyền.
- JSON đầu vào là object UTF-8 tối đa 8 KiB, chỉ có `from`, `to`, `actor_ref`, `department`, `module`, `action`, `sources`. Khóa phải đúng chữ, không trùng (kể cả escape Unicode), không null, không trailing JSON. `from/to` bắt buộc, theo khoảng `[from,to)` và dài tối đa 93 ngày; thời gian chuẩn hóa UTC/microsecond như PostgreSQL và audit API hiện hữu.
- `actor_ref`, nếu có, phải là UUID khác nil. Department/module/action dùng mẫu mã hiện hữu, không trường tự do, reason, tên hay payload. Module và mỗi source phải có trong registry; source phải thuộc module đã chọn. Tối đa 32 source, không trùng; sắp xếp khóa để canonical. Bỏ `sources` thì lấy các nguồn registry phù hợp module; gửi mảng rỗng bị từ chối.
- Server tạo `version=1`, source `kn.audit_export`, template `kn.audit_log`, format `xlsx`, tenant/job/requestor và `as_of` từ `RequestedAt`. Client không được gửi các trường metadata này. `Decode` yêu cầu các trường snapshot bắt buộc và kiểm lại đầy đủ binding với `SourceJobAccess`, không nhận `Scope.All` hoặc geographic units giả cho tenant mode.
- Replay dùng job ID, người yêu cầu và thời điểm yêu cầu ban đầu do #82 cung cấp. Thứ tự khóa JSON, múi giờ tương đương và thứ tự source không làm đổi canonical snapshot. Codec không chứng minh authority hoặc sự bất biến của DB; lớp job #82 giữ private envelope, gói tiếp theo phải cung cấp quyền hiện hành thật.
- Mọi lỗi codec chỉ trả thông báo cố định ở `source_args`; không đưa raw input, lỗi decoder, UUID bị tra hay payload vào lỗi/log. Không có trường số nghiệp vụ mới. Test vẫn thử exponent cực lớn/nhỏ và chuỗi 100.000 ký tự để chứng minh giới hạn trước parse.

## Điều kiện trước khi bật nguồn (#259)

`Principal` hiện chỉ có MFA/AuthTime, chưa cung cấp SID và expiry OIDC đã verified. Bảng context hiện hữu cũng không phải bằng chứng phiên đã thực hiện MFA. `HasOTP` chỉ là enrollment; không suy origin session bằng tương quan AuthTime, không thêm snapshot role/MFA hoặc Principal giả để lấp khoảng trống. Gói tiếp theo cần producer metadata phiên OIDC đã verified qua getter typed chỉ đọc, phối hợp owner AUTH-1 trước khi sửa authentication dùng chung.

Builder/reader phải kiểm current Admin KN/MFA/assignment và origin-session/job authority, dùng tenant tường minh, ghi biên nhận bền vào sổ hiện hữu trước khi trả mỗi trang. Owner callback có I/O phải chạy ngoài reports DB transaction: đặc biệt khảo sát `CheckExportReplay` → `checkSourceReplay` trước khi nối live Directory/current-authority. Cần test pool một kết nối và kết nối tái sử dụng. Custom scheduling tiếp tục bị từ chối cho đến khi có per-run freeze contract. Không thay retention/coverage của các nguồn bằng sự hiện diện của codec.

## Bằng chứng và giới hạn

Unit tests kiểm canonical/replay, 93 ngày, source/module allowlist, unknown/duplicate/null/trailing/schema lỗi, metadata thiếu/hỏng, binding khác job/tenant/requestor và phạm vi giả. Phép làm sai có kiểm soát bỏ so sánh job ID phải làm test binding thất bại, sau đó khôi phục đúng byte và chạy lại thành công.

`tests/knaudit/TestAuditExportFilterPrivateRoundtrip83` dùng reports HTTP, idempotency middleware và PostgreSQL/JSONB thật: snapshot đọc lại dưới timezone +07 vẫn khớp cutoff microsecond; replay không tạo thêm job/outbox; invalid/collision không đổi snapshot; public create/list/detail DTO và outbox không sao chép args. Trigger PostgreSQL chặn sửa snapshot kể cả với quản trị test; codec từ chối snapshot đã lưu khi server cung cấp job binding khác. Policy bị gỡ hoặc fixture authority từ chối đều đóng truy cập trước cache. **Authentication/authorizer trong fixture là test double**, builder luôn trả lỗi và không được chạy. Test này không chứng minh live authority, worker, receipt hoặc Excel/DLP. Test riêng giữ source/catalog production chưa đăng ký. Các test audit viewer và retention hiện hữu tiếp tục chạy trên PostgreSQL thật.

## Tệp → thứ tự triển khai → hợp đồng

| Tệp | Bước 1–5 | FN/UR/BR/AC hoặc hợp đồng |
|---|---|---|
| `internal/kn/extension/module_knaudit_export.go` | 1, 3: facade của module, không dependency mới | N/A — constructor codec kỹ thuật #83 |
| `internal/kn/extension/knaudit/export_filter.go` | 3: core codec thuần | N/A — bounded canonical/private binding #83/#82 |
| `internal/kn/extension/knaudit/export_filter_test.go` | 3: strict schema, canonical, binding, mutation oracle | N/A — hợp đồng codec |
| `tests/knaudit/export_filter_integration_test.go` | 2, 4: kiểm lưu/đọc dữ liệu và API #82 hiện hữu, không migration/API mới | N/A — JSONB/privacy/replay và staging fail closed |
| `docs/kn/gia-dinh/AUDIT-EXPORT-KN.md` | 1–5: phạm vi, điều kiện nối tiếp và cách kiểm | N/A — lát 1; bước 5 thực hiện ở #259 |
| `docs/roadmap.d/kn-2026-10/AUDIT-EXPORT-KN.md` | 1–5: mốc codec và gói còn lại | N/A — kỹ thuật |
| `docs/uat/audit-export-acceptance.md` | 2–4: nghiệm thu codec/private storage/API hiện hữu | N/A — kỹ thuật |

Bước 1 không thêm dependency; bước 2 không thay schema/migration/query; bước 4 không thêm route/OpenAPI; bước 5 không thay UI/menu/mock/generated. Native report catalog/builder golden bytes được giữ.

## Cách chạy và kiểm

Từ own worktree, đặt toolchain đúng `go.mod`, PATH và thư mục tạm theo môi trường phát triển; không dùng PostgreSQL dùng chung:

```powershell
$env:GOTOOLCHAIN = (Select-String -Path go.mod -Pattern '^toolchain\s+(\S+)').Matches[0].Groups[1].Value
$env:GOMAXPROCS = '2'
go version
go test -count=1 -p 1 -v ./internal/kn/extension/knaudit -run TestExportFilter
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/knaudit ./tests/knaudit ./internal/platform/reports/... ./cmd/api-platform/assembly
go build ./...
go vet ./...
go run ./tools/modgen -check
go run ./tools/migratecheck -base origin/dev migrations
go run ./tools/archlint ./...
go run ./tools/contractcheck
go run ./tools/knboundary -base origin/dev
go run ./tools/docscheck
go run ./tools/mfaflagcheck
golangci-lint run --new-from-rev origin/dev ./...
git merge-tree --write-tree origin/dev HEAD
```

Đòi 0 SKIP, controlled fault thất bại rồi khôi phục/PASS, native golden bytes giữ nguyên và toàn bộ CI trên đúng SHA xanh trước self-review. Linux CI thực hiện race check của Go. Không cần migrate API/web mới cho lát codec; chạy API/web thông thường không xuất hiện nguồn Excel này.
