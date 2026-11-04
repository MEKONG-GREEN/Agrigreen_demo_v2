import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { TENANT } from "./personas";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcDashboardExports } from "./kn-khtc-reports";
import { khtcReportInputs, khtcReportPlan } from "./kn-khtc-forms";
import { khtcReportSource } from "./kn-khtc-sources";
import { Decimal } from "@/kn/grid/decimal";
import type { components } from "../generated/application";
type Dashboard = components["schemas"]["KHTCDashboard"];
const KHTC = "ke_hoach_tai_chinh";
const DAY = 86400000;
const date = (value: number) => new Date(value).toISOString().slice(0, 10);
function liveWindow(year: number, quarter: number, raw: string | null, now: Date): Dashboard["progress_window"] | Response {
    const start = Date.UTC(year, quarter * 3 - 3, 1), end = Date.UTC(year, quarter * 3, 0);
    const localDay = Date.parse(`${date(now.getTime() + 7 * 3600000)}T00:00:00Z`);
    let requested = Math.min(end, localDay);
    if (raw !== null) {
        if (raw.length !== 10 || !/^\d{4}-\d{2}-\d{2}$/.test(raw)) return problem(422, "validation_failed", "Ngày yêu cầu không hợp lệ");
        requested = Date.parse(`${raw}T00:00:00Z`);
        if (!Number.isFinite(requested) || date(requested) !== raw || requested < start || requested > end) return problem(422, "validation_failed", "Ngày yêu cầu ngoài năm/quý");
    } else if (localDay < start) return null;
    const effective = Math.min(requested, localDay);
    if (effective < start) return null;
    const jan1 = Date.UTC(year, 0, 1), from = jan1 - ((new Date(jan1).getUTCDay() - 3 + 7) % 7) * DAY;
    const to = effective - ((new Date(effective).getUTCDay() - 2 + 7) % 7) * DAY;
    if (to < from) return null;
    return {year,quarter,through_date:date(requested),effective_cutoff:new Date(Math.min(effective + DAY - 7 * 3600000 - 1,now.getTime())).toISOString(),from_date:date(from),quantity_to:date(to),calendar_id:KHTC,calendar_version:2,plan_version_id:null};
}
function boundedNumber(raw?: string): Decimal {
    if (!raw) return Decimal.ZERO;
    if (raw.length > 128 || !/^-?\d+(?:\.\d+)?$/.test(raw)) throw new Error("Số liệu nguồn không hợp lệ");
    const value = Decimal.parse(raw);
    if (!value) throw new Error("Số liệu nguồn không hợp lệ");
    return value;
}
function handle(ctx: Ctx): Response {
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (!kc || kc.role !== "admin" && !(kc.department_code === KHTC && ["kn_tinh", "kn_tw"].includes(kc.role)))
        return problem(403, "forbidden", "Không có quyền");
    const now = new Date(), local = new Date(now.getTime() + 7 * 3600000), rawY = ctx.url.searchParams.get("year") ?? String(local.getUTCFullYear()), rawQ = ctx.url.searchParams.get("quarter") ?? String(Math.floor(local.getUTCMonth() / 3) + 1), department = ctx.url.searchParams.get("department") ?? "", field = ctx.url.searchParams.get("field") ?? "";
    if (!/^\d{4}$/.test(rawY) || +rawY < 2000 || +rawY > 2100 || !/^[1-4]$/.test(rawQ) || department.length > 64 || field.length > 64)
        return problem(422, "validation_failed", "Bộ lọc không hợp lệ");
    for (const key of ["year","quarter","through_date"]) if (ctx.url.searchParams.getAll(key).length > 1 || ctx.url.searchParams.has(key) && ctx.url.searchParams.get(key) === "") return problem(422,"validation_failed","Bộ lọc không hợp lệ");
    const year = +rawY, quarter = +rawQ, start = `${year}-${String(quarter * 3 - 2).padStart(2, "0")}-01`, end = new Date(Date.UTC(year, quarter * 3, 0)).toISOString().slice(0, 10), today = local.toISOString().slice(0, 10);
    const window = liveWindow(year,quarter,ctx.url.searchParams.get("through_date"),now);
    if (window instanceof Response) return window;
    const canReadAdhoc = !kc.read_only && (kc.role === "kn_tinh" || kc.role === "admin" && ctx.persona.me.mfa);
    const periods = khtcMockStore().filter(s => (s.adhocTenant ?? TENANT) === ctx.persona.me.tenant_id && s.p.year === year && s.terminal !== "cancelled" && (!department || s.p.departments.some(d => d.code === department)) && (s.p.report_type !== "adhoc" || canReadAdhoc));
    const out: Dashboard = { year, quarter, department, field, as_of: now.toISOString(), can_act: kc.role === "kn_tinh" && !kc.read_only, counts: { exported: 0, open: 0, sent: 0, forms: 0, achieved: 0, items: 0, slow: 0 }, progress: { items: 0, achieved: 0, slow: 0, missing: 0, programs: [], rows: [] }, progress_period: null, reports: [], status: [] };
    out.progress_window = window;
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
            if (sub && sub.decision !== "returned")
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
    const effectiveDay = window ? date(Date.parse(window.effective_cutoff) + 7 * 3600000) : "";
    const selected = periods.filter(s => window && s.p.report_type === "program" && s.p.cutoff_date <= effectiveDay).sort((a, b) => b.p.cutoff_date.localeCompare(a.p.cutoff_date))[0];
    if (selected)
        out.progress_period = { id: selected.p.id, name: selected.p.name, type: "program", state: selected.terminal || selected.p.state, cutoff: selected.p.cutoff_date, exported_at: null };
    if (window) {
      const weeks = periods.filter(s => s.p.report_type === "week" && s.p.to_date <= window.quantity_to).sort((a,b)=>a.p.to_date.localeCompare(b.p.to_date));
      try {
        for (const dept of ["trong_trot","chan_nuoi_thuy_san","thong_tin_truyen_thong","hanh_chinh_tong_hop"]) {
          const fixtureField = dept === "trong_trot" ? "trong_trot" : dept === "chan_nuoi_thuy_san" ? "chan_nuoi" : "";
          if (department && department !== dept || field && field !== fixtureField) continue;
          // PLAN is the same immutable synthetic plan used by native mock Forms;
          // no fake Stored period, ID, version or draft is created to read it.
          const plan = year === local.getUTCFullYear() || weeks.length ? khtcReportPlan(dept) : [];
          for (const item of plan) {
            let value = Decimal.ZERO, adjustment = Decimal.ZERO, known = false;
            for (const week of weeks) {
              const sub = khtcReportInputs(week,dept).submission;
              if (!sub || sub.decision === "returned") continue;
              const cell = sub.content.cells[item.ref];
              if (cell) { known = true; value = value.add(boundedNumber(cell.done)); }
              for (const a of sub.content.adjustments ?? []) if (a.entry_ref === item.ref && a.period_year === year) adjustment = adjustment.add(boundedNumber(a.amount));
            }
            value = value.add(adjustment);
            if (item.module) {
              known = false;
              // Only actual stored closed source values can be used. No call to
              // refresh, hard-coded live figure or synthetic source snapshot.
              for (const week of [...weeks].reverse()) {
                const source = khtcReportSource(ctx,week.p.id);
                const metric = source.values.find(v=>v.mode === (dept === "trong_trot" ? "season" : "sum"));
                if (!metric || !metric.closed || metric.provisional || metric.no_source || metric.unavailable || !source.snapshot || source.snapshot.taken_at > window.effective_cutoff) continue;
                value = boundedNumber(metric.year_to_date ?? metric.sum ?? undefined); known = true; break;
              }
              if (!known) value = Decimal.ZERO;
              adjustment = Decimal.ZERO;
            }
            const planned = boundedNumber(item.qty);
            const row: Dashboard["progress"]["rows"][number] = {ref:item.ref,parent_ref:null,program_ref:null,kind:"item",tab:"programs",roman:"",name:item.name,unit:item.unit,department:dept,planned:item.qty,scale:"",in_period:value.sub(adjustment).toString(),adjustment:adjustment.toString(),year_to_date:value.toString(),percent:planned.sign()>0?value.mul(Decimal.fromInt(100)).divRound(planned,1).toString():null,level:planned.sign()<=0?"qualitative":value.cmp(planned)>=0?"achieved":value.mul(Decimal.fromInt(100)).cmp(planned.mul(Decimal.fromInt(30)))>=0?"progressing":"slow",status:"",content:"",people:"0",budget:"0",missing:!known,source_key:item.module?"module":"",communes:[]};
            out.progress.rows.push(row);
            if (planned.sign() <= 0)
                continue;
            out.progress.items++;
            const percent = value.mul(Decimal.fromInt(100)).divRound(planned, 2).toString();
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
        }
      } catch { return problem(503,"extension.dependency_unavailable","Số liệu nguồn không hợp lệ"); }
        out.counts.items = out.progress.items;
        out.counts.achieved = out.progress.achieved;
        out.counts.slow = out.progress.slow;
    }
    return json(out);
}
export const khtcDashboardRoutes: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/dashboard$/, handler: handle }];
