-- W11.66: immutable G1 report drafts, Excel exports and final designation. Only pf_extension.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.khtc_report_draft (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  period_id uuid NOT NULL,
  action text NOT NULL CHECK (action IN ('save', 'refresh')),
  payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 12582912),
  input_hash text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  supersedes_id uuid NULL,
  correction_reason text NULL,
  actor_ref uuid NOT NULL,
  device_id text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT khtc_report_draft_stream CHECK (stream_key = 'khtc_report_draft:' || period_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_report_draft_period ON pf_extension.khtc_report_draft (tenant_id, period_id, seq DESC);
CREATE INDEX khtc_report_draft_id ON pf_extension.khtc_report_draft (tenant_id, id);

CREATE TABLE pf_extension.khtc_report_export (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  period_id uuid NOT NULL,
  draft_id uuid NOT NULL,
  payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 12582912),
  input_hash text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  export_filter jsonb NOT NULL CHECK (jsonb_typeof(export_filter) = 'object'),
  file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  body bytea NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 33554432),
  body_hash text NOT NULL CHECK (body_hash ~ '^[0-9a-f]{64}$'),
  provisional boolean NOT NULL,
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  supersedes_id uuid NULL,
  correction_reason text NULL,
  actor_ref uuid NOT NULL,
  device_id text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT khtc_report_export_stream CHECK (stream_key = 'khtc_report_export:' || period_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_report_export_period ON pf_extension.khtc_report_export (tenant_id, period_id, seq DESC);
CREATE INDEX khtc_report_export_id ON pf_extension.khtc_report_export (tenant_id, id);

CREATE TABLE pf_extension.khtc_report_final (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  period_id uuid NOT NULL,
  export_id uuid NOT NULL,
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  supersedes_id uuid NULL,
  correction_reason text NULL,
  actor_ref uuid NOT NULL,
  device_id text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT khtc_report_final_stream CHECK (stream_key = 'khtc_report_final:' || period_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_report_final_period ON pf_extension.khtc_report_final (tenant_id, period_id, seq DESC);
CREATE INDEX khtc_report_final_id ON pf_extension.khtc_report_final (tenant_id, id);

-- Use the pre-existing snapshot lock everywhere: finalization/refresh/close/cancel must serialize.
-- The app also holds this lock before calling cap/forms cancellation (before external side effects).
CREATE FUNCTION pf_extension.fn_khtc_report_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_final boolean;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id) THEN
    RAISE EXCEPTION 'missing KHTC period' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_report_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id AND e.action = 'cancelled') THEN
    RAISE EXCEPTION 'cancelled KHTC period' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_cancelled';
  END IF;
  SELECT EXISTS(SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) INTO v_final;
  IF v_final THEN
    RAISE EXCEPTION 'final KHTC report is immutable' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_already_final';
  END IF;
  IF TG_TABLE_NAME LIKE 'khtc_report_export%' THEN
    IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_report_draft d WHERE d.tenant_id = NEW.tenant_id AND d.period_id = NEW.period_id
                    AND d.id = NEW.draft_id AND d.input_hash = NEW.input_hash AND d.payload = NEW.payload) THEN
      RAISE EXCEPTION 'export must reference an immutable report draft' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_export_draft';
    END IF;
  ELSIF TG_TABLE_NAME LIKE 'khtc_report_final%' THEN
    IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_report_export x WHERE x.tenant_id = NEW.tenant_id AND x.period_id = NEW.period_id
                    AND x.id = NEW.export_id AND NOT x.provisional) THEN
      RAISE EXCEPTION 'provisional or missing export cannot become final' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_final_export';
    END IF;
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_khtc_report_guard BEFORE INSERT ON pf_extension.khtc_report_draft FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_report_guard();
CREATE TRIGGER trg_khtc_report_guard BEFORE INSERT ON pf_extension.khtc_report_export FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_report_guard();
CREATE TRIGGER trg_khtc_report_guard BEFORE INSERT ON pf_extension.khtc_report_final FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_report_guard();

CREATE FUNCTION pf_extension.fn_khtc_report_source_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  -- Existing snapshot guard acquired this same lock; keep an explicit acquisition for direct invocation safety.
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) THEN
    RAISE EXCEPTION 'final KHTC report freezes its sources' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_already_final';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_snapshot_report_final BEFORE INSERT ON pf_extension.khtc_snapshot
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_report_source_guard();

CREATE FUNCTION pf_extension.fn_khtc_report_period_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NEW.action IN ('closed', 'cancelled') THEN
    PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  END IF;
  IF NEW.action = 'cancelled' AND EXISTS (SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) THEN
    RAISE EXCEPTION 'final KHTC report prevents cancellation' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_final_cancel';
  END IF;
  RETURN NEW;
END $$;
-- Alphabetically before the existing transition trigger: snapshot lock -> period-event lock.
CREATE TRIGGER trg_00_khtc_report_period_guard BEFORE INSERT ON pf_extension.khtc_period_event
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_report_period_guard();

SELECT shared.apply_ledger('pf_extension.khtc_report_draft', 'pf_extension.khtc_report_draft');
SELECT shared.apply_ledger('pf_extension.khtc_report_export', 'pf_extension.khtc_report_export');
SELECT shared.apply_ledger('pf_extension.khtc_report_final', 'pf_extension.khtc_report_final');
GRANT SELECT, INSERT ON pf_extension.khtc_report_draft, pf_extension.khtc_report_export, pf_extension.khtc_report_final TO r_extension;
