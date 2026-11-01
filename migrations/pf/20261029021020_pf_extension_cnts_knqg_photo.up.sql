-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.53b (#158), BRD CNTS v3.7 UR-TINH-21 "kiểm tra thực
-- địa (kèm ảnh)": ẢNH THỰC ĐỊA của báo cáo kiểm tra Mẫu 03 (pf_extension.cnts_knqg_doc, W11.53). KN không ghi dữ liệu HTX
-- (ADR-0055, Q-KNS-04).
--
-- cnts_knqg_photo — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'knqgphoto:<doc_ref>': mỗi dòng là một sự việc trên một
-- ảnh của biểu: added (tải lên — khóa kho đối tượng + SHA-256, tên, kiểu, cỡ, chú thích) hoặc removed (gỡ khỏi báo cáo;
-- tệp vẫn ở kho, dòng added cũ còn tra được). Ảnh hiện hành = added chưa có removed. Tệp ở kho đối tượng (cùng kho của bản
-- scan đã ký / kho văn bản phòng — W11.54), ghi TRƯỚC giao dịch; sổ chỉ giữ khóa và băm.
-- Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.cnts_knqg_photo (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của dòng sổ
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'knqgphoto:<doc_ref>'
  seq                 bigint        NOT NULL,
  doc_ref             uuid          NOT NULL,             -- biểu Mẫu 03 (cnts_knqg_doc.doc_ref)
  photo_ref           uuid          NOT NULL,             -- định danh bền của ảnh trong biểu
  event               text          NOT NULL CHECK (event IN ('added', 'removed')),
  object_key          text          NOT NULL CHECK (char_length(object_key) BETWEEN 1 AND 300),
  file_name           text          NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  content_type        text          NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
  size_bytes          bigint        NOT NULL CHECK (size_bytes BETWEEN 1 AND 5242880),
  sha256              text          NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  caption             text          NULL CHECK (caption IS NULL OR char_length(caption) BETWEEN 1 AND 300),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,                 -- chế độ hỗ trợ của Admin
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_knqg_photo_stream CHECK (stream_key = 'knqgphoto:' || doc_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_knqg_photo_doc ON pf_extension.cnts_knqg_photo (tenant_id, doc_ref, seq);
CREATE INDEX cnts_knqg_photo_idem ON pf_extension.cnts_knqg_photo (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_extension.cnts_knqg_photo', 'pf_extension.cnts_knqg_photo');

REVOKE ALL ON pf_extension.cnts_knqg_photo FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_knqg_photo TO r_extension;
