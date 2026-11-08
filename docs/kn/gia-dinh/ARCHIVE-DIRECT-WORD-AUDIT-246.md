## ARCHIVE-DIRECT-WORD-AUDIT-246 — Word/history trực tiếp

Căn cứ: Function chi tiết FN-TW-01.01 (P713–738), FN-PN-03.03 (P1592–1615), BRD KHTC v2.3 Product DOCX SHA256 `15859351F90574BF667190B1A12160465BA03F8CA9E408B94C90D285A3502E25`. Lát này thuộc #246 sau #227; chưa hoàn tất toàn FN, quyền phòng/lãnh đạo/BGĐ, UAT hoặc G1.

### Phạm vi và hành vi

Giữ reader KHTC/Admin/TW-KHTC và tenant hiện hành. Hai URL cũ `/report/words` và `/report/words/{runID}/download` dùng cùng chứng cứ liên kết bất biến #199 với đúng snapshot Excel. Không dùng tên tệp, số thứ tự hay loại mẫu để suy liên kết. Đường đọc không ghi binding, không dựng lại Word, không cấp lại số.

History ghi `read_history`/`metadata` với kỳ, actor, selected context, clock và trace đã xác thực; ngân sách byte/file/targets bằng 0. Event phải COMMIT trước khi gọi public exporter `Runs` ngoài giao dịch. Sau đó từng metadata run phải khớp đủ binding và header snapshot thuộc tenant. History chứa run chưa có chứng cứ liên kết trả 503 với lý do rõ ràng, thay vì suy snapshot hoặc âm thầm bỏ run. Giữ DTO và giới hạn 100 lần xuất hiện hữu của exporter.

Direct Word GET tra binding trong owner metadata, rồi gọi lại `DownloadArchiveWord`: xác minh snapshot, COMMIT `word_attempt` với đúng run/export/byte budget trước FilePort; đối chiếu toàn metadata và SHA-256 của byte đã lưu. Không thay đổi byte, hash, số, người xuất hay thời điểm xuất. Lỗi audit INSERT hoặc deferred COMMIT không trả metadata/file và không gọi cổng exporter. Audit là truy cập/attempt, không xác nhận giao mạng hoàn tất. A5/ADR0024 Internal Observe metering giữ nguyên.

Hợp đồng alias legacy phân loại thiếu owner attestation là đúng `503 extension.dependency_unavailable` với chi tiết thiếu chứng cứ liên kết, kể cả UUID ngẫu nhiên. Public exporter chỉ có `Runs` metadata bị chặn ở 100 bản và `Run` kèm byte; consumer không thể chứng minh ID không tồn tại thay vì run thực chưa liên kết. Không gọi cổng byte hoặc đọc SQL riêng chỉ để suy 404. Kỳ không có trong tenant, hoặc binding thực đã chứng minh thuộc kỳ khác, vẫn 404 theo owner. Hai mã 404/503 đã có trong OpenAPI; negative này là thay đổi phân loại có chủ đích, không phải chấp nhận lỗi bất kỳ.

Không thêm dependency, migration, quyền, route, schema DTO, menu hoặc web. Sổ access và ràng buộc append-only đã có đủ action cần dùng; không sửa migration cũ hay dữ liệu đã đóng băng. Các test #199 trước đây đọc được run chưa attested qua URL cũ nay yêu cầu 503/no FilePort, vẫn giữ kiểm số, snapshot archive và bằng chứng không dựng/cấp số lại.

### Kiểm chứng và giới hạn

`TestArchive246DirectWordCommittedAudit` tái hiện native RED: cả history và direct Word GET trả 200 nhưng thiếu event, trong khi snapshot Word #199 là đối chứng. Các test #246 chạy router lắp ráp, signed fixture OIDC, accounts chọn/giải lại context và PostgreSQL thật; dữ liệu report tổng hợp được tạo bởi fixture HTTP owner/exporter hiện hữu. Không sao chép người xuất thành quyền người đọc, không inject context vào request kiểm quyền.

Phủ INSERT/deferred COMMIT rollback/recovery; Word GET thực và mixed ZIP POST dùng fresh key; tenant/current selected department/missing/unknown/foreign actor context, Admin thiếu MFA, revoke và read-only; metadata drift, unlinked Word; một kết nối cùng kiểm không I/O trong DBTX. Ma trận giữ quy tắc accounts hiện hữu: context chỉ xem được GET, POST ZIP bị chặn tập trung. Bộ kiểm này không chứng minh UAT người dùng hoặc quyền chưa có public supplier.

Phần còn lại đã tách thành child chính thức, không nằm trong lát này:

- [#297 W11.69-ARCHIVE-SNAPSHOT-AUTHORITY](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/297): public original-department provenance và quyền lãnh đạo/BGĐ; quyền trên actual frozen rows sau `export_filter` thống nhất mọi đường kho. Selected department/field metadata không thay provenance; giữ Q027/Q055 và không mở TW chéo tenant Trung tâm. Phần này khác warning provenance #245; sau actual dev #246, phối hợp metadata owner #259.
- [#298 W11.69-ARCHIVE-DENY-REPLAY-AUDIT](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/298): public hook của session/idempotency để mandatory deny/retry/cache-hit audit trước khi trả data; shared middleware hiện có thể replay trước handler. Không nhận header chưa xác thực làm tenant/actor/context, không biến Observe metering thành quyền. Sau actual dev #246 và #297.
- [#299 W11.69-ARCHIVE-LEGACY-ATTESTATION](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/299): attestation được phép hoặc full unique fingerprint của Word legacy với đúng snapshot. Public exporter hiện có Export/Run/Runs, chưa có năng lực xác nhận liên kết lịch sử; chưa chứng minh thì unavailable. Không đọc private Forms/export SQL, không backfill trong đường đọc. Sau actual dev #246 và public supplier phải vào actual dev trước consumer.

Windows CGO=0/no gcc không chạy được race; Linux race và whole CI đúng SHA vẫn phải hoàn tất khi hết HOLD tích hợp. Không tuyên bố ready/merge từ kiểm chứng cục bộ.

### Tệp và cách chạy

| Tệp | Bước | FN/BRD |
|---|---|---|
| `internal/kn/extension/khtcreport/app/archive_word_direct.go` | 3 — service | FN-TW-01.01: binding đúng snapshot, COMMIT audit trước port, unlinked unavailable |
| `internal/kn/extension/khtcreport/app/word_service.go` | 3 — service | Giữ tạo Word, chuyển reader trực tiếp sang service audit |
| `tests/kn77/khtc_archive_direct246_integration_test.go` | 3–4 — native HTTP | Audit rollback/recovery, authority hiện hữu, Word/mixed ZIP, pool1 |
| `tests/kn77/khtc_archive_word199_integration_test.go` | 3–4 — hồi quy | Chặn direct legacy chưa attested, giữ byte/number/binding assertions |
| `tests/kn77/khtc_word_integration_test.go` | 3–4 — negative contract | Giữ kiểm byte/hash/số; thiếu attestation trả đúng 503/code/detail và chính request có 0 FilePort |

Bước 1 không thêm dependency; bước 2 không đổi data schema/migration; bước 4 không đổi route/DTO/OpenAPI; bước 5 không đổi web/menu.

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/kn77 -run '^TestArchive246'
```

Affected/direct consumers: `./internal/kn/extension/khtcreport/...`, `./internal/kn/extension`, `./internal/kn/extension/adhocintegration`, `./internal/kn/extension/khtcform/app`, `./tests/kn77`, `./cmd/api-platform/assembly`, `./migrations`; native PG phải 0 SKIP. Chạy đủ static CONTEXT §3.3, lint-new0, merge-tree; controlled business FAIL → exact restore → control PASS. Whole CI/public self-review/ready/PR/merge còn HOLD tới actual dev #287 → #286 → #261/#262 và explicit ROOT release.
