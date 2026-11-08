-- #244: report draft save audit and receipt commit atomically with the draft.
-- apply_ledger applies tenant RLS and append-only/hash guards; no backfill.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.khtc_report_save_operation (
 id uuid NOT NULL,
 tenant_id uuid NOT NULL,
 stream_key text NOT NULL,
 seq bigint NOT NULL,
 operation_id uuid NOT NULL,
 period_id uuid NOT NULL,
 request_key text NOT NULL CHECK(request_key ~ '^[0-9a-f]{64}$'),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 draft_id uuid NOT NULL,
 base_draft_id uuid NULL,
 base_version bigint NOT NULL CHECK(base_version >= 0),
 draft_version bigint NOT NULL CHECK(draft_version > base_version),
 changes jsonb NOT NULL CHECK(jsonb_typeof(changes)='array' AND octet_length(changes::text)<=1048576),
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
 CHECK(id=operation_id),
 CHECK(stream_key='khtc_report_save_operation:'||operation_id::text),
 CHECK((base_draft_id IS NULL)=(base_version=0)),
 CHECK(supersedes_id IS NULL AND correction_reason IS NULL),
 CHECK(rule_version='report-save-receipt/v1')
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_extension.khtc_report_save_operation','pf_extension.khtc_report_save_operation');
CREATE INDEX khtc_report_save_operation_identity ON pf_extension.khtc_report_save_operation(tenant_id,operation_id);
CREATE INDEX khtc_report_save_operation_key ON pf_extension.khtc_report_save_operation(tenant_id,request_key);
CREATE INDEX khtc_report_save_operation_period ON pf_extension.khtc_report_save_operation(tenant_id,period_id,draft_version DESC);
GRANT SELECT,INSERT ON pf_extension.khtc_report_save_operation TO r_extension;

CREATE FUNCTION pf_extension.check_report_save_operation244() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE v_base uuid; v_version bigint;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_report_save_operation:'||NEW.tenant_id::text||':'||NEW.operation_id::text,0));
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_report_save_key:'||NEW.tenant_id::text||':'||NEW.request_key,0));
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_report_save_operation WHERE tenant_id=NEW.tenant_id AND (operation_id=NEW.operation_id OR request_key=NEW.request_key)) THEN
  RAISE EXCEPTION 'report save operation already exists' USING ERRCODE='23514',CONSTRAINT='khtc_report_save_operation_conflict';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pf_extension.khtc_report_draft WHERE tenant_id=NEW.tenant_id AND period_id=NEW.period_id AND id=NEW.draft_id AND seq=NEW.draft_version AND action='save' AND actor_ref=NEW.actor_ref AND event_time=NEW.event_time) THEN
  RAISE EXCEPTION 'report save receipt must identify its committed draft' USING ERRCODE='23514',CONSTRAINT='khtc_report_save_operation_draft';
 END IF;
 SELECT id,seq INTO v_base,v_version FROM pf_extension.khtc_report_draft WHERE tenant_id=NEW.tenant_id AND period_id=NEW.period_id AND seq<NEW.draft_version ORDER BY seq DESC LIMIT 1;
 IF v_base IS DISTINCT FROM NEW.base_draft_id OR coalesce(v_version,0)<>NEW.base_version THEN
  RAISE EXCEPTION 'report save receipt base does not match' USING ERRCODE='23514',CONSTRAINT='khtc_report_save_operation_base';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_report_save_operation244 BEFORE INSERT ON pf_extension.khtc_report_save_operation FOR EACH ROW EXECUTE FUNCTION pf_extension.check_report_save_operation244();
