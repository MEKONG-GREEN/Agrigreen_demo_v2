-- Cụm pf: chuyển quyền tìm tenant của job định kỳ từ ag_outbox_publisher sang ag_ops_jobs (shared 000009).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24). Chỉ GRANT/REVOKE — không đụng đối tượng của module.
--
-- Job ở worker (leader) cần biết tenant nào có việc; việc thật chạy bằng vai của module trong ngữ cảnh
-- từng tenant (R-05). ag_ops_jobs (BYPASSRLS) nhận ĐÚNG các cột mà ag_outbox_publisher từng có
-- (pf 000006, 000012, 000013, 000035, 000043, 000045, 000070, 000075); ag_outbox_publisher mất toàn bộ
-- quyền trên schema module, chỉ còn shared.outbox. Module mới cần tìm tenant: GRANT SELECT (tenant_id, …)
-- cho ag_ops_jobs, không cho ag_outbox_publisher.
--
-- Số 000114 (bản review dùng 000092): cấp lại khi gộp lên architecture-v4.1 — 000092 là k_ref (W2.5),
-- 000093…000113 dành cho các gói kế hoạch v4.1. Thêm khi gộp: quyền tìm tenant của k_ref (pf 000092 cấp
-- cho ag_outbox_publisher — worker -kref-rebuild, drill kref) cũng chuyển sang ag_ops_jobs.

-- pf/extension: leo thang nhiệm vụ (000006), vòng đời báo cáo (000070), lịch công tác (000075).
GRANT USAGE ON SCHEMA pf_extension TO ag_ops_jobs;
GRANT SELECT (tenant_id, status, escalated_at, due_at) ON pf_extension.task_current TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON pf_extension.report_period TO ag_ops_jobs;
GRANT SELECT (tenant_id, week_start, locked_at) ON pf_extension.work_schedule_current TO ag_ops_jobs;
GRANT SELECT (tenant_id, active) ON pf_extension.staff TO ag_ops_jobs;

-- pf/notify: email tóm tắt (000012).
GRANT USAGE ON SCHEMA pf_notify TO ag_ops_jobs;
GRANT SELECT (tenant_id, sent_at) ON pf_notify.email_digest_item TO ag_ops_jobs;

-- pf/reports: lịch gửi báo cáo (000013).
GRANT USAGE ON SCHEMA pf_reports TO ag_ops_jobs;
GRANT SELECT (tenant_id, active) ON pf_reports.schedule TO ag_ops_jobs;

-- pf/mechanization: độ tươi dữ liệu App HTX (000035).
GRANT USAGE ON SCHEMA pf_mechanization TO ag_ops_jobs;
GRANT SELECT (tenant_id, source) ON pf_mechanization.area_sync_log TO ag_ops_jobs;

-- pf/sync: gauge xung đột quá SLA (000043).
GRANT USAGE ON SCHEMA pf_sync TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON pf_sync.conflict TO ag_ops_jobs;

-- pf/mrv: leo thang hồ sơ quá hạn (000045).
GRANT USAGE ON SCHEMA pf_mrv TO ag_ops_jobs;
GRANT SELECT (tenant_id, review_status) ON pf_mrv.evidence_current TO ag_ops_jobs;

-- Thu hồi khỏi ag_outbox_publisher (REVOKE mức bảng thu hồi cả quyền theo cột).
REVOKE ALL ON pf_extension.task_current, pf_extension.report_period, pf_extension.work_schedule_current,
  pf_extension.staff, pf_notify.email_digest_item, pf_reports.schedule, pf_mechanization.area_sync_log,
  pf_sync.conflict, pf_mrv.evidence_current FROM ag_outbox_publisher;
REVOKE USAGE ON SCHEMA pf_extension, pf_notify, pf_reports, pf_mechanization, pf_sync, pf_mrv FROM ag_outbox_publisher;

-- pf/kref (W2.5, pf 000092): tìm tenant khi dựng lại k_ref (worker -kref-rebuild, drill kref).
GRANT USAGE ON SCHEMA k_ref, pf_party, pf_masterdata TO ag_ops_jobs;
GRANT SELECT (tenant_id) ON k_ref.org, k_ref.admin_unit, k_ref.share_grant_active TO ag_ops_jobs;
GRANT SELECT (tenant_id) ON pf_party.org, pf_party.share_grant, pf_masterdata.admin_unit TO ag_ops_jobs;
REVOKE ALL ON k_ref.org, k_ref.admin_unit, k_ref.share_grant_active, pf_party.org, pf_party.share_grant,
  pf_masterdata.admin_unit FROM ag_outbox_publisher;
REVOKE USAGE ON SCHEMA k_ref, pf_party, pf_masterdata FROM ag_outbox_publisher;
