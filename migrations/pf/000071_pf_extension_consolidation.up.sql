-- pf_extension KN v7.7 P4 (4/4): "Báo cáo tổng hợp các xã" — mỗi lần "Cập nhật" (hoặc tự động một lần lúc
-- chốt kỳ, người = Hệ thống) là một LẦN TỔNG HỢP BẤT BIẾN (FN-TI-02.05; docs/brd/kn-v7.7-plan.md §2 pf 000071,
-- gap §4.1 "nhật ký không xóa", §4.6 "số đã chốt giữ theo cấu trúc lúc chốt").
--
--   1. consolidation_run          SỔ: kỳ, biểu, người/Hệ thống, số xã có dữ liệu, số xã/ô thay đổi, ảnh chụp
--                                 cấu trúc khu vực (mã, tên khu vực, tên xã lúc chạy)
--   2. consolidation_snapshot     giá trị tổng hợp theo mẫu (dòng xã + dòng Khu vực/Toàn tỉnh) của lần chạy —
--                                 NGUỒN DUY NHẤT cho dashboard Tỉnh/TW, báo cáo TW và báo cáo Chi cục
--   3. consolidation_cell_change  chi tiết ô thay đổi so với lần trước: xã, cột Excel, cũ → mới, người sửa, lúc
--                                 xã gửi, loại (new | resubmit | adjusted | computed)
--   4. consolidation_metric       chỉ số dashboard dựng sẵn khi chạy (bảng tóm tắt, < 5 s — FN-TW-01.01 AC)
--   5. consolidation_latest       con trỏ lần chạy mới nhất theo kỳ × biểu (bảng chiếu, dựng lại được từ sổ)
--
-- 2–4 chỉ ghi thêm (trigger fn_immutable), không phải sổ băm: số dòng lớn (102 xã × 226 cột), nguồn là sổ
-- submission_version + consolidation_run đã có chuỗi băm.

CREATE TABLE pf_extension.consolidation_run (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                -- 'cons:<period_id>:<template_id>'
  seq                bigint      NOT NULL,
  run_no             integer     NOT NULL CHECK (run_no >= 1),
  period_id          uuid        NOT NULL,
  batch_id           uuid        NOT NULL,
  template_id        uuid        NOT NULL,
  version_id         uuid        NOT NULL,
  form_code          text        NOT NULL,
  department_code    text        NOT NULL,
  province_code      text        NOT NULL,
  trigger            text        NOT NULL CHECK (trigger IN ('manual', 'system')),
  communes_total     integer     NOT NULL CHECK (communes_total >= 0),
  communes_with_data integer     NOT NULL CHECK (communes_with_data >= 0),
  communes_changed   integer     NOT NULL CHECK (communes_changed >= 0),
  cells_changed      integer     NOT NULL CHECK (cells_changed >= 0),
  provisional        boolean     NOT NULL,                -- kỳ chưa chốt hoặc còn xã chưa nộp → "Tạm tính"
  missing_communes   text[]      NOT NULL DEFAULT '{}',
  region_snapshot    jsonb       NOT NULL,                -- [{commune, commune_name, region, region_name}]
  previous_run_id    uuid        NULL,
  on_behalf_of       uuid        NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (communes_with_data <= communes_total)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX consolidation_run_period ON pf_extension.consolidation_run (tenant_id, period_id, template_id, run_no DESC);
CREATE INDEX consolidation_run_id ON pf_extension.consolidation_run (tenant_id, id);
SELECT shared.apply_ledger('pf_extension.consolidation_run', 'pf_extension.consolidation_run');

CREATE TABLE pf_extension.consolidation_snapshot (
  tenant_id    uuid        NOT NULL,
  run_id       uuid        NOT NULL,
  period_id    uuid        NOT NULL,
  template_id  uuid        NOT NULL,
  rows         jsonb       NOT NULL,                      -- [{key, commune, region, indicator, cells}]
  totals       jsonb       NOT NULL,                      -- [{key, kind, region, indicator, members, cells}]
  sources      jsonb       NOT NULL,                      -- {mã xã: {submission_id, version_no, submitted_at, actor_ref, on_behalf_of}}
  created_at   timestamptz NOT NULL,
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, run_id)
);
SELECT shared.apply_tenant_rls('pf_extension.consolidation_snapshot');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_extension.consolidation_snapshot
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.consolidation_snapshot
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_extension.consolidation_cell_change (
  tenant_id      uuid        NOT NULL,
  id             uuid        NOT NULL,
  run_id         uuid        NOT NULL,
  period_id      uuid        NOT NULL,
  template_id    uuid        NOT NULL,
  form_code      text        NOT NULL,
  commune_code   text        NOT NULL,
  row_key        text        NOT NULL,
  letter         text        NOT NULL CHECK (letter ~ '^[A-Z]{1,3}$'),
  old_value      text        NULL,
  new_value      text        NULL,
  change_kind    text        NOT NULL CHECK (change_kind IN ('new', 'resubmit', 'adjusted', 'computed')),
  submission_id  uuid        NULL,
  changed_by     uuid        NULL,                        -- người gửi bản nộp (actor_ref; tên tra lúc đọc)
  on_behalf_of   uuid        NULL,
  submitted_at   timestamptz NULL,
  created_at     timestamptz NOT NULL,
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX consolidation_cell_change_run ON pf_extension.consolidation_cell_change (tenant_id, run_id, commune_code, letter);
CREATE INDEX consolidation_cell_change_cell ON pf_extension.consolidation_cell_change (tenant_id, period_id, template_id, commune_code, letter, created_at DESC);
SELECT shared.apply_tenant_rls('pf_extension.consolidation_cell_change');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_extension.consolidation_cell_change
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.consolidation_cell_change
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_extension.consolidation_metric (
  tenant_id       uuid          NOT NULL,
  run_id          uuid          NOT NULL,
  period_id       uuid          NOT NULL,
  batch_id        uuid          NOT NULL,
  form_code       text          NOT NULL,
  department_code text          NOT NULL,
  province_code   text          NOT NULL,
  metric          text          NOT NULL CHECK (metric ~ '^[a-z][a-z0-9_.:-]{1,79}$'),
  scope           text          NOT NULL CHECK (scope IN ('province', 'region', 'commune')),
  unit_code       text          NOT NULL,                 -- mã tỉnh, mã khu vực ('' = chưa gán) hoặc mã xã
  value           numeric(18,4) NOT NULL,
  recorded_at     timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, run_id, metric, scope, unit_code)
);
CREATE INDEX consolidation_metric_lookup ON pf_extension.consolidation_metric (tenant_id, province_code, department_code, period_id, metric);
SELECT shared.apply_tenant_rls('pf_extension.consolidation_metric');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_extension.consolidation_metric
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.consolidation_metric
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_extension.consolidation_latest (
  tenant_id       uuid        NOT NULL,
  period_id       uuid        NOT NULL,
  template_id     uuid        NOT NULL,
  run_id          uuid        NOT NULL,
  run_no          integer     NOT NULL,
  batch_id        uuid        NOT NULL,
  form_code       text        NOT NULL,
  department_code text        NOT NULL,
  province_code   text        NOT NULL,
  run_at          timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, period_id, template_id)
);
CREATE INDEX consolidation_latest_scope ON pf_extension.consolidation_latest (tenant_id, province_code, department_code, form_code);
SELECT shared.apply_tenant_rls('pf_extension.consolidation_latest');

GRANT SELECT, INSERT ON pf_extension.consolidation_run, pf_extension.consolidation_snapshot, pf_extension.consolidation_cell_change,
  pf_extension.consolidation_metric TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.consolidation_run, pf_extension.consolidation_snapshot, pf_extension.consolidation_cell_change,
  pf_extension.consolidation_metric FROM extension_app;
GRANT SELECT, INSERT, UPDATE ON pf_extension.consolidation_latest TO extension_app;
REVOKE DELETE, TRUNCATE ON pf_extension.consolidation_latest FROM extension_app;
