# Quy tắc cho một gói việc

Áp cho mọi gói (work package) chạy song song, do người hoặc AI agent làm. Tệp này **chỉ ghi phần chưa có** ở [g1-conventions.md](../dev/g1-conventions.md) (cấu trúc module, quy tắc dữ liệu, API, kiểm thử bắt buộc) và [package-checklist.md](../dev/package-checklist.md) (migration, ADR, docs gói); khi mâu thuẫn, mục "Ghi đè" dưới đây thắng.

## 1. Không gian làm việc

- Mỗi gói một worktree riêng, nhánh tạo từ `origin/dev` (lấy bằng `git fetch origin +refs/heads/dev:refs/remotes/origin/dev`). Chỉ làm trong worktree của mình; dùng đường dẫn tuyệt đối khi ghi tệp.
- Không push, không gộp ngoài `ticket.ps1 merge` ([two-tool-workflow.md §4](two-tool-workflow.md#4-gộp-khóa-tích-hợp--pull-request); trước khi chuyển luồng PR là bản do người điều phối cung cấp — [giai đoạn chuyển](two-tool-workflow.md#giai-đoạn-chuyển-trước-142)). Rebase thì rebase lên `origin/dev`.
- Không đụng checkout chính của người khác hay worktree của gói khác.
- Ưu tiên sửa tệp bằng công cụ sửa từng đoạn; nếu viết lại cả tệp bằng script, kiểm kích thước/diff trước khi commit.
- Hook kiểm tra (vd GateGuard) có thể hỏi xác nhận trước lệnh/sửa — trả lời ngắn và thử lại.
- Tiết kiệm: tìm bằng grep/glob, chỉ đọc phần cần.

## 2. Đọc trước

1. Issue của gói (tiêu chí nghiệm thu, làn, phụ thuộc).
2. [AGENTS.md](../../AGENTS.md), đặc biệt [§9 lỗi bắt buộc tránh](../../AGENTS.md#9-lỗi-bắt-buộc-tránh).
3. ADR được issue nhắc tới ([chỉ mục](../adr/README.md)); [modules.md](../dev/modules.md); [tài liệu kiến trúc v4.1](../kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md).
4. Với gói KN: [ADR-0055](../adr/0055-tach-he-thong-khuyen-nong.md), [đối chiếu BRD KN](../kn/doi-chieu-brd-kn-2026-10.md), ma trận nghiệm thu trong [docs/uat/](../uat/).

## 3. Quy tắc mã bổ sung

- **Cấm float** trong module có tiền/khối lượng (đặc biệt `pf/price`, `extension/khtc`): dùng `shared/money`, `numeric`; test so sánh chuỗi.
- Truy cập DB trong module đã chuyển đổi qua `db.ForModule` / `db.WithContextTx` (vai `r_<module>`); `k_ref` đọc được bởi mọi `r_*`.
- Đọc chéo module chỉ qua view `_pub` có hợp đồng ở `contracts/pubview/`; gọi chéo chỉ qua `port/`.
- Đổi import/port/vai/schema → `go run ./tools/modgen`; giữ `modgen -check` và `archlint` xanh; `modgraph_exceptions.yaml` chỉ thu nhỏ.
- Ranh giới KN: không thêm điểm dính KN mới vào module dùng chung — dùng port/registry; baseline `knboundary` chỉ giảm.
- Không gọi port module khác/I-O ngoài trong transaction: [transactions-and-io.md](../conventions/transactions-and-io.md).
- Lệnh ghi từ web: [idempotency.md](../conventions/idempotency.md). Kênh ngoài (SMTP, FCM, URL công khai): [fail-closed-config.md](../conventions/fail-closed-config.md).
- Kiểm số (không âm, bắt buộc, phạm vi) **ở server**; UI kiểm thêm, không thay thế.
- Bảo mật: không ghi mật khẩu/khóa/token vào repo hay log; PII chỉ qua `vault/identity`; `AG_MFA_DISABLED` chỉ hợp lệ khi `AG_DEPLOY_ENV=demo` (`tools/mfaflagcheck`).
- Lịch tuần theo phòng (D8): `calendar_id` = phòng; KHTC dùng lịch v2 (T4→T3); tuần nguồn rơi vào tuần KHTC chứa **ngày kết thúc** của nó.

## 4. Ghi đè (mới hơn g1-conventions)

| Chủ đề | g1-conventions ghi | Hiện hành (từ WH-B 13/10/2026) |
|---|---|---|
| Số migration | số cấp trước | mốc UTC từ `migratecheck -new`; kiểm `-base origin/dev` |
| Mã lỗi mới | mục module trong `docs/errors.md` | `docs/errors/<cụm>-<module>.md` + nhãn tiếng Việt trong web |
| Giả định gói | tệp chung | `docs/kn/gia-dinh/<PKG>.md` |
| Roadmap | `docs/roadmap.md` | `docs/roadmap.d/<mục>/<PKG>.md` |
| Kiểm thử DB | PG chung localhost:5432 | `dev-env.ps1` hoặc `pg-ephemeral.ps1`; phải thấy `--- PASS` |
| Gộp | push vào nhánh tích hợp | `ticket.ps1 merge`: sau khi chuyển luồng PR qua PR vào `dev`; trước đó bản hiện hành gộp dưới khóa (giai đoạn chuyển) |

## 5. Kiểm thử

- Lệnh cổng: [AGENTS.md §8](../../AGENTS.md#8-kiểm-thử-và-cổng).
- `go test -count=1 -p 1 -timeout 55m` trên gói đụng tới + gói tiêu thụ trực tiếp; đổi menu → thêm `./internal/platform/party/...`.
- Lỗi 0.00s hàng loạt thường là PostgreSQL sập, không phải lỗi mã (`pgtest` in một dòng `INFRA:`).
- Gói sửa lỗi: viết test tái hiện **đỏ trên dev trước**, xanh sau sửa; ghi cả hai kết quả vào bằng chứng.

## 6. Tài liệu của gói

Theo [package-checklist.md](../dev/package-checklist.md): giả định, dòng roadmap, mã lỗi, ADR (số trống kế tiếp, `docscheck`), ghi chú "Hiện thực" trong ADR liên quan.

## 7. Commit và báo cáo

- Commit sớm và nhiều lần; message tiếng Việt `<PKG>: …`; commit AI kèm trailer `Co-Authored-By`.
- Báo cáo bàn giao (bình luận `ready`): SHA commit, việc đã đổi, migration (tên tệp), lệnh kiểm + kết quả chính xác (`--- PASS`), lệch/giả định, tệp dễ xung đột với gói khác.
- Tệp hay xung đột: menu party (`menu_departments.go`, `implementedMenu`), golden `kn-menu.json`, mock `handler.ts`, `contracts/openapi/application.yaml` + `apps/web-ops/src/api/generated/*`, `module.go` của `forms`/`extension`, `cmd/api-platform/assembly/modules.go`. Tệp sinh: sinh lại, không merge tay ([AGENTS.md §6](../../AGENTS.md#6-tệp-sinh-ra-không-merge-tay)).
