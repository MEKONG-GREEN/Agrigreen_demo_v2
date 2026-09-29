-- mkg_hub.valve_export: sổ xuất của van công bố MKG → nền tảng (mục 5, R-03).
--
-- Mỗi dòng là ĐÚNG payload đã đi qua van, sau khi đã kiểm theo hợp đồng trong
-- contracts/publication/<contract>.json (mặc định từ chối; kiểm lúc build bằng
-- tools/contractcheck và lúc chạy bằng contracts/publication.Validate).
--
-- Vận chuyển: cùng giao dịch với dòng này, module ghi một sự kiện vào
-- shared.outbox của cụm mkg với event_id = id của dòng này; outbox-publisher (đã
-- quét mọi cụm, vai ag_outbox_publisher) phát lên RabbitMQ. Không có kết nối DB
-- nào từ pf tới mkg hay ngược lại. Sổ này là bằng chứng kiểm toán "đã công bố gì,
-- khi nào, theo hợp đồng phiên bản nào" (RB-1) — outbox không phải sổ (R-60).
--
-- stream_key = 'valve:<contract>:<subject_ref>': thứ tự theo từng hub, không
-- xếp hàng mọi lần xuất vào một chuỗi.

CREATE TABLE mkg_hub.valve_export (
  id                uuid        NOT NULL,                 -- = event_id trên shared.outbox
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  contract          text        NOT NULL CHECK (contract IN ('hub_facility', 'hub_capacity')),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  event_type        text        NOT NULL CHECK (event_type ~ '^mkg\.[a-z_]+\.[a-z_]+\.[a-z_]+$'),
  schema_version    integer     NOT NULL CHECK (schema_version >= 1),
  subject_ref       uuid        NOT NULL,                 -- hub_ref
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  payload_sha256    bytea       NOT NULL CHECK (length(payload_sha256) = 32),
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX valve_export_idem    ON mkg_hub.valve_export (tenant_id, idem_key);
CREATE INDEX valve_export_stream  ON mkg_hub.valve_export (tenant_id, stream_key, seq);
CREATE INDEX valve_export_subject ON mkg_hub.valve_export (tenant_id, contract, subject_ref, event_time);
CREATE INDEX valve_export_id      ON mkg_hub.valve_export (id);
SELECT shared.apply_ledger('mkg_hub.valve_export', 'mkg_hub.valve_export');

GRANT SELECT, INSERT ON mkg_hub.valve_export TO hub_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_hub.valve_export FROM hub_app;
