-- pf_extension KN v7.7 P4 (1/4): đợt phát hành biểu, kỳ đã chia, công việc xã × biểu × kỳ, giá trị
-- cột "Tỉnh giao" (FN-TI-02.02, FN-XA-03.01; docs/brd/kn-v7.7-plan.md §2 pf 000068, gap §4.1).
--
--   1. report_batch_event   SỔ đợt phát hành: issued / withdrawn (chỉ khi chưa xã nào nộp) /
--                           deadline_changed (chỉ kỳ chưa đến hạn) — "thu hồi", "sửa hạn" là sự kiện bù
--      report_batch         bảng chiếu hiện hành của đợt
--   2. report_period        kỳ đã chia của đợt (nhãn, từ–đến, hạn, giờ chốt tự động) — bảng chiếu; trạng thái
--                           đang chốt/mở là phép chiếu của sổ period_event (000070)
--   3. report_task_event    SỔ công việc (lưu nháp, gửi, yêu cầu điều chỉnh, xác nhận, chốt, mở lại, nhắc…)
--      report_task          bảng chiếu công việc xã × biểu × kỳ (cán bộ phụ trách cùng phòng ban)
--   4. provincial_value     SỔ giá trị cột Tỉnh giao theo xã của đợt (nhập tay, Excel, lấy từ đợt trước)
--
-- Mọi bản ghi mang department_code (kế hoạch §1.2). Không PII (R-04): chỉ mã xã, mã tham chiếu cán bộ.

-- ---------------------------------------------------------------------------
-- 1. Đợt phát hành
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.report_batch_event (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'batch:<batch_id>'
  seq               bigint      NOT NULL,
  batch_id          uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('issued', 'withdrawn', 'deadline_changed')),
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  province_code     text        NOT NULL,
  payload           jsonb       NOT NULL,                 -- ảnh chụp yêu cầu (mẫu, phạm vi, kỳ, hạn…) hoặc thay đổi
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  on_behalf_of      uuid        NULL,                     -- chế độ hỗ trợ Admin (FN-AD-04.07)
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
  CHECK (action = 'issued' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX report_batch_event_stream ON pf_extension.report_batch_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_extension.report_batch_event', 'pf_extension.report_batch_event');

CREATE TABLE pf_extension.report_batch (
  tenant_id            uuid        NOT NULL,
  id                   uuid        NOT NULL,
  department_code      text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  province_code        text        NOT NULL,
  report_type          text        NOT NULL CHECK (report_type IN ('weekly', 'monthly', 'seasonal', 'annual', 'adhoc')),
  period_type          text        NOT NULL CHECK (period_type IN ('week', 'month', 'season', 'year', 'adhoc')),
  season_label         text        NOT NULL CHECK (char_length(season_label) BETWEEN 1 AND 120),
  from_date            date        NOT NULL,
  to_date              date        NOT NULL,
  period_rule          text        NOT NULL,              -- phiên bản quy tắc chia kỳ (Q-17, tạm)
  deadline_offset_days integer     NOT NULL CHECK (deadline_offset_days BETWEEN 0 AND 60),
  deadline_time        text        NOT NULL CHECK (deadline_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  close_after_days     integer     NOT NULL CHECK (close_after_days BETWEEN 0 AND 60),
  note                 text        NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  forms                jsonb       NOT NULL,              -- [{template_id, version_id, code, name, version}]
  communes             text[]      NOT NULL,
  status               text        NOT NULL CHECK (status IN ('issued', 'withdrawn')),
  period_count         integer     NOT NULL CHECK (period_count >= 1),
  task_count           integer     NOT NULL CHECK (task_count >= 0),
  issued_by            uuid        NOT NULL,
  issued_at            timestamptz NOT NULL,
  withdrawn_by         uuid        NULL,
  withdrawn_at         timestamptz NULL,
  withdraw_reason      text        NULL,
  last_event_id        uuid        NOT NULL,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  CHECK (to_date >= from_date),
  CHECK ((status = 'withdrawn') = (withdrawn_at IS NOT NULL))
);
CREATE INDEX report_batch_scope ON pf_extension.report_batch (tenant_id, province_code, department_code, issued_at DESC);
SELECT shared.apply_tenant_rls('pf_extension.report_batch');

-- ---------------------------------------------------------------------------
-- 2. Kỳ đã chia
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.report_period (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  batch_id        uuid        NOT NULL,
  slot            integer     NOT NULL CHECK (slot >= 1),          -- kỳ thứ n của đợt = khe kỳ của mẫu nhiều kỳ
  label           text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 120),
  range_label     text        NOT NULL,
  iso_week        integer     NULL CHECK (iso_week IS NULL OR iso_week BETWEEN 1 AND 53),
  from_date       date        NOT NULL,
  to_date         date        NOT NULL,
  due_at          timestamptz NOT NULL,
  auto_close_at   timestamptz NOT NULL,
  status          text        NOT NULL CHECK (status IN ('open', 'closed')),
  closed_at       timestamptz NULL,
  closed_by       uuid        NULL,
  close_mode      text        NULL CHECK (close_mode IS NULL OR close_mode IN ('manual', 'auto')),
  department_code text        NOT NULL,
  province_code   text        NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, batch_id, slot),
  FOREIGN KEY (tenant_id, batch_id) REFERENCES pf_extension.report_batch (tenant_id, id),
  CHECK (to_date >= from_date),
  CHECK (auto_close_at >= due_at),
  CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);
-- Job tự chốt tìm kỳ mở đã quá giờ chốt (chỉ mục theo giờ chốt).
CREATE INDEX report_period_autoclose ON pf_extension.report_period (tenant_id, auto_close_at) WHERE status = 'open';
CREATE INDEX report_period_scope ON pf_extension.report_period (tenant_id, province_code, department_code, from_date);
SELECT shared.apply_tenant_rls('pf_extension.report_period');

-- ---------------------------------------------------------------------------
-- 3. Công việc xã × biểu × kỳ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.report_task_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'task:<task_id>'
  seq               bigint      NOT NULL,
  task_id           uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('draft_saved', 'submitted', 'adjustment_requested', 'confirmed',
                      'closed', 'reopened', 'reclosed', 'reminded', 'withdrawn', 'deadline_changed')),
  status_after      text        NOT NULL,
  submission_id     uuid        NULL,
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) <= 2000),
  payload           jsonb       NOT NULL DEFAULT '{}'::jsonb,
  on_behalf_of      uuid        NULL,
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
  CHECK (action NOT IN ('adjustment_requested', 'reopened', 'withdrawn') OR char_length(btrim(coalesce(reason, ''))) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX report_task_event_task ON pf_extension.report_task_event (tenant_id, task_id, id);
SELECT shared.apply_ledger('pf_extension.report_task_event', 'pf_extension.report_task_event');

CREATE TABLE pf_extension.report_task (
  tenant_id               uuid        NOT NULL,
  id                      uuid        NOT NULL,
  batch_id                uuid        NOT NULL,
  period_id               uuid        NOT NULL,
  template_id             uuid        NOT NULL,              -- mẫu pf_reports (tham chiếu thuần, R-06)
  version_id              uuid        NOT NULL,              -- phiên bản mẫu ghim lúc phát hành
  form_code               text        NOT NULL,
  department_code         text        NOT NULL,
  province_code           text        NOT NULL,
  commune_code            text        NOT NULL,
  region_code             text        NOT NULL DEFAULT '',   -- khu vực lúc phát hành ('' = chưa gán)
  slot                    integer     NOT NULL CHECK (slot >= 1),
  period_block            boolean     NOT NULL,              -- mẫu có khối cột theo kỳ (PL 01, 01B, 02, 07)
  due_at                  timestamptz NOT NULL,
  status                  text        NOT NULL CHECK (status IN ('not_started', 'draft', 'submitted', 'adjustment_requested', 'confirmed')),
  assignees               uuid[]      NOT NULL DEFAULT '{}', -- cán bộ xã phụ trách cùng phòng ban lúc phát hành
  latest_version          integer     NOT NULL DEFAULT 0,
  latest_submission_id    uuid        NULL,
  submitted_submission_id uuid        NULL,                  -- bản "Đã gửi" mới nhất — nguồn tổng hợp
  submitted_at            timestamptz NULL,
  submitted_by            uuid        NULL,
  on_behalf_of            uuid        NULL,
  first_submitted_at      timestamptz NULL,                  -- nộp đúng hạn (thống kê FN-XA-03.07)
  adjustment_reason       text        NULL,
  adjustment_requested_at timestamptz NULL,
  confirmed_by            uuid        NULL,
  confirmed_at            timestamptz NULL,
  last_reminded_at        timestamptz NULL,
  reopened_until          timestamptz NULL,                  -- mở lại kỳ cho xã/biểu này đến lúc
  withdrawn               boolean     NOT NULL DEFAULT false,
  last_event_id           uuid        NULL,
  created_at              timestamptz NOT NULL,
  updated_at              timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, period_id, template_id, commune_code),
  FOREIGN KEY (tenant_id, batch_id) REFERENCES pf_extension.report_batch (tenant_id, id),
  FOREIGN KEY (tenant_id, period_id) REFERENCES pf_extension.report_period (tenant_id, id),
  CHECK ((submitted_submission_id IS NULL) = (submitted_at IS NULL))
);
CREATE INDEX report_task_commune ON pf_extension.report_task (tenant_id, department_code, commune_code, due_at) WHERE NOT withdrawn;
CREATE INDEX report_task_period ON pf_extension.report_task (tenant_id, period_id, template_id);
CREATE INDEX report_task_batch ON pf_extension.report_task (tenant_id, batch_id, status);
SELECT shared.apply_tenant_rls('pf_extension.report_task');

-- ---------------------------------------------------------------------------
-- 4. Giá trị cột Tỉnh giao
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.provincial_value (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'pv:<batch_id>'
  seq               bigint      NOT NULL,
  batch_id          uuid        NOT NULL,
  template_id       uuid        NOT NULL,
  commune_code      text        NOT NULL,
  cells             jsonb       NOT NULL,                 -- {chữ cái cột: giá trị decimal dạng chuỗi}
  source            text        NOT NULL CHECK (source IN ('manual', 'excel', 'previous_batch')),
  source_batch_id   uuid        NULL,
  department_code   text        NOT NULL,
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
  CHECK ((source = 'previous_batch') = (source_batch_id IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX provincial_value_batch ON pf_extension.provincial_value (tenant_id, batch_id, template_id, commune_code);
SELECT shared.apply_ledger('pf_extension.provincial_value', 'pf_extension.provincial_value');

GRANT SELECT, INSERT ON pf_extension.report_batch_event, pf_extension.report_task_event, pf_extension.provincial_value TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.report_batch_event, pf_extension.report_task_event, pf_extension.provincial_value FROM extension_app;
GRANT SELECT, INSERT, UPDATE ON pf_extension.report_batch, pf_extension.report_period, pf_extension.report_task TO extension_app;
REVOKE DELETE, TRUNCATE ON pf_extension.report_batch, pf_extension.report_period, pf_extension.report_task FROM extension_app;
