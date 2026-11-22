-- pf/hrm (`dom/hrm`, NOTIFY-HCTH-KN #50 — W11.27, BRD Hành chính – Tổng hợp v3.0 FN-11; prototype "Nhắc phòng chưa lập"):
-- SỔ NHẮC LẬP KẾ HOẠCH NHÂN LỰC.
--
-- Sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   - plan_reminder — mỗi lần Phòng HCTH/Admin nhắc các đơn vị CHƯA gửi kế hoạch của một kỳ, chuỗi 'remind:<period_ref>':
--                     năm, Nút sở hữu phòng ban của phiên người nhắc (node_ref — Trung tâm), danh sách đơn vị nội bộ chưa
--                     gửi lúc nhắc. Sự kiện pf.hrm.plan.reminded ghi CÙNG giao dịch (outbox) → pf/notify gửi lãnh đạo các
--                     đơn vị đó. idem_key (Idempotency-Key của request) chặn ghi đôi khi request chạy lại. Không PII.
-- Vai: r_hrm SELECT, INSERT.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_hrm.plan_reminder (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'remind:<period_ref>'
  seq               bigint      NOT NULL,
  period_ref        uuid        NOT NULL,                 -- kỳ native cap/forms (pf_forms.period)
  plan_year         integer     NOT NULL CHECK (plan_year BETWEEN 2020 AND 2100),
  node_ref          uuid        NOT NULL,                 -- Nút sở hữu phòng ban của phiên người nhắc
  units             text[]      NOT NULL CHECK (cardinality(units) BETWEEN 1 AND 200),
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
  CHECK (node_ref <> '00000000-0000-0000-0000-000000000000'::uuid),
  CONSTRAINT plan_reminder_stream CHECK (stream_key = 'remind:' || period_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX plan_reminder_period ON pf_hrm.plan_reminder (tenant_id, period_ref, seq);
SELECT shared.apply_ledger('pf_hrm.plan_reminder', 'pf_hrm.plan_reminder');

REVOKE ALL ON pf_hrm.plan_reminder FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.plan_reminder TO r_hrm;
