## ARCHIVE-READ-AUDIT-227 — W11.69-ARCHIVE-POLICY-LEGACY: Audit đọc kho (#227)

Nguồn: BRD KHTC v2.3 Product DOCX SHA256 `15859351F90574BF667190B1A12160465BA03F8CA9E408B94C90D285A3502E25`, P713–738/P1592–1615; #227/#205/#199. Migration mới: `pf/20261106042325_pf_extension_khtc_archive_read_audit.up.sql`. Đây là lát audit các đường đọc được phép hiện hữu, chưa hoàn tất toàn FN-TW-01.01/FN-PN-03.03, UAT hoặc G1.

### Phạm vi audit

Sổ append-only `pf_extension.khtc_archive_access` được dùng tiếp, không thêm cột, không sửa hàng cũ. Migration mở rộng tập action/format, thêm ràng buộc scope riêng; tất cả CHECK được thêm `NOT VALID` rồi `VALIDATE CONSTRAINT` ở câu riêng theo R-25. Không đổi baseline, quyền DB, ràng buộc Word/ZIP cũ hoặc giới hạn hiện có.

| Đường đọc | Action / format | Nội dung ghi nhận |
|---|---|---|
| List kho | `read_list` / `metadata` | Truy cập metadata của tenant hiện tại; kỳ trong bộ lọc nếu có |
| Chọn kỳ, chọn lĩnh vực | `read_periods`, `read_fields` / `metadata` | Truy cập lựa chọn; kỳ trong bộ lọc nếu có |
| Lịch sử bản xuất có/không bộ lọc | `read_history` / `metadata` | Kỳ đã xác minh; giữ giới hạn 100 bản hiện hữu |
| Preview | `preview_attempt` / `preview` | Đúng kỳ/bản xuất đã lưu; một target, không đoán kích thước JSON gửi đi |
| Tải Excel | `excel_attempt` / `excel` | Đúng kỳ/bản xuất và số byte lưu thực tế; một target |

Metadata access không đọc một tệp tải về: `expected_bytes=0`, `file_count=0`, targets rỗng; các giá trị này không dùng thay số dòng hay ngân sách egress. Preview ghi một target nhưng `expected_bytes=0` vì không biết số byte JSON trên mạng. Excel ghi ngân sách byte thực sự đọc từ bản lưu. Không action nào chứng nhận mạng đã giao xong. Không chép query tự do, nội dung báo cáo, auth state hoặc PII vào audit.

Actor, context, thời gian UTC và trace lấy từ caller/session/clock/request đã xác thực. List/choices/history/Excel ghi cùng transaction đọc; preview dựng cục bộ ngoài transaction rồi commit audit riêng trước khi trả kết quả. Không gọi exporter, metering hoặc port ngoài trong transaction audit. Lỗi INSERT hoặc COMMIT audit đều chặn JSON/file trả về; rollback không để lại event thành công giả. Audit có thể ghi attempt khi kiểm băm hoặc egress phía sau thất bại, vẫn không khẳng định delivery.

Audit Word/ZIP của #199 và rule `archive-word-v1` giữ nguyên; sáu action mới mang `archive-read-v1`. Excel/Word/ZIP giữ số, byte và hash đã lưu, không refresh nguồn hoặc cấp số lại. A5/ADR0024 metering cho `Internal=true` tiếp tục có ngữ nghĩa Observe; sổ access bắt buộc mới độc lập với hợp đồng metering đó, không tự đổi chính sách egress.

### Quyền và phần chưa làm

Reader không đổi: Admin hoặc tỉnh/Quốc gia KHTC theo tenant và selected session hiện hành. Không cấp quyền từ metadata lĩnh vực, không tự mở quyền phòng/lãnh đạo/BGĐ hay TW tenant chéo Trung tâm (Q027/Q055). Không có input số hoặc API/response/menu/web mới; giữ parser và giới hạn cũ. Preview tiếp tục dùng full-payload renderer hiện hữu, không tuyên bố đã xử lý mọi payload legacy lỗi số.

[#246 W11.69-ARCHIVE-AFTER-READ-AUDIT](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/246) giữ quyền snapshot/phòng gốc/leadership, deny-audit/cache-hit, các đường direct Word/history legacy và attestation chưa binding. Child chỉ dùng source chung sau actual merge #227. Không suy binding theo tên/seq/kind, không backfill hoặc mở quyền trong đường đọc.

### Kiểm chứng nghiệp vụ

`TestArchive227ReadAuditIsCommittedBeforeMetadata` tái hiện RED đủ bảy đường đọc thiếu audit rồi kiểm event thực đã commit, actor/context/clock/trace đúng request. `TestArchive227ReadAuditFailureAndCommitRollback` dùng trigger tenant riêng tại INSERT và constraint trigger deferred trên partition thực tại COMMIT, kiểm cả mười đường HTTP, rollback/recovery và không FilePort trước audit. `TestArchive227ExistingAuthorityBeforeAllReadPaths` dùng role/phòng bị từ chối, fresh key cho stored Word GET và mixed ZIP POST, tenant khác và giữ byte dù nguồn live đổi. Các fixture đi qua owner/exporter HTTP và PostgreSQL thật. Đây là bằng chứng native; không coi mock browser hoặc test code là UAT người dùng.

Trước READY vẫn phải đủ cổng static/migration/lint-new, toàn test DB liên quan 0 SKIP, controlled business FAIL rồi restore/control PASS, kiểm web phù hợp, merge-tree và whole CI xanh trên đúng SHA. Claude review chéo rồi mới ticket merge dev; Codex không tự review/gộp.
