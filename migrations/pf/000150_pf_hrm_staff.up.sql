-- pf/hrm (`dom/hrm`, W11.22 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-07, FN-08, FN-20): HỒ SƠ NHÂN SỰ.
--
-- KHÔNG có dữ liệu cá nhân ở schema này (RB-3): họ tên, SĐT, địa chỉ ở vault.identity; ngày sinh, giới tính, email,
-- ghi chú ở ngăn vault 'hrm.profile'; dân tộc, tôn giáo, ngày vào Đảng, quê quán, nơi sinh, ngày TGCM (thẻ Thống kê BC
-- Sở, TK-01..06) ở ngăn vault 'hrm.sensitive' có khóa và mục đích riêng (vault 000004). pf_hrm chỉ giữ mã tham chiếu
-- chủ thể (subject_ref), mã hồ sơ, loại nhân sự, quá trình (mã danh mục, ngày, số quyết định) và TÊN trường đã đổi.
--
-- Bốn sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   - staff          — phiên bản đầu hồ sơ, chuỗi 'staff:<staff_id>': created (mã HS-01 tự sinh, loại HS-15, chủ thể
--                      vault, tài khoản gắn — phạm vi "bản thân" của FN-20), profile/sensitive (đã sửa ngăn vault: chỉ
--                      ghi tên trường), type, account. Hồ sơ không xóa (FN-07 BR-01).
--   - staff_event    — quá trình có hiệu lực (valid_from + mức chính xác BR-05; valid_to cho tăng cường, kỷ luật):
--                      vào làm/nghỉ việc (FN-08), đơn vị, chức vụ, tăng cường, ngạch, bậc (bậc = phép chiếu của các sự
--                      kiện ngạch/bậc — Q-069: lưu ngày xếp bậc và hệ số bảo lưu), văn bằng, bồi dưỡng, khen thưởng
--                      (không số tiền), kỷ luật (cờ — Q-075). Sửa = dòng mới supersedes_id + lý do; hủy = dòng action
--                      'void' supersedes_id + lý do. Truy vấn as-of chiếu các dòng còn hiệu lực tới ngày hỏi.
--   - record_access  — NHẬT KÝ XEM/XUẤT: mỗi lần xem chi tiết (view), xem thẻ Thống kê BC Sở (view_sensitive), xuất
--                      danh sách (export, mã các hồ sơ đã xuất + bộ lọc không chứa PII). Giữ ≥ 1 năm (NFR Module 2; sổ
--                      phân vùng theo recorded_at, chính sách giữ của sổ chung ≥ 13 tháng).
--   - perm_rule      — ma trận phân quyền hồ sơ (FN-20): mỗi dòng là phiên bản của (nhóm vai | người dùng) → phạm vi
--                      theo nhóm quyền; sổ chính là "nhật ký đổi quyền".
-- Vai: r_hrm SELECT, INSERT.

CREATE TABLE pf_hrm.staff (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'staff:<staff_id>'
  seq               bigint      NOT NULL,
  staff_id          uuid        NOT NULL,
  code              text        NOT NULL CHECK (code ~ '^(VC|HD|XA)-[0-9]{5,}$'),
  staff_type        text        NOT NULL CHECK (staff_type IN ('vien_chuc', 'hop_dong', 'can_bo_xa')),
  subject_ref       uuid        NOT NULL,                 -- chủ thể ở kho định danh (R-41: mã ngẫu nhiên)
  account_ref       uuid        NULL,                     -- tài khoản (sub Keycloak) của chính nhân sự — phạm vi "bản thân"
  change            text        NOT NULL CHECK (change IN ('created', 'profile', 'sensitive', 'type', 'account')),
  changed_fields    text[]      NOT NULL DEFAULT '{}',    -- TÊN trường đã đổi (không giá trị) — "Lịch sử sửa"
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
  CHECK (array_position(changed_fields, NULL) IS NULL AND cardinality(changed_fields) <= 20),
  CONSTRAINT staff_stream CHECK (stream_key = 'staff:' || staff_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX staff_id_seq ON pf_hrm.staff (tenant_id, staff_id, seq);
CREATE INDEX staff_code ON pf_hrm.staff (tenant_id, code);
CREATE INDEX staff_subject ON pf_hrm.staff (tenant_id, subject_ref);
SELECT shared.apply_ledger('pf_hrm.staff', 'pf_hrm.staff');

CREATE TABLE pf_hrm.staff_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'staff:<staff_id>'
  seq               bigint      NOT NULL,
  staff_id          uuid        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('work_start', 'work_end', 'unit', 'position', 'secondment', 'grade',
                                  'step', 'degree', 'training', 'award', 'discipline')),
  action            text        NOT NULL CHECK (action IN ('add', 'void')),
  valid_from        date        NOT NULL,
  date_precision    text        NOT NULL CHECK (date_precision IN ('day', 'month', 'year')),
  valid_to          date        NULL CHECK (valid_to IS NULL OR valid_to >= valid_from),
  payload           jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,                     -- dòng bị thay (action add) hoặc bị hủy (action void)
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
  CHECK (action = 'add' OR supersedes_id IS NOT NULL),
  CONSTRAINT staff_event_stream CHECK (stream_key = 'staff:' || staff_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX staff_event_staff ON pf_hrm.staff_event (tenant_id, staff_id, seq);
CREATE INDEX staff_event_kind ON pf_hrm.staff_event (tenant_id, kind, valid_from);
SELECT shared.apply_ledger('pf_hrm.staff_event', 'pf_hrm.staff_event');

CREATE TABLE pf_hrm.record_access (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'actor:<actor_ref>'
  seq               bigint      NOT NULL,
  action            text        NOT NULL CHECK (action IN ('view', 'view_sensitive', 'export')),
  staff_id          uuid        NULL,                     -- view, view_sensitive
  staff_ids         uuid[]      NULL CHECK (staff_ids IS NULL OR cardinality(staff_ids) <= 5000),  -- export
  item_count        integer     NOT NULL CHECK (item_count >= 0),
  scope             text        NOT NULL CHECK (scope IN ('self', 'unit', 'all')),  -- phạm vi đã cho phép
  query             jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(query) = 'object'), -- bộ lọc (mã, ngày; không PII)
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
  CHECK ((action = 'export') = (staff_ids IS NOT NULL) AND (action = 'export' OR staff_id IS NOT NULL)),
  CONSTRAINT record_access_stream CHECK (stream_key = 'actor:' || actor_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX record_access_staff ON pf_hrm.record_access (tenant_id, staff_id, recorded_at);
CREATE INDEX record_access_actor ON pf_hrm.record_access (tenant_id, actor_ref, recorded_at);
CREATE INDEX record_access_export ON pf_hrm.record_access USING gin (staff_ids);
SELECT shared.apply_ledger('pf_hrm.record_access', 'pf_hrm.record_access');

CREATE TABLE pf_hrm.perm_rule (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'perm:<grantee_kind>:<grantee>'
  seq               bigint      NOT NULL,
  grantee_kind      text        NOT NULL CHECK (grantee_kind IN ('role', 'user')),
  grantee           text        NOT NULL CHECK (grantee ~ '^[a-z0-9][a-z0-9_-]{1,63}$'),
  perms             jsonb       NOT NULL CHECK (jsonb_typeof(perms) = 'object'),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
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
  CHECK (grantee_kind = 'user' OR grantee <> 'admin'),     -- quyền Admin cố định (chặn gỡ Admin cuối)
  CONSTRAINT perm_rule_stream CHECK (stream_key = 'perm:' || grantee_kind || ':' || grantee)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX perm_rule_grantee ON pf_hrm.perm_rule (tenant_id, grantee_kind, grantee, seq);
SELECT shared.apply_ledger('pf_hrm.perm_rule', 'pf_hrm.perm_rule');

REVOKE ALL ON pf_hrm.staff, pf_hrm.staff_event, pf_hrm.record_access, pf_hrm.perm_rule FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.staff, pf_hrm.staff_event, pf_hrm.record_access, pf_hrm.perm_rule TO r_hrm;
