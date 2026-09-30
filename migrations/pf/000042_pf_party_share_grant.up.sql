-- pf_party G3: cấp quyền chia sẻ dữ liệu giữa hai tenant (ADR-0020; RB-2, Q-MKG-12).
--
-- Quyết định của chủ sản phẩm (29–30/09/2026): hệ thống nội bộ của MKG (ERP, TMS) KHÔNG dùng
-- chung tenant với nền tảng; chỉ liên thông các dữ liệu cần thiết từ nền tảng qua hệ thống
-- nội bộ. Vì vậy MKG là tenant riêng và mọi luồng chéo tenant đi qua một cấp quyền tường minh:
--
--   share_grant_event     SỔ vòng đời cấp quyền (đề xuất → duyệt → thu hồi), tenant = BÊN CẤP
--                         (tenant chương trình). Người duyệt ≠ người đề xuất (R-56) kiểm ở app/
--                         VÀ bằng CHECK dưới đây.
--   share_grant           bảng chiếu hiện hành ở tenant bên cấp (quản trị liệt kê, kiểm lại khi đọc).
--   share_grant_inbound   bảng chiếu ở tenant BÊN NHẬN (MKG): cho phép bên nhận (tài khoản dịch vụ
--                         mkg_sync, consumer van theo tenant phong bì) tra cấp quyền của chính mình
--                         mà không cần vai BYPASSRLS. Chỉ app/ của pf/party ghi, trong cùng giao dịch
--                         với sổ (tenant.Within), khi duyệt và khi thu hồi.
--   partner_read_log      SỔ mọi lần bên nhận đọc dữ liệu bên cấp qua bề mặt đối tác /v1/partner/*
--                         (mỗi lời gọi một dòng, chỉ ghi thêm), tenant = bên cấp.
--
-- Phạm vi (kinds) là danh sách loại dữ liệu có tên; thêm loại mới = migration mới + ADR-0020.
-- Không có dữ liệu cá nhân (R-04): chỉ *_ref, mã tenant, mục đích và lý do dạng văn bản ngắn.

-- ---------------------------------------------------------------------------
-- 1. Sổ vòng đời cấp quyền (tenant = bên cấp)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.share_grant_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- bên cấp (tenant chương trình)
  stream_key        text        NOT NULL,                 -- 'grant:<grant_id>'
  seq               bigint      NOT NULL,
  grant_id          uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('proposed', 'approved', 'revoked')),
  grantee_tenant_id uuid        NOT NULL,
  kinds             text[]      NOT NULL CHECK (cardinality(kinds) BETWEEN 1 AND 16 AND kinds <@ ARRAY[
                      'cooperative_directory', 'plot_directory', 'season_harvest_dates', 'htx_collection_responses',
                      'valve_hub', 'valve_collection', 'valve_mrv_evidence']::text[]),
  purpose           text        NOT NULL CHECK (length(purpose) BETWEEN 10 AND 500),
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  proposed_by       uuid        NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (grantee_tenant_id <> tenant_id),
  CHECK (valid_to > valid_from),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (action <> 'approved' OR actor_ref <> proposed_by),   -- R-56: không tự duyệt
  CHECK (action <> 'revoked' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX share_grant_event_grant ON pf_party.share_grant_event (tenant_id, grant_id, seq);
SELECT shared.apply_ledger('pf_party.share_grant_event', 'pf_party.share_grant_event');

-- ---------------------------------------------------------------------------
-- 2. Bảng chiếu hiện hành ở tenant bên cấp
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.share_grant (
  tenant_id         uuid        NOT NULL,                 -- bên cấp
  grant_id          uuid        NOT NULL,
  grantee_tenant_id uuid        NOT NULL,
  kinds             text[]      NOT NULL,
  purpose           text        NOT NULL,
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  status            text        NOT NULL CHECK (status IN ('proposed', 'active', 'revoked')),
  proposed_by       uuid        NOT NULL,
  proposed_at       timestamptz NOT NULL,
  approved_by       uuid        NULL,
  approved_at       timestamptz NULL,
  revoked_by        uuid        NULL,
  revoked_at        timestamptz NULL,
  revoke_reason     text        NULL,
  last_event_id     uuid        NOT NULL,
  last_seq          bigint      NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, grant_id),
  CHECK (grantee_tenant_id <> tenant_id),
  CHECK (valid_to > valid_from),
  CHECK (approved_by IS NULL OR approved_by <> proposed_by),  -- R-56
  CHECK (status <> 'active' OR approved_by IS NOT NULL),
  CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
CREATE INDEX share_grant_grantee ON pf_party.share_grant (tenant_id, grantee_tenant_id, status);
SELECT shared.apply_tenant_rls('pf_party.share_grant');

-- ---------------------------------------------------------------------------
-- 3. Bảng chiếu ở tenant bên nhận (chỉ cấp quyền đã duyệt)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.share_grant_inbound (
  tenant_id         uuid        NOT NULL,                 -- bên nhận (MKG)
  grant_id          uuid        NOT NULL,
  grantor_tenant_id uuid        NOT NULL,
  kinds             text[]      NOT NULL,
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  status            text        NOT NULL CHECK (status IN ('active', 'revoked')),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, grant_id),
  CHECK (grantor_tenant_id <> tenant_id),
  CHECK (valid_to > valid_from)
);
CREATE INDEX share_grant_inbound_active ON pf_party.share_grant_inbound (tenant_id, status, grantor_tenant_id);
SELECT shared.apply_tenant_rls('pf_party.share_grant_inbound');

-- ---------------------------------------------------------------------------
-- 4. Sổ đọc của bên nhận (tenant = bên cấp)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.partner_read_log (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- bên cấp (chủ dữ liệu)
  stream_key        text        NOT NULL,                 -- 'grant:<grant_id>'
  seq               bigint      NOT NULL,
  grant_id          uuid        NOT NULL,
  grantee_tenant_id uuid        NOT NULL,
  kind              text        NOT NULL,
  endpoint          text        NOT NULL CHECK (length(endpoint) BETWEEN 1 AND 200),
  query             text        NULL CHECK (query IS NULL OR length(query) <= 500),
  item_count        integer     NOT NULL CHECK (item_count >= 0),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- sub của tài khoản dịch vụ bên nhận
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX partner_read_log_grant ON pf_party.partner_read_log (tenant_id, grant_id, seq);
SELECT shared.apply_ledger('pf_party.partner_read_log', 'pf_party.partner_read_log');

-- ---------------------------------------------------------------------------
-- 5. Quyền của party_app (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_party.share_grant_event, pf_party.partner_read_log TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.share_grant_event, pf_party.partner_read_log FROM party_app;
GRANT SELECT, INSERT, UPDATE ON pf_party.share_grant, pf_party.share_grant_inbound TO party_app;
