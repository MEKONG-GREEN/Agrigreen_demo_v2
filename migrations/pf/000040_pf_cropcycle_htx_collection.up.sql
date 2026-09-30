-- pf_cropcycle G3-2: phía HTX của thu gom rơm MKG (docs/dev/g3-conventions.md §4, §5).
--
-- MKG → nền tảng CHỈ qua van công bố (contracts/publication/collection_schedule.json,
-- htx_collection_statement.json; người nhận ghi trong hợp đồng: pf/cropcycle). Nền tảng
-- → MKG CHỈ qua Application API công khai mà MKG gọi định kỳ bằng tài khoản dịch vụ
-- (vai mkg_sync) — không có hàng đợi pf → mkg.
--
--   collection_valve_receipt   SỔ nhận van (mọi bản đã nhận, kể cả bản cũ đến muộn):
--                              chống trùng theo (hợp đồng, ref, revision) = idem_key
--                              'valve:<hợp đồng>:<ref>:<revision>'. Dựng lại được hai bảng chiếu.
--   collection_schedule_view   bảng chiếu lịch gom theo HTX: bản revision lớn nhất theo schedule_ref.
--   collection_statement_view  bảng chiếu bảng kê số cuộn theo thửa: bản revision lớn nhất theo
--                              statement_ref, kèm phản hồi hiện hành của HTX (nếu có).
--   collection_response        SỔ phản hồi của HTX (confirmed | disputed kèm số cuộn + lý do), do
--                              quản lý HTX (coop_manager) của đúng HTX ghi; bản sau cùng revision
--                              THAY bản trước (supersedes_id + lý do). feed_seq là con trỏ cho
--                              MKG kéo về (cấp sau khóa tư vấn theo tenant → thứ tự commit =
--                              thứ tự feed_seq, con trỏ không bỏ sót dòng commit muộn).
--
-- Tenant: bản ghi nằm ở tenant của phong bì van (như pf_geo.hub_view, pf_mrv.collection_evidence).
-- source_tenant_id ghi tenant nguồn để cổng kéo của MKG chỉ trả phản hồi thuộc bảng kê do chính
-- tenant đó gửi (RB-2 — cấp quyền chia sẻ chờ khách, xem docs/design/mkg.md). Không có dữ liệu cá
-- nhân (R-04): HTX, thửa, người ghi chỉ là *_ref; lý do chặn chuỗi số dạng SĐT/CCCD ở app/.

-- ---------------------------------------------------------------------------
-- 1. Sổ nhận van
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.collection_valve_receipt (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'valve:<contract>:<subject_ref>'
  seq               bigint      NOT NULL,
  contract          text        NOT NULL CHECK (contract IN ('collection_schedule', 'htx_collection_statement')),
  subject_ref       uuid        NOT NULL,                 -- schedule_ref / statement_ref
  revision          integer     NOT NULL CHECK (revision >= 0),
  cooperative_ref   uuid        NOT NULL,                 -- htx_ref của hợp đồng
  source_event_id   uuid        NOT NULL,                 -- event_id của phong bì van
  source_tenant_id  uuid        NOT NULL,
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  became_current    boolean     NOT NULL,                 -- false = bản cũ đến muộn, không thay bảng chiếu
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'valve:<contract>:<subject_ref>:<revision>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX collection_valve_receipt_idem    ON pf_cropcycle.collection_valve_receipt (tenant_id, idem_key);
CREATE INDEX collection_valve_receipt_stream  ON pf_cropcycle.collection_valve_receipt (tenant_id, stream_key, seq);
CREATE INDEX collection_valve_receipt_subject ON pf_cropcycle.collection_valve_receipt (tenant_id, contract, subject_ref, revision);
SELECT shared.apply_ledger('pf_cropcycle.collection_valve_receipt', 'pf_cropcycle.collection_valve_receipt');

-- ---------------------------------------------------------------------------
-- 2. Bảng chiếu lịch gom
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_cropcycle.collection_sync_seq;

CREATE TABLE pf_cropcycle.collection_schedule_view (
  tenant_id        uuid        NOT NULL,
  schedule_ref     uuid        NOT NULL,
  cooperative_ref  uuid        NOT NULL,
  commune_code     text        NOT NULL CHECK (commune_code ~ '^[0-9]{5}$'),
  window_from      date        NOT NULL,
  window_to        date        NOT NULL,
  status           text        NOT NULL CHECK (status IN ('planned', 'confirmed', 'in_progress', 'completed', 'cancelled')),
  revision         integer     NOT NULL CHECK (revision >= 0),
  receipt_id       uuid        NOT NULL,
  source_tenant_id uuid        NOT NULL,
  published_at     timestamptz NOT NULL,
  received_at      timestamptz NOT NULL,
  sync_seq         bigint      NOT NULL DEFAULT nextval('pf_cropcycle.collection_sync_seq'),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, schedule_ref),
  CHECK (window_to >= window_from)
);
CREATE INDEX collection_schedule_view_coop    ON pf_cropcycle.collection_schedule_view (tenant_id, cooperative_ref, window_from DESC, schedule_ref);
CREATE INDEX collection_schedule_view_commune ON pf_cropcycle.collection_schedule_view (tenant_id, commune_code);
CREATE INDEX collection_schedule_view_sync    ON pf_cropcycle.collection_schedule_view (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_cropcycle.collection_schedule_view');

-- ---------------------------------------------------------------------------
-- 3. Bảng chiếu bảng kê số cuộn (+ phản hồi hiện hành)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.collection_statement_view (
  tenant_id          uuid        NOT NULL,
  statement_ref      uuid        NOT NULL,
  cooperative_ref    uuid        NOT NULL,
  plot_ref           uuid        NOT NULL,
  season_ref         uuid        NULL,
  collected_on       date        NOT NULL,
  rolls              integer     NOT NULL CHECK (rolls >= 0),
  revision           integer     NOT NULL CHECK (revision >= 1),
  receipt_id         uuid        NOT NULL,
  source_tenant_id   uuid        NOT NULL,
  published_at       timestamptz NOT NULL,
  received_at        timestamptz NOT NULL,
  -- Phản hồi hiện hành của HTX (bản mới nhất, có thể cho revision cũ hơn).
  response_id        uuid        NULL,
  response_revision  integer     NULL,
  response_outcome   text        NULL CHECK (response_outcome IS NULL OR response_outcome IN ('confirmed', 'disputed')),
  response_htx_rolls integer     NULL,
  response_reason    text        NULL,
  responded_at       timestamptz NULL,
  sync_seq           bigint      NOT NULL DEFAULT nextval('pf_cropcycle.collection_sync_seq'),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, statement_ref),
  CHECK ((response_id IS NULL) = (response_outcome IS NULL)),
  CHECK ((response_id IS NULL) = (response_revision IS NULL))
);
CREATE INDEX collection_statement_view_coop ON pf_cropcycle.collection_statement_view (tenant_id, cooperative_ref, collected_on DESC, statement_ref);
CREATE INDEX collection_statement_view_plot ON pf_cropcycle.collection_statement_view (tenant_id, plot_ref);
CREATE INDEX collection_statement_view_sync ON pf_cropcycle.collection_statement_view (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_cropcycle.collection_statement_view');

-- ---------------------------------------------------------------------------
-- 4. Sổ phản hồi của HTX
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_cropcycle.collection_response_feed_seq;

CREATE TABLE pf_cropcycle.collection_response (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                -- 'statement:<statement_ref>'
  seq                bigint      NOT NULL,
  statement_ref      uuid        NOT NULL,
  statement_revision integer     NOT NULL CHECK (statement_revision >= 1),
  cooperative_ref    uuid        NOT NULL,
  plot_ref           uuid        NOT NULL,
  mkg_rolls          integer     NOT NULL CHECK (mkg_rolls >= 0),   -- số cuộn của bảng kê HTX đã xem
  outcome            text        NOT NULL CHECK (outcome IN ('confirmed', 'disputed')),
  htx_rolls          integer     NOT NULL CHECK (htx_rolls >= 0),
  reason             text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  source_tenant_id   uuid        NOT NULL,
  feed_seq           bigint      NOT NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,                    -- phản hồi trước cho CÙNG revision
  correction_reason  text        NULL CHECK (correction_reason IS NULL OR length(correction_reason) BETWEEN 10 AND 500),
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  -- Cùng quy tắc với mkg_fieldops.htx_confirmation (RB-6 chiều mua).
  CHECK (outcome <> 'confirmed' OR htx_rolls = mkg_rolls),
  CHECK (outcome <> 'disputed' OR (reason IS NOT NULL AND htx_rolls <> mkg_rolls))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX collection_response_idem      ON pf_cropcycle.collection_response (tenant_id, idem_key);
CREATE INDEX collection_response_stream    ON pf_cropcycle.collection_response (tenant_id, stream_key, seq);
CREATE INDEX collection_response_statement ON pf_cropcycle.collection_response (tenant_id, statement_ref, seq);
CREATE INDEX collection_response_feed      ON pf_cropcycle.collection_response (tenant_id, source_tenant_id, feed_seq);
CREATE INDEX collection_response_id        ON pf_cropcycle.collection_response (id);
SELECT shared.apply_ledger('pf_cropcycle.collection_response', 'pf_cropcycle.collection_response');

-- ---------------------------------------------------------------------------
-- 5. Quyền của cropcycle_app (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_cropcycle.collection_valve_receipt, pf_cropcycle.collection_response TO cropcycle_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_cropcycle.collection_valve_receipt, pf_cropcycle.collection_response FROM cropcycle_app;
-- DELETE trên bảng chiếu chỉ để dựng lại từ sổ (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_cropcycle.collection_schedule_view, pf_cropcycle.collection_statement_view TO cropcycle_app;
GRANT USAGE ON SEQUENCE pf_cropcycle.collection_sync_seq, pf_cropcycle.collection_response_feed_seq TO cropcycle_app;
