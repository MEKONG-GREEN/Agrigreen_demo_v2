-- pf/extension (`dom/extension`) — BF-SRV-KN-ATTRS (#119, con của #113 BF-SRV-GUARD): HỒ SƠ KN RIÊNG cho hai thuộc
-- tính mà khai báo nền không có — năm sản xuất theo loại máy của HTX (TT-10) và cây trồng của thửa (TT-11). Dữ liệu
-- thuộc KN, ở schema pf_extension; KHÔNG thêm cột, KHÔNG ghi bảng HTX (pf_coop, pf_mechanization). HTX/thửa chỉ được
-- đọc qua publication đã cấp (pf_party_pub.v1_org_tree, pf_cooperative_pub.v1_plots) trước giao dịch ghi.
--
-- Hai sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE; RLS tenant). Bản
-- hiện hành = dòng mới nhất theo khóa; không bảng chiếu riêng (không UPDATE):
-- 1. kn_machine_year — chuỗi 'kn_machine_year:<phòng ban>:<cooperative_ref>'; mỗi dòng là một loại máy: set (năm sản
--    xuất) hoặc removed (loại máy bỏ khỏi hồ sơ). Năm ≤ năm hiện tại kiểm ở ứng dụng theo giờ Việt Nam.
-- 2. kn_plot_crop — chuỗi 'kn_plot_crop:<phòng ban>:<plot_ref>'; mỗi dòng là cây trồng hiện hành của thửa (danh mục
--    masterdata "crop", tên chép lúc ghi). Cây trồng bắt buộc: không có sự kiện xóa.
-- Không dữ liệu cá nhân ngoài actor_ref (mã tài khoản). Vai r_extension: SELECT, INSERT.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.kn_machine_year (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'kn_machine_year:<department_code>:<cooperative_ref>'
  seq               bigint      NOT NULL,
  cooperative_ref   uuid        NOT NULL,                 -- HTX (pf_party_pub.v1_org_tree, kind cooperative)
  commune_code      text        NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  machine_type      text        NOT NULL CHECK (char_length(machine_type) BETWEEN 1 AND 60),
  machine_type_name text        NOT NULL CHECK (char_length(machine_type_name) BETWEEN 1 AND 200),
  event             text        NOT NULL CHECK (event IN ('set', 'removed')),
  year_made         integer     NULL CHECK (year_made IS NULL OR year_made BETWEEN 1900 AND 2100),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,                     -- chế độ hỗ trợ của Admin
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_machine_year_stream CHECK (stream_key = 'kn_machine_year:' || department_code || ':' || cooperative_ref::text),
  CONSTRAINT kn_machine_year_event CHECK ((event = 'set') = (year_made IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_machine_year_coop ON pf_extension.kn_machine_year (tenant_id, department_code, cooperative_ref, machine_type, seq DESC);
SELECT shared.apply_ledger('pf_extension.kn_machine_year', 'pf_extension.kn_machine_year');

CREATE TABLE pf_extension.kn_plot_crop (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'kn_plot_crop:<department_code>:<plot_ref>'
  seq               bigint      NOT NULL,
  plot_ref          uuid        NOT NULL,                 -- thửa (pf_cooperative_pub.v1_plots)
  commune_code      text        NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  event             text        NOT NULL CHECK (event = 'set'),
  crop_code         text        NOT NULL CHECK (char_length(crop_code) BETWEEN 1 AND 60),
  crop_name         text        NOT NULL CHECK (char_length(crop_name) BETWEEN 1 AND 200),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_plot_crop_stream CHECK (stream_key = 'kn_plot_crop:' || department_code || ':' || plot_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_plot_crop_plot ON pf_extension.kn_plot_crop (tenant_id, department_code, plot_ref, seq DESC);
CREATE INDEX kn_plot_crop_commune ON pf_extension.kn_plot_crop (tenant_id, department_code, commune_code, plot_ref);
SELECT shared.apply_ledger('pf_extension.kn_plot_crop', 'pf_extension.kn_plot_crop');

REVOKE ALL ON pf_extension.kn_machine_year, pf_extension.kn_plot_crop FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.kn_machine_year, pf_extension.kn_plot_crop TO r_extension;
