-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.48, D4 CN8, BRD CNTS v3.7 FN-10 (nhật ký), FN-13
-- (rà soát, xác thực), UR-XA-11, UR-XA-12, UR-TINH-12: NHẬT KÝ NUÔI BM-08 (gà lai thương phẩm), BM-09 (vịt xiêm), BM-19
-- (thủy sản — mẫu sổ chờ phòng gửi, Q-077: trường theo BRD NT-01…NT-15, giả định tạm).
--
-- ADR-0055, Q-KNS-04: nhật ký gắn hộ mô hình CỦA KN (pf_extension.cnts_model_household, W11.47) — không ghi dữ liệu HTX.
-- QĐ-8: tọa độ GPS lúc ghi KHÔNG nằm trong sổ không xóa được; sổ chỉ giữ sai số, KHOẢNG CÁCH tới điểm của hộ/điểm
-- trình diễn và các cờ (xa, GPS yếu, giả lập). Ảnh ở pf/evidence (mã hóa phong bì bằng khóa của chủ hộ ở kho định danh);
-- ở đây chỉ có mã tệp.
--
-- 1. cnts_diary_cycle — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'dcycle:<cycle_ref>': LỨA NUÔI của một hộ (NC-02:
--    ngày nhận giống, giống/đối tượng nuôi, số con đầu kỳ, trọng lượng nhận, ngày tuổi; NC-03 vật tư nhận). Mỗi dòng là
--    một phiên bản đủ trường. Sự việc: created, updated, closed (cuối lứa — kèm HẠCH TOÁN NC-15: tổng chi theo khoản, tổng
--    thu), reopened (lý do bắt buộc). Lợi nhuận = thu − chi tính khi đọc.
-- 2. cnts_diary_entry — sổ chỉ ghi thêm, chuỗi 'dentry:<entry_ref>': LẦN GHI (một lần cân, đo, ghi của một hộ tại một
--    thời điểm). Mỗi dòng là một phiên bản đủ trường: chỉ số (metrics), bảng phụ (mua thức ăn, điều trị, tiêm phòng, xuất
--    bán, ý kiến cán bộ), ảnh, kết quả kiểm tra tự động (checks), vị trí (sai số, khoảng cách, cờ — KHÔNG tọa độ).
--    Vòng đời (Mục III.5): draft → submitted → verified; submitted → returned (lý do) → submitted; verified không sửa —
--    điều chỉnh là sự việc 'corrected' (lý do) đưa về submitted để xác thực lại. Người xác thực khác người ghi.
-- 3. cnts_diary_fix — BẢNG THƯỜNG: tọa độ GPS lúc ghi của lần ghi (một dòng mỗi lần ghi), không UPDATE được (trigger —
--    NT-15 "không sửa được"), XÓA được (hạn lưu purge_after, tham số cnts.diary_fix_retention_days; yêu cầu xóa dữ liệu
--    cá nhân). Job extension_work_schedule dọn theo lô (pf_extension.purge_diary_fixes).
-- 4. cnts_diary_photo — BẢNG THƯỜNG chỉ thêm: ảnh đã tải (mã tệp pf/evidence, hộ, loại ảnh, thời điểm chụp). Lần ghi chỉ
--    nhận ảnh có ở đây và cùng hộ.
-- Vai r_extension: sổ SELECT, INSERT; cnts_diary_fix SELECT, INSERT, DELETE; cnts_diary_photo SELECT, INSERT.

CREATE TABLE pf_extension.cnts_diary_cycle (
  id                    uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id             uuid          NOT NULL,
  stream_key            text          NOT NULL,             -- 'dcycle:<cycle_ref>'
  seq                   bigint        NOT NULL,
  cycle_ref             uuid          NOT NULL,             -- định danh bền của lứa
  event                 text          NOT NULL CHECK (event IN ('created', 'updated', 'closed', 'reopened')),
  household_ref         uuid          NOT NULL,             -- hộ mô hình của KN (W11.47)
  site_ref              uuid          NOT NULL,             -- chép từ hộ để lọc
  project_ref           uuid          NOT NULL,
  commune_code          text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  form                  text          NOT NULL CHECK (form IN ('BM-08', 'BM-09', 'BM-19')),
  breed                 text          NOT NULL CHECK (char_length(breed) BETWEEN 1 AND 200),       -- giống / đối tượng nuôi
  received_date         date          NOT NULL,             -- ngày nhận giống / thả giống
  initial_count         integer       NOT NULL CHECK (initial_count BETWEEN 1 AND 100000000),      -- số con đầu kỳ
  initial_weight_g      numeric(12,2) NULL CHECK (initial_weight_g IS NULL OR initial_weight_g > 0),
  age_days              integer       NULL CHECK (age_days IS NULL OR age_days BETWEEN 0 AND 3650), -- ngày tuổi lúc nhận
  supplies              jsonb         NOT NULL DEFAULT '[]'::jsonb                                  -- NC-03 vật tư nhận
                          CHECK (jsonb_typeof(supplies) = 'array' AND jsonb_array_length(supplies) <= 50),
  vaccination_batch_ref uuid          NULL,                 -- lứa của lịch tiêm BM-07 (W11.46), nếu có
  note                  text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  -- NC-15 hạch toán kinh tế (cuối lứa) — đồng; khối lượng kg.
  cost_seed             numeric(18,0) NULL CHECK (cost_seed IS NULL OR cost_seed >= 0),
  cost_feed             numeric(18,0) NULL CHECK (cost_feed IS NULL OR cost_feed >= 0),
  cost_depreciation     numeric(18,0) NULL CHECK (cost_depreciation IS NULL OR cost_depreciation >= 0),
  cost_transport        numeric(18,0) NULL CHECK (cost_transport IS NULL OR cost_transport >= 0),
  cost_vet              numeric(18,0) NULL CHECK (cost_vet IS NULL OR cost_vet >= 0),
  cost_probiotic        numeric(18,0) NULL CHECK (cost_probiotic IS NULL OR cost_probiotic >= 0),
  revenue_heads         integer       NULL CHECK (revenue_heads IS NULL OR revenue_heads >= 0),
  revenue_output_kg     numeric(14,2) NULL CHECK (revenue_output_kg IS NULL OR revenue_output_kg >= 0),
  revenue_price         numeric(18,0) NULL CHECK (revenue_price IS NULL OR revenue_price >= 0),    -- đồng/kg
  revenue_amount        numeric(18,0) NULL CHECK (revenue_amount IS NULL OR revenue_amount >= 0),
  status_reason         text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time            timestamptz   NOT NULL,
  recorded_at           timestamptz   NOT NULL DEFAULT now(),
  actor_ref             uuid          NOT NULL,
  on_behalf_of          uuid          NULL,
  support_reason        text          NULL,
  idem_key              text          NOT NULL,
  rule_version          text          NOT NULL,
  prev_hash             bytea         NOT NULL,
  row_hash              bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_diary_cycle_stream CHECK (stream_key = 'dcycle:' || cycle_ref::text),
  CONSTRAINT cnts_diary_cycle_reason CHECK (event <> 'reopened' OR status_reason IS NOT NULL),
  CONSTRAINT cnts_diary_cycle_accounting CHECK (event <> 'closed' OR revenue_amount IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_diary_cycle_ref ON pf_extension.cnts_diary_cycle (tenant_id, cycle_ref, seq);
CREATE INDEX cnts_diary_cycle_household ON pf_extension.cnts_diary_cycle (tenant_id, household_ref);
CREATE INDEX cnts_diary_cycle_site ON pf_extension.cnts_diary_cycle (tenant_id, site_ref);
SELECT shared.apply_ledger('pf_extension.cnts_diary_cycle', 'pf_extension.cnts_diary_cycle');

CREATE TABLE pf_extension.cnts_diary_entry (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'dentry:<entry_ref>'
  seq                 bigint        NOT NULL,
  entry_ref           uuid          NOT NULL,             -- định danh bền của lần ghi (UUIDv7 máy khách sinh được — ngoại tuyến)
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'submitted', 'returned', 'verified', 'corrected')),
  status              text          NOT NULL CHECK (status IN ('draft', 'submitted', 'returned', 'verified')),
  cycle_ref           uuid          NOT NULL,
  household_ref       uuid          NOT NULL,
  site_ref            uuid          NOT NULL,
  project_ref         uuid          NOT NULL,
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  form                text          NOT NULL CHECK (form IN ('BM-08', 'BM-09', 'BM-19')),
  entry_kind          text          NOT NULL CHECK (entry_kind IN ('daily', 'growth_env', 'env', 'harvest')),
  entry_date          date          NOT NULL,             -- NC-04 ngày ghi
  captured_at         timestamptz   NOT NULL,             -- NT-03 thời điểm thiết bị lúc ghi (giữ khi đồng bộ muộn)
  metrics             jsonb         NOT NULL CHECK (jsonb_typeof(metrics) = 'object'),
  tables              jsonb         NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(tables) = 'object'),
  photo_refs          uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(photo_refs) <= 10)
                        CHECK (array_position(photo_refs, NULL) IS NULL),
  checks              jsonb         NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(checks) = 'array'),
  -- Vị trí lúc ghi (NT-15, NC-16) — chép nguyên từ lần ghi đầu, không đổi ở phiên bản sau. Không có tọa độ (QĐ-8).
  fix_accuracy_m      numeric(8,1)  NULL CHECK (fix_accuracy_m IS NULL OR fix_accuracy_m >= 0),
  distance_m          numeric(10,1) NULL CHECK (distance_m IS NULL OR distance_m >= 0),
  has_fix             boolean       NOT NULL,             -- thiết bị gửi tọa độ
  fix_stored          boolean       NOT NULL,             -- còn/đã ghi dòng cnts_diary_fix lúc ghi
  no_target_point     boolean       NOT NULL,             -- hộ/điểm chưa có tọa độ (không tính được khoảng cách)
  far                 boolean       NOT NULL,             -- cách điểm quá ngưỡng (cnts.location_distance_m, 200 m)
  weak_accuracy       boolean       NOT NULL,             -- sai số > cnts.gps_accuracy_m (50 m)
  mock_location       boolean       NOT NULL,             -- thiết bị báo vị trí giả lập
  mock_suspected      boolean       NOT NULL,             -- máy chủ nghi giả lập (cờ thiết bị, sai số 0, trùng tọa độ y hệt)
  author_ref          uuid          NOT NULL,             -- người ghi (tạo lần ghi)
  reviewer_ref        uuid          NULL,                 -- người xác thực / yêu cầu bổ sung
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
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
  CONSTRAINT cnts_diary_entry_stream CHECK (stream_key = 'dentry:' || entry_ref::text),
  CONSTRAINT cnts_diary_entry_reason CHECK (event NOT IN ('returned', 'corrected') OR status_reason IS NOT NULL),
  CONSTRAINT cnts_diary_entry_review CHECK (event NOT IN ('returned', 'verified') OR reviewer_ref IS NOT NULL),
  -- Người xác thực khác người ghi (UR-TINH-12, kiểm cả ở máy chủ ứng dụng).
  CONSTRAINT cnts_diary_entry_four_eyes CHECK (event <> 'verified' OR (reviewer_ref <> author_ref AND reviewer_ref = actor_ref)),
  CONSTRAINT cnts_diary_entry_far CHECK (NOT far OR distance_m IS NOT NULL),
  CONSTRAINT cnts_diary_entry_fix CHECK (has_fix OR (NOT fix_stored AND distance_m IS NULL AND fix_accuracy_m IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_diary_entry_ref ON pf_extension.cnts_diary_entry (tenant_id, entry_ref, seq);
CREATE INDEX cnts_diary_entry_cycle ON pf_extension.cnts_diary_entry (tenant_id, cycle_ref, entry_date);
CREATE INDEX cnts_diary_entry_household ON pf_extension.cnts_diary_entry (tenant_id, household_ref);
CREATE INDEX cnts_diary_entry_project ON pf_extension.cnts_diary_entry (tenant_id, project_ref, status);
SELECT shared.apply_ledger('pf_extension.cnts_diary_entry', 'pf_extension.cnts_diary_entry');

CREATE TABLE pf_extension.cnts_diary_fix (
  tenant_id           uuid          NOT NULL,
  entry_ref           uuid          NOT NULL,             -- = cnts_diary_entry.entry_ref
  household_ref       uuid          NOT NULL,
  point               geometry(Point, 4326) NOT NULL,
  accuracy_m          numeric(8,1)  NULL CHECK (accuracy_m IS NULL OR accuracy_m >= 0),
  captured_at         timestamptz   NOT NULL,
  purge_after         date          NOT NULL,             -- hạn lưu: job xóa khi purge_after < hôm nay
  actor_ref           uuid          NOT NULL,
  created_at          timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, entry_ref)
);
CREATE INDEX cnts_diary_fix_purge ON pf_extension.cnts_diary_fix (purge_after);
CREATE INDEX cnts_diary_fix_household ON pf_extension.cnts_diary_fix (tenant_id, household_ref);
SELECT shared.apply_tenant_rls('pf_extension.cnts_diary_fix');
-- Không sửa được (NT-15, NC-16); XÓA được (hạn lưu, yêu cầu xóa dữ liệu cá nhân) — không phải sổ.
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_extension.cnts_diary_fix
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_extension.cnts_diary_photo (
  tenant_id           uuid          NOT NULL,
  file_ref            uuid          NOT NULL,             -- pf_files.file.file_ref (pf/evidence) — bản mã ở kho đối tượng
  household_ref       uuid          NOT NULL,
  kind                text          NOT NULL CHECK (kind IN ('weighing', 'test_kit', 'flock', 'other')),
  content_type        text          NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
  size_bytes          integer       NOT NULL CHECK (size_bytes BETWEEN 1 AND 1048576),
  captured_at         timestamptz   NULL,
  uploaded_by         uuid          NOT NULL,
  uploaded_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, file_ref)
);
CREATE INDEX cnts_diary_photo_household ON pf_extension.cnts_diary_photo (tenant_id, household_ref);
SELECT shared.apply_tenant_rls('pf_extension.cnts_diary_photo');
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_extension.cnts_diary_photo
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();

-- Dọn tọa độ lần ghi quá hạn lưu (job extension_work_schedule, theo lô, tenant hiện hành qua RLS — SECURITY INVOKER).
CREATE FUNCTION pf_extension.purge_diary_fixes(p_today date, p_limit integer DEFAULT 1000) RETURNS bigint
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  n bigint;
BEGIN
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 100000 THEN
    RAISE EXCEPTION 'purge_diary_fixes: p_limit phải trong 1..100000' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  WITH gone AS (
    DELETE FROM pf_extension.cnts_diary_fix p
    WHERE (p.tenant_id, p.entry_ref) IN (
      SELECT x.tenant_id, x.entry_ref FROM pf_extension.cnts_diary_fix x
      WHERE x.purge_after < p_today ORDER BY x.purge_after LIMIT p_limit)
    RETURNING 1)
  SELECT count(*) INTO n FROM gone;
  RETURN n;
END $$;
REVOKE ALL ON FUNCTION pf_extension.purge_diary_fixes(date, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.purge_diary_fixes(date, integer) TO r_extension;

REVOKE ALL ON pf_extension.cnts_diary_cycle, pf_extension.cnts_diary_entry, pf_extension.cnts_diary_fix,
  pf_extension.cnts_diary_photo FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_diary_cycle, pf_extension.cnts_diary_entry, pf_extension.cnts_diary_photo TO r_extension;
GRANT SELECT, INSERT, DELETE ON pf_extension.cnts_diary_fix TO r_extension;
