-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.46, D4 CN6, BRD CNTS v3.7 FN-12, FN-15:
-- LỊCH TRỰC KSGM (BM-03), LỊCH TIÊM PHÒNG (BM-07), LỊCH CÔNG TÁC + CHECK-IN (BM-05/06).
--
-- 1. cnts_duty_roster — sổ chỉ ghi thêm (shared.apply_ledger): mỗi dòng là MỘT phiên bản lịch trực của một tháng
--    (chuỗi 'duty:<YYYY-MM>', version = seq). Ô lưới (cơ sở × viên chức × ngày → ca N/Đ/NĐ, UR-TINH-10) chụp ĐỦ trong
--    cells (mảng {f: facility_ref, o: officer_ref, d: ngày 1..31, s: n|d|nd}); bản mới nhất là lịch hiện hành, bản
--    cũ là lịch sử đổi ca (thông báo viên chức liên quan). Ca đêm qua nửa đêm thuộc ngày bắt đầu ca (BR-03).
-- 2. cnts_vaccination_batch — sổ chỉ ghi thêm: lịch tiêm của MỘT lứa nuôi (hộ mô hình × ngày nhận giống, chuỗi
--    'vbatch:<batch_ref>'). Mỗi phiên bản chụp đủ danh sách mũi (doses: {vaccine, shot, age_days, expected, actual,
--    note}) sinh từ quy trình tiêm (pf_masterdata cnts_vaccination_schedule, giá trị có hiệu lực tại ngày nhận giống —
--    "đổi quy trình chỉ áp dụng lứa mới"). Ngày dự kiến = ngày nhận + ngày tuổi − tuổi lúc nhận, sửa được từng hộ;
--    ngày thực tế do Cấp Xã ghi. Hộ: household_ref và/hoặc địa bàn — không lưu họ tên tự do (§2.4).
-- 3. work_schedule_item_place — sổ chỉ ghi thêm cạnh work_schedule_item (không thêm cột vào sổ cũ): ĐỊA ĐIỂM CÔNG
--    TÁC đã đăng ký (CV-06, điểm trên bản đồ) của một dòng mục. Là địa điểm của công việc, không phải vị trí người.
-- 4. work_schedule_checkin — sổ chỉ ghi thêm: SỰ VIỆC check-in (CV-07, FN-12 BR-02 "không sửa được") — thời điểm,
--    sai số, KHOẢNG CÁCH tới địa điểm đã đăng ký, cờ xa/GPS yếu/giả lập. KHÔNG có tọa độ (QĐ-8: không lưu vị trí
--    người trong sổ không xóa được). Mỗi mục công tác (item_id) tối đa một check-in (chuỗi 'wcheckin:<item_id>').
-- 5. work_schedule_checkin_point — BẢNG THƯỜNG (không phải sổ): tọa độ GPS lúc check-in, có hạn lưu purge_after
--    (tham số extension.work_schedule_checkin_point_retention_days, mặc định 90 ngày); job extension_work_schedule
--    xóa dòng quá hạn; không UPDATE được (trigger). Cờ extension.work_schedule_checkin_store_point = off → không ghi
--    bảng này (chỉ còn khoảng cách). Đọc: người đăng ký và người duyệt lịch.
-- Vai r_extension: sổ SELECT, INSERT; bảng tọa độ SELECT, INSERT, DELETE.

CREATE TABLE pf_extension.cnts_duty_roster (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'duty:<YYYY-MM>'
  seq                 bigint        NOT NULL,             -- = số phiên bản
  month               date          NOT NULL CHECK (extract(day FROM month) = 1),
  source              text          NOT NULL CHECK (source IN ('grid', 'copy', 'excel')),
  cells               jsonb         NOT NULL CHECK (jsonb_typeof(cells) = 'array'),
  cell_count          integer       NOT NULL CHECK (cell_count BETWEEN 0 AND 20000),
  note                text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,                 -- chế độ hỗ trợ của Admin
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_duty_roster_stream CHECK (stream_key = 'duty:' || to_char(month, 'YYYY-MM')),
  CONSTRAINT cnts_duty_roster_count CHECK (cell_count = jsonb_array_length(cells))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_duty_roster_month ON pf_extension.cnts_duty_roster (tenant_id, month, seq);
SELECT shared.apply_ledger('pf_extension.cnts_duty_roster', 'pf_extension.cnts_duty_roster');

CREATE TABLE pf_extension.cnts_vaccination_batch (
  id                  uuid          NOT NULL,             -- UUIDv7 — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'vbatch:<batch_ref>'
  seq                 bigint        NOT NULL,
  batch_ref           uuid          NOT NULL,             -- định danh bền của lứa
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'dose_recorded', 'cancelled')),
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  model_type_code     text          NOT NULL CHECK (char_length(model_type_code) BETWEEN 1 AND 64),
  breed_code          text          NULL CHECK (breed_code IS NULL OR char_length(breed_code) BETWEEN 1 AND 64),
  schedule_code       text          NOT NULL CHECK (char_length(schedule_code) BETWEEN 1 AND 64),
  schedule_valid_from date          NOT NULL,             -- phiên bản quy trình đã dùng
  household_ref       uuid          NULL,                 -- hộ mô hình (pf/cooperative)
  locality            text          NULL CHECK (locality IS NULL OR char_length(locality) BETWEEN 1 AND 300),
  received_date       date          NOT NULL,             -- ngày nhận giống
  age_at_receipt      integer       NOT NULL CHECK (age_at_receipt BETWEEN 0 AND 3650),
  head_count          integer       NULL CHECK (head_count IS NULL OR head_count BETWEEN 1 AND 100000000),
  doses               jsonb         NOT NULL CHECK (jsonb_typeof(doses) = 'array' AND jsonb_array_length(doses) BETWEEN 1 AND 60),
  note                text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  captured_at         timestamptz   NULL,                 -- giờ thiết bị (Mobile, ngoại tuyến)
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_vaccination_batch_stream CHECK (stream_key = 'vbatch:' || batch_ref::text),
  CONSTRAINT cnts_vaccination_batch_household CHECK (household_ref IS NOT NULL OR locality IS NOT NULL),
  CONSTRAINT cnts_vaccination_batch_reason CHECK (event <> 'cancelled' OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_vaccination_batch_ref ON pf_extension.cnts_vaccination_batch (tenant_id, batch_ref, seq);
CREATE INDEX cnts_vaccination_batch_commune ON pf_extension.cnts_vaccination_batch (tenant_id, commune_code, received_date);
SELECT shared.apply_ledger('pf_extension.cnts_vaccination_batch', 'pf_extension.cnts_vaccination_batch');

CREATE TABLE pf_extension.work_schedule_item_place (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'wsched:<schedule_id>' (cùng chuỗi với work_schedule_item)
  seq                 bigint        NOT NULL,
  schedule_id         uuid          NOT NULL,
  version             integer       NOT NULL CHECK (version >= 1),
  item_row_id         uuid          NOT NULL,             -- dòng work_schedule_item của phiên bản
  item_id             uuid          NOT NULL,
  point               geometry(Point, 4326) NOT NULL,
  place_source        text          NOT NULL CHECK (place_source IN ('map', 'gps')),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT work_schedule_item_place_stream CHECK (stream_key = 'wsched:' || schedule_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX work_schedule_item_place_row ON pf_extension.work_schedule_item_place (tenant_id, item_row_id);
SELECT shared.apply_ledger('pf_extension.work_schedule_item_place', 'pf_extension.work_schedule_item_place');

CREATE TABLE pf_extension.work_schedule_checkin (
  id                  uuid          NOT NULL,             -- UUIDv7 — mã check-in
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'wcheckin:<item_id>'
  seq                 bigint        NOT NULL,
  schedule_id         uuid          NOT NULL,
  item_id             uuid          NOT NULL,
  item_version        integer       NOT NULL CHECK (item_version >= 1),
  owner_ref           uuid          NOT NULL,
  department_code     text          NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  day                 date          NOT NULL,             -- ngày công tác của mục
  captured_at         timestamptz   NOT NULL,             -- giờ thiết bị lúc bấm "Check-in"
  accuracy_m          numeric(8,2)  NULL CHECK (accuracy_m IS NULL OR accuracy_m >= 0),
  distance_m          numeric(12,1) NULL CHECK (distance_m IS NULL OR distance_m >= 0),   -- tới địa điểm đã đăng ký
  far                 boolean       NOT NULL,             -- distance_m > ngưỡng (mặc định 500 m)
  weak_accuracy       boolean       NOT NULL,             -- sai số > ngưỡng (cnts.gps_accuracy_m, 50 m)
  mock_location       boolean       NOT NULL,             -- thiết bị báo vị trí giả lập
  point_stored        boolean       NOT NULL,             -- có dòng work_schedule_checkin_point (cờ tính năng)
  device_id           text          NULL CHECK (device_id IS NULL OR char_length(device_id) <= 200),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT work_schedule_checkin_stream CHECK (stream_key = 'wcheckin:' || item_id::text),
  CONSTRAINT work_schedule_checkin_far CHECK (NOT far OR distance_m IS NOT NULL),
  CONSTRAINT work_schedule_checkin_owner CHECK (actor_ref = owner_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX work_schedule_checkin_schedule ON pf_extension.work_schedule_checkin (tenant_id, schedule_id);
CREATE INDEX work_schedule_checkin_item ON pf_extension.work_schedule_checkin (tenant_id, item_id);
SELECT shared.apply_ledger('pf_extension.work_schedule_checkin', 'pf_extension.work_schedule_checkin');

CREATE TABLE pf_extension.work_schedule_checkin_point (
  tenant_id           uuid          NOT NULL,
  checkin_id          uuid          NOT NULL,             -- = work_schedule_checkin.id
  point               geometry(Point, 4326) NOT NULL,
  captured_at         timestamptz   NOT NULL,
  purge_after         date          NOT NULL,             -- hạn lưu: job xóa khi purge_after < hôm nay
  created_at          timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, checkin_id)
);
CREATE INDEX work_schedule_checkin_point_purge ON pf_extension.work_schedule_checkin_point (purge_after);
SELECT shared.apply_tenant_rls('pf_extension.work_schedule_checkin_point');
-- Không sửa được (FN-12 BR-02, UR-TECH-02); XÓA được (hạn lưu, yêu cầu xóa dữ liệu cá nhân) — không phải sổ.
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_extension.work_schedule_checkin_point
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();

-- Dọn tọa độ quá hạn lưu (job extension_work_schedule, theo lô, tenant hiện hành qua RLS — SECURITY INVOKER).
CREATE FUNCTION pf_extension.purge_checkin_points(p_today date, p_limit integer DEFAULT 1000) RETURNS bigint
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  n bigint;
BEGIN
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 100000 THEN
    RAISE EXCEPTION 'purge_checkin_points: p_limit phải trong 1..100000' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  WITH gone AS (
    DELETE FROM pf_extension.work_schedule_checkin_point p
    WHERE (p.tenant_id, p.checkin_id) IN (
      SELECT x.tenant_id, x.checkin_id FROM pf_extension.work_schedule_checkin_point x
      WHERE x.purge_after < p_today ORDER BY x.purge_after LIMIT p_limit)
    RETURNING 1)
  SELECT count(*) INTO n FROM gone;
  RETURN n;
END $$;
REVOKE ALL ON FUNCTION pf_extension.purge_checkin_points(date, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.purge_checkin_points(date, integer) TO r_extension;

REVOKE ALL ON pf_extension.cnts_duty_roster, pf_extension.cnts_vaccination_batch, pf_extension.work_schedule_item_place,
  pf_extension.work_schedule_checkin, pf_extension.work_schedule_checkin_point FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_duty_roster, pf_extension.cnts_vaccination_batch,
  pf_extension.work_schedule_item_place, pf_extension.work_schedule_checkin TO r_extension;
GRANT SELECT, INSERT, DELETE ON pf_extension.work_schedule_checkin_point TO r_extension;
