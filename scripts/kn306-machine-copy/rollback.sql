-- KN-STANDALONE-5-TT-DATA-MAY (#306): QUAY LUI lần chép số máy (migration 20261111002854_pf_extension_kn_machine_count_copy).
-- Chỉ dùng khi phải trả về trạng thái trước khi chép, TRƯỚC khi KN ghi thêm gì lên khai báo đã chép: nếu chuỗi của HTX đã
-- chép có dòng sổ nào khác 'migrated', hoặc chuỗi năm sản xuất đã chép có dòng ghi sau, thì dừng (RAISE) và không xóa gì.
-- Bảng nguồn CGH (pf_mechanization.coop_machine_count*) và dòng năm sản xuất cũ không bị đụng.
-- Chạy bằng siêu người dùng (psql -1 -f): sổ chỉ ghi thêm chặn DELETE bằng trigger nên tạm tắt trigger trong giao dịch
-- này (session_replication_role = replica), xóa đầu chuỗi băm và khóa idem của các chuỗi đã chép, rồi bật lại.
-- Sau khi quay lui, áp lại bằng cách chạy nội dung migration chép (chạy lại được).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pf_extension.kn_machine_count_event v
             WHERE v.action <> 'migrated'
               AND EXISTS (SELECT 1 FROM pf_extension.kn_machine_count_event m
                           WHERE m.tenant_id = v.tenant_id AND m.stream_key = v.stream_key AND m.action = 'migrated'))
     OR EXISTS (SELECT 1 FROM pf_extension.kn_machine_year v
                WHERE v.idem_key NOT LIKE 'kn306:migrate:year:%'
                  AND EXISTS (SELECT 1 FROM pf_extension.kn_machine_year m
                              WHERE m.tenant_id = v.tenant_id AND m.stream_key = v.stream_key AND m.idem_key LIKE 'kn306:migrate:year:%')) THEN
    RAISE EXCEPTION 'kn306 rollback: khai báo máy đã chép có ghi mới ở kho KN — không quay lui tự động';
  END IF;
END $$;

SET LOCAL session_replication_role = replica;

CREATE TEMP TABLE kn306_rb ON COMMIT DROP AS
  SELECT 'pf_extension.kn_machine_count_event'::text AS ledger, tenant_id, stream_key, idem_key, cooperative_ref
  FROM pf_extension.kn_machine_count_event WHERE action = 'migrated'
  UNION ALL
  SELECT 'pf_extension.kn_machine_year', tenant_id, stream_key, idem_key, cooperative_ref
  FROM pf_extension.kn_machine_year WHERE idem_key LIKE 'kn306:migrate:year:%';

DELETE FROM shared.ledger_idem i USING kn306_rb r
  WHERE i.ledger = r.ledger AND i.tenant_id = r.tenant_id AND i.idem_key = r.idem_key;
DELETE FROM shared.chain_head h USING kn306_rb r
  WHERE h.ledger = r.ledger AND h.tenant_id = r.tenant_id AND h.stream_key = r.stream_key;
DELETE FROM pf_extension.kn_machine_count k USING kn306_rb r
  WHERE r.ledger = 'pf_extension.kn_machine_count_event' AND k.tenant_id = r.tenant_id AND k.cooperative_ref = r.cooperative_ref;
DELETE FROM pf_extension.kn_machine_count_event v USING kn306_rb r
  WHERE r.ledger = 'pf_extension.kn_machine_count_event' AND v.tenant_id = r.tenant_id AND v.idem_key = r.idem_key;
DELETE FROM pf_extension.kn_machine_year v USING kn306_rb r
  WHERE r.ledger = 'pf_extension.kn_machine_year' AND v.tenant_id = r.tenant_id AND v.idem_key = r.idem_key;

SET LOCAL session_replication_role = origin;
