-- pf_cropcycle (2/2): vụ mùa, nhật ký canh tác dạng sổ, duyệt nhật ký, khai báo
-- sản lượng và các bảng chiếu (App HTX FN-10/11/12/16; ADR-0011: nhật ký canh tác
-- là bằng chứng MRV nên là SỔ chỉ ghi thêm, sửa = bản ghi bù).
--
-- Sổ (mẫu mục 8.1, ADR-0012):
--   - season_event: vòng đời vụ — opened | corrected | completed | cancelled. Mỗi
--     vụ một chuỗi 'season:<season_ref>'. Ảnh chụp lúc ghi: thửa, HTX, xã, diện
--     tích (numeric(14,4)), phiên bản SOP đã ghim, người ghi ≠ nông hộ ("ghi hộ").
--   - farm_log_batch: đầu lô ghi nhật ký hàng loạt (HTX US-LOG-03) — một đầu lô +
--     một dòng farm_log cho mỗi thửa (giữ minh chứng từng thửa cho MRV).
--   - farm_log: phiên bản nhật ký; chuỗi 'farm_log:<log_ref>' (log_ref = id phiên
--     bản đầu). Sửa = phiên bản mới supersedes phiên bản hiện hành, GIỮ event_time
--     và GPS (HTX US-LOG-01 AC-3).
--   - farm_log_review: duyệt (approved | needs_supplement + ghi chú); người duyệt ≠
--     người ghi phiên bản (R-56 tinh thần, CHECK).
--   - harvest_declaration: sản lượng kg (shared/mass), năng suất t/ha tính từ diện
--     tích ảnh chụp; bất thường cần xác nhận (HTX US-YIELD-03).
-- Bảng chiếu (R-21 cùng giao dịch; R-22 dựng lại được từ sổ):
--   season_current, farm_log_current, coop_season_area, yield_summary.
-- Không có dữ liệu cá nhân (R-04): nông hộ chỉ là household_ref. Toạ độ GPS là
-- geometry(Point, 4326); gắn với hộ nên chỉ đọc qua API có phạm vi.

CREATE EXTENSION IF NOT EXISTS postgis;

-- ---------------------------------------------------------------------------
-- 1. Sổ vòng đời vụ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.season_event (
  id                      uuid                  NOT NULL,
  tenant_id               uuid                  NOT NULL,
  stream_key              text                  NOT NULL,           -- 'season:<season_ref>'
  seq                     bigint                NOT NULL,
  season_ref              uuid                  NOT NULL,
  action                  text                  NOT NULL CHECK (action IN ('opened', 'corrected', 'completed', 'cancelled')),
  plot_ref                uuid                  NOT NULL,
  plot_code               text                  NOT NULL CHECK (char_length(plot_code) BETWEEN 1 AND 40),
  household_ref           uuid                  NULL,
  cooperative_ref         uuid                  NOT NULL,
  commune_code            text                  NOT NULL,
  season_code             text                  NOT NULL CHECK (season_code ~ '^[A-Za-z0-9_]{1,40}$'),
  season_year             integer               NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  variety_code            text                  NOT NULL CHECK (variety_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
  sop_template_ref        uuid                  NOT NULL,
  sop_version_id          uuid                  NOT NULL,
  sop_version_no          integer               NOT NULL CHECK (sop_version_no >= 1),
  sowing_date             date                  NOT NULL,
  expected_harvest_date   date                  NOT NULL,
  harvest_open_date       date                  NOT NULL,           -- sạ + offset bước thu hoạch − cửa sổ (HTX US-YIELD-01 AC-1)
  area_ha                 numeric(14,4)         NOT NULL CHECK (area_ha > 0),
  expected_yield_t_per_ha numeric(9,4)          NULL CHECK (expected_yield_t_per_ha IS NULL OR expected_yield_t_per_ha >= 0),
  copied_from_season_ref  uuid                  NULL,               -- HTX US-SEASON-03
  harvest_id              uuid                  NULL,
  reason                  text                  NULL,
  gps                     geometry(Point, 4326) NULL,
  gps_accuracy_m          numeric(8,2)          NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  gps_source              text                  NOT NULL CHECK (gps_source IN ('device', 'photo_exif', 'manual_pick', 'denied', 'no_device')),
  location_status         text                  NOT NULL CHECK (location_status IN ('match', 'mismatch', 'missing')),
  distance_m              numeric(12,2)         NULL CHECK (distance_m IS NULL OR distance_m >= 0),
  on_behalf               boolean               NOT NULL,           -- người ghi không phải nông hộ (quản lý HTX ghi hộ)
  channel                 text                  NOT NULL CHECK (channel IN ('app', 'web', 'sync', 'import')),
  event_time              timestamptz           NOT NULL,
  recorded_at             timestamptz           NOT NULL DEFAULT now(),
  supersedes_id           uuid                  NULL,
  correction_reason       text                  NULL,
  actor_ref               uuid                  NOT NULL,
  device_id               text                  NULL,
  idem_key                text                  NOT NULL,
  rule_version            text                  NOT NULL,
  prev_hash               bytea                 NOT NULL,
  row_hash                bytea                 NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- Sửa và hủy vụ là bản ghi bù thay sự kiện hiện hành; mở và hoàn thành thì không.
  CHECK ((action IN ('corrected', 'cancelled')) = (supersedes_id IS NOT NULL)),
  CHECK ((action = 'completed') = (harvest_id IS NOT NULL)),
  CHECK (harvest_open_date >= sowing_date AND expected_harvest_date >= sowing_date)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX season_event_idem   ON pf_cropcycle.season_event (tenant_id, idem_key);
CREATE INDEX season_event_stream ON pf_cropcycle.season_event (tenant_id, stream_key, seq);
CREATE INDEX season_event_id     ON pf_cropcycle.season_event (id);
SELECT shared.apply_ledger('pf_cropcycle.season_event', 'pf_cropcycle.season_event');

-- ---------------------------------------------------------------------------
-- 2. Sổ đầu lô nhật ký hàng loạt (HTX US-LOG-03)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.farm_log_batch (
  id                uuid        NOT NULL,                              -- = batch_ref
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                              -- 'farm_log_batch:<id>'
  seq               bigint      NOT NULL,
  cooperative_ref   uuid        NULL,
  activity_code     text        NOT NULL CHECK (activity_code IN ('lam_dat', 'gieo_sa', 'tuoi_nuoc', 'bon_phan', 'phun_thuoc', 'kiem_tra', 'thu_hoach')),
  sop_step_seq      integer     NULL CHECK (sop_step_seq IS NULL OR sop_step_seq >= 1),
  requested         integer     NOT NULL CHECK (requested >= 1),
  accepted          integer     NOT NULL CHECK (accepted >= 0 AND accepted <= requested),
  skipped           jsonb       NOT NULL CHECK (jsonb_typeof(skipped) = 'array'),   -- [{plot_ref, code, reason}]
  machine_codes     text[]      NOT NULL DEFAULT '{}',
  inputs            jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(inputs) = 'array'),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  on_behalf         boolean     NOT NULL,
  channel           text        NOT NULL CHECK (channel IN ('app', 'web', 'sync')),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX farm_log_batch_idem   ON pf_cropcycle.farm_log_batch (tenant_id, idem_key);
CREATE INDEX farm_log_batch_stream ON pf_cropcycle.farm_log_batch (tenant_id, stream_key, seq);
CREATE INDEX farm_log_batch_id     ON pf_cropcycle.farm_log_batch (id);
SELECT shared.apply_ledger('pf_cropcycle.farm_log_batch', 'pf_cropcycle.farm_log_batch');

-- ---------------------------------------------------------------------------
-- 3. Sổ nhật ký canh tác
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.farm_log (
  id                uuid                  NOT NULL,                    -- id phiên bản; ngoại tuyến do máy sinh (R-11)
  tenant_id         uuid                  NOT NULL,
  stream_key        text                  NOT NULL,                    -- 'farm_log:<log_ref>'
  seq               bigint                NOT NULL,                    -- = số phiên bản
  log_ref           uuid                  NOT NULL,                    -- id phiên bản đầu
  season_ref        uuid                  NOT NULL,
  plot_ref          uuid                  NOT NULL,
  household_ref     uuid                  NULL,
  cooperative_ref   uuid                  NOT NULL,
  commune_code      text                  NOT NULL,
  activity_code     text                  NOT NULL CHECK (activity_code IN ('lam_dat', 'gieo_sa', 'tuoi_nuoc', 'bon_phan', 'phun_thuoc', 'kiem_tra', 'thu_hoach')),
  sop_step_seq      integer               NULL CHECK (sop_step_seq IS NULL OR sop_step_seq >= 1),
  gps               geometry(Point, 4326) NULL,
  gps_accuracy_m    numeric(8,2)          NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  gps_source        text                  NOT NULL CHECK (gps_source IN ('device', 'photo_exif', 'manual_pick', 'denied', 'no_device')),
  location_status   text                  NOT NULL CHECK (location_status IN ('match', 'mismatch', 'missing')),
  distance_m        numeric(12,2)         NULL CHECK (distance_m IS NULL OR distance_m >= 0),
  machine_codes     text[]                NOT NULL DEFAULT '{}',
  inputs            jsonb                 NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(inputs) = 'array'), -- [{name, qty (decimal chuỗi), unit}]
  note              text                  NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  file_refs         uuid[]                NOT NULL DEFAULT '{}',       -- đối tượng mã hóa ở pf_files
  batch_ref         uuid                  NULL,
  -- false: bản ghi bù ngoại tuyến dựa trên phiên bản không còn hiện hành — được giữ,
  -- không thành hiện hành, mở xung đột đồng bộ (mục 15).
  became_current    boolean               NOT NULL,
  on_behalf         boolean               NOT NULL,
  channel           text                  NOT NULL CHECK (channel IN ('app', 'web', 'sync')),
  event_time        timestamptz           NOT NULL,
  recorded_at       timestamptz           NOT NULL DEFAULT now(),
  supersedes_id     uuid                  NULL,
  correction_reason text                  NULL,
  actor_ref         uuid                  NOT NULL,
  device_id         text                  NULL,
  idem_key          text                  NOT NULL,
  rule_version      text                  NOT NULL,
  prev_hash         bytea                 NOT NULL,
  row_hash          bytea                 NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((supersedes_id IS NULL) = (log_ref = id)),
  CHECK (cardinality(file_refs) <= 20 AND cardinality(machine_codes) <= 20)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX farm_log_idem   ON pf_cropcycle.farm_log (tenant_id, idem_key);
CREATE INDEX farm_log_stream ON pf_cropcycle.farm_log (tenant_id, stream_key, seq);
CREATE INDEX farm_log_id     ON pf_cropcycle.farm_log (id);
CREATE INDEX farm_log_season ON pf_cropcycle.farm_log (tenant_id, season_ref);
SELECT shared.apply_ledger('pf_cropcycle.farm_log', 'pf_cropcycle.farm_log');

-- ---------------------------------------------------------------------------
-- 4. Sổ duyệt nhật ký (HTX US-LOG-05)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.farm_log_review (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                              -- 'farm_log_review:<log_ref>'
  seq               bigint      NOT NULL,
  log_ref           uuid        NOT NULL,
  log_version_id    uuid        NOT NULL,                              -- phiên bản được duyệt
  season_ref        uuid        NOT NULL,
  cooperative_ref   uuid        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('approved', 'needs_supplement')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  author_ref        uuid        NOT NULL,                              -- người ghi phiên bản được duyệt
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                              -- người duyệt
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- Yêu cầu bổ sung phải ghi rõ cần bổ sung gì.
  CHECK (decision <> 'needs_supplement' OR char_length(btrim(note)) >= 3),
  -- Người ghi nhật ký không tự duyệt (g2-conventions §1, tinh thần R-56).
  CHECK (author_ref <> actor_ref)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX farm_log_review_idem   ON pf_cropcycle.farm_log_review (tenant_id, idem_key);
CREATE INDEX farm_log_review_stream ON pf_cropcycle.farm_log_review (tenant_id, stream_key, seq);
CREATE INDEX farm_log_review_id     ON pf_cropcycle.farm_log_review (id);
SELECT shared.apply_ledger('pf_cropcycle.farm_log_review', 'pf_cropcycle.farm_log_review');

-- ---------------------------------------------------------------------------
-- 5. Sổ khai báo sản lượng (HTX US-YIELD-01..03)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.harvest_declaration (
  id                uuid                  NOT NULL,
  tenant_id         uuid                  NOT NULL,
  stream_key        text                  NOT NULL,                    -- 'harvest:<season_ref>'
  seq               bigint                NOT NULL,
  season_ref        uuid                  NOT NULL,
  plot_ref          uuid                  NOT NULL,
  household_ref     uuid                  NULL,
  cooperative_ref   uuid                  NOT NULL,
  commune_code      text                  NOT NULL,
  season_code       text                  NOT NULL,
  season_year       integer               NOT NULL,
  variety_code      text                  NOT NULL,
  harvest_date      date                  NOT NULL,
  mass_kg           numeric(14,3)         NOT NULL CHECK (mass_kg >= 0),
  area_ha           numeric(14,4)         NOT NULL CHECK (area_ha > 0),
  yield_t_per_ha    numeric(9,4)          NOT NULL CHECK (yield_t_per_ha >= 0),
  moisture_pct      numeric(5,2)          NULL CHECK (moisture_pct IS NULL OR (moisture_pct >= 0 AND moisture_pct <= 100)),
  anomaly_kind      text                  NULL CHECK (anomaly_kind IS NULL OR anomaly_kind IN ('zero', 'above_threshold')),
  anomaly_threshold numeric(9,4)          NULL,
  anomaly_confirmed boolean               NOT NULL,
  source            text                  NOT NULL CHECK (source IN ('app', 'web', 'excel', 'sync')),
  gps               geometry(Point, 4326) NULL,
  gps_accuracy_m    numeric(8,2)          NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  gps_source        text                  NOT NULL CHECK (gps_source IN ('device', 'photo_exif', 'manual_pick', 'denied', 'no_device')),
  location_status   text                  NOT NULL CHECK (location_status IN ('match', 'mismatch', 'missing')),
  distance_m        numeric(12,2)         NULL CHECK (distance_m IS NULL OR distance_m >= 0),
  on_behalf         boolean               NOT NULL,
  event_time        timestamptz           NOT NULL,
  recorded_at       timestamptz           NOT NULL DEFAULT now(),
  supersedes_id     uuid                  NULL,
  correction_reason text                  NULL,
  actor_ref         uuid                  NOT NULL,
  device_id         text                  NULL,
  idem_key          text                  NOT NULL,
  rule_version      text                  NOT NULL,
  prev_hash         bytea                 NOT NULL,
  row_hash          bytea                 NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- Sản lượng bất thường chỉ được ghi khi người khai đã xác nhận (HTX US-YIELD-03 AC-1).
  CHECK (anomaly_kind IS NULL OR anomaly_confirmed)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX harvest_declaration_idem   ON pf_cropcycle.harvest_declaration (tenant_id, idem_key);
CREATE INDEX harvest_declaration_stream ON pf_cropcycle.harvest_declaration (tenant_id, stream_key, seq);
CREATE INDEX harvest_declaration_id     ON pf_cropcycle.harvest_declaration (id);
SELECT shared.apply_ledger('pf_cropcycle.harvest_declaration', 'pf_cropcycle.harvest_declaration');

-- ---------------------------------------------------------------------------
-- 6. Bảng chiếu: vụ hiện hành (R-21; dựng lại từ season_event + harvest_declaration)
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_cropcycle.season_sync_seq;

CREATE TABLE pf_cropcycle.season_current (
  tenant_id               uuid          NOT NULL,
  season_ref              uuid          NOT NULL,
  plot_ref                uuid          NOT NULL,
  plot_code               text          NOT NULL,
  household_ref           uuid          NULL,
  cooperative_ref         uuid          NOT NULL,
  commune_code            text          NOT NULL,
  season_code             text          NOT NULL,
  season_year             integer       NOT NULL,
  variety_code            text          NOT NULL,
  sop_template_ref        uuid          NOT NULL,
  sop_version_id          uuid          NOT NULL,
  sop_version_no          integer       NOT NULL,
  sowing_date             date          NOT NULL,
  expected_harvest_date   date          NOT NULL,
  harvest_open_date       date          NOT NULL,
  area_ha                 numeric(14,4) NOT NULL CHECK (area_ha > 0),
  expected_yield_t_per_ha numeric(9,4)  NULL,
  status                  text          NOT NULL CHECK (status IN ('open', 'completed', 'cancelled')),
  current_event_id        uuid          NOT NULL,
  version                 bigint        NOT NULL CHECK (version >= 1),   -- seq của sự kiện hiện hành
  location_status         text          NOT NULL,
  gps_source              text          NOT NULL,
  on_behalf               boolean       NOT NULL,
  opened_by               uuid          NOT NULL,
  opened_at               timestamptz   NOT NULL,
  copied_from_season_ref  uuid          NULL,
  harvest_id              uuid          NULL,                             -- khai báo sản lượng hiện hành
  completed_at            timestamptz   NULL,
  sync_seq                bigint        NOT NULL DEFAULT nextval('pf_cropcycle.season_sync_seq'),
  updated_at              timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, season_ref),
  CHECK ((status = 'completed') = (harvest_id IS NOT NULL))
);
-- HTX US-SEASON-04: tối đa MỘT vụ đang mở trên một thửa (lưới an toàn sau khóa tư vấn).
CREATE UNIQUE INDEX season_current_one_open ON pf_cropcycle.season_current (tenant_id, plot_ref) WHERE status = 'open';
CREATE INDEX season_current_plot    ON pf_cropcycle.season_current (tenant_id, plot_ref, opened_at);
CREATE INDEX season_current_coop    ON pf_cropcycle.season_current (tenant_id, cooperative_ref, season_code, season_year);
CREATE INDEX season_current_commune ON pf_cropcycle.season_current (tenant_id, commune_code);
CREATE INDEX season_current_hh      ON pf_cropcycle.season_current (tenant_id, household_ref);
CREATE INDEX season_current_sync    ON pf_cropcycle.season_current (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_cropcycle.season_current');

-- ---------------------------------------------------------------------------
-- 7. Bảng chiếu: phiên bản nhật ký + trạng thái duyệt
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_cropcycle.farm_log_sync_seq;

CREATE TABLE pf_cropcycle.farm_log_current (
  tenant_id       uuid          NOT NULL,
  id              uuid          NOT NULL,                                 -- id phiên bản
  log_ref         uuid          NOT NULL,
  version         bigint        NOT NULL CHECK (version >= 1),
  season_ref      uuid          NOT NULL,
  plot_ref        uuid          NOT NULL,
  household_ref   uuid          NULL,
  cooperative_ref uuid          NOT NULL,
  commune_code    text          NOT NULL,
  activity_code   text          NOT NULL,
  sop_step_seq    integer       NULL,
  event_time      timestamptz   NOT NULL,
  gps_source      text          NOT NULL,
  location_status text          NOT NULL,
  distance_m      numeric(12,2) NULL,
  author_ref      uuid          NOT NULL,
  on_behalf       boolean       NOT NULL,
  batch_ref       uuid          NULL,
  status          text          NOT NULL CHECK (status IN ('pending', 'approved', 'needs_supplement', 'superseded')),
  superseded_by   uuid          NULL,
  last_review_id  uuid          NULL,
  review_note     text          NULL,
  reviewed_by     uuid          NULL,
  reviewed_at     timestamptz   NULL,
  recorded_at     timestamptz   NOT NULL,
  sync_seq        bigint        NOT NULL DEFAULT nextval('pf_cropcycle.farm_log_sync_seq'),
  updated_at      timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
-- Mỗi nhật ký có đúng một phiên bản chưa bị thay.
CREATE UNIQUE INDEX farm_log_current_live ON pf_cropcycle.farm_log_current (tenant_id, log_ref) WHERE status <> 'superseded';
CREATE INDEX farm_log_current_coop   ON pf_cropcycle.farm_log_current (tenant_id, cooperative_ref, status, event_time);
CREATE INDEX farm_log_current_season ON pf_cropcycle.farm_log_current (tenant_id, season_ref, status);
CREATE INDEX farm_log_current_sync   ON pf_cropcycle.farm_log_current (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_cropcycle.farm_log_current');

-- ---------------------------------------------------------------------------
-- 8. Bảng chiếu: diện tích vụ theo HTX (CGH FN-07 qua pf.cropcycle.coop_season_area.changed)
-- ---------------------------------------------------------------------------
-- Σ diện tích các vụ đang mở + đã hoàn thành theo (HTX, vụ, năm); vụ bị hủy không tính.
CREATE TABLE pf_cropcycle.coop_season_area (
  tenant_id         uuid          NOT NULL,
  cooperative_ref   uuid          NOT NULL,
  season_code       text          NOT NULL,
  season_year       integer       NOT NULL,
  area_ha           numeric(14,4) NOT NULL CHECK (area_ha >= 0),
  seasons           integer       NOT NULL CHECK (seasons >= 0),
  open_seasons      integer       NOT NULL CHECK (open_seasons >= 0),
  completed_seasons integer       NOT NULL CHECK (completed_seasons >= 0),
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cooperative_ref, season_code, season_year)
);
SELECT shared.apply_tenant_rls('pf_cropcycle.coop_season_area');

-- ---------------------------------------------------------------------------
-- 9. Bảng chiếu: sản lượng dự kiến – thực tế và tiến độ SOP theo vụ (HTX US-DASH-01)
-- ---------------------------------------------------------------------------
-- Chỉ nhật ký ĐÃ DUYỆT tính vào tiến độ SOP (HTX US-LOG-05 AC-2).
CREATE TABLE pf_cropcycle.yield_summary (
  tenant_id               uuid          NOT NULL,
  season_ref              uuid          NOT NULL,
  plot_ref                uuid          NOT NULL,
  household_ref           uuid          NULL,
  cooperative_ref         uuid          NOT NULL,
  commune_code            text          NOT NULL,
  season_code             text          NOT NULL,
  season_year             integer       NOT NULL,
  variety_code            text          NOT NULL,
  status                  text          NOT NULL,
  area_ha                 numeric(14,4) NOT NULL,
  expected_yield_t_per_ha numeric(9,4)  NULL,
  expected_kg             numeric(14,3) NULL,
  harvest_id              uuid          NULL,
  actual_kg               numeric(14,3) NULL,
  actual_yield_t_per_ha   numeric(9,4)  NULL,
  harvest_date            date          NULL,
  sop_steps               integer       NOT NULL,
  sop_mandatory_steps     integer       NOT NULL,
  sop_steps_done          integer       NOT NULL,
  sop_mandatory_done      integer       NOT NULL,
  approved_logs           integer       NOT NULL,
  pending_logs            integer       NOT NULL,
  updated_at              timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, season_ref)
);
CREATE INDEX yield_summary_coop ON pf_cropcycle.yield_summary (tenant_id, cooperative_ref, season_code, season_year);
SELECT shared.apply_tenant_rls('pf_cropcycle.yield_summary');

-- ---------------------------------------------------------------------------
-- 10. Quyền của cropcycle_app (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_cropcycle.season_event, pf_cropcycle.farm_log_batch, pf_cropcycle.farm_log,
  pf_cropcycle.farm_log_review, pf_cropcycle.harvest_declaration TO cropcycle_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_cropcycle.season_event, pf_cropcycle.farm_log_batch, pf_cropcycle.farm_log,
  pf_cropcycle.farm_log_review, pf_cropcycle.harvest_declaration FROM cropcycle_app;
-- DELETE trên bảng chiếu chỉ để dựng lại từ sổ (R-22); bảng chiếu không phải sổ.
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_cropcycle.season_current, pf_cropcycle.farm_log_current,
  pf_cropcycle.coop_season_area, pf_cropcycle.yield_summary TO cropcycle_app;
GRANT USAGE ON SEQUENCE pf_cropcycle.season_sync_seq, pf_cropcycle.farm_log_sync_seq TO cropcycle_app;
