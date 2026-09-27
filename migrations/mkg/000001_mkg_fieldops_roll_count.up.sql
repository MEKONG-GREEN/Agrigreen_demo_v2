-- mkg_fieldops.roll_count: bảng sổ tham chiếu đúng như mục 8.1 của tài liệu
-- kiến trúc, dùng để kiểm chứng mẫu sổ ở cụm mkg từ G0. Thiết kế chi tiết
-- mkg/fieldops thuộc G2; bảng này có thể được mở rộng bằng migration cộng thêm.

CREATE SCHEMA mkg_fieldops;
REVOKE ALL ON SCHEMA mkg_fieldops FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_fieldops TO fieldops_app;

CREATE TABLE mkg_fieldops.roll_count (
  id                   uuid        NOT NULL,
  tenant_id            uuid        NOT NULL,
  stream_key           text        NOT NULL,              -- 'lot:<uuid>'
  seq                  bigint      NOT NULL,
  plot_ref             uuid        NOT NULL,
  htx_ref              uuid        NOT NULL,
  rolls                integer     NOT NULL CHECK (rolls >= 0),
  htx_confirmation_ref uuid        NULL,                  -- mục 13.3: chưa bắt buộc ở ĐTK 1
  event_time           timestamptz NOT NULL,
  recorded_at          timestamptz NOT NULL DEFAULT now(),
  supersedes_id        uuid        NULL,
  correction_reason    text        NULL,
  actor_ref            uuid        NOT NULL,
  device_id            text        NULL,
  idem_key             text        NOT NULL,
  rule_version         text        NOT NULL,
  prev_hash            bytea       NOT NULL,
  row_hash             bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX roll_count_idem   ON mkg_fieldops.roll_count (tenant_id, idem_key);
CREATE INDEX roll_count_stream ON mkg_fieldops.roll_count (tenant_id, stream_key, seq);

SELECT shared.apply_ledger('mkg_fieldops.roll_count', 'mkg_fieldops.roll_count');

GRANT SELECT, INSERT ON mkg_fieldops.roll_count TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.roll_count FROM fieldops_app;
