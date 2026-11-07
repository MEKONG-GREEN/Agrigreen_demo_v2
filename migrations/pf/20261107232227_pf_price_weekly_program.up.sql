-- FN-TI-02.13, BRD Báo giá v2.0: program snapshots and operation audit.
-- A program-scoped lock serializes CAS/close/new-week/import/catalog changes.
-- Deletion is a tombstone projection; closed report inputs and ledger rows are never updated.
CREATE TABLE pf_price.weekly_program (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  program           text        NOT NULL CHECK (program IN ('information', 'training')),
  week_id           uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('initialize', 'save', 'close', 'new', 'adjust', 'add', 'delete', 'import')),
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4194304),
  request_hash      bytea       NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT weekly_program_stream CHECK (stream_key = 'weekly:' || profile || ':' || program)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX weekly_program_current ON pf_price.weekly_program (tenant_id, profile, program, seq DESC);
CREATE INDEX weekly_program_idem ON pf_price.weekly_program (tenant_id, profile, program, idem_key);
SELECT shared.apply_ledger('pf_price.weekly_program', 'pf_price.weekly_program');
REVOKE ALL ON pf_price.weekly_program FROM PUBLIC;
GRANT SELECT, INSERT ON pf_price.weekly_program TO r_price;
