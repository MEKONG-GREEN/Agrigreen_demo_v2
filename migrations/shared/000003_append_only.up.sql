-- Nền dùng chung G3 cho MỌI database (ag_pf, ag_mkg, ag_vault, ag_ts).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
-- Nội dung:
--   1. shared.apply_append_only: bảng CHỈ GHI THÊM không có chuỗi băm từng dòng.
--      Dùng cho chuỗi thời gian ts_* (ADR-0017): toàn vẹn của số đo thô đến từ
--      lô có chữ ký Ed25519 tại nguồn (ADR-0019), không từ fn_chain — một chuỗi
--      băm theo dòng ở tốc độ đọc cân là điểm nghẽn ghi và không thêm bằng chứng
--      nào mà chữ ký lô chưa có.
--   2. shared.v_missing_immutability mở rộng: mọi bảng trong schema ts_* phải
--      chặn UPDATE/DELETE/TRUNCATE (R-20, R-71) — chuỗi băm chỉ bắt buộc với bảng
--      sổ (có cột row_hash) như trước.

-- ---------------------------------------------------------------------------
-- 1. Áp mẫu chỉ ghi thêm (không chuỗi băm)
-- ---------------------------------------------------------------------------
-- Lớp 1 (quyền) do migration của bảng tự làm: GRANT SELECT, INSERT + REVOKE
-- UPDATE, DELETE, TRUNCATE. Hàm này làm lớp 2 (trigger chặn, áp cả owner), RLS
-- tenant (RB-2) và — nếu bảng phân vùng — phân vùng DEFAULT + 24 tháng tới theo
-- cùng cơ chế với bảng sổ (migrate maintain tạo tiếp phân vùng).
CREATE FUNCTION shared.apply_append_only(p_table regclass) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
  v_schema text;
  v_name   text;
  v_kind   "char";
  v_def    text;
BEGIN
  SELECT n.nspname, c.relname, c.relkind INTO v_schema, v_name, v_kind
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = p_table;

  EXECUTE format('CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON %s
                  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable()', p_table);
  EXECUTE format('CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON %s
                  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable()', p_table);
  PERFORM shared.apply_tenant_rls(p_table);

  IF v_kind = 'p' THEN
    v_def := format('%I.%I', v_schema, v_name || '_default');
    EXECUTE format('CREATE TABLE %s PARTITION OF %s DEFAULT', v_def, p_table);
    PERFORM shared.secure_partition(v_def::regclass);
    PERFORM shared.create_monthly_partitions(p_table, (now() AT TIME ZONE 'UTC')::date, 24);
  END IF;
END $$;
REVOKE ALL ON FUNCTION shared.apply_append_only(regclass) FROM PUBLIC;

-- ---------------------------------------------------------------------------
-- 2. View cổng R-20 mở rộng cho chuỗi thời gian
-- ---------------------------------------------------------------------------
-- Cùng cột, cùng kiểu với bản của shared/000001 (CREATE OR REPLACE VIEW).
CREATE OR REPLACE VIEW shared.v_missing_immutability AS
WITH ledgers AS (
  SELECT c.oid, n.nspname, c.relname,
         EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'row_hash' AND NOT a.attisdropped) AS chained
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p')
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND (n.nspname ~ '^ts_'
         OR EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'row_hash' AND NOT a.attisdropped))
), trg AS (
  SELECT t.tgrelid,
         bool_or(t.tgfoid = 'shared.fn_immutable'::regproc AND (t.tgtype & 1) = 1 AND (t.tgtype & 16) = 16 AND t.tgenabled <> 'D') AS blocks_update,
         bool_or(t.tgfoid = 'shared.fn_immutable'::regproc AND (t.tgtype & 1) = 1 AND (t.tgtype & 8)  = 8  AND t.tgenabled <> 'D') AS blocks_delete,
         bool_or(t.tgfoid = 'shared.fn_immutable'::regproc AND (t.tgtype & 32) = 32 AND t.tgenabled <> 'D')                     AS blocks_truncate,
         bool_or(t.tgfoid = 'shared.fn_chain'::regproc     AND (t.tgtype & 1) = 1 AND (t.tgtype & 4)  = 4  AND t.tgenabled <> 'D') AS chains_insert
  FROM pg_trigger t
  GROUP BY t.tgrelid
)
SELECT l.nspname AS schema_name, l.relname AS table_name,
       concat_ws(', ',
         CASE WHEN NOT coalesce(trg.blocks_update, false)   THEN 'thiếu chặn UPDATE'   END,
         CASE WHEN NOT coalesce(trg.blocks_delete, false)   THEN 'thiếu chặn DELETE'   END,
         CASE WHEN NOT coalesce(trg.blocks_truncate, false) THEN 'thiếu chặn TRUNCATE' END,
         CASE WHEN l.chained AND NOT coalesce(trg.chains_insert, false) THEN 'thiếu chuỗi băm' END) AS problem
FROM ledgers l
LEFT JOIN trg ON trg.tgrelid = l.oid
WHERE NOT (coalesce(trg.blocks_update, false) AND coalesce(trg.blocks_delete, false)
       AND coalesce(trg.blocks_truncate, false) AND (NOT l.chained OR coalesce(trg.chains_insert, false)));
