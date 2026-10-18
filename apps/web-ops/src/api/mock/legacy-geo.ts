import type { PlotFeature } from "../types";
import { uuidv7 } from "../idempotency";
import { boundaryOf, COOPERATIVES, findCoop, plotsOf } from "./data";
import { inScope, json, omit, pageSlice, problem, type Route } from "./http";
import { MISSING_BOUNDARY } from "./ops";

const PLOT_STATE = new Map<string, Pick<PlotFeature["properties"], "cooperative_ref" | "status" | "needs_review">>();

function plotsWithState(coop: (typeof COOPERATIVES)[number]): PlotFeature[] {
  return plotsOf(coop).map((p) => {
    const s = PLOT_STATE.get(p.properties.ref);
    return s ? { ...p, properties: { ...p.properties, ...s, revision: p.properties.revision + 1 } } : p;
  });
}

function findPlot(ref: string | undefined): { plot: PlotFeature; index: number; coop: (typeof COOPERATIVES)[number] } | undefined {
  for (const coop of COOPERATIVES) {
    const list = plotsWithState(coop);
    const index = list.findIndex((p) => p.properties.ref === ref);
    if (index >= 0) return { plot: list[index]!, index, coop };
  }
  return undefined;
}

export const appRoutes38: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/plots$/,
    handler: ({ params, url, persona }) => {
      const c = findCoop(params[0]);
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(403, "geo.out_of_scope", "Out of scope");
      const own = plotsWithState(c).filter((p) => p.properties.cooperative_ref === c.api.ref);
      const { slice, next } = pageSlice(own, url, 200, 1000);
      return json({ type: "FeatureCollection", features: slice, next_cursor: next });
    },
  }];

export const appRoutes39: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/plots$/,
    handler: ({ url, persona }) => {
      const parts = (url.searchParams.get("bbox") ?? "").split(",").map(Number);
      if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n))) {
        return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "bbox", reason: "Cần minLon,minLat,maxLon,maxLat (WGS84)" }] });
      }
      const [minLon, minLat, maxLon, maxLat] = parts as [number, number, number, number];
      const limit = Math.min(Number(url.searchParams.get("limit") ?? 1000) || 1000, 1000);
      const inside = COOPERATIVES.filter((c) => inScope(persona, c.province_code, c.api.admin_unit_code))
        .flatMap((c) => plotsWithState(c))
        .filter((p) => {
          if (p.properties.status === "retired") return false;
          const pt = (p.geometry.coordinates[0] as number[][] | undefined)?.[0];
          return !!pt && pt[0]! >= minLon && pt[0]! <= maxLon && pt[1]! >= minLat && pt[1]! <= maxLat;
        });
      return json({ type: "FeatureCollection", features: inside.slice(0, limit), truncated: inside.length > limit, limit });
    },
  }];

export const appRoutes40: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/geo\/admin-boundaries\/([0-9A-Z.-]+)$/,
    handler: ({ params }) => {
      const f = params[0] === MISSING_BOUNDARY ? null : boundaryOf(params[0] ?? "");
      if (!f) return problem(404, "geo.admin_boundary_not_found", "Boundary not imported");
      // Route theo mã không có parent_code trong hợp đồng.
      return json({ ...f, properties: omit(f.properties, "parent_code") });
    },
  }];

export const appRoutes42: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/plots\/([\w-]+)\/assign$/,
    handler: ({ params, body, persona }) => {
      const found = findPlot(params[0]);
      if (!found || !inScope(persona, found.coop.province_code, found.coop.api.admin_unit_code)) return problem(404, "geo.plot_not_found", "Not found");
      const { plot, index } = found;
      if (plot.properties.status === "retired") return problem(409, "geo.plot_state_conflict", "Retired");
      const input = (body ?? {}) as { cooperative_ref?: string | null; reason?: string; confirm_overlap?: boolean };
      const target = input.cooperative_ref ?? null;
      if (target === plot.properties.cooperative_ref) return problem(409, "geo.plot_state_conflict", "No change");
      const overlaps = [{ plot_ref: plot.properties.ref, code: `${plot.properties.code.slice(0, -1)}9`, cooperative_ref: target, intersect_ha: "0.0420", overlap_pct: "3.10" }];
      // Minh hoạ: 1/3 thửa nháp chồng lấn HTX khác (không bỏ qua được), còn lại cần xác nhận.
      if (target && index % 3 === 0) return problem(409, "geo.overlap_cross_coop", "Cross-coop overlap", { overlaps });
      if (target && !input.confirm_overlap) return problem(409, "geo.overlap_needs_confirm", "Needs confirm", { needs_confirm: true, overlaps });
      const now = new Date().toISOString();
      PLOT_STATE.set(plot.properties.ref, { cooperative_ref: target, status: target ? "active" : "draft", needs_review: !!input.confirm_overlap });
      const updated = findPlot(params[0])!.plot;
      return json({
        plot: updated,
        conflicts: input.confirm_overlap
          ? [{
              id: uuidv7(), kind: "same_coop_overlap", plot_ref: updated.properties.ref, boundary_id: updated.properties.boundary_id, other_plot_refs: [],
              max_overlap_pct: "3.10", commune_code: updated.properties.commune_code, owner_ref: target, due_at: new Date(Date.now() + 72 * 3_600_000).toISOString(),
              status: "open", detected_at: now, resolved_by: null, resolved_at: null, resolution_note: null,
            }]
          : [],
        event: { id: uuidv7(), action: "assigned", seq: updated.properties.revision, event_time: now, reason: input.reason ?? null, actor_ref: persona.me.actor_ref },
      });
    },
  }];
