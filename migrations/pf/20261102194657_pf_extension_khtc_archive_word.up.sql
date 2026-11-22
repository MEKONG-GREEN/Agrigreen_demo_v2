-- W11.69-ARCHIVE-FINAL (#199). AFTER W10.18-GUEST-OTP (#184).
-- Explicit immutable snapshot-to-Word binding and access-attempt audit only.
-- No pf_export reads/joins, no legacy inference/backfill and no role expansion.
SET LOCAL lock_timeout='5s';
CREATE TABLE pf_extension.khtc_word_binding (
 period_id uuid NOT NULL CHECK(period_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 export_id uuid NOT NULL CHECK(export_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 run_id uuid NOT NULL CHECK(run_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 export_input_hash text NOT NULL CHECK(export_input_hash ~ '^[0-9a-f]{64}$'),
 export_body_hash text NOT NULL CHECK(export_body_hash ~ '^[0-9a-f]{64}$'),
 word_version bigint NOT NULL CHECK(word_version>0),
 template_key text NOT NULL CHECK(char_length(template_key) BETWEEN 1 AND 128),
 template_version integer NOT NULL CHECK(template_version>0),
 template_hash text NOT NULL CHECK(template_hash ~ '^[0-9a-f]{64}$'),
 word_input_hash text NOT NULL CHECK(word_input_hash ~ '^[0-9a-f]{64}$'),
 body_hash text NOT NULL CHECK(body_hash ~ '^[0-9a-f]{64}$'),
 body_size bigint NOT NULL CHECK(body_size>0),
 file_name text NOT NULL CHECK(char_length(file_name) BETWEEN 1 AND 180 AND file_name !~ '[/\\]'),
 content_type text NOT NULL CHECK(content_type='application/vnd.openxmlformats-officedocument.wordprocessingml.document'),
 doc_number text NOT NULL,
 doc_number_auto boolean NOT NULL,
 word_actor_ref uuid NOT NULL CHECK(word_actor_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 word_at timestamptz NOT NULL,
 id uuid NOT NULL,
 tenant_id uuid NOT NULL,
 stream_key text NOT NULL,
 seq bigint NOT NULL,
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
 PRIMARY KEY(id,recorded_at),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR length(correction_reason)>=10),
 CHECK(stream_key='khtc_word_binding:'||run_id::text)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_word_binding_export ON pf_extension.khtc_word_binding(tenant_id,period_id,export_id,word_version DESC,run_id DESC);
CREATE INDEX khtc_word_binding_run ON pf_extension.khtc_word_binding(tenant_id,run_id);
SELECT shared.apply_ledger('pf_extension.khtc_word_binding','pf_extension.khtc_word_binding');

-- One binding per run for its entire lifetime, independent of ledger idem TTL.
CREATE FUNCTION pf_extension.fn_khtc_word_binding_once() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_word_binding:'||NEW.tenant_id::text||':'||NEW.run_id::text,0));
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_word_binding b WHERE b.tenant_id=NEW.tenant_id AND b.run_id=NEW.run_id) THEN
  RAISE EXCEPTION 'Word run already has a snapshot binding' USING ERRCODE='unique_violation',CONSTRAINT='khtc_word_binding_once';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pf_extension.khtc_report_export x WHERE x.tenant_id=NEW.tenant_id AND x.period_id=NEW.period_id AND x.id=NEW.export_id AND x.input_hash=NEW.export_input_hash AND x.body_hash=NEW.export_body_hash) THEN
  RAISE EXCEPTION 'Word binding does not identify the stored export' USING ERRCODE='check_violation',CONSTRAINT='khtc_word_binding_snapshot';
 END IF;
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id=NEW.tenant_id AND e.period_id=NEW.period_id AND e.action='cancelled') THEN
  RAISE EXCEPTION 'Word binding cannot attach to a cancelled period' USING ERRCODE='check_violation',CONSTRAINT='khtc_word_binding_cancelled';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_word_binding_once BEFORE INSERT ON pf_extension.khtc_word_binding FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_word_binding_once();

-- Attempts are committed before reading/returning stored files. expected_bytes
-- is an authorized immutable metadata budget, never a claim of network delivery.
CREATE TABLE pf_extension.khtc_archive_access (
 context_ref uuid NOT NULL CHECK(context_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 action text NOT NULL CHECK(action IN('word_list','word_attempt','zip_entry_attempt','zip_attempt')),
 period_id uuid NULL,
 export_id uuid NULL,
 run_id uuid NULL,
 format text NOT NULL CHECK(format IN('excel','word','zip')),
 expected_bytes bigint NOT NULL CHECK(expected_bytes>=0),
 file_count integer NOT NULL CHECK(file_count BETWEEN 0 AND 50),
 trace_id text NOT NULL DEFAULT '' CHECK(char_length(trace_id)<=128),
 targets jsonb NOT NULL CHECK(jsonb_typeof(targets)='array' AND jsonb_array_length(targets)<=50 AND octet_length(targets::text)<=16384),
 id uuid NOT NULL,
 tenant_id uuid NOT NULL,
 stream_key text NOT NULL,
 seq bigint NOT NULL,
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
 PRIMARY KEY(id,recorded_at),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR length(correction_reason)>=10),
 CHECK(stream_key='khtc_archive_access:'||actor_ref::text),
 CHECK(action<>'word_list' OR (format='word' AND period_id IS NOT NULL AND export_id IS NOT NULL AND run_id IS NULL)),
 CHECK(action<>'word_attempt' OR (format='word' AND period_id IS NOT NULL AND export_id IS NOT NULL AND run_id IS NOT NULL)),
 CHECK(action<>'zip_entry_attempt' OR (format IN('excel','word') AND period_id IS NOT NULL AND export_id IS NOT NULL AND (format<>'word' OR run_id IS NOT NULL))),
 CHECK(action<>'zip_attempt' OR (format='zip' AND period_id IS NULL AND export_id IS NULL AND run_id IS NULL))
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_archive_access_export ON pf_extension.khtc_archive_access(tenant_id,period_id,export_id,seq DESC);
SELECT shared.apply_ledger('pf_extension.khtc_archive_access','pf_extension.khtc_archive_access');
GRANT SELECT,INSERT ON pf_extension.khtc_word_binding,pf_extension.khtc_archive_access TO r_extension;
