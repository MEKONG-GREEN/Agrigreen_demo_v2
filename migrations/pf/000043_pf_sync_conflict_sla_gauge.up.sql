-- pf_sync (ADR-0006, mục 19): gauge ag.sync.conflict.overdue — số xung đột đồng bộ đang
-- mở đã quá SLA (C-8) — do worker đo cho MỌI tenant. Vai ag_outbox_publisher (BYPASSRLS,
-- đã dùng cho các job của worker) chỉ đọc HAI cột để tìm tenant có xung đột đang mở; số
-- quá hạn đếm bằng vai sync_app của module trong giao dịch tenant (R-05).

GRANT USAGE ON SCHEMA pf_sync TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status) ON pf_sync.conflict TO ag_outbox_publisher;
