-- BF-FORMS-PUBLISH-OP-RECEIPT: native batch creation evidence, no caller rollout.
SET LOCAL lock_timeout='5s';
CREATE TABLE pf_forms.publish_operation_key (
 tenant_id uuid NOT NULL CHECK(tenant_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 operation_id uuid NOT NULL CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 profile text NOT NULL CHECK(char_length(profile) BETWEEN 1 AND 64),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 request_content jsonb NOT NULL CHECK(jsonb_typeof(request_content)='object' AND octet_length(request_content::text)<=2097152),
 actor_ref uuid NOT NULL CHECK(actor_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 receipt_id uuid NOT NULL,
 receipt_recorded_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,operation_id)
);
SELECT shared.apply_tenant_rls('pf_forms.publish_operation_key');
CREATE FUNCTION pf_forms.deny_publish_operation_key224() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN RAISE EXCEPTION 'publication operation identity is permanent' USING ERRCODE='check_violation',CONSTRAINT='publish_operation_key_immutable224'; END $$;
CREATE TRIGGER trg_publish_operation_key_immutable224 BEFORE UPDATE OR DELETE ON pf_forms.publish_operation_key FOR EACH ROW EXECUTE FUNCTION pf_forms.deny_publish_operation_key224();
CREATE TRIGGER trg_publish_operation_key_no_truncate224 BEFORE TRUNCATE ON pf_forms.publish_operation_key FOR EACH STATEMENT EXECUTE FUNCTION pf_forms.deny_publish_operation_key224();

CREATE TABLE pf_forms.publish_operation_receipt (
 operation_id uuid NOT NULL,
 profile text NOT NULL CHECK(char_length(profile) BETWEEN 1 AND 64),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 request_content jsonb NOT NULL CHECK(jsonb_typeof(request_content)='object' AND octet_length(request_content::text)<=2097152),
 content jsonb NOT NULL CHECK(jsonb_typeof(content)='object' AND octet_length(content::text)<=2097152),
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
 CHECK(supersedes_id IS NULL AND correction_reason IS NULL),
 CHECK(stream_key='publish_operation:'||operation_id::text),
 CHECK(rule_version='forms.publish_operation/v1'),
 CONSTRAINT publish_operation_anchor224 FOREIGN KEY(tenant_id,operation_id) REFERENCES pf_forms.publish_operation_key(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED
) PARTITION BY RANGE(recorded_at);
CREATE INDEX publish_operation_receipt_identity224 ON pf_forms.publish_operation_receipt(tenant_id,id,recorded_at);
CREATE INDEX publish_operation_receipt_operation224 ON pf_forms.publish_operation_receipt(tenant_id,operation_id);
SELECT shared.apply_ledger('pf_forms.publish_operation_receipt','pf_forms.publish_operation_receipt');

-- Fresh INSERT checks initial due absence. Read checks ONLY immutable original
-- native tuples, never current heads/due/status after a legitimate successor.
CREATE FUNCTION pf_forms.publish_operation_proof224(p_tenant uuid,p_operation uuid,p_actor uuid,p_profile text,p_digest text,p_authority text,p_request jsonb,p_content jsonb,p_fresh boolean) RETURNS boolean
LANGUAGE plpgsql STABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE
 v_items jsonb; v_periods jsonb; v_input jsonb; v_row record; v_item jsonb; v_period jsonb; v_part text; v_count integer;
BEGIN
 IF (p_request->>'operation_id')::uuid IS DISTINCT FROM p_operation OR (p_request->>'tenant_id')::uuid IS DISTINCT FROM p_tenant
 OR (p_content->>'operation_id')::uuid IS DISTINCT FROM p_operation OR (p_content->>'tenant_id')::uuid IS DISTINCT FROM p_tenant
 OR (p_content->>'actor_ref')::uuid IS DISTINCT FROM p_actor OR p_content->>'profile' IS DISTINCT FROM p_profile
 OR p_content->>'request_digest' IS DISTINCT FROM p_digest OR p_content->>'authority_hash' IS DISTINCT FROM p_authority
 OR p_content->>'outcome' IS DISTINCT FROM 'applied' THEN RETURN false; END IF;
 v_input:=p_request->'input';v_periods:=v_input->'Periods';v_items:=p_content->'items';
 IF v_input->>'Profile' IS DISTINCT FROM p_profile OR jsonb_typeof(v_items) IS DISTINCT FROM 'array' OR jsonb_typeof(v_periods) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 v_count:=jsonb_array_length(v_items);
 IF v_count<1 OR v_count>366 OR v_count<>jsonb_array_length(v_periods)
 OR v_count<>(SELECT count(DISTINCT x->>'id') FROM jsonb_array_elements(v_items) x)
 OR v_count<>(SELECT count(DISTINCT x->>'event_id') FROM jsonb_array_elements(v_items) x) THEN RETURN false; END IF;
 FOR v_row IN SELECT value,ordinality FROM jsonb_array_elements(v_items) WITH ORDINALITY LOOP
  v_item:=v_row.value;v_period:=v_periods->((v_row.ordinality-1)::integer);
  v_part:=coalesce(nullif(v_period->>'Key',''),substring(v_period->>'From',1,10));
  IF v_item->>'key' IS DISTINCT FROM v_period->>'Key' OR v_item->>'label' IS DISTINCT FROM v_period->>'Label'
   OR (v_item->>'due_at')::timestamptz IS DISTINCT FROM (v_period->>'DueAt')::timestamptz
   OR (v_item->'applied'->>'tenant_id')::uuid IS DISTINCT FROM p_tenant
   OR (v_item->'applied'->>'period_id')::uuid IS DISTINCT FROM (v_item->>'id')::uuid
   OR v_item->'applied'->>'profile' IS DISTINCT FROM p_profile
   OR (v_item->'applied'->>'due_seq')::bigint IS DISTINCT FROM 0
   OR (v_item->'applied'->>'event_seq')::bigint<1 THEN RETURN false; END IF;
  IF (SELECT count(*) FROM pf_forms.period p WHERE p.tenant_id=p_tenant AND p.id=(v_item->>'id')::uuid)<>1 THEN RETURN false; END IF;
  IF NOT EXISTS(
   SELECT 1 FROM pf_forms.period p
   JOIN pf_forms.period_event e ON e.tenant_id=p.tenant_id AND e.period_ref=p.id
   JOIN pf_forms.period_spec s ON s.tenant_id=p.tenant_id AND s.period_id=p.id
   WHERE p.tenant_id=p_tenant AND p.id=(v_item->>'id')::uuid AND p.profile=p_profile AND p.actor_ref=p_actor
   AND p.template_id=(v_input->>'TemplateID')::uuid AND p.label=v_period->>'Label' AND p.due_at=(v_period->>'DueAt')::timestamptz
   AND to_jsonb(p.units)=v_input->'Units'
   AND p.idem_key='forms.publish_operation:'||p_operation::text||':period:'||v_part
   AND e.id=(v_item->>'event_id')::uuid AND e.recorded_at=(v_item->>'event_recorded_at')::timestamptz
   AND e.seq=(v_item->'applied'->>'event_seq')::bigint AND e.origin='native' AND e.action='published'
   AND e.profile=p_profile AND e.actor_ref=p_actor
   AND e.idem_key='forms.publish_operation:'||p_operation::text||':published:'||v_part
   AND s.profile=p_profile AND s.actor_ref=p_actor AND s.seq=1
   AND s.idem_key='forms.publish_operation:'||p_operation::text||':spec:'||v_part
   AND (v_input->>'UnitKind'='' OR s.unit_kind=v_input->>'UnitKind')
   AND (jsonb_array_length(v_input->'UnitParents')=0 OR to_jsonb(s.unit_parents)=v_input->'UnitParents')
   AND (v_input->'MaxSubmissions'='null'::jsonb OR coalesce(s.max_submissions,0)=(v_input->>'MaxSubmissions')::integer)
   AND coalesce(s.period_kind,'')=v_period->>'Kind' AND coalesce(s.period_key,'')=v_period->>'Key'
   AND s.period_from=((v_period->>'From')::timestamptz AT TIME ZONE 'UTC')::date
   AND s.period_to=((v_period->>'To')::timestamptz AT TIME ZONE 'UTC')::date
   AND coalesce(s.period_rule,'')=v_period->>'Rule'
  ) THEN RETURN false; END IF;
  IF p_fresh AND EXISTS(SELECT 1 FROM pf_forms.period_due d WHERE d.tenant_id=p_tenant AND d.period_id=(v_item->>'id')::uuid) THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

CREATE FUNCTION pf_forms.guard_publish_operation_receipt224() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.publish_operation:'||NEW.tenant_id::text||':'||NEW.operation_id::text,0));
 IF EXISTS(SELECT 1 FROM pf_forms.publish_operation_key k WHERE k.tenant_id=NEW.tenant_id AND k.operation_id=NEW.operation_id)
 OR EXISTS(SELECT 1 FROM pf_forms.publish_operation_receipt r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id) THEN
  RAISE EXCEPTION 'publication receipt already recorded' USING ERRCODE='unique_violation',CONSTRAINT='publish_operation_receipt_once224';
 END IF;
 IF pf_forms.publish_operation_proof224(NEW.tenant_id,NEW.operation_id,NEW.actor_ref,NEW.profile,NEW.request_digest,NEW.authority_hash,NEW.request_content,NEW.content,true) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'publication receipt lacks exact native creation evidence' USING ERRCODE='check_violation',CONSTRAINT='publish_operation_native_proof224';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_publish_operation_receipt224 BEFORE INSERT ON pf_forms.publish_operation_receipt FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_publish_operation_receipt224();

CREATE FUNCTION pf_forms.guard_publish_operation_key224() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.publish_operation:'||NEW.tenant_id::text||':'||NEW.operation_id::text,0));
 IF NOT EXISTS(SELECT 1 FROM pf_forms.publish_operation_receipt r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id
  AND r.id=NEW.receipt_id AND r.recorded_at=NEW.receipt_recorded_at AND r.actor_ref=NEW.actor_ref AND r.profile=NEW.profile
  AND r.request_digest=NEW.request_digest AND r.authority_hash=NEW.authority_hash AND r.request_content=NEW.request_content) THEN
  RAISE EXCEPTION 'permanent publication key must reference its exact receipt' USING ERRCODE='check_violation',CONSTRAINT='publish_operation_key_receipt224';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_publish_operation_key_receipt224 BEFORE INSERT ON pf_forms.publish_operation_key FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_publish_operation_key224();
GRANT SELECT,INSERT ON pf_forms.publish_operation_key,pf_forms.publish_operation_receipt TO r_forms;
