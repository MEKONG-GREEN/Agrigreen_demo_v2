// GIẢ LẬP độ tươi dữ liệu diện tích vụ App HTX → Bản đồ CGH (CGH FN-07 B5, BR-04, AC-02/AC-03):
//   GET /v1/mechanization/area-sync-status (cgh_viewer, cgh_admin, platform_admin),
//   GET /v1/mechanization/area-sync-log (cgh_admin, platform_admin; vai khác 403).
// Minh hoạ tình huống feed QUÁ HẠN: lần nhận App HTX gần nhất cách đây 3 ngày, ngưỡng 48 giờ → banner
// "đang dùng dữ liệu nhập tay dự phòng".
import type { AppGet } from "../types";
import { hexId, mulberry32 } from "./data";
import { hasRole, json, pageSlice, problem, validation, type Route } from "./http";
import { RUNS } from "./mechanization";

type SyncLog = AppGet<"/v1/mechanization/area-sync-log">["items"][number];
type SyncCoop = AppGet<"/v1/mechanization/area-sync-status">["items"][number];

export const AREA_FEED_THRESHOLD_HOURS = "48";
const HOUR = 3_600_000;
const SOURCE_LABEL = { app_htx: "App HTX", manual: "Nhập tay" } as const;

const fmt = new Intl.DateTimeFormat("vi-VN", { timeZone: "Asia/Ho_Chi_Minh", hour: "2-digit", minute: "2-digit", day: "2-digit", month: "2-digit", year: "numeric" });

function buildLog(): SyncLog[] {
  const r = mulberry32(7072026);
  const now = Date.now();
  const out: SyncLog[] = [];
  let n = 0;
  for (const run of RUNS) {
    const { season_code, season_year } = run.run;
    // Mùa mới nhất: lần App HTX gần nhất cách đây 3 ngày (quá ngưỡng 48 giờ).
    const base = run === RUNS[0] ? now - 72 * HOUR : Date.parse(run.run.computed_at) - 5 * 24 * HOUR;
    run.results.slice(0, 18).forEach((res, idx) => {
      const coop = res.coop.api;
      const area = coop.area_ha ?? "0";
      const entries: { source: "app_htx" | "manual"; outcome: SyncLog["outcome"]; hoursBefore: number }[] = [{ source: "app_htx", outcome: "applied", hoursBefore: idx * 3 }];
      if (idx % 4 === 1) entries.push({ source: "manual", outcome: "applied", hoursBefore: idx * 3 + 30 });
      if (idx % 5 === 2) entries.push({ source: "app_htx", outcome: "kept_locked", hoursBefore: idx * 3 + 12 });
      if (idx % 6 === 3) entries.push({ source: "app_htx", outcome: "stale_ignored", hoursBefore: idx * 3 + 6 });
      for (const e of entries) {
        n++;
        const sourceAt = base - e.hoursBefore * HOUR - Math.floor(r() * 40) * 60_000;
        out.push({
          id: hexId(80000 + n, "a5a5"),
          cooperative_ref: coop.ref,
          season_code,
          season_year,
          source: e.source,
          source_label: SOURCE_LABEL[e.source],
          outcome: e.outcome,
          area_ha: area,
          source_at: new Date(sourceAt).toISOString(),
          event_id: e.source === "app_htx" ? hexId(90000 + n, "e5e5") : null,
          recorded_at: new Date(sourceAt + 2 * 60_000).toISOString(),
        });
      }
    });
  }
  return out.sort((a, b) => Date.parse(b.recorded_at) - Date.parse(a.recorded_at));
}

const LOG = buildLog();

export function areaSyncStatus(seasonCode: string, seasonYear: number, now = Date.now()): AppGet<"/v1/mechanization/area-sync-status"> {
  const rows = LOG.filter((l) => l.season_code === seasonCode && l.season_year === seasonYear);
  const threshold = Number(AREA_FEED_THRESHOLD_HOURS) * HOUR;
  const lastApp = rows.filter((l) => l.source === "app_htx").reduce<string | null>((m, l) => (!m || Date.parse(l.source_at) > Date.parse(m) ? l.source_at : m), null);
  const stale = !!lastApp && now - Date.parse(lastApp) > threshold;
  const byCoop = new Map<string, SyncCoop>();
  for (const l of rows) {
    const cur = byCoop.get(l.cooperative_ref);
    if (!cur) {
      const lastAppCoop = rows.filter((x) => x.cooperative_ref === l.cooperative_ref && x.source === "app_htx")[0];
      byCoop.set(l.cooperative_ref, {
        ...l,
        sync_count: rows.filter((x) => x.cooperative_ref === l.cooperative_ref).length,
        stale: !!lastAppCoop && now - Date.parse(lastAppCoop.source_at) > threshold,
      });
    }
  }
  return {
    last_app_htx_at: lastApp,
    threshold_hours: AREA_FEED_THRESHOLD_HOURS,
    stale,
    fallback_manual: stale,
    message: stale ? `Dữ liệu App HTX cập nhật lần cuối lúc ${fmt.format(Date.parse(lastApp!))}, quá ${AREA_FEED_THRESHOLD_HOURS} giờ — đang dùng dữ liệu nhập tay dự phòng.` : null,
    items: Array.from(byCoop.values()),
  };
}

export const areaSyncRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/area-sync-status$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, "cgh_viewer", "cgh_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const code = url.searchParams.get("season_code") ?? "";
      const year = Number(url.searchParams.get("season_year"));
      const errs = [
        ...(code ? [] : [{ field: "season_code", reason: "Bắt buộc" }]),
        ...(Number.isInteger(year) && year > 2000 ? [] : [{ field: "season_year", reason: "Năm vụ không hợp lệ" }]),
      ];
      if (errs.length) return validation(errs);
      return json(areaSyncStatus(code, year));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/area-sync-log$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, "cgh_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const source = q("source");
      if (source && source !== "app_htx" && source !== "manual") return validation([{ field: "source", reason: "Nguồn không hợp lệ" }]);
      const year = q("season_year") ? Number(q("season_year")) : null;
      const rows = LOG.filter(
        (l) =>
          (!q("cooperative_ref") || l.cooperative_ref === q("cooperative_ref")) &&
          (!q("season_code") || l.season_code === q("season_code")) &&
          (year === null || l.season_year === year) &&
          (!source || l.source === source),
      );
      const { slice, next } = pageSlice(rows, url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
];
