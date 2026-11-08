import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcReportInputs } from "./kn-khtc-forms";
import { activeAdhoc } from "./kn-khtc-adhoc-state";
import { TENANT } from "./personas";

// Mock native submission metadata shares the recorded submission identity with
// the existing reviewer/report reader. Drafts never supply pending facts.
function pending(ctx: Ctx): Response {
  const session = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!session || !(session.role === "admin" && ctx.persona.me.mfa || session.role === "kn_tinh" && session.department_code === "ke_hoach_tai_chinh"))
    return problem(403, "forbidden", "Không có quyền đọc hộp phiếu");
  const query = ctx.url.searchParams;
  const mode = query.get("report_type") ?? "regular";
  const department = query.get("department");
  const period = query.get("period_id");
  const validId = (value: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) && value !== "00000000-0000-0000-0000-000000000000";
  if (["report_type", "period_id", "department"].some(field => query.getAll(field).length > 1)
    || !["regular", "adhoc"].includes(mode) || department !== null && !/^[a-z][a-z0-9_]{0,39}$/.test(department)
    || period !== null && !validId(period)) return problem(400, "bad_request", "Bộ lọc không hợp lệ");
  if (mode === "adhoc" && (session.read_only || session.role === "admin" && !ctx.persona.me.mfa))
    return problem(403, "forbidden", "Không có quyền đọc phiếu phát sinh");
  const cursors = ctx.url.searchParams.getAll("before");
  const before = cursors[0] ?? null;
  if (cursors.length && (cursors.length !== 1 || !before || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(before) || before === "00000000-0000-0000-0000-000000000000"))
    return problem(400, "bad_request", "Con trỏ kỳ không hợp lệ");
  const periods = khtcMockStore().filter(item => (item.adhocTenant ?? TENANT) === ctx.persona.me.tenant_id && !item.terminal && (mode === "adhoc" ? item.p.report_type === "adhoc" : item.p.report_type !== "adhoc")
    && (!period || item.p.id === period) && (!department || item.p.departments.some(unit => unit.code === department))
    && (!before || item.p.id.toLowerCase() < before.toLowerCase())).sort((a, b) => b.p.id.localeCompare(a.p.id)).slice(0, 26);
  const rows: components["schemas"]["KHTCPendingReviewRow243"][] = [];
  for (const item of periods.slice(0, 25)) {
    if (!["open", "reopened"].includes(item.p.forms_status)) continue;
    for (const unit of item.p.departments) {
      if (department && unit.code !== department) continue;
      const sub = khtcReportInputs(item, unit.code).submission;
      if (!sub || sub.decision) continue;
      const request = mode === "adhoc" ? activeAdhoc(item.p.id, unit.code) : undefined;
      if (request && (request.state !== "fulfilled" || sub.content?.adhoc_request_id !== request.id))
        return problem(503, "extension.dependency_unavailable", "Chưa đối chiếu được lần gửi native với yêu cầu bổ sung");
      rows.push({ period_id: item.p.id, period_name: item.p.name, department: unit.code, department_name: unit.name,
        submission_id: sub.id, attempt: sub.attempt, submitted_at: sub.at });
    }
  }
  return json({ rows, next_cursor: periods.length > 25 ? periods[24]!.p.id : null } satisfies components["schemas"]["KHTCPendingReviewPage243"]);
}

export const khtcPendingRoutes243: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/pending-review$/, handler: ctx => {
  const response = pending(ctx); response.headers.set("Cache-Control", "no-store"); return response;
} }];
