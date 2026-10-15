import type { AppGet } from "../types";
import { COMMUNES, PROVINCES, TASKS } from "./data";
import { hasRole, inScope, json, paginate, type Route } from "./http";
import { communeKpi, resolveUnit, sumKpi } from "./ops";
import { type MockPersona } from "./personas";

type ExtDashboardBody = AppGet<"/v1/extension/dashboard">;

function extensionDashboard(p: MockPersona, url: URL): ExtDashboardBody | Response {
  const period = url.searchParams.get("period") ?? new Date().toISOString().slice(0, 7);
  const asOf = new Date(Date.now() - 40 * 60_000).toISOString();
  const r = resolveUnit(p, url.searchParams.get("unit"));
  if (r instanceof Response) return r;
  if (r.commune) {
    return { unit: r.commune.code, level: "commune", period, as_of: asOf, total: communeKpi(r.commune.code), period_closed: false, closed_provinces: [], children: [] };
  }
  const children = r.province
    ? COMMUNES.filter((c) => c.province_code === r.province!.code).map((c) => ({ unit: c.code, kpi: communeKpi(c.code) }))
    : PROVINCES.map((pr) => ({ unit: pr.code, kpi: sumKpi(COMMUNES.filter((c) => c.province_code === pr.code).map((c) => communeKpi(c.code))) }));
  return {
    unit: r.province?.code ?? null,
    level: r.province ? "province" : "national",
    period,
    as_of: asOf,
    total: sumKpi(children.map((c) => c.kpi)),
    period_closed: false,
    closed_provinces: ["92"],
    children,
  };
}

export const appRoutes28: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/extension\/dashboard$/,
    handler: ({ persona, url }) => {
      const d = extensionDashboard(persona, url);
      return d instanceof Response ? d : json(d);
    },
  }];

export const appRoutes29: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/extension\/tasks$/,
    handler: ({ url, persona }) => {
      const status = url.searchParams.get("status");
      const priority = url.searchParams.get("priority");
      const overdue = url.searchParams.get("overdue") === "true";
      // Quản lý HTX (không kiêm vai KN) chỉ thấy nhiệm vụ của HTX mình — như pf/extension.
      const coopOnly = hasRole(persona, "coop_manager") && !hasRole(persona, "kn_officer", "kn_province_admin", "platform_admin");
      const list = TASKS.filter(
        (t) =>
          inScope(persona, t.province_code, t.commune_code) &&
          (!coopOnly || t.cooperative_ref === persona.me.org?.ref) &&
          (!status || t.status === status) &&
          (!priority || t.priority === priority) &&
          (!overdue || (t.overdue && t.status !== "resolved")),
      ).sort((a, b) => Date.parse(a.due_at) - Date.parse(b.due_at));
      return json(paginate(list, url));
    },
  }];
