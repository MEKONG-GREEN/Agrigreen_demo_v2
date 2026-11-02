-- BF-CN-VACC (#168, CN-09; BRD CNTS v3.7 bảng cảnh báo Mục III.5 dòng 11 "Mũi tiêm quá hạn"): job extension_cnts_alert
-- của worker (pf/extension) tìm thêm tenant có lịch tiêm BM-07 để sinh cảnh báo mũi quá hạn. ag_ops_jobs (BYPASSRLS) chỉ
-- đọc cột tenant_id; việc đọc lịch chạy bằng vai extension_app trong giao dịch tenant (RLS, R-05). Chỉ GRANT.
GRANT SELECT (tenant_id) ON pf_extension.cnts_vaccination_batch TO ag_ops_jobs;
