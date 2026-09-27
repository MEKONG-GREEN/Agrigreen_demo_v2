-- pf_party: đồng ý theo mục đích (mục 10.5). Module mẫu của G0 — bảng sổ đầu
-- tiên theo đúng mẫu mục 8.1, cùng bảng chiếu *_current (R-21).

CREATE SCHEMA pf_party;
REVOKE ALL ON SCHEMA pf_party FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_party TO party_app;

-- Sổ: mỗi dòng một (chủ thể, mục đích, phiên bản văn bản, trạng thái, thời điểm).
CREATE TABLE pf_party.consent (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'subject:<subject_ref>'
  seq               bigint      NOT NULL,
  subject_ref       uuid        NOT NULL,                 -- mã tham chiếu hộ/người, không phải định danh (R-41)
  purpose           text        NOT NULL CHECK (purpose ~ '^[a-z][a-z0-9_.]{2,63}$'),
  text_version      text        NOT NULL CHECK (length(text_version) BETWEEN 1 AND 40),
  status            text        NOT NULL CHECK (status IN ('granted', 'withdrawn')),
  channel           text        NOT NULL CHECK (channel IN ('field_app', 'web', 'zalo', 'paper')),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);

-- Tính duy nhất của idem_key và (stream_key, seq) do shared.fn_chain bảo đảm (ADR-0012).
CREATE INDEX consent_idem   ON pf_party.consent (tenant_id, idem_key);
CREATE INDEX consent_stream ON pf_party.consent (tenant_id, stream_key, seq);
CREATE INDEX consent_id     ON pf_party.consent (id);

SELECT shared.apply_ledger('pf_party.consent', 'pf_party.consent');

-- Bảng chiếu trạng thái hiện tại theo (chủ thể, mục đích). Được UPDATE (R-21);
-- dựng lại được hoàn toàn từ sổ (R-22).
CREATE TABLE pf_party.consent_current (
  tenant_id    uuid        NOT NULL,
  subject_ref  uuid        NOT NULL,
  purpose      text        NOT NULL,
  status       text        NOT NULL CHECK (status IN ('granted', 'withdrawn')),
  text_version text        NOT NULL,
  consent_id   uuid        NOT NULL,
  consent_seq  bigint      NOT NULL,
  event_time   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subject_ref, purpose)
);
SELECT shared.apply_tenant_rls('pf_party.consent_current');

GRANT SELECT, INSERT ON pf_party.consent TO party_app;
-- DELETE trên bảng chiếu chỉ để dựng lại từ sổ (R-22); bảng chiếu không phải sổ.
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_party.consent_current TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.consent FROM party_app;
