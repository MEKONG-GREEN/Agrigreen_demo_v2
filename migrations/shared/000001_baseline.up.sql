-- Nền dùng chung cho MỌI database (ag_pf, ag_mkg, ag_vault).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến, không có bản lùi (R-24).
--
-- Nội dung:
--   1. Chặn sửa/xóa bảng sổ (mục 8.2 lớp 2)
--   2. Chuỗi băm tính trong DB (mục 8.2 lớp 3) + sổ đăng ký chống trùng
--   3. Tiện ích áp RLS theo tenant (mục 9) và phân vùng bảng sổ
--   4. View kiểm tra dùng làm cổng CI (R-20, RB-2)
--   5. Bảng vận hành: outbox, inbox, idempotency (mục 12, R-51, R-60)

CREATE SCHEMA IF NOT EXISTS shared;
REVOKE ALL ON SCHEMA shared FROM PUBLIC;
GRANT USAGE ON SCHEMA shared TO ag_shared_user;

-- ---------------------------------------------------------------------------
-- 1. Chặn sửa/xóa — áp cả với owner
-- ---------------------------------------------------------------------------
CREATE FUNCTION shared.fn_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'append-only table %.%: % is forbidden', TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
    USING ERRCODE = 'insufficient_privilege',
          HINT = 'Sửa sai bằng bản ghi bù (supersedes_id + correction_reason), không UPDATE/DELETE.';
END $$;

-- ---------------------------------------------------------------------------
-- 2. Chuỗi băm
-- ---------------------------------------------------------------------------
-- Đầu chuỗi của mỗi (sổ, tenant, stream). Bảng vận hành, chỉ fn_chain chạm tới.
-- Nó thay cho việc đọc "dòng cuối" của bảng sổ: bảng sổ phân vùng theo
-- recorded_at nên không thể có UNIQUE (tenant_id, stream_key, seq) — PostgreSQL
-- bắt khóa duy nhất của bảng phân vùng phải chứa khóa phân vùng. Xem ADR-0012.
CREATE TABLE shared.chain_head (
  ledger      text        NOT NULL,
  tenant_id   uuid        NOT NULL,
  stream_key  text        NOT NULL,
  last_seq    bigint      NOT NULL,
  last_hash   bytea       NOT NULL,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (ledger, tenant_id, stream_key)
);
COMMENT ON TABLE shared.chain_head IS 'ag:no-rls chỉ truy cập qua shared.fn_chain (SECURITY DEFINER), không cấp quyền cho vai ứng dụng';

-- Sổ đăng ký idem_key: thay UNIQUE (tenant_id, idem_key) mà bảng phân vùng không có được.
CREATE TABLE shared.ledger_idem (
  ledger      text        NOT NULL,
  tenant_id   uuid        NOT NULL,
  idem_key    text        NOT NULL,
  row_id      uuid        NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (ledger, tenant_id, idem_key)
);
COMMENT ON TABLE shared.ledger_idem IS 'ag:no-rls chỉ truy cập qua shared.fn_chain (SECURITY DEFINER), không cấp quyền cho vai ứng dụng';

-- Tuần tự hóa chuẩn các cột nghiệp vụ. jsonb sắp khóa cố định; loại recorded_at
-- (mục 8.2) và hai cột băm. Gọi trong hàm có TimeZone = UTC để timestamptz
-- luôn ra cùng một chuỗi, bất kể cấu hình phiên của người gọi.
CREATE FUNCTION shared.canonical(r jsonb) RETURNS bytea
LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT convert_to((r - 'recorded_at' - 'prev_hash' - 'row_hash')::text, 'UTF8')
$$;

-- TG_ARGV[0] = tên logic của sổ, vd 'pf_party.consent'. Cần vì trigger dòng được
-- nhân bản xuống từng phân vùng, nơi TG_TABLE_NAME là tên phân vùng.
CREATE FUNCTION shared.fn_chain() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET timezone = 'UTC'
AS $$
DECLARE
  v_ledger text := TG_ARGV[0];
  v_seq    bigint;
  v_hash   bytea;
BEGIN
  IF v_ledger IS NULL THEN
    RAISE EXCEPTION 'fn_chain: thiếu tên sổ trong tham số trigger';
  END IF;
  IF NEW.tenant_id IS NULL OR NEW.stream_key IS NULL OR NEW.idem_key IS NULL THEN
    RAISE EXCEPTION 'fn_chain: tenant_id, stream_key, idem_key là bắt buộc' USING ERRCODE = 'not_null_violation';
  END IF;
  -- recorded_at do hệ đặt (R-13): chỉ chấp nhận giá trị mặc định now().
  IF NEW.recorded_at IS DISTINCT FROM now() THEN
    RAISE EXCEPTION 'fn_chain: recorded_at do hệ thống ghi, không được truyền vào' USING ERRCODE = 'check_violation';
  END IF;

  -- Chống trùng: trùng khóa → unique_violation trên ledger_idem_pkey, ứng dụng đọc lại bản ghi cũ.
  INSERT INTO shared.ledger_idem (ledger, tenant_id, idem_key, row_id)
  VALUES (v_ledger, NEW.tenant_id, NEW.idem_key, NEW.id);

  -- Khóa đầu chuỗi: mọi lần ghi cùng chuỗi xếp hàng tại đây.
  INSERT INTO shared.chain_head (ledger, tenant_id, stream_key, last_seq, last_hash)
  VALUES (v_ledger, NEW.tenant_id, NEW.stream_key, 0, '\x00'::bytea)
  ON CONFLICT DO NOTHING;

  SELECT last_seq, last_hash INTO v_seq, v_hash
  FROM shared.chain_head
  WHERE ledger = v_ledger AND tenant_id = NEW.tenant_id AND stream_key = NEW.stream_key
  FOR UPDATE;

  NEW.seq       := v_seq + 1;
  NEW.prev_hash := v_hash;
  NEW.row_hash  := sha256(NEW.prev_hash || shared.canonical(to_jsonb(NEW)));

  UPDATE shared.chain_head
     SET last_seq = NEW.seq, last_hash = NEW.row_hash, updated_at = now()
   WHERE ledger = v_ledger AND tenant_id = NEW.tenant_id AND stream_key = NEW.stream_key;

  RETURN NEW;
END $$;

-- Dựng lại chuỗi và báo vị trí đứt đầu tiên. SECURITY INVOKER: người gọi phải
-- đặt ngữ cảnh tenant (WithTenantTx) — RLS vẫn áp dụng.
CREATE FUNCTION shared.verify_chain(p_table regclass, p_tenant uuid, p_stream text)
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
BEGIN
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
    IF r.row_hash <> sha256(r.prev_hash || shared.canonical(r.j)) THEN
      RETURN QUERY SELECT false, v_n, r.seq, 'row_hash không khớp nội dung dòng';
      RETURN;
    END IF;
    v_prev := r.row_hash;
    v_expected := v_expected + 1;
  END LOOP;
  RETURN QUERY SELECT true, v_n, NULL::bigint, NULL::text;
END $$;

-- Neo hằng ngày (mục 8.2): chạy lúc 00:30 giờ Việt Nam bằng job vận hành.
CREATE TABLE shared.chain_anchor (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  business_date date        NOT NULL,
  ledger        text        NOT NULL,
  tenant_id     uuid        NOT NULL,
  stream_key    text        NOT NULL,
  last_seq      bigint      NOT NULL,
  last_hash     bytea       NOT NULL,
  anchored_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (business_date, ledger, tenant_id, stream_key)
);
COMMENT ON TABLE shared.chain_anchor IS 'ag:no-rls bảng kiểm toán của vận hành, chỉ ghi qua shared.anchor_chains';
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON shared.chain_anchor
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON shared.chain_anchor
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE FUNCTION shared.anchor_chains(p_business_date date) RETURNS bigint
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH ins AS (
    INSERT INTO shared.chain_anchor (business_date, ledger, tenant_id, stream_key, last_seq, last_hash)
    SELECT p_business_date, ledger, tenant_id, stream_key, last_seq, last_hash FROM shared.chain_head
    ON CONFLICT DO NOTHING
    RETURNING 1)
  SELECT count(*) FROM ins
$$;
REVOKE ALL ON FUNCTION shared.anchor_chains(date) FROM PUBLIC;

-- ---------------------------------------------------------------------------
-- 3. Tiện ích áp chính sách cho bảng
-- ---------------------------------------------------------------------------
-- Một policy tenant duy nhất (R-33). Thiếu ngữ cảnh → NULL → policy trả rỗng:
-- mặc định chặn (R-31).
--
-- LƯU Ý (lệch mẫu mục 9 của tài liệu kiến trúc): trên kết nối đã từng SET LOCAL
-- app.tenant_id, sau khi giao dịch kết thúc current_setting(..., true) trả ''
-- chứ không phải NULL, và ''::uuid là LỖI cú pháp. Vì vậy phải bọc nullif(..., '').
-- Kiểm thử: TestNoTenantContextSeesNothing. Xem ADR-0005.
CREATE FUNCTION shared.current_tenant() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT nullif(current_setting('app.tenant_id', true), '')::uuid
$$;
GRANT EXECUTE ON FUNCTION shared.current_tenant() TO PUBLIC;

CREATE FUNCTION shared.apply_tenant_rls(p_table regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', p_table);
  EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', p_table);
  EXECUTE format('DROP POLICY IF EXISTS p_tenant ON %s', p_table);
  EXECUTE format($f$CREATE POLICY p_tenant ON %s
    USING      (tenant_id = shared.current_tenant())
    WITH CHECK (tenant_id = shared.current_tenant())$f$, p_table);
END $$;

-- Phân vùng của bảng sổ: RLS riêng (truy cập thẳng phân vùng không đi qua
-- policy của bảng cha) và trigger TRUNCATE riêng (trigger mức câu lệnh không
-- được nhân bản xuống phân vùng). Trigger mức dòng tự nhân bản.
CREATE FUNCTION shared.secure_partition(p_part regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM shared.apply_tenant_rls(p_part);
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = p_part AND tgname = 'trg_immutable_trunc') THEN
    EXECUTE format('CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON %s
                    FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable()', p_part);
  END IF;
END $$;

-- Tạo phân vùng theo tháng (UTC) từ p_from, p_months tháng. Job vận hành gọi
-- định kỳ để luôn có phân vùng đi trước; phân vùng DEFAULT là lưới an toàn.
CREATE FUNCTION shared.create_monthly_partitions(p_parent regclass, p_from date, p_months int)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  v_schema text;
  v_name   text;
  v_month  date;
  v_part   text;
BEGIN
  SELECT n.nspname, c.relname INTO v_schema, v_name
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = p_parent;
  FOR i IN 0 .. p_months - 1 LOOP
    v_month := (date_trunc('month', p_from) + make_interval(months => i))::date;
    v_part  := format('%I.%I', v_schema, v_name || '_' || to_char(v_month, 'YYYYMM'));
    IF to_regclass(v_part) IS NULL THEN
      EXECUTE format('CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (%L) TO (%L)',
        v_part, p_parent,
        (v_month::timestamp AT TIME ZONE 'UTC'),
        ((v_month + interval '1 month')::timestamp AT TIME ZONE 'UTC'));
      PERFORM shared.secure_partition(v_part::regclass);
    END IF;
  END LOOP;
END $$;

-- Áp toàn bộ mẫu sổ cho một bảng đã khai báo theo mẫu mục 8.1:
-- trigger chuỗi băm, chặn sửa/xóa, RLS, phân vùng DEFAULT + 24 tháng tới.
CREATE FUNCTION shared.apply_ledger(p_parent regclass, p_ledger text) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
  v_schema text;
  v_name   text;
  v_def    text;
BEGIN
  SELECT n.nspname, c.relname INTO v_schema, v_name
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = p_parent;

  EXECUTE format('CREATE TRIGGER trg_chain BEFORE INSERT ON %s
                  FOR EACH ROW EXECUTE FUNCTION shared.fn_chain(%L)', p_parent, p_ledger);
  EXECUTE format('CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON %s
                  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable()', p_parent);
  EXECUTE format('CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON %s
                  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable()', p_parent);
  PERFORM shared.apply_tenant_rls(p_parent);

  v_def := format('%I.%I', v_schema, v_name || '_default');
  EXECUTE format('CREATE TABLE %s PARTITION OF %s DEFAULT', v_def, p_parent);
  PERFORM shared.secure_partition(v_def::regclass);
  PERFORM shared.create_monthly_partitions(p_parent, (now() AT TIME ZONE 'UTC')::date, 24);
END $$;

-- ---------------------------------------------------------------------------
-- 4. View kiểm tra — cổng CI: phải rỗng
-- ---------------------------------------------------------------------------
-- Bảng sổ = bảng có cột row_hash. Thiếu bất kỳ trigger nào → xuất hiện ở đây (R-20).
CREATE VIEW shared.v_missing_immutability AS
WITH ledgers AS (
  SELECT c.oid, n.nspname, c.relname, c.relkind, c.relispartition
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p')
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'row_hash' AND NOT a.attisdropped)
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
         CASE WHEN NOT coalesce(trg.chains_insert, false)   THEN 'thiếu chuỗi băm'     END) AS problem
FROM ledgers l
LEFT JOIN trg ON trg.tgrelid = l.oid
WHERE NOT (coalesce(trg.blocks_update, false) AND coalesce(trg.blocks_delete, false)
       AND coalesce(trg.blocks_truncate, false) AND coalesce(trg.chains_insert, false));

-- Mọi bảng thuộc schema nghiệp vụ (pf_*, mkg_*, vault*, ts_*, sync_*) và schema
-- shared phải có tenant_id, ENABLE + FORCE RLS và đúng một policy p_tenant (RB-2, R-33).
-- Ngoại lệ phải ghi rõ lý do bằng COMMENT bắt đầu 'ag:no-rls'.
CREATE VIEW shared.v_missing_rls AS
SELECT n.nspname AS schema_name, c.relname AS table_name,
       concat_ws(', ',
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
              THEN 'thiếu cột tenant_id' END,
         CASE WHEN NOT c.relrowsecurity THEN 'chưa ENABLE RLS' END,
         CASE WHEN NOT c.relforcerowsecurity THEN 'chưa FORCE RLS' END,
         CASE WHEN (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid) <> 1 THEN 'số policy khác 1' END,
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant') THEN 'thiếu policy p_tenant' END
       ) AS problem
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND (n.nspname = 'shared' OR n.nspname ~ '^(pf_|mkg_|vault|ts_|sync_)')
  AND coalesce(obj_description(c.oid, 'pg_class'), '') NOT LIKE 'ag:no-rls%'
  AND NOT (
        EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
    AND c.relrowsecurity AND c.relforcerowsecurity
    AND (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid) = 1
    AND EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant'));

-- ---------------------------------------------------------------------------
-- 5. Bảng vận hành (R-60: được UPDATE các cột trạng thái)
-- ---------------------------------------------------------------------------
CREATE TABLE shared.outbox (
  id             uuid        PRIMARY KEY,            -- = event_id, UUIDv7
  tenant_id      uuid        NOT NULL,
  event_type     text        NOT NULL,
  schema_version integer     NOT NULL CHECK (schema_version > 0),
  occurred_at    timestamptz NOT NULL,
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  trace_id       text        NULL,
  producer       text        NOT NULL,
  payload        jsonb       NOT NULL,
  published_at   timestamptz NULL,
  attempts       integer     NOT NULL DEFAULT 0,
  last_error     text        NULL,
  CHECK (event_type ~ '^(pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+$')  -- R-64
);
CREATE INDEX outbox_unpublished ON shared.outbox (id) WHERE published_at IS NULL;
SELECT shared.apply_tenant_rls('shared.outbox');

CREATE TABLE shared.inbox (
  event_id     uuid        NOT NULL,
  consumer     text        NOT NULL,
  tenant_id    uuid        NOT NULL,
  event_type   text        NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, consumer)
);
SELECT shared.apply_tenant_rls('shared.inbox');

CREATE TABLE shared.idempotency_key (
  tenant_id     uuid        NOT NULL,
  idem_key      text        NOT NULL CHECK (length(idem_key) BETWEEN 8 AND 200),
  actor_ref     uuid        NOT NULL,
  method        text        NOT NULL,
  route         text        NOT NULL,
  request_hash  bytea       NOT NULL,
  status        integer     NULL,          -- NULL = đang xử lý
  response_body bytea       NULL,
  content_type  text        NULL,
  location      text        NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL DEFAULT now() + interval '48 hours',
  PRIMARY KEY (tenant_id, idem_key)
);
CREATE INDEX idempotency_key_expiry ON shared.idempotency_key (expires_at);
SELECT shared.apply_tenant_rls('shared.idempotency_key');

GRANT SELECT, INSERT ON shared.outbox TO ag_shared_user;
GRANT SELECT, INSERT ON shared.inbox TO ag_shared_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON shared.idempotency_key TO ag_shared_user;
GRANT EXECUTE ON FUNCTION shared.verify_chain(regclass, uuid, text) TO ag_shared_user;

-- Publisher đọc xuyên tenant (vai BYPASSRLS), chỉ cập nhật cột trạng thái.
GRANT USAGE ON SCHEMA shared TO ag_outbox_publisher;
GRANT SELECT ON shared.outbox TO ag_outbox_publisher;
GRANT UPDATE (published_at, attempts, last_error) ON shared.outbox TO ag_outbox_publisher;

-- Bảng nội bộ của fn_chain: không vai ứng dụng nào được chạm.
REVOKE ALL ON shared.chain_head, shared.ledger_idem, shared.chain_anchor FROM PUBLIC;
REVOKE ALL ON FUNCTION shared.fn_chain() FROM PUBLIC;
REVOKE ALL ON FUNCTION shared.apply_ledger(regclass, text), shared.apply_tenant_rls(regclass),
                  shared.secure_partition(regclass), shared.create_monthly_partitions(regclass, date, int) FROM PUBLIC;
