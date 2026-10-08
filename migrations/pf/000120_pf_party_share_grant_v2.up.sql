-- pf_party W4.1: cấp phép chia sẻ v2 (ADR-0030 mục 3, kiến trúc v4.1 §17.1, §17.3, R-SEC-04).
--
-- Thêm vào cấp phép của ADR-0020 (sổ share_grant_event giữ nguyên, bốn mắt bằng CHECK, thu hồi có lý do):
--   grant_class     intra_platform (đọc tại chỗ qua share_scope, W4.2) | partner (MKG, bên thứ ba — đường ADR-0020)
--   module_table    '<module>:<share>' — tên logic khai trong `shares` của module.yaml chủ (app kiểm theo danh mục
--                   modreg; W4.2 ánh xạ sang bảng vật lý của share_scope, vd pf/cooperative:household → pf_coop.household)
--   scope_node_ref  Nút phạm vi của đơn vị chủ (pf_party.org của tenant bên cấp)
--   purpose_code    mục đích có mã (vd khuyen-nong), khớp app.purpose của phiên
--   basis_ref       căn cứ: mã thỏa thuận chia sẻ (C-10 / H-15) — không dữ liệu cá nhân
--
-- KHÔNG thêm cột vào sổ share_grant_event: chuỗi băm của sổ tính trên to_jsonb(dòng) (shared.fn_chain /
-- shared.verify_chain), nên một cột mới (kể cả NULL) đổi nội dung băm của MỌI dòng cũ và verify_chain báo gãy
-- (cùng lý do docs/design/mkg.md: sổ phụ roll_count_location thay vì thêm cột vào roll_count). Điều khoản v2 ghi
-- ở SỔ PHỤ share_grant_terms (một dòng mỗi cấp phép, ghi cùng giao dịch với dòng 'proposed'); bảng chiếu
-- share_grant (không băm) mang thêm các cột NULL được. Cấp phép cũ không có dòng terms = partner (kế thừa).
--
--   share_grant_review  SỔ rà soát cấp phép định kỳ (R-SEC-04): giữ | thu hồi, người rà soát, ghi chú.
--   context_epoch       Bộ đếm epoch ngữ cảnh theo tenant (đơn vị xem). Thu hồi một cấp phép đang hiệu lực tăng
--                       epoch của tenant bên nhận (cùng giao dịch, kèm sự kiện pf.party.context_epoch.bumped) để
--                       phiên của đơn vị xem làm mới ngữ cảnh ở yêu cầu kế tiếp (§17.1). Bản tối thiểu ở DB cụm pf;
--                       W7.2 (k/access) hợp nhất với epoch ở Valkey định danh.
--
-- Không UPDATE/DELETE dữ liệu (R-25): dòng cũ giữ NULL. Không dữ liệu cá nhân (R-04).
-- Bảng chiếu k_ref.share_grant_active (chủ pf/kref) chưa thêm cột ở đây: agmodgraph cấm một migration sửa schema
-- của hai module; W4.2 (bên dựng share_scope) thêm cột ở migration của pf/kref.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Sổ cấp phép: cho phép kinds rỗng (cấp phép intra_platform phạm vi theo module_table × Nút, không theo loại).
--    Ràng buộc "partner ⇒ ≥ 1 loại, intra_platform ⇒ 0 loại" nằm ở bảng chiếu share_grant (cùng giao dịch).
-- ---------------------------------------------------------------------------
ALTER TABLE pf_party.share_grant_event DROP CONSTRAINT share_grant_event_kinds_check;
ALTER TABLE pf_party.share_grant_event ADD CONSTRAINT share_grant_event_kinds_check
  CHECK (cardinality(kinds) <= 16 AND kinds <@ ARRAY[
    'cooperative_directory', 'plot_directory', 'season_harvest_dates', 'htx_collection_responses',
    'waterway_routes',
    'valve_hub', 'valve_collection', 'valve_mrv_evidence',
    'dp_collection_volume', 'dp_hub_capacity', 'dp_mrv_package_status']::text[]) NOT VALID;
ALTER TABLE pf_party.share_grant_event VALIDATE CONSTRAINT share_grant_event_kinds_check;

-- ---------------------------------------------------------------------------
-- 2. Sổ phụ điều khoản v2 (một dòng mỗi cấp phép: stream 'terms:<grant_id>', seq = 1)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.share_grant_terms (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- bên cấp (đơn vị chủ)
  stream_key        text        NOT NULL,                 -- 'terms:<grant_id>'
  seq               bigint      NOT NULL CHECK (seq = 1), -- điều khoản không đổi sau đề xuất (đổi = cấp phép mới)
  grant_id          uuid        NOT NULL,
  grant_class       text        NOT NULL CHECK (grant_class IN ('intra_platform', 'partner')),
  module_table      text        NULL CHECK (module_table IS NULL OR module_table ~ '^(pf|mkg)/[a-z][a-z0-9_]{0,39}:[a-z][a-z0-9_]{0,59}$'),
  scope_node_ref    uuid        NULL,
  purpose_code      text        NULL CHECK (purpose_code IS NULL OR purpose_code ~ '^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){0,3}$'),
  basis_ref         text        NULL CHECK (basis_ref IS NULL OR char_length(basis_ref) BETWEEN 3 AND 200),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người đề xuất (quản trị đơn vị chủ)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (stream_key = 'terms:' || grant_id::text),
  -- intra_platform: đủ bốn trường phạm vi; partner: không có bảng/Nút (phạm vi theo kinds).
  CHECK (grant_class <> 'intra_platform'
         OR (module_table IS NOT NULL AND scope_node_ref IS NOT NULL AND purpose_code IS NOT NULL AND basis_ref IS NOT NULL)),
  CHECK (grant_class <> 'partner' OR (module_table IS NULL AND scope_node_ref IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX share_grant_terms_grant ON pf_party.share_grant_terms (tenant_id, grant_id);
SELECT shared.apply_ledger('pf_party.share_grant_terms', 'pf_party.share_grant_terms');

-- ---------------------------------------------------------------------------
-- 3. Sổ rà soát định kỳ (R-SEC-04): stream 'review:<grant_id>'
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.share_grant_review (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- bên cấp (đơn vị chủ)
  stream_key        text        NOT NULL,                 -- 'review:<grant_id>'
  seq               bigint      NOT NULL,
  grant_id          uuid        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('retain', 'revoke')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 10 AND 500),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người rà soát (quản trị đơn vị chủ)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (stream_key = 'review:' || grant_id::text),
  CHECK (decision <> 'revoke' OR note IS NOT NULL)          -- thu hồi khi rà soát phải có lý do
) PARTITION BY RANGE (recorded_at);
CREATE INDEX share_grant_review_grant ON pf_party.share_grant_review (tenant_id, grant_id, seq);
SELECT shared.apply_ledger('pf_party.share_grant_review', 'pf_party.share_grant_review');

-- ---------------------------------------------------------------------------
-- 4. Bảng chiếu share_grant: điều khoản v2 + lần rà soát cuối (NULL được — dòng cũ = partner kế thừa)
-- ---------------------------------------------------------------------------
ALTER TABLE pf_party.share_grant
  ADD COLUMN grant_class      text        NULL CHECK (grant_class IS NULL OR grant_class IN ('intra_platform', 'partner')),
  ADD COLUMN module_table     text        NULL,
  ADD COLUMN scope_node_ref   uuid        NULL,
  ADD COLUMN purpose_code     text        NULL,
  ADD COLUMN basis_ref        text        NULL,
  ADD COLUMN last_reviewed_at timestamptz NULL,
  ADD COLUMN last_reviewed_by uuid        NULL;
ALTER TABLE pf_party.share_grant ADD CONSTRAINT share_grant_class_kinds
  CHECK (CASE WHEN grant_class = 'intra_platform'
              THEN cardinality(kinds) = 0 AND module_table IS NOT NULL AND scope_node_ref IS NOT NULL
                   AND purpose_code IS NOT NULL AND basis_ref IS NOT NULL
              ELSE cardinality(kinds) BETWEEN 1 AND 16 AND module_table IS NULL AND scope_node_ref IS NULL END) NOT VALID;
ALTER TABLE pf_party.share_grant VALIDATE CONSTRAINT share_grant_class_kinds;
CREATE INDEX share_grant_review_due ON pf_party.share_grant (tenant_id, status, (coalesce(last_reviewed_at, approved_at)));

-- ---------------------------------------------------------------------------
-- 5. Epoch ngữ cảnh theo tenant (đơn vị xem) — bộ đếm, không phải sổ: sự kiện outbox giữ lịch sử
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.context_epoch (
  tenant_id      uuid        NOT NULL PRIMARY KEY,         -- đơn vị xem
  epoch          bigint      NOT NULL CHECK (epoch >= 1),
  cause          text        NOT NULL CHECK (cause IN ('share_grant_revoked')),
  cause_ref      uuid        NULL,                         -- grant_id gây tăng
  cause_tenant   uuid        NULL,                         -- tenant bên cấp của cấp phép
  updated_at     timestamptz NOT NULL DEFAULT now()
);
SELECT shared.apply_tenant_rls('pf_party.context_epoch');

-- ---------------------------------------------------------------------------
-- 6. Quyền (R-05, ADR-0028): vai nhóm r_party (party_app và vai đăng nhập app_* kế thừa). Sổ: SELECT, INSERT.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_party' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_party (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;
REVOKE ALL ON pf_party.share_grant_terms, pf_party.share_grant_review, pf_party.context_epoch FROM PUBLIC;
GRANT SELECT, INSERT ON pf_party.share_grant_terms, pf_party.share_grant_review TO r_party;
GRANT SELECT, INSERT, UPDATE ON pf_party.context_epoch TO r_party;
