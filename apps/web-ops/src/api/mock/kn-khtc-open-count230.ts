import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore } from "./kn-khtc-periods";
import { TENANT } from "./personas";

// Same global ledger metadata and current reader/adhoc policy as the API.
// Reading does not project Forms, create a period, or change a stored record.
export function khtcOpenPeriodCount230(ctx: Ctx): number | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc || kc.role !== "admin" && !(kc.department_code === "ke_hoach_tai_chinh" && ["kn_tinh", "kn_tw"].includes(kc.role)))
    return problem(403, "forbidden", "Không có quyền");
  const adhoc = !kc.read_only && (kc.role === "kn_tinh" || kc.role === "admin" && ctx.persona.me.mfa);
  return khtcMockStore().filter(s => (s.adhocTenant ?? TENANT) === ctx.persona.me.tenant_id && !s.terminal
    && (s.p.report_type !== "adhoc" || adhoc)).length;
}

export const khtcOpenCountRoutes230: Route[] = [{
  method: "GET",
  pattern: /^\/v1\/extension\/khtc\/open-period-count$/,
  handler: ctx => {
    const count = khtcOpenPeriodCount230(ctx);
    if (count instanceof Response) { count.headers.set("Cache-Control", "no-store"); return count; }
    return json({ open_count: count }, 200, { "Cache-Control": "no-store" });
  },
}];
