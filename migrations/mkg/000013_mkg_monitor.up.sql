-- Cụm mkg — giám sát vận hành xuyên tenant cho sql_exporter (mục 19; docs/runbooks/ingest.md mục 7).
--
-- Bài toán: collector deploy/observability/sql-exporter/mkg-measurement.collector.yml cần số ĐẾM
-- và TUỔI trên mọi tenant, nhưng mọi bảng mkg_measurement bật FORCE RLS (owner cũng bị lọc) và
-- vai giám sát không được thấy dòng nghiệp vụ (khối lượng, lô, khách, người duyệt).
--
-- Cách làm (không có vai đọc bảng gốc):
--   - vai đăng nhập `mkg_monitor` (NOBYPASSRLS, không thuộc ag_shared_user): chỉ USAGE schema
--     mkg_monitor + EXECUTE các hàm dưới. KHÔNG có quyền nào trên mkg_measurement.* (kiểm thử
--     TestMonitorRolesCannotReadBaseTables);
--   - vai NOLOGIN `mkg_monitor_owner` (BYPASSRLS, dbadmin) sở hữu các hàm SECURITY DEFINER và chỉ
--     được SELECT đúng các CỘT vận hành cần đếm (GRANT theo cột). ag_migrator là thành viên để
--     chuyển chủ sở hữu (ALTER FUNCTION … OWNER TO) — không thêm quyền gì ag_migrator chưa có (owner bảng);
--   - hàm chỉ trả số đếm/tuổi gộp theo nhãn vận hành (trạng thái, mức, điểm cân, mã thiết bị), không
--     trả tenant_id, lot_ref, khối lượng hay danh tính. search_path cố định (chống chiếm search_path).
--
-- Số migration: mkg 000013 (người tích hợp cấp cho G3, việc "vai giám sát mkg_monitor").

CREATE SCHEMA mkg_monitor;
REVOKE ALL ON SCHEMA mkg_monitor FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_monitor TO mkg_monitor;
-- Chủ hàm phải có CREATE trên schema của hàm để nhận quyền sở hữu.
GRANT USAGE, CREATE ON SCHEMA mkg_monitor TO mkg_monitor_owner;

-- Chủ hàm: chỉ các cột vận hành.
GRANT USAGE ON SCHEMA mkg_measurement TO mkg_monitor_owner;
GRANT SELECT (device_id, epoch, point, status, last_seen_at, decided_at, proposed_at, buffer_pct)
  ON mkg_measurement.device_current TO mkg_monitor_owner;
GRANT SELECT (commercial_status, recorded_at) ON mkg_measurement.reconciliation TO mkg_monitor_owner;
GRANT SELECT (status, severity, opened_at) ON mkg_measurement.dispute_current TO mkg_monitor_owner;

-- ---------------------------------------------------------------------------
-- Sức khỏe thiết bị: mỗi kỳ đang hiệu lực/chờ duyệt một dòng (nhãn vận hành, không tenant).
CREATE FUNCTION mkg_monitor.device_health()
RETURNS TABLE (device_id text, epoch text, point text, status text, last_seen_ts double precision, buffer_pct double precision)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT d.device_id, d.epoch::text, d.point, d.status,
         extract(epoch FROM coalesce(d.last_seen_at, d.decided_at, d.proposed_at))::float8,
         d.buffer_pct::float8
  FROM mkg_measurement.device_current d
  WHERE d.status IN ('active', 'pending')
$$;

-- Đối soát 24 giờ qua theo trạng thái thương mại (§19: tỉ lệ phiếu vượt dung sai).
CREATE FUNCTION mkg_monitor.reconciliations_24h()
RETURNS TABLE (commercial_status text, n double precision)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT r.commercial_status, count(*)::float8
  FROM mkg_measurement.reconciliation r
  WHERE r.recorded_at > now() - interval '24 hours'
  GROUP BY r.commercial_status
$$;

-- Tranh chấp đang mở / đang xem xét theo mức.
CREATE FUNCTION mkg_monitor.disputes_open()
RETURNS TABLE (severity text, n double precision)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT d.severity, count(*)::float8
  FROM mkg_measurement.dispute_current d
  WHERE d.status IN ('open', 'under_review')
  GROUP BY d.severity
$$;

-- Tuổi tranh chấp đang mở lâu nhất: thời điểm mở (unix), 0 = không có.
CREATE FUNCTION mkg_monitor.dispute_oldest_open_ts()
RETURNS double precision
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT coalesce(extract(epoch FROM min(d.opened_at)), 0)::float8
  FROM mkg_measurement.dispute_current d
  WHERE d.status IN ('open', 'under_review')
$$;

ALTER FUNCTION mkg_monitor.device_health() OWNER TO mkg_monitor_owner;
ALTER FUNCTION mkg_monitor.reconciliations_24h() OWNER TO mkg_monitor_owner;
ALTER FUNCTION mkg_monitor.disputes_open() OWNER TO mkg_monitor_owner;
ALTER FUNCTION mkg_monitor.dispute_oldest_open_ts() OWNER TO mkg_monitor_owner;

REVOKE ALL ON FUNCTION mkg_monitor.device_health(), mkg_monitor.reconciliations_24h(), mkg_monitor.disputes_open(),
  mkg_monitor.dispute_oldest_open_ts() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION mkg_monitor.device_health(), mkg_monitor.reconciliations_24h(), mkg_monitor.disputes_open(),
  mkg_monitor.dispute_oldest_open_ts() TO mkg_monitor;
