// GIẢ LẬP W10.17: Word KHTC Mẫu 1–5 từ bản Excel đã lưu, qua sổ lần xuất giả lập của `cap/export` (./export).
import { bindArchiveMockWord, archiveMockWords, archiveMockWord } from "./kn-khtc-archive-word-store";
import { TENANT } from "./personas";
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { downloadResponse, mockExportBody, mockExportRun, mockExportRuns, placeDateLine } from "./export";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcReportExport, renderKhtcMockFixed } from "./kn-khtc-reports";
type Word = components["schemas"]["KHTCReportWord"];
const KHTC = "ke_hoach_tai_chinh";
const subject = (id: string) => `khtc_report:${id}`;
const word = (r: ReturnType<typeof mockExportRun>): Word => { const { subject: _s, ...rest } = r; void _s; return rest; };
export const khtcWordRoutes: Route[] = [
    {method:"GET",pattern:/^\/v1\/extension\/khtc\/periods\/([^/]+)\/report\/exports\/([^/]+)\/words(?:\/([^/]+)\/download)?$/,handler:archiveRead},
    { method: "POST", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report\/exports\/([^/]+)\/word$/, handler: create },
    { method: "GET", pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/report\/words(?:\/([^/]+)\/download)?$/, handler: read },
];
function allowed(ctx: Ctx, write: boolean): boolean {
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (!kc)
        return true;
    if (write)
        return kc.role === "kn_tinh" && kc.department_code === KHTC && !kc.read_only;
    return kc.role === "admin" || kc.department_code === KHTC && (kc.role === "kn_tinh" || kc.role === "kn_tw");
}
function create(ctx: Ctx): Response {
    const [id, exportID] = ctx.params as [string, string];
    const s = khtcMockStore().find(x => x.p.id === id), x = khtcReportExport(id, exportID);
    const kc=knContext(ctx.request.headers.get("X-AG-Context")??"");
    if(s?.p.report_type==="adhoc"){
      if(!kc || kc.read_only || !(kc.role==="admin"||kc.role==="kn_tinh"&&kc.department_code===KHTC&&s.adhocOwner===ctx.persona.me.actor_ref))return problem(403,"forbidden","Không có quyền xuất Word");
      if(s.adhocTenant!==ctx.persona.me.tenant_id)return problem(404,"extension.khtc_report_not_found","Không có báo cáo");
    }else if(!allowed(ctx,true))return problem(403,"forbidden","Chỉ cán bộ KHTC phụ trách được xuất Word");
    if (!s || !x)
        return problem(404, "extension.khtc_report_not_found", "Không có bản xuất");
    if (s.terminal === "cancelled")
        return problem(409, "extension.khtc_period_terminal", "Kỳ báo cáo đã hủy");
    const admin = (k: string) => x.detail.content.admin[k]?.override ?? x.detail.content.admin[k]?.system ?? "";
    const no = admin("document_no").trim(), day = admin("report_date").trim(), place = admin("place");
    const date = day ? placeDateLine(place, new Date(`${day}T05:00:00Z`)) : placeDateLine(place);
    let document: ReturnType<typeof renderKhtcMockFixed>;
    try { document=renderKhtcMockFixed(x.detail.content,x.meta.filter); } catch(e) { return problem(422,"extension.khtc_report_invalid",e instanceof Error?e.message:"Không dựng được nguồn mẫu đã ghim"); }
    const compiled=document?.nodes.flatMap(n=>[`${n.number}. ${n.title}`,...n.rows.flatMap(r=>[r.preamble??"",r.tokens.map(t=>t.text).join("")])])??[];
    const run = mockExportRun({ template: document ? "khtc.fixed" : `khtc.${x.detail.content.data.template}`, subject: subject(id), fileName: `bao-cao-khtc-${x.detail.content.data.template}-${id}-v${x.meta.version}.docx`,
        docNumber: no ? (no.includes("/") ? no : `${no}/BC-TTKN`) : x.meta.provisional ? "…/BC-TTKN" : "", series: x.meta.provisional ? undefined : "BC-TTKN", actor: ctx.persona.me.actor_ref,
        lines: [date, "BÁO CÁO", x.detail.content.data.period.name, `Bản xuất Excel phiên bản ${x.meta.version}`, ...compiled, ...x.detail.content.data.rows.map(r => `${r.name}: trong kỳ ${r.in_period}, lũy kế ${r.year_to_date}`),...(document ? [] : [...x.detail.content.data.metrics.map(m=>m.label+": "+(m.text??m.value.Increment??m.value.Sum??m.value.Cumulative??"")), ...Object.values(x.detail.content.texts)])] });
    const stored = mockExportBody(subject(id),run.id);
    if (!stored) return problem(503,"extension.khtc_dependency_unavailable","Không có byte đã lưu");
    bindArchiveMockWord({tenant:ctx.persona.me.tenant_id,period:id,exportID,exportName:x.detail.content.data.period.name,exportVersion:x.meta.version,body:stored.body,
      meta:{period_id:id,export_id:exportID,run_id:run.id,version:run.seq,template:run.template,template_version:run.template_version,template_hash:run.template_hash,input_hash:run.input_hash,body_hash:run.body_hash,size:new TextEncoder().encode(stored.body).length,file_name:run.file_name.replace(/\.docx$/,"-gia-lap.txt"),content_type:"text/plain; charset=utf-8",doc_number:run.doc_number,doc_number_auto:run.doc_number_auto,actor_ref:run.actor_ref,at:run.at}});
    return json(word(run), 201);
}
function read(ctx: Ctx): Response {
    const [id, runID] = ctx.params as [string, string | undefined];
    if (!allowed(ctx, false))
        return problem(403, "forbidden", "Không có quyền");
    if (!khtcMockStore().some(x => x.p.id === id))
        return problem(404, "extension.khtc_report_not_found", "Không có báo cáo");
    if (!runID)
        return json({ items: mockExportRuns(subject(id)).map(word) });
    const r = mockExportBody(subject(id), runID);
    return r ? downloadResponse(r) : problem(404, "export.run_not_found", "Không có lần xuất này");
}

function archiveRead(ctx: Ctx): Response {
 const [period,exportID,runID] = ctx.params as [string,string,string|undefined];
 const kc=knContext(ctx.request.headers.get("X-AG-Context")??"");
 if (!kc || !allowed(ctx,false)) return problem(403,"forbidden","Không có quyền");
 const p=khtcMockStore().find(p=>p.p.id===period && (p.adhocTenant??TENANT)===ctx.persona.me.tenant_id);
 if (!p || p.terminal==="cancelled" || !khtcReportExport(period,exportID)) return problem(404,"extension.khtc_report_not_found","Không có bản xuất");
 if (runID) {
   const b=archiveMockWord(ctx.persona.me.tenant_id,period,exportID,runID);
   return b ? new Response(b.body,{headers:{"Content-Type":b.meta.content_type,"Content-Disposition":`attachment; filename="${b.meta.file_name}"`,"Cache-Control":"no-store"}}) : problem(404,"extension.khtc_report_not_found","Không có Word đã xác minh");
 }
 const offset=ctx.url.searchParams.get("offset")??"0", limit=ctx.url.searchParams.get("limit")??"20";
 if (ctx.url.searchParams.getAll("offset").length>1 || ctx.url.searchParams.getAll("limit").length>1 || !/^\d{1,6}$/.test(offset) || +offset>100000 || !/^\d{1,2}$/.test(limit) || +limit<1 || +limit>50) return problem(422,"extension.khtc_report_invalid","Phân trang không hợp lệ");
 const items=archiveMockWords(ctx.persona.me.tenant_id,period,exportID), x=khtcReportExport(period,exportID)!;
 return json({period_id:period,export_id:exportID,export_version:x.meta.version,export_name:x.detail.content.data.period.name,items:items.slice(+offset,+offset+(+limit)).map(b=>b.meta),has_more:items.length>+offset+(+limit),offset:+offset},200,{"Cache-Control":"no-store"});
}
