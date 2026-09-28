-- pf_extension (bổ sung): báo cáo tiến độ Đề án 1 triệu ha lúa chất lượng cao,
-- phát thải thấp (BRD App Khuyến nông v1.0 FR-03): diện tích đăng ký / đang triển
-- khai / hoàn thành theo HTX × vụ. KNV xã tổng hợp, Admin tỉnh duyệt, Admin TW
-- xem toàn quốc. Mục tiêu diện tích của tỉnh là tham số pf/masterdata
-- "extension.program_target_ha:<mã tỉnh>" (tra theo thời điểm, mục 16).
--
-- Bảng sổ mới, KHÔNG thêm cột vào field_report: chuỗi băm của sổ tính trên toàn
-- bộ dòng (to_jsonb), thêm cột sẽ làm verify_chain của dòng cũ sai.

CREATE TABLE pf_extension.program_report (
  id                 uuid          NOT NULL,
  tenant_id          uuid          NOT NULL,
  stream_key         text          NOT NULL,
  seq                bigint        NOT NULL,
  version_key        text          NOT NULL,               -- program:<xã>:<HTX>:<vụ>:<năm>
  commune_code       text          NOT NULL,
  province_code      text          NOT NULL,
  cooperative_ref    uuid          NOT NULL,
  season_code        text          NOT NULL,
  season_year        integer       NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  registered_area_ha numeric(14,4) NOT NULL CHECK (registered_area_ha >= 0),
  deploying_area_ha  numeric(14,4) NOT NULL CHECK (deploying_area_ha >= 0),
  completed_area_ha  numeric(14,4) NOT NULL CHECK (completed_area_ha >= 0),
  note               text          NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  version            integer       NOT NULL CHECK (version >= 1),
  previous_id        uuid          NULL,
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- Đang triển khai + hoàn thành không vượt diện tích đăng ký.
  CHECK (deploying_area_ha + completed_area_ha <= registered_area_ha)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX program_report_idem   ON pf_extension.program_report (tenant_id, idem_key);
CREATE INDEX program_report_stream ON pf_extension.program_report (tenant_id, stream_key, seq);
CREATE INDEX program_report_id     ON pf_extension.program_report (id);

SELECT shared.apply_ledger('pf_extension.program_report', 'pf_extension.program_report');

-- Phiên bản hiện hành theo khóa (R-21) — nguồn của báo cáo FR-03. approved_report_id
-- là phiên bản đã được Admin tỉnh duyệt; gửi phiên bản mới thì phải duyệt lại.
CREATE TABLE pf_extension.program_report_current (
  tenant_id          uuid          NOT NULL,
  version_key        text          NOT NULL,
  report_id          uuid          NOT NULL,
  commune_code       text          NOT NULL,
  province_code      text          NOT NULL,
  cooperative_ref    uuid          NOT NULL,
  season_code        text          NOT NULL,
  season_year        integer       NOT NULL,
  registered_area_ha numeric(14,4) NOT NULL,
  deploying_area_ha  numeric(14,4) NOT NULL,
  completed_area_ha  numeric(14,4) NOT NULL,
  version            integer       NOT NULL,
  submitted_by       uuid          NOT NULL,
  approved_report_id uuid          NULL,
  approved_by        uuid          NULL,
  approved_at        timestamptz   NULL,
  updated_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, version_key),
  CHECK ((approved_report_id IS NULL) = (approved_by IS NULL))
);
CREATE INDEX program_report_current_unit ON pf_extension.program_report_current (tenant_id, province_code, commune_code, season_code, season_year);
SELECT shared.apply_tenant_rls('pf_extension.program_report_current');

-- Quyết định duyệt của Admin tỉnh (chỉ ghi thêm).
CREATE TABLE pf_extension.program_report_approval (
  tenant_id    uuid        NOT NULL,
  report_id    uuid        NOT NULL,
  approver_ref uuid        NOT NULL,
  approved_at  timestamptz NOT NULL,
  note         text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, report_id)
);
SELECT shared.apply_tenant_rls('pf_extension.program_report_approval');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_extension.program_report_approval
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.program_report_approval
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_extension.program_report, pf_extension.program_report_approval TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.program_report, pf_extension.program_report_approval FROM extension_app;
GRANT SELECT, INSERT, UPDATE ON pf_extension.program_report_current TO extension_app;
