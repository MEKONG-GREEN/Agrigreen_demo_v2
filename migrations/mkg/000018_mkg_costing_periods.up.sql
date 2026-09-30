-- mkg_costing (2/2): kỳ tháng, đóng kỳ theo mẫu mục 12 (202 + job, ảnh chụp đầu vào bất biến có băm),
-- kết quả đóng kỳ, quyết toán HTX (proto PM-01..04), chia doanh thu carbon (tạm, chờ khách)
-- (docs/design/mkg.md §5.6; docs/dev/g4-conventions.md §1, §2).
--
--   period_event          SỔ vòng đời kỳ: opened → close_requested → closed → posted. stream 'period:<id>'
--   period_current        bảng chiếu kỳ (một mã YYYY-MM mỗi tenant). Kỳ đã đóng KHÔNG mở lại
--   close_job             bảng vận hành job đóng kỳ: ảnh chụp đầu vào (jsonb) + SHA-256, bất biến trừ trạng thái;
--                         người đóng ≠ người lập bút toán điều chỉnh của kỳ (R-56: CHECK + trigger)
--   period_summary        SỔ kết quả đóng kỳ (dòng tổng hợp tất định, result_set_sha256). stream 'period:<id>'
--   settlement_event      SỔ quyết toán HTX. stream 'settlement:<id>'. approved_by ≠ prepared_by (CHECK)
--   settlement_current    bảng chiếu quyết toán (một lệnh gom một quyết toán gốc)
--   carbon_revenue_event  SỔ doanh thu carbon đã ghi + phần chia 45 % / 70-15-15 (tham số tạm, chờ khách)
--
-- Chốt kỳ: trigger BEFORE INSERT trên cost_entry / settlement_event (approved) / carbon_revenue_event
-- đọc period_current FOR SHARE — yêu cầu đóng kỳ khóa FOR UPDATE rồi chuyển closing trong cùng giao
-- dịch với ảnh chụp, nên không bút toán nào lọt vào kỳ sau khi ảnh chụp đã chụp.

-- ---------------------------------------------------------------------------
-- 1. Kỳ
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.period_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'period:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  period_code       text        NOT NULL CHECK (period_code ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  action            text        NOT NULL CHECK (action IN ('opened', 'close_requested', 'closed', 'posted')),
  from_at           timestamptz NOT NULL,
  to_at             timestamptz NOT NULL,
  close_run_id      uuid        NULL,
  journal_ref       uuid        NULL,
  external_ref      text        NULL CHECK (external_ref IS NULL OR length(external_ref) <= 200),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (to_at > from_at),
  CHECK (action NOT IN ('close_requested', 'closed', 'posted') OR close_run_id IS NOT NULL),
  CHECK (action <> 'posted' OR journal_ref IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_event_idem   ON mkg_costing.period_event (tenant_id, idem_key);
CREATE INDEX period_event_stream ON mkg_costing.period_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_costing.period_event', 'mkg_costing.period_event');

CREATE TABLE mkg_costing.period_current (
  tenant_id     uuid        NOT NULL,
  period_id     uuid        NOT NULL,
  code          text        NOT NULL,
  from_at       timestamptz NOT NULL,
  to_at         timestamptz NOT NULL,
  status        text        NOT NULL CHECK (status IN ('open', 'closing', 'closed', 'posted')),
  opened_at     timestamptz NOT NULL,
  close_run_id  uuid        NULL,
  closed_by     uuid        NULL,
  closed_at     timestamptz NULL,
  journal_ref   uuid        NULL,
  posted_at     timestamptz NULL,
  last_seq      bigint      NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, period_id),
  UNIQUE (tenant_id, code),
  CHECK (status = 'open' OR close_run_id IS NOT NULL),
  CHECK (status NOT IN ('closed', 'posted') OR (closed_by IS NOT NULL AND closed_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('mkg_costing.period_current');

-- Kỳ không bao giờ lùi trạng thái (không mở lại).
CREATE FUNCTION mkg_costing.fn_period_forward() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_rank_old int := array_position(ARRAY['open', 'closing', 'closed', 'posted'], OLD.status);
  v_rank_new int := array_position(ARRAY['open', 'closing', 'closed', 'posted'], NEW.status);
BEGIN
  IF v_rank_new < v_rank_old THEN
    RAISE EXCEPTION 'period_current %: kỳ % không mở lại (% → %)', OLD.period_id, OLD.code, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_period_forward BEFORE UPDATE ON mkg_costing.period_current
  FOR EACH ROW EXECUTE FUNCTION mkg_costing.fn_period_forward();

-- Bút toán chỉ vào kỳ đang MỞ. FOR SHARE xếp hàng sau yêu cầu đóng kỳ (FOR UPDATE). Tham số trigger là
-- tên bảng cha (TG_TABLE_NAME là tên phân vùng).
CREATE FUNCTION mkg_costing.fn_entry_period_open() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_status text;
BEGIN
  IF TG_ARGV[0] = 'cost_entry' THEN
    SELECT status INTO v_status FROM mkg_costing.period_current
     WHERE tenant_id = NEW.tenant_id AND code = NEW.period_code FOR SHARE;
  ELSIF TG_ARGV[0] = 'settlement_event' THEN
    IF NEW.action <> 'approved' THEN
      RETURN NEW;
    END IF;
    SELECT status INTO v_status FROM mkg_costing.period_current
     WHERE tenant_id = NEW.tenant_id AND period_id = NEW.period_ref FOR SHARE;
  ELSE
    SELECT status INTO v_status FROM mkg_costing.period_current
     WHERE tenant_id = NEW.tenant_id AND period_id = NEW.period_ref FOR SHARE;
  END IF;
  IF v_status IS DISTINCT FROM 'open' THEN
    RAISE EXCEPTION 'costing: kỳ không mở (%) — kỳ đã đóng không nhận bút toán, dùng điều chỉnh ở kỳ đang mở',
      coalesce(v_status, 'chưa mở') USING ERRCODE = 'check_violation', CONSTRAINT = 'costing_period_not_open';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_period_open BEFORE INSERT ON mkg_costing.cost_entry
  FOR EACH ROW EXECUTE FUNCTION mkg_costing.fn_entry_period_open('cost_entry');

-- ---------------------------------------------------------------------------
-- 2. Job đóng kỳ (mục 12). Bảng vận hành: chỉ đổi được cột trạng thái.
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.close_job (
  id                    uuid        PRIMARY KEY,          -- UUIDv7 = close_run_id
  tenant_id             uuid        NOT NULL,
  period_id             uuid        NOT NULL,
  period_code           text        NOT NULL,
  status                text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  requested_by          uuid        NOT NULL,
  requested_at          timestamptz NOT NULL DEFAULT now(),
  as_of                 timestamptz NOT NULL,             -- mốc ảnh chụp (recorded_at < as_of)
  snapshot              jsonb       NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
  input_snapshot_sha256 text        NOT NULL CHECK (input_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  adjusting_authors     uuid[]      NOT NULL,             -- người lập bút toán điều chỉnh của kỳ (R-56)
  idem_key              text        NOT NULL,
  rule_version          text        NOT NULL,
  error_code            text        NULL,
  completed_at          timestamptz NULL,
  attempts              integer     NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, idem_key),
  CHECK (NOT (requested_by = ANY (adjusting_authors))),   -- R-56 lớp DB: người đóng ≠ người lập điều chỉnh
  CHECK ((status = 'completed') = (completed_at IS NOT NULL AND error_code IS NULL))
);
CREATE UNIQUE INDEX close_job_one_live ON mkg_costing.close_job (tenant_id, period_id) WHERE status IN ('queued', 'completed');
SELECT shared.apply_tenant_rls('mkg_costing.close_job');

CREATE FUNCTION mkg_costing.fn_close_job_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    -- adjusting_authors phải đủ: không tin ứng dụng, đối chiếu lại sổ.
    IF EXISTS (SELECT 1 FROM mkg_costing.cost_entry
                WHERE tenant_id = NEW.tenant_id AND period_code = NEW.period_code
                  AND entry_kind = 'adjusting' AND actor_ref = NEW.requested_by) THEN
      RAISE EXCEPTION 'close_job: người đóng kỳ % là người lập bút toán điều chỉnh của kỳ (R-56)', NEW.period_code
        USING ERRCODE = 'check_violation', CONSTRAINT = 'close_job_closer_not_adjuster';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status IN ('completed', 'failed') THEN
    RAISE EXCEPTION 'close_job %: job đã % là bất biến', OLD.id, OLD.status USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.period_id <> OLD.period_id OR NEW.snapshot <> OLD.snapshot
     OR NEW.input_snapshot_sha256 <> OLD.input_snapshot_sha256 OR NEW.requested_by <> OLD.requested_by
     OR NEW.requested_at <> OLD.requested_at OR NEW.as_of <> OLD.as_of OR NEW.idem_key <> OLD.idem_key
     OR NEW.adjusting_authors <> OLD.adjusting_authors THEN
    RAISE EXCEPTION 'close_job %: ảnh chụp đầu vào bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_close_job_guard BEFORE INSERT OR UPDATE ON mkg_costing.close_job
  FOR EACH ROW EXECUTE FUNCTION mkg_costing.fn_close_job_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON mkg_costing.close_job
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON mkg_costing.close_job
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 3. Kết quả đóng kỳ
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.period_summary (
  id                    uuid        NOT NULL,
  tenant_id             uuid        NOT NULL,
  stream_key            text        NOT NULL,             -- 'period:<period_id>'
  seq                   bigint      NOT NULL,
  period_id             uuid        NOT NULL,
  period_code           text        NOT NULL,
  close_run_id          uuid        NOT NULL,
  from_at               timestamptz NOT NULL,
  to_at                 timestamptz NOT NULL,
  lines                 jsonb       NOT NULL CHECK (jsonb_typeof(lines) = 'array'),
  line_count            integer     NOT NULL CHECK (line_count >= 0),
  input_snapshot_sha256 text        NOT NULL CHECK (input_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  result_set_sha256     text        NOT NULL CHECK (result_set_sha256 ~ '^[0-9a-f]{64}$'),
  closed_by             uuid        NOT NULL,
  event_time            timestamptz NOT NULL,
  recorded_at           timestamptz NOT NULL DEFAULT now(),
  supersedes_id         uuid        NULL,
  correction_reason     text        NULL,
  actor_ref             uuid        NOT NULL,
  device_id             text        NULL,
  idem_key              text        NOT NULL,
  rule_version          text        NOT NULL,
  prev_hash             bytea       NOT NULL,
  row_hash              bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (supersedes_id IS NULL AND correction_reason IS NULL)  -- kết quả đóng kỳ không bao giờ bị thay
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_summary_idem   ON mkg_costing.period_summary (tenant_id, idem_key);
CREATE INDEX period_summary_stream ON mkg_costing.period_summary (tenant_id, stream_key, seq);
CREATE INDEX period_summary_run    ON mkg_costing.period_summary (tenant_id, close_run_id);
SELECT shared.apply_ledger('mkg_costing.period_summary', 'mkg_costing.period_summary');

-- ---------------------------------------------------------------------------
-- 4. Quyết toán HTX (proto PM-01..04)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.settlement_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,            -- 'settlement:<settlement_id>'
  seq                  bigint        NOT NULL,
  settlement_id        uuid          NOT NULL,
  action               text          NOT NULL CHECK (action IN ('drafted', 'refreshed', 'priced', 'approved', 'paid', 'cancelled')),
  kind                 text          NOT NULL CHECK (kind IN ('original', 'adjustment')),
  status               text          NOT NULL CHECK (status IN ('awaiting_weighing', 'pending_confirmation', 'approved', 'paid', 'cancelled')),
  collection_order_ref uuid          NOT NULL,
  htx_ref              uuid          NOT NULL,
  harvest_date         date          NOT NULL,
  contract_ref         uuid          NULL,
  contract_version     integer       NULL,
  basis                text          NULL CHECK (basis IS NULL OR basis IN ('per_tonne', 'per_bale')),
  price_source         text          NULL CHECK (price_source IS NULL OR price_source IN ('contract', 'manual')),
  unit_price           numeric(18,4) NULL CHECK (unit_price IS NULL OR unit_price > 0),
  rolls                integer       NULL CHECK (rolls IS NULL OR rolls >= 0),
  mass_kg              numeric(14,3) NULL CHECK (mass_kg IS NULL OR mass_kg >= 0),
  gross_amount         numeric(18,2) NULL,
  deductions           numeric(18,2) NOT NULL CHECK (deductions >= 0),
  amount               numeric(18,2) NULL,
  blocked_reason       text          NULL CHECK (blocked_reason IS NULL OR blocked_reason IN ('no_contract', 'awaiting_weighing', 'no_rolls')),
  due_on               date          NULL,
  period_ref           uuid          NULL,
  prepared_by          uuid          NOT NULL,
  approved_by          uuid          NULL,
  reason               text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  param_pins           jsonb         NOT NULL CHECK (jsonb_typeof(param_pins) = 'object'),
  event_time           timestamptz   NOT NULL,
  recorded_at          timestamptz   NOT NULL DEFAULT now(),
  supersedes_id        uuid          NULL,                -- quyết toán điều chỉnh: quyết toán gốc đã duyệt/đã trả
  correction_reason    text          NULL,
  actor_ref            uuid          NOT NULL,
  device_id            text          NULL,
  idem_key             text          NOT NULL,
  rule_version         text          NOT NULL,
  prev_hash            bytea         NOT NULL,
  row_hash             bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((kind = 'adjustment') = (supersedes_id IS NOT NULL)),
  CHECK (approved_by IS NULL OR approved_by <> prepared_by),                       -- R-56 lớp DB
  CHECK (action <> 'approved' OR (approved_by IS NOT NULL AND amount IS NOT NULL AND due_on IS NOT NULL
                                  AND period_ref IS NOT NULL AND basis IS NOT NULL AND unit_price IS NOT NULL)),
  CHECK (action <> 'cancelled' OR reason IS NOT NULL),
  CHECK (action <> 'approved' OR kind = 'adjustment' OR amount > 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX settlement_event_idem   ON mkg_costing.settlement_event (tenant_id, idem_key);
CREATE INDEX settlement_event_stream ON mkg_costing.settlement_event (tenant_id, stream_key, seq);
CREATE INDEX settlement_event_period ON mkg_costing.settlement_event (tenant_id, period_ref) WHERE action = 'approved';
SELECT shared.apply_ledger('mkg_costing.settlement_event', 'mkg_costing.settlement_event');
CREATE TRIGGER trg_period_open BEFORE INSERT ON mkg_costing.settlement_event
  FOR EACH ROW EXECUTE FUNCTION mkg_costing.fn_entry_period_open('settlement_event');

CREATE TABLE mkg_costing.settlement_current (
  tenant_id            uuid          NOT NULL,
  settlement_id        uuid          NOT NULL,
  kind                 text          NOT NULL,
  status               text          NOT NULL,
  collection_order_ref uuid          NOT NULL,
  htx_ref              uuid          NOT NULL,
  harvest_date         date          NOT NULL,
  contract_ref         uuid          NULL,
  contract_version     integer       NULL,
  basis                text          NULL,
  price_source         text          NULL,
  unit_price           numeric(18,4) NULL,
  rolls                integer       NULL,
  mass_kg              numeric(14,3) NULL,
  gross_amount         numeric(18,2) NULL,
  deductions           numeric(18,2) NOT NULL,
  amount               numeric(18,2) NULL,
  blocked_reason       text          NULL,
  due_on               date          NULL,
  period_ref           uuid          NULL,
  prepared_by          uuid          NOT NULL,
  approved_by          uuid          NULL,
  approved_at          timestamptz   NULL,
  supersedes_id        uuid          NULL,
  rule_version         text          NOT NULL,
  last_seq             bigint        NOT NULL,
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, settlement_id),
  CHECK (approved_by IS NULL OR approved_by <> prepared_by)
);
-- proto PM-01: một lệnh gom một quyết toán gốc (trừ đã hủy).
CREATE UNIQUE INDEX settlement_current_one_per_order ON mkg_costing.settlement_current (tenant_id, collection_order_ref)
  WHERE kind = 'original' AND status <> 'cancelled';
SELECT shared.apply_tenant_rls('mkg_costing.settlement_current');

-- ---------------------------------------------------------------------------
-- 5. Doanh thu carbon + phần chia (tham số tạm, chờ khách; không tính tCO2e — pf/mrv, C-1)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.carbon_revenue_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'period:<period_id>'
  seq               bigint        NOT NULL,
  period_ref        uuid          NOT NULL,
  period_code       text          NOT NULL,
  revenue_amount    numeric(18,2) NOT NULL CHECK (revenue_amount <> 0),
  retained_amount   numeric(18,2) NOT NULL,
  farmers_amount    numeric(18,2) NOT NULL,
  authority_amount  numeric(18,2) NOT NULL,
  verifier_amount   numeric(18,2) NOT NULL,
  retained_pct      numeric(5,2)  NOT NULL,
  farmers_pct       numeric(5,2)  NOT NULL,
  authority_pct     numeric(5,2)  NOT NULL,
  verifier_pct      numeric(5,2)  NOT NULL,
  param_pins        jsonb         NOT NULL CHECK (jsonb_typeof(param_pins) = 'object'),
  reference         text          NULL CHECK (reference IS NULL OR length(reference) <= 200),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (retained_amount + farmers_amount + authority_amount + verifier_amount = revenue_amount),
  CHECK (farmers_pct + authority_pct + verifier_pct = 100)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX carbon_revenue_event_idem   ON mkg_costing.carbon_revenue_event (tenant_id, idem_key);
CREATE INDEX carbon_revenue_event_stream ON mkg_costing.carbon_revenue_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_costing.carbon_revenue_event', 'mkg_costing.carbon_revenue_event');
CREATE TRIGGER trg_period_open BEFORE INSERT ON mkg_costing.carbon_revenue_event
  FOR EACH ROW EXECUTE FUNCTION mkg_costing.fn_entry_period_open('carbon_revenue_event');

-- ---------------------------------------------------------------------------
-- 6. Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_costing.period_event, mkg_costing.period_summary, mkg_costing.settlement_event,
                        mkg_costing.carbon_revenue_event TO costing_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_costing.period_event, mkg_costing.period_summary, mkg_costing.settlement_event,
                                   mkg_costing.carbon_revenue_event FROM costing_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_costing.period_current, mkg_costing.settlement_current TO costing_app;
GRANT SELECT, INSERT, UPDATE ON mkg_costing.close_job TO costing_app;
REVOKE DELETE, TRUNCATE ON mkg_costing.close_job FROM costing_app;
