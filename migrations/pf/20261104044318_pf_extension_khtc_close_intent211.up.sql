-- #211: KHTC Close only; no Forms private schema or historical209 hash rewrite.
CREATE TABLE pf_extension.khtc_close_intent211 (
 id uuid NOT NULL,tenant_id uuid NOT NULL,stream_key text NOT NULL,seq bigint NOT NULL,
 operation_id uuid NOT NULL,period_id uuid NOT NULL,close_event_id uuid NOT NULL,actor_ref uuid NOT NULL,
 request_key text NOT NULL CHECK(request_key ~ '^[0-9a-f]{64}$'),request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 phase text NOT NULL CHECK(phase IN('prepared','dispatched','applied','hydrated','completed','rejected','unresolved')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),outcome jsonb CHECK(outcome IS NULL OR jsonb_typeof(outcome)='object'),
 event_time timestamptz NOT NULL,recorded_at timestamptz NOT NULL DEFAULT now(),supersedes_id uuid,correction_reason text,device_id text,
 idem_key text NOT NULL,rule_version text NOT NULL,prev_hash bytea NOT NULL,row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),CHECK((phase='completed')=(outcome IS NOT NULL))
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_extension.khtc_close_intent211','pf_extension.khtc_close_intent211');
CREATE INDEX khtc_close_key211 ON pf_extension.khtc_close_intent211(tenant_id,request_key,seq DESC);
CREATE INDEX khtc_close_resource211 ON pf_extension.khtc_close_intent211(tenant_id,period_id,operation_id,seq DESC);
GRANT SELECT,INSERT ON pf_extension.khtc_close_intent211 TO r_extension;

CREATE FUNCTION pf_extension.fn_khtc_close_pending211(tid uuid,pid uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)phase FROM pf_extension.khtc_close_intent211 WHERE tenant_id=tid AND period_id=pid ORDER BY operation_id,seq DESC)x WHERE phase NOT IN('completed','rejected'))
$$;
REVOKE ALL ON FUNCTION pf_extension.fn_khtc_close_pending211(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.fn_khtc_close_pending211(uuid,uuid) TO r_extension;

CREATE FUNCTION pf_extension.check_close_intent211() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE prior pf_extension.khtc_close_intent211%ROWTYPE;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_deadline_key:'||NEW.tenant_id::text||':close211:'||NEW.request_key,0));
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_deadline_period:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 SELECT * INTO prior FROM pf_extension.khtc_close_intent211 WHERE tenant_id=NEW.tenant_id AND operation_id=NEW.operation_id ORDER BY seq DESC LIMIT 1;
 IF NOT FOUND THEN
  IF NEW.phase<>'prepared' OR NOT(NEW.payload ? 'base' AND NEW.payload ? 'native') OR NEW.payload ? 'receipt'
   OR EXISTS(SELECT 1 FROM pf_extension.khtc_close_intent211 WHERE tenant_id=NEW.tenant_id AND request_key=NEW.request_key)
   OR pf_extension.fn_khtc_close_pending211(NEW.tenant_id,NEW.period_id)
   OR EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)phase FROM pf_extension.khtc_deadline_intent WHERE tenant_id=NEW.tenant_id AND period_id=NEW.period_id ORDER BY operation_id,seq DESC)x WHERE phase NOT IN('completed','undone'))
  THEN RAISE EXCEPTION 'khtc_close_intent_conflict211' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.period_id,NEW.close_event_id,NEW.actor_ref,NEW.request_key,NEW.request_hash,NEW.authority_hash) IS DISTINCT FROM (prior.period_id,prior.close_event_id,prior.actor_ref,prior.request_key,prior.request_hash,prior.authority_hash)
   OR NEW.payload->'base' IS DISTINCT FROM prior.payload->'base' OR NEW.payload->'native' IS DISTINCT FROM prior.payload->'native'
   OR (prior.payload ? 'receipt' AND NEW.payload->'receipt' IS DISTINCT FROM prior.payload->'receipt')
   OR (prior.payload ? 'progress' AND NEW.payload->'progress' IS DISTINCT FROM prior.payload->'progress')
   OR NOT((prior.phase='prepared' AND NEW.phase IN('dispatched','rejected')) OR(prior.phase='dispatched' AND NEW.phase IN('applied','rejected','unresolved')) OR(prior.phase='applied' AND NEW.phase IN('hydrated','unresolved')) OR(prior.phase='unresolved' AND NEW.phase IN('applied','hydrated','rejected','completed')) OR(prior.phase='hydrated' AND NEW.phase IN('completed','unresolved')))
  THEN RAISE EXCEPTION 'khtc_close_phase_conflict211' USING ERRCODE='23514'; END IF;
 END IF;
 IF NEW.phase IN('applied','hydrated','completed') AND (NOT(NEW.payload ? 'receipt') OR NEW.payload->'receipt'->>'outcome'<>'applied') THEN RAISE EXCEPTION 'khtc_close_receipt_required211' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_close_intent211 BEFORE INSERT ON pf_extension.khtc_close_intent211 FOR EACH ROW EXECUTE FUNCTION pf_extension.check_close_intent211();

-- Existing209 NEW intents share the same resource fence; old phases/hash untouched.
CREATE FUNCTION pf_extension.check_deadline_close_fence211() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NEW.phase='prepared' AND pf_extension.fn_khtc_close_pending211(NEW.tenant_id,NEW.period_id) THEN RAISE EXCEPTION 'khtc_close_pending211' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER b_close_fence211 BEFORE INSERT ON pf_extension.khtc_deadline_intent FOR EACH ROW EXECUTE FUNCTION pf_extension.check_deadline_close_fence211();

-- Report-owned reservation has no lease or automatic expiry after unknown I/O.
CREATE TABLE pf_extension.khtc_close_reservation211 (
 id uuid NOT NULL,tenant_id uuid NOT NULL,stream_key text NOT NULL,seq bigint NOT NULL,
 operation_id uuid NOT NULL,period_id uuid NOT NULL,close_event_id uuid NOT NULL,action text NOT NULL CHECK(action IN('reserved','completed','released')),
 actor_ref uuid NOT NULL,event_time timestamptz NOT NULL,recorded_at timestamptz NOT NULL DEFAULT now(),supersedes_id uuid,correction_reason text,device_id text,
 idem_key text NOT NULL,rule_version text NOT NULL,prev_hash bytea NOT NULL,row_hash bytea NOT NULL,PRIMARY KEY(id,recorded_at)
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_extension.khtc_close_reservation211','pf_extension.khtc_close_reservation211');
CREATE INDEX khtc_close_reservation_resource211 ON pf_extension.khtc_close_reservation211(tenant_id,period_id,operation_id,seq DESC);
GRANT SELECT,INSERT ON pf_extension.khtc_close_reservation211 TO r_extension;
CREATE FUNCTION pf_extension.fn_khtc_close_reserved211(tid uuid,pid uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)action FROM pf_extension.khtc_close_reservation211 WHERE tenant_id=tid AND period_id=pid ORDER BY operation_id,seq DESC)x WHERE action='reserved')
$$;
REVOKE ALL ON FUNCTION pf_extension.fn_khtc_close_reserved211(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.fn_khtc_close_reserved211(uuid,uuid) TO r_extension;
CREATE FUNCTION pf_extension.check_close_reservation211() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE prior pf_extension.khtc_close_reservation211%ROWTYPE;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 SELECT * INTO prior FROM pf_extension.khtc_close_reservation211 WHERE tenant_id=NEW.tenant_id AND operation_id=NEW.operation_id ORDER BY seq DESC LIMIT 1;
 IF NOT FOUND THEN
  IF NEW.action<>'reserved' OR pf_extension.fn_khtc_close_reserved211(NEW.tenant_id,NEW.period_id) OR pf_extension.fn_khtc_cancel_pending(NEW.tenant_id,NEW.period_id)
   OR NOT EXISTS(SELECT 1 FROM pf_extension.khtc_close_intent211 WHERE tenant_id=NEW.tenant_id AND operation_id=NEW.operation_id AND period_id=NEW.period_id AND close_event_id=NEW.close_event_id AND actor_ref=NEW.actor_ref AND phase='prepared')
  THEN RAISE EXCEPTION 'khtc_close_reservation_conflict211' USING ERRCODE='23514'; END IF;
 ELSE
  IF prior.action<>'reserved' OR NEW.action NOT IN('completed','released') OR (NEW.period_id,NEW.close_event_id,NEW.actor_ref) IS DISTINCT FROM(prior.period_id,prior.close_event_id,prior.actor_ref)
  THEN RAISE EXCEPTION 'khtc_close_reservation_phase211' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_close_reservation211 BEFORE INSERT ON pf_extension.khtc_close_reservation211 FOR EACH ROW EXECUTE FUNCTION pf_extension.check_close_reservation211();

CREATE FUNCTION pf_extension.check_cancel_close_fence211() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
 IF NEW.claim_id IS NOT NULL AND pf_extension.fn_khtc_close_reserved211(NEW.tenant_id,NEW.period_id) THEN RAISE EXCEPTION 'khtc_close_pending211' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_close_fence211 BEFORE INSERT OR UPDATE ON pf_extension.khtc_cancel_intent FOR EACH ROW EXECUTE FUNCTION pf_extension.check_cancel_close_fence211();
CREATE FUNCTION pf_extension.check_period_close_owner211() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE reserved record;
BEGIN
 IF NEW.action IN('closed','cancelled','deadline_changed') THEN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||NEW.tenant_id::text||':'||NEW.period_id::text,0));
  SELECT * INTO reserved FROM(SELECT DISTINCT ON(operation_id)* FROM pf_extension.khtc_close_reservation211 WHERE tenant_id=NEW.tenant_id AND period_id=NEW.period_id ORDER BY operation_id,seq DESC)x WHERE action='reserved' LIMIT 1;
  IF FOUND AND (NEW.action<>'closed' OR NEW.id<>reserved.close_event_id OR NEW.actor_ref<>reserved.actor_ref) THEN RAISE EXCEPTION 'khtc_close_owned_event211' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a_close_owner211 BEFORE INSERT ON pf_extension.khtc_period_event FOR EACH ROW EXECUTE FUNCTION pf_extension.check_period_close_owner211();
