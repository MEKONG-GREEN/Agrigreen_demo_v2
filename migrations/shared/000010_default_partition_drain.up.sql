-- Phân vùng DEFAULT có dòng (job maintain trễ): tự chuyển dòng ra phân vùng tháng + chỉ số giám sát.
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24). Áp cho schema shared của MỌI database.
--
-- Vấn đề (shared 000001): apply_ledger / apply_append_only tạo phân vùng DEFAULT làm lưới an toàn. Nếu
-- dòng rơi vào DEFAULT (thiếu phân vùng tháng), CREATE TABLE … PARTITION OF cho tháng đó LỖI mãi
-- (check_violation: "updated partition constraint for default partition would be violated") —
-- create_monthly_partitions và migrate maintain hỏng vĩnh viễn ở bảng đó.
--
-- Cách xử lý — tách, tạo, chuyển, gắn lại, trong MỘT giao dịch:
--   1. DETACH phân vùng DEFAULT (ACCESS EXCLUSIVE trên bảng cha suốt giao dịch — chỉ chạy ở nhánh
--      ngoại lệ, khi DEFAULT thật sự có dòng; lock_timeout 10 giây để không treo hàng đợi khóa).
--      Sau DETACH, trigger dòng nhân bản từ bảng cha (trg_chain, trg_immutable) KHÔNG còn trên bảng đã
--      tách (PostgreSQL xóa trigger nhân bản khi tách) → DELETE ở bước 3 không bị trg_immutable chặn,
--      không cần tắt trigger hay session_replication_role.
--   2. Mỗi tháng có dòng: tạo bảng rời (LIKE bảng cha: cột, mặc định, CHECK) — CHƯA gắn nên chưa có
--      trigger: INSERT … SELECT chép NGUYÊN dòng (seq, prev_hash, row_hash, recorded_at giữ nguyên;
--      fn_chain không chạy lại, shared.ledger_idem / chain_head không đổi) → chuỗi băm không đứt.
--   3. DELETE đúng các dòng đó khỏi bảng DEFAULT đã tách; ATTACH bảng tháng (PostgreSQL quét kiểm khoảng,
--      nhân bản trigger/chỉ mục/khóa ngoại của bảng cha); secure_partition (RLS + chặn TRUNCATE).
--   4. ATTACH lại DEFAULT, secure_partition (bật lại FORCE RLS đã tạm tắt ở bước 1), ANALYZE.
-- Đây là di chuyển vật lý trong cùng một bảng sổ (dòng không đổi nội dung, không mất dòng), không
-- phải sửa/xóa bản ghi sổ — cùng giao dịch, người khác không bao giờ thấy trạng thái giữa chừng.
-- Owner (ag_migrator) bị FORCE RLS nên phải NO FORCE trên bảng đã tách mới đọc được dòng; bảng không
-- gắn với bảng cha nên không vai ứng dụng nào chạm tới trong lúc đó. Lỗi bất kỳ → cả giao dịch lùi.

CREATE FUNCTION shared.drain_default_partition(p_parent regclass) RETURNS bigint
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
SET lock_timeout = '10s'
AS $$
DECLARE
  v_schema text;
  v_name   text;
  v_def    regclass;
  v_key    text;
  v_cols   text;
  v_month  date;
  v_part   text;
  v_lo     timestamptz;
  v_hi     timestamptz;
  v_n      bigint;
  v_total  bigint := 0;
BEGIN
  SELECT n.nspname, c.relname INTO v_schema, v_name
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.oid = p_parent AND c.relkind = 'p';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'drain_default_partition: % không phải bảng phân vùng', p_parent;
  END IF;
  SELECT pt.partdefid::regclass INTO v_def
  FROM pg_partitioned_table pt WHERE pt.partrelid = p_parent AND pt.partdefid <> 0;
  IF v_def IS NULL THEN
    RETURN 0;
  END IF;
  -- Chỉ mẫu của shared.create_monthly_partitions: RANGE một cột timestamptz, tháng theo UTC.
  SELECT a.attname INTO v_key
  FROM pg_partitioned_table pt
  JOIN pg_attribute a ON a.attrelid = pt.partrelid AND a.attnum = pt.partattrs[0]
  WHERE pt.partrelid = p_parent AND pt.partstrat = 'r' AND pt.partnatts = 1
    AND a.atttypid = 'timestamptz'::regtype;
  IF v_key IS NULL THEN
    RAISE EXCEPTION 'drain_default_partition: % không phân vùng RANGE theo một cột timestamptz', p_parent;
  END IF;
  SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum) INTO v_cols
  FROM pg_attribute a
  WHERE a.attrelid = p_parent AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = '';

  EXECUTE format('ALTER TABLE %s DETACH PARTITION %s', p_parent, v_def);
  EXECUTE format('ALTER TABLE %s NO FORCE ROW LEVEL SECURITY', v_def);

  FOR v_month IN EXECUTE format(
      'SELECT DISTINCT date_trunc(''month'', %I AT TIME ZONE ''UTC'')::date FROM %s ORDER BY 1', v_key, v_def)
  LOOP
    v_part := format('%I.%I', v_schema, v_name || '_' || to_char(v_month, 'YYYYMM'));
    v_lo   := v_month::timestamp AT TIME ZONE 'UTC';
    v_hi   := (v_month + interval '1 month')::timestamp AT TIME ZONE 'UTC';
    IF to_regclass(v_part) IS NOT NULL THEN
      RAISE EXCEPTION 'drain_default_partition: % đã tồn tại mà DEFAULT vẫn có dòng tháng %', v_part, v_month;
    END IF;
    EXECUTE format('CREATE TABLE %s (LIKE %s INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING GENERATED)', v_part, p_parent);
    EXECUTE format('INSERT INTO %s (%s) SELECT %s FROM %s WHERE %I >= %L AND %I < %L',
      v_part, v_cols, v_cols, v_def, v_key, v_lo, v_key, v_hi);
    GET DIAGNOSTICS v_n = ROW_COUNT;
    EXECUTE format('DELETE FROM %s WHERE %I >= %L AND %I < %L', v_def, v_key, v_lo, v_key, v_hi);
    EXECUTE format('ALTER TABLE %s ATTACH PARTITION %s FOR VALUES FROM (%L) TO (%L)', p_parent, v_part, v_lo, v_hi);
    PERFORM shared.secure_partition(v_part::regclass);
    RAISE WARNING 'drain_default_partition: chuyển % dòng từ % sang %', v_n, v_def, v_part;
    v_total := v_total + v_n;
  END LOOP;

  EXECUTE format('ALTER TABLE %s ATTACH PARTITION %s DEFAULT', p_parent, v_def);
  PERFORM shared.secure_partition(v_def);
  EXECUTE format('ANALYZE %s', v_def);
  RETURN v_total;
END $$;
REVOKE ALL ON FUNCTION shared.drain_default_partition(regclass) FROM PUBLIC;

-- Giữ nguyên chữ ký và hành vi của bản shared 000001; thêm: tháng bị DEFAULT chặn (check_violation)
-- → chuyển dòng khỏi DEFAULT (tạo luôn phân vùng mọi tháng có dòng) rồi đi tiếp.
CREATE OR REPLACE FUNCTION shared.create_monthly_partitions(p_parent regclass, p_from date, p_months int)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  v_schema text;
  v_name   text;
  v_month  date;
  v_part   text;
  v_ddl    text;
BEGIN
  SELECT n.nspname, c.relname INTO v_schema, v_name
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = p_parent;
  FOR i IN 0 .. p_months - 1 LOOP
    v_month := (date_trunc('month', p_from) + make_interval(months => i))::date;
    v_part  := format('%I.%I', v_schema, v_name || '_' || to_char(v_month, 'YYYYMM'));
    IF to_regclass(v_part) IS NULL THEN
      v_ddl := format('CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (%L) TO (%L)',
        v_part, p_parent,
        (v_month::timestamp AT TIME ZONE 'UTC'),
        ((v_month + interval '1 month')::timestamp AT TIME ZONE 'UTC'));
      BEGIN
        EXECUTE v_ddl;
      EXCEPTION WHEN check_violation THEN
        -- DEFAULT đang giữ dòng của tháng này: chuyển ra (drain tạo phân vùng cho mọi tháng có dòng).
        PERFORM shared.drain_default_partition(p_parent);
        IF to_regclass(v_part) IS NULL THEN
          EXECUTE v_ddl;
        END IF;
      END;
      PERFORM shared.secure_partition(v_part::regclass);
    END IF;
  END LOOP;
END $$;

-- Giám sát: số dòng ƯỚC LƯỢNG (thống kê n_live_tup — owner bị FORCE RLS nên không count(*) được, và
-- thống kê không lộ dữ liệu) trong phân vùng DEFAULT của mọi bảng phân vùng (bảng sổ + bảng chỉ ghi
-- thêm phân vùng). > 0 → từng thiếu phân vùng tháng: chạy migrate maintain (tự chuyển dòng) và tìm vì
-- sao job trễ. Đọc catalog/thống kê công khai nên không cần SECURITY DEFINER.
CREATE FUNCTION shared.ledger_default_rows()
RETURNS TABLE (table_name text, est_rows bigint)
LANGUAGE sql STABLE
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT format('%I.%I', n.nspname, c.relname), coalesce(s.n_live_tup, 0)
  FROM pg_partitioned_table pt
  JOIN pg_class c ON c.oid = pt.partrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace
  LEFT JOIN pg_stat_all_tables s ON s.relid = pt.partdefid
  WHERE pt.partdefid <> 0 AND NOT c.relispartition
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  ORDER BY 1
$$;
REVOKE ALL ON FUNCTION shared.ledger_default_rows() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.ledger_default_rows() TO ag_ops_jobs;

-- sql_exporter (mục 19): vai giám sát của cụm mkg / ts đọc chỉ số này. Vai chỉ tồn tại khi cụm của nó
-- được dựng (dbadmin.RolesFor, AG_CLUSTERS) — cấp có điều kiện; database của chính cụm đó luôn có vai
-- vì bootstrap chạy trước migrate. Vai giám sát chỉ nhận USAGE schema shared + EXECUTE đúng hàm này.
DO $$
DECLARE
  r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['mkg_monitor', 'ts_monitor'] LOOP
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('GRANT USAGE ON SCHEMA shared TO %I', r);
      EXECUTE format('GRANT EXECUTE ON FUNCTION shared.ledger_default_rows() TO %I', r);
    END IF;
  END LOOP;
END $$;
