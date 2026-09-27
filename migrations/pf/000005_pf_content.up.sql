-- pf_content: thư viện quy trình kỹ thuật, tài liệu, tin tức, cảnh báo khẩn (ADR-0011).
--
-- KN FR-12..16 (US-LIB-01..06), HTX FN-06/FN-14 (US-CMS-01/02, US-NEWS-01..03).
--
-- Mẫu:
--   * document_version — sổ chỉ ghi thêm (mục 8.1): mỗi dòng là MỘT phiên bản nội
--     dung đầy đủ (tiêu đề, tóm tắt, thân đã lọc, tệp đính kèm, nhãn). Xuất bản
--     phiên bản mới không bao giờ ghi đè phiên bản cũ (RB-1).
--   * document_event — sổ chỉ ghi thêm cho chuyển trạng thái: gửi duyệt, trả
--     lại, xuất bản, gỡ (kèm người thực hiện và lý do).
--   * document — bảng chiếu trạng thái hiện tại (R-21), cập nhật trong CÙNG giao
--     dịch với sổ; dựng lại được từ hai sổ (R-22). Giữ bản đã xuất bản hiện hành
--     để đọc, tìm kiếm và đồng bộ ngoại tuyến (sync_seq).
--   * attachment — siêu dữ liệu tệp đính kèm đã lưu ở kho đối tượng (khóa + sha256).
--     Chỉ INSERT: tệp ghi một lần, không xóa.
--
-- Tài liệu không phải dữ liệu cá nhân (R-04): không có họ tên, SĐT, CCCD ở đây.
--
-- Tìm kiếm không dấu: unaccent là extension "trusted" nhưng hàm unaccent() chỉ
-- STABLE nên không dùng được trong cột sinh. Chuẩn hóa (bỏ dấu, đ → d, chữ
-- thường) làm ở Go (domain.Normalize), lưu vào search_text; tsvector cấu hình
-- 'simple' sinh từ cột đó. App ngoại tuyến dùng cùng quy tắc.

CREATE SCHEMA pf_content;
REVOKE ALL ON SCHEMA pf_content FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_content TO content_app;

-- ---------------------------------------------------------------------------
-- Sổ phiên bản nội dung. stream_key = 'doc:<document_ref>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_content.document_version (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  document_ref      uuid        NOT NULL,
  version_no        integer     NOT NULL CHECK (version_no >= 1),
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  summary           text        NOT NULL DEFAULT '' CHECK (char_length(summary) <= 2000),
  body              text        NOT NULL CHECK (octet_length(body) <= 524288),   -- đã qua bộ lọc allow-list
  body_format       text        NOT NULL CHECK (body_format IN ('markdown', 'html')),
  category          text        NULL CHECK (category ~ '^[a-z][a-z0-9_]{1,39}$'),
  tags              text[]      NOT NULL DEFAULT '{}',    -- '<danh mục>:<mã>' của pf/masterdata
  attachments       jsonb       NOT NULL DEFAULT '[]',    -- [{id, key, sha256, size, content_type, file_name, role}]
  based_on_version  integer     NULL,                     -- phiên bản làm gốc khi soạn
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- tác giả của phiên bản
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (jsonb_typeof(attachments) = 'array')
) PARTITION BY RANGE (recorded_at);

-- Tính duy nhất của idem_key và (stream_key, seq) do shared.fn_chain bảo đảm (ADR-0012).
CREATE INDEX document_version_idem   ON pf_content.document_version (tenant_id, idem_key);
CREATE INDEX document_version_stream ON pf_content.document_version (tenant_id, stream_key, seq);
CREATE INDEX document_version_doc    ON pf_content.document_version (tenant_id, document_ref, version_no);

SELECT shared.apply_ledger('pf_content.document_version', 'pf_content.document_version');

-- ---------------------------------------------------------------------------
-- Sổ chuyển trạng thái. stream_key = 'doc:<document_ref>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_content.document_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  document_ref      uuid        NOT NULL,
  version_no        integer     NULL CHECK (version_no >= 1),
  action            text        NOT NULL CHECK (action IN ('submitted', 'returned', 'published', 'retired')),
  reason            text        NULL,
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
  CHECK (action = 'retired' OR version_no IS NOT NULL),
  CHECK (action NOT IN ('returned', 'retired') OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX document_event_idem   ON pf_content.document_event (tenant_id, idem_key);
CREATE INDEX document_event_stream ON pf_content.document_event (tenant_id, stream_key, seq);
CREATE INDEX document_event_doc    ON pf_content.document_event (tenant_id, document_ref, seq);

SELECT shared.apply_ledger('pf_content.document_event', 'pf_content.document_event');

-- ---------------------------------------------------------------------------
-- Trạng thái hiện tại (bảng chiếu, R-21).
--   status: draft/review khi CHƯA từng xuất bản; published sau lần xuất bản đầu
--   (kể cả khi đang soạn phiên bản mới — xem working_*); retired = đã gỡ, ẩn.
--   Phạm vi áp dụng cố định theo tài liệu: national (toàn quốc), province (một
--   tỉnh), commune (một hoặc nhiều xã cùng tỉnh).
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_content.document_sync_seq;

CREATE TABLE pf_content.document (
  tenant_id            uuid        NOT NULL,
  ref                  uuid        NOT NULL,
  kind                 text        NOT NULL CHECK (kind IN ('procedure', 'guide', 'news', 'alert', 'supplement')),
  audience_level       text        NOT NULL CHECK (audience_level IN ('national', 'province', 'commune')),
  audience_units       text[]      NOT NULL DEFAULT '{}',
  province_code        text        NULL,                   -- tỉnh sở hữu (province/commune); NULL với toàn quốc
  parent_ref           uuid        NULL,                   -- quy trình gốc TW của bản bổ sung địa phương
  status               text        NOT NULL CHECK (status IN ('draft', 'review', 'published', 'retired')),
  created_by           uuid        NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  last_version_no      integer     NOT NULL CHECK (last_version_no >= 1),
  working_version_no   integer     NULL,
  working_status       text        NULL CHECK (working_status IN ('draft', 'review')),
  published_version_no integer     NULL,
  published_version_id uuid        NULL,
  published_at         timestamptz NULL,
  publisher_ref        uuid        NULL,
  -- Ảnh của phiên bản ĐÃ XUẤT BẢN hiện hành (để liệt kê/tìm kiếm không phải đọc sổ).
  title                text        NULL,
  summary              text        NULL,
  category             text        NULL,
  tags                 text[]      NOT NULL DEFAULT '{}',
  search_text          text        NULL,
  search_tsv           tsvector    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(search_text, ''))) STORED,
  retired_at           timestamptz NULL,
  retire_reason        text        NULL,
  sync_seq             bigint      NULL,                   -- tăng mỗi khi bản hiện hành đổi (xuất bản, gỡ)
  PRIMARY KEY (tenant_id, ref),
  FOREIGN KEY (tenant_id, parent_ref) REFERENCES pf_content.document (tenant_id, ref),
  CHECK ((audience_level = 'national') = (cardinality(audience_units) = 0)),
  CHECK (audience_level <> 'province' OR cardinality(audience_units) = 1),
  CHECK (kind <> 'supplement' OR (parent_ref IS NOT NULL AND audience_level <> 'national')),
  CHECK (kind <> 'procedure' OR audience_level = 'national'),
  CHECK ((working_version_no IS NULL) = (working_status IS NULL)),
  CHECK ((status IN ('published', 'retired')) OR published_version_no IS NULL),
  CHECK (status <> 'retired' OR (retired_at IS NOT NULL AND char_length(btrim(retire_reason)) >= 10))
);
CREATE INDEX document_listing  ON pf_content.document (tenant_id, status, published_at DESC, ref DESC);
CREATE INDEX document_units    ON pf_content.document USING gin (audience_units);
CREATE INDEX document_tags     ON pf_content.document USING gin (tags);
CREATE INDEX document_search   ON pf_content.document USING gin (search_tsv);
CREATE INDEX document_parent   ON pf_content.document (tenant_id, parent_ref);
CREATE UNIQUE INDEX document_sync ON pf_content.document (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_content.document');

-- ---------------------------------------------------------------------------
-- Tệp đính kèm đã tải lên (kho đối tượng ghi một lần). Không phải dữ liệu cá
-- nhân nên lưu bản rõ; vẫn kiểm SHA-256 (objstore.Put).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_content.attachment (
  tenant_id    uuid        NOT NULL,
  id           uuid        NOT NULL,
  document_ref uuid        NOT NULL,
  object_key   text        NOT NULL,
  sha256       text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes   bigint      NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 15728640),
  content_type text        NOT NULL,
  file_name    text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  uploaded_by  uuid        NOT NULL,
  idem_key     text        NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  FOREIGN KEY (tenant_id, document_ref) REFERENCES pf_content.document (tenant_id, ref)
);
CREATE INDEX attachment_doc ON pf_content.attachment (tenant_id, document_ref);
SELECT shared.apply_tenant_rls('pf_content.attachment');

-- ---------------------------------------------------------------------------
-- Quyền của content_app. Sổ: chỉ SELECT, INSERT. Bảng chiếu: không DELETE
-- (gỡ là trạng thái, không xóa cứng). Tệp đính kèm: chỉ ghi thêm.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_content.document_version, pf_content.document_event TO content_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_content.document_version, pf_content.document_event FROM content_app;
GRANT SELECT, INSERT, UPDATE ON pf_content.document TO content_app;
GRANT SELECT, INSERT ON pf_content.attachment TO content_app;
GRANT USAGE ON SEQUENCE pf_content.document_sync_seq TO content_app;
