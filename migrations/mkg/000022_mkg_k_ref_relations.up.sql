-- Schema k_ref của cụm mkg (W2.5, ADR-0029 §1, §3): CÙNG HÌNH với cụm pf (pf 000091 hàm, pf 000092 quan hệ), chủ
-- là module lõi pf/kref (zones [pf, mkg]).
--
-- Kênh nguồn của k_ref ở cụm mkg (chốt W2.5, ADR-0029 §3): KHÔNG kết nối DB chéo cụm, KHÔNG import chéo vùng
-- (ADR-0008, R-03). Nguồn là cụm pf, đi qua kênh đã được phép cho vùng nội bộ — bề mặt đối tác có cấp quyền
-- (ADR-0020), về sau là cổng tích hợp (ADR-0033); bên ghi là pf/kref chạy trong cmd/worker-internal bằng vai
-- r_kref. Chưa có chính sách RLS nào của cụm mkg đọc k_ref, nên bảng để RỖNG cho tới gói nối kênh (W7.4 đồng bộ
-- k_ref xuyên cụm) — không có bên ghi nào là thành viên r_kref ở cụm này.
--
-- Không PII. RLS theo tenant như cụm pf. Quyền: vai module mkg/* (chưa có r_* ở cụm mkg — W2.6 mkg 000023 chép
-- sang r_*) SELECT + EXECUTE hàm ngữ cảnh; chỉ r_kref ghi.

CREATE SCHEMA IF NOT EXISTS k_ref;

CREATE FUNCTION k_ref.ctx_reject(k text) RETURNS text
LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
BEGIN
  RAISE EXCEPTION 'k_ref.ctx_*: chỉ đọc biến ngữ cảnh app.* (nhận %)', coalesce(quote_literal(k), 'NULL')
    USING ERRCODE = '22023';
END
$$;

CREATE FUNCTION k_ref.ctx_text(k text) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT CASE WHEN k ~ '^app\.[a-z][a-z0-9_]*$'
              THEN nullif(current_setting(k, true), '')
              ELSE k_ref.ctx_reject(k) END
$$;

CREATE FUNCTION k_ref.ctx_uuid(k text) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT CASE WHEN k_ref.ctx_text(k) ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
              THEN k_ref.ctx_text(k)::uuid END
$$;

COMMENT ON FUNCTION k_ref.ctx_text(text) IS 'Biến ngữ cảnh app.* của giao dịch; rỗng/thiếu → NULL (DB-13, W2.5 cụm mkg)';
COMMENT ON FUNCTION k_ref.ctx_uuid(text) IS 'Biến ngữ cảnh app.* dạng uuid; rỗng/thiếu/sai dạng → NULL (DB-13, W2.5 cụm mkg)';
REVOKE ALL ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) FROM PUBLIC;

CREATE TABLE k_ref.org (
  tenant_id       uuid        NOT NULL,
  ref             uuid        NOT NULL,
  kind            text        NOT NULL CHECK (length(kind) BETWEEN 1 AND 40),
  code            text        NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
  admin_unit_code text        NULL,
  parent_ref      uuid        NULL,
  active          boolean     NOT NULL,
  source_seq      bigint      NOT NULL CHECK (source_seq >= 0),
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
  valid_to        date        NULL,
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
  tenant_id         uuid        NOT NULL,
  grant_id          uuid        NOT NULL,
  grantee_tenant_id uuid        NOT NULL,
  kinds             text[]      NOT NULL,
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  source_seq        bigint      NOT NULL CHECK (source_seq >= 0),
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

REVOKE ALL ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope FROM PUBLIC;

DO $$
DECLARE
  readers constant text[] := ARRAY[
    'acctbridge_app', 'costing_app', 'fieldops_app', 'hub_app', 'logistics_app', 'measurement_app', 'orders_app', 'r_kref'
  ];
  r text;
BEGIN
  FOREACH r IN ARRAY readers LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      RAISE EXCEPTION 'thiếu vai % — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', r;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA k_ref TO %I', r);
    EXECUTE format('GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO %I', r);
    EXECUTE format('GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO %I', r);
  END LOOP;
END
$$;

GRANT INSERT, UPDATE, DELETE ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_kref;
