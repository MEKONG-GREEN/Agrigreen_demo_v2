-- pf_apiclient W5.3 (R-INT-06, ADR-0033 §2): KHO FEED chỉ ghi thêm của cổng tích hợp — cursor theo seq tăng đơn
-- điệu mỗi feed, khóa thực thể, op upsert/retract, giữ 30 ngày.
--
--   feed_head   đầu feed (một dòng mỗi feed × phân vùng): last_seq cấp seq dưới khóa dòng (người ghi xếp hàng theo
--               feed nên thứ tự commit = thứ tự seq — người đọc không bao giờ thấy seq N+1 trước N), purged_through
--               = seq lớn nhất đã dọn (cursor < purged_through → 410 integration.cursor_expired).
--   feed_entry  một mục feed: (feed, partition_key, seq) duy nhất, event_id UUIDv7 (khóa idempotent của bên nhận),
--               entity_key = giá trị các trường entity_key của hợp đồng nối bằng ':', record = bản ghi theo hợp
--               đồng contracts/integration (NULL khi retract), contract_version, dataset_version (feed theo phiên bản).
--
-- partition_key: '' cho feed chung (tổng hợp, tham chiếu — tính một lần, phát cho mọi đối tác có quyền, không lọc
-- từng hàng); mã tenant bên cấp (feed cấp phép theo bên cấp, W5.5) hoặc mã cặp (feed song phương, W5.6).
--
-- GIỮ 30 NGÀY — chọn bảng THƯỜNG + job dọn, không phải sổ: feed_entry KHÔNG là sổ (không row_hash, không
-- shared.apply_ledger) — nó là bản phát lại có hạn của dữ liệu đã dựng; nguồn sự thật là sổ/bảng chiếu của module
-- chủ và dataset_version (pf 000127, bất biến). Job pf/apiclient feed_retention (cmd/worker, vai r_apiclient) xóa
-- theo lô các mục recorded_at < now − 30 ngày rồi nâng purged_through. Hai lớp bảo vệ:
--   - trigger trg_feed_entry_guard: chặn MỌI UPDATE và TRUNCATE; DELETE chỉ được với dòng đã quá 30 ngày (áp cả
--     chủ bảng) — không thể dọn sớm hơn cam kết R-INT-06 bằng mã ứng dụng;
--   - quyền: r_apiclient SELECT, INSERT, DELETE (không UPDATE, không TRUNCATE).
-- Không dùng bảng phân vùng + DROP PARTITION: job chạy dưới vai module không có DDL (ADR-0028), lượng ghi §38.3
-- (vài nghìn dòng/ngày) nhỏ; xem lại ở W5.9 khi có số đo tải.

CREATE TABLE pf_apiclient.feed_head (
  tenant_id         uuid        NOT NULL,                 -- đơn vị vận hành (AG_PUBLIC_OPERATOR_TENANT)
  feed              text        NOT NULL CHECK (feed ~ '^[a-z][a-z0-9_]{2,47}$'),
  partition_key     text        NOT NULL DEFAULT '' CHECK (partition_key = '' OR partition_key ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
  last_seq          bigint      NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
  purged_through    bigint      NOT NULL DEFAULT 0 CHECK (purged_through >= 0),
  last_recorded_at  timestamptz NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, feed, partition_key),
  CHECK (purged_through <= last_seq)
);
SELECT shared.apply_tenant_rls('pf_apiclient.feed_head');

CREATE TABLE pf_apiclient.feed_entry (
  tenant_id         uuid        NOT NULL,
  feed              text        NOT NULL,
  partition_key     text        NOT NULL,
  seq               bigint      NOT NULL CHECK (seq >= 1),
  event_id          uuid        NOT NULL,                 -- UUIDv7 (R-11)
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  op                text        NOT NULL CHECK (op IN ('upsert', 'retract')),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  dataset_version   bigint      NULL CHECK (dataset_version IS NULL OR dataset_version >= 1),
  record            jsonb       NULL,
  recorded_at       timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, feed, partition_key, seq),
  FOREIGN KEY (tenant_id, feed, partition_key) REFERENCES pf_apiclient.feed_head (tenant_id, feed, partition_key),
  CHECK ((op = 'upsert') = (record IS NOT NULL)),
  CHECK (record IS NULL OR jsonb_typeof(record) = 'object')
);
CREATE UNIQUE INDEX feed_entry_event ON pf_apiclient.feed_entry (tenant_id, event_id);
-- Job dọn: quét theo thời điểm ghi trong từng feed.
CREATE INDEX feed_entry_recorded ON pf_apiclient.feed_entry (tenant_id, recorded_at);
SELECT shared.apply_tenant_rls('pf_apiclient.feed_entry');

-- Chỉ ghi thêm; xóa chỉ khi đã quá hạn giữ 30 ngày (R-INT-06). Áp cả chủ bảng.
CREATE FUNCTION pf_apiclient.fn_feed_entry_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND OLD.recorded_at < now() - interval '30 days' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'pf_apiclient.feed_entry: % bị cấm (chỉ ghi thêm; chỉ dọn mục đã quá 30 ngày)', TG_OP
    USING ERRCODE = 'insufficient_privilege';
END $$;
REVOKE ALL ON FUNCTION pf_apiclient.fn_feed_entry_guard() FROM PUBLIC;
CREATE TRIGGER trg_feed_entry_guard BEFORE UPDATE OR DELETE ON pf_apiclient.feed_entry
  FOR EACH ROW EXECUTE FUNCTION pf_apiclient.fn_feed_entry_guard();
CREATE TRIGGER trg_feed_entry_guard_trunc BEFORE TRUNCATE ON pf_apiclient.feed_entry
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- Dọn theo hạn giữ (job feed_retention): xóa tối đa p_limit mục đã quá 30 ngày của tenant hiện hành (RLS — chạy
-- với quyền NGƯỜI GỌI, không SECURITY DEFINER), nâng purged_through của từng feed lên seq lớn nhất đã xóa. Trả số
-- mục đã xóa; người gọi lặp tới khi < p_limit. Trigger guard vẫn áp (không dọn được mục chưa đủ 30 ngày).
CREATE FUNCTION pf_apiclient.purge_feed_entries(p_limit integer DEFAULT 5000) RETURNS bigint
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  n bigint;
BEGIN
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 100000 THEN
    RAISE EXCEPTION 'purge_feed_entries: p_limit phải trong 1..100000' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  WITH gone AS (
    DELETE FROM pf_apiclient.feed_entry e
    WHERE (e.tenant_id, e.feed, e.partition_key, e.seq) IN (
      SELECT x.tenant_id, x.feed, x.partition_key, x.seq FROM pf_apiclient.feed_entry x
      WHERE x.recorded_at < now() - interval '30 days'
      ORDER BY x.recorded_at, x.seq
      LIMIT p_limit)
    RETURNING e.tenant_id, e.feed, e.partition_key, e.seq
  ), heads AS (
    UPDATE pf_apiclient.feed_head h
    SET purged_through = GREATEST(h.purged_through, g.max_seq), updated_at = now()
    FROM (SELECT tenant_id, feed, partition_key, max(seq) AS max_seq FROM gone GROUP BY 1, 2, 3) g
    WHERE h.tenant_id = g.tenant_id AND h.feed = g.feed AND h.partition_key = g.partition_key
    RETURNING 1
  )
  SELECT count(*) INTO n FROM gone, (SELECT count(*) FROM heads) AS hc;
  RETURN n;
END $$;
REVOKE ALL ON FUNCTION pf_apiclient.purge_feed_entries(integer) FROM PUBLIC;

-- Quyền (ADR-0028: chỉ vai nhóm của module chủ).
REVOKE ALL ON pf_apiclient.feed_head, pf_apiclient.feed_entry FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_apiclient TO r_apiclient;
GRANT SELECT, INSERT, UPDATE ON pf_apiclient.feed_head TO r_apiclient;
GRANT SELECT, INSERT, DELETE ON pf_apiclient.feed_entry TO r_apiclient;
GRANT EXECUTE ON FUNCTION pf_apiclient.purge_feed_entries(integer) TO r_apiclient;
