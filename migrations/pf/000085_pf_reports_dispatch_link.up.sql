-- Quyết định A4 (chốt 03/10/2026, docs/cau-hoi-ton-dong.md; FN-TI-02.06): email Chi cục vượt giới hạn đính kèm gửi
-- LIÊN KẾT TẢI do api-platform phục vụ (trong lãnh thổ), hạn 7 ngày (tham số reports.dispatch_link_ttl_hours, mặc
-- định 168), có ghi nhật ký truy cập — thay URL ký sẵn 15 phút của kho đối tượng (objstore.MaxPresignTTL).
--
--   1. dispatch_link         mỗi (lần gửi × tệp × người nhận To) một liên kết. Mã thông báo ngẫu nhiên 32 byte CHỈ lưu
--                            dạng băm SHA-256 (token_hash) — DB lộ cũng không dựng lại được liên kết. Bất biến trừ ba cột
--                            thu hồi (quyền UPDATE theo cột cho reports_app): Admin thu hồi → 410.
--   2. dispatch_link_access  SỔ truy cập (apply_ledger, chỉ thêm): ai (mã người nhận trong danh bạ Chi cục, không địa
--                            chỉ email), khi nào, IP đã băm (HMAC với khóa = token_hash, không lưu IP thô), kết quả
--                            served | expired | revoked | invalid | egress_denied. Không dữ liệu cá nhân (R-14).
--   3. dispatch_event.action thêm 'links_revoked' (đọc CHECK hiện hành rồi cộng thêm, mẫu 000072/000083).
--
-- Tải qua liên kết đi qua điểm kiểm soát xuất (export_audit kind 'download' + pf/metering kênh report_export, ADR-0024).

CREATE TABLE pf_reports.dispatch_link (
  tenant_id      uuid        NOT NULL,
  id             uuid        NOT NULL,                     -- UUIDv7, nằm trong mã thông báo
  dispatch_id    uuid        NOT NULL,
  file_id        uuid        NOT NULL,
  recipient_ref  uuid        NOT NULL,                     -- pf_reports.dispatch_recipient.id (người nhận To)
  token_hash     bytea       NOT NULL CHECK (octet_length(token_hash) = 32),
  ttl_hours      integer     NOT NULL CHECK (ttl_hours BETWEEN 1 AND 2160),
  expires_at     timestamptz NOT NULL,
  created_at     timestamptz NOT NULL,
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  revoked_at     timestamptz NULL,
  revoked_by     uuid        NULL,
  revoke_reason  text        NULL CHECK (revoke_reason IS NULL OR char_length(revoke_reason) BETWEEN 10 AND 500),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, dispatch_id) REFERENCES pf_reports.dispatch (tenant_id, id),
  FOREIGN KEY (tenant_id, file_id) REFERENCES pf_reports.dispatch_file (tenant_id, id),
  CHECK (expires_at > created_at),
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revoke_reason IS NULL))
);
CREATE INDEX dispatch_link_dispatch ON pf_reports.dispatch_link (tenant_id, dispatch_id);
SELECT shared.apply_tenant_rls('pf_reports.dispatch_link');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.dispatch_link
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.dispatch_link
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_reports.dispatch_link_access (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dispatch:<dispatch_id>'
  seq               bigint      NOT NULL,
  link_id           uuid        NOT NULL,
  dispatch_id       uuid        NOT NULL,
  file_id           uuid        NOT NULL,
  recipient_ref     uuid        NOT NULL,
  outcome           text        NOT NULL CHECK (outcome IN ('served', 'expired', 'revoked', 'invalid', 'egress_denied')),
  ip_hash           text        NULL CHECK (ip_hash IS NULL OR ip_hash ~ '^[0-9a-f]{64}$'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = recipient_ref (người nhận ngoài hệ thống)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dispatch_link_access_dispatch ON pf_reports.dispatch_link_access (tenant_id, dispatch_id, id);
SELECT shared.apply_ledger('pf_reports.dispatch_link_access', 'pf_reports.dispatch_link_access');

DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(c.oid) INTO def
    FROM pg_constraint c
   WHERE c.conrelid = 'pf_reports.dispatch_event'::regclass AND c.conname = 'dispatch_event_action_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'thiếu ràng buộc pf_reports.dispatch_event.dispatch_event_action_check';
  END IF;
  EXECUTE 'ALTER TABLE pf_reports.dispatch_event DROP CONSTRAINT dispatch_event_action_check';
  EXECUTE format('ALTER TABLE pf_reports.dispatch_event ADD CONSTRAINT dispatch_event_action_check CHECK ((%s) OR action = %L)',
                 substring(def FROM '^CHECK \((.*)\)$'), 'links_revoked');
END
$$;

GRANT SELECT, INSERT ON pf_reports.dispatch_link, pf_reports.dispatch_link_access TO reports_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_reports.dispatch_link, pf_reports.dispatch_link_access FROM reports_app;
GRANT UPDATE (revoked_at, revoked_by, revoke_reason) ON pf_reports.dispatch_link TO reports_app;  -- chỉ cột thu hồi
