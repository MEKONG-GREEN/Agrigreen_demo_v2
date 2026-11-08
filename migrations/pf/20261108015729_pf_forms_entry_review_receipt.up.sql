-- FORMS-ENTRY-REVIEW-RECEIPT-244 (#288): permanent identity and immutable review audit.
SET LOCAL lock_timeout='5s';
CREATE TABLE pf_forms.review_operation_key (
 tenant_id uuid NOT NULL CHECK(tenant_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 operation_id uuid NOT NULL CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 actor_ref uuid NOT NULL CHECK(actor_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 receipt_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,operation_id)
);
SELECT shared.apply_tenant_rls('pf_forms.review_operation_key');
CREATE FUNCTION pf_forms.deny_review_operation_key_change() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 RAISE EXCEPTION 'review operation identity is permanent' USING ERRCODE='check_violation',CONSTRAINT='review_operation_key_immutable';
END $$;
CREATE TRIGGER trg_review_operation_key_immutable BEFORE UPDATE OR DELETE ON pf_forms.review_operation_key FOR EACH ROW EXECUTE FUNCTION pf_forms.deny_review_operation_key_change();
CREATE TRIGGER trg_review_operation_key_no_truncate BEFORE TRUNCATE ON pf_forms.review_operation_key FOR EACH STATEMENT EXECUTE FUNCTION pf_forms.deny_review_operation_key_change();

CREATE TABLE pf_forms.review_operation_receipt (
 operation_id uuid NOT NULL,
 profile text NOT NULL CHECK(char_length(profile) BETWEEN 1 AND 64),
 period_id uuid NOT NULL,
 unit_code text NOT NULL CHECK(char_length(unit_code) BETWEEN 1 AND 200),
 submission_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version>0),
 decision text NOT NULL CHECK(decision IN('accepted','returned')),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 outcome text NOT NULL CHECK(outcome IN('applied','rejected')),
 content jsonb NOT NULL CHECK(jsonb_typeof(content)='object' AND octet_length(content::text)<=8192),
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
 CHECK(stream_key='review_operation:'||operation_id::text AND seq=1),
 CHECK(rule_version='forms.review_operation/v1')
) PARTITION BY RANGE(recorded_at);
CREATE INDEX review_operation_receipt_identity ON pf_forms.review_operation_receipt(tenant_id,id);
SELECT shared.apply_ledger('pf_forms.review_operation_receipt','pf_forms.review_operation_receipt');

CREATE FUNCTION pf_forms.check_review_operation_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pf_forms.review_operation_key k WHERE k.tenant_id=NEW.tenant_id AND k.operation_id=NEW.operation_id
  AND k.receipt_id=NEW.id AND k.actor_ref=NEW.actor_ref AND k.request_digest=NEW.request_digest AND k.authority_hash=NEW.authority_hash)
 OR NOT NEW.content @> jsonb_build_object('operation_id',NEW.operation_id,'tenant_id',NEW.tenant_id,'profile',NEW.profile,'period_id',NEW.period_id,
  'unit',NEW.unit_code,'submission_id',NEW.submission_id,'expected_version',NEW.expected_version,'decision',NEW.decision,
  'request_digest',NEW.request_digest,'authority_hash',NEW.authority_hash,'actor_ref',NEW.actor_ref,'outcome',NEW.outcome) THEN
  RAISE EXCEPTION 'receipt does not match permanent identity' USING ERRCODE='check_violation',CONSTRAINT='review_operation_receipt_key';
 END IF;
 IF EXISTS(SELECT 1 FROM pf_forms.review_operation_receipt r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id) THEN
  RAISE EXCEPTION 'receipt already recorded' USING ERRCODE='unique_violation',CONSTRAINT='review_operation_receipt_once';
 END IF;
 IF NEW.outcome='applied' AND NOT EXISTS(
  SELECT 1 FROM pf_forms.review r JOIN pf_forms.submission s ON s.tenant_id=r.tenant_id AND s.id=r.submission_id
  JOIN pf_forms.period p ON p.tenant_id=s.tenant_id AND p.id=s.period_id
  WHERE r.tenant_id=NEW.tenant_id AND r.id=(NEW.content->'result'->>'review_id')::uuid
   AND r.submission_id=NEW.submission_id AND r.period_id=NEW.period_id AND r.unit_code=NEW.unit_code AND p.profile=NEW.profile
   AND r.recorded_at=NEW.recorded_at AND r.idem_key='forms.review_operation:'||NEW.operation_id::text||':review'
   AND s.seq=NEW.expected_version AND r.reviewer_ref=NEW.actor_ref AND r.decision=NEW.decision
   AND r.seq=(NEW.content->'result'->>'review_seq')::bigint
   AND r.reviewer_ref=(NEW.content->'result'->>'reviewer')::uuid
   AND r.event_time=(NEW.content->'result'->>'reviewed_at')::timestamptz
   AND r.recorded_at=(NEW.content->'result'->>'review_recorded_at')::timestamptz
   AND r.workflow_request_id IS NOT DISTINCT FROM (NEW.content->'result'->>'workflow_request_id')::uuid
 ) THEN
  RAISE EXCEPTION 'receipt is not the exact native review' USING ERRCODE='check_violation',CONSTRAINT='review_operation_receipt_native';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_review_operation_receipt_proof BEFORE INSERT ON pf_forms.review_operation_receipt FOR EACH ROW EXECUTE FUNCTION pf_forms.check_review_operation_receipt();
GRANT SELECT,INSERT ON pf_forms.review_operation_key,pf_forms.review_operation_receipt TO r_forms;
