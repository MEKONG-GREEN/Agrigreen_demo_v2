// GIẢ LẬP giám sát đồng bộ tích hợp (GIS US-SYNC-01/02) — Admin API pf/sync:
//   GET /v1/sync/integrations (tổng hợp theo consumer trong cửa sổ window_hours),
//   GET /v1/sync/integrations/log (nhật ký từng lần xử lý, mới nhất trước, cursor).
// Dữ liệu tổng hợp là SUY RA từ nhật ký giả lập (không có số rời) để bảng và nhật ký luôn khớp nhau.
import type { AdminGet } from "../types";
import { hexId, mulberry32 } from "./data";
import { hasRole, json, pageSlice, problem, validation, type Route } from "./http";

type Integration = AdminGet<"/v1/sync/integrations">["items"][number];
type LogEntry = AdminGet<"/v1/sync/integrations/log">["items"][number];

interface Consumer {
  consumer: string;
  source: string;
  eventType: string;
  /** Số sự kiện mỗi ngày (0 = không có dữ liệu). */
  perDay: number;
  dlqDepth: number | null;
}

export const SYNC_CONSUMERS: readonly Consumer[] = [
  { consumer: "pf.mechanization.area_sync", source: "App HTX → Bản đồ CGH (diện tích vụ)", eventType: "pf.cropcycle.season_area.upserted.v1", perDay: 6, dlqDepth: 1 },
  { consumer: "pf.geo.hub_valve", source: "ERP/MKG → GIS (van công bố kho/Hub)", eventType: "mkg.hub.capacity.published.v1", perDay: 4, dlqDepth: 0 },
  { consumer: "pf.cropcycle.gps_log", source: "App HTX → GIS (nhật ký GPS thửa)", eventType: "pf.cropcycle.gps_log.recorded.v1", perDay: 10, dlqDepth: 0 },
  { consumer: "pf.geo.tms_export", source: "GIS → TMS (bản đồ chuẩn hóa)", eventType: "pf.geo.standard_map.published.v1", perDay: 0, dlqDepth: null },
];

const HOUR = 3_600_000;

function buildLog(): LogEntry[] {
  const r = mulberry32(20260930);
  const now = Date.now();
  const out: LogEntry[] = [];
  let n = 0;
  for (const c of SYNC_CONSUMERS) {
    const total = c.perDay * 7;
    for (let i = 0; i < total; i++) {
      n++;
      // Rải đều 7 ngày qua; lần đầu tiên (i = 0) là mới nhất.
      const at = new Date(now - (i * 24 * HOUR) / c.perDay - Math.floor(r() * 50) * 60_000 - 5 * 60_000).toISOString();
      const areaFail = c.consumer === "pf.mechanization.area_sync" && (i === 0 || i === 3 || i === 17);
      const dead = areaFail && i === 0;
      out.push({
        at,
        consumer: c.consumer,
        source: c.source,
        event_type: c.eventType,
        event_id: hexId(70000 + n, "e7e7"),
        status: areaFail ? "error" : "ok",
        record_count: 1,
        error_code: areaFail ? (dead ? "mechanization.cooperative_invalid" : "db_timeout") : null,
        delivery_count: areaFail ? (dead ? 5 : 2) : 1,
        dead_lettered: dead,
      });
    }
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

const LOG = buildLog();

function summarize(c: Consumer, since: number): Integration {
  const rows = LOG.filter((l) => l.consumer === c.consumer && Date.parse(l.at) >= since);
  const ok = rows.filter((l) => l.status === "ok");
  const failed = rows.filter((l) => l.status === "error");
  const dead = failed.filter((l) => l.dead_lettered);
  const lastOk = ok[0]?.at ?? null;
  const lastFail = failed[0] ?? null;
  const status: Integration["status"] =
    rows.length === 0 ? "idle" : dead.length > 0 || (lastFail && (!lastOk || Date.parse(lastFail.at) > Date.parse(lastOk))) ? "error" : "ok";
  return {
    consumer: c.consumer,
    source: c.source,
    status,
    processed_count: ok.length,
    last_processed_at: lastOk,
    last_event_type: (ok[0] ?? rows[0])?.event_type ?? null,
    failure_count: failed.length,
    dead_lettered_count: dead.length,
    last_failure_at: lastFail?.at ?? null,
    last_error_code: lastFail?.error_code ?? null,
    dlq_depth: c.dlqDepth,
  };
}

const allowed = (p: Parameters<typeof hasRole>[0]) => hasRole(p, "platform_admin", "device_admin");

export const syncMonitorRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/sync\/integrations$/,
    handler: ({ persona, url }) => {
      if (!allowed(persona)) return problem(403, "forbidden", "Forbidden");
      const raw = url.searchParams.get("window_hours");
      const hours = raw === null ? 24 : Number(raw);
      if (!Number.isInteger(hours) || hours < 1 || hours > 720) return validation([{ field: "window_hours", reason: "Cửa sổ thời gian từ 1 đến 720 giờ" }]);
      const consumer = url.searchParams.get("consumer");
      const status = url.searchParams.get("status");
      if (status && !["ok", "error", "idle"].includes(status)) return validation([{ field: "status", reason: "Trạng thái không hợp lệ" }]);
      const since = Date.now() - hours * HOUR;
      const items = SYNC_CONSUMERS.filter((c) => !consumer || c.consumer === consumer)
        .map((c) => summarize(c, since))
        .filter((i) => !status || i.status === status);
      return json({ since: new Date(since).toISOString(), dlq_available: true, items });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/sync\/integrations\/log$/,
    handler: ({ persona, url }) => {
      if (!allowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const status = q("status");
      if (status && status !== "ok" && status !== "error") return validation([{ field: "status", reason: "Trạng thái không hợp lệ" }]);
      const to = q("to") ? Date.parse(q("to")!) : Date.now();
      const from = q("from") ? Date.parse(q("from")!) : to - 7 * 24 * HOUR;
      if (!Number.isFinite(to) || !Number.isFinite(from)) return validation([{ field: q("to") && !Number.isFinite(to) ? "to" : "from", reason: "Thời điểm không hợp lệ (RFC 3339)" }]);
      if (from > to) return validation([{ field: "from", reason: "Từ ngày phải trước đến ngày" }]);
      const consumer = q("consumer");
      const rows = LOG.filter((l) => {
        const t = Date.parse(l.at);
        return t >= from && t <= to && (!consumer || l.consumer === consumer) && (!status || l.status === status);
      });
      const { slice, next } = pageSlice(rows, url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
];
