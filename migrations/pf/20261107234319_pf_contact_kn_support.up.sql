-- PRICE-V2-SUPPORT-REQUEST #274, Báo giá v2 FN-CH-00.01 / standalone Q10.
-- Public intake is anonymous: actor_ref NULL means no authenticated actor, never a fabricated account.
-- PII/free text is only an authenticated envelope. The vault retains the private key; no plaintext in either table.
CREATE TABLE pf_contact.support_request (
  tenant_id uuid NOT NULL,
  id uuid NOT NULL,
  key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
  payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
  subject_ref uuid NOT NULL,
  nonce bytea NOT NULL CHECK (octet_length(nonce) = 12),
  wrapped_dek bytea NOT NULL CHECK (octet_length(wrapped_dek) = 80),
  ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 16 AND 12288),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, key_hash)
);
SELECT shared.apply_append_only('pf_contact.support_request');

CREATE TABLE pf_contact.support_event (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  request_id uuid NULL,
  event text NOT NULL CHECK (event IN ('submitted', 'resolved', 'viewed', 'listed')),
  expected_version bigint NULL,
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  supersedes_id uuid NULL,
  correction_reason text NULL,
  actor_ref uuid NULL,
  device_id text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((event = 'submitted' AND actor_ref IS NULL) OR (event <> 'submitted' AND actor_ref IS NOT NULL)),
  CHECK ((event = 'listed' AND request_id IS NULL) OR (event <> 'listed' AND request_id IS NOT NULL)),
  CHECK ((event = 'resolved' AND expected_version > 0) OR (event <> 'resolved' AND expected_version IS NULL)),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (stream_key = CASE WHEN event = 'listed' THEN 'support-queue:' || actor_ref::text ELSE 'support:' || request_id::text END)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX support_event_request ON pf_contact.support_event (tenant_id, request_id, seq DESC);
SELECT shared.apply_ledger('pf_contact.support_event', 'pf_contact.support_event');
REVOKE ALL ON pf_contact.support_request, pf_contact.support_event FROM PUBLIC;
GRANT SELECT, INSERT ON pf_contact.support_request, pf_contact.support_event TO r_contact;
