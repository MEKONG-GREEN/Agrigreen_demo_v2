-- KN-STANDALONE-5-TT-DATA (#271): QUAY LUI lần chép thửa (migration 20261110013755_pf_extension_kn_plot_copy). Chỉ dùng
-- khi phải trả về trạng thái trước khi chép, TRƯỚC khi KN ghi thêm gì lên thửa đã chép: nếu có ảnh chụp nào khác
-- 'migrated' trên chuỗi của thửa đã chép thì dừng (RAISE) và không xóa gì. Bảng nguồn HTX (pf_coop.plot) không bị đụng.
-- Chạy bằng siêu người dùng (psql -1 -f): sổ chỉ ghi thêm chặn DELETE bằng trigger nên tạm tắt trigger trong giao dịch
-- này (session_replication_role = replica), xóa đầu chuỗi băm và khóa idem của các chuỗi đã chép, rồi bật lại.
-- Sau khi quay lui, áp lại bằng cách chạy nội dung migration chép (chạy lại được).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pf_extension.kn_plot_version v
             WHERE v.event <> 'migrated'
               AND EXISTS (SELECT 1 FROM pf_extension.kn_plot_version m
                           WHERE m.tenant_id = v.tenant_id AND m.plot_ref = v.plot_ref AND m.event = 'migrated')) THEN
    RAISE EXCEPTION 'kn271 rollback: thửa đã chép có ghi mới ở kho KN — không quay lui tự động';
  END IF;
END $$;

SET LOCAL session_replication_role = replica;

CREATE TEMP TABLE kn271_rb ON COMMIT DROP AS
  SELECT tenant_id, plot_ref, stream_key, idem_key FROM pf_extension.kn_plot_version WHERE event = 'migrated';

DELETE FROM shared.ledger_idem i USING kn271_rb r
  WHERE i.ledger = 'pf_extension.kn_plot_version' AND i.tenant_id = r.tenant_id AND i.idem_key = r.idem_key;
DELETE FROM shared.chain_head h USING kn271_rb r
  WHERE h.ledger = 'pf_extension.kn_plot_version' AND h.tenant_id = r.tenant_id AND h.stream_key = r.stream_key;
DELETE FROM pf_extension.kn_plot k USING kn271_rb r WHERE k.tenant_id = r.tenant_id AND k.ref = r.plot_ref;
DELETE FROM pf_extension.kn_plot_version v USING kn271_rb r
  WHERE v.tenant_id = r.tenant_id AND v.plot_ref = r.plot_ref AND v.event = 'migrated';

SET LOCAL session_replication_role = origin;
