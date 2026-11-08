# ARCHIVE-DIRECT-WORD-AUDIT-246

Lát LOCALPREP của #246, kế tiếp audit các đường đọc được phép #227. FN-TW-01.01 P713–738 và FN-PN-03.03 P1592–1615 của BRD KHTC v2.3 quy định nhật ký truy cập và tải đúng tệp đã xuất.

| Tệp | Bước triển khai | Phạm vi BRD |
|---|---|---|
| `internal/kn/extension/khtcreport/app/archive_word_direct.go` | 3 — service | Direct Word/history chỉ dùng attestation đúng snapshot; COMMIT audit trước port, unlinked unavailable |
| `internal/kn/extension/khtcreport/app/word_service.go` | 3 — service | Giữ luồng tạo Word; chuyển reader trực tiếp sang service audit |
| `tests/kn77/khtc_archive_direct246_integration_test.go` | 3–4 — native HTTP | RED, audit rollback/recovery, current authority, stored Word/mixed ZIP, pool1/no I/O TX |
| `tests/kn77/khtc_archive_word199_integration_test.go` | 3–4 — hồi quy | Run chưa attested không thoát qua URL cũ; giữ numbering/byte/binding assertions |
| Tài liệu giả định, lỗi và acceptance của #246 | 1–5 | Phạm vi, cách chạy, supplier remainder và giới hạn bằng chứng |

Bước 1 không thêm dependency; bước 2 không đổi data schema/migration; bước 4 không đổi route/DTO/OpenAPI; bước 5 không đổi web/menu. Không có source của supplier chưa gộp trong lát này.

Chạy lại từ gốc worktree theo môi trường toolchain của repo:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/kn77 -run '^TestArchive246'
```

Các gói affected/direct consumer: `./internal/kn/extension/khtcreport/...`, `./internal/kn/extension`, `./internal/kn/extension/adhocintegration`, `./tests/kn77`, `./cmd/api-platform/assembly`; migration dùng cùng native PG và phải 0 SKIP. Chạy đủ bộ static của CONTEXT §3.3, lint-new0, merge-tree; controlled business FAIL → exact restore → control PASS. Web không đổi. Windows CGO=0/no gcc chỉ cho phép local preparation; final Linux race/whole CI cần chạy sau tích hợp đúng SHA.

Whole CI/public self-review/ready/PR/merge còn HOLD tới chuỗi actual dev #287 → #286 → #261/#262 và explicit ROOT release. Các child còn lại về original-department snapshot authority, session/idempotency denial/replay audit và legacy attestation phải được lập chính thức trước khi đóng #246; không coi lát này hoàn thành toàn FN/UAT/G1.
