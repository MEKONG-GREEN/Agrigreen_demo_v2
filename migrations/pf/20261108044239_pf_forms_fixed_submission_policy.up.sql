-- Generic fixed submission policy and durable request-bound Submit receipt.
-- Profiles are registered by the genuine server-owned application at startup.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_forms.profile_submission_policy (
  profile text PRIMARY KEY CHECK (profile ~ '^[a-z][a-z0-9_]{0,39}$'),
  max_submissions integer NOT NULL CHECK (max_submissions BETWEEN 1 AND 20),
  auto_lock boolean NOT NULL,
  registered_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE pf_forms.profile_submission_policy IS
  'ag:no-rls immutable deployment profile rules, no tenant/user data; only Forms startup registry writes, no HTTP configuration';
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_forms.profile_submission_policy
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_forms.profile_submission_policy
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
REVOKE ALL ON pf_forms.profile_submission_policy FROM PUBLIC;
GRANT SELECT, INSERT ON pf_forms.profile_submission_policy TO r_forms;

-- The canonical period owns the profile. NEW.profile and session GUCs never
-- select the policy. Existing period_spec and submission rows remain unchanged.
CREATE OR REPLACE FUNCTION pf_forms.fn_submission_limit() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_max integer;
BEGIN
  SELECT coalesce(r.max_submissions,s.max_submissions) INTO v_max
  FROM pf_forms.period p
  LEFT JOIN pf_forms.period_spec s ON s.tenant_id=p.tenant_id AND s.period_id=p.id
  LEFT JOIN pf_forms.profile_submission_policy r ON r.profile=p.profile
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.period_id;
  IF v_max IS NOT NULL AND NEW.seq>v_max THEN
    RAISE EXCEPTION 'submission limit reached' USING ERRCODE='AGF03';
  END IF;
  RETURN NEW;
END $$;

CREATE FUNCTION pf_forms.guard_fixed_limit_review() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_period uuid; v_profile text; v_seq bigint; v_max integer; v_auto boolean;
BEGIN
  SELECT s.period_id,s.seq,p.profile INTO v_period,v_seq,v_profile
  FROM pf_forms.submission s JOIN pf_forms.period p ON p.tenant_id=s.tenant_id AND p.id=s.period_id
  WHERE s.tenant_id=NEW.tenant_id AND s.id=NEW.submission_id;
  IF FOUND THEN
    PERFORM pg_advisory_xact_lock(hashtextextended('pf_forms.period:'||v_period::text,0));
    SELECT r.max_submissions,r.auto_lock INTO v_max,v_auto FROM pf_forms.profile_submission_policy r WHERE r.profile=v_profile;
    IF coalesce(v_auto,false) AND v_seq>=v_max THEN
      RAISE EXCEPTION 'final allowed submission is locked' USING ERRCODE='AGF03';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_aa_fixed_limit_review BEFORE INSERT ON pf_forms.review
  FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_fixed_limit_review();
REVOKE ALL ON FUNCTION pf_forms.guard_fixed_limit_review() FROM PUBLIC;

CREATE TABLE pf_forms.submission_receipt (
  profile text NOT NULL,
  period_id uuid NOT NULL,
  unit_code text NOT NULL,
  submission_id uuid NOT NULL,
  status text NOT NULL CHECK (status IN ('submitted','locked','awaiting_reopen')),
  request_content jsonb NOT NULL CHECK (jsonb_typeof(request_content)='object'),
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
  PRIMARY KEY (id,recorded_at),
  CHECK (supersedes_id IS NULL AND correction_reason IS NULL),
  CHECK (stream_key='submit:'||idem_key AND seq=1),
  CHECK (rule_version='forms.submit/v1')
) PARTITION BY RANGE(recorded_at);
CREATE INDEX submission_receipt_key ON pf_forms.submission_receipt(tenant_id,idem_key);
SELECT shared.apply_ledger('pf_forms.submission_receipt','pf_forms.submission_receipt');

-- A receipt proves the original native tuple committed by this exact operation,
-- not the latest submission or a caller-supplied success marker.
CREATE FUNCTION pf_forms.guard_submission_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_ok boolean; v_max integer; v_auto boolean; v_seq bigint; v_expected text;
BEGIN
  IF NEW.request_content->>'Unit' IS DISTINCT FROM NEW.unit_code THEN
    RAISE EXCEPTION 'receipt target mismatch' USING ERRCODE='check_violation',CONSTRAINT='submission_receipt_proof';
  END IF;
  IF NEW.status='awaiting_reopen' THEN
    SELECT true INTO v_ok FROM pf_forms.held_submission h
    JOIN pf_forms.period p ON p.tenant_id=h.tenant_id AND p.id=h.period_id
    WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.submission_id AND h.period_id=NEW.period_id
      AND h.unit_code=NEW.unit_code AND h.submitter_ref=NEW.actor_ref AND p.profile=NEW.profile
      AND h.recorded_at=now() AND h.idem_key=NEW.idem_key||':held';
  ELSE
    SELECT s.seq INTO v_seq FROM pf_forms.submission s
    JOIN pf_forms.period p ON p.tenant_id=s.tenant_id AND p.id=s.period_id
    JOIN pf_forms.submission_meta m ON m.tenant_id=s.tenant_id AND m.submission_id=s.id
    WHERE s.tenant_id=NEW.tenant_id AND s.id=NEW.submission_id AND s.period_id=NEW.period_id
      AND s.unit_code=NEW.unit_code AND s.submitter_ref=NEW.actor_ref AND p.profile=NEW.profile
      AND s.recorded_at=now() AND s.idem_key=NEW.idem_key||':submission'
      AND (NEW.request_content->'BaseVersion'='null'::jsonb OR (NEW.request_content->>'BaseVersion')::bigint=m.base_version)
      AND (NEW.request_content->'ID'='null'::jsonb OR (NEW.request_content->>'ID')::uuid=s.id)
      AND coalesce((NEW.request_content->>'NilReport')::boolean,false)=m.nil_report
      AND coalesce(NEW.request_content->>'Note','')=coalesce(m.note,'')
      AND coalesce(NEW.request_content->>'Explanation','')=coalesce(m.explanation,'')
      AND ((NEW.request_content->>'CapturedAt')::timestamptz IS NOT DISTINCT FROM m.captured_at)
      AND (m.nil_report OR NEW.request_content->'Values'=s.values);
    v_ok:=FOUND;
    SELECT r.max_submissions,r.auto_lock INTO v_max,v_auto FROM pf_forms.profile_submission_policy r WHERE r.profile=NEW.profile;
    v_expected:=CASE WHEN coalesce(v_auto,false) AND v_seq>=v_max THEN 'locked' ELSE 'submitted' END;
    v_ok:=v_ok AND NEW.status=v_expected;
  END IF;
  IF v_ok IS DISTINCT FROM true THEN
    RAISE EXCEPTION 'receipt lacks original native submission proof' USING ERRCODE='check_violation',CONSTRAINT='submission_receipt_proof';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_receipt_proof BEFORE INSERT ON pf_forms.submission_receipt
  FOR EACH ROW EXECUTE FUNCTION pf_forms.guard_submission_receipt();
REVOKE ALL ON FUNCTION pf_forms.guard_submission_receipt() FROM PUBLIC;
REVOKE ALL ON pf_forms.submission_receipt FROM PUBLIC;
GRANT SELECT,INSERT ON pf_forms.submission_receipt TO r_forms;
