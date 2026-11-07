-- KN-STANDALONE-8-BAOGIA-WEB: D2 defers FN-TI-02.07. Local attempts
-- acknowledge a disconnected website; they never create external delivery work.
-- Existing boards, publication snapshots and hashes remain unchanged.
CREATE TABLE pf_price.board_website_attempt (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  profile text NOT NULL CHECK (profile <> ''),
  board_date date NOT NULL,
  board_version integer NOT NULL CHECK (board_version >= 1),
  status text NOT NULL CHECK (status = 'not_connected'),
  trace_id text NOT NULL CHECK (char_length(trace_id) <= 128),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  supersedes_id uuid NULL,
  correction_reason text NULL,
  actor_ref uuid NOT NULL,
  device_id text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL CHECK (rule_version = 'kn-standalone-d2-v1'),
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (stream_key = 'website:' || profile || ':' || to_char(board_date, 'YYYY-MM-DD'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX board_website_attempt_version ON pf_price.board_website_attempt
  (tenant_id, profile, board_date, board_version, seq DESC);
SELECT shared.apply_ledger('pf_price.board_website_attempt', 'pf_price.board_website_attempt');
REVOKE ALL ON pf_price.board_website_attempt FROM PUBLIC;
GRANT SELECT, INSERT ON pf_price.board_website_attempt TO r_price;
