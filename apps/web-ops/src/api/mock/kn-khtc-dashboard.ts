import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { TENANT } from "./personas";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcDashboardCapture, khtcDashboardExports } from "./kn-khtc-reports";
import { khtcReportSource } from "./kn-khtc-sources";
import { Decimal } from "@/kn/grid/decimal";
import type { components } from "../generated/application";
type Dashboard = components["schemas"]["KHTCDashboard"];
const KHTC = "ke_hoach_tai_chinh";
function handle(ctx: Ctx): Response {
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (!kc || kc.role !== "admin" && !(kc.department_code === KHTC && ["kn_tinh", "kn_tw"].includes(kc.role)))
        return problem(403, "forbidden", "Không có quyền");
    const now = new Date(), local = new Date(now.getTime() + 7 * 3600000), rawY = ctx.url.searchParams.get("year") ?? String(local.getUTCFullYear()), rawQ = ctx.url.searchParams.get("quarter") ?? String(Math.floor(local.getUTCMonth() / 3) + 1), department = ctx.url.searchParams.get("department") ?? "", field = ctx.url.searchParams.get("field") ?? "";
    if (!/^\d{4}$/.test(rawY) || +rawY < 2000 || +rawY > 2100 || !/^[1-4]$/.test(rawQ) || department.length > 64 || field.length > 64)
        return problem(422, "validation_failed", "Bộ lọc không hợp lệ");
    const year = +rawY, quarter = +rawQ, start = `${year}-${String(quarter * 3 - 2).padStart(2, "0")}-01`, end = new Date(Date.UTC(year, quarter * 3, 0)).toISOString().slice(0, 10), today = local.toISOString().slice(0, 10);
    const periods = khtcMockStore().filter(s => (s.adhocTenant ?? TENANT) === ctx.persona.me.tenant_id && s.p.year === year && s.terminal !== "cancelled" && (!department || s.p.departments.some(d => d.code === department)) && s.p.report_type !== "adhoc");
    if (periods.length > 100)
        return problem(422, "extension.khtc_report_invalid", "Năm có hơn 100 kỳ; chưa thể tổng hợp đầy đủ");
    const out: Dashboard = { year, quarter, department, field, as_of: now.toISOString(), can_act: kc.role === "kn_tinh" && !kc.read_only, counts: { exported: 0, open: 0, sent: 0, forms: 0, achieved: 0, items: 0, slow: 0 }, progress: { items: 0, achieved: 0, slow: 0, missing: 0, programs: [], rows: [] }, progress_period: null, reports: [], status: [] };
    for (const s of periods) {
        const p = s.p, exports = khtcDashboardExports(p.id);
        out.counts.exported += exports.length;
        if (["quarter", "year"].includes(p.report_type))
            out.reports.push({ id: p.id, name: p.name, type: p.report_type, state: exports.length ? "exported" : s.terminal || p.state, cutoff: p.cutoff_date, exported_at: exports.at(-1)?.at ?? null });
        if (s.terminal || p.from_date > end || p.to_date < start)
            continue;
        out.counts.open++;
        for (const d of p.departments) {
            if (department && department !== d.code)
                continue;
            const sub = s.submitted[d.code];
            out.counts.forms++;
            if (sub)
                out.counts.sent++;
            out.status.push({ period_id: p.id, period_name: p.name, department: d.code, name: d.name, kind: "form", state: sub ? (sub.decision === "accepted" ? "confirmed" : sub.decision === "returned" ? "supplement" : "submitted") : "not_started", at: sub?.at ?? null });
        }
        const source = khtcReportSource(ctx, p.id);
        for (const v of source.values) {
            const dept = v.source_key.split(":")[1] ?? "";
            if (department && department !== dept)
                continue;
            out.status.push({ period_id: p.id, period_name: p.name, department: dept, name: v.source_key, kind: "source", state: v.closed ? "closed" : "open", at: source.snapshot?.taken_at ?? null });
        }
    }
    const selected = periods.filter(s => s.p.report_type === "program" && s.p.cutoff_date <= end && s.p.cutoff_date <= today).sort((a, b) => b.p.cutoff_date.localeCompare(a.p.cutoff_date))[0];
    if (selected) {
        out.progress_period = { id: selected.p.id, name: selected.p.name, type: "program", state: selected.terminal || selected.p.state, cutoff: selected.p.cutoff_date, exported_at: null };
        const data = khtcDashboardCapture(ctx, selected);
        for (const row of data.rows) {
            const fixtureField = row.department === "trong_trot" ? "trong_trot" : row.department === "chan_nuoi_thuy_san" ? "chan_nuoi" : "";
            if (row.kind !== "item" || department && row.department !== department || field && fixtureField !== field)
                continue;
            out.progress.rows.push(row);
            const planned = Decimal.parse(row.planned ?? "");
            if (!planned || planned.sign() <= 0)
                continue;
            out.progress.items++;
            const value = Decimal.parse(row.year_to_date) ?? Decimal.ZERO, percent = value.mul(Decimal.fromInt(100)).divRound(planned, 2).toString();
            const missing = row.missing;
            if (missing)
                out.progress.missing++;
            else {
                if (value.cmp(planned) >= 0)
                    out.progress.achieved++;
                if (today >= `${year}-10-01` && value.mul(Decimal.fromInt(100)).cmp(planned.mul(Decimal.fromInt(30))) < 0)
                    out.progress.slow++;
            }
            out.progress.programs.push({ ref: row.program_ref ?? row.ref, name: row.name, items: 1, known: missing ? 0 : 1, percent: missing ? null : percent });
        }
        out.counts.items = out.progress.items;
        out.counts.achieved = out.progress.achieved;
        out.counts.slow = out.progress.slow;
    }
    return json(out);
}
export const khtcDashboardRoutes: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/dashboard$/, handler: handle }];
