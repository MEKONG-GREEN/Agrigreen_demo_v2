# Hợp đồng bộ điều hợp phần mềm kế toán (`mkg/acctbridge`)

Tài liệu cho người viết bộ điều hợp tới phần mềm kế toán pháp định của Mekong Green và cho vận hành. Căn cứ: tài liệu kiến trúc v3.0 mục 16 (`mkg/acctbridge`: **chỉ đẩy bút toán tổng hợp theo kỳ, một chiều, tần suất tháng**, không đẩy giao dịch chi tiết), R-62 (DLQ + cảnh báo), RB-7 (lưu trữ trong lãnh thổ), RB-4 (giấy phép); [mkg.md §5.7](../design/mkg.md#57-mkgacctbridge--bút-toán-tổng-hợp--kế-toán-pháp-định); [g4-conventions.md](g4-conventions.md) §1, §1.2, §6.

**Nhà cung cấp chưa chốt** (MISA, FAST, Bravo… — câu hỏi khách Q-1 ở [g4-acceptance.md](../uat/g4-acceptance.md)). G4 có ba bộ điều hợp: **xuất tệp** (mặc định), **giả** (kiểm thử, dev) và **chưa cấu hình**. Bộ điều hợp của nhà cung cấp thật thêm sau, **không đổi interface**.

## 1. Luồng

```
mkg/costing đóng kỳ ──mkg.costing.period.closed──▶ acctbridge: job dựng (mẫu mục 12)
   job chốt: kỳ, lần đóng kỳ (close_run_id), result_set_sha256, phiên bản ánh xạ đã duyệt hiệu lực đến cuối kỳ
worker: costing/port.ClosedPeriods → PeriodSummary → kiểm lần đóng kỳ + băm → dựng tất định → sổ journal_event 'built'
chief_accountant duyệt (≠ người dựng, R-56 app/ + CHECK DB) → job đẩy lần 1
worker: LedgerAdapter.Push(SummaryJournal) ─┬─ thành công → 'pushed' + mkg.acctbridge.journal.pushed (costing: kỳ → posted)
                                            ├─ ErrAdapterUnavailable → hẹn thử lại 1, 2, 4… phút (trần 1 giờ), tối đa retry_max lần
                                            └─ ErrAdapterRejected / hết lượt → push_failed (DLQ của module) + cảnh báo
```

Người dựng: bút toán tự dựng khi đóng kỳ có người dựng = **người đóng kỳ** (`ClosedBy` của sự kiện); dựng lại tay = người yêu cầu (`accounting_bridge_operator`). Người đó không duyệt được bút toán.

Sửa sai: **trước khi đẩy**, bản dựng lại (ánh xạ mới, lần đóng kỳ mới) supersedes bản cũ — bản cũ giữ nguyên với trạng thái `superseded`. **Sau khi đẩy**, bút toán không bao giờ sửa (app/ trả `acctbridge.journal_pushed_immutable`; trigger DB chặn mọi UPDATE dòng `pushed`); sai sót được sửa bằng **bút toán điều chỉnh của costing ở kỳ sau** — dòng tổng hợp âm, acctbridge ghi đảo bên và bù vào dòng cùng tài khoản.

## 2. Interface (đóng băng ở G4-0)

`internal/mkg/acctbridge/port`:

```go
type LedgerAdapter interface {
    Name() string                                            // tên + phiên bản, vd "file_export.v1"
    Push(ctx context.Context, j SummaryJournal) (ExternalRef, error)
}
```

Bộ điều hợp **phải**:

1. **Idempotent theo `j.IdempotencyKey`** (`acctbridge:<journal_id>`, không phụ thuộc lần thử): đẩy lại bút toán đích đã nhận trả **cùng** `ExternalRef.Ref`, không ghi trùng. Worker gọi `Push` trong giao dịch DB; nếu giao dịch hỏng sau khi đích đã nhận, hàng đợi giao lại và `Push` được gọi lần nữa với cùng khóa. Đích không hỗ trợ khóa idempotency → bộ điều hợp tự tra trước (theo số chứng từ tất định, vd `<period_code>-<journal_id>`) rồi mới ghi.
2. Phân loại lỗi đúng:
   - `ErrAdapterUnavailable` (bọc bằng `fmt.Errorf("%w: …")`): mạng, bảo trì, hết hạn token, 5xx, quá thời gian — **tạm thời**, được thử lại. Lỗi lạ không bọc cũng bị coi là tạm thời.
   - `ErrAdapterRejected`: tài khoản không tồn tại bên đích, kỳ đích đã khóa, sai định dạng, bút toán không cân — **vĩnh viễn**, không tự thử lại.
3. Không ghi log nội dung bút toán (số tiền, mã đối tượng), không đưa dữ liệu ra ngoài lãnh thổ (RB-7). Mã chiều (`Dimension`) là `<loại>:<uuid>` — không có dữ liệu cá nhân; ánh xạ sang mã đối tượng công nợ bên đích là việc của bộ điều hợp (bảng tra riêng, cũng trong lãnh thổ).
4. Tôn trọng `ctx` (worker đặt hạn 30 giây mỗi lần gọi).
5. Trả `ExternalRef{Adapter, Ref, ArtifactSHA256, AcceptedAt}`: `Ref` = mã chứng từ bên đích (hoặc khóa tệp); `ArtifactSHA256` chỉ cho bộ điều hợp tệp.

`SummaryJournal` đã được kiểm trước khi gọi: Σ Nợ = Σ Có > 0, mọi dòng > 0, tiền tệ `VND`, `SourceHash` = `result_set_sha256` của kỳ costing, `ApprovedBy` ≠ `BuiltBy`.

Bộ điều hợp mới: thêm kiểu trong `internal/mkg/acctbridge/adapter`, một nhánh trong `acctbridge.AdapterFromEnv`, kiểm thử hợp đồng như `adapter_test.go` (idempotent, phân loại lỗi), kiểm giấy phép SDK/thư viện của nhà cung cấp (RB-4), không import `internal/platform/*` (R-03), không float (R-10, `agnofloat`).

## 3. Bộ xuất tệp (`file_export.v1`, mặc định)

Ghi ba đối tượng **ghi-một-lần** vào kho đối tượng trong lãnh thổ (`shared/objstore`: S3 trong lãnh thổ ở production, thư mục cục bộ ở dev):

```
<prefix>/<tenant_id>/<period_code>_<journal_id>.csv
<prefix>/<tenant_id>/<period_code>_<journal_id>.xml
<prefix>/<tenant_id>/<period_code>_<journal_id>.sha256     # tệp ký (định dạng sha256sum), ghi CUỐI
```

`ExternalRef.Ref` = `<prefix>/<tenant_id>/<period_code>_<journal_id>`; `ArtifactSHA256` = SHA-256 của tệp `.sha256`. Nội dung **tất định byte-cho-byte** theo bút toán (không thời điểm xuất, không người duyệt) nên đẩy lại ghi đúng các byte cũ (kho trả đối tượng đã có); cùng khóa khác nội dung → `ErrAdapterRejected`; kho lỗi → `ErrAdapterUnavailable`. Kế toán tải tệp (API `GET /v1/acctbridge/journals/{id}/export?format=csv|xml`, cùng byte, header `X-Content-SHA256`) rồi nhập vào phần mềm kế toán tới khi có bộ điều hợp của nhà cung cấp.

### 3.1 CSV (`acctbridge.export.v1`)

UTF-8 **không BOM**, dấu phân cách `;`, xuống dòng **CRLF**, số tiền 2 chữ số thập phân dấu chấm, không phân cách hàng nghìn; trường chứa `;`, `"` hoặc xuống dòng được bao `"…"` (nhân đôi `"`). Một dòng tiêu đề, một dòng mỗi dòng bút toán:

| Cột | Nội dung |
|---|---|
| `schema` | `acctbridge.export.v1` |
| `journal_id`, `period_id`, `period_code`, `close_run_id` | định danh bút toán, kỳ (`YYYY-MM`), lần đóng kỳ |
| `currency` | `VND` |
| `source_hash` | `result_set_sha256` của kỳ costing |
| `mapping_version` | phiên bản bảng ánh xạ tài khoản đã dùng |
| `line_no`, `account`, `side` | số dòng, số hiệu tài khoản, `debit` / `credit` |
| `debit`, `credit` | số tiền ở cột theo bên, cột kia rỗng |
| `category`, `dimension`, `description` | nhóm chi phí/doanh thu của costing, mã chiều, diễn giải |

Thứ tự dòng: Nợ trước Có, rồi theo tài khoản, nhóm, chiều.

### 3.2 XML

UTF-8, xuống dòng LF, thuộc tính theo thứ tự cố định, namespace `urn:agrigreen:mkg:acctbridge:summary-journal:v1`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<SummaryJournal xmlns="…" schema="acctbridge.export.v1" journalId="…" periodId="…" periodCode="2026-09" closeRunId="…"
                currency="VND" sourceHash="…" mappingVersion="1" totalDebit="…" totalCredit="…" lineCount="13">
  <Line no="1" account="131" side="debit" amount="310000000.00" category="sales_revenue" dimension="customer:…">Tổng hợp …</Line>
</SummaryJournal>
```

Mẫu đầy đủ: `internal/mkg/acctbridge/domain/testdata/journal_2026-09.{csv,xml}` (golden của `TestExportGolden_B06`). Đổi định dạng = phiên bản mới (`acctbridge.export.v2`) + tên bộ điều hợp mới, không sửa v1.

## 4. Bảng ánh xạ tài khoản (tạm, chờ khách)

Quy tắc `(category, dimension_kind) → debit_account / credit_account`, `dimension_on` ∈ `none | debit | credit` (tài khoản nào mang mã chiều, vd công nợ HTX ở bên Có 331); `dimension_kind = "*"` là dự phòng của nhóm. Có phiên bản: `accounting_bridge_operator` đề xuất (`POST /v1/acctbridge/mappings`, bề mặt quản trị), `chief_accountant` **khác** duyệt; hiệu lực theo `effective_from` so với **ngày cuối kỳ**. Chưa có phiên bản được duyệt → job dựng lỗi `acctbridge.mapping_missing`.

Mặc định (đề xuất không kèm `rules`, `provisional = true`) theo hệ thống tài khoản doanh nghiệp VAS kiểu Thông tư 200/2014/TT-BTC, cấp 1 — **chờ kế toán trưởng xác nhận chế độ kế toán đang áp dụng và tiết khoản**:

| Nhóm (costing) | Nợ | Có | Chiều |
|---|---|---|---|
| `straw_purchase` | 152 | 331 | Có (HTX) |
| `collection_labour` | 622 | 334 | — |
| `consumables` | 627 | 152 | — |
| `transport_road`, `transport_waterway`, `hub_processing`, `storage`, `handling` | 627 | 331 | — |
| `loss` | 632 | 154 | — |
| `sales_revenue` | 131 | 511 | Nợ (khách) |
| `carbon_revenue` | 131 | 511 | — |
| `carbon_share` | 632 | 338 | — |

Phải trả là nợ phải trả (331/338), không cộng vào chi phí lần hai (proto D03).

## 5. Cấu hình

| Biến | Ý nghĩa |
|---|---|
| `AG_ACCTBRIDGE_ADAPTER` | `file_export` (mặc định) \| `fake` (chỉ dev/kiểm thử — nhận giả, **không** dùng ở môi trường thật). Giá trị lạ → `unconfigured` |
| `AG_ACCTBRIDGE_EXPORT_PREFIX` | tiền tố khóa đối tượng (mặc định `acctbridge`) |
| `AG_S3_*` / `AG_OBJSTORE_DIR` | kho đối tượng (`shared/objstore.FromEnv`). Thiếu → `unconfigured`: luôn tạm không nhận, bút toán đã duyệt được giữ, thử lại rồi vào DLQ + cảnh báo |
| `AG_ACCTBRIDGE_PUSH_RETRY_MAX` | `acctbridge.push.retry_max` (mặc định 8 — tạm, chờ khách) |
| `AG_MKG_JOBS` (api-internal) | gồm `acctbridge_push_retry` (mỗi phút, chỉ leader) để thử lại theo lịch |

Consumer ở `cmd/worker-internal`: `mkg.acctbridge.period_closed`, `mkg.acctbridge.journal_build`, `mkg.acctbridge.journal_push` (vai `acctbridge_app`). `costing/port.ClosedPeriods` nối ở `cmd/worker-internal/assembly` khi `mkg/costing` có mặt (người tích hợp G4).

## 6. Vận hành

Cảnh báo: `deploy/observability/prometheus/agrigreen-mkg-acctbridge.rules.yml`.

| Cảnh báo | Làm gì |
|---|---|
| `AgrigreenAcctbridgePushDeadLettered` | `GET /v1/acctbridge/journals?status=push_failed`, xem `push_attempts` + `last_failure_code`. `adapter_rejected`: sửa nguyên nhân bên đích (mở tài khoản, mở kỳ) hoặc đề xuất/duyệt ánh xạ mới rồi `POST /v1/acctbridge/periods/{period_id}/journal-builds` (bản mới supersedes, cần duyệt lại). `retry_exhausted`: khôi phục đích/kho rồi `POST /v1/acctbridge/journals/{id}/retry-push` |
| `AgrigreenAcctbridgePushRetrying` | đích tạm không nhận nhiều lần — kiểm kho đối tượng / kết nối; hệ thống tự thử lại |
| `AgrigreenAcctbridgeBuildFailed` | `mapping_missing`: duyệt bảng ánh xạ rồi yêu cầu dựng lại; `source_mismatch`: đối chiếu lần đóng kỳ với mkg/costing |
| DLQ RabbitMQ `mkg.acctbridge.*.dlq` | lỗi hạ tầng của worker (DB, costing chưa nối) — cảnh báo chung vhost `mkg.internal`; sửa rồi chuyển thông điệp về hàng đợi gốc ([rabbitmq-queues.md](../runbooks/rabbitmq-queues.md)) |

Không bao giờ xóa `push_attempt`, `journal_event` hay bút toán `push_failed`. Dựng lại bảng chiếu (diễn tập R-22): `POST /v1/acctbridge/projections/rebuild` (chief_accountant, bề mặt quản trị).

## 7. Câu hỏi cho khách (chặn bộ điều hợp thật)

1. Nhà cung cấp và phiên bản phần mềm kế toán; nhà cung cấp có hạ tầng + pháp nhân trong lãnh thổ (RB-7); giấy phép API/SDK (RB-4).
2. Chế độ kế toán áp dụng và hệ thống tài khoản, tiết khoản; mã đối tượng công nợ HTX / khách bên đích.
3. Định dạng nhận (API, tệp nhập Excel/XML theo mẫu của phần mềm), khóa idempotency / số chứng từ bên đích, cách khóa kỳ bên đích.
4. Tần suất và hạn đẩy (tháng — mục 16), ai nhập tệp khi dùng bộ xuất tệp.
