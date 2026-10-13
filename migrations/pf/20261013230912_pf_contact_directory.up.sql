-- pf/contact (`cap/directory`, W11.23 — BRD Hành chính – Tổng hợp v3.0 FN-13 Contact; Q-071, Q-072 theo đề xuất của dev):
-- DANH BẠ LIÊN HỆ DÙNG CHUNG cho mọi module/ứng dụng.
--
-- Một người = MỘT bản ghi (FN-13 BR-01) = một chủ thể ở kho định danh (subject_ref). Cán bộ thuộc tỉnh (viên chức, hợp
-- đồng, cán bộ khuyến nông cấp xã) KHÔNG lưu ở đây: pf/contact chiếu từ hồ sơ nhân sự pf/hrm qua port (sửa tại hồ sơ
-- FN-07 — AC-02), theo cổng AG_HRM_RECORDS. Schema này chỉ giữ liên hệ NGOÀI TỈNH (Trung ương, tỉnh khác, đơn vị khác) và
-- thẻ Đơn vị.
--
-- KHÔNG có dữ liệu cá nhân ở schema này (RB-3): họ tên, SĐT của người ở vault.identity; email, năm sinh ở ngăn vault
-- 'contact.card' (mục đích contact.directory — vault 20261013165158). pf_contact.person chỉ có mã tham chiếu, cấp, tên cơ
-- quan, chức danh và TÊN trường đã đổi (lịch sử thay đổi BR-05, không giá trị).
-- pf_contact.unit là liên hệ của TỔ CHỨC (tên đơn vị, cấp, số máy/email/địa chỉ công vụ của cơ quan) — không phải dữ liệu
-- cá nhân; số di động của một người phải nhập ở thẻ Cán bộ (kho định danh), không ở thẻ Đơn vị.
--
-- Hai sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant). Mỗi dòng
-- là MỘT phiên bản; phiên bản mới nhất là hiện hành; "ngừng hiển thị" = phiên bản hidden = true (không xóa).
-- Vai: r_contact SELECT, INSERT; k_ref như mọi module pf sinh sau 000092 (R-DB-05).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_contact' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_contact (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;

CREATE SCHEMA pf_contact;
REVOKE ALL ON SCHEMA pf_contact FROM PUBLIC;

CREATE TABLE pf_contact.person (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'person:<subject_ref>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  subject_ref       uuid        NOT NULL,                 -- chủ thể ở kho định danh = mã liên hệ (một bản ghi/người)
  level             text        NOT NULL CHECK (level IN ('tw', 'tinh', 'xa', 'khac')),
  organization      text        NULL CHECK (organization IS NULL OR char_length(organization) BETWEEN 1 AND 200),
  title             text        NULL CHECK (title IS NULL OR char_length(title) BETWEEN 1 AND 200),
  hidden            boolean     NOT NULL DEFAULT false,   -- ngừng hiển thị (FN-13 B4)
  change            text        NOT NULL CHECK (change IN ('created', 'updated', 'hidden', 'shown')),
  changed_fields    text[]      NOT NULL DEFAULT '{}',    -- TÊN trường đã đổi (không giá trị) — BR-05
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
  CHECK (array_position(changed_fields, NULL) IS NULL AND cardinality(changed_fields) <= 10),
  CONSTRAINT person_stream CHECK (stream_key = 'person:' || subject_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX person_subject_seq ON pf_contact.person (tenant_id, subject_ref, seq);
SELECT shared.apply_ledger('pf_contact.person', 'pf_contact.person');

CREATE TABLE pf_contact.unit (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'unit:<unit_id>'
  seq               bigint      NOT NULL,
  unit_id           uuid        NOT NULL,
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  level             text        NOT NULL CHECK (level IN ('tw', 'tinh', 'xa', 'khac')),
  org_ref           uuid        NULL,                     -- tổ chức ở pf/party (nếu là đơn vị trong cây tổ chức)
  phone             text        NULL CHECK (phone IS NULL OR phone ~ '^[0-9]{8,11}$'),   -- số máy công vụ của cơ quan
  email             text        NULL CHECK (email IS NULL OR char_length(email) BETWEEN 3 AND 254),
  address           text        NULL CHECK (address IS NULL OR char_length(address) BETWEEN 1 AND 300),
  hidden            boolean     NOT NULL DEFAULT false,
  change            text        NOT NULL CHECK (change IN ('created', 'updated', 'hidden', 'shown')),
  changed_fields    text[]      NOT NULL DEFAULT '{}',
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
  CHECK (array_position(changed_fields, NULL) IS NULL AND cardinality(changed_fields) <= 10),
  CONSTRAINT unit_stream CHECK (stream_key = 'unit:' || unit_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX unit_id_seq ON pf_contact.unit (tenant_id, unit_id, seq);
SELECT shared.apply_ledger('pf_contact.unit', 'pf_contact.unit');

REVOKE ALL ON pf_contact.person, pf_contact.unit FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_contact TO r_contact;
GRANT SELECT, INSERT ON pf_contact.person, pf_contact.unit TO r_contact;

-- k_ref như mọi module pf sinh sau 000092 (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_contact;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_contact;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_contact;
