-- W11.43 (D4 CN3; BRD CNTS v3.7 UR-TINH-08 "cấu hình nhắc nhở tự động", FN-13 BR-02 "tự nhắc trước hạn, trễ hạn"): job
-- extension_cnts_alert của worker (pf/extension) tìm tenant có kỳ native của profile kn_cnts để nhắc nộp và tính trễ liên
-- tiếp. ag_ops_jobs (BYPASSRLS) chỉ đọc hai cột; việc đọc tiến độ chạy bằng r_forms qua cổng SubmissionFeed theo từng
-- tenant (R-05). Chỉ GRANT.
GRANT USAGE ON SCHEMA pf_forms TO ag_ops_jobs;
GRANT SELECT (tenant_id, profile) ON pf_forms.period TO ag_ops_jobs;
