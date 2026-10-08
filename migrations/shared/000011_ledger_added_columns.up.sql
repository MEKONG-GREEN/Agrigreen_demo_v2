-- fx2 (ADR-0012, mục "Cột thêm vào sổ sau khi đã có dòng"): ALTER TABLE <sổ> ADD COLUMN không còn làm
-- đứt chuỗi băm giả. Chạy bằng vai ag_migrator. Chỉ tiến (R-24), không UPDATE/DELETE dòng sổ (R-25).
-- Áp cho schema shared của MỌI database.
--
-- Vấn đề: fn_chain băm sha256(prev_hash || canonical(to_jsonb(NEW))) — toàn bộ dòng trừ recorded_at,
-- prev_hash, row_hash. verify_chain băm lại to_jsonb(dòng hiện tại). Sau ADD COLUMN, dòng cũ hiện thêm
-- khóa "<cột>": null (hoặc giá trị DEFAULT) → băm lại khác row_hash → mọi chuỗi có dòng cũ báo
-- 'row_hash không khớp nội dung dòng' (cảnh báo giả P1 của job xác minh, che mất tín hiệu sửa thật).
-- Đã xảy ra: mkg 000006 (mkg_fieldops.roll_count, 4 cột) và pf 000079 (pf_geo.plot_boundary 4 cột,
-- pf_geo.plot_event 8 cột) — dòng ghi trước hai migration này không xác minh được.
--
-- Cách sửa (không đụng fn_chain, không đổi row_hash nào):
--   1. shared.ledger_added_column: sổ đăng ký cột thêm vào bảng sổ sau khi tạo, theo "đợt" (epoch) tăng dần.
--   2. verify_chain: nếu băm cả dòng không khớp, thử bỏ lần lượt các đợt cột thêm (mới nhất trước) — CHỈ khi
--      giá trị của mọi cột trong đợt là NULL (đúng như dòng có trước đợt đó hiện ra) — rồi băm lại.
--      Không yếu đi: dòng chỉ được nhận khi row_hash đã lưu bằng đúng băm của nội dung thiếu các khóa null
--      đó; sửa bất kỳ giá trị nào (kể cả đặt cột thêm từ có giá trị về NULL ở dòng mới) vẫn lệch băm.
--   3. shared.register_ledger_columns(bảng, cột[]): migration thêm cột vào sổ PHẢI gọi ngay sau ADD COLUMN
--      (migratecheck chặn nếu thiếu). Chỉ nhận cột NULL được, không DEFAULT, không generated — DEFAULT làm
--      dòng cũ hiện giá trị không có lúc ghi (sự kiện giả trên sổ chỉ ghi thêm).
--   4. Đăng ký bù cho mkg 000006 và pf 000079 (tên bảng dạng văn bản: shared chạy trước migration cụm nên
--      bảng có thể chưa tồn tại; dòng đăng ký của cụm khác vô hại — chỉ dùng khi tên bảng khớp).

CREATE TABLE shared.ledger_added_column (
  ledger_table  text        NOT NULL,   -- format('%I.%I', schema, bảng cha của sổ)
  column_name   text        NOT NULL,
  epoch         integer     NOT NULL CHECK (epoch > 0),   -- đợt thêm, tăng dần theo từng sổ
  registered_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (ledger_table, column_name)
);
COMMENT ON TABLE shared.ledger_added_column IS
  'ag:no-rls siêu dữ liệu lược đồ (không có dữ liệu tenant): cột thêm vào bảng sổ sau khi tạo — verify_chain bỏ khóa null của các đợt này khi băm lại dòng cũ (ADR-0012)';
REVOKE ALL ON shared.ledger_added_column FROM PUBLIC;

-- Các đợt cột thêm của một sổ (bảng cha hoặc phân vùng), đợt mới nhất trước. SECURITY DEFINER để
-- verify_chain (SECURITY INVOKER, chạy dưới vai ứng dụng) đọc được mà không cấp quyền bảng.
CREATE FUNCTION shared.ledger_added_columns(p_table regclass)
RETURNS TABLE (epoch integer, columns text[])
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT a.epoch, array_agg(a.column_name ORDER BY a.column_name)
  FROM shared.ledger_added_column a
  JOIN pg_class c ON c.oid = coalesce(pg_partition_root(p_table), p_table)
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE a.ledger_table = format('%I.%I', n.nspname, c.relname)
  GROUP BY a.epoch
  ORDER BY a.epoch DESC
$$;

-- Gọi trong migration, ngay sau ALTER TABLE <sổ> ADD COLUMN …: mọi cột trong một lần gọi là một đợt.
CREATE FUNCTION shared.register_ledger_columns(p_table regclass, p_columns text[]) RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_name  text;
  v_epoch integer;
  v_col   text;
  a       record;
BEGIN
  SELECT format('%I.%I', n.nspname, c.relname) INTO v_name
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.oid = p_table AND NOT c.relispartition;
  IF v_name IS NULL THEN
    RAISE EXCEPTION 'register_ledger_columns: % là phân vùng — đăng ký trên bảng cha', p_table;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid = p_table AND t.tgname = 'trg_chain'
                   AND t.tgfoid = 'shared.fn_chain'::regproc) THEN
    RAISE EXCEPTION 'register_ledger_columns: % không phải bảng sổ (thiếu trg_chain)', v_name;
  END IF;
  IF coalesce(cardinality(p_columns), 0) = 0 THEN
    RAISE EXCEPTION 'register_ledger_columns: danh sách cột rỗng';
  END IF;
  SELECT coalesce(max(l.epoch), 0) + 1 INTO v_epoch FROM shared.ledger_added_column l WHERE l.ledger_table = v_name;
  FOREACH v_col IN ARRAY p_columns LOOP
    SELECT at.attnotnull, at.atthasdef, at.attgenerated INTO a
    FROM pg_attribute at WHERE at.attrelid = p_table AND at.attname = v_col AND at.attnum > 0 AND NOT at.attisdropped;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'register_ledger_columns: %.% không tồn tại', v_name, v_col;
    END IF;
    IF a.attnotnull OR a.atthasdef OR a.attgenerated <> '' THEN
      RAISE EXCEPTION 'register_ledger_columns: %.% phải NULL được, không DEFAULT, không generated (ADR-0012)', v_name, v_col
        USING ERRCODE = 'check_violation';
    END IF;
    IF v_col IN ('recorded_at', 'prev_hash', 'row_hash') THEN
      RAISE EXCEPTION 'register_ledger_columns: % là cột hệ thống của sổ', v_col;
    END IF;
    INSERT INTO shared.ledger_added_column (ledger_table, column_name, epoch) VALUES (v_name, v_col, v_epoch);
  END LOOP;
END $$;
REVOKE ALL ON FUNCTION shared.register_ledger_columns(regclass, text[]) FROM PUBLIC;

-- Như shared 000001; chỉ thêm bước thử bỏ các đợt cột thêm khi băm cả dòng không khớp.
CREATE OR REPLACE FUNCTION shared.verify_chain(p_table regclass, p_tenant uuid, p_stream text)
RETURNS TABLE (ok boolean, rows_checked bigint, broken_at_seq bigint, reason text)
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
SET timezone = 'UTC'
AS $$
DECLARE
  r          record;
  v_expected bigint := 1;
  v_prev     bytea  := '\x00'::bytea;
  v_n        bigint := 0;
  v_epochs   jsonb;   -- mảng các mảng tên cột, đợt mới nhất trước
  v_j        jsonb;
  v_cols     text[];
  v_match    boolean;
  i          integer;
BEGIN
  SELECT coalesce(jsonb_agg(to_jsonb(e.columns) ORDER BY e.epoch DESC), '[]'::jsonb) INTO v_epochs
  FROM shared.ledger_added_columns(p_table) e;

  FOR r IN EXECUTE format(
      'SELECT seq, prev_hash, row_hash, to_jsonb(t) AS j FROM %s t
        WHERE tenant_id = $1 AND stream_key = $2 ORDER BY seq', p_table)
    USING p_tenant, p_stream
  LOOP
    v_n := v_n + 1;
    IF r.seq <> v_expected THEN
      RETURN QUERY SELECT false, v_n, r.seq, format('thiếu hoặc trùng seq: mong %s, gặp %s', v_expected, r.seq);
      RETURN;
    END IF;
    IF r.prev_hash <> v_prev THEN
      RETURN QUERY SELECT false, v_n, r.seq, 'prev_hash không khớp row_hash của dòng trước';
      RETURN;
    END IF;
    v_match := r.row_hash = sha256(r.prev_hash || shared.canonical(r.j));
    IF NOT v_match THEN
      -- Dòng ghi trước đợt thêm cột k không có khóa của đợt k..mới nhất: bỏ dần từ đợt mới nhất,
      -- chỉ khi mọi cột của đợt đang NULL (giá trị thật nào cũng không được bỏ).
      v_j := r.j;
      FOR i IN 0 .. jsonb_array_length(v_epochs) - 1 LOOP
        v_cols := ARRAY(SELECT jsonb_array_elements_text(v_epochs -> i));
        EXIT WHEN EXISTS (SELECT 1 FROM unnest(v_cols) AS c(name) WHERE v_j ->> c.name IS NOT NULL);
        v_j := v_j - v_cols;
        IF r.row_hash = sha256(r.prev_hash || shared.canonical(v_j)) THEN
          v_match := true;
          EXIT;
        END IF;
      END LOOP;
    END IF;
    IF NOT v_match THEN
      RETURN QUERY SELECT false, v_n, r.seq, 'row_hash không khớp nội dung dòng';
      RETURN;
    END IF;
    v_prev := r.row_hash;
    v_expected := v_expected + 1;
  END LOOP;
  RETURN QUERY SELECT true, v_n, NULL::bigint, NULL::text;
END $$;
GRANT EXECUTE ON FUNCTION shared.verify_chain(regclass, uuid, text) TO ag_shared_user;

-- Đăng ký bù: cột đã thêm vào sổ trước quy tắc này (đều NULL được, không DEFAULT — xem hai migration).
INSERT INTO shared.ledger_added_column (ledger_table, column_name, epoch) VALUES
  -- mkg 000006_mkg_fieldops_g3
  ('mkg_fieldops.roll_count', 'order_id', 1),
  ('mkg_fieldops.roll_count', 'stage', 1),
  ('mkg_fieldops.roll_count', 'loose_kg', 1),
  ('mkg_fieldops.roll_count', 'discrepancy_reason', 1),
  -- pf 000079_pf_geo_kn_plot
  ('pf_geo.plot_boundary', 'shape', 1),
  ('pf_geo.plot_boundary', 'declared_area_ha', 1),
  ('pf_geo.plot_boundary', 'on_behalf_of', 1),
  ('pf_geo.plot_boundary', 'support_reason', 1),
  ('pf_geo.plot_event', 'farmer_ref', 1),
  ('pf_geo.plot_event', 'lot_name', 1),
  ('pf_geo.plot_event', 'note', 1),
  ('pf_geo.plot_event', 'declared_area_ha', 1),
  ('pf_geo.plot_event', 'shape', 1),
  ('pf_geo.plot_event', 'department_code', 1),
  ('pf_geo.plot_event', 'on_behalf_of', 1),
  ('pf_geo.plot_event', 'support_reason', 1);
