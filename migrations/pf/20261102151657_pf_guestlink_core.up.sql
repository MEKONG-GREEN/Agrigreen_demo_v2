-- Dormant generic guest capability, ADR-0048. No OTP/contact/bearer plaintext.
SET LOCAL lock_timeout='5s';
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='r_guestlink' AND NOT rolcanlogin) THEN
  RAISE EXCEPTION 'run bootstrap before guestlink migration';
 END IF;
END $$;
CREATE SCHEMA pf_guestlink;
REVOKE ALL ON SCHEMA pf_guestlink FROM PUBLIC;
CREATE TABLE pf_guestlink.grant_record (
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
 binding_key text NOT NULL CHECK(binding_key ~ '^[0-9a-f]{64}$'),
 owner text NOT NULL CHECK(owner ~ '^[a-z][a-z0-9_]{0,30}/[a-z][a-z0-9_]{0,30}$'),
 subject_ref uuid NOT NULL,
 resource_ref uuid NOT NULL,
 step text NOT NULL CHECK(step ~ '^[a-z][a-z0-9_]{0,31}$'),
 owner_revision uuid NOT NULL,
 contact_revision uuid NOT NULL,
 link_hash text NOT NULL CHECK(link_hash ~ '^[0-9a-f]{64}$'),
 issue_key text NOT NULL CHECK(issue_key ~ '^[0-9a-f]{64}$'),
 expires_at timestamptz NOT NULL,
 CHECK(owner_revision<>'00000000-0000-0000-0000-000000000000'::uuid AND subject_ref<>'00000000-0000-0000-0000-000000000000'::uuid AND resource_ref<>'00000000-0000-0000-0000-000000000000'::uuid AND contact_revision<>'00000000-0000-0000-0000-000000000000'::uuid),
 CHECK(expires_at>event_time AND expires_at<=event_time+interval '24 hours'),
 CHECK(stream_key='guestlink:'||binding_key),
 PRIMARY KEY(id,recorded_at)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX guestlink_grant_link ON pf_guestlink.grant_record(tenant_id,link_hash);
CREATE INDEX guestlink_grant_binding ON pf_guestlink.grant_record(tenant_id,binding_key,seq DESC);
CREATE INDEX guestlink_grant_idem ON pf_guestlink.grant_record(tenant_id,issue_key);
CREATE INDEX guestlink_grant_subject ON pf_guestlink.grant_record(tenant_id,subject_ref);
SELECT shared.apply_ledger('pf_guestlink.grant_record','pf_guestlink.grant_record');
CREATE TABLE pf_guestlink.access_event (
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
 grant_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN ('opened','challenge','sent','disabled','otp_wrong','verified','session_ok','session_denied','revoked','rotated','denied','quota')),
 challenge_id uuid NOT NULL,
 session_ref uuid NOT NULL,
 operation_hash text NOT NULL DEFAULT '' CHECK(operation_hash='' OR operation_hash ~ '^[0-9a-f]{64}$'),
 session_hash text NOT NULL DEFAULT '' CHECK(session_hash='' OR session_hash ~ '^[0-9a-f]{64}$'),
 ip_hash text NOT NULL CHECK(ip_hash ~ '^[0-9a-f]{64}$'),
 expires_at timestamptz NULL,
 reason text NOT NULL CHECK(reason ~ '^[a-z_]{0,32}$'),
 CHECK(stream_key='guestlink:'||grant_id::text),
 PRIMARY KEY(id,recorded_at)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX guestlink_event_grant ON pf_guestlink.access_event(tenant_id,grant_id,seq DESC);
CREATE INDEX guestlink_event_session ON pf_guestlink.access_event(tenant_id,session_hash) WHERE session_hash<>'';
CREATE INDEX guestlink_event_ip ON pf_guestlink.access_event(tenant_id,ip_hash,event_time);
SELECT shared.apply_ledger('pf_guestlink.access_event','pf_guestlink.access_event');
REVOKE ALL ON pf_guestlink.grant_record,pf_guestlink.access_event FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_guestlink TO r_guestlink;
GRANT SELECT,INSERT ON pf_guestlink.grant_record,pf_guestlink.access_event TO r_guestlink;
GRANT USAGE ON SCHEMA k_ref TO r_guestlink;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text),k_ref.ctx_text(text),k_ref.ctx_uuid(text) TO r_guestlink;
GRANT SELECT ON k_ref.org,k_ref.node_closure,k_ref.admin_unit,k_ref.share_grant_active,k_ref.share_scope TO r_guestlink;
