// GIẢ LẬP W11.66. Reads the same period, submitted-form and source stores as the other KHTC screens.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { Decimal } from "@/kn/grid/decimal";
import { filterRows } from "@/kn/khtc/reports/api";
import { validAdhocSelection, selectedAdhocContent } from "@/kn/khtc/adhoc/selection";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { TENANT } from "./personas";
import { khtcMockStore, type Stored } from "./kn-khtc-periods";
import {ADHOC_REQUESTS} from "./kn-khtc-adhoc-state";
import { khtcAdhocSubmitted, khtcReportInputs } from "./kn-khtc-forms";
import { khtcReportSource } from "./kn-khtc-sources";
import { khtcExternalForPeriod } from "./kn-khtc-external";
import { selectKhtcMockTemplate } from "./kn-khtc-templates";
import { canonical, invalidDefinition, isFixedKey, mockDigest, renderTemplate, type Definition } from "./kn-khtc-template-render";
import { templateContext } from "./kn-khtc-template-context";
type S = components["schemas"];
// Internal ledger is mutable; these fields remain readOnly in the HTTP contract.
type Content = Omit<S["KHTCReportContent"], "template_terminal"> & { template_terminal?: string };
type Detail = Omit<S["KHTCReportDetail"], "content" | "locked_admin"> & { content: Content; locked_admin?: string[] };
type Export = S["KHTCReportExport"];
const STORE = new Map<string, {
    detail: Detail;
    exports: {
        meta: Export;
        content: Content;
        body: string;
        documentNo: string;
    }[];
}>();
export interface KhtcTemplateNarrativeUsage {
 draftID: string | null; version: number; hasSavedNarrative: boolean;
 revisions: {draftID: string; version: number; sections: string[]}[];
}
const NARRATIVE_REVISIONS = new Map<string,KhtcTemplateNarrativeUsage["revisions"]>();
// Fixed mock periods are fixtures of TENANT; adhoc periods carry their real mock tenant.
function templatePeriod(ctx: Ctx, periodID: string): Stored | undefined {
 return khtcMockStore().find(p=>p.p.id===periodID && (p.adhocTenant??TENANT)===ctx.persona.me.tenant_id);
}
/** Ledger metadata only: never load/capture or synthesize a draft for this read. */
export function khtcTemplateNarrativeUsage(ctx: Ctx, periodID: string): KhtcTemplateNarrativeUsage | null {
 if(!templatePeriod(ctx,periodID))return null;
 const detail=STORE.get(periodID)?.detail, revisions=NARRATIVE_REVISIONS.get(ctx.persona.me.tenant_id+":"+periodID)??[];
 return structuredClone({draftID:detail?.draft_id??null,version:detail?.version??0,hasSavedNarrative:revisions.some(r=>r.sections.length>0),revisions});
}
function recordNarrativeRevision(ctx:Ctx, d:Detail):void {
 const key=ctx.persona.me.tenant_id+":"+d.period_id, history=NARRATIVE_REVISIONS.get(key)??[];
 const sections=Object.entries(d.content.texts).filter(([,v])=>v.trim()).map(([k])=>k);
 history.push({draftID:d.draft_id!,version:d.version,sections});NARRATIVE_REVISIONS.set(key,history);
}
const ADMIN = ["document_no", "place", "report_date", "recipient", "basis", "recipients", "authority", "signer_title", "signer"];
const KHTC = "ke_hoach_tai_chinh";
const code = (name: string, title: string, status = 409) => problem(status, `extension.khtc_report_${name}`, title);
const sections = (kind: string) => (kind === "adhoc" ? [["noi_dung", "Nội dung báo cáo", true], ["chi_tieu", "Chỉ tiêu được chọn", false]] : kind === "week" ? [["A", "Kết quả đạt được", false], ["A.I.1", "Trồng trọt", false], ["A.I.2", "Chăn nuôi", false], ["A.I.3", "Thủy sản", false], ["A.II.3.1", "Công tác báo cáo", true], ["B", "Hạn chế, vướng mắc", true], ["C.1", "Nhiệm vụ sản xuất kỳ tới", true], ["C.2", "Nhiệm vụ khuyến nông kỳ tới", true], ["C.3", "Nhiệm vụ giá cả kỳ tới", true], ["C.4", "Nhiệm vụ CCHC kỳ tới", true], ["D", "Kiến nghị", true]] : kind === "program" ? [["I", "Tiến độ thực hiện", true], ["II", "Kế hoạch kỳ tới", true]] : [["I", "Kết quả thực hiện", false], ["I.3.1", "Công tác báo cáo", true], ["III.1", "Thuận lợi", true], ["III.2", "Khó khăn", true], ["III.3", "Nguyên nhân", true], ["IV", "Phương hướng kỳ tới", true], ["V", "Kiến nghị", true]]).map(([key, title, editable]) => ({ key: String(key), title: String(title), editable: Boolean(editable) }));
const zero = (s?: string) => Decimal.parse(s ?? "0") ?? Decimal.ZERO;
function emptyMetricValue():S["KHTCReportSourceValue"]{
 return {Cumulative:null,Base:null,Increment:null,YearToDate:null,Sum:null,Min:null,Max:null,Unconfirmed:null,Closed:false,NoSource:false,Frozen:false,SourceChanged:false,Fallback:false,Unavailable:false,SourceKey:"",Mode:"",BaseRef:"",PriceDay:"",PriceDays:[],SourceKeys:[],Weeks:[]};
}
function capture(ctx: Ctx, s: Stored, refresh: boolean): Content {
    const p = s.p, state = khtcReportSource(ctx, p.id, refresh);
    const rows: S["KHTCReportRow"][] = [], submissions: S["KHTCReportSubmission"][] = [], texts: Record<string, Record<string, string>> = {}, provisional: string[] = [];
    for (const dept of p.departments) {
        const current = khtcReportInputs(s, dept.code);
        if (!current.submission) {
            if(p.report_type !== "adhoc") provisional.push(`${dept.name} chưa gửi phiếu`);
        }
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
            rows.push({ ref: plan.ref, parent_ref: null, program_ref: null, kind: "item", tab: "programs", roman: "", name: plan.name, unit: plan.unit, department: dept.code, planned: plan.qty, scale: "", in_period: done.toString(), adjustment: adj.toString(), year_to_date: ytd.toString(), percent, level, status: cell?.status ?? "", content: cell?.content ?? "", people: cell?.people ?? "", budget: cell?.budget ?? "", missing: p.report_type === "adhoc" ? !periods.some(x=>x.p.report_type==="week"&&x.p.to_date>=p.from_date&&khtcReportInputs(x,dept.code).submission?.content.cells[plan.ref]) : !current.submission, source_key: plan.module ? "module" : "", communes: [] });
        }
    }
    if (!state.snapshot)
        provisional.push("Chưa cập nhật nguồn");
    if (state.values.some(v => v.provisional))
        provisional.push("Nguồn còn tạm tính");
    const now = new Date().toISOString();
    const metrics:S["KHTCReportSourceMetric"][]=[];
    if(p.report_type==="adhoc")for(const v of state.values)metrics.push({key:v.source_key,department:v.source_key.split(":")[1]??"",form:"module",label:v.source_key,unit:"",stock:false,value:{...emptyMetricValue(),SourceKey:v.source_key,Mode:v.mode,Cumulative:v.cumulative,Increment:v.increment,YearToDate:v.year_to_date,Sum:v.sum,Min:v.price_min,Max:v.price_max,Closed:v.closed}});
    const requests=p.report_type==="adhoc"?ADHOC_REQUESTS.filter(r=>r.period_id===p.id&&r.tenant===ctx.persona.me.tenant_id):[];
    const supplements=requests.map(r=>({id:r.id,department:r.department,instruction:r.instruction,state:r.state,due_at:r.due_at,native_submission_ref:null,return_applied:r.return_applied}));
    for(const r of requests){
      const answer=khtcAdhocSubmitted(s,r.department).find(x=>x.content.adhoc_request_id===r.id);
      if(!answer)continue;
      for(const d of r.indicators){const key="bo_sung:"+r.id+":"+d.code;metrics.push({key,department:r.department,form:"bo_sung",label:d.name,unit:d.unit,stock:false,trace:[{target_key:r.id,module:"khtc",calendar:"adhoc",native_key:answer.id,from:null,to:null,closed:answer.decision==="accepted",figures_hash:answer.id}],value:{...emptyMetricValue(),SourceKey:key,Mode:"sum",Sum:answer.content.adhoc_values?.[d.code]??"0",Closed:answer.decision==="accepted",SourceKeys:[r.id]}});}
      if(answer.content.adhoc_text){const key="bo_sung:"+r.id+":loi_van";metrics.push({key,department:r.department,form:"bo_sung",label:"Nội dung bổ sung",unit:"",text:answer.content.adhoc_text,stock:false,value:{...emptyMetricValue(),SourceKey:key,Closed:answer.decision==="accepted"}});}
    }
    const external = khtcExternalForPeriod(p.id);
    return { data: { period: { id: p.id, no: p.no ?? null, scope: p.scope ?? null, type: p.report_type as S["KHTCReportPeriod"]["type"], year: p.year, name: p.name, from: p.from_date, to: p.to_date, cutoff: p.cutoff_date, departments: p.departments.map(d => d.code) }, template: ({ week: "mau_1", month: "mau_2", quarter: "mau_3", year: "mau_4", program: "mau_5", adhoc: "adhoc" } as const)[p.report_type as "week"] ?? "mau_1", plan: { VersionID: p.id, Version: 1, Year: p.year, DecisionNo: "MINH HỌA", Entries: [] }, sources: state.snapshot ? { id: state.snapshot.id, version: state.snapshot.no, plan_version_id: null, figures_hash: state.snapshot.figures_hash, status_hash: state.snapshot.status_hash, closed: state.snapshot.closed, at: state.snapshot.taken_at } : null, metrics, ...(supplements.length?{supplements}:{}), submissions, rows, form_texts: texts, provisional, input_hash: JSON.stringify([submissions, state.snapshot?.id, external.map(x => [x.entry_id, x.version]),metrics,supplements]), captured_at: now, ...(external.length ? { external } : {}) }, admin: Object.fromEntries(ADMIN.map(k => [k, { system: k === "report_date" ? now.slice(0, 10) : "", override: null, actor: null, at: null, system_changed: false }])), texts: Object.fromEntries(sections(p.report_type).filter(s => s.editable).map(s => [s.key, s.key === "D" ? "Không có." : ""])) };
}
function load(ctx: Ctx, s: Stored, preview = false) {
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
    if (!preview) {
        if (!value.detail.version && isFixedKey(value.detail.content.data.template)) {
            value.detail.content.template_snapshot = selectKhtcMockTemplate(ctx, value.detail.content.data.template, value.detail.content.data.period.from, sections(s.p.report_type));
            value.detail.content.template_terminal = s.terminal;
        }
        decorateFixedReport(value.detail);
    }
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    value.detail.editable = (s.p.report_type==="adhoc" ? !!kc&&!kc.read_only&&(kc.role==="admin"||kc.role==="kn_tinh"&&kc.department_code===KHTC&&s.adhocOwner===ctx.persona.me.actor_ref) : !kc || kc.role==="kn_tinh"&&kc.department_code===KHTC&&!kc.read_only) && !value.detail.final && s.terminal!=="cancelled";
    value.detail.can_refresh = value.detail.editable && !s.terminal;
    const current = capture(ctx, s, false);
    value.detail.source_changed = current.data.input_hash !== value.detail.content.data.input_hash || (khtcReportSource(ctx, s.p.id)).source_changed;
    return value;
}
/** Read-only preview seam: the template editor shares the composer's projection. */
export function khtcTemplatePreviewContent(ctx: Ctx, periodId: string): Content | null {
 const period=templatePeriod(ctx,periodId);
 return period ? structuredClone(load(ctx,period,true).detail.content) : null;
}
export const khtcReportRoutes: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/exported-reports$/, handler: archive },{ method: "GET", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report(?:\/(exports)(?:\/([^/]+)\/(download))?)?$/, handler: handle }, { method: "POST", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report\/(draft|refresh|exports)(?:\/([^/]+)\/(final))?$/, handler: handle }];
function handle(ctx: Ctx): Response {
    const [id, op, exportID, action] = ctx.params;
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (kc && kc.role !== "admin" && !(kc.department_code === KHTC && (kc.role === "kn_tinh" || kc.role === "kn_tw")))
        return problem(403, "forbidden", "Không có quyền");
    const s = khtcMockStore().find(x => x.p.id === id);
    if (!s)
        return code("not_found", "Không có báo cáo", 404);
    if(s.p.report_type==="adhoc" && (!kc || kc.read_only || !(kc.role==="admin" || kc.role==="kn_tinh"&&kc.department_code===KHTC)))return problem(403,"forbidden","Không có quyền xem bản soạn phát sinh");
    if(s.p.report_type==="adhoc" && s.adhocTenant!==ctx.persona.me.tenant_id)return code("not_found","Không có báo cáo",404);
    let value: ReturnType<typeof load>;
    try { value=load(ctx,s); } catch(e) { return code("invalid",e instanceof Error?e.message:"Không đọc được mẫu đã ghim",422); }
    const d=value.detail;
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
        if(s.p.report_type==="adhoc" && d.source_changed)return code("source_changed","Yêu cầu hoặc nguồn đã đổi — cập nhật bản soạn");
        if(s.p.report_type==="adhoc"&&!validAdhocSelection(d.content.adhoc,d.content.data))return code("invalid","Chọn 1–200 chỉ tiêu có nguồn",422);
        if (!d.version)
            return code("version_conflict", "Lưu bản soạn trước khi xuất");
        if (d.content.data.provisional.length && !body.acknowledge_provisional)
            return code("provisional", "Xác nhận xuất Tạm tính");
        const filter = body.filter ?? {};
        if (filter.level && !["achieved", "progressing", "slow", "qualitative"].includes(filter.level))
            return code("invalid", "Mức đạt không hợp lệ", 422);
        const rows = filterRows(selectedAdhocContent(d.content).data.rows ?? [], filter), esc = (s: string) => `"${s.replaceAll('"', '""')}"`;
        const document=renderKhtcMockFixed(d.content,filter);
        const compiled=document?.nodes.flatMap(n=>[[`${n.number}. ${n.title}`],...n.rows.map(r=>[r.preamble??"",r.tokens.map(t=>t.text).join("")])])??[];
        const bytes = "\uFEFF" + [...compiled,["GIẢ LẬP KHTC", d.content.data.period.name], ["Hạng mục", "Trong kỳ", "Điều chỉnh", "Lũy kế năm"], ...rows.map(r => [r.name, r.in_period, r.adjustment, r.year_to_date]),...selectedAdhocContent(d.content).data.metrics.map(m=>[m.label,m.text??m.value.Increment??m.value.Sum??m.value.Cumulative??"",m.unit,m.department]),...Object.entries(d.content.texts).map(([k,v])=>[k,v])].map(r => r.map(esc).join(",")).join("\r\n");
        const x: Export = { id: uuidv7(), period_id: s.p.id, version: value.exports.length + 1, draft_id: d.draft_id!, file_name: `gia-lap-khtc-v${value.exports.length + 1}.csv`, body_hash: "0".repeat(64), input_hash: d.content.data.input_hash, provisional: !!d.content.data.provisional.length, filter, actor_ref: ctx.persona.me.actor_ref, at: new Date().toISOString() };
        value.exports.push({ meta: x, content: structuredClone(d.content), body: bytes, documentNo: d.content.admin.document_no?.override ?? d.content.admin.document_no?.system ?? "" });
        return json(x, 201);
    }
    if (op === "refresh") {
        if (!d.can_refresh)
            return code("source_changed", "Kỳ đã đóng giữ số đã chốt");
        const fresh = capture(ctx, s, true);
        fresh.admin = d.content.admin;
        fresh.texts = d.content.texts;
        if(d.content.template_snapshot) { fresh.template_snapshot=structuredClone(d.content.template_snapshot);fresh.template_terminal=s.terminal; }
        if(d.content.adhoc)fresh.adhoc=structuredClone(d.content.adhoc);
        d.content = fresh;
    }
    else {
        if (Object.keys(body).some(k => !["base_version", "admin", "texts", "adhoc"].includes(k)) || Object.keys(body.admin ?? {}).some(k => !ADMIN.includes(k) || d.locked_admin?.includes(k)) || Object.keys(body.texts ?? {}).some(k => !d.sections.some(s => s.key === k && s.editable)))
            return code("invalid", "Ô số và mục hệ thống chỉ đọc", 422);
        if(body.adhoc!==undefined){
            if(s.p.report_type!=="adhoc"||!validAdhocSelection(body.adhoc,d.content.data))return code("invalid","Chọn 1–200 chỉ tiêu có nguồn",422);
            d.content.adhoc=structuredClone(body.adhoc);
        }
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
    if(d.content.template_snapshot) d.content.template_terminal=s.terminal;
    try { decorateFixedReport(d); } catch(e) { return code("invalid",e instanceof Error?e.message:"Không dựng được mẫu đã ghim",422); }
    if(s.p.report_type==="adhoc")d.content.data.provisional=selectedAdhocContent(d.content).data.provisional;
    d.version++;
    d.draft_id = uuidv7();
    recordNarrativeRevision(ctx,d);
    return json((load(ctx, s)).detail, 201);
}
/** W10.17: bản xuất Excel đã lưu cùng bản soạn hiện tại — nguồn của Word giả lập (kn-khtc-words). */
export function khtcReportExport(periodId: string, exportId: string) {
    const value = STORE.get(periodId), x = value?.exports.find(e => e.meta.id === exportId);
    return value && x ? { detail: {...value.detail,content:selectedAdhocContent(structuredClone(x.content))}, meta: x.meta } : undefined;
}

function archive(ctx: Ctx): Response {
 const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
 if (!kc || kc.role !== "admin" && !(kc.department_code === KHTC && (kc.role === "kn_tinh" || kc.role === "kn_tw"))) return problem(403,"forbidden","Không có quyền");
 const u = new URL(ctx.request.url), y=u.searchParams.get("year")??"", rawOffset=u.searchParams.get("offset")??"0", rawLimit=u.searchParams.get("limit")??"20";
 const kind=u.searchParams.get("kind")??"", q=u.searchParams.get("q")??"", versions=u.searchParams.get("versions")??"false";
 if (!/^\d{4}$/.test(y) || +y<2000 || +y>2100 || !/^\d{1,6}$/.test(rawOffset) || +rawOffset>100000 || !/^\d{1,2}$/.test(rawLimit) || +rawLimit<1 || +rawLimit>50 || q.length>800 || [...q].length>200 || !["","week","month","quarter","year","program","adhoc"].includes(kind) || !["true","false"].includes(versions)) return code("invalid","Bộ lọc không hợp lệ",422);
 const rows:S["KHTCArchiveRow"][]=[];
 for(const p of khtcMockStore()) {
  if(p.p.year!==+y || p.terminal==="cancelled" || kind && p.p.report_type!==kind) continue;
  const stored=STORE.get(p.p.id); if(!stored)continue;
  const chosen=versions==="true" ? stored.exports : [stored.exports.find(x=>x.meta.id===stored.detail.final?.export_id)??stored.exports.at(-1)].filter(x=>x!==undefined);
  for(const x of chosen)if((p.p.name+" "+x.documentNo).toLocaleLowerCase().includes(q.trim().toLocaleLowerCase()))rows.push({id:x.meta.id,period_id:p.p.id,period_name:p.p.name,report_type:p.p.report_type as S["KHTCArchiveRow"]["report_type"],document_no:x.documentNo,version:x.meta.version,final:stored.detail.final?.export_id===x.meta.id,provisional:x.meta.provisional,actor_ref:x.meta.actor_ref,at:x.meta.at});
 }
 rows.sort((a,b)=>b.at.localeCompare(a.at)||b.id.localeCompare(a.id));const offset=+rawOffset,limit=+rawLimit;
 return json({items:rows.slice(offset,offset+limit),has_more:rows.length>offset+limit,offset},200,{"Cache-Control":"no-store"});
}

export function renderKhtcMockFixed(content:Content, filter:S["KHTCReportFilter"]={}):S["KHTCTemplateRendered"]|undefined {
 const snapshot=content.template_snapshot;
 if(!snapshot || snapshot.mode==="legacy")return undefined;
 const definition=snapshot.definition as unknown as Definition;
 if(snapshot.key!==content.data.template || snapshot.rule!=="khtc-template/v2.3-1" || snapshot.hash!==mockDigest(canonical(definition)) || invalidDefinition(definition))throw new Error("Nguồn mẫu đã ghim không khớp");
 const selected=structuredClone(content);selected.data.rows=filterRows(selected.data.rows,filter);
 return renderTemplate(definition,templateContext(selected,content.template_terminal??""),"actual");
}
function decorateFixedReport(detail:Detail):void {
 const document=renderKhtcMockFixed(detail.content);
 if(!document) {
  delete detail.template_document;delete detail.locked_admin;
  const embedded=detail.content.template_snapshot?.definition.sections;
  if(detail.content.template_snapshot?.mode==="legacy" && Array.isArray(embedded))detail.sections=structuredClone(embedded) as S["KHTCReportSection"][];
  return;
 }
 detail.template_document=document;
 detail.sections=document.nodes.map(n=>({key:n.id,title:n.title,editable:n.text_slot}));
 const definition=detail.content.template_snapshot!.definition as unknown as Definition;
 detail.locked_admin=[...new Set(definition.nodes.flatMap(n=>n.locked_admin))].sort();
}
