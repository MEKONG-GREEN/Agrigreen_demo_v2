-- pf_metrics (G3-A5): nhật ký thay đổi lớp GIS, ảnh chụp cuối ngày, phát lại
-- (GIS FN-18 US-EVL-01, FN-19 US-SNAP-01, FN-20 US-REPLAY-01, FN-21 US-RETAIN-01).
--
--   1. layer_event     SỔ chỉ ghi thêm: mỗi thay đổi đối tượng trên một lớp (tạo, sửa,
--                      ngừng dùng, khôi phục) kèm thuộc tính TRƯỚC/SAU và nguồn (event_id,
--                      event_type của module chủ lớp). Idempotent theo source_event_id.
--                      Hình học KHÔNG chép vào đây: thuộc tính mang geometry_ref trỏ dòng
--                      sổ hình học bất biến của pf/geo, tra as-of qua port lúc phát lại.
--   2. layer_snapshot  ảnh chụp cuối ngày D của một lớp, dựng TỪ NHẬT KÝ (recorded_at <
--                      cutoff), kèm watermark (event_id, recorded_at) của dòng cuối đã gập.
--                      Chỉ ghi thêm; một dòng mỗi (lớp, ngày).
--
-- Phát lại D = ảnh chụp mới nhất ≤ D + dòng nhật ký sau watermark và trước cutoff(D).
-- Lưu trữ (FN-21) là CHUYỂN TẦNG, không xóa (RB-1) — xem docs/runbooks/metrics.md.

-- ---------------------------------------------------------------------------
-- 1. Nhật ký lớp (mẫu mục 8.1). stream_key = 'layer:<layer>:<feature_ref>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metrics.layer_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  layer             text        NOT NULL CHECK (layer ~ '^[a-z][a-z0-9_]{1,39}$'),
  feature_ref       uuid        NOT NULL,
  op                text        NOT NULL CHECK (op IN ('create', 'update', 'retire', 'restore')),
  before_attrs      jsonb       NULL,
  after_attrs       jsonb       NULL,
  source_event_id   uuid        NOT NULL,
  source_type       text        NOT NULL,
  source_module     text        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,                -- 'layer:<source_event_id>:<layer>:<feature_ref>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(btrim(correction_reason)) >= 10),
  CHECK (op <> 'create' OR before_attrs IS NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX layer_event_replay  ON pf_metrics.layer_event (tenant_id, layer, recorded_at, id);
CREATE INDEX layer_event_feature ON pf_metrics.layer_event (tenant_id, layer, feature_ref, recorded_at DESC, id DESC);
CREATE INDEX layer_event_source  ON pf_metrics.layer_event (tenant_id, source_event_id);
CREATE INDEX layer_event_idem    ON pf_metrics.layer_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_metrics.layer_event', 'pf_metrics.layer_event');

-- ---------------------------------------------------------------------------
-- 2. Ảnh chụp cuối ngày (chỉ ghi thêm, không chuỗi băm — băm nội dung ở features_sha256)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metrics.layer_snapshot (
  tenant_id             uuid        NOT NULL,
  layer                 text        NOT NULL,
  business_date         date        NOT NULL,
  cutoff                timestamptz NOT NULL,
  watermark_event_id    uuid        NULL,                -- NULL: chưa có dòng nhật ký nào
  watermark_recorded_at timestamptz NULL,
  feature_count         integer     NOT NULL CHECK (feature_count >= 0),
  features              jsonb       NOT NULL CHECK (jsonb_typeof(features) = 'array'),
  features_sha256       bytea       NOT NULL CHECK (length(features_sha256) = 32),
  base_date             date        NULL,                -- ảnh chụp trước đã dùng làm nền (NULL = dựng từ đầu)
  rule_version          text        NOT NULL,
  recorded_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, layer, business_date),
  CHECK ((watermark_event_id IS NULL) = (watermark_recorded_at IS NULL)),
  CHECK (watermark_recorded_at IS NULL OR watermark_recorded_at < cutoff)
  -- "chỉ chụp ngày đã kết thúc" kiểm ở app/ theo shared/clock (không bằng now() của DB).
);
SELECT shared.apply_append_only('pf_metrics.layer_snapshot');

-- ---------------------------------------------------------------------------
-- Quyền (R-05): chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_metrics.layer_event, pf_metrics.layer_snapshot TO metrics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_metrics.layer_event, pf_metrics.layer_snapshot FROM metrics_app;
