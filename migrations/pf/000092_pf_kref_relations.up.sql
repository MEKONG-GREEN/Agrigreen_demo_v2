-- Quan hệ k_ref của cụm pf (W2.5, ADR-0029, kiến trúc v4.1 §17.1–17.3, R-DB-05): bản chiếu CHỈ ĐỌC của dữ liệu
-- lõi, chủ là module pf/kref (`k/ref`, kernel). Schema và hàm k_ref.ctx_* đã có ở pf 000091 (W2.4).
--
--   org                 Nút của cây tổ chức: mã, loại, đơn vị hành chính, cha, trạng thái (nguồn pf_party.org).
--                       "Nút" = mọi dòng pf_party.org; khu vực là thuộc tính của Nút xã, phòng ban là cấu hình
--                       của đơn vị — không phải Nút (KN v7.7 FN-AD-04.02/04.03).
--   node_closure        Bao đóng cây Nút (tổ tiên, hậu duệ, độ sâu), (x, x, 0) cho chính Nút — chính sách RLS
--                       phạm vi Nút (W3.4) viết thẳng EXISTS trên bảng này (DB-14).
--   admin_unit          Phiên bản đơn vị hành chính có hiệu lực theo thời gian + mã kế thừa (pf_masterdata.admin_unit,
--                       ADR-0032; bảng kế thừa chi tiết pf_masterdata.admin_unit_succession của W2.2 không chiếu).
--   share_grant_active  Cấp quyền chia sẻ đang hiệu lực (status active ở pf_party.share_grant); tenant = bên cấp.
--   share_scope         Phạm vi chia sẻ mở rộng (đơn vị xem, mục đích, bảng, đơn vị chủ, Nút) — tạo RỖNG: chưa có
--                       bên dựng (W4.2, pf 000099); chính sách đọc nó ở W4.x.
--
-- Không PII (ADR-0029 §2): chỉ mã, mã tham chiếu, trạng thái. Dữ liệu DẪN XUẤT: dựng lại được bất kỳ lúc nào
-- từ nguồn (pf/kref Rebuild — worker -kref-rebuild, drill kref).
--
-- RLS theo tenant như mọi bảng có tenant_id (RB-2): org, node_closure, admin_unit dùng shared.apply_tenant_rls
-- (mẫu repo); share_grant_active thấy được ở CẢ bên cấp lẫn bên nhận, share_scope theo đơn vị xem — chính sách
-- p_tenant viết tay, đọc biến qua k_ref.ctx_uuid (R-DB-08). WITH CHECK chỉ tenant hiện hành.
--
-- Quyền (R-DB-05): mọi r_* SELECT; CHỈ r_kref (worker pf.kref, vai đăng nhập app_worker) INSERT/UPDATE/DELETE.
-- Không TRUNCATE (dựng lại xóa theo tenant dưới RLS). ag_outbox_publisher (BYPASSRLS, job/diễn tập tìm tenant)
-- chỉ đọc cột tenant_id của k_ref và của bảng nguồn — chỉ GRANT, không tạo/sửa đối tượng của schema khác.

CREATE TABLE k_ref.org (
  tenant_id       uuid        NOT NULL,
  ref             uuid        NOT NULL,
  kind            text        NOT NULL CHECK (length(kind) BETWEEN 1 AND 40),
  code            text        NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
  admin_unit_code text        NULL,
  parent_ref      uuid        NULL,
  active          boolean     NOT NULL,
  source_seq      bigint      NOT NULL CHECK (source_seq >= 0),   -- seq cuối của pf_party.org_event đã áp
  synced_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  CHECK (parent_ref IS NULL OR parent_ref <> ref)
);
CREATE INDEX org_parent ON k_ref.org (tenant_id, parent_ref);
SELECT shared.apply_tenant_rls('k_ref.org');

CREATE TABLE k_ref.node_closure (
  tenant_id  uuid    NOT NULL,
  ancestor   uuid    NOT NULL,
  descendant uuid    NOT NULL,
  depth      integer NOT NULL CHECK (depth >= 0),
  PRIMARY KEY (tenant_id, ancestor, descendant),
  CHECK ((depth = 0) = (ancestor = descendant))
);
CREATE INDEX node_closure_descendant ON k_ref.node_closure (tenant_id, descendant, ancestor);
SELECT shared.apply_tenant_rls('k_ref.node_closure');

CREATE TABLE k_ref.admin_unit (
  tenant_id       uuid        NOT NULL,
  code            text        NOT NULL,
  valid_from      date        NOT NULL,
  valid_to        date        NULL,                  -- NULL = còn hiệu lực
  name            text        NOT NULL,
  level           text        NOT NULL CHECK (level IN ('province', 'district', 'commune')),
  parent_code     text        NULL,
  successor_codes text[]      NOT NULL DEFAULT '{}',
  synced_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, code, valid_from)
);
CREATE INDEX admin_unit_parent ON k_ref.admin_unit (tenant_id, parent_code);
SELECT shared.apply_tenant_rls('k_ref.admin_unit');

CREATE TABLE k_ref.share_grant_active (
  tenant_id         uuid        NOT NULL,                  -- bên cấp (chủ dữ liệu)
  grant_id          uuid        NOT NULL,
  grantee_tenant_id uuid        NOT NULL,                  -- bên nhận
  kinds             text[]      NOT NULL,
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  source_seq        bigint      NOT NULL CHECK (source_seq >= 0),   -- seq cuối của pf_party.share_grant_event đã áp
  synced_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, grant_id),
  CHECK (grantee_tenant_id <> tenant_id),
  CHECK (valid_to > valid_from)
);
CREATE INDEX share_grant_active_grantee ON k_ref.share_grant_active (grantee_tenant_id, tenant_id);
ALTER TABLE k_ref.share_grant_active ENABLE ROW LEVEL SECURITY;
ALTER TABLE k_ref.share_grant_active FORCE ROW LEVEL SECURITY;
CREATE POLICY p_tenant ON k_ref.share_grant_active
  USING (tenant_id = k_ref.ctx_uuid('app.tenant_id') OR grantee_tenant_id = k_ref.ctx_uuid('app.tenant_id'))
  WITH CHECK (tenant_id = k_ref.ctx_uuid('app.tenant_id'));

-- §17.3: khóa chính đúng thứ tự điều kiện của chính sách đọc tại chỗ.
CREATE TABLE k_ref.share_scope (
  viewer_tenant uuid NOT NULL,
  purpose       text NOT NULL,
  module_table  text NOT NULL,
  owner_tenant  uuid NOT NULL,
  node_id       uuid NOT NULL,
  PRIMARY KEY (viewer_tenant, purpose, module_table, owner_tenant, node_id)
);
ALTER TABLE k_ref.share_scope ENABLE ROW LEVEL SECURITY;
ALTER TABLE k_ref.share_scope FORCE ROW LEVEL SECURITY;
CREATE POLICY p_tenant ON k_ref.share_scope
  USING (viewer_tenant = k_ref.ctx_uuid('app.tenant_id'))
  WITH CHECK (viewer_tenant = k_ref.ctx_uuid('app.tenant_id'));
COMMENT ON TABLE k_ref.share_scope IS 'Phạm vi chia sẻ mở rộng (cấp phép × cây Nút) — RỖNG tới W4.2 (ADR-0029, ADR-0030)';

-- Quyền. Danh sách khớp dbadmin.ModuleGroups() (TestMigration92MatchesModuleGroups); thiếu vai → lỗi.
REVOKE ALL ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope FROM PUBLIC;

DO $$
DECLARE
  groups constant text[] := ARRAY[
    'r_accounts', 'r_apiclient', 'r_content', 'r_cropcycle', 'r_extension', 'r_geo', 'r_kref', 'r_masterdata',
    'r_mechanization', 'r_metering', 'r_metrics', 'r_mrv', 'r_notify', 'r_party', 'r_reports',
    'r_simulation', 'r_sync', 'r_waterway'
  ];
  grp text;
BEGIN
  FOREACH grp IN ARRAY groups LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = grp AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', grp;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA k_ref TO %I', grp);
    EXECUTE format('GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO %I', grp);
    EXECUTE format('GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO %I', grp);
  END LOOP;
END
$$;

GRANT INSERT, UPDATE, DELETE ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_kref;

-- Tìm tenant cho lần dựng lại mọi tenant (pf/kref ProjectedTenants, party/masterdata KRefTenants).
GRANT USAGE ON SCHEMA k_ref, pf_party, pf_masterdata TO ag_outbox_publisher;
GRANT SELECT (tenant_id) ON k_ref.org, k_ref.admin_unit, k_ref.share_grant_active TO ag_outbox_publisher;
GRANT SELECT (tenant_id) ON pf_party.org, pf_party.share_grant, pf_masterdata.admin_unit TO ag_outbox_publisher;
