-- Loại sự kiện trên outbox (ADR-0018, Q-MKG-01) cho MỌI database.
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
--   platform  phát lên exchange ag.events của vhost nền tảng — mọi consumer nền
--             tảng bind được, nên từ vùng mkg chỉ được là payload van công bố.
--   internal  sự kiện NỘI BỘ MKG: outbox-publisher phát lên vhost RabbitMQ riêng
--             mkg.internal mà tài khoản của nền tảng không có quyền (không bind,
--             không đọc). Chỉ tên mkg.* (CHECK dưới đây); contractcheck bắt payload
--             //ag:internal-event không bao giờ đi với kind platform.
--
-- Dòng cũ nhận mặc định 'platform' (đúng nghĩa trước G3).
ALTER TABLE shared.outbox ADD COLUMN kind text NOT NULL DEFAULT 'platform'
  CHECK (kind IN ('platform', 'internal'));
ALTER TABLE shared.outbox ADD CONSTRAINT outbox_internal_is_mkg
  CHECK (kind = 'platform' OR event_type LIKE 'mkg.%');

-- Publisher quét theo loại: mỗi loại một kết nối broker (vhost) riêng.
CREATE INDEX outbox_unpublished_kind ON shared.outbox (kind, id) WHERE published_at IS NULL;
