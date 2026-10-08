-- W3.3 (docs/design/cooperative.md mục 8.1): CHỈ KIỂM, không sửa. Bước 3/3 sau 000131–000132. Điều kiện nào sai →
-- RAISE, migration lỗi, giao dịch lùi (không ghi version) — không để chuyển nửa vời lọt qua.

DO $$
DECLARE
  moved constant text[] := ARRAY[
    'cooperative', 'household', 'household_membership', 'kn_profile_event',
    'plot', 'plot_boundary', 'plot_event', 'boundary_conflict', 'cooperative_boundary', 'cooperative_area',
    'sop_template', 'sop_version', 'variety_sop_binding', 'season_event', 'farm_log_batch', 'farm_log',
    'farm_log_review', 'harvest_declaration', 'season_current', 'farm_log_current', 'coop_season_area', 'yield_summary'];
  -- sổ → tên sổ (TG_ARGV[0]) phải giữ nguyên (D-3)
  ledgers constant text[][] := ARRAY[
    ['kn_profile_event', 'pf_party.kn_profile_event'],
    ['plot_boundary', 'pf_geo.plot_boundary'],
    ['plot_event', 'pf_geo.plot_event'],
    ['cooperative_boundary', 'pf_geo.cooperative_boundary'],
    ['season_event', 'pf_cropcycle.season_event'],
    ['farm_log_batch', 'pf_cropcycle.farm_log_batch'],
    ['farm_log', 'pf_cropcycle.farm_log'],
    ['farm_log_review', 'pf_cropcycle.farm_log_review'],
    ['harvest_declaration', 'pf_cropcycle.harvest_declaration']];
  seqs constant text[] := ARRAY['plot_code_seq', 'plot_sync_seq', 'sop_sync_seq', 'season_sync_seq', 'farm_log_sync_seq'];
  fns constant text[] := ARRAY['fn_boundary_conflict_guard', 'fn_sop_template_guard', 'fn_sop_version_guard',
                               'fn_variety_binding_guard'];
  t text;
  bad text;
  n bigint;
BEGIN
  -- 1. 22 bảng cha ở pf_coop, không còn ở schema cũ.
  FOREACH t IN ARRAY moved LOOP
    IF to_regclass('pf_coop.' || t) IS NULL THEN
      RAISE EXCEPTION 'W3.3: thiếu pf_coop.%', t;
    END IF;
    IF to_regclass('pf_party.' || t) IS NOT NULL OR to_regclass('pf_geo.' || t) IS NOT NULL
       OR to_regclass('pf_cropcycle.' || t) IS NOT NULL THEN
      RAISE EXCEPTION 'W3.3: % còn ở schema cũ', t;
    END IF;
  END LOOP;
  FOREACH t IN ARRAY seqs LOOP
    IF to_regclass('pf_coop.' || t) IS NULL THEN
      RAISE EXCEPTION 'W3.3: thiếu chuỗi số pf_coop.%', t;
    END IF;
  END LOOP;
  SELECT string_agg(f.name, ', ') INTO bad FROM unnest(fns) f(name)
    LEFT JOIN pg_proc p ON p.proname = f.name AND p.pronamespace = 'pf_coop'::regnamespace
   WHERE p.oid IS NULL;
  IF bad IS NOT NULL OR (SELECT count(*) FROM pg_proc p WHERE p.proname = ANY (fns) AND p.pronamespace = 'pf_coop'::regnamespace) <> 4 THEN
    RAISE EXCEPTION 'W3.3: hàm trigger chưa ở pf_coop';
  END IF;
  SELECT string_agg(p.oid::regprocedure::text, ', ') INTO bad FROM pg_proc p
   WHERE p.proname = ANY (fns) AND p.pronamespace IN ('pf_party'::regnamespace, 'pf_geo'::regnamespace, 'pf_cropcycle'::regnamespace);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: hàm còn ở schema cũ: %', bad;
  END IF;

  -- 2. Không phân vùng nào (mọi cấp) của bảng pf_coop nằm ngoài pf_coop; không phân vùng nào ở schema cũ có cha ở pf_coop.
  SELECT string_agg(c.oid::regclass::text, ', ') INTO bad
    FROM pg_inherits i
    JOIN pg_class c ON c.oid = i.inhrelid
    JOIN pg_class p ON p.oid = i.inhparent
   WHERE (p.relnamespace = 'pf_coop'::regnamespace) <> (c.relnamespace = 'pf_coop'::regnamespace);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: phân vùng lệch schema với bảng cha: %', left(bad, 500);
  END IF;

  -- 3. trg_chain của 9 sổ còn trên bảng cha và GIỮ tên sổ cũ (D-3).
  FOR i IN 1 .. array_length(ledgers, 1) LOOP
    SELECT count(*) INTO n FROM pg_trigger tg
     WHERE tg.tgrelid = ('pf_coop.' || ledgers[i][1])::regclass AND tg.tgname = 'trg_chain'
       AND tg.tgfoid = 'shared.fn_chain'::regproc
       AND encode(tg.tgargs, 'escape') = ledgers[i][2] || '\000';
    IF n <> 1 THEN
      RAISE EXCEPTION 'W3.3: trg_chain của pf_coop.% không còn tên sổ %', ledgers[i][1], ledgers[i][2];
    END IF;
  END LOOP;

  -- 4. RLS bật + ép trên mọi bảng (cha và phân vùng) của pf_coop; cổng chung của shared rỗng.
  SELECT string_agg(c.oid::regclass::text, ', ') INTO bad FROM pg_class c
   WHERE c.relnamespace = 'pf_coop'::regnamespace AND c.relkind IN ('r', 'p')
     AND NOT (c.relrowsecurity AND c.relforcerowsecurity);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: thiếu RLS/FORCE RLS: %', left(bad, 500);
  END IF;
  IF EXISTS (SELECT 1 FROM shared.v_missing_rls) THEN
    RAISE EXCEPTION 'W3.3: shared.v_missing_rls không rỗng';
  END IF;
  IF EXISTS (SELECT 1 FROM shared.v_missing_immutability) THEN
    RAISE EXCEPTION 'W3.3: shared.v_missing_immutability không rỗng';
  END IF;

  -- 5. Quyền: trên pf_coop chỉ chủ (ag_migrator) và r_cooperative có quyền (bảng, cột, chuỗi số, schema); r_party,
  --    r_geo, r_cropcycle không còn quyền gì; r_cooperative không có quyền trên phân vùng.
  SELECT string_agg(DISTINCT c.oid::regclass::text || '→' || g.rolname, ', ') INTO bad
    FROM pg_class c
    CROSS JOIN LATERAL aclexplode(c.relacl) e
    JOIN pg_roles g ON g.oid = e.grantee
   WHERE c.relnamespace = 'pf_coop'::regnamespace AND e.grantee <> c.relowner
     AND (g.rolname <> 'r_cooperative' OR c.relispartition);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: quyền lạ trên pf_coop: %', left(bad, 500);
  END IF;
  SELECT string_agg(DISTINCT c.oid::regclass::text || '→' || g.rolname, ', ') INTO bad
    FROM pg_class c
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
    CROSS JOIN LATERAL aclexplode(a.attacl) e
    JOIN pg_roles g ON g.oid = e.grantee
   WHERE c.relnamespace = 'pf_coop'::regnamespace;
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: quyền cột lạ trên pf_coop: %', left(bad, 500);
  END IF;
  SELECT string_agg(g.rolname, ', ') INTO bad
    FROM pg_namespace ns CROSS JOIN LATERAL aclexplode(ns.nspacl) e JOIN pg_roles g ON g.oid = e.grantee
   WHERE ns.nspname = 'pf_coop' AND e.grantee <> ns.nspowner AND g.rolname <> 'r_cooperative';
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.3: quyền schema pf_coop lạ: %', bad;
  END IF;
  FOREACH t IN ARRAY ARRAY['r_party', 'r_geo', 'r_cropcycle'] LOOP
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = t) AND EXISTS (
         SELECT 1 FROM pg_class c WHERE c.relnamespace = 'pf_coop'::regnamespace AND c.relkind IN ('r', 'p')
            AND (has_table_privilege(t, c.oid, 'SELECT') OR has_table_privilege(t, c.oid, 'INSERT')
                 OR has_table_privilege(t, c.oid, 'UPDATE') OR has_table_privilege(t, c.oid, 'DELETE'))) THEN
      RAISE EXCEPTION 'W3.3: % còn quyền trên bảng pf_coop', t;
    END IF;
  END LOOP;
  IF NOT has_table_privilege('r_cooperative', 'pf_coop.plot', 'SELECT')
     OR has_table_privilege('r_cooperative', 'pf_coop.plot_boundary', 'UPDATE')
     OR has_table_privilege('r_cooperative', 'pf_coop.plot', 'DELETE')
     OR NOT has_table_privilege('r_cooperative', 'pf_coop.season_current', 'DELETE')
     OR NOT has_sequence_privilege('r_cooperative', 'pf_coop.plot_sync_seq', 'USAGE') THEN
    RAISE EXCEPTION 'W3.3: ma trận quyền r_cooperative sai (mục 8.4)';
  END IF;

  -- 6. Đăng ký cột thêm vào sổ theo tên mới (shared 000011).
  IF EXISTS (SELECT 1 FROM shared.ledger_added_column a
              WHERE a.ledger_table IN ('pf_geo.plot_boundary', 'pf_geo.plot_event')
                AND NOT EXISTS (SELECT 1 FROM shared.ledger_added_column b
                                 WHERE b.ledger_table = 'pf_coop.' || split_part(a.ledger_table, '.', 2)
                                   AND b.column_name = a.column_name AND b.epoch = a.epoch)) THEN
    RAISE EXCEPTION 'W3.3: thiếu đăng ký cột thêm cho tên bảng pf_coop';
  END IF;
END
$$;
