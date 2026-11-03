-- #209: durable KHTC deadline consumer; public native CAS only, no foreign SQL.
CREATE TABLE pf_extension.khtc_deadline_intent (
 id uuid NOT NULL,
 tenant_id uuid NOT NULL,
 stream_key text NOT NULL,
 seq bigint NOT NULL,
 operation_id uuid NOT NULL,
 period_id uuid NOT NULL,
 request_key text NOT NULL CHECK(length(request_key)=64),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 phase text NOT NULL CHECK(phase IN('prepared','applied','undo_prepared','completed','undone','unresolved')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 outcome jsonb NULL CHECK(outcome IS NULL OR jsonb_typeof(outcome)='object'),
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
 CHECK((phase='completed')=(outcome IS NOT NULL))
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_extension.khtc_deadline_intent','pf_extension.khtc_deadline_intent');
CREATE INDEX khtc_deadline_key ON pf_extension.khtc_deadline_intent(tenant_id,request_key,seq DESC);
CREATE INDEX khtc_deadline_resource ON pf_extension.khtc_deadline_intent(tenant_id,period_id,operation_id,seq DESC);
GRANT SELECT,INSERT ON pf_extension.khtc_deadline_intent TO r_extension;

CREATE FUNCTION pf_extension.check_deadline_intent209() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior pf_extension.khtc_deadline_intent%ROWTYPE;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_deadline_key:'||NEW.tenant_id::text||':'||NEW.request_key,0));
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_deadline_period:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 SELECT * INTO prior FROM pf_extension.khtc_deadline_intent WHERE tenant_id=NEW.tenant_id AND operation_id=NEW.operation_id ORDER BY seq DESC LIMIT 1;
 IF NOT FOUND THEN
  IF NEW.phase<>'prepared' OR NEW.payload->'base' IS NULL OR NEW.payload->'expected' IS NULL
   OR NEW.payload ? 'applied' OR NEW.payload ? 'undo_applied'
   OR EXISTS(SELECT 1 FROM pf_extension.khtc_deadline_intent WHERE tenant_id=NEW.tenant_id AND request_key=NEW.request_key)
   OR EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)phase FROM pf_extension.khtc_deadline_intent WHERE tenant_id=NEW.tenant_id AND period_id=NEW.period_id ORDER BY operation_id,seq DESC)x WHERE phase NOT IN('completed','undone'))
  THEN RAISE EXCEPTION 'khtc_deadline_intent_conflict' USING ERRCODE='23514'; END IF;
 ELSE
  IF NEW.period_id<>prior.period_id OR NEW.actor_ref<>prior.actor_ref OR NEW.request_key<>prior.request_key OR NEW.request_hash<>prior.request_hash OR NEW.authority_hash<>prior.authority_hash
   OR NEW.payload->'base' IS DISTINCT FROM prior.payload->'base' OR NEW.payload->'expected' IS DISTINCT FROM prior.payload->'expected'
   OR (prior.payload ? 'applied' AND NEW.payload->'applied' IS DISTINCT FROM prior.payload->'applied')
   OR NOT ((prior.phase='prepared' AND NEW.phase IN('applied','unresolved')) OR (prior.phase='applied' AND NEW.phase IN('completed','undo_prepared','unresolved')) OR (prior.phase='undo_prepared' AND NEW.phase IN('undone','unresolved')))
  THEN RAISE EXCEPTION 'khtc_deadline_phase_conflict' USING ERRCODE='23514'; END IF;
 END IF;
 IF NEW.phase IN('applied','completed','undo_prepared','undone') AND NOT (NEW.payload ? 'applied') THEN
  RAISE EXCEPTION 'khtc_deadline_applied_required' USING ERRCODE='23514';
 END IF;
 IF NEW.phase='undone' AND NOT (NEW.payload ? 'undo_applied') THEN
  RAISE EXCEPTION 'khtc_deadline_undo_required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_deadline_intent209 BEFORE INSERT ON pf_extension.khtc_deadline_intent FOR EACH ROW EXECUTE FUNCTION pf_extension.check_deadline_intent209();

