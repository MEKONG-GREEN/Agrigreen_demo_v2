-- pf_coop D2 (CEO 13/10/2026 — ADR-0052): NEO HTX THEO MÃ SỐ THUẾ, HTX TẠM, NHẬN QUYỀN, ĐỒNG SỞ HỮU MKG + HTX.
--
-- Dữ liệu nông hộ, xã viên, thửa, vụ, nhật ký do HTX nhập là ĐỒNG SỞ HỮU của MKG và HTX (Khuyến nông là bên thứ ba,
-- đọc qua share_grant có basis_ref = thỏa thuận HTX). Dữ liệu KHÔNG đổi tenant_id (sổ chỉ ghi thêm, RB-1): tenant giữ
-- dòng là tenant LƯU KÝ; chủ sở hữu ghi ở neo dưới đây. Dữ liệu neo vào định danh HTX ổn định (neo), không vào tài khoản.
--
--   htx_anchor_event  SỔ của neo, chuỗi 'anchor:<anchor_id>': provisional_created (KN hoặc MKG lập HTX tạm — có MST
--                     hoặc mã tạm TMP-…), legacy_backfilled (neo cho HTX có trước D2 — do KN lập, coi là HTX tạm),
--                     mst_attached (gắn MST cho neo mã tạm), claimed (HTX thật nhận quyền — MST đã xác minh, tài khoản
--                     HTX đã kích hoạt; details = vết kiểm toán: bên trước/sau, Nút, số dòng chuyển chủ, xung đột).
--   htx_anchor        bảng chiếu (R-21) cập nhật cùng giao dịch: khóa anchor_key (MST chính tắc hoặc mã tạm) DUY NHẤT
--                     trong tenant, mst duy nhất, trạng thái provisional | claimed.
--   htx_anchor_alias  Nút → neo, CHỈ GHI THÊM: Nút tạm và Nút nhận quyền đều là bí danh của neo; một Nút thuộc tối đa
--                     một neo (khóa chính). Bản ghi ngoại tuyến tạo dưới Nút tạm phân giải qua bí danh sau nhận quyền.
--   htx_anchor_claim  một dòng mỗi neo đã nhận quyền, CHỈ GHI THÊM — khóa chính bảo đảm "một lần nhận quyền mỗi neo"
--                     ngay cả khi hai yêu cầu song song (bên thua: 23505 → 409, giao dịch lùi toàn bộ).
--
-- Trạng thái xác nhận (quy tắc 13/10/2026): Nút có neo claimed → "Đã xác nhận"; còn lại (neo provisional, hoặc HTX
-- chưa có neo) → "Chưa xác nhận". Dữ liệu HTX tạm vẫn vào mọi báo cáo, tính vào tổng mặc định; nhận quyền chỉ lật
-- trạng thái, không sao chép dòng nào.
--
-- RLS: p_tenant (apply_tenant_rls / apply_ledger), không p_scope — chỉ quản trị (registry_admin, platform_admin, KN
-- quản trị) đọc/ghi qua app; không có nhánh chia sẻ (không thuộc shares). Không UPDATE/DELETE (R-25). Không PII.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_coop.htx_anchor_event (
  id                  uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id           uuid        NOT NULL,                 -- tenant lưu ký
  stream_key          text        NOT NULL,                 -- 'anchor:<anchor_id>'
  seq                 bigint      NOT NULL,
  anchor_id           uuid        NOT NULL,
  action              text        NOT NULL CHECK (action IN ('provisional_created', 'legacy_backfilled', 'mst_attached', 'claimed')),
  anchor_key          text        NOT NULL CHECK (anchor_key ~ '^([0-9]{10}(-[0-9]{3})?|TMP-[0-9A-F]{12})$'),
  mst                 text        NULL CHECK (mst IS NULL OR mst ~ '^[0-9]{10}(-(00[1-9]|0[1-9][0-9]|[1-9][0-9]{2}))?$'),
  created_by_kind     text        NOT NULL CHECK (created_by_kind IN ('kn', 'mkg')),
  provisional_org_ref uuid        NOT NULL,                 -- Nút HTX tạm (Nút lập neo)
  org_ref             uuid        NOT NULL,                 -- Nút hiện hành của neo SAU sự kiện
  htx_account_ref     uuid        NULL,                     -- tài khoản HTX đã kích hoạt (claimed)
  verification_ref    text        NULL CHECK (verification_ref IS NULL OR char_length(verification_ref) BETWEEN 3 AND 200),
  agreement_ref       text        NULL CHECK (agreement_ref IS NULL OR char_length(agreement_ref) BETWEEN 3 AND 200),
  details             jsonb       NULL CHECK (details IS NULL OR jsonb_typeof(details) = 'object'),
  event_time          timestamptz NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  supersedes_id       uuid        NULL,
  correction_reason   text        NULL,
  actor_ref           uuid        NOT NULL,
  device_id           text        NULL,
  idem_key            text        NOT NULL,
  rule_version        text        NOT NULL,
  prev_hash           bytea       NOT NULL,
  row_hash            bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CONSTRAINT htx_anchor_event_stream CHECK (stream_key = 'anchor:' || anchor_id::text),
  CHECK (action <> 'mst_attached' OR mst IS NOT NULL),
  CHECK (action <> 'claimed' OR (mst IS NOT NULL AND htx_account_ref IS NOT NULL AND verification_ref IS NOT NULL AND details IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX htx_anchor_event_anchor ON pf_coop.htx_anchor_event (tenant_id, anchor_id, seq);
SELECT shared.apply_ledger('pf_coop.htx_anchor_event', 'pf_coop.htx_anchor_event');

CREATE TABLE pf_coop.htx_anchor (
  tenant_id           uuid        NOT NULL,
  anchor_id           uuid        NOT NULL,
  anchor_key          text        NOT NULL,
  mst                 text        NULL,
  status              text        NOT NULL CHECK (status IN ('provisional', 'claimed')),
  created_by_kind     text        NOT NULL CHECK (created_by_kind IN ('kn', 'mkg')),
  created_by          uuid        NOT NULL,                 -- người lập
  provisional_org_ref uuid        NOT NULL,
  org_ref             uuid        NOT NULL,                 -- Nút hiện hành (= provisional_org_ref tới khi nhận quyền sang Nút khác)
  htx_account_ref     uuid        NULL,
  agreement_ref       text        NULL,                     -- thỏa thuận HTX — basis_ref của share_grant cho KN
  claimed_at          timestamptz NULL,
  claimed_by          uuid        NULL,
  last_event_id       uuid        NOT NULL,
  last_seq            bigint      NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, anchor_id),
  UNIQUE (tenant_id, anchor_key),
  UNIQUE (tenant_id, mst),
  UNIQUE (tenant_id, provisional_org_ref),
  CHECK (status <> 'claimed' OR (mst IS NOT NULL AND htx_account_ref IS NOT NULL AND claimed_at IS NOT NULL AND claimed_by IS NOT NULL)),
  CHECK (mst IS NULL OR anchor_key = mst)
);
CREATE INDEX htx_anchor_org ON pf_coop.htx_anchor (tenant_id, org_ref);
CREATE INDEX htx_anchor_status ON pf_coop.htx_anchor (tenant_id, status, anchor_id);
SELECT shared.apply_tenant_rls('pf_coop.htx_anchor');

CREATE TABLE pf_coop.htx_anchor_alias (
  tenant_id  uuid        NOT NULL,
  org_ref    uuid        NOT NULL,
  anchor_id  uuid        NOT NULL,
  role       text        NOT NULL CHECK (role IN ('provisional', 'claimed')),
  event_id   uuid        NOT NULL,                          -- dòng sổ tạo bí danh
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, org_ref),
  FOREIGN KEY (tenant_id, anchor_id) REFERENCES pf_coop.htx_anchor (tenant_id, anchor_id)
);
CREATE INDEX htx_anchor_alias_anchor ON pf_coop.htx_anchor_alias (tenant_id, anchor_id);
SELECT shared.apply_tenant_rls('pf_coop.htx_anchor_alias');

CREATE TABLE pf_coop.htx_anchor_claim (
  tenant_id  uuid        NOT NULL,
  anchor_id  uuid        NOT NULL,
  event_id   uuid        NOT NULL,
  org_ref    uuid        NOT NULL,
  claimed_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, anchor_id),
  FOREIGN KEY (tenant_id, anchor_id) REFERENCES pf_coop.htx_anchor (tenant_id, anchor_id)
);
SELECT shared.apply_tenant_rls('pf_coop.htx_anchor_claim');

REVOKE ALL ON pf_coop.htx_anchor_event, pf_coop.htx_anchor, pf_coop.htx_anchor_alias, pf_coop.htx_anchor_claim FROM PUBLIC;
GRANT SELECT, INSERT ON pf_coop.htx_anchor_event, pf_coop.htx_anchor_alias, pf_coop.htx_anchor_claim TO r_cooperative;
GRANT SELECT, INSERT, UPDATE ON pf_coop.htx_anchor TO r_cooperative;
