import { hasRole, json, problem, type Route } from "./http";
import { coverage as mechCoverage, dashboard as mechDashboard, findRun, panel as mechPanel, RUNS } from "./mechanization";
import { type MockPersona } from "./personas";

const cghAllowed = (p: MockPersona) => p.me.org?.kind === "dcrd" || hasRole(p, "platform_admin", "cgh_viewer", "cgh_admin");

export const appRoutes43: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/mechanization\/balance-runs$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const code = url.searchParams.get("season_code");
      const year = Number(url.searchParams.get("season_year")) || null;
      return json({ items: RUNS.map((r) => r.run).filter((r) => (!code || r.season_code === code) && (!year || r.season_year === year)) });
    },
  }];

export const appRoutes44: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/mechanization\/coverage$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null });
      if (!stored) return problem(404, "mechanization.run_not_found", "Run not found");
      return json(mechCoverage(stored, { level: q("level") ?? "province", province_code: q("province_code"), commune_code: q("commune_code"), farm_stage: q("farm_stage") }));
    },
  }];

export const appRoutes45: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/mechanization\/cooperatives\/([\w-]+)\/panel$/,
    handler: ({ url, persona, params }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null }) ?? RUNS[0]!;
      const p = mechPanel(stored, params[0] ?? "");
      return p ? json(p) : problem(422, "mechanization.cooperative_invalid", "Cooperative invalid");
    },
  }];

export const appRoutes46: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/mechanization\/dashboard$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null });
      if (!stored) return json({ kpis: { total_machines: 0, total_cooperatives: 0, cooperatives_sufficient: 0, cooperatives_shortage: 0 }, cooperatives_by_status: {}, priority: [], run: null });
      return json(mechDashboard(stored));
    },
  }];
