// GIẢ LẬP W11.66. Reads the same period, submitted-form and source stores as the other KHTC screens.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { Decimal } from "@/kn/grid/decimal";
import { filterRows } from "@/kn/khtc/reports/api";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore, type Stored } from "./kn-khtc-periods";
import { khtcReportInputs } from "./kn-khtc-forms";
import { khtcReportSource } from "./kn-khtc-sources";
import { khtcExternalForPeriod } from "./kn-khtc-external";
type S = components["schemas"];
type Detail = S["KHTCReportDetail"];
type Content = S["KHTCReportContent"];
type Export = S["KHTCReportExport"];
const STORE = new Map<string, {
    detail: Detail;
    exports: {
        meta: Export;
        body: string;
    }[];
}>();
const ADMIN = ["document_no", "place", "report_date", "recipient", "basis", "recipients", "authority", "signer_title", "signer"];
const KHTC = "ke_hoach_tai_chinh";
const code = (name: string, title: string, status = 409) => problem(status, `extension.khtc_report_${name}`, title);
const sections = (kind: string) => (kind === "week" ? [["A", "Kết quả đạt được", false], ["A.I.1", "Trồng trọt", false], ["A.I.2", "Chăn nuôi", false], ["A.I.3", "Thủy sản", false], ["A.II.3.1", "Công tác báo cáo", true], ["B", "Hạn chế, vướng mắc", true], ["C.1", "Nhiệm vụ sản xuất kỳ tới", true], ["C.2", "Nhiệm vụ khuyến nông kỳ tới", true], ["C.3", "Nhiệm vụ giá cả kỳ tới", true], ["C.4", "Nhiệm vụ CCHC kỳ tới", true], ["D", "Kiến nghị", true]] : kind === "program" ? [["I", "Tiến độ thực hiện", true], ["II", "Kế hoạch kỳ tới", true]] : [["I", "Kết quả thực hiện", false], ["I.3.1", "Công tác báo cáo", true], ["III.1", "Thuận lợi", true], ["III.2", "Khó khăn", true], ["III.3", "Nguyên nhân", true], ["IV", "Phương hướng kỳ tới", true], ["V", "Kiến nghị", true]]).map(([key, title, editable]) => ({ key: String(key), title: String(title), editable: Boolean(editable) }));
const zero = (s?: string) => Decimal.parse(s ?? "0") ?? Decimal.ZERO;
function capture(ctx: Ctx, s: Stored, refresh: boolean): Content {
    const p = s.p, state = khtcReportSource(ctx, p.id, refresh);
    const rows: S["KHTCReportRow"][] = [], submissions: S["KHTCReportSubmission"][] = [], texts: Record<string, Record<string, string>> = {}, provisional: string[] = [];
    for (const dept of p.departments) {
        const current = khtcReportInputs(s, dept.code);
        if (!current.submission)
            provisional.push(`${dept.name} chưa gửi phiếu`);
        else {
            texts[dept.code] = current.submission.content.texts;
            if (current.submission.decision === "returned")
                provisional.push(`${dept.name} đang bổ sung`);
        }
        const periods = khtcMockStore().filter(x => x.terminal !== "cancelled" && x.p.year === p.year && x.p.to_date <= p.to_date && (x.p.report_type === "week" || x.p.id === p.id));
        for (const x of periods) {
            const sub = khtcReportInputs(x, dept.code).submission;
            if (sub)
                submissions.push({ period_id: x.p.id, department: dept.code, type: x.p.report_type, year: p.year, from: x.p.from_date, to: x.p.to_date, submission_id: sub.id, attempt: sub.attempt, sheet_id: sub.id, hash: sub.id, decision: sub.decision });
        }
        for (const plan of current.plan) {
            let done = Decimal.ZERO, ytd = Decimal.ZERO, adj = Decimal.ZERO;
            for (const x of periods.filter(x => x.p.report_type === "week")) {
                const sub = khtcReportInputs(x, dept.code).submission;
                if (!sub)
                    continue;
                const amount = zero(sub.content.cells[plan.ref]?.done);
                ytd = ytd.add(amount);
                if (x.p.to_date >= p.from_date)
                    done = done.add(amount);
                for (const a of sub.content.adjustments ?? [])
                    if (a.entry_ref === plan.ref && a.period_year === p.year) {
                        ytd = ytd.add(zero(a.amount));
                        if (x.p.to_date >= p.from_date)
                            adj = adj.add(zero(a.amount));
                    }
            }
            if (plan.module) {
                const value = state.values.find(v => v.mode === (dept.code === "trong_trot" ? "season" : "sum"));
                done = zero(value?.increment ?? value?.sum ?? undefined);
                ytd = zero(value?.year_to_date ?? undefined);
            }
            const cell = current.submission?.content.cells[plan.ref];
            const planned = zero(plan.qty);
            const percent = planned.sign() > 0 ? ytd.mul(Decimal.fromInt(100)).divRound(planned, 1).toString() : null;
            const level = planned.sign() <= 0 ? "qualitative" : ytd.cmp(planned) >= 0 ? "achieved" : ytd.mul(Decimal.fromInt(100)).cmp(planned.mul(Decimal.fromInt(30))) >= 0 ? "progressing" : "slow";
            rows.push({ ref: plan.ref, parent_ref: null, program_ref: null, kind: "item", tab: "programs", roman: "", name: plan.name, unit: plan.unit, department: dept.code, planned: plan.qty, scale: "", in_period: done.toString(), adjustment: adj.toString(), year_to_date: ytd.toString(), percent, level, status: cell?.status ?? "", content: cell?.content ?? "", people: cell?.people ?? "", budget: cell?.budget ?? "", missing: !current.submission, source_key: plan.module ? "module" : "", communes: [] });
        }
    }
    if (!state.snapshot)
        provisional.push("Chưa cập nhật nguồn");
    if (state.values.some(v => v.provisional))
        provisional.push("Nguồn còn tạm tính");
    const now = new Date().toISOString();
    const external = khtcExternalForPeriod(p.id);
    return { data: { period: { id: p.id, no: p.no ?? null, scope: p.scope ?? null, type: p.report_type as S["KHTCReportPeriod"]["type"], year: p.year, name: p.name, from: p.from_date, to: p.to_date, cutoff: p.cutoff_date, departments: p.departments.map(d => d.code) }, template: ({ week: "mau_1", month: "mau_2", quarter: "mau_3", year: "mau_4", program: "mau_5" } as const)[p.report_type as "week"] ?? "mau_1", plan: { VersionID: p.id, Version: 1, Year: p.year, DecisionNo: "MINH HỌA", Entries: [] }, sources: state.snapshot ? { id: state.snapshot.id, version: state.snapshot.no, plan_version_id: null, figures_hash: state.snapshot.figures_hash, status_hash: state.snapshot.status_hash, closed: state.snapshot.closed, at: state.snapshot.taken_at } : null, metrics: [], submissions, rows, form_texts: texts, provisional, input_hash: JSON.stringify([submissions, state.snapshot?.id, external.map(x => [x.entry_id, x.version])]), captured_at: now, ...(external.length ? { external } : {}) }, admin: Object.fromEntries(ADMIN.map(k => [k, { system: k === "report_date" ? now.slice(0, 10) : "", override: null, actor: null, at: null, system_changed: false }])), texts: Object.fromEntries(sections(p.report_type).filter(s => s.editable).map(s => [s.key, s.key === "D" ? "Không có." : ""])) };
}
function load(ctx: Ctx, s: Stored) {
    let value = STORE.get(s.p.id);
    if (!value) {
        value = { detail: { period_id: s.p.id, draft_id: null, version: 0, content: capture(ctx, s, false), sections: sections(s.p.report_type), final: null, editable: false, can_refresh: false, source_changed: false, source_error: "", terminal: s.terminal }, exports: [] };
        if (s.p.report_type === "program") {
            const previous = [...STORE.values()].filter(v => v.detail.version > 0 && v.detail.content.data.period.type === "program" && v.detail.content.data.period.to < s.p.to_date && khtcMockStore().some(p => p.p.id === v.detail.period_id && p.terminal !== "cancelled")).sort((a, b) => b.detail.content.data.period.to.localeCompare(a.detail.content.data.period.to))[0];
            if (previous && Object.hasOwn(previous.detail.content.texts, "II"))
                value.detail.content.texts.II = previous.detail.content.texts.II!;
        }
        STORE.set(s.p.id, value);
    }
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    value.detail.editable = (!kc || kc.role === "kn_tinh" && kc.department_code === KHTC && !kc.read_only) && !value.detail.final && s.terminal !== "cancelled";
    value.detail.can_refresh = value.detail.editable && !s.terminal;
    const current = capture(ctx, s, false);
    value.detail.source_changed = current.data.input_hash !== value.detail.content.data.input_hash || (khtcReportSource(ctx, s.p.id)).source_changed;
    return value;
}
export const khtcReportRoutes: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report(?:\/(exports)(?:\/([^/]+)\/(download))?)?$/, handler: handle }, { method: "POST", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report\/(draft|refresh|exports)(?:\/([^/]+)\/(final))?$/, handler: handle }];
function handle(ctx: Ctx): Response {
    const [id, op, exportID, action] = ctx.params;
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (kc && kc.role !== "admin" && !(kc.department_code === KHTC && (kc.role === "kn_tinh" || kc.role === "kn_tw")))
        return problem(403, "forbidden", "Không có quyền");
    const s = khtcMockStore().find(x => x.p.id === id);
    if (!s)
        return code("not_found", "Không có báo cáo", 404);
    const value = load(ctx, s), d = value.detail;
    if (ctx.request.method === "GET") {
        if (action === "download") {
            const x = value.exports.find(x => x.meta.id === exportID);
            if (!x)
                return code("not_found", "Không có bản xuất", 404);
            return new Response(x.body, { headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": `attachment; filename="${x.meta.file_name}"` } });
        }
        return json(op === "exports" ? { items: [...value.exports].reverse().map(x => x.meta) } : d);
    }
    if (d.final)
        return code("final", "Báo cáo đã có Bản cuối");
    if (!d.editable)
        return problem(403, "forbidden", "Chỉ cán bộ KHTC phụ trách được soạn");
    const body = (ctx.body ?? {}) as S["KHTCReportSave"] & S["KHTCReportExportInput"];
    if (action === "final") {
        const x = value.exports.find(x => x.meta.id === exportID);
        if (!x)
            return code("not_found", "Không có bản xuất", 404);
        if (x.meta.provisional)
            return code("provisional", "Báo cáo còn tạm tính");
        if (d.source_changed)
            return code("source_changed", "Cần cập nhật số liệu");
        if (x.meta.draft_id !== d.draft_id)
            return code("version_conflict", "Bản soạn đã thay đổi");
        d.final = { export_id: x.meta.id, actor_ref: ctx.persona.me.actor_ref, at: new Date().toISOString() };
        s.reportFinal = true;
        return json((load(ctx, s)).detail, 201);
    }
    if (body.base_version !== d.version)
        return code("version_conflict", "Bản soạn đã thay đổi");
    if (op === "exports") {
        if (!d.version)
            return code("version_conflict", "Lưu bản soạn trước khi xuất");
        if (d.content.data.provisional.length && !body.acknowledge_provisional)
            return code("provisional", "Xác nhận xuất Tạm tính");
        const filter = body.filter ?? {};
        if (filter.level && !["achieved", "progressing", "slow", "qualitative"].includes(filter.level))
            return code("invalid", "Mức đạt không hợp lệ", 422);
        const rows = filterRows(d.content.data.rows ?? [], filter), esc = (s: string) => `"${s.replaceAll('"', '""')}"`;
        const bytes = "\uFEFF" + [["GIẢ LẬP KHTC", d.content.data.period.name], ["Hạng mục", "Trong kỳ", "Điều chỉnh", "Lũy kế năm"], ...rows.map(r => [r.name, r.in_period, r.adjustment, r.year_to_date])].map(r => r.map(esc).join(",")).join("\r\n");
        const x: Export = { id: uuidv7(), period_id: s.p.id, version: value.exports.length + 1, draft_id: d.draft_id!, file_name: `gia-lap-khtc-v${value.exports.length + 1}.csv`, body_hash: "0".repeat(64), input_hash: d.content.data.input_hash, provisional: !!d.content.data.provisional.length, filter, actor_ref: ctx.persona.me.actor_ref, at: new Date().toISOString() };
        value.exports.push({ meta: x, body: bytes });
        return json(x, 201);
    }
    if (op === "refresh") {
        if (!d.can_refresh)
            return code("source_changed", "Kỳ đã đóng giữ số đã chốt");
        const fresh = capture(ctx, s, true);
        fresh.admin = d.content.admin;
        fresh.texts = d.content.texts;
        d.content = fresh;
    }
    else {
        if (Object.keys(body).some(k => !["base_version", "admin", "texts"].includes(k)) || Object.keys(body.admin ?? {}).some(k => !ADMIN.includes(k)) || Object.keys(body.texts ?? {}).some(k => !d.sections.some(s => s.key === k && s.editable)))
            return code("invalid", "Ô số và mục hệ thống chỉ đọc", 422);
        for (const [key, text] of Object.entries(body.admin ?? {})) {
            if (text !== null && text.length > 2000)
                return code("invalid", "Ô hành chính quá dài", 422);
            const field = d.content.admin[key]!;
            field.override = text;
            field.actor = text === null ? null : ctx.persona.me.actor_ref;
            field.at = text === null ? null : new Date().toISOString();
        }
        for (const [key, text] of Object.entries(body.texts ?? {})) {
            if (text.length > 12000)
                return code("invalid", "Lời văn quá dài", 422);
            d.content.texts[key] = text;
        }
    }
    d.version++;
    d.draft_id = uuidv7();
    return json((load(ctx, s)).detail, 201);
}
