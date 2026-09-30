-- Cụm ts — giám sát vận hành xuyên tenant cho sql_exporter (mục 19; ADR-0017). ts 000002 để dành ts_env.
--
-- Cùng mẫu với mkg 000013: vai đăng nhập `ts_monitor` (NOBYPASSRLS) chỉ EXECUTE hàm trong schema
-- ts_monitor; vai NOLOGIN `ts_monitor_owner` (BYPASSRLS) sở hữu hàm SECURITY DEFINER và chỉ được
-- SELECT các cột vận hành của ts_weigh.reading_batch. Không có quyền nào của ts_monitor trên
-- ts_weigh.* — số đo thô (value_kg, raw) và chữ ký không bao giờ lộ qua giám sát. Không tham chiếu
-- schema pf_*/mkg_*/vault (R-07).

CREATE SCHEMA ts_monitor;
REVOKE ALL ON SCHEMA ts_monitor FROM PUBLIC;
GRANT USAGE ON SCHEMA ts_monitor TO ts_monitor;
GRANT USAGE, CREATE ON SCHEMA ts_monitor TO ts_monitor_owner;

GRANT USAGE ON SCHEMA ts_weigh TO ts_monitor_owner;
GRANT SELECT (device_id, epoch, received_at) ON ts_weigh.reading_batch TO ts_monitor_owner;

-- Lô gần nhất theo thiết bị/kỳ trong 7 ngày (kiểm chéo với device_current.last_seen_at của cụm mkg).
CREATE FUNCTION ts_monitor.device_last_batch()
RETURNS TABLE (device_id text, epoch text, last_batch_ts double precision)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT b.device_id, b.epoch::text, extract(epoch FROM max(b.received_at))::float8
  FROM ts_weigh.reading_batch b
  WHERE b.received_at > now() - interval '7 days'
  GROUP BY b.device_id, b.epoch
$$;

-- Số lô nhận trong 1 giờ qua (toàn cụm) — phát hiện ingest ngừng ghi.
CREATE FUNCTION ts_monitor.batches_1h()
RETURNS double precision
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT count(*)::float8 FROM ts_weigh.reading_batch b WHERE b.received_at > now() - interval '1 hour'
$$;

ALTER FUNCTION ts_monitor.device_last_batch() OWNER TO ts_monitor_owner;
ALTER FUNCTION ts_monitor.batches_1h() OWNER TO ts_monitor_owner;

REVOKE ALL ON FUNCTION ts_monitor.device_last_batch(), ts_monitor.batches_1h() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION ts_monitor.device_last_batch(), ts_monitor.batches_1h() TO ts_monitor;
