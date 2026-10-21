-- KNSPLIT-4B-TRANSFER-CORE: metadata only, no owner payload or source tenant rewrite.
-- Immutable witnesses and permanent uniqueness outlive shared.ledger_idem TTL.
SET LOCAL lock_timeout = '5s';

-- Immutable registry binds every local entity to its actual business scope.
-- Child rows cannot relabel scope_ref to evade the selected-node restrictive policy.
CREATE TABLE pf_party.transfer_entity (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  id uuid NOT NULL,
  kind text NOT NULL CHECK (kind IN ('intent', 'plan')),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_intent (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  id uuid NOT NULL,
  operation_id uuid NOT NULL,
  input_hash text NOT NULL CHECK (input_hash ~ '^[a-f0-9]{64}$'),
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  actor_ref uuid NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, operation_id),
  FOREIGN KEY (tenant_id, id, scope_ref) REFERENCES pf_party.transfer_entity(tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_plan (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  id uuid NOT NULL,
  intent_id uuid NOT NULL,
  side text NOT NULL CHECK (side IN ('source', 'target')),
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  actor_ref uuid NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, intent_id),
  UNIQUE (tenant_id, id, scope_ref),
  FOREIGN KEY (tenant_id, id, scope_ref) REFERENCES pf_party.transfer_entity(tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_binding (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  first_plan_id uuid NOT NULL,
  source_tenant uuid NOT NULL,
  source_scope_ref uuid NOT NULL,
  target_tenant uuid NOT NULL,
  target_scope_ref uuid NOT NULL,
  binding_hash text NOT NULL CHECK (binding_hash ~ '^[a-f0-9]{64}$'),
  actor_ref uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CHECK (tenant_id = target_tenant AND scope_ref = target_scope_ref AND source_tenant <> target_tenant),
  PRIMARY KEY (tenant_id, first_plan_id),
  UNIQUE (source_tenant, source_scope_ref),
  UNIQUE (target_tenant),
  FOREIGN KEY (tenant_id, first_plan_id, scope_ref) REFERENCES pf_party.transfer_plan(tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_item (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  plan_id uuid NOT NULL,
  namespace text NOT NULL CHECK (char_length(namespace) BETWEEN 1 AND 96),
  source_ref uuid NOT NULL,
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  PRIMARY KEY (tenant_id, plan_id, namespace, source_ref),
  UNIQUE (tenant_id, plan_id, scope_ref, namespace, source_ref),
  FOREIGN KEY (tenant_id, plan_id, scope_ref) REFERENCES pf_party.transfer_plan(tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_idmap (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  created_plan_id uuid NOT NULL,
  source_tenant uuid NOT NULL,
  source_scope_ref uuid NOT NULL,
  namespace text NOT NULL CHECK (char_length(namespace) BETWEEN 1 AND 96),
  source_ref uuid NOT NULL,
  target_ref uuid NOT NULL,
  binding_hash text NOT NULL CHECK (binding_hash ~ '^[a-f0-9]{64}$'),
  PRIMARY KEY (tenant_id, source_tenant, source_scope_ref, namespace, source_ref),
  UNIQUE (source_tenant, source_scope_ref, namespace, source_ref),
  UNIQUE (tenant_id, namespace, target_ref),
  CHECK (substring(target_ref::text, 15, 1) = '7'),
  FOREIGN KEY (tenant_id, created_plan_id, scope_ref, namespace, source_ref) REFERENCES pf_party.transfer_item(tenant_id, plan_id, scope_ref, namespace, source_ref)
);
CREATE TABLE pf_party.transfer_batch (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  plan_id uuid NOT NULL,
  id uuid NOT NULL,
  namespace text NOT NULL CHECK (char_length(namespace) BETWEEN 1 AND 96),
  manifest_hash text NOT NULL CHECK (manifest_hash ~ '^[a-f0-9]{64}$'),
  map_hash text NOT NULL CHECK (map_hash ~ '^[a-f0-9]{64}$'),
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  actor_ref uuid NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, plan_id, id, scope_ref),
  FOREIGN KEY (tenant_id, plan_id, scope_ref) REFERENCES pf_party.transfer_plan(tenant_id, id, scope_ref)
);
CREATE TABLE pf_party.transfer_batch_item (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  plan_id uuid NOT NULL,
  batch_id uuid NOT NULL,
  namespace text NOT NULL CHECK (char_length(namespace) BETWEEN 1 AND 96),
  source_ref uuid NOT NULL,
  PRIMARY KEY (tenant_id, plan_id, namespace, source_ref),
  FOREIGN KEY (tenant_id, plan_id, batch_id, scope_ref) REFERENCES pf_party.transfer_batch(tenant_id, plan_id, id, scope_ref),
  FOREIGN KEY (tenant_id, plan_id, scope_ref, namespace, source_ref) REFERENCES pf_party.transfer_item(tenant_id, plan_id, scope_ref, namespace, source_ref)
);
CREATE TABLE pf_party.transfer_receipt (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  plan_id uuid NOT NULL,
  batch_id uuid NOT NULL,
  operation text NOT NULL CHECK (operation IN ('export', 'apply', 'verify', 'compensate', 'release_source', 'release_target')),
  namespace text NOT NULL CHECK (char_length(namespace) BETWEEN 1 AND 96),
  owner_receipt_ref uuid NOT NULL,
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  actor_ref uuid NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, plan_id, batch_id, operation, namespace),
  FOREIGN KEY (tenant_id, plan_id, scope_ref) REFERENCES pf_party.transfer_plan(tenant_id, id, scope_ref)
);

CREATE TABLE pf_party.transfer_operation (
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  entity_id uuid NOT NULL,
  kind text NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 64),
  semantic_key text NOT NULL CHECK (char_length(semantic_key) BETWEEN 1 AND 512),
  operation_id uuid NOT NULL,
  input_hash text NOT NULL CHECK (input_hash ~ '^[a-f0-9]{64}$'),
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
  PRIMARY KEY (tenant_id, entity_id, kind, semantic_key),
  UNIQUE (tenant_id, operation_id),
  FOREIGN KEY (tenant_id, entity_id, scope_ref) REFERENCES pf_party.transfer_entity(tenant_id, id, scope_ref)
);

CREATE TABLE pf_party.transfer_event (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  scope_ref uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  entity_id uuid NOT NULL,
  kind text NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 64),
  document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
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
  PRIMARY KEY (id, recorded_at),
  CHECK (stream_key = 'transfer:' || entity_id::text),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  FOREIGN KEY (tenant_id, entity_id, scope_ref) REFERENCES pf_party.transfer_entity(tenant_id, id, scope_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX transfer_event_stream ON pf_party.transfer_event(tenant_id, entity_id, seq DESC);
SELECT shared.apply_ledger('pf_party.transfer_event', 'pf_party.transfer_event');

SELECT shared.apply_append_only('pf_party.transfer_intent');

SELECT shared.apply_append_only('pf_party.transfer_plan');

SELECT shared.apply_append_only('pf_party.transfer_binding');

SELECT shared.apply_append_only('pf_party.transfer_item');

SELECT shared.apply_append_only('pf_party.transfer_idmap');

SELECT shared.apply_append_only('pf_party.transfer_batch');

SELECT shared.apply_append_only('pf_party.transfer_batch_item');

SELECT shared.apply_append_only('pf_party.transfer_receipt');
CREATE POLICY p_scope ON pf_party.transfer_intent AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_intent TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_intent FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_plan AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_plan TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_plan FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_binding AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_binding TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_binding FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_item AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_item TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_item FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_idmap AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_idmap TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_idmap FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_batch AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_batch TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_batch FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_batch_item AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_batch_item TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_batch_item FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_receipt AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_receipt TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_receipt FROM r_party;
CREATE POLICY p_scope ON pf_party.transfer_event AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_event TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_event FROM r_party;

SELECT shared.apply_append_only('pf_party.transfer_operation');
CREATE POLICY p_scope ON pf_party.transfer_operation AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_operation TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_operation FROM r_party;

SELECT shared.apply_append_only('pf_party.transfer_entity');
CREATE POLICY p_scope ON pf_party.transfer_entity AS RESTRICTIVE
  USING (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid)
  WITH CHECK (nullif(current_setting('app.node_id', true), '')::uuid IS NULL OR scope_ref = nullif(current_setting('app.node_id', true), '')::uuid);
GRANT SELECT, INSERT ON pf_party.transfer_entity TO r_party;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.transfer_entity FROM r_party;
