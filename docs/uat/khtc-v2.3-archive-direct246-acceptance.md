# KHTC v2.3 — acceptance kỹ thuật cho direct Word/history #246

Nguồn Function: FN-TW-01.01 P713–738, FN-PN-03.03 P1592–1615; Product DOCX SHA256 `15859351F90574BF667190B1A12160465BA03F8CA9E408B94C90D285A3502E25`. Đây là kiểm chứng kỹ thuật cục bộ cho một lát, chưa là UAT người dùng hay hoàn tất toàn Function.

| Yêu cầu | Kiểm chứng |
|---|---|
| Mỗi lần tải có log; chỉ tệp đã xuất | `TestArchive246DirectWordCommittedAudit`: native RED cho hai URL cũ, audit actor/context/clock/trace và COMMIT trước exporter metadata/byte; snapshot Word là control |
| Không trả data khi log bắt buộc lỗi | `TestArchive246DirectAuditInsertAndDeferredCommitFailure`: INSERT và deferred COMMIT fail; rollback, không port/no bytes; khôi phục rồi đọc nguyên byte/hash qua Word GET và mixed ZIP thực |
| Quyền reader hiện hữu trước truy cập | `TestArchive246CurrentNativeAuthorityBeforeWordAndZIP`: current selected department, context thiếu/lạ/của actor khác/cross-tenant, Admin MFA, revoke và read-only; fresh key mỗi POST |
| Không suy liên kết hoặc đổi bản lưu | `TestArchive246UnlinkedAndChangedWordMetadataUnavailable` và hồi quy #199: metadata drift/unlinked trả unavailable, không backfill/regenerate/number allocation |
| Không I/O trong DBTX, pool nhỏ | `TestArchive246DirectReadsPoolOneNoPortInTransaction`: PostgreSQL pool1 cùng exporter thật, probe transaction và audit COMMIT |

Giữ policy tenant KHTC/Admin/TW-KHTC hiện hành. Context BGD chỉ xem đã có GET được phép theo phòng KHTC; accounts vẫn chặn POST ZIP của context read-only. Không cấp quyền phòng khác từ field metadata, không chứng minh phòng gốc hoặc unique OIDC login từ selected context.

Chưa nghiệm thu: quyền phòng/lãnh đạo từ frozen rows sau `export_filter`, denial/retry/cache-hit audit tại shared pre-return seam và attestation legacy chưa liên kết. Các phần này cần public supplier + child chính thức và tích hợp trên actual dev; không đóng parent từ lát hiện tại. A5/ADR0024 metering Observe không thay quyền. Linux race/whole CI đúng SHA và quy trình tích hợp vẫn HOLD theo điều phối.
