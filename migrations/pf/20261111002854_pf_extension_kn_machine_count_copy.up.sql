-- pf/extension — KN-STANDALONE-5-TT-DATA-MAY (#306): chép MỘT LẦN số máy đang khai báo (active) của module CGH vào kho KN
-- (pf_extension.kn_machine_count_event, kn_machine_count; migration 20261111002853). Nguồn đọc qua hợp đồng công bố
-- pf_mechanization_pub.v1_machine_count_export (migration 20261111002852) — KN không nhắc bảng riêng của CGH, không sửa/xóa
-- gì ở CGH. HTX nền tảng → hồ sơ KN loại HTX đang liên kết (htx_ref của phiên bản mới nhất; nhiều hồ sơ thì mã nhỏ nhất);
-- không có hồ sơ KN liên kết → giữ khóa là mã HTX cũ (cooperative_ref = legacy_htx_ref) để tổng số máy theo xã không đổi.
-- Năm sản xuất theo loại máy (pf_extension.kn_machine_year, sổ của KN, trước đây khóa theo HTX nền tảng): với HTX có hồ sơ
-- KN liên kết, ghi THÊM dòng 'set' hiện hành sang chuỗi của hồ sơ KN (dòng cũ giữ nguyên làm lịch sử); HTX chưa có hồ sơ
-- KN giữ nguyên khóa cũ nên không cần chép.
-- Vai chạy migration (ag_migrator) NOBYPASSRLS, mọi bảng FORCE RLS: chép theo từng tenant (đặt app.tenant_id); danh sách
-- tenant lấy từ đầu chuỗi băm của mọi sổ (shared.chain_head — siêu dữ liệu không RLS; tenant có khai báo máy luôn có sổ).
-- Chạy lại được (bỏ qua dòng đã chép theo idem_key). Đếm/so trước–sau và quay lui: scripts/kn306-machine-copy/{count,rollback}.sql.
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
  t uuid;
  h text;  -- 48 bit mili giây của UUIDv7 (R-11) cho dòng sổ được chép
  prev text := current_setting('app.tenant_id', true);
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM shared.chain_head ORDER BY tenant_id LOOP
    PERFORM set_config('app.tenant_id', t::text, true);
    h := lpad(to_hex((extract(epoch FROM clock_timestamp()) * 1000)::bigint), 12, '0');

    INSERT INTO pf_extension.kn_machine_count_event (id, tenant_id, stream_key, cooperative_ref, commune_code, machine_type, action,
        quantity, note, department_code, request_key, legacy_htx_ref, event_time, actor_ref, idem_key, rule_version)
      SELECT overlay(overlay(gen_random_uuid()::text PLACING substr(h, 1, 8) || '-' || substr(h, 9, 4) FROM 1 FOR 13) PLACING '7' FROM 15 FOR 1)::uuid,
        s.tenant_id, 'kn_machines:' || k.ref::text, k.ref, s.commune_code, s.machine_type, 'migrated', s.quantity, nullif(s.note, ''),
        s.department_code, 'kn306:migrate:' || s.cooperative_ref::text, s.cooperative_ref, s.updated_at, s.updated_by,
        'kn306:migrate:' || s.cooperative_ref::text || '|' || s.machine_type, 'kn.machine_count/v1'
      FROM pf_mechanization_pub.v1_machine_count_export s
      CROSS JOIN LATERAL (
        SELECT coalesce((SELECT min(cur.producer_ref::text)::uuid
                         FROM (SELECT DISTINCT ON (p.producer_ref) p.producer_ref, p.htx_ref
                               FROM pf_extension.kn_producer p WHERE p.kind = 'cooperative'
                               ORDER BY p.producer_ref, p.seq DESC) cur
                         WHERE cur.htx_ref = s.cooperative_ref), s.cooperative_ref) AS ref
      ) k
      WHERE s.tenant_id = t AND s.active
        AND NOT EXISTS (SELECT 1 FROM pf_extension.kn_machine_count_event e WHERE e.tenant_id = s.tenant_id
                          AND e.idem_key = 'kn306:migrate:' || s.cooperative_ref::text || '|' || s.machine_type)
      ORDER BY k.ref, s.machine_type;

    INSERT INTO pf_extension.kn_machine_count (tenant_id, cooperative_ref, machine_type, quantity, note, commune_code, department_code,
        active, legacy_htx_ref, updated_at, updated_by, last_event_id, last_seq)
      SELECT e.tenant_id, e.cooperative_ref, e.machine_type, e.quantity, e.note, e.commune_code, e.department_code, true,
        e.legacy_htx_ref, e.event_time, e.actor_ref, e.id, e.seq
      FROM pf_extension.kn_machine_count_event e
      WHERE e.tenant_id = t AND e.action = 'migrated'
        AND NOT EXISTS (SELECT 1 FROM pf_extension.kn_machine_count k WHERE k.tenant_id = e.tenant_id
                          AND k.cooperative_ref = e.cooperative_ref AND k.machine_type = e.machine_type);

    INSERT INTO pf_extension.kn_machine_year (id, tenant_id, stream_key, cooperative_ref, commune_code, department_code, machine_type,
        machine_type_name, event, year_made, event_time, actor_ref, idem_key, rule_version)
      SELECT overlay(overlay(gen_random_uuid()::text PLACING substr(h, 1, 8) || '-' || substr(h, 9, 4) FROM 1 FOR 13) PLACING '7' FROM 15 FOR 1)::uuid,
        y.tenant_id, 'kn_machine_year:' || y.department_code || ':' || m.producer_ref::text, m.producer_ref, y.commune_code,
        y.department_code, y.machine_type, y.machine_type_name, 'set', y.year_made, y.event_time, y.actor_ref,
        'kn306:migrate:year:' || y.department_code || ':' || y.cooperative_ref::text || '|' || y.machine_type, y.rule_version
      FROM (SELECT DISTINCT ON (department_code, cooperative_ref, machine_type) *
            FROM pf_extension.kn_machine_year
            ORDER BY department_code, cooperative_ref, machine_type, seq DESC) y
      JOIN (SELECT cur.htx_ref, min(cur.producer_ref::text)::uuid AS producer_ref
            FROM (SELECT DISTINCT ON (p.producer_ref) p.producer_ref, p.htx_ref
                  FROM pf_extension.kn_producer p WHERE p.kind = 'cooperative'
                  ORDER BY p.producer_ref, p.seq DESC) cur
            WHERE cur.htx_ref IS NOT NULL
            GROUP BY cur.htx_ref) m ON m.htx_ref = y.cooperative_ref
      WHERE y.tenant_id = t AND y.event = 'set'
        AND NOT EXISTS (SELECT 1 FROM pf_extension.kn_machine_year e WHERE e.tenant_id = y.tenant_id
                          AND e.idem_key = 'kn306:migrate:year:' || y.department_code || ':' || y.cooperative_ref::text || '|' || y.machine_type)
      ORDER BY y.department_code, m.producer_ref, y.machine_type;
  END LOOP;
  PERFORM set_config('app.tenant_id', coalesce(prev, ''), true);
END $$;
