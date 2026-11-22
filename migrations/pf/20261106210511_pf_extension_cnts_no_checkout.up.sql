-- pf/extension — CNTS-V4-CHECKIN-MONITOR (#250), BRD CNTS v4.0 FN-12 BR-07/BR-08, AC-10, AC-13, Mục III.5 dòng 13
-- "Chưa check-out": job extension_cnts_alert (worker) quét check-in đã qua 23:59 ngày công tác (giờ Việt Nam) mà chưa có
-- check-out để sinh cảnh báo cho người theo dõi lịch cán bộ xã, và tự đóng khi check-out đồng bộ.
-- 1. ag_ops_jobs (BYPASSRLS) chỉ đọc cột tenant_id, day của sổ check-in để tìm tenant có check-in trong cửa sổ quét;
--    việc đọc check-in chạy bằng vai extension_app trong giao dịch tenant (RLS, R-05).
-- 2. Chỉ mục (tenant_id, day) cho truy vấn quét theo ngày công tác.
-- Chỉ GRANT và chỉ mục: không đổi dữ liệu.

SET LOCAL lock_timeout = '5s';

GRANT SELECT (tenant_id, day) ON pf_extension.work_schedule_checkin TO ag_ops_jobs;
CREATE INDEX work_schedule_checkin_day ON pf_extension.work_schedule_checkin (tenant_id, day);
