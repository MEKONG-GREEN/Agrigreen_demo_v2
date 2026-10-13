-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.49, D4 CN9, BRD CNTS v3.7 FN-11, UR-XA-10 (BM-10):
-- Ổ DỊCH có phiên bản diễn biến + BÁO NGHI DỊCH.
--
-- 1. cnts_outbreak — sổ chỉ ghi thêm (shared.apply_ledger): mỗi dòng là MỘT phiên bản của ổ dịch (chuỗi
--    'outbreak:<outbreak_ref>', version = seq). Mỗi phiên bản chụp ĐỦ các trường DB-01…DB-13 tại lần ghi (FN-11 BR-02 "mỗi
--    lần cập nhật lưu phiên bản") — bản mới nhất là hiện hành, bản cũ là diễn biến. Sự việc: reported (Cấp Xã gửi Cấp
--    Tỉnh), updated (cập nhật diễn biến theo ngày as_of), closed (Cấp Tỉnh xác nhận kết thúc), reopened (Cấp Tỉnh mở lại).
--    Trạng thái hiển thị (Đã gửi / Đang theo dõi / Đã kết thúc) suy từ sự việc của bản hiện hành.
--    Bệnh, loài là mã danh mục CNTS (pf_masterdata cnts_disease, cnts_species — W11.41, kiểm theo ngày ở ứng dụng); xã là
--    mã ĐVHC cấp xã. Vị trí DB-05 bắt buộc (AC-02) — geometry Point 4326 cho lớp bản đồ (W11.52 vẽ vùng ở gói sau).
--    Hộ chăn nuôi DB-04: household_ref (pf/cooperative) và/hoặc địa bàn (ấp, khu vực) — KHÔNG lưu họ tên tự do (PII, bản
--    đối chiếu §2.4); văn bản quét số điện thoại/CCCD ở ứng dụng. Tệp đính kèm DB-13 là mã tệp pf_files.
-- 2. cnts_suspect — sổ chỉ ghi thêm: BÁO NGHI DỊCH (chuỗi 'suspect:<suspect_ref>'). Sự việc: reported (Cấp Xã ghi nhận
--    tin báo của nông hộ/UBND/cán bộ; Nông hộ Mobile là GĐ2 W11.55), confirmed (cán bộ xã xác nhận → tạo ổ dịch,
--    outbreak_ref), dismissed (bác bỏ, có lý do). Điểm báo nghi dịch chỉ lên bản đồ sau khi xác nhận (FN-18) = ổ dịch.
-- Khối lượng, thể tích là numeric (R-CODE-01). Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.cnts_outbreak (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'outbreak:<outbreak_ref>'
  seq                 bigint        NOT NULL,             -- = số phiên bản
  outbreak_ref        uuid          NOT NULL,             -- định danh bền của ổ dịch
  event               text          NOT NULL CHECK (event IN ('reported', 'updated', 'closed', 'reopened')),
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  disease_code        text          NOT NULL CHECK (char_length(disease_code) BETWEEN 1 AND 64),   -- DB-01
  species_code        text          NOT NULL CHECK (char_length(species_code) BETWEEN 1 AND 64),
  received_at         timestamptz   NOT NULL,             -- DB-02 thời điểm tiếp nhận thông tin
  source              text          NOT NULL CHECK (source IN ('ubnd_xa', 'nong_ho', 'can_bo')), -- DB-03
  suspect_ref         uuid          NULL,                 -- tạo từ báo nghi dịch
  household_ref       uuid          NULL,                 -- DB-04 (pf/cooperative)
  locality            text          NULL CHECK (locality IS NULL OR char_length(locality) BETWEEN 1 AND 300),
  location            geometry(Point, 4326) NOT NULL,     -- DB-05
  location_source     text          NOT NULL CHECK (location_source IN ('gps', 'map')),
  gps_accuracy_m      numeric(10,2) NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  herd_size           integer       NOT NULL CHECK (herd_size BETWEEN 1 AND 100000000),          -- DB-06
  herd_age_days       integer       NULL CHECK (herd_age_days IS NULL OR herd_age_days BETWEEN 0 AND 20000),
  dead                integer       NOT NULL CHECK (dead >= 0),                                   -- DB-07 (lũy kế)
  symptoms            text          NOT NULL CHECK (char_length(symptoms) BETWEEN 1 AND 2000),   -- DB-08
  sample_date         date          NULL,                 -- DB-09
  lab_name            text          NULL CHECK (lab_name IS NULL OR char_length(lab_name) <= 300),
  lab_result          text          NULL CHECK (lab_result IS NULL OR char_length(lab_result) <= 2000),
  culled_head         integer       NULL CHECK (culled_head IS NULL OR culled_head >= 0),       -- DB-10 (lũy kế)
  culled_kg           numeric(14,3) NULL CHECK (culled_kg IS NULL OR culled_kg >= 0),
  chemical_liters     numeric(14,3) NULL CHECK (chemical_liters IS NULL OR chemical_liters >= 0),
  buffer_note         text          NULL CHECK (buffer_note IS NULL OR char_length(buffer_note) <= 2000),         -- DB-11
  prevention_note     text          NULL CHECK (prevention_note IS NULL OR char_length(prevention_note) <= 2000), -- DB-12
  attachment_refs     uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(attachment_refs) <= 10)             -- DB-13
                        CHECK (array_position(attachment_refs, NULL) IS NULL),
  as_of               date          NOT NULL,             -- ngày của diễn biến (ngày phát hiện ở bản 1)
  progress_note       text          NULL CHECK (progress_note IS NULL OR char_length(progress_note) <= 2000),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  captured_at         timestamptz   NULL,                 -- giờ thiết bị của bản ngoại tuyến (Mobile)
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
  CONSTRAINT cnts_outbreak_stream CHECK (stream_key = 'outbreak:' || outbreak_ref::text),
  -- Quy tắc chặn BM-10 (UR-XA-10, Mục 4 quy tắc liên biểu số 8): số con chết, tiêu hủy ≤ quy mô đàn.
  CONSTRAINT cnts_outbreak_dead CHECK (dead <= herd_size),
  CONSTRAINT cnts_outbreak_culled CHECK (culled_head IS NULL OR culled_head <= herd_size),
  CONSTRAINT cnts_outbreak_household CHECK (household_ref IS NOT NULL OR locality IS NOT NULL),
  CONSTRAINT cnts_outbreak_reason CHECK (event NOT IN ('closed', 'reopened') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_outbreak_ref ON pf_extension.cnts_outbreak (tenant_id, outbreak_ref, seq);
CREATE INDEX cnts_outbreak_commune ON pf_extension.cnts_outbreak (tenant_id, commune_code, as_of);
CREATE INDEX cnts_outbreak_suspect ON pf_extension.cnts_outbreak (tenant_id, suspect_ref) WHERE suspect_ref IS NOT NULL;
SELECT shared.apply_ledger('pf_extension.cnts_outbreak', 'pf_extension.cnts_outbreak');

CREATE TABLE pf_extension.cnts_suspect (
  id                  uuid          NOT NULL,             -- UUIDv7 — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'suspect:<suspect_ref>'
  seq                 bigint        NOT NULL,
  suspect_ref         uuid          NOT NULL,
  event               text          NOT NULL CHECK (event IN ('reported', 'confirmed', 'dismissed')),
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  reporter_kind       text          NOT NULL CHECK (reporter_kind IN ('nong_ho', 'ubnd_xa', 'can_bo')),
  observed_at         timestamptz   NOT NULL,             -- lúc phát hiện / nhận tin
  species_code        text          NULL CHECK (species_code IS NULL OR char_length(species_code) BETWEEN 1 AND 64),
  disease_code        text          NULL CHECK (disease_code IS NULL OR char_length(disease_code) BETWEEN 1 AND 64),
  household_ref       uuid          NULL,
  locality            text          NULL CHECK (locality IS NULL OR char_length(locality) BETWEEN 1 AND 300),
  location            geometry(Point, 4326) NULL,         -- tin báo qua điện thoại có thể chưa có vị trí
  gps_accuracy_m      numeric(10,2) NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  affected            integer       NULL CHECK (affected IS NULL OR affected >= 0),
  dead                integer       NULL CHECK (dead IS NULL OR dead >= 0),
  description         text          NOT NULL CHECK (char_length(description) BETWEEN 1 AND 2000),
  attachment_refs     uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(attachment_refs) <= 10)
                        CHECK (array_position(attachment_refs, NULL) IS NULL),
  outbreak_ref        uuid          NULL,                 -- ổ dịch tạo khi xác nhận
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  captured_at         timestamptz   NULL,                 -- giờ thiết bị của bản ngoại tuyến (Mobile)
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
  CONSTRAINT cnts_suspect_stream CHECK (stream_key = 'suspect:' || suspect_ref::text),
  CONSTRAINT cnts_suspect_household CHECK (household_ref IS NOT NULL OR locality IS NOT NULL),
  CONSTRAINT cnts_suspect_confirmed CHECK (event <> 'confirmed' OR outbreak_ref IS NOT NULL),
  CONSTRAINT cnts_suspect_dismissed CHECK (event <> 'dismissed' OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_suspect_ref ON pf_extension.cnts_suspect (tenant_id, suspect_ref, seq);
CREATE INDEX cnts_suspect_commune ON pf_extension.cnts_suspect (tenant_id, commune_code, observed_at);
SELECT shared.apply_ledger('pf_extension.cnts_suspect', 'pf_extension.cnts_suspect');

REVOKE ALL ON pf_extension.cnts_outbreak, pf_extension.cnts_suspect FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_outbreak, pf_extension.cnts_suspect TO r_extension;
