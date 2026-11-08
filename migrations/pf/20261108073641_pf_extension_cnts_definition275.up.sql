-- BF-CN-FORM-DESIGN-EDIT-DIARY #275: KN request-key fence across definition targets.
-- Native Forms owns the canonical definition, receipt and atomic creation audit.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.cnts_definition_intent (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  operation_key text NOT NULL CHECK (char_length(operation_key) BETWEEN 8 AND 200),
  actor_ref uuid NOT NULL,
  session_ref uuid NOT NULL,
  authority_hash text NOT NULL CHECK (authority_hash ~ '^[0-9a-f]{64}$'),
  request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  request_body jsonb NOT NULL CHECK (jsonb_typeof(request_body) = 'object' AND octet_length(request_body::text) <= 4194304),
  command_body jsonb NOT NULL CHECK (jsonb_typeof(command_body) = 'object' AND octet_length(command_body::text) <= 4194304),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (stream_key = 'cnts-definition:' || operation_key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_definition_intent_key ON pf_extension.cnts_definition_intent (tenant_id, operation_key);
SELECT shared.apply_ledger('pf_extension.cnts_definition_intent', 'pf_extension.cnts_definition_intent');
REVOKE ALL ON pf_extension.cnts_definition_intent FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_definition_intent TO r_extension;
