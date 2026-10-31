# Lỗi AI hay mắc trong repo này

Luôn nạp. Bổ sung cho AGENTS.md §9 (lỗi bắt buộc tránh) — đọc §9 trước.

- **Không làm test "xanh giả".** Không hard-code kết quả, không nới assertion, không `t.Skip`, không xóa test fail. Test DB **SKIP là trượt**: thiếu `AG_TEST_PG_ADMIN_URL` thì `ok` vô nghĩa — chạy `go test -v -run <Test>` và phải thấy `--- PASS`.
- **Không bịa API.** Trước khi gọi hàm của `internal/shared/*` hay `port/` module khác, mở mã thật để xem chữ ký. Dùng gói có sẵn thay vì tự viết: `ids`, `money`, `mass`, `clock`, `problem`, `authz`, `tenant`, `db`, `outbox`, `inbox`, `idempotency`, `httpx`, `pgtest`, `objstore`, `params`.
- **Không sửa tay tệp sinh** (`module.yaml`, `internal/shared/modreg/*_gen.go`, `apps/*/src/api/generated/*`, menu/mock/OpenAPI gộp của web, golden `kn-menu.json`). Sửa nguồn rồi chạy lệnh sinh ở AGENTS.md §6.
- **Không nới ngoại lệ, baseline.** `tools/archlint/modgraph_exceptions.yaml`, `tools/knboundary/baseline.yaml`, `migrations/schema_isolation_exceptions.yaml`, `migrations/rls_explain_exceptions.yaml`, `tools/migratecheck/grandfather.go` chỉ được thu nhỏ.
- **Không sửa tệp do người tích hợp giữ:** `internal/shared/dbadmin/dbadmin.go`, `cmd/api-platform/main.go`, `.github/workflows/ci.yml`, `deploy/**`, migration của module khác, migration đã gộp. Cần đổi → ghi vào báo cáo bàn giao. `cmd/*/assembly` chỉ thêm phần nối module của mình, gọn.
- **Đổi `internal/shared/*`:** thay đổi nhỏ, tương thích ngược, ghi rõ trong báo cáo.
- **Đúng làn.** Chỉ ghi trong phạm vi làn của issue (AGENTS.md §1 "Làn"). Ngoài làn → dừng, đề xuất ticket con.
- **Bắt chước mã hiện có** của cùng module (cách chia lớp, đặt tên, xử lý lỗi, chú thích tiếng Việt). Không mang phong cách khác vào.
- **Dependency mới:** chỉ khi cần thật; giấy phép phải có trong `deploy/policies/allowed-licenses.txt`; không thêm `github.com/lib/pq` (dùng `pgx/v5`).
- **Không bí mật, không đường dẫn máy** trong mã, test, tài liệu (dùng `<repo>`, `<worktree>`). Bí mật sinh bằng `scripts/gen-dev-secrets.*`.
- **Yêu cầu chưa rõ:** không đoán ngầm trong mã. Hỏi lại; nếu buộc phải giả định, ghi vào `docs/kn/gia-dinh/<PKG>.md` và nêu trong báo cáo.
