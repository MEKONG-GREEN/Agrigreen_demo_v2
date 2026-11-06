import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcReportInputs } from "./kn-khtc-forms";
import { TENANT } from "./personas";

// Mock native submission metadata shares the recorded submission identity with
// the existing reviewer/report reader. Drafts never supply pending facts.
function pending(ctx: Ctx): Response {
  const session = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!session || !(session.role === "admin" && ctx.persona.me.mfa || session.role === "kn_tinh" && session.department_code === "ke_hoach_tai_chinh"))
    return problem(403, "forbidden", "Không có quyền đọc hộp phiếu");
  const cursors = ctx.url.searchParams.getAll("before");
  const before = cursors[0] ?? null;
  if (cursors.length && (cursors.length !== 1 || !before || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(before) || before === "00000000-0000-0000-0000-000000000000"))
    return problem(400, "bad_request", "Con trỏ kỳ không hợp lệ");
  const periods = khtcMockStore().filter(item => (item.adhocTenant ?? TENANT) === ctx.persona.me.tenant_id && !item.terminal && item.p.report_type !== "adhoc"
    && (!before || item.p.id.toLowerCase() < before.toLowerCase())).sort((a, b) => b.p.id.localeCompare(a.p.id)).slice(0, 26);
  const rows: components["schemas"]["KHTCPendingReviewRow243"][] = [];
  for (const item of periods.slice(0, 25)) {
    if (!["open", "reopened"].includes(item.p.forms_status)) continue;
    for (const department of item.p.departments) {
      const sub = khtcReportInputs(item, department.code).submission;
      if (!sub || sub.decision) continue;
      rows.push({ period_id: item.p.id, period_name: item.p.name, department: department.code, department_name: department.name,
        submission_id: sub.id, attempt: sub.attempt, submitted_at: sub.at });
    }
  }
  return json({ rows, next_cursor: periods.length > 25 ? periods[24]!.p.id : null } satisfies components["schemas"]["KHTCPendingReviewPage243"]);
}

export const khtcPendingRoutes243: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/pending-review$/, handler: ctx => {
  const response = pending(ctx); response.headers.set("Cache-Control", "no-store"); return response;
} }];
