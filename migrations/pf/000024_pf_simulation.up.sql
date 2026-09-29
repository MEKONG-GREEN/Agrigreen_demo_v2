-- pf_simulation: mô phỏng chọn Hub (mục 12, 16) — Hub ứng viên, kịch bản (sổ +
-- bảng chiếu), việc mô phỏng, ảnh chụp đầu vào BẤT BIẾN, kết quả BẤT BIẾN và kết
-- quả độ nhạy. Engine tính nằm ở optimizer/ (Python 3.12), không ở Go.
--
-- Nguồn: BRD Supply Chain Hub Simulation v1.4 (FN-01..FN-19, FN-17), đặc tả chạy
-- được mekong-green src/erp/simulation (ENGINE_VERSION 2026.09.24).
--
-- KHÔNG có dữ liệu cá nhân (R-04, RB-7): HTX chỉ là cooperative_ref; ảnh chụp
-- gửi optimizer không có tên, không toạ độ.
--
--   1. candidate_hub     Hub ứng viên (FN-02/03) — ngừng dùng bằng trạng thái kèm lý do
--   2. scenario_change   SỔ thay đổi kịch bản (tạo, gán/sửa/gỡ Hub, yêu cầu/nhận kết quả, chính thức)
--   3. scenario          bảng chiếu kịch bản (R-21)
--   4. scenario_hub      bảng chiếu cấu hình Hub × kịch bản (FN-02 BR-03)
--   5. simulation        việc mô phỏng (= job, R-52): chỉ đổi được trạng thái
--   6. input_snapshot    ảnh chụp đầu vào bất biến + sha256 (mục 16)
--   7. result            kết quả bất biến + cột tóm tắt cho so sánh (FN-14)
--   8. sensitivity_result kết quả độ nhạy bất biến (FN-18)

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA pf_simulation;
REVOKE ALL ON SCHEMA pf_simulation FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_simulation TO simulation_app;

-- ---------------------------------------------------------------------------
-- 1. Hub ứng viên. Toạ độ là vị trí cơ sở kinh doanh (không phải dữ liệu cá nhân);
-- không bao giờ vào ảnh chụp gửi optimizer — chỉ khoảng cách đã tính.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.candidate_hub (
  tenant_id      uuid                  NOT NULL,
  id             uuid                  NOT NULL,
  code           text                  NOT NULL CHECK (code ~ '^HUB-[0-9]{4,}$'),
  name           text                  NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
  location       geometry(Point, 4326) NOT NULL,
  unit_code      text                  NULL,
  status         text                  NOT NULL CHECK (status IN ('active', 'retired')),
  retired_reason text                  NULL CHECK (retired_reason IS NULL OR char_length(retired_reason) BETWEEN 10 AND 500),
  created_by     uuid                  NOT NULL,
  created_at     timestamptz           NOT NULL,
  updated_at     timestamptz           NOT NULL,
  idem_key       text                  NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, code),
  UNIQUE (tenant_id, idem_key),
  CHECK ((status = 'retired') = (retired_reason IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_simulation.candidate_hub');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_simulation.candidate_hub
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_simulation.candidate_hub
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Sổ thay đổi kịch bản (mẫu mục 8.1). stream_key = 'scenario:<id>'. Gỡ Hub là
-- bản ghi bù: supersedes_id trỏ bản ghi gán/sửa, kèm lý do. Yêu cầu mô phỏng và
-- nhận kết quả cũng ghi ở đây (nhật ký kiểm toán của mục 12).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.scenario_change (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  scenario_id       uuid        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('created', 'hub_attached', 'hub_updated', 'hub_detached',
                                                       'simulation_requested', 'simulation_completed', 'simulation_failed',
                                                       'marked_official')),
  hub_id            uuid        NULL,
  simulation_id     uuid        NULL,
  payload           jsonb       NOT NULL,
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
  CHECK (correction_reason IS NULL OR char_length(correction_reason) >= 10),
  CHECK ((kind = 'hub_detached') = (supersedes_id IS NOT NULL)),
  CHECK (kind NOT IN ('hub_attached', 'hub_updated', 'hub_detached') OR hub_id IS NOT NULL),
  CHECK (kind NOT IN ('simulation_requested', 'simulation_completed', 'simulation_failed', 'marked_official') OR simulation_id IS NOT NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX scenario_change_idem   ON pf_simulation.scenario_change (tenant_id, idem_key);
CREATE INDEX scenario_change_stream ON pf_simulation.scenario_change (tenant_id, stream_key, seq);
CREATE INDEX scenario_change_id     ON pf_simulation.scenario_change (id);

SELECT shared.apply_ledger('pf_simulation.scenario_change', 'pf_simulation.scenario_change');

-- ---------------------------------------------------------------------------
-- 3. Bảng chiếu kịch bản. Nhà máy đầu ra: toạ độ (định tuyến) + nhu cầu năm
-- (NULL → tham số #12). version = seq cuối của sổ.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.scenario (
  tenant_id                    uuid                  NOT NULL,
  id                           uuid                  NOT NULL,
  code                         text                  NOT NULL CHECK (code ~ '^KB-[0-9]{4,}$'),
  name                         text                  NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
  plant_ref                    uuid                  NULL,
  plant_location               geometry(Point, 4326) NOT NULL,
  plant_annual_demand_tons     numeric(14,3)         NULL CHECK (plant_annual_demand_tons IS NULL OR plant_annual_demand_tons > 0),
  lifecycle_years              integer               NOT NULL CHECK (lifecycle_years BETWEEN 1 AND 50),
  discounted                   boolean               NOT NULL,
  baseline_mode                text                  NOT NULL CHECK (baseline_mode IN ('no_hub', 'manual', 'scenario')),
  baseline_manual_cost_per_ton numeric(16,2)         NULL CHECK (baseline_manual_cost_per_ton IS NULL OR baseline_manual_cost_per_ton > 0),
  baseline_scenario_id         uuid                  NULL,
  reference_year               integer               NOT NULL CHECK (reference_year BETWEEN 2000 AND 2100),
  version                      bigint                NOT NULL,
  created_by                   uuid                  NOT NULL,
  created_at                   timestamptz           NOT NULL,
  updated_at                   timestamptz           NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, code),
  CHECK (baseline_mode <> 'manual' OR baseline_manual_cost_per_ton IS NOT NULL),
  CHECK (baseline_mode <> 'scenario' OR baseline_scenario_id IS NOT NULL)
);
SELECT shared.apply_tenant_rls('pf_simulation.scenario');

-- ---------------------------------------------------------------------------
-- 4. Bảng chiếu cấu hình Hub × kịch bản. Gỡ Hub: active = false (dòng giữ lại).
-- Công suất = TỒN KHO TỐI ĐA (tấn), không phải thông lượng năm.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.scenario_hub (
  tenant_id            uuid          NOT NULL,
  scenario_id          uuid          NOT NULL,
  hub_id               uuid          NOT NULL,
  id                   uuid          NOT NULL,                -- scenario_hub_id trong ảnh chụp
  service_radius_km    numeric(8,3)  NOT NULL CHECK (service_radius_km >= 0 AND service_radius_km <= 500),
  design_capacity_tons numeric(14,3) NOT NULL CHECK (design_capacity_tons >= 0),
  barge_payload_tons   numeric(10,3) NULL CHECK (barge_payload_tons IS NULL OR barge_payload_tons > 0),
  mode_field_hub       text          NOT NULL CHECK (mode_field_hub IN ('auto', 'road', 'waterway')),
  mode_hub_plant       text          NOT NULL CHECK (mode_hub_plant IN ('auto', 'road', 'waterway')),
  land_mode            text          NOT NULL CHECK (land_mode IN ('mua', 'thue')),
  sort_order           integer       NOT NULL,
  active               boolean       NOT NULL,
  change_id            uuid          NOT NULL,                -- bản ghi sổ tạo ra trạng thái này
  updated_at           timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, scenario_id, hub_id),
  FOREIGN KEY (tenant_id, scenario_id) REFERENCES pf_simulation.scenario (tenant_id, id),
  FOREIGN KEY (tenant_id, hub_id) REFERENCES pf_simulation.candidate_hub (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_simulation.scenario_hub');

-- ---------------------------------------------------------------------------
-- 5. Việc mô phỏng (mẫu mục 12). id = job_id. Trạng thái:
--   queued → running (đã có ảnh chụp, đã phát job.requested) → completed | failed.
-- Trường yêu cầu bất biến; đã completed/failed thì chỉ còn được đánh dấu chính thức.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.simulation (
  tenant_id        uuid        NOT NULL,
  id               uuid        NOT NULL,
  scenario_id      uuid        NOT NULL,
  scenario_version bigint      NOT NULL,
  options          jsonb       NOT NULL,
  requested_by     uuid        NOT NULL,
  requested_at     timestamptz NOT NULL,
  idem_key         text        NOT NULL,
  status           text        NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
  snapshot_id      uuid        NULL,
  error_code       text        NULL,
  error_detail     text        NULL CHECK (error_detail IS NULL OR char_length(error_detail) <= 500),
  completed_at     timestamptz NULL,
  official         boolean     NOT NULL DEFAULT false,
  official_by      uuid        NULL,
  official_at      timestamptz NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  FOREIGN KEY (tenant_id, scenario_id) REFERENCES pf_simulation.scenario (tenant_id, id),
  CHECK (status NOT IN ('running', 'completed') OR snapshot_id IS NOT NULL),
  CHECK ((status IN ('completed', 'failed')) = (completed_at IS NOT NULL)),
  CHECK (status <> 'failed' OR error_code IS NOT NULL),
  CHECK (official = (official_by IS NOT NULL AND official_at IS NOT NULL)),
  CHECK (NOT official OR status = 'completed')
);
CREATE INDEX simulation_scenario ON pf_simulation.simulation (tenant_id, scenario_id, requested_at DESC);
SELECT shared.apply_tenant_rls('pf_simulation.simulation');

CREATE FUNCTION pf_simulation.fn_simulation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.scenario_id <> OLD.scenario_id
     OR NEW.scenario_version <> OLD.scenario_version OR NEW.options <> OLD.options
     OR NEW.requested_by <> OLD.requested_by OR NEW.requested_at <> OLD.requested_at OR NEW.idem_key <> OLD.idem_key THEN
    RAISE EXCEPTION 'simulation %: yêu cầu mô phỏng bất biến', OLD.id USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.snapshot_id IS NOT NULL AND NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id THEN
    RAISE EXCEPTION 'simulation %: không đổi được ảnh chụp đã ghim', OLD.id USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.status IN ('completed', 'failed') THEN
    IF NEW.status <> OLD.status OR NEW.error_code IS DISTINCT FROM OLD.error_code
       OR NEW.completed_at IS DISTINCT FROM OLD.completed_at OR OLD.official THEN
      RAISE EXCEPTION 'simulation %: việc đã % là bất biến', OLD.id, OLD.status USING ERRCODE = 'insufficient_privilege';
    END IF;
  END IF;
  IF (OLD.status = 'queued' AND NEW.status NOT IN ('queued', 'running', 'failed'))
     OR (OLD.status = 'running' AND NEW.status NOT IN ('running', 'completed', 'failed')) THEN
    RAISE EXCEPTION 'simulation %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_simulation_guard BEFORE UPDATE ON pf_simulation.simulation
  FOR EACH ROW EXECUTE FUNCTION pf_simulation.fn_simulation_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_simulation.simulation
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_simulation.simulation
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 6. Ảnh chụp đầu vào BẤT BIẾN (mục 16): bộ tham số đã giải (phiên bản + cờ phê
-- duyệt), phiên bản mạng đường thủy, KẾT QUẢ ĐỊNH TUYẾN đã dùng (lưu bền vững,
-- không dựa cache), nguồn cung HTX × vụ, algorithm_version. sha256 tính trên JSON
-- chuẩn do Go tuần tự hóa — Internal API trả đúng các byte đó.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.input_snapshot (
  tenant_id           uuid        NOT NULL,
  id                  uuid        NOT NULL,
  simulation_id       uuid        NOT NULL,
  scenario_id         uuid        NOT NULL,
  payload             jsonb       NOT NULL,
  sha256              text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  param_set_version   text        NOT NULL,
  all_params_approved boolean     NOT NULL,
  network_version     text        NOT NULL,
  algorithm_version   text        NOT NULL,
  cooperative_count   integer     NOT NULL CHECK (cooperative_count >= 0),
  route_count         integer     NOT NULL CHECK (route_count >= 0),
  created_at          timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, simulation_id),
  FOREIGN KEY (tenant_id, simulation_id) REFERENCES pf_simulation.simulation (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_simulation.input_snapshot');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_simulation.input_snapshot
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_simulation.input_snapshot
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 7. Kết quả BẤT BIẾN do optimizer nộp (một kết quả cho mỗi việc; nộp lặp trả lại
-- bản đã lưu). Cột tóm tắt (numeric, R-10) phục vụ so sánh FN-14 mà không phải
-- đọc jsonb; nguồn sự thật vẫn là payload.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.result (
  tenant_id                      uuid          NOT NULL,
  simulation_id                  uuid          NOT NULL,
  snapshot_id                    uuid          NOT NULL,
  snapshot_sha256                text          NOT NULL,
  payload                        jsonb         NOT NULL,
  payload_sha256                 text          NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  engine_version                 text          NOT NULL,
  algorithm_version              text          NOT NULL,
  official_eligible              boolean       NOT NULL,   -- = ảnh chụp all_params_approved (FN-01 BR-04)
  parameter_set_version          text          NOT NULL,
  lifecycle_years                integer       NOT NULL,
  discounted                     boolean       NOT NULL,
  hub_count                      integer       NOT NULL,
  delivered_tons                 numeric(16,3) NOT NULL,
  capex                          numeric(20,2) NOT NULL,
  opex_year1                     numeric(20,2) NOT NULL,
  collection_cost                numeric(20,2) NOT NULL,
  transportation_cost            numeric(20,2) NOT NULL,
  warehouse_cost                 numeric(20,2) NOT NULL,
  handling_cost                  numeric(20,2) NOT NULL,
  total_logistics_cost           numeric(20,2) NOT NULL,
  cost_per_ton                   numeric(16,2) NULL,
  tco                            numeric(20,2) NOT NULL,
  tco_per_ton                    numeric(16,2) NULL,
  lifecycle_cost_per_ton         numeric(16,2) NULL,
  roi_pct                        numeric(12,2) NULL,
  payback_years                  integer       NULL,
  payback_status                 text          NOT NULL,
  plant_annual_demand_tons       numeric(16,3) NOT NULL,
  plant_demand_coverage_pct      numeric(10,2) NOT NULL,
  shortfall_warnings             integer       NOT NULL,
  baseline_available             boolean       NOT NULL,
  baseline_delivered_tons        numeric(16,3) NOT NULL,
  baseline_collection_cost       numeric(20,2) NOT NULL,
  baseline_transportation_cost   numeric(20,2) NOT NULL,
  baseline_total_cost            numeric(20,2) NOT NULL,
  baseline_cost_per_ton          numeric(16,2) NULL,
  received_at                    timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, simulation_id),
  FOREIGN KEY (tenant_id, simulation_id) REFERENCES pf_simulation.simulation (tenant_id, id),
  FOREIGN KEY (tenant_id, snapshot_id) REFERENCES pf_simulation.input_snapshot (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_simulation.result');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_simulation.result
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_simulation.result
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 8. Kết quả độ nhạy BẤT BIẾN (FN-18) — cùng lần nộp với kết quả chính.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_simulation.sensitivity_result (
  tenant_id        uuid          NOT NULL,
  simulation_id    uuid          NOT NULL,
  payload          jsonb         NOT NULL,
  payload_sha256   text          NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  high_sensitivity boolean       NOT NULL,
  swing_pct        numeric(12,2) NOT NULL,
  received_at      timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, simulation_id),
  FOREIGN KEY (tenant_id, simulation_id) REFERENCES pf_simulation.result (tenant_id, simulation_id)
);
SELECT shared.apply_tenant_rls('pf_simulation.sensitivity_result');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_simulation.sensitivity_result
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_simulation.sensitivity_result
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- Quyền của simulation_app. Sổ và bảng bất biến: SELECT, INSERT. Bảng trạng
-- thái/chiếu: thêm UPDATE. Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_simulation.scenario_change, pf_simulation.input_snapshot, pf_simulation.result,
  pf_simulation.sensitivity_result TO simulation_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_simulation.scenario_change, pf_simulation.input_snapshot, pf_simulation.result,
  pf_simulation.sensitivity_result FROM simulation_app;
GRANT SELECT, INSERT, UPDATE ON pf_simulation.candidate_hub, pf_simulation.scenario, pf_simulation.scenario_hub,
  pf_simulation.simulation TO simulation_app;
REVOKE DELETE, TRUNCATE ON pf_simulation.candidate_hub, pf_simulation.scenario, pf_simulation.scenario_hub,
  pf_simulation.simulation FROM simulation_app;
