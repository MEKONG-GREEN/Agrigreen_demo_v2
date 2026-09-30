-- mkg_logistics G4: chuyến, chặng đường thủy / đường bộ, ảnh chụp kết quả định tuyến đã dùng,
-- danh mục ghe có phiên bản, tham số có phiên bản (docs/design/mkg.md §5.4,
-- docs/dev/g4-conventions.md §1, §1.2, §2). Schema tạo ở mkg 000014.
--
--   trip_event      SỔ chuyến (stream 'trip:<trip_id>'): planned / revised / vehicle_assigned / departed /
--                   arrived / completed / cancelled. Bản bù (supersedes_id) chỉ cho completed (sửa khối
--                   lượng thật hoặc cước — proto plant weighing, xác nhận cước thực).
--   leg_event       SỔ chặng (stream 'legs:<trip_id>'): mỗi lần điều phối lập chặng là một phiên bản kế
--                   hoạch (plan_revision) gồm các chặng seq 1..n; phiên bản mới nhất là hiệu lực.
--   route_snapshot  SỔ ảnh chụp kết quả định tuyến ĐÃ DÙNG (stream 'route:<trip_id>'): NGUYÊN VĂN thân
--                   phản hồi + SHA-256 (CHECK trong DB) — tái hiện km và cước, không định tuyến lại (mục 16).
--   vessel_event    SỔ danh mục phương tiện (stream 'vessel:<vessel_id>'): created / revised, phiên bản
--                   tăng dần; số hiệu chuẩn hóa (proto GH-01).
--   param_version   SỔ tham số có phiên bản (stream 'param:<key>'): đề xuất (dispatcher) / duyệt / từ chối
--                   (chief_accountant) — người duyệt ≠ người đề xuất (R-56, CHECK DB). Mặc định tạm, chờ khách.
--
--   trip_current    bảng chiếu chuyến (R-21, cùng giao dịch với sổ); trạng thái CHỈ TIẾN — trigger
--                   fn_trip_forward chặn lùi ở tầng DB (lớp hai sau domain/).
--   vessel_current  bảng chiếu danh mục phương tiện, số hiệu duy nhất theo tenant.
--   param_current   bảng chiếu phiên bản tham số.
--
-- Không dữ liệu cá nhân (R-04, RB-3): tài xế, chủ ghe, người ghi là *_ref. Khối lượng numeric(14,3),
-- tiền numeric(18,2), đơn giá numeric(18,4), km numeric(10,3), tọa độ numeric(10,7) (R-10).

-- ---------------------------------------------------------------------------
-- 1. Sổ chuyến
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.trip_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,                 -- 'trip:<trip_id>'
  seq                  bigint        NOT NULL,
  trip_id              uuid          NOT NULL,
  kind                 text          NOT NULL CHECK (kind IN ('planned', 'revised', 'vehicle_assigned', 'departed', 'arrived',
                                                            'completed', 'cancelled')),
  status               text          NOT NULL CHECK (status IN ('planned', 'departed', 'arrived', 'completed', 'cancelled')),
  code                 text          NOT NULL CHECK (code ~ '^CH[0-9]{6,}$'),
  mode                 text          NOT NULL CHECK (mode IN ('road', 'waterway')),
  vehicle_ref          uuid          NULL,
  driver_ref           uuid          NULL,
  collection_order_ref uuid          NULL,
  delivery_ref         uuid          NULL,
  planned_kg           numeric(14,3) NULL CHECK (planned_kg IS NULL OR planned_kg >= 0),
  actual_kg            numeric(14,3) NULL CHECK (actual_kg IS NULL OR actual_kg > 0),
  actual_cost          numeric(18,2) NULL CHECK (actual_cost IS NULL OR actual_cost >= 0),
  cost_source          text          NULL CHECK (cost_source IS NULL OR cost_source IN ('param_rate', 'manual', 'vessel_rate')),
  cargo                jsonb         NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(cargo) = 'array'),
  lat                  numeric(10,7) NULL CHECK (lat IS NULL OR lat BETWEEN -90 AND 90),
  lng                  numeric(10,7) NULL CHECK (lng IS NULL OR lng BETWEEN -180 AND 180),
  completed_at         timestamptz   NULL,
  reason               text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  flags                text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(flags) <= 20),
  source_ref           uuid          NULL,                     -- roll_count_id (FM-05) / ticket_id (phiếu cân)
  param_version        text          NULL,                     -- tham số đã ghim khi tính (cước, tải)
  event_time           timestamptz   NOT NULL,
  recorded_at          timestamptz   NOT NULL DEFAULT now(),
  supersedes_id        uuid          NULL,
  correction_reason    text          NULL,
  actor_ref            uuid          NOT NULL,
  device_id            text          NULL,
  idem_key             text          NOT NULL,
  rule_version         text          NOT NULL,
  prev_hash            bytea         NOT NULL,
  row_hash             bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) BETWEEN 10 AND 500),
  CHECK (supersedes_id IS NULL OR kind = 'completed'),
  CHECK (kind <> 'planned' OR status = 'planned'),
  CHECK (kind <> 'departed' OR (status = 'departed' AND lat IS NOT NULL AND lng IS NOT NULL)),
  CHECK (kind <> 'arrived' OR (status = 'arrived' AND lat IS NOT NULL AND lng IS NOT NULL)),
  CHECK (kind <> 'cancelled' OR (status = 'cancelled' AND reason IS NOT NULL)),
  CHECK (kind <> 'vehicle_assigned' OR vehicle_ref IS NOT NULL),
  CHECK (kind <> 'completed' OR (status = 'completed' AND actual_kg IS NOT NULL AND actual_cost IS NOT NULL
                                 AND cost_source IS NOT NULL AND completed_at IS NOT NULL)),
  CHECK (cost_source IS DISTINCT FROM 'manual' OR reason IS NOT NULL)          -- cước nhập tay phải có lý do / chứng từ
) PARTITION BY RANGE (recorded_at);
CREATE INDEX trip_event_idem   ON mkg_logistics.trip_event (tenant_id, idem_key);
CREATE INDEX trip_event_stream ON mkg_logistics.trip_event (tenant_id, stream_key, seq);
CREATE INDEX trip_event_trip   ON mkg_logistics.trip_event (tenant_id, trip_id, seq);
CREATE INDEX trip_event_done   ON mkg_logistics.trip_event (tenant_id, completed_at, trip_id) WHERE kind = 'completed';
SELECT shared.apply_ledger('mkg_logistics.trip_event', 'mkg_logistics.trip_event');

-- ---------------------------------------------------------------------------
-- 2. Sổ chặng + ảnh chụp định tuyến
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.route_snapshot (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                     -- 'route:<trip_id>'
  seq               bigint      NOT NULL,
  trip_id           uuid        NOT NULL,
  leg_id            uuid        NOT NULL,
  provider          text        NOT NULL CHECK (provider IN ('pf_partner_waterway', 'road_haversine')),
  request           jsonb       NOT NULL CHECK (jsonb_typeof(request) = 'object'),
  raw               bytea       NOT NULL CHECK (length(raw) BETWEEN 2 AND 4194304),
  raw_sha256        bytea       NOT NULL CHECK (length(raw_sha256) = 32),
  grantor_tenant    uuid        NULL,
  network_version   integer     NULL CHECK (network_version IS NULL OR network_version >= 1),
  fetched_at        timestamptz NOT NULL,
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
  CHECK (raw_sha256 = sha256(raw))                                -- băm nguyên văn kiểm ở tầng DB
) PARTITION BY RANGE (recorded_at);
CREATE INDEX route_snapshot_idem   ON mkg_logistics.route_snapshot (tenant_id, idem_key);
CREATE INDEX route_snapshot_stream ON mkg_logistics.route_snapshot (tenant_id, stream_key, seq);
CREATE INDEX route_snapshot_id     ON mkg_logistics.route_snapshot (tenant_id, id);
SELECT shared.apply_ledger('mkg_logistics.route_snapshot', 'mkg_logistics.route_snapshot');

CREATE TABLE mkg_logistics.leg_event (
  id                 uuid          NOT NULL,
  tenant_id          uuid          NOT NULL,
  stream_key         text          NOT NULL,                  -- 'legs:<trip_id>'
  seq                bigint        NOT NULL,
  trip_id            uuid          NOT NULL,
  leg_id             uuid          NOT NULL,
  plan_revision      integer       NOT NULL CHECK (plan_revision >= 1),
  leg_seq            integer       NOT NULL CHECK (leg_seq BETWEEN 1 AND 24),
  mode               text          NOT NULL CHECK (mode IN ('road', 'waterway')),
  from_kind          text          NOT NULL CHECK (from_kind IN ('plot', 'collection_point', 'facility', 'customer_site', 'point')),
  from_ref           uuid          NULL,
  from_lat           numeric(10,7) NOT NULL CHECK (from_lat BETWEEN -90 AND 90),
  from_lng           numeric(10,7) NOT NULL CHECK (from_lng BETWEEN -180 AND 180),
  to_kind            text          NOT NULL CHECK (to_kind IN ('plot', 'collection_point', 'facility', 'customer_site', 'point')),
  to_ref             uuid          NULL,
  to_lat             numeric(10,7) NOT NULL CHECK (to_lat BETWEEN -90 AND 90),
  to_lng             numeric(10,7) NOT NULL CHECK (to_lng BETWEEN -180 AND 180),
  distance_km        numeric(10,3) NOT NULL CHECK (distance_km >= 0),
  route_source       text          NOT NULL CHECK (route_source IN ('routed', 'digitised', 'haversine', 'manual')),
  route_snapshot_id  uuid          NULL,
  network_version    integer       NULL CHECK (network_version IS NULL OR network_version >= 1),
  manual_reason      text          NULL CHECK (manual_reason IS NULL OR length(manual_reason) BETWEEN 10 AND 500),
  mass_kg            numeric(14,3) NULL CHECK (mass_kg IS NULL OR mass_kg >= 0),
  rate_vnd_per_t_km  numeric(18,4) NOT NULL CHECK (rate_vnd_per_t_km >= 0),
  planned_cost       numeric(18,2) NOT NULL CHECK (planned_cost >= 0),
  param_version      text          NOT NULL,
  flags              text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(flags) <= 20),
  event_time         timestamptz   NOT NULL,
  recorded_at        timestamptz   NOT NULL DEFAULT now(),
  supersedes_id      uuid          NULL,
  correction_reason  text          NULL,
  actor_ref          uuid          NOT NULL,
  device_id          text          NULL,
  idem_key           text          NOT NULL,
  rule_version       text          NOT NULL,
  prev_hash          bytea         NOT NULL,
  row_hash           bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (route_source <> 'manual' OR manual_reason IS NOT NULL),           -- km tay phải có lý do
  CHECK (route_source = 'manual' OR route_snapshot_id IS NOT NULL)         -- km tính phải có ảnh chụp nguyên văn
) PARTITION BY RANGE (recorded_at);
CREATE INDEX leg_event_idem   ON mkg_logistics.leg_event (tenant_id, idem_key);
CREATE INDEX leg_event_stream ON mkg_logistics.leg_event (tenant_id, stream_key, seq);
CREATE INDEX leg_event_trip   ON mkg_logistics.leg_event (tenant_id, trip_id, plan_revision, leg_seq);
SELECT shared.apply_ledger('mkg_logistics.leg_event', 'mkg_logistics.leg_event');

-- ---------------------------------------------------------------------------
-- 3. Danh mục phương tiện (proto GH-01..03)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.vessel_event (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,                 -- 'vessel:<vessel_id>'
  seq                 bigint        NOT NULL,
  vessel_id           uuid          NOT NULL,
  kind                text          NOT NULL CHECK (kind IN ('created', 'revised')),
  version             integer       NOT NULL CHECK (version >= 1),
  code                text          NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9.\-]{1,31}$'),
  vehicle_kind        text          NOT NULL CHECK (vehicle_kind IN ('ghe', 'sa_lan', 'truck')),
  name                text          NULL CHECK (name IS NULL OR length(name) BETWEEN 1 AND 200),
  owner_ref           uuid          NULL,                     -- chủ ghe ở vault tenant MKG (RB-3)
  registered_tons     numeric(10,3) NULL CHECK (registered_tons IS NULL OR registered_tons > 0),
  straw_payload_tons  numeric(10,3) NULL CHECK (straw_payload_tons IS NULL OR straw_payload_tons > 0),
  registration_expiry date          NULL,
  rate_type           text          NULL CHECK (rate_type IS NULL OR rate_type IN ('per_ton', 'per_trip', 'per_ton_km')),
  rate_vnd            numeric(18,4) NULL CHECK (rate_vnd IS NULL OR rate_vnd > 0),
  status              text          NOT NULL CHECK (status IN ('active', 'inactive')),
  reason              text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  supersedes_id       uuid          NULL,
  correction_reason   text          NULL,
  actor_ref           uuid          NOT NULL,
  device_id           text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((rate_type IS NULL) = (rate_vnd IS NULL)),                          -- GH-01: có loại đơn giá thì đơn giá > 0
  CHECK (kind <> 'created' OR (version = 1 AND status = 'active')),
  CHECK (kind <> 'revised' OR (version > 1 AND reason IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX vessel_event_idem   ON mkg_logistics.vessel_event (tenant_id, idem_key);
CREATE INDEX vessel_event_stream ON mkg_logistics.vessel_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_logistics.vessel_event', 'mkg_logistics.vessel_event');

CREATE TABLE mkg_logistics.vessel_current (
  tenant_id           uuid          NOT NULL,
  vessel_id           uuid          NOT NULL,
  version             integer       NOT NULL,
  code                text          NOT NULL,
  vehicle_kind        text          NOT NULL,
  name                text          NULL,
  owner_ref           uuid          NULL,
  registered_tons     numeric(10,3) NULL,
  straw_payload_tons  numeric(10,3) NULL,
  registration_expiry date          NULL,
  rate_type           text          NULL,
  rate_vnd            numeric(18,4) NULL,
  status              text          NOT NULL CHECK (status IN ('active', 'inactive')),
  last_event_id       uuid          NOT NULL,
  created_at          timestamptz   NOT NULL,
  updated_at          timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, vessel_id),
  UNIQUE (tenant_id, code)                                                   -- GH-01: số hiệu duy nhất
);
CREATE INDEX vessel_current_expiry ON mkg_logistics.vessel_current (tenant_id, registration_expiry) WHERE status = 'active';
SELECT shared.apply_tenant_rls('mkg_logistics.vessel_current');

-- ---------------------------------------------------------------------------
-- 4. Tham số có phiên bản (tạm, chờ khách — g4-conventions §1.2)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.param_version (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                   -- 'param:<param_key>'
  seq               bigint        NOT NULL,
  param_key         text          NOT NULL CHECK (param_key IN (
                      'logistics.rate.road_vnd_per_t_km', 'logistics.rate.waterway_vnd_per_t_km',
                      'logistics.mode.road_feasible_km', 'logistics.vessel.straw_payload_tons',
                      'logistics.vessel.payload_warn_factor', 'logistics.vessel.expiry_warn_days',
                      'logistics.trip.unweighed_alert_hours', 'logistics.route.road_haversine_factor')),
  version_id        uuid          NOT NULL,
  version_seq       integer       NOT NULL CHECK (version_seq >= 1),
  kind              text          NOT NULL CHECK (kind IN ('proposed', 'approved', 'rejected')),
  value             numeric(18,4) NOT NULL CHECK (value > 0),
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
  CHECK (kind = 'proposed' OR actor_ref <> proposer_ref),                   -- R-56 ở tầng DB
  CHECK (kind <> 'rejected' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_version_idem   ON mkg_logistics.param_version (tenant_id, idem_key);
CREATE INDEX param_version_stream ON mkg_logistics.param_version (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_logistics.param_version', 'mkg_logistics.param_version');

CREATE TABLE mkg_logistics.param_current (
  tenant_id       uuid          NOT NULL,
  param_key       text          NOT NULL,
  version_id      uuid          NOT NULL,
  version_seq     integer       NOT NULL,
  value           numeric(18,4) NOT NULL,
  effective_from  date          NOT NULL,
  provisional     boolean       NOT NULL,
  status          text          NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  proposed_by     uuid          NOT NULL,
  proposal_reason text          NOT NULL,
  decided_by      uuid          NULL,
  decided_at      timestamptz   NULL,
  decision_reason text          NULL,
  updated_at      timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, param_key, version_id),
  UNIQUE (tenant_id, param_key, version_seq),
  CHECK (decided_by IS NULL OR decided_by <> proposed_by)                    -- R-56
);
SELECT shared.apply_tenant_rls('mkg_logistics.param_current');

-- ---------------------------------------------------------------------------
-- 5. Bảng chiếu chuyến — trạng thái chỉ tiến (L-02)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.trip_current (
  tenant_id            uuid          NOT NULL,
  trip_id              uuid          NOT NULL,
  code                 text          NOT NULL,
  status               text          NOT NULL CHECK (status IN ('planned', 'departed', 'arrived', 'completed', 'cancelled')),
  mode                 text          NOT NULL CHECK (mode IN ('road', 'waterway')),
  vehicle_ref          uuid          NULL,
  driver_ref           uuid          NULL,
  collection_order_ref uuid          NULL,
  delivery_ref         uuid          NULL,
  cargo                jsonb         NOT NULL DEFAULT '[]',
  planned_kg           numeric(14,3) NULL,
  actual_kg            numeric(14,3) NULL,
  planned_cost         numeric(18,2) NULL,
  actual_cost          numeric(18,2) NULL,
  cost_source          text          NULL,
  plan_revision        integer       NOT NULL DEFAULT 0,
  planned_at           timestamptz   NOT NULL,
  departed_at          timestamptz   NULL,
  arrived_at           timestamptz   NULL,
  completed_at         timestamptz   NULL,
  cancelled_at         timestamptz   NULL,
  cancel_reason        text          NULL,
  completion_event_id  uuid          NULL,
  ticket_ref           uuid          NULL,
  source_ref           uuid          NULL,
  flags                text[]        NOT NULL DEFAULT '{}',
  rule_version         text          NOT NULL,
  last_seq             bigint        NOT NULL,
  recorded_at          timestamptz   NOT NULL,                             -- recorded_at của bản ghi sổ mới nhất đã tính
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, trip_id),
  UNIQUE (tenant_id, code),
  CHECK (status <> 'completed' OR (actual_kg IS NOT NULL AND actual_cost IS NOT NULL AND completed_at IS NOT NULL)),
  CHECK (status <> 'cancelled' OR cancel_reason IS NOT NULL)
);
CREATE INDEX trip_current_status ON mkg_logistics.trip_current (tenant_id, status, planned_at);
CREATE INDEX trip_current_order  ON mkg_logistics.trip_current (tenant_id, collection_order_ref) WHERE collection_order_ref IS NOT NULL;
CREATE INDEX trip_current_source ON mkg_logistics.trip_current (tenant_id, source_ref) WHERE source_ref IS NOT NULL;
CREATE INDEX trip_current_cargo  ON mkg_logistics.trip_current USING gin (cargo jsonb_path_ops);
SELECT shared.apply_tenant_rls('mkg_logistics.trip_current');

-- Thứ hạng trạng thái: planned 1 → departed 2 → arrived 3 → completed 4; cancelled là nhánh cuối từ
-- planned/departed. Cho phép: giữ nguyên (sửa trường khác, bản bù completed), tiến (kể cả nhảy — phiếu
-- cân đóng chuyến chưa ghi mốc, proto plant weighing), hủy từ planned/departed. Cấm mọi chiều lùi và mọi
-- thay đổi sau cancelled.
CREATE FUNCTION mkg_logistics.fn_trip_forward() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  r_old int := array_position(ARRAY['planned', 'departed', 'arrived', 'completed'], OLD.status);
  r_new int := array_position(ARRAY['planned', 'departed', 'arrived', 'completed'], NEW.status);
BEGIN
  IF NEW.status = OLD.status THEN
    IF OLD.status = 'cancelled' THEN
      RAISE EXCEPTION 'mkg_logistics.trip_current: chuyến % đã hủy — không đổi được', OLD.trip_id
        USING ERRCODE = 'check_violation', CONSTRAINT = 'trip_forward_only';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.status = 'cancelled' AND OLD.status IN ('planned', 'departed') THEN
    RETURN NEW;
  END IF;
  IF OLD.status <> 'cancelled' AND NEW.status <> 'cancelled' AND r_new > r_old THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'mkg_logistics.trip_current: chuyển trạng thái % → % bị cấm (chỉ tiến, L-02)', OLD.status, NEW.status
    USING ERRCODE = 'check_violation', CONSTRAINT = 'trip_forward_only';
END $$;
CREATE TRIGGER trg_trip_forward BEFORE UPDATE ON mkg_logistics.trip_current
  FOR EACH ROW EXECUTE FUNCTION mkg_logistics.fn_trip_forward();

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_logistics.trip_event, mkg_logistics.leg_event, mkg_logistics.route_snapshot,
                        mkg_logistics.vessel_event, mkg_logistics.param_version TO logistics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_logistics.trip_event, mkg_logistics.leg_event, mkg_logistics.route_snapshot,
                                   mkg_logistics.vessel_event, mkg_logistics.param_version FROM logistics_app;
-- Bảng chiếu: được UPDATE (R-21); DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_logistics.trip_current, mkg_logistics.vessel_current,
                                        mkg_logistics.param_current TO logistics_app;
