-- pf/cooperative — PA1-HTX-SUPPORT-GPSLOG (#313): chép MỘT LẦN yêu cầu hỗ trợ cũ của module KN vào bảng HTX
-- pf_coop.support_request (migration 20261116020730). Nguồn đọc qua hợp đồng công bố
-- pf_extension_pub.v1_support_request_export (migration 20261116020729) — HTX không nhắc bảng riêng của KN, không sửa/xóa
-- gì ở KN (bảng cũ giữ chỉ đọc). Giữ nguyên id, khóa idem (máy gửi lặp bản cũ → trùng, không ghi mới), toạ độ, thời điểm.
-- legacy_task_id/legacy_task_status = nhiệm vụ KN và trạng thái lúc chép (App HTX giữ nhãn cũ).
-- Con trỏ đồng bộ: máy nông dân đang giữ con trỏ entity support_request = sync_seq nhiệm vụ KN. Dòng chép lấy đúng số đó,
-- rồi chuỗi số mới đặt lớn hơn mọi số đã chép → yêu cầu mới luôn ở sau con trỏ cũ, máy không bỏ sót.
-- Vai chạy migration (ag_migrator) NOBYPASSRLS, mọi bảng FORCE RLS: chép theo từng tenant (đặt app.tenant_id); danh sách
-- tenant lấy từ đầu chuỗi băm của mọi sổ (shared.chain_head — tenant có yêu cầu hỗ trợ luôn có sổ pf_extension.task_event).
-- Chạy lại được (bỏ qua id đã có). Đếm trước–sau: scripts/pa1-313-support-copy/count.sql.
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
  t uuid;
  hi bigint := 0;
  prev text := current_setting('app.tenant_id', true);
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM shared.chain_head ORDER BY tenant_id LOOP
    PERFORM set_config('app.tenant_id', t::text, true);
    hi := greatest(hi, coalesce((SELECT max(task_sync_seq) FROM pf_extension_pub.v1_support_request_export WHERE tenant_id = t), 0));
  END LOOP;
  IF hi > 0 AND hi >= (SELECT last_value FROM pf_coop.support_request_sync_seq) THEN
    PERFORM setval('pf_coop.support_request_sync_seq', hi, true);
  END IF;

  FOR t IN SELECT DISTINCT tenant_id FROM shared.chain_head ORDER BY tenant_id LOOP
    PERFORM set_config('app.tenant_id', t::text, true);
    INSERT INTO pf_coop.support_request (tenant_id, id, household_ref, cooperative_ref, commune_code, province_code, plot_ref,
        category, urgency, description, photo_refs, location, gps_accuracy_m, event_time, received_at, submitted_by, device_id,
        idem_key, sync_seq, legacy_task_id, legacy_task_status)
      SELECT s.tenant_id, s.id, s.household_ref, s.cooperative_ref, s.commune_code, s.province_code, s.plot_ref, s.category,
        s.urgency, s.description, s.photo_refs, s.location, s.gps_accuracy_m, s.event_time, s.received_at, s.submitted_by,
        s.device_id, s.idem_key, coalesce(s.task_sync_seq, nextval('pf_coop.support_request_sync_seq')), s.task_id, s.task_status
      FROM pf_extension_pub.v1_support_request_export s
      WHERE s.tenant_id = t
        AND NOT EXISTS (SELECT 1 FROM pf_coop.support_request c WHERE c.tenant_id = s.tenant_id AND c.id = s.id)
      ORDER BY s.id;
  END LOOP;
  PERFORM set_config('app.tenant_id', coalesce(prev, ''), true);
END $$;
