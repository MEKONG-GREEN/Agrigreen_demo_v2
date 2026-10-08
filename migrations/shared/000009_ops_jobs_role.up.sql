-- Vai riêng cho job vận hành: ag_ops_jobs (dbadmin.OpsJobs) — tách khỏi ag_outbox_publisher.
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24). Áp cho schema shared của MỌI database.
--
-- Trước đây ag_outbox_publisher (LOGIN, BYPASSRLS, CONNECT mọi cụm) vừa phát outbox vừa chạy mọi
-- job vận hành và tìm tenant cho job định kỳ của ≥ 5 module — lộ mật khẩu của nó là lộ cả hai bộ
-- quyền. Nay:
--   - ag_outbox_publisher: CHỈ shared.outbox (SELECT, UPDATE cột trạng thái — shared 000001/000005),
--     dùng bởi cmd/outbox-publisher;
--   - ag_ops_jobs: neo chuỗi (anchor_chains, chain_anchors_of), dọn Idempotency-Key và idem_key sổ,
--     đọc tenant_id của outbox (tìm tenant dự phòng — pf/metrics, rebuilddrill). Quyền cột trên bảng
--     module chuyển ở pf 000114 và mkg 000034.
--
-- Kỳ chuyển: migrate up chạy TRƯỚC khi triển khai ứng dụng (mục 20). Giữa lúc này và lúc worker/CronJob
-- bản mới chạy, job vận hành của bản cũ (vai ag_outbox_publisher) sẽ lỗi quyền — job định kỳ, chạy lại
-- ở lượt sau; triển khai worker ngay sau migrate. Cần mật khẩu AG_DB_PASSWORD_AG_OPS_JOBS (hoặc
-- AG_DB_APP_PASSWORD) trước khi chạy migrate bootstrap.

GRANT USAGE ON SCHEMA shared TO ag_ops_jobs;

-- 1. Neo chuỗi hằng ngày (shared 000002).
GRANT EXECUTE ON FUNCTION shared.anchor_chains(date), shared.chain_anchors_of(date) TO ag_ops_jobs;
REVOKE EXECUTE ON FUNCTION shared.anchor_chains(date), shared.chain_anchors_of(date) FROM ag_outbox_publisher;

-- 2. Dọn Idempotency-Key (shared 000002 — SECURITY INVOKER, cần quyền bảng + BYPASSRLS) và idem_key
--    sổ (shared 000006 — SECURITY DEFINER, chỉ EXECUTE).
GRANT EXECUTE ON FUNCTION shared.purge_expired_idempotency(integer), shared.purge_expired_ledger_idem(integer) TO ag_ops_jobs;
GRANT SELECT (tenant_id, idem_key, expires_at), DELETE ON shared.idempotency_key TO ag_ops_jobs;
REVOKE EXECUTE ON FUNCTION shared.purge_expired_idempotency(integer), shared.purge_expired_ledger_idem(integer) FROM ag_outbox_publisher;
REVOKE ALL ON shared.idempotency_key FROM ag_outbox_publisher;

-- 3. Tìm tenant dự phòng từ outbox: chỉ cột tenant_id (không payload).
GRANT SELECT (tenant_id) ON shared.outbox TO ag_ops_jobs;
