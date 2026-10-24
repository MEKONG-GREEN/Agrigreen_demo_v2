-- pf/hrm (`dom/hrm`, W11.27 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-11, UR-HC-23, UR-LDP-03; D3 H7): KẾ HOẠCH NHÂN
-- LỰC NĂM.
--
-- Vòng đời kỳ (mở, hạn, nộp, rà soát "yêu cầu điều chỉnh", chốt kèm ảnh chụp đóng băng) là kỳ native của cap/forms, profile
-- "hrm_plan", đơn vị lá = đơn vị nội bộ (W11.07). pf_hrm chỉ giữ phần riêng của HCTH:
--
-- Ba sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   - plan_period     — kỳ kế hoạch theo năm, chuỗi 'year:<năm>': mã kỳ cap/forms (period_ref), ngày chốt số liệu hiện có
--                       (BR-01: số hiện có, dự kiến nghỉ hưu tính tại ngày này), hạn gửi, danh sách đơn vị. Một kỳ mỗi năm
--                       (seq = 1; trùng → xung đột chuỗi).
--   - plan_draft      — bản nháp của đơn vị (prototype: "Lưu nháp"), chuỗi 'draft:<period_ref>:<đơn vị>'; bản sau thay bản
--                       trước khi hiển thị. Bài gửi chính thức nằm ở cap/forms.
--   - plan_adjustment — điều chỉnh SAU CHỐT có lý do (BR-02: "kế hoạch đã chốt không sửa; lưu lịch sử điều chỉnh"), chuỗi
--                       'adj:<period_ref>:<đơn vị>'; giá trị đầy đủ sau điều chỉnh, ảnh chụp chốt của cap/forms giữ nguyên.
-- Vai: r_hrm SELECT, INSERT.

CREATE TABLE pf_hrm.plan_period (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'year:<năm>'
  seq               bigint      NOT NULL,
  plan_year         integer     NOT NULL CHECK (plan_year BETWEEN 2020 AND 2100),
  period_ref        uuid        NOT NULL,                 -- kỳ native cap/forms (pf_forms.period)
  template_ref      uuid        NOT NULL,                 -- phiên bản biểu HRM_PLAN
  cutoff_on         date        NOT NULL,                 -- ngày chốt số liệu hiện có (BR-01)
  due_at            timestamptz NOT NULL,                 -- hạn các đơn vị gửi
  units             text[]      NOT NULL CHECK (cardinality(units) BETWEEN 1 AND 200),
  change            text        NOT NULL CHECK (change IN ('opened')),
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
  CHECK (array_position(units, NULL) IS NULL),
  CONSTRAINT plan_period_stream CHECK (stream_key = 'year:' || plan_year::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX plan_period_year ON pf_hrm.plan_period (tenant_id, plan_year, seq);
CREATE INDEX plan_period_ref ON pf_hrm.plan_period (tenant_id, period_ref);
SELECT shared.apply_ledger('pf_hrm.plan_period', 'pf_hrm.plan_period');

CREATE TABLE pf_hrm.plan_draft (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'draft:<period_ref>:<đơn vị>'
  seq               bigint      NOT NULL,
  period_ref        uuid        NOT NULL,
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  plan_values       jsonb       NOT NULL CHECK (jsonb_typeof(plan_values) = 'object'),
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
  CONSTRAINT plan_draft_stream CHECK (stream_key = 'draft:' || period_ref::text || ':' || unit_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX plan_draft_unit ON pf_hrm.plan_draft (tenant_id, period_ref, unit_code, seq);
SELECT shared.apply_ledger('pf_hrm.plan_draft', 'pf_hrm.plan_draft');

CREATE TABLE pf_hrm.plan_adjustment (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'adj:<period_ref>:<đơn vị>'
  seq               bigint      NOT NULL,
  period_ref        uuid        NOT NULL,
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  plan_values       jsonb       NOT NULL CHECK (jsonb_typeof(plan_values) = 'object'),  -- giá trị đầy đủ sau điều chỉnh
  reason            text        NOT NULL CHECK (char_length(reason) BETWEEN 10 AND 500),
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
  CONSTRAINT plan_adjustment_stream CHECK (stream_key = 'adj:' || period_ref::text || ':' || unit_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX plan_adjustment_unit ON pf_hrm.plan_adjustment (tenant_id, period_ref, unit_code, seq);
SELECT shared.apply_ledger('pf_hrm.plan_adjustment', 'pf_hrm.plan_adjustment');

REVOKE ALL ON pf_hrm.plan_period, pf_hrm.plan_draft, pf_hrm.plan_adjustment FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.plan_period, pf_hrm.plan_draft, pf_hrm.plan_adjustment TO r_hrm;
