-- W11.68 / FN-TI-02.10: durable supplemental requests on existing native KHTC tasks.
-- Append-only, tenant RLS via apply_ledger. No external I/O in these transactions.
SET LOCAL lock_timeout='5s';
CREATE TABLE pf_extension.khtc_adhoc_request (
 period_id uuid NOT NULL,
 department text NOT NULL CHECK (char_length(department) BETWEEN 1 AND 64),
 indicators jsonb NOT NULL CHECK(jsonb_typeof(indicators)='array' AND jsonb_array_length(indicators)<=20 AND octet_length(indicators::text)<=32768),
 instruction text NOT NULL CHECK (char_length(instruction) BETWEEN 1 AND 4000),
 due_at timestamptz NOT NULL,
 payload_hash text NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
 base_attempt bigint NOT NULL CHECK (base_attempt BETWEEN 0 AND 3),

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
 CHECK ((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK (correction_reason IS NULL OR length(correction_reason)>=10)
,
 CHECK(stream_key='khtc_adhoc_request:'||period_id::text||':'||department)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_adhoc_request_period ON pf_extension.khtc_adhoc_request(tenant_id,period_id,department,seq DESC);
CREATE INDEX khtc_adhoc_request_id ON pf_extension.khtc_adhoc_request(tenant_id,id);
CREATE TABLE pf_extension.khtc_adhoc_request_event (
 request_id uuid NOT NULL,
 period_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('issued','fulfilled','cancelled','failed')),
 native_submission_ref uuid NULL,
 return_applied boolean NULL,
 reason text NOT NULL DEFAULT '' CHECK(char_length(reason)<=1000),

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
 CHECK ((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK (correction_reason IS NULL OR length(correction_reason)>=10)
,
 CHECK(stream_key='khtc_adhoc_request_event:'||request_id::text),
 CHECK(action<>'fulfilled' OR native_submission_ref IS NOT NULL)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_adhoc_request_event_request ON pf_extension.khtc_adhoc_request_event(tenant_id,request_id,seq DESC);
SELECT shared.apply_ledger('pf_extension.khtc_adhoc_request','pf_extension.khtc_adhoc_request');
SELECT shared.apply_ledger('pf_extension.khtc_adhoc_request_event','pf_extension.khtc_adhoc_request_event');
GRANT SELECT,INSERT ON pf_extension.khtc_adhoc_request,pf_extension.khtc_adhoc_request_event TO r_extension;

CREATE FUNCTION pf_extension.fn_khtc_adhoc_request_bind() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 IF NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.period_id
   AND p.report_type='adhoc' AND NEW.department=ANY(p.departments)) THEN
  RAISE EXCEPTION 'request outside assigned adhoc department' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_scope';
 END IF;
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id=NEW.tenant_id AND e.period_id=NEW.period_id AND e.action IN('closed','cancelled'))
 OR EXISTS(SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id=NEW.tenant_id AND f.period_id=NEW.period_id) THEN
  RAISE EXCEPTION 'adhoc period is terminal' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_terminal';
 END IF;
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_request r
 LEFT JOIN LATERAL(SELECT x.action FROM pf_extension.khtc_adhoc_request_event x WHERE x.tenant_id=r.tenant_id AND x.request_id=r.id ORDER BY x.seq DESC LIMIT 1)e ON true
 WHERE r.tenant_id=NEW.tenant_id AND r.period_id=NEW.period_id AND r.department=NEW.department AND coalesce(e.action,'intent') IN('intent','issued')) THEN
 RAISE EXCEPTION 'department has a pending request' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_pending';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_adhoc_request_bind BEFORE INSERT ON pf_extension.khtc_adhoc_request FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_adhoc_request_bind();

CREATE FUNCTION pf_extension.fn_khtc_adhoc_event_bind() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 IF NOT EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_request r WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.request_id AND r.period_id=NEW.period_id) THEN
  RAISE EXCEPTION 'request event has no parent' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_event_scope';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_adhoc_event_bind BEFORE INSERT ON pf_extension.khtc_adhoc_request_event FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_adhoc_event_bind();

-- A stale form cannot be sealed after a new request was committed. Intent is a barrier
-- while native Return executes outside the transaction; only an issued token is usable.
CREATE FUNCTION pf_extension.fn_khtc_adhoc_sheet_token() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE request_ref uuid; request_state text;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 SELECT r.id,coalesce(e.action,'intent') INTO request_ref,request_state
 FROM pf_extension.khtc_adhoc_request r
 LEFT JOIN LATERAL(SELECT x.action FROM pf_extension.khtc_adhoc_request_event x WHERE x.tenant_id=r.tenant_id AND x.request_id=r.id ORDER BY x.seq DESC LIMIT 1)e ON true
 WHERE r.tenant_id=NEW.tenant_id AND r.period_id=NEW.period_id AND r.department=NEW.department AND coalesce(e.action,'intent') NOT IN('failed','cancelled') ORDER BY r.seq DESC LIMIT 1;
 IF request_ref IS NOT NULL AND request_state IN('intent','issued','fulfilled') THEN
  IF request_state='intent' OR coalesce(NEW.content->>'adhoc_request_id','')<>request_ref::text THEN
   RAISE EXCEPTION 'reload the supplemental request' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_changed';
  END IF;
 ELSIF coalesce(NEW.content->>'adhoc_request_id','')<>'' THEN
  RAISE EXCEPTION 'unknown supplemental request' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_changed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_00_khtc_adhoc_sheet_token BEFORE INSERT ON pf_extension.khtc_form_sheet FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_adhoc_sheet_token();

CREATE FUNCTION pf_extension.fn_khtc_adhoc_final_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 IF NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event t WHERE t.tenant_id=NEW.tenant_id AND t.period_id=NEW.period_id AND t.action IN('closed','cancelled'))
 AND EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_request r
  LEFT JOIN LATERAL(SELECT x.action FROM pf_extension.khtc_adhoc_request_event x WHERE x.tenant_id=r.tenant_id AND x.request_id=r.id ORDER BY x.seq DESC LIMIT 1)e ON true
  WHERE r.tenant_id=NEW.tenant_id AND r.period_id=NEW.period_id AND coalesce(e.action,'intent') IN('intent','issued')) THEN
  RAISE EXCEPTION 'supplemental request is pending' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_request_pending';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_00_khtc_adhoc_final_guard BEFORE INSERT ON pf_extension.khtc_report_final FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_adhoc_final_guard();

-- A durable, caller/session-bound publish intent survives a lost native response.
CREATE TABLE pf_extension.khtc_adhoc_open_intent (
 planned_period_id uuid NOT NULL,
 session_ref uuid NOT NULL,
 context_hash text NOT NULL CHECK(context_hash ~ '^[0-9a-f]{64}$'),
 session_expires_at timestamptz NOT NULL,
 native_idem_key text NOT NULL CHECK(char_length(native_idem_key) BETWEEN 1 AND 256),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 batch_hash text NOT NULL CHECK(batch_hash ~ '^[0-9a-f]{64}$'),
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object' AND octet_length(input::text)<=65536),
 plan jsonb NOT NULL CHECK(jsonb_typeof(plan)='object' AND plan->>'Type'='adhoc' AND octet_length(plan::text)<=65536),
 batch jsonb NOT NULL CHECK(jsonb_typeof(batch)='object' AND octet_length(batch::text)<=65536),
 source_refs jsonb NOT NULL CHECK(jsonb_typeof(source_refs)='array' AND jsonb_array_length(source_refs)<=10000 AND octet_length(source_refs::text)<=4194304),
 departments text[] NOT NULL CHECK(cardinality(departments) BETWEEN 1 AND 64),
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL,
 seq bigint NOT NULL, event_time timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL, correction_reason text NULL,
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL,
 rule_version text NOT NULL, prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR length(correction_reason)>=10),
 CHECK(stream_key='khtc_adhoc_open:'||actor_ref::text||':'||native_idem_key)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_adhoc_open_idem ON pf_extension.khtc_adhoc_open_intent(tenant_id,native_idem_key);
CREATE INDEX khtc_adhoc_open_ref ON pf_extension.khtc_adhoc_open_intent(tenant_id,id);
SELECT shared.apply_ledger('pf_extension.khtc_adhoc_open_intent','pf_extension.khtc_adhoc_open_intent');
GRANT SELECT,INSERT ON pf_extension.khtc_adhoc_open_intent TO r_extension;

-- Adoption never rewrites actor, pinned batch/key or planned KN identity.
CREATE TABLE pf_extension.khtc_adhoc_open_lease(
 intent_id uuid NOT NULL,
 generation bigint NOT NULL CHECK(generation>0),
 previous_session_ref uuid NOT NULL,
 session_ref uuid NOT NULL,
 expires_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 10 AND 500),
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL,
 seq bigint NOT NULL, event_time timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL, correction_reason text NULL,
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL,
 rule_version text NOT NULL, prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR length(correction_reason)>=10),
 CHECK(stream_key='khtc_adhoc_lease:'||intent_id::text)
)PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_adhoc_lease_ref ON pf_extension.khtc_adhoc_open_lease(tenant_id,intent_id,generation DESC);
SELECT shared.apply_ledger('pf_extension.khtc_adhoc_open_lease','pf_extension.khtc_adhoc_open_lease');
GRANT SELECT,INSERT ON pf_extension.khtc_adhoc_open_lease TO r_extension;
CREATE FUNCTION pf_extension.fn_khtc_adhoc_open_lease_bind() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE original pf_extension.khtc_adhoc_open_intent%ROWTYPE;
 current_session uuid; current_expiry timestamptz; current_generation bigint;
BEGIN
 SELECT * INTO original FROM pf_extension.khtc_adhoc_open_intent WHERE tenant_id=NEW.tenant_id AND id=NEW.intent_id;
 IF original.id IS NULL OR original.actor_ref<>NEW.actor_ref THEN
  RAISE EXCEPTION 'lease has no owned intent' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_lease_owner';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_adhoc_open:'||NEW.tenant_id::text||':'||original.native_idem_key,0));
 SELECT session_ref,expires_at,generation INTO current_session,current_expiry,current_generation
 FROM pf_extension.khtc_adhoc_open_lease WHERE tenant_id=NEW.tenant_id AND intent_id=NEW.intent_id ORDER BY generation DESC LIMIT 1;
 IF current_session IS NULL THEN
  current_session:=original.session_ref;current_expiry:=original.session_expires_at;current_generation:=0;
 END IF;
 IF NEW.previous_session_ref<>current_session OR NEW.session_ref=current_session OR NEW.generation<>current_generation+1
 OR current_expiry>NEW.event_time OR current_expiry='0001-01-01 00:00:00+00'::timestamptz
 OR NEW.expires_at<=NEW.event_time THEN
  RAISE EXCEPTION 'stale or unexpired lease' USING ERRCODE='check_violation',CONSTRAINT='khtc_adhoc_lease_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_adhoc_open_lease_bind BEFORE INSERT ON pf_extension.khtc_adhoc_open_lease
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_adhoc_open_lease_bind();
