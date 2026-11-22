-- BF-FORMS-PERIOD-OP-RECEIPT: permanent identity anchor + native atomic receipt.
-- The key anchor has no expiry and cannot be purged with a receipt partition.
SET LOCAL lock_timeout='5s';
CREATE TABLE pf_forms.period_operation_key (
 tenant_id uuid NOT NULL CHECK(tenant_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 operation_id uuid NOT NULL CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 profile text NOT NULL CHECK(char_length(profile) BETWEEN 1 AND 64),
 period_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('change_due','close','reopen','cancel')),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 authority_hash text NOT NULL CHECK(authority_hash ~ '^[0-9a-f]{64}$'),
 actor_ref uuid NOT NULL CHECK(actor_ref<>'00000000-0000-0000-0000-000000000000'::uuid),
 receipt_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,operation_id)
);
SELECT shared.apply_tenant_rls('pf_forms.period_operation_key');
CREATE FUNCTION pf_forms.deny_period_operation_key_change() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 RAISE EXCEPTION 'period operation identity is permanent' USING ERRCODE='check_violation',CONSTRAINT='period_operation_key_immutable';
END $$;
CREATE TRIGGER trg_period_operation_key_immutable BEFORE UPDATE OR DELETE ON pf_forms.period_operation_key FOR EACH ROW EXECUTE FUNCTION pf_forms.deny_period_operation_key_change();
CREATE TRIGGER trg_period_operation_key_no_truncate BEFORE TRUNCATE ON pf_forms.period_operation_key FOR EACH STATEMENT EXECUTE FUNCTION pf_forms.deny_period_operation_key_change();

CREATE TABLE pf_forms.period_operation_receipt (
 operation_id uuid NOT NULL,
 profile text NOT NULL CHECK(char_length(profile) BETWEEN 1 AND 64),
 period_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('change_due','close','reopen','cancel')),
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
 CHECK(stream_key='period_operation:'||operation_id::text),
 CHECK(rule_version='forms.period_operation/v1')
) PARTITION BY RANGE(recorded_at);
CREATE INDEX period_operation_receipt_identity ON pf_forms.period_operation_receipt(tenant_id,id);
SELECT shared.apply_ledger('pf_forms.period_operation_receipt','pf_forms.period_operation_receipt');
CREATE FUNCTION pf_forms.check_period_operation_receipt_key() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pf_forms.period_operation_key k WHERE k.tenant_id=NEW.tenant_id AND k.operation_id=NEW.operation_id
  AND k.receipt_id=NEW.id AND k.profile=NEW.profile AND k.period_id=NEW.period_id AND k.action=NEW.action
  AND k.request_digest=NEW.request_digest AND k.authority_hash=NEW.authority_hash AND k.actor_ref=NEW.actor_ref) THEN
  RAISE EXCEPTION 'receipt does not match permanent identity' USING ERRCODE='check_violation',CONSTRAINT='period_operation_receipt_key';
 END IF;
 IF EXISTS(SELECT 1 FROM pf_forms.period_operation_receipt r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id) THEN
  RAISE EXCEPTION 'receipt already recorded' USING ERRCODE='unique_violation',CONSTRAINT='period_operation_receipt_once';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_period_operation_receipt_key BEFORE INSERT ON pf_forms.period_operation_receipt FOR EACH ROW EXECUTE FUNCTION pf_forms.check_period_operation_receipt_key();
GRANT SELECT,INSERT ON pf_forms.period_operation_key,pf_forms.period_operation_receipt TO r_forms;
