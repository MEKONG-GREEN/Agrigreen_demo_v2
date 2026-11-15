-- Immutable metadata extends the existing template ledger without changing old rows.
-- Native template remains the canonical result, actor audit and idempotency identity.
SET LOCAL lock_timeout='5s';
CREATE FUNCTION pf_forms.definition_digest303(p_tenant uuid,p_id uuid,p_profile text,p_code text,p_version bigint,p_title text,p_fields jsonb,p_fixture boolean,p_runtime jsonb)
RETURNS text LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('schema','forms.template/v1','tenant_id',p_tenant,'id',p_id,'profile',p_profile,'code',p_code,'version',p_version,'title',p_title,'fields',p_fields,'fixture',p_fixture,'runtime',p_runtime)::text,'UTF8')),'hex')
$$;

CREATE TABLE pf_forms.template_definition (
 template_id uuid NOT NULL,
 template_recorded_at timestamptz NOT NULL,
 actual_base bigint NOT NULL CHECK(actual_base>=0),
 intent_key text NULL CHECK(intent_key IS NULL OR char_length(intent_key) BETWEEN 8 AND 200),
 runtime jsonb NOT NULL CHECK(runtime='null'::jsonb OR (jsonb_typeof(runtime)='object' AND runtime->>'schema'='forms.runtime/v1' AND runtime->>'contract' ~ '^[a-z][a-z0-9_.-]{0,63}/v[1-9][0-9]{0,3}$' AND jsonb_typeof(runtime->'config')='object' AND octet_length((runtime->'config')::text)<=70000)),
 intent jsonb NOT NULL CHECK(jsonb_typeof(intent)='object' AND octet_length(intent::text)<=1048576),
 intent_hash text NOT NULL CHECK(intent_hash ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 id uuid NOT NULL,tenant_id uuid NOT NULL,stream_key text NOT NULL,seq bigint NOT NULL,
 event_time timestamptz NOT NULL,recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL,correction_reason text NULL,actor_ref uuid NOT NULL,device_id text NULL,
 idem_key text NOT NULL,rule_version text NOT NULL,prev_hash bytea NOT NULL,row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 FOREIGN KEY(template_id,template_recorded_at) REFERENCES pf_forms.template(id,recorded_at),
 CHECK(supersedes_id IS NULL AND correction_reason IS NULL),
 CHECK(stream_key='definition:'||template_id::text AND seq=1 AND rule_version='forms.definition/v1')
) PARTITION BY RANGE(recorded_at);
CREATE INDEX template_definition_identity303 ON pf_forms.template_definition(tenant_id,template_id);
CREATE INDEX template_definition_intent303 ON pf_forms.template_definition(tenant_id,intent_key) WHERE intent_key IS NOT NULL;
SELECT shared.apply_ledger('pf_forms.template_definition','pf_forms.template_definition');

CREATE TABLE pf_forms.template_use (
 template_id uuid NOT NULL,
 template_recorded_at timestamptz NOT NULL,
 template_version bigint NOT NULL CHECK(template_version>0),
 definition_digest text NOT NULL CHECK(definition_digest ~ '^[0-9a-f]{64}$'),
 runtime_contract text NOT NULL CHECK(runtime_contract ~ '^[a-z][a-z0-9_.-]{0,63}/v[1-9][0-9]{0,3}$'),
 has_data boolean NOT NULL,
 runtime_ref uuid NOT NULL CHECK(runtime_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object'),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 id uuid NOT NULL,tenant_id uuid NOT NULL,stream_key text NOT NULL,seq bigint NOT NULL,
 event_time timestamptz NOT NULL,recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL,correction_reason text NULL,actor_ref uuid NOT NULL,device_id text NULL,
 idem_key text NOT NULL,rule_version text NOT NULL,prev_hash bytea NOT NULL,row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 FOREIGN KEY(template_id,template_recorded_at) REFERENCES pf_forms.template(id,recorded_at),
 CHECK(supersedes_id IS NULL AND correction_reason IS NULL),
 CHECK(stream_key='template_use:'||template_id::text AND rule_version='forms.template_use/v1')
) PARTITION BY RANGE(recorded_at);
CREATE INDEX template_use_identity303 ON pf_forms.template_use(tenant_id,template_id);
SELECT shared.apply_ledger('pf_forms.template_use','pf_forms.template_use');

-- Existing columns are byte-semantically equal JSON values; only additional
-- columns are allowed. Unknown legacy coverage is never treated as unused.
CREATE FUNCTION pf_forms.additive_fields303(p_old jsonb,p_new jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_old) old_field WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_new) new_field WHERE new_field=old_field))
$$;
CREATE FUNCTION pf_forms.guard_template_definition303() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE t pf_forms.template; old_t record;
BEGIN
 SELECT * INTO STRICT t FROM pf_forms.template WHERE tenant_id=NEW.tenant_id AND id=NEW.template_id AND recorded_at=NEW.template_recorded_at;
 IF NEW.intent_key IS NOT NULL THEN
  PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.template.intent:'||t.tenant_id::text||':'||NEW.intent_key,0));
  IF EXISTS(SELECT 1 FROM pf_forms.template_definition d WHERE d.tenant_id=t.tenant_id AND d.intent_key=NEW.intent_key) THEN
   RAISE EXCEPTION 'definition intent key already used' USING ERRCODE='unique_violation',CONSTRAINT='template_definition_intent303';
  END IF;
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.template.stream:'||t.tenant_id::text||':'||t.profile||':'||t.code,0));
 IF EXISTS(SELECT 1 FROM pf_forms.template_definition d WHERE d.tenant_id=t.tenant_id AND d.template_id=t.id)
 OR NEW.actor_ref<>t.actor_ref OR NEW.idem_key<>t.idem_key OR NEW.actual_base<>t.seq-1
 OR NEW.intent->>'profile' IS DISTINCT FROM t.profile OR NEW.intent->>'code' IS DISTINCT FROM t.code
 OR NEW.intent->>'title' IS DISTINCT FROM t.title OR NEW.intent->'fields' IS DISTINCT FROM t.fields
 OR NEW.intent->'fixture' IS DISTINCT FROM to_jsonb(t.fixture) OR NEW.intent->'runtime' IS DISTINCT FROM NEW.runtime
 OR (NEW.intent->'base_version'<>'null'::jsonb AND (NEW.intent->>'base_version')::bigint<>NEW.actual_base) THEN
  RAISE EXCEPTION 'definition metadata lacks exact native version receipt' USING ERRCODE='check_violation',CONSTRAINT='template_definition_native303';
 END IF;
 IF NEW.runtime<>'null'::jsonb OR EXISTS(SELECT 1 FROM pf_forms.template_definition d JOIN pf_forms.template x ON x.tenant_id=d.tenant_id AND x.id=d.template_id AND x.recorded_at=d.template_recorded_at WHERE x.tenant_id=t.tenant_id AND x.profile=t.profile AND x.code=t.code AND d.runtime<>'null'::jsonb) THEN
  IF NEW.runtime='null'::jsonb THEN RAISE EXCEPTION 'runtime metadata cannot be discarded' USING ERRCODE='AGF05'; END IF;
  IF EXISTS(SELECT 1 FROM pf_forms.template x WHERE x.tenant_id=t.tenant_id AND x.profile=t.profile AND x.code=t.code AND x.seq<t.seq AND (
   NOT EXISTS(SELECT 1 FROM pf_forms.template_definition d WHERE d.tenant_id=x.tenant_id AND d.template_id=x.id)
   OR EXISTS(SELECT 1 FROM pf_forms.template_use u WHERE u.tenant_id=x.tenant_id AND u.template_id=x.id AND u.has_data)
   OR EXISTS(SELECT 1 FROM pf_forms.submission sub WHERE sub.tenant_id=x.tenant_id AND sub.template_id=x.id)
   OR EXISTS(SELECT 1 FROM pf_forms.held_submission h WHERE h.tenant_id=x.tenant_id AND h.template_id=x.id))) THEN
   SELECT fields INTO old_t FROM pf_forms.template WHERE tenant_id=t.tenant_id AND profile=t.profile AND code=t.code AND seq=t.seq-1;
   IF NOT pf_forms.additive_fields303(old_t.fields,t.fields) THEN RAISE EXCEPTION 'template with data or unknown legacy only allows new columns' USING ERRCODE='AGF05'; END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_template_definition303 BEFORE INSERT ON pf_forms.template_definition FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_template_definition303();

-- First data and edit linearize on the SAME profile/code stream lock. A
-- historical version remains valid after an edit that happened before its data.
CREATE FUNCTION pf_forms.fence_template_use303(p_tenant uuid,p_template uuid) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE t pf_forms.template;
BEGIN
 SELECT * INTO STRICT t FROM pf_forms.template WHERE tenant_id=p_tenant AND id=p_template;
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.template.stream:'||t.tenant_id::text||':'||t.profile||':'||t.code,0));
END $$;
CREATE FUNCTION pf_forms.guard_template_use303() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE t pf_forms.template; r jsonb;
BEGIN
 SELECT * INTO STRICT t FROM pf_forms.template WHERE tenant_id=NEW.tenant_id AND id=NEW.template_id AND recorded_at=NEW.template_recorded_at;
 PERFORM pf_forms.fence_template_use303(t.tenant_id,t.id);
 SELECT coalesce((SELECT runtime FROM pf_forms.template_definition WHERE tenant_id=t.tenant_id AND template_id=t.id),'null'::jsonb) INTO r;
 IF NEW.template_version<>t.seq OR NEW.definition_digest<>pf_forms.definition_digest303(t.tenant_id,t.id,t.profile,t.code,t.seq,t.title,t.fields,t.fixture,r)
 OR NEW.request->'identity'->>'digest' IS DISTINCT FROM NEW.definition_digest
 OR (NEW.request->'identity'->>'id')::uuid IS DISTINCT FROM t.id
 OR (NEW.request->'identity'->>'tenant_id')::uuid IS DISTINCT FROM t.tenant_id
 OR (NEW.request->'identity'->>'version')::bigint IS DISTINCT FROM t.seq
 OR NEW.request->'identity'->>'profile' IS DISTINCT FROM t.profile OR NEW.request->'identity'->>'code' IS DISTINCT FROM t.code
 OR (NEW.request->>'kind'='data' AND NOT NEW.has_data) OR NEW.request->>'kind' NOT IN ('runtime','data')
 OR NEW.request->>'runtime' IS DISTINCT FROM NEW.runtime_contract OR (NEW.request->>'reference')::uuid IS DISTINCT FROM NEW.runtime_ref THEN
 RAISE EXCEPTION 'runtime use lacks exact immutable native template evidence' USING ERRCODE='check_violation',CONSTRAINT='template_use_native303'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_template_use303 BEFORE INSERT ON pf_forms.template_use FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_template_use303();
CREATE FUNCTION pf_forms.guard_native_template_use303() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN PERFORM pf_forms.fence_template_use303(NEW.tenant_id,NEW.template_id); RETURN NEW; END $$;
CREATE TRIGGER zzz_period_template_use303 BEFORE INSERT ON pf_forms.period FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_native_template_use303();
CREATE TRIGGER zzz_native_template_use303 BEFORE INSERT ON pf_forms.submission FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_native_template_use303();
CREATE TRIGGER zzz_held_template_use303 BEFORE INSERT ON pf_forms.held_submission FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_native_template_use303();
GRANT SELECT,INSERT ON pf_forms.template_definition,pf_forms.template_use TO r_forms;
