-- pf/extension — PA1-HTX-SUPPORT-GPSLOG (#313, kế hoạch PA1 bước 2, Q7): yêu cầu hỗ trợ của App HTX chuyển về module chủ
-- HTX (pf/cooperative, bảng pf_coop.support_request — migration 20261116020730). Tệp này làm hai việc phía KN:
--   1. Hợp đồng đọc có phiên bản cho lần chép MỘT LẦN (migration 20261116020731): view pf_extension_pub.v1_support_request_export.
--      Mã/migration ngoài KN không được nhắc bảng KN (knboundary sql-into-kn) — chỉ đọc qua view công bố. Kèm ảnh chụp
--      trạng thái nhiệm vụ KN lúc chép (task_status, task_sync_seq) để App HTX giữ nhãn cũ và con trỏ đồng bộ không lùi.
--      Chủ view là ag_migrator (NOBYPASSRLS), bảng gốc FORCE RLS → p_tenant áp qua view: người đọc phải đặt app.tenant_id.
--   2. Bảng cũ pf_extension.support_request GIỮ CHỈ ĐỌC: thu quyền INSERT của vai ứng dụng KN. Không DROP, không xóa dòng.
-- Không dữ liệu cá nhân (mô tả đã chặn SĐT/CCCD lúc nhận; nông hộ, HTX, thửa, ảnh là *_ref).
SET LOCAL lock_timeout = '5s';

CREATE VIEW pf_extension_pub.v1_support_request_export WITH (security_barrier=true) AS
  SELECT s.tenant_id, s.id, s.household_ref, s.cooperative_ref, s.commune_code, s.province_code, s.plot_ref, s.category,
    s.urgency, s.description, s.photo_refs, s.location, s.gps_accuracy_m, s.event_time, s.received_at, s.submitted_by,
    s.device_id, s.idem_key, s.task_id, t.status AS task_status, t.sync_seq AS task_sync_seq
  FROM pf_extension.support_request s
  LEFT JOIN pf_extension.task_current t ON t.tenant_id = s.tenant_id AND t.task_id = s.task_id;

REVOKE ALL ON pf_extension_pub.v1_support_request_export FROM PUBLIC;
GRANT SELECT ON pf_extension_pub.v1_support_request_export TO r_extension_pub;

REVOKE INSERT ON pf_extension.support_request FROM r_extension;
REVOKE INSERT ON pf_extension.support_request FROM extension_app;
