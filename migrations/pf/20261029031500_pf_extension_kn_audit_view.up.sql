-- pf/extension (`dom/extension`) — AUDIT-LOG-KN (#73, con của #64/#70): SỔ LƯỢT XEM NHẬT KÝ HỆ THỐNG của KN.
-- Mỗi lần Admin KN xem GET /v1/accounts/audit (một trang, kể cả trang rỗng) ghi đúng một dòng "đã xem" TRƯỚC khi
-- dữ liệu được trả (port.AuditAccess.RecordRead, #70); mỗi lần bị chủ KN từ chối (không phải Admin, đang hỗ trợ, thiếu
-- ngữ cảnh) ghi một dòng "bị từ chối". Đây là sự kiện XEM mới, không sao chép nội dung nhật ký nguồn: chỉ bộ lọc đã
-- chuẩn hóa (mã/ref), băm bộ lọc, danh sách nguồn và VỊ TRÍ các dòng đã trả (recorded_at, nguồn, event_ref).
--
-- kn_audit_view — sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE/TRUNCATE
-- với MỌI vai, kể cả chủ bảng; RLS tenant), chuỗi 'kn_audit_view:<actor_ref>' (người xem).
--
-- LƯU GIỮ (BRD: nhật ký giữ tối thiểu 3 tháng): KHÔNG có chính sách dọn — không hàm purge, không job tách/xóa phân vùng
-- nào nhắc bảng này; trigger trg_immutable chặn xóa dòng. Dòng được giữ vô thời hạn (≥ 3 tháng). Muốn dọn sau này phải là
-- migration có chủ, giữ tối thiểu 3 tháng gần nhất và được PO duyệt (docs/kn/gia-dinh/AUDIT-LOG-KN.md). Kiểm thử
-- knaudit TestAuditViewRetention chứng minh không vai nào xóa/sửa/cắt được dòng.
-- Không dữ liệu cá nhân: chỉ mã tham chiếu (UUID) và mã (code). Vai r_extension: SELECT, INSERT.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.kn_audit_view (
  id                uuid          NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'kn_audit_view:<actor_ref>'
  seq               bigint        NOT NULL,
  request_ref       uuid          NOT NULL,                 -- receipt.RequestRef (một trang = một lượt)
  outcome           text          NOT NULL CHECK (outcome IN ('succeeded', 'denied')),
  denial_code       text          NULL CHECK (denial_code IS NULL OR denial_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  purpose_code      text          NOT NULL CHECK (purpose_code = 'audit_view'),
  session_ref       uuid          NULL,                     -- ngữ cảnh phiên KN của người xem (nếu có)
  filter_hash       text          NULL CHECK (filter_hash IS NULL OR filter_hash ~ '^[0-9a-f]{64}$'),
  filter_from       timestamptz   NULL,
  filter_to         timestamptz   NULL,
  as_of             timestamptz   NULL,
  filter_actor_ref  uuid          NULL,                     -- người bị tra (khác actor_ref = người xem)
  filter_department text          NULL CHECK (filter_department IS NULL OR filter_department ~ '^[a-z][a-z0-9_.-]{0,79}$'),
  filter_module     text          NULL CHECK (filter_module IS NULL OR filter_module ~ '^[a-z][a-z0-9_.-]{0,79}$'),
  filter_action     text          NULL CHECK (filter_action IS NULL OR filter_action ~ '^[a-z][a-z0-9_.-]{0,79}$'),
  page_limit        integer       NULL CHECK (page_limit IS NULL OR page_limit BETWEEN 1 AND 200),
  sources           text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(sources) <= 32)
                      CHECK (array_position(sources, NULL) IS NULL),
  returned_at       timestamptz[] NOT NULL DEFAULT '{}' CHECK (cardinality(returned_at) <= 200)
                      CHECK (array_position(returned_at, NULL) IS NULL),
  returned_sources  text[]        NOT NULL DEFAULT '{}' CHECK (array_position(returned_sources, NULL) IS NULL),
  returned_refs     uuid[]        NOT NULL DEFAULT '{}' CHECK (array_position(returned_refs, NULL) IS NULL),
  event_time        timestamptz   NOT NULL,                 -- receipt.ReadAt
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  actor_ref         uuid          NOT NULL,                 -- người xem
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_audit_view_stream CHECK (stream_key = 'kn_audit_view:' || actor_ref::text),
  CONSTRAINT kn_audit_view_returned CHECK (cardinality(returned_at) = cardinality(returned_sources)
    AND cardinality(returned_at) = cardinality(returned_refs)),
  CONSTRAINT kn_audit_view_succeeded CHECK (outcome <> 'succeeded' OR (denial_code IS NULL AND filter_hash IS NOT NULL
    AND filter_from IS NOT NULL AND filter_to IS NOT NULL AND as_of IS NOT NULL AND page_limit IS NOT NULL
    AND cardinality(sources) >= 1)),
  CONSTRAINT kn_audit_view_denied CHECK (outcome <> 'denied' OR (denial_code IS NOT NULL AND cardinality(returned_at) = 0))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_audit_view_time ON pf_extension.kn_audit_view (tenant_id, recorded_at DESC, id DESC);
CREATE INDEX kn_audit_view_actor ON pf_extension.kn_audit_view (tenant_id, actor_ref, recorded_at DESC);
SELECT shared.apply_ledger('pf_extension.kn_audit_view', 'pf_extension.kn_audit_view');

REVOKE ALL ON pf_extension.kn_audit_view FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.kn_audit_view TO r_extension;
COMMENT ON TABLE pf_extension.kn_audit_view IS
  'AUDIT-LOG-KN (#73): sổ lượt xem nhật ký hệ thống — chỉ ghi thêm, giữ vô thời hạn (≥ 3 tháng), không chính sách dọn';
