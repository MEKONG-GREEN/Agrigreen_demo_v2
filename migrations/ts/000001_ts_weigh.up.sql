-- Cụm ts (database ag_ts) — schema ts_weigh: số đo cân thô từ agent biên
-- (tài liệu kiến trúc mục 13.2, 14; ADR-0017 kho chuỗi thời gian, ADR-0019 định
-- danh thiết bị và đồng hồ). Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
-- Luồng: edge-agent ký một LÔ số đo (dãy bộ đếm đơn điệu liên tiếp) → cmd/ingest
-- kiểm mTLS + chữ ký nguồn + thiết bị đã duyệt (mkg/measurement DeviceRegistry)
-- → MỘT giao dịch: INSERT reading_batch + INSERT raw_reading của lô. Phiếu cân
-- (mkg_measurement.weigh_ticket, cụm mkg) do ingest tích phân sau (R-70) — không
-- có join nào giữa bảng này và bảng nghiệp vụ (R-07: khác database).
--
-- Toàn vẹn: KHÔNG có chuỗi băm theo dòng. Bằng chứng là chữ ký Ed25519 của lô
-- trên dạng chuẩn (contracts/edge/batch.v1.json, internal/mkg/measurement/port/edgewire):
-- reading_batch lưu payload_sha256 + signature + key_id; raw_reading lưu đúng các
-- trường đã ký ở dạng tái dựng được byte-cho-byte → xác minh lại được bất kỳ lúc nào.
--
-- Chỉ ghi thêm (RB-1, R-71): trigger chặn UPDATE/DELETE/TRUNCATE kể cả owner
-- (shared.apply_append_only), vai ứng dụng chỉ SELECT/INSERT. Tầng lạnh = tách
-- phân vùng tháng → xuất nén có ký số vào kho đối tượng TRONG LÃNH THỔ, không xóa
-- trước khi xác minh bản xuất (ADR-0017; cửa sổ kiểm toán C-7 chờ khách).

CREATE SCHEMA ts_weigh;
REVOKE ALL ON SCHEMA ts_weigh FROM PUBLIC;
GRANT USAGE ON SCHEMA ts_weigh TO ingest_ts, ts_reader;

-- received_at do hệ đặt (R-13): chỉ nhận giá trị mặc định now(). Mọi dòng của
-- một lô ghi trong cùng giao dịch nên có cùng received_at với reading_batch.
CREATE FUNCTION ts_weigh.fn_received_now() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.received_at IS DISTINCT FROM now() THEN
    RAISE EXCEPTION 'ts_weigh: received_at do hệ thống ghi, không được truyền vào'
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- ---------------------------------------------------------------------------
-- Lô số đo đã ký — một dòng mỗi lô (không phân vùng: khóa idempotency toàn cục)
-- ---------------------------------------------------------------------------
CREATE TABLE ts_weigh.reading_batch (
  id                uuid        PRIMARY KEY,                 -- UUIDv7 do ingest sinh
  tenant_id         uuid        NOT NULL,                    -- RB-2: tenant của thiết bị (sổ thiết bị đã duyệt)
  device_id         text        NOT NULL CHECK (device_id ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
  epoch             integer     NOT NULL CHECK (epoch >= 1), -- kỳ đăng ký; mất đĩa → kỳ mới, không dùng lại bộ đếm
  counter_from      bigint      NOT NULL CHECK (counter_from >= 1),
  counter_to        bigint      NOT NULL,
  reading_count     integer     NOT NULL,
  key_id            text        NOT NULL CHECK (key_id ~ '^[0-9a-f]{16}$'),  -- 8 byte đầu SHA-256(khóa công khai), hex
  payload_sha256    bytea       NOT NULL CHECK (length(payload_sha256) = 32), -- SHA-256 của dạng chuẩn
  signature         bytea       NOT NULL CHECK (length(signature) = 64),      -- Ed25519 trên dạng chuẩn
  device_time_first timestamptz NOT NULL,                    -- đồng hồ thiết bị: chỉ thông tin, KHÔNG dùng làm thứ tự
  device_time_last  timestamptz NOT NULL,
  ntp_offset_ms     integer     NULL,                        -- NULL = thiết bị báo chưa đồng bộ NTP
  received_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (counter_to >= counter_from),
  CHECK (reading_count::bigint = counter_to - counter_from + 1),
  UNIQUE (device_id, epoch, counter_from)                    -- idempotency (device_id, counter) ở mức lô
);
CREATE INDEX reading_batch_device_recv ON ts_weigh.reading_batch (tenant_id, device_id, received_at);

-- Bộ đếm đơn điệu (ADR-0019): lô mới phải bắt đầu SAU lô cuối của cùng
-- (device_id, epoch). Cùng counter_from → để khóa UNIQUE báo trùng (ingest đọc
-- lại lô cũ, so payload_sha256: trùng = gửi lặp idempotent, khác = báo động).
-- Khoảng trống (counter_from > cuối + 1) được nhận — giám sát báo mất lô.
CREATE FUNCTION ts_weigh.fn_batch_monotonic() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_last bigint;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('ts_weigh.batch:' || NEW.device_id || ':' || NEW.epoch, 0));
  IF EXISTS (SELECT 1 FROM ts_weigh.reading_batch
             WHERE device_id = NEW.device_id AND epoch = NEW.epoch AND counter_from = NEW.counter_from) THEN
    RETURN NEW;
  END IF;
  SELECT counter_to INTO v_last FROM ts_weigh.reading_batch
  WHERE device_id = NEW.device_id AND epoch = NEW.epoch
  ORDER BY counter_from DESC LIMIT 1;
  IF v_last IS NOT NULL AND NEW.counter_from <= v_last THEN
    RAISE EXCEPTION 'ts_weigh: bộ đếm không đơn điệu cho thiết bị % kỳ %: lô bắt đầu % ≤ cuối lô trước %',
      NEW.device_id, NEW.epoch, NEW.counter_from, v_last
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_batch_monotonic BEFORE INSERT ON ts_weigh.reading_batch
  FOR EACH ROW EXECUTE FUNCTION ts_weigh.fn_batch_monotonic();
CREATE TRIGGER trg_received_now BEFORE INSERT ON ts_weigh.reading_batch
  FOR EACH ROW EXECUTE FUNCTION ts_weigh.fn_received_now();
SELECT shared.apply_append_only('ts_weigh.reading_batch');

-- ---------------------------------------------------------------------------
-- Số đo thô — phân vùng tháng theo received_at (giờ hệ, không phải giờ thiết bị)
-- ---------------------------------------------------------------------------
-- Bố cục tương thích Timescale (ADR-0017): cột thời gian phân vùng + khóa thiết
-- bị, không khóa ngoại, không UNIQUE (bảng phân vùng không có UNIQUE toàn cục —
-- ADR-0012; idempotency nằm ở reading_batch trong cùng giao dịch).
CREATE TABLE ts_weigh.raw_reading (
  tenant_id   uuid          NOT NULL,
  batch_id    uuid          NOT NULL,                        -- reading_batch.id (không FK: tách phân vùng ra tầng lạnh không vướng)
  device_id   text          NOT NULL,
  epoch       integer       NOT NULL CHECK (epoch >= 1),
  counter     bigint        NOT NULL CHECK (counter >= 1),
  device_time timestamptz   NOT NULL,                        -- đồng hồ thiết bị (độ phân giải ms) — không dùng làm thứ tự
  value_kg    numeric(14,3) NOT NULL,                        -- R-10; đúng 3 chữ số thập phân như trên dây
  stable      boolean       NOT NULL,                        -- đầu cân báo ổn định
  unit        text          NOT NULL CHECK (unit IN ('kg', 't')),  -- đơn vị đầu cân hiển thị; value_kg luôn là kg
  raw         bytea         NULL CHECK (raw IS NULL OR length(raw) BETWEEN 1 AND 256),  -- khung gốc từ đầu cân (pháp chứng)
  received_at timestamptz   NOT NULL DEFAULT now()
) PARTITION BY RANGE (received_at);
CREATE INDEX raw_reading_device_counter ON ts_weigh.raw_reading (tenant_id, device_id, epoch, counter);
CREATE INDEX raw_reading_batch ON ts_weigh.raw_reading (batch_id);

CREATE TRIGGER trg_received_now BEFORE INSERT ON ts_weigh.raw_reading
  FOR EACH ROW EXECUTE FUNCTION ts_weigh.fn_received_now();
SELECT shared.apply_append_only('ts_weigh.raw_reading');

-- ---------------------------------------------------------------------------
-- Quyền (lớp 1): ingest_ts ghi, ts_reader chỉ đọc. Không UPDATE/DELETE/TRUNCATE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON ts_weigh.reading_batch, ts_weigh.raw_reading TO ingest_ts;
GRANT SELECT ON ts_weigh.reading_batch, ts_weigh.raw_reading TO ts_reader;
REVOKE UPDATE, DELETE, TRUNCATE ON ts_weigh.reading_batch, ts_weigh.raw_reading FROM ingest_ts, ts_reader;
