-- pf_mechanization: Bản đồ số cơ giới hóa (CGH BRD v1.3, ADR-0011).
--
-- FN-04 chủ sở hữu máy · FN-05 hồ sơ máy · FN-06 diện tích canh tác & lịch mùa
-- vụ theo vụ · FN-09 cân đối cung–cầu (QT-01/QT-02) lưu theo vụ.
--
-- Mẫu (mục 8.1, R-21): mọi biến động là một dòng sổ chỉ ghi thêm (*_event) mang
-- ảnh chụp đầy đủ trạng thái SAU sự việc; bảng hiện tại cập nhật trong CÙNG giao
-- dịch và dựng lại được từ sổ (R-22). Kết quả cân đối là sổ bất biến (FN-10: báo
-- cáo đọc kết quả đã lưu, không tính lại).
--
-- KHÔNG có dữ liệu cá nhân (R-04): họ tên, SĐT chủ máy là cá nhân nằm ở kho định
-- danh; ở đây chỉ giữ person_ref. Tham chiếu HTX (pf_party) là UUID thuần (R-06).

CREATE SCHEMA pf_mechanization;
REVOKE ALL ON SCHEMA pf_mechanization FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_mechanization TO mechanization_app;

-- ---------------------------------------------------------------------------
-- Bộ đếm mã hiển thị CSH-<tỉnh>-00001 / MAY-<tỉnh>-00001 (FN-04 BR-02, FN-05 BR-02).
-- Mã là NHÃN hiển thị cấp một lần khi tạo, không đổi khi máy/HTX đổi tỉnh (ADR-0009).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.code_counter (
  tenant_id      uuid    NOT NULL,
  prefix         text    NOT NULL CHECK (prefix IN ('CSH', 'MAY')),
  province_label text    NOT NULL CHECK (province_label ~ '^[A-Z]{2,6}$'),
  last_value     integer NOT NULL CHECK (last_value > 0),
  PRIMARY KEY (tenant_id, prefix, province_label)
);
SELECT shared.apply_tenant_rls('pf_mechanization.code_counter');

-- ---------------------------------------------------------------------------
-- FN-04: chủ sở hữu máy. stream_key = 'owner:<owner_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.owner_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  owner_id          uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('created', 'deactivated', 'reactivated')),
  code              text        NOT NULL,
  owner_type        text        NOT NULL CHECK (owner_type IN ('thanh_vien_htx', 'htx', 'doanh_nghiep', 'khac')),
  cooperative_ref   uuid        NULL,
  person_ref        uuid        NULL,                     -- mã tham chiếu kho định danh
  display_name      text        NULL,                     -- tên tổ chức (DN/đơn vị khác) — không phải dữ liệu cá nhân
  active            boolean     NOT NULL,
  reason            text        NULL,
  machines_affected integer     NOT NULL DEFAULT 0,
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
  CHECK (action <> 'deactivated' OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX owner_event_idem   ON pf_mechanization.owner_event (tenant_id, idem_key);
CREATE INDEX owner_event_stream ON pf_mechanization.owner_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mechanization.owner_event', 'pf_mechanization.owner_event');

CREATE TABLE pf_mechanization.owner (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  code            text        NOT NULL CHECK (code ~ '^CSH-[A-Z]{2,6}-[0-9]{5,}$'),
  owner_type      text        NOT NULL CHECK (owner_type IN ('thanh_vien_htx', 'htx', 'doanh_nghiep', 'khac')),
  cooperative_ref uuid        NULL,
  person_ref      uuid        NULL,
  display_name    text        NULL CHECK (display_name IS NULL OR length(display_name) BETWEEN 1 AND 200),
  active          boolean     NOT NULL DEFAULT true,
  status_reason   text        NULL,
  last_seq        bigint      NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, code),
  -- FN-04 BR-04: Thành viên HTX / HTX liên kết đúng một HTX.
  CHECK (owner_type NOT IN ('thanh_vien_htx', 'htx') OR cooperative_ref IS NOT NULL),
  -- Cá nhân (thành viên HTX) phải có định danh trong kho; HTX không có.
  CHECK (owner_type <> 'thanh_vien_htx' OR person_ref IS NOT NULL),
  CHECK (owner_type <> 'htx' OR person_ref IS NULL)
);
-- Chống trùng chủ sở hữu theo định danh trong kho (SĐT — chỉ mục mù ở vault).
CREATE UNIQUE INDEX owner_person_unique ON pf_mechanization.owner (tenant_id, person_ref) WHERE person_ref IS NOT NULL;
-- Mỗi HTX chỉ có một hồ sơ chủ sở hữu loại "HTX".
CREATE UNIQUE INDEX owner_coop_unique ON pf_mechanization.owner (tenant_id, cooperative_ref) WHERE owner_type = 'htx';
CREATE INDEX owner_coop ON pf_mechanization.owner (tenant_id, cooperative_ref);
SELECT shared.apply_tenant_rls('pf_mechanization.owner');

-- ---------------------------------------------------------------------------
-- FN-05: máy móc – thiết bị. stream_key = 'machine:<machine_ref>'.
-- Sổ là nguồn của tình trạng theo thời gian: tình trạng tại T = dòng có
-- event_time ≤ T mới nhất (thiếu thì dòng đầu tiên).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.machine_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  machine_ref       uuid          NOT NULL,
  action            text          NOT NULL CHECK (action IN ('created', 'updated', 'status_changed', 'deactivated', 'reactivated')),
  source            text          NOT NULL CHECK (source IN ('manual', 'import', 'app_htx')),
  code              text          NOT NULL,
  machine_type      text          NOT NULL,
  farm_stage        text          NOT NULL,
  cooperative_ref   uuid          NOT NULL,
  owner_id          uuid          NOT NULL,
  brand             text          NULL,
  model             text          NULL,
  serial_no         text          NULL,
  chassis_no        text          NULL,
  year_made         integer       NULL,
  fuel              text          NULL,
  power_hp          numeric(10,2) NULL,
  capacity_ha       numeric(14,4) NULL,
  owned_since       date          NOT NULL,
  status            text          NOT NULL CHECK (status IN ('hoat_dong', 'bao_tri', 'hong', 'ngung_hoat_dong')),
  active            boolean       NOT NULL,
  deactivated_on    date          NULL,
  reason            text          NULL,
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (action <> 'deactivated' OR (deactivated_on IS NOT NULL AND char_length(btrim(reason)) >= 10))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX machine_event_idem    ON pf_mechanization.machine_event (tenant_id, idem_key);
CREATE INDEX machine_event_stream  ON pf_mechanization.machine_event (tenant_id, stream_key, seq);
CREATE INDEX machine_event_machine ON pf_mechanization.machine_event (tenant_id, machine_ref, event_time);
SELECT shared.apply_ledger('pf_mechanization.machine_event', 'pf_mechanization.machine_event');

CREATE TABLE pf_mechanization.machine (
  tenant_id       uuid          NOT NULL,
  ref             uuid          NOT NULL,
  code            text          NOT NULL CHECK (code ~ '^MAY-[A-Z]{2,6}-[0-9]{5,}$'),
  machine_type    text          NOT NULL,
  farm_stage      text          NOT NULL,             -- suy ra từ chủng loại (FN-05 BR-03, AC-03)
  cooperative_ref uuid          NOT NULL,
  owner_id        uuid          NOT NULL,
  brand           text          NULL,
  model           text          NULL,
  serial_no       text          NULL CHECK (serial_no IS NULL OR length(serial_no) BETWEEN 1 AND 80),
  chassis_no      text          NULL CHECK (chassis_no IS NULL OR length(chassis_no) BETWEEN 1 AND 80),
  year_made       integer       NULL CHECK (year_made IS NULL OR year_made BETWEEN 1950 AND 2100),
  fuel            text          NULL,
  power_hp        numeric(10,2) NULL CHECK (power_hp IS NULL OR power_hp >= 0),
  capacity_ha     numeric(14,4) NULL CHECK (capacity_ha IS NULL OR capacity_ha >= 0),
  owned_since     date          NOT NULL,
  status          text          NOT NULL CHECK (status IN ('hoat_dong', 'bao_tri', 'hong', 'ngung_hoat_dong')),
  status_source   text          NOT NULL CHECK (status_source IN ('manual', 'import', 'app_htx')),
  status_at       timestamptz   NOT NULL,
  active          boolean       NOT NULL DEFAULT true,
  deactivated_on  date          NULL,
  status_reason   text          NULL,
  last_seq        bigint        NOT NULL,
  created_at      timestamptz   NOT NULL DEFAULT now(),
  updated_at      timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code),
  FOREIGN KEY (tenant_id, owner_id) REFERENCES pf_mechanization.owner (tenant_id, id),
  -- FN-05 BR-01: phải có Số máy/SN hoặc Số khung.
  CHECK (serial_no IS NOT NULL OR chassis_no IS NOT NULL),
  CHECK (deactivated_on IS NULL OR deactivated_on >= owned_since),
  CHECK (active = (deactivated_on IS NULL))
);
-- FN-05 BR-01: định danh vật lý duy nhất trong tenant, kể cả máy đã vô hiệu hóa.
CREATE UNIQUE INDEX machine_serial_unique  ON pf_mechanization.machine (tenant_id, serial_no) WHERE serial_no IS NOT NULL;
CREATE UNIQUE INDEX machine_chassis_unique ON pf_mechanization.machine (tenant_id, chassis_no) WHERE chassis_no IS NOT NULL;
CREATE INDEX machine_coop  ON pf_mechanization.machine (tenant_id, cooperative_ref, machine_type);
CREATE INDEX machine_owner ON pf_mechanization.machine (tenant_id, owner_id);
SELECT shared.apply_tenant_rls('pf_mechanization.machine');

-- ---------------------------------------------------------------------------
-- FN-06: diện tích canh tác & lịch mùa vụ theo HTX × vụ, nhãn nguồn và cờ khóa
-- (QT-03). stream_key = 'area:<area_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.season_area_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  area_id           uuid          NOT NULL,
  action            text          NOT NULL CHECK (action IN ('recorded', 'synced', 'locked', 'unlocked')),
  cooperative_ref   uuid          NOT NULL,
  season_code       text          NOT NULL,
  season_year       integer       NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  area_ha           numeric(14,4) NOT NULL CHECK (area_ha >= 0),
  source            text          NOT NULL CHECK (source IN ('app_htx', 'manual')),
  locked            boolean       NOT NULL,
  calendar          jsonb         NOT NULL DEFAULT '[]'::jsonb,
  reason            text          NULL,
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX season_area_event_idem   ON pf_mechanization.season_area_event (tenant_id, idem_key);
CREATE INDEX season_area_event_stream ON pf_mechanization.season_area_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mechanization.season_area_event', 'pf_mechanization.season_area_event');

CREATE TABLE pf_mechanization.season_area (
  tenant_id       uuid          NOT NULL,
  id              uuid          NOT NULL,
  cooperative_ref uuid          NOT NULL,
  season_code     text          NOT NULL,
  season_year     integer       NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  commune_code    text          NULL,
  area_ha         numeric(14,4) NOT NULL CHECK (area_ha >= 0),
  source          text          NOT NULL CHECK (source IN ('app_htx', 'manual')),
  locked          boolean       NOT NULL DEFAULT false,
  calendar        jsonb         NOT NULL DEFAULT '[]'::jsonb,
  source_at       timestamptz   NOT NULL,             -- thời điểm cập nhật của nguồn (nhãn [App HTX]/[Nhập tay])
  last_seq        bigint        NOT NULL,
  updated_at      timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, cooperative_ref, season_code, season_year)
);
CREATE INDEX season_area_season ON pf_mechanization.season_area (tenant_id, season_code, season_year);
SELECT shared.apply_tenant_rls('pf_mechanization.season_area');

-- ---------------------------------------------------------------------------
-- FN-09: kết quả cân đối lưu theo vụ — BẤT BIẾN. Mỗi lần tính là một balance_run
-- (stream 'season:<mã vụ>-<năm>') và các dòng balance_result (stream
-- 'run:<run_id>') theo HTX × khâu × chủng loại, kèm rule_version và phiên bản
-- tham số đã dùng. Tính lại tạo lần tính mới; báo cáo đọc lần tính mới nhất.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.balance_run (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  season_code       text        NOT NULL,
  season_year       integer     NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  reference_date    date        NOT NULL,             -- T: định mức, ngưỡng, số máy lấy tại thời điểm này
  pilot_stages      text[]      NOT NULL,
  thresholds        jsonb       NOT NULL,             -- giá trị + phiên bản tham số QT-02 đã dùng (null nếu chưa cấu hình)
  result_count      integer     NOT NULL CHECK (result_count >= 0),
  status_counts     jsonb       NOT NULL,
  missing_norms     text[]      NOT NULL DEFAULT '{}',
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
CREATE INDEX balance_run_idem   ON pf_mechanization.balance_run (tenant_id, idem_key);
CREATE INDEX balance_run_stream ON pf_mechanization.balance_run (tenant_id, stream_key, seq);
CREATE INDEX balance_run_id     ON pf_mechanization.balance_run (tenant_id, id);
SELECT shared.apply_ledger('pf_mechanization.balance_run', 'pf_mechanization.balance_run');

CREATE TABLE pf_mechanization.balance_result (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  run_id            uuid          NOT NULL,
  season_code       text          NOT NULL,
  season_year       integer       NOT NULL,
  reference_date    date          NOT NULL,
  cooperative_ref   uuid          NOT NULL,
  commune_code      text          NULL,
  province_code     text          NULL,
  farm_stage        text          NOT NULL,
  machine_type      text          NOT NULL,
  area_ha           numeric(14,4) NULL,
  area_source       text          NULL CHECK (area_source IS NULL OR area_source IN ('app_htx', 'manual')),
  area_locked       boolean       NOT NULL DEFAULT false,
  norm_key          text          NOT NULL,
  norm_value        numeric(14,4) NULL,               -- ha/máy/vụ (QT-01)
  norm_version      integer       NULL,
  norm_source_doc   text          NULL,
  required          numeric(14,4) NULL,               -- số máy cần = diện tích ÷ định mức (chưa làm tròn)
  required_whole    integer       NULL,               -- làm tròn lên, để hiển thị "Thiếu N"
  machines_total    integer       NOT NULL CHECK (machines_total >= 0),
  capacity          integer       NOT NULL CHECK (capacity >= 0),   -- máy "Hoạt động" tại T
  coverage_pct      numeric(9,4)  NULL,
  gap               integer       NULL,               -- capacity − required_whole
  status            text          NOT NULL CHECK (status IN ('sufficient', 'attention', 'shortage', 'surplus', 'no_data', 'no_norm', 'unclassified')),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX balance_result_idem ON pf_mechanization.balance_result (tenant_id, idem_key);
CREATE INDEX balance_result_run  ON pf_mechanization.balance_result (tenant_id, run_id, seq);
SELECT shared.apply_ledger('pf_mechanization.balance_result', 'pf_mechanization.balance_result');

-- ---------------------------------------------------------------------------
-- Quyền của mechanization_app. Sổ: chỉ SELECT, INSERT. Bảng hiện tại: không
-- DELETE (không xóa cứng — vô hiệu hóa là trạng thái, FN-05 BR-06/BR-10).
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_mechanization.owner_event, pf_mechanization.machine_event,
  pf_mechanization.season_area_event, pf_mechanization.balance_run, pf_mechanization.balance_result TO mechanization_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mechanization.owner_event, pf_mechanization.machine_event,
  pf_mechanization.season_area_event, pf_mechanization.balance_run, pf_mechanization.balance_result FROM mechanization_app;
GRANT SELECT, INSERT, UPDATE ON pf_mechanization.code_counter, pf_mechanization.owner, pf_mechanization.machine,
  pf_mechanization.season_area TO mechanization_app;
