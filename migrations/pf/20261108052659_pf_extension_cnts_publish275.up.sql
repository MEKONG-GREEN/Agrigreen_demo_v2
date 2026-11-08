-- BF-CN-FORM-DESIGN-EDIT-DIARY #275: immutable selected-template dispatch and receipts.
-- Only KN metadata lives here; native Forms keeps its own creation receipt.
-- Both ledgers are append-only and tenant isolated. No existing ledger is changed.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.cnts_publish_intent (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  operation_key text NOT NULL CHECK (char_length(operation_key) BETWEEN 8 AND 200),
  actor_ref uuid NOT NULL,
  session_ref uuid NOT NULL,
  authority_hash text NOT NULL CHECK (authority_hash ~ '^[0-9a-f]{64}$'),
  request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  batch_hash text NOT NULL CHECK (batch_hash ~ '^[0-9a-f]{64}$'),
  request_body jsonb NOT NULL CHECK (jsonb_typeof(request_body) = 'object' AND octet_length(request_body::text) <= 1048576),
  batch_body jsonb NOT NULL CHECK (jsonb_typeof(batch_body) = 'object' AND octet_length(batch_body::text) <= 1048576),
  series_body jsonb NOT NULL CHECK (jsonb_typeof(series_body) = 'object' AND octet_length(series_body::text) <= 4194304),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (stream_key = 'cnts-publish:' || operation_key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_publish_intent_key ON pf_extension.cnts_publish_intent (tenant_id, operation_key);
SELECT shared.apply_ledger('pf_extension.cnts_publish_intent', 'pf_extension.cnts_publish_intent');

CREATE TABLE pf_extension.cnts_publish_receipt (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  intent_ref uuid NOT NULL,
  response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body) = 'object' AND octet_length(response_body::text) <= 4194304),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_ref uuid NOT NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (id = intent_ref AND stream_key = 'cnts-publish:' || id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_publish_receipt_intent ON pf_extension.cnts_publish_receipt (tenant_id, intent_ref);
SELECT shared.apply_ledger('pf_extension.cnts_publish_receipt', 'pf_extension.cnts_publish_receipt');

REVOKE ALL ON pf_extension.cnts_publish_intent, pf_extension.cnts_publish_receipt FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_publish_intent, pf_extension.cnts_publish_receipt TO r_extension;
