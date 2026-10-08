-- pf/reports: chỉ mục cho khóa ngoại chưa có chỉ mục dẫn đầu tương ứng (rà soát FK).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
--   - dispatch_file, gsheet_link (pf 000072): FK (tenant_id, dispatch_id) → dispatch; chỉ có PK
--     (tenant_id, id) — màn chi tiết công văn đọc tệp/liên kết theo dispatch_id.
--   - dispatch_link (pf 000085): FK (tenant_id, file_id) → dispatch_file; đã có chỉ mục
--     (tenant_id, dispatch_id) nhưng chưa có theo file_id.
-- Không CONCURRENTLY: mỗi tệp migration chạy trong một giao dịch; bảng nhỏ, khóa SHARE ngắn.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS dispatch_file_dispatch ON pf_reports.dispatch_file (tenant_id, dispatch_id);
CREATE INDEX IF NOT EXISTS gsheet_link_dispatch ON pf_reports.gsheet_link (tenant_id, dispatch_id);
CREATE INDEX IF NOT EXISTS dispatch_link_file ON pf_reports.dispatch_link (tenant_id, file_id);
