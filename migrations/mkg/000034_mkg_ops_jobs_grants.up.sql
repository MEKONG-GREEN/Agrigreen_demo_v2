-- Cụm mkg: chuyển quyền tìm tenant của job định kỳ (api-internal, leader) từ ag_outbox_publisher sang
-- ag_ops_jobs (shared 000009). Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24). Chỉ GRANT/REVOKE.
--
-- Số 000034 (bản review dùng 000023): cấp lại khi gộp lên architecture-v4.1 — 000022 là k_ref (W2.5),
-- 000023…000032 dành cho các gói kế hoạch v4.1, 000033 là bản sao danh bạ fieldops (áp trước tệp này).
--
-- ag_ops_jobs nhận đúng các cột ag_outbox_publisher từng có (mkg 000005, 000019, 000020);
-- ag_outbox_publisher chỉ còn shared.outbox.

-- mkg/hub: công bố sức chứa theo ngày (000005).
GRANT USAGE ON SCHEMA mkg_hub TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON mkg_hub.facility_current TO ag_ops_jobs;

-- mkg/acctbridge: thử lại đẩy bút toán (000019).
GRANT USAGE ON SCHEMA mkg_acctbridge TO ag_ops_jobs;
GRANT SELECT (tenant_id, status, next_retry_at) ON mkg_acctbridge.journal_current TO ag_ops_jobs;

-- mkg/logistics: cảnh báo chuyến / phương tiện (000020).
GRANT USAGE ON SCHEMA mkg_logistics TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON mkg_logistics.trip_current TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON mkg_logistics.vessel_current TO ag_ops_jobs;

-- Thu hồi khỏi ag_outbox_publisher (REVOKE mức bảng thu hồi cả quyền theo cột).
REVOKE ALL ON mkg_hub.facility_current, mkg_acctbridge.journal_current, mkg_logistics.trip_current,
  mkg_logistics.vessel_current FROM ag_outbox_publisher;
REVOKE USAGE ON SCHEMA mkg_hub, mkg_acctbridge, mkg_logistics FROM ag_outbox_publisher;
