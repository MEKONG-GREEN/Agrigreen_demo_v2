-- pf_accounts G2: tài khoản nông dân của App HTX (HTX US-ACC-01/02, US-LOGIN-01;
-- quyết định g2-conventions §1).
--
--   farmer_link      : liên kết tài khoản Keycloak (user_ref) ↔ nông hộ (household_ref)
--                      của HTX. Phạm vi của vai farmer tính từ bảng này — tài khoản
--                      nông dân KHÔNG mang org_ref trong token (phạm vi pf/party rỗng,
--                      mặc định chặn), module khác đọc qua accounts/port.FarmerHouseholds.
--   activation_grant : lượt kích hoạt một lần (liên kết action token của Keycloak,
--                      execute-actions UPDATE_PASSWORD) do quản lý HTX / cán bộ KN phát
--                      cho nông dân dưới dạng URL + QR. CHỈ lưu siêu dữ liệu (ai phát,
--                      lúc nào, hạn, đã dùng / đã thu hồi) — KHÔNG có cột token hay
--                      liên kết: liên kết chỉ trả MỘT lần trong phản hồi, không ghi log.
--
-- SĐT (tên đăng nhập) chỉ nằm ở Keycloak và kho định danh (R-04). Tham chiếu sang
-- module khác (household_ref, cooperative_ref) là UUID thuần (R-06).

CREATE TABLE pf_accounts.farmer_link (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,                  -- UUIDv7 (R-11)
  user_ref        uuid        NOT NULL,                  -- id người dùng Keycloak (= sub của token)
  household_ref   uuid        NOT NULL,                  -- pf_party.household (mã kho định danh)
  cooperative_ref uuid        NOT NULL,                  -- HTX của nông hộ lúc liên kết
  commune_code    text        NOT NULL,
  linked_by       uuid        NOT NULL,
  linked_at       timestamptz NOT NULL,                  -- R-13: lúc liên kết
  recorded_at     timestamptz NOT NULL DEFAULT now(),    -- R-13: lúc ghi nhận
  unlinked_at     timestamptz NULL,                      -- gỡ liên kết là trạng thái, không xóa (RB-1)
  unlinked_by     uuid        NULL,
  unlink_reason   text        NULL,
  idem_key        text        NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  CHECK ((unlinked_at IS NULL) = (unlinked_by IS NULL)),
  CHECK ((unlinked_at IS NULL) = (unlink_reason IS NULL)),
  CHECK (unlink_reason IS NULL OR char_length(btrim(unlink_reason)) >= 10)
);
-- Một nông hộ gắn đúng một tài khoản nông dân đang dùng tại một thời điểm.
CREATE UNIQUE INDEX farmer_link_household_open ON pf_accounts.farmer_link (tenant_id, household_ref) WHERE unlinked_at IS NULL;
CREATE INDEX farmer_link_user ON pf_accounts.farmer_link (tenant_id, user_ref) WHERE unlinked_at IS NULL;
CREATE INDEX farmer_link_coop ON pf_accounts.farmer_link (tenant_id, cooperative_ref) WHERE unlinked_at IS NULL;
SELECT shared.apply_tenant_rls('pf_accounts.farmer_link');

CREATE TABLE pf_accounts.activation_grant (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                    -- UUIDv7
  user_ref      uuid        NOT NULL,
  actions       text[]      NOT NULL CHECK (cardinality(actions) BETWEEN 1 AND 3),
  channel       text        NOT NULL CHECK (channel IN ('link_qr')),
  issued_by     uuid        NOT NULL,
  issued_at     timestamptz NOT NULL,
  expires_at    timestamptz NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  used_at       timestamptz NULL,                        -- người dùng đã hoàn tất hành động (đọc từ Keycloak)
  revoked_at    timestamptz NULL,
  revoked_by    uuid        NULL,
  revoke_reason text        NULL CHECK (revoke_reason IN ('reissued', 'account_locked')),
  replaced_by   uuid        NULL,                        -- lượt mới thay lượt này (phát lại)
  idem_key      text        NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  CHECK (expires_at > issued_at),
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)),
  CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL)),
  CHECK (used_at IS NULL OR revoked_at IS NULL)
);
-- Phát lại thu hồi lượt trước: mỗi tài khoản tối đa một lượt còn mở.
CREATE UNIQUE INDEX activation_grant_open ON pf_accounts.activation_grant (tenant_id, user_ref)
  WHERE revoked_at IS NULL AND used_at IS NULL;
CREATE INDEX activation_grant_user ON pf_accounts.activation_grant (tenant_id, user_ref, issued_at DESC);
SELECT shared.apply_tenant_rls('pf_accounts.activation_grant');

-- Quyền của accounts_app: không DELETE; UPDATE chỉ các cột trạng thái.
GRANT SELECT, INSERT ON pf_accounts.farmer_link, pf_accounts.activation_grant TO accounts_app;
GRANT UPDATE (unlinked_at, unlinked_by, unlink_reason) ON pf_accounts.farmer_link TO accounts_app;
GRANT UPDATE (used_at, revoked_at, revoked_by, revoke_reason, replaced_by) ON pf_accounts.activation_grant TO accounts_app;
