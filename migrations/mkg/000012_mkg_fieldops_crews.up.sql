-- mkg_fieldops G3 (bù khoảng trống nghiệm thu): sổ tổ gom có phiên bản, phân công có năng
-- lực, kiểm ngày gặt khi cuộn, hạn cuộn sau gặt (docs/uat/g3-acceptance.md, prototype
-- mekong-green src/erp/field/service.ts FM-01, FM-02, FM-06, FM-07, FM-08).
--
--   crew_event                 SỔ tổ gom (stream 'crewdir:<crew_id>'): created/revised/deactivated/
--                              reactivated; mỗi sự kiện là một phiên bản đầy đủ. Không xóa cứng (RB-1).
--                              Thành viên = subject tài khoản MKG (UUID), không có dữ liệu cá nhân (R-04).
--   crew_current               bảng chiếu tổ gom hiện hành (mã duy nhất theo tenant).
--   crew_assignment            SỔ phân công (stream 'assign:<order_id>'): tổ, phiên bản tổ, ngày dự kiến,
--                              khối lượng việc, tải/ngày, vượt năng lực + lý do của giám sát (FM-06),
--                              cách giao (manual | suggested, FM-08). Cùng giao dịch với sự kiện
--                              'assigned' của collection_order_event.
--   crew_assignment_current    bảng chiếu phân công hiện hành theo lệnh gom (tải của tổ).
--   roll_count_harvest         SỔ kết quả kiểm ngày gặt của bản ghi cuộn gốc (FM-01, stream
--                              'harvest:<roll_count_id>'): ok | unknown | unavailable. Không bao giờ im lặng.
--   order_plot_harvest_current bảng đệm ngày gặt theo (lệnh gom, thửa) đọc từ nền tảng qua cổng
--                              HarvestDates (bề mặt partner, ADR-0020) — dựng lại được, không phải nguồn sự thật.
--   param_version              SỔ tham số có phiên bản của fieldops (stream 'param:<key>'): đề xuất / duyệt /
--                              từ chối; người duyệt ≠ người đề xuất (R-56, CHECK DB). G3 chỉ có
--                              baling_deadline_days (FM-02; mặc định 3 ngày — tạm, chờ khách).
--   param_current              bảng chiếu phiên bản tham số.
--
-- Không sửa bảng sổ đã có (thêm cột vào sổ làm verify_chain của dòng cũ lệch) — mọi dữ kiện
-- mới nằm ở sổ riêng, như roll_count_location (mkg 000010).

-- ---------------------------------------------------------------------------
-- 1. Sổ tổ gom
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.crew_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'crewdir:<crew_id>'
  seq               bigint        NOT NULL,
  crew_id           uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('created', 'revised', 'deactivated', 'reactivated')),
  version           integer       NOT NULL CHECK (version >= 1),
  code              text          NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
  name              text          NOT NULL CHECK (length(name) BETWEEN 2 AND 200),
  member_refs       uuid[]        NOT NULL CHECK (cardinality(member_refs) BETWEEN 1 AND 100),
  capacity_unit     text          NOT NULL CHECK (capacity_unit IN ('bales', 'ha')),
  capacity_per_day  numeric(12,3) NOT NULL CHECK (capacity_per_day > 0),
  service_communes  text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(service_communes) <= 50),
  status            text          NOT NULL CHECK (status IN ('active', 'inactive')),
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind <> 'created' OR (status = 'active' AND version = 1)),
  CHECK (kind <> 'deactivated' OR (status = 'inactive' AND reason IS NOT NULL)),
  CHECK (kind <> 'reactivated' OR (status = 'active' AND reason IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX crew_event_idem   ON mkg_fieldops.crew_event (tenant_id, idem_key);
CREATE INDEX crew_event_stream ON mkg_fieldops.crew_event (tenant_id, stream_key, seq);
CREATE INDEX crew_event_crew   ON mkg_fieldops.crew_event (tenant_id, crew_id, version);
SELECT shared.apply_ledger('mkg_fieldops.crew_event', 'mkg_fieldops.crew_event');

CREATE TABLE mkg_fieldops.crew_current (
  tenant_id         uuid          NOT NULL,
  crew_id           uuid          NOT NULL,
  code              text          NOT NULL,
  name              text          NOT NULL,
  member_refs       uuid[]        NOT NULL,
  capacity_unit     text          NOT NULL CHECK (capacity_unit IN ('bales', 'ha')),
  capacity_per_day  numeric(12,3) NOT NULL CHECK (capacity_per_day > 0),
  service_communes  text[]        NOT NULL,
  status            text          NOT NULL CHECK (status IN ('active', 'inactive')),
  version           integer       NOT NULL,
  last_seq          bigint        NOT NULL,
  created_at        timestamptz   NOT NULL,
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, crew_id),
  UNIQUE (tenant_id, code)
);
CREATE INDEX crew_current_status  ON mkg_fieldops.crew_current (tenant_id, status, code);
CREATE INDEX crew_current_members ON mkg_fieldops.crew_current USING gin (member_refs);
SELECT shared.apply_tenant_rls('mkg_fieldops.crew_current');

-- ---------------------------------------------------------------------------
-- 2. Phân công có năng lực (FM-06/07/08)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.crew_assignment (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'assign:<order_id>'
  seq               bigint        NOT NULL,
  order_id          uuid          NOT NULL,
  order_event_id    uuid          NOT NULL,                 -- sự kiện 'assigned' của collection_order_event
  crew_id           uuid          NOT NULL,
  crew_version      integer       NOT NULL CHECK (crew_version >= 1),
  capacity_unit     text          NOT NULL CHECK (capacity_unit IN ('bales', 'ha')),
  capacity_per_day  numeric(12,3) NOT NULL CHECK (capacity_per_day > 0),
  planned_from      date          NOT NULL,
  planned_to        date          NOT NULL,
  expected_qty      numeric(12,3) NOT NULL CHECK (expected_qty > 0),   -- theo capacity_unit
  daily_load        numeric(12,3) NOT NULL CHECK (daily_load > 0),
  peak_load         numeric(14,3) NOT NULL CHECK (peak_load >= 0),     -- tải cao nhất/ngày của tổ sau khi giao
  over_capacity     boolean       NOT NULL,
  override_reason   text          NULL CHECK (override_reason IS NULL OR length(override_reason) BETWEEN 10 AND 500),
  mode              text          NOT NULL CHECK (mode IN ('manual', 'suggested')),
  suggestion_rank   integer       NULL CHECK (suggestion_rank IS NULL OR suggestion_rank >= 1),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (planned_to >= planned_from),
  CHECK (NOT over_capacity OR override_reason IS NOT NULL),          -- FM-06: vượt năng lực chỉ khi giám sát ghi lý do
  CHECK (NOT over_capacity OR mode = 'manual')                        -- FM-08: gợi ý không bao giờ vượt năng lực
) PARTITION BY RANGE (recorded_at);
CREATE INDEX crew_assignment_idem   ON mkg_fieldops.crew_assignment (tenant_id, idem_key);
CREATE INDEX crew_assignment_stream ON mkg_fieldops.crew_assignment (tenant_id, stream_key, seq);
CREATE INDEX crew_assignment_order  ON mkg_fieldops.crew_assignment (tenant_id, order_id, seq);
SELECT shared.apply_ledger('mkg_fieldops.crew_assignment', 'mkg_fieldops.crew_assignment');

CREATE TABLE mkg_fieldops.crew_assignment_current (
  tenant_id        uuid          NOT NULL,
  order_id         uuid          NOT NULL,
  assignment_id    uuid          NOT NULL,
  crew_id          uuid          NOT NULL,
  crew_version     integer       NOT NULL,
  capacity_unit    text          NOT NULL,
  planned_from     date          NOT NULL,
  planned_to       date          NOT NULL,
  expected_qty     numeric(12,3) NOT NULL,
  daily_load       numeric(12,3) NOT NULL,
  over_capacity    boolean       NOT NULL,
  override_reason  text          NULL,
  mode             text          NOT NULL,
  updated_at       timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id)
);
CREATE INDEX crew_assignment_current_crew ON mkg_fieldops.crew_assignment_current (tenant_id, crew_id, planned_from, planned_to);
SELECT shared.apply_tenant_rls('mkg_fieldops.crew_assignment_current');

-- ---------------------------------------------------------------------------
-- 3. Ngày gặt (FM-01, FM-02)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.roll_count_harvest (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                   -- 'harvest:<roll_count_id>'
  seq               bigint      NOT NULL,
  roll_count_id     uuid        NOT NULL,
  order_id          uuid        NOT NULL,
  plot_ref          uuid        NOT NULL,
  bale_date         date        NOT NULL,                   -- ngày nghiệp vụ (giờ VN) của event_time bản ghi cuộn
  harvest_check     text        NOT NULL CHECK (harvest_check IN ('ok', 'unknown', 'unavailable')),
  harvest_date      date        NULL,                   -- ngày gặt dự kiến của vụ (expected_harvest_date) — gốc hạn FM-02
  harvest_open_date date        NULL,                   -- ngày mở cửa sổ gặt của vụ (harvest_open_date) — mốc FM-01
  season_ref        uuid        NULL,
  harvest_source    text        NOT NULL CHECK (harvest_source IN ('partner', 'cache', 'none')),
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
  CHECK ((harvest_check = 'ok') = (harvest_date IS NOT NULL)),
  CHECK (harvest_check <> 'ok' OR bale_date >= coalesce(harvest_open_date, harvest_date)),  -- FM-01 ở tầng DB (lớp hai sau app/)
  CHECK ((harvest_source = 'none') = (harvest_check <> 'ok'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX roll_count_harvest_idem   ON mkg_fieldops.roll_count_harvest (tenant_id, idem_key);
CREATE INDEX roll_count_harvest_stream ON mkg_fieldops.roll_count_harvest (tenant_id, stream_key, seq);
CREATE INDEX roll_count_harvest_order  ON mkg_fieldops.roll_count_harvest (tenant_id, order_id, plot_ref);
SELECT shared.apply_ledger('mkg_fieldops.roll_count_harvest', 'mkg_fieldops.roll_count_harvest');

CREATE TABLE mkg_fieldops.order_plot_harvest_current (
  tenant_id     uuid        NOT NULL,
  order_id      uuid        NOT NULL,
  plot_ref      uuid        NOT NULL,
  harvest_check text        NOT NULL CHECK (harvest_check IN ('ok', 'unknown', 'unavailable')),
  harvest_date      date        NULL,               -- ngày gặt dự kiến (gốc hạn cuộn FM-02)
  harvest_open_date date        NULL,               -- ngày mở cửa sổ gặt (mốc FM-01)
  season_ref        uuid        NULL,
  fetched_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id, plot_ref),
  CHECK ((harvest_check = 'ok') = (harvest_date IS NOT NULL))
);
SELECT shared.apply_tenant_rls('mkg_fieldops.order_plot_harvest_current');

-- ---------------------------------------------------------------------------
-- 4. Tham số có phiên bản (FM-02 baling_deadline_days — tạm, chờ khách)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.param_version (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'param:<param_key>'
  seq               bigint        NOT NULL,
  param_key         text          NOT NULL CHECK (param_key IN ('baling_deadline_days')),
  version_id        uuid          NOT NULL,
  version_seq       integer       NOT NULL CHECK (version_seq >= 1),
  kind              text          NOT NULL CHECK (kind IN ('proposed', 'approved', 'rejected')),
  value             numeric(12,3) NOT NULL,
  effective_from    date          NOT NULL,
  provisional       boolean       NOT NULL,
  proposer_ref      uuid          NOT NULL,
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind <> 'proposed' OR (actor_ref = proposer_ref AND reason IS NOT NULL)),
  CHECK (kind = 'proposed' OR actor_ref <> proposer_ref),             -- R-56 ở tầng DB
  CHECK (kind <> 'rejected' OR reason IS NOT NULL),
  CHECK (param_key <> 'baling_deadline_days' OR (value = trunc(value) AND value BETWEEN 0 AND 60))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_version_idem   ON mkg_fieldops.param_version (tenant_id, idem_key);
CREATE INDEX param_version_stream ON mkg_fieldops.param_version (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_fieldops.param_version', 'mkg_fieldops.param_version');

CREATE TABLE mkg_fieldops.param_current (
  tenant_id      uuid          NOT NULL,
  param_key      text          NOT NULL,
  version_id     uuid          NOT NULL,
  version_seq    integer       NOT NULL,
  value          numeric(12,3) NOT NULL,
  effective_from date          NOT NULL,
  provisional    boolean       NOT NULL,
  status         text          NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  proposed_by    uuid          NOT NULL,
  proposal_reason text         NOT NULL,
  decided_by     uuid          NULL,
  decided_at     timestamptz   NULL,
  decision_reason text         NULL,
  updated_at     timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, param_key, version_id),
  UNIQUE (tenant_id, param_key, version_seq),
  CHECK (decided_by IS NULL OR decided_by <> proposed_by)
);
CREATE INDEX param_current_effective ON mkg_fieldops.param_current (tenant_id, param_key, status, effective_from);
SELECT shared.apply_tenant_rls('mkg_fieldops.param_current');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_fieldops.crew_event, mkg_fieldops.crew_assignment, mkg_fieldops.roll_count_harvest,
                        mkg_fieldops.param_version TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.crew_event, mkg_fieldops.crew_assignment, mkg_fieldops.roll_count_harvest,
                                   mkg_fieldops.param_version FROM fieldops_app;
-- Bảng chiếu / bảng đệm: được UPDATE (R-21); DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_fieldops.crew_current, mkg_fieldops.crew_assignment_current,
                                        mkg_fieldops.order_plot_harvest_current, mkg_fieldops.param_current TO fieldops_app;
