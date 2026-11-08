## CNTS-V4-HCN09 — tên cũ → mới trong lịch sử hộ (#276)

Căn cứ duy nhất: BRD CNTS v4.0 FN-10 BR-05, HCN-02/03/09, ma trận FN-10/Mục VI; Q9 được người dùng chốt ngày 08/11/2026. Xem [bổ sung ADR-0056](../../adr/0056-ho-chan-nuoi-cnts-v4.md). Quyết định che tên của #251 được giữ như lịch sử, được bổ sung này thay ở HCN-09.

HCN-09 đọc hai tên đầy đủ từ genuine `pii_revision_ref` đã lưu và hai bản mã trong vault; chỉ SĐT vẫn che. Không migration, không sửa dữ liệu cũ hay dữ liệu đóng băng, không lấy tên hiện hành làm giá trị cũ. Mã thiếu bản sửa hoặc kho không hỗ trợ được hiển thị rõ chưa đọc được. Mỗi lần đọc kiểm phiên account hiện hành, tenant, Nút, phòng ban và phạm vi hộ trước khi gọi kho; cohort được đọc theo ma trận vẫn đọc lịch sử khi chỉ xem, còn ghi bị chặn. Không thay `CanEdit`, vai vault, scope hoặc chính sách MFA/quota.

| Tệp | Bước 1–5 | FN/BR/HCN |
|---|---|---|
| `internal/vault/identity/client/revision_resolve.go`, `app/revision_resolve.go` | 3 — hợp đồng tùy chọn và dịch vụ Resolve bản sửa; data sẵn có, không thay schema | FN-10 BR-05, HCN-02/09 |
| `internal/kn/extension/cntslivestock/app/history.go`, `app/households.go` | 3 — ghép tên đúng revision + subject sau reader/scope; SĐT giữ masked | FN-10 BR-05, HCN-03/09 |
| `internal/vault/identity/handler/{http,revision_resolve}.go`, `internal/kn/extension/cntslivestock/handler/http.go`, `contracts/openapi/identity-vault.yaml` | 4 — POST no-store, không cache Idempotency | FN-10 HCN-09, audit Resolve |
| `apps/web-ops/src/api/mock/kn-cnts-livestock.ts`, `src/kn/cnts/livestock/history.test.tsx` | 5 — hợp đồng giả lập và kiểm màn chi tiết/escape; màn thật dùng `LogPanel` đã có | FN-10 HCN-09 |
| `internal/vault/identity/app/revision_resolve_test.go`, `internal/kn/extension/cntslivestock/handler/*integration_test.go`, `cmd/identity-vault/hcn09_integration_test.go` | 3–4 — PG thật, source owner/current account, vault HTTP thật, fixture định danh tổng hợp | FN-10 HCN-09, ma trận quyền |

Giới hạn 200 bản sửa là giới hạn mỗi lần gọi vault. Lịch sử có nhiều hơn 200 lần đổi tên được chia batch, giữ đủ hai tên của mọi revision hợp lệ; nếu batch sau lỗi đọc/audit, không trả tên đã lấy từ batch trước. `internal/kn/extension/cntslivestock/app/history_batch_test.go` kiểm 201 revision và lỗi batch thứ hai bằng contract fake (kiểm batching, không thay bằng chứng quyền/vault native). Chạy riêng: `go test -count=1 -p 1 -timeout 55m -v -run TestHistoryNamesBatch276 ./internal/kn/extension/cntslivestock/app`.

`ResolveRevisions` chỉ dùng `extension.field_support`, yêu cầu Registrar và kiểm sensitive như Resolve (MFA khi bật, quota từng ref). Audit bắt buộc commit trước khi trả tên; lỗi audit/KMS không trả một phần tên. Kho không giải mã SĐT cho endpoint mới. I/O vault/KMS nằm ngoài giao dịch; có kiểm pool 1 và RLS dùng lại kết nối. Các consumer `Revisions` cũ tiếp tục nhận nhãn che bớt. Không log tên/SĐT; HTTP/client không cache phản hồi tên.

Cách kiểm: đặt toolchain theo `go.mod`; chạy `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/vault/identity/... ./internal/kn/extension/cntslivestock/... ./cmd/identity-vault` và các consumer/assembly bị ảnh hưởng; xem `TestResolveRevisionNames_276`, `TestLivestockHistoryVersionsAndUnavailable_276`, `TestHCN09NativeCurrentSession_276`. Web: `npm ci`, `npm run check:api`, `npm run typecheck`, `npm run lint`, `npx vitest run`, `npm run build:mock`; mở luồng Thêm hộ → sửa tên/SĐT hai lần → xem lịch sử trên desktop/mobile.

Giới hạn bằng chứng: fixture native dùng token ký kiểm thử, trạng thái account/địa bàn tổng hợp và KMS phần mềm, không phải OIDC/KMS triển khai. Browser/mock chỉ chứng minh hiển thị/hợp đồng/escape và che SĐT, tách khỏi kiểm quyền/vault PG. Gói này không nghiệm thu toàn FN-10, toàn UAT CNTS hay profile KN độc lập. `brd-surface.yaml` chưa có trên nền gói; bổ sung bản đồ chung sau khi #262 thực sự được tích hợp.

Phản hồi POST trực tiếp vẫn có tên đã được vault audit. Bản lưu Idempotency dùng `SetStoredBody` hiện có, chỉ lưu metadata: bỏ họ tên hiện hành/người sửa, thay tên lịch sử bằng trạng thái chưa đọc được. Gửi lại cùng khóa không phát lại tên từ cache; GET chi tiết đọc lại tên qua vault với quyền hiện hành. Không sửa payload cũ đã lưu. `TestHCN09NativeCurrentSession_276` kiểm DB cache, replay và GET mới.

Giả lập HCN-09 cũng không dùng persona dự phòng để mở tên: cần ngữ cảnh được chọn, vai/cohort đọc hợp lệ, Nút/thời hạn và phạm vi hộ. Provider giả lập cũ chưa chiếu `province_group` cho Cấp Tỉnh: trường hợp thiếu metadata này hiện unavailable, không suy nhóm từ cấp/department. Quyền đọc của đủ 5 nhóm Cấp Tỉnh (kể cả BGĐ chỉ xem) được kiểm trên account owner/vault native, không dùng mock thiếu metadata làm bằng chứng quyền.
