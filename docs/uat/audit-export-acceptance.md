# Nghiệm thu kỹ thuật audit export — lát codec #83

FN/UR/BR/AC nghiệp vụ: **N/A — hợp đồng kỹ thuật #83/#82**. Nguồn export production chưa đăng ký. Phần authority/worker/receipt/Excel/DLP/UI nằm ở [#259](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/259). Không dùng các ca fixture dưới đây làm chứng cứ hoàn thành chức năng xuất Excel hoặc #64.

| Kiểm tra | Kết quả yêu cầu | Bằng chứng chạy lại |
|---|---|---|
| JSON/schema/bounds | Object <=8 KiB; chỉ 7 field client; chặn khóa trùng/lạ/null/trailing, UUID/mã/nguồn lỗi; khoảng <=93 ngày | `TestExportFilterStrictBounds83`, `TestExportFilterSnapshotSchema83` |
| Canonical/replay | UTC/microsecond, source sắp xếp, tương đương timezone/thứ tự khóa có cùng bytes; as_of từ server | `TestExportFilterCanonicalReplay83` |
| Private binding | Từ chối job/tenant/requestor/source/template/format/version sai, metadata thiếu và Scope.All/units giả | `TestExportFilterBindingsAndTampering83`, `TestExportFilterSnapshotSchema83` |
| PostgreSQL #82 | JSONB/timestamptz roundtrip dưới +07; replay/collision/invalid không tạo thêm job/outbox, không đổi snapshot | `TestAuditExportFilterPrivateRoundtrip83` |
| Privacy/fail closed | Create/list/detail DTO và outbox không chứa args; trigger chặn sửa snapshot, codec chặn job binding khác, policy bị gỡ từ chối; lỗi không echo input | `TestAuditExportFilterPrivateRoundtrip83` |
| Staging | Catalog/builder production không đăng ký nguồn codec khi chưa có authority thật | `TestAuditExportSourceRemainsUnregistered83` |
| Audit hiện hữu | Viewer/receipt/tenant/retention vẫn đạt trên PG thật | Các test AUDIT73 trong `tests/knaudit` |
| Controlled fault | Bỏ job ID comparison làm test binding FAIL; khôi phục byte gốc rồi PASS | `TestExportFilterBindingsAndTampering83` |

Authentication và authorizer của test roundtrip là test double; builder cố ý trả lỗi, test xác nhận không chạy builder. Không có proof quyền hiện hành hoặc receipt mới trong lát này. Không có field số nghiệp vụ mới; ca exponent `1e100000`, `1e-100000` và chuỗi 100.000 ký tự phải bị chặn trước parse.

## Cách chạy

Áp dụng toolchain trong `go.mod` và môi trường phát triển. Chạy DB chỉ bằng cluster riêng:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/knaudit ./tests/knaudit ./internal/platform/reports/... ./cmd/api-platform/assembly
```

Kết quả bắt buộc có `--- PASS`, không SKIP. Chạy static/lint/merge-tree và whole CI đúng SHA theo [hướng dẫn gói](../kn/gia-dinh/AUDIT-EXPORT-KN.md#cách-chạy-và-kiểm); native golden bytes không đổi. Lát này không có migration hoặc API/UI mới để khởi động.
