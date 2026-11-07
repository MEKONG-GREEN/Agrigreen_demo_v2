// FN-09 v4: regular only, current profile and personal NB07. FN10 and historical files keep legacy kinds.
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { hrmScaleAt } from "./kn-hrm";
import { hrmStaffInternals as H } from "./kn-hrm-staff";

type S = components["schemas"];
type Case = S["HrmPromoCase"];
type Kind = S["HrmPromoKind"];
type Status = S["HrmPromoStatus"];
type Staff = ReturnType<typeof H.staff>[number];
type Ev = Staff["events"][number];


interface PEvent { id: string; key: string; staff: string; kind: Kind; action: string; reason: string; award?: string; export_id?: string; decision_id?: string; at: string }
interface Export extends Omit<S["HrmPromoExportSummary"], "decision_no"> { items: (S["HrmPromoExportItem"] & { award_event_id?: string })[] }
interface Dec { d: Omit<S["HrmDecision"], "status" | "cancel_reason">; cancel?: string; steps: Record<string, string>; awards: string[] }
interface Store { ratings: S["HrmAnnualRating"][]; events: PEvent[]; exports: Export[]; decs: Dec[]; scans: number }
let ST: Store = { ratings: [], events: [], exports: [], decs: [], scans: 0 };
const uuid = () => crypto.randomUUID();
const now = () => new Date().toISOString();
const vnToday = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);

/** Đặt lại dữ liệu nâng bậc giả lập (kiểm thử). */
export function resetHrmPromotion(): void {
  ST = { ratings: [], events: [], exports: [], decs: [], scans: 0 };
  const vc1 = H.staff().find((s) => s.code === "VC-00001");
  const y = Number(vnToday().slice(0, 4));
  if (vc1) for (let i = 5; i >= 1; i--) ST.ratings.push(rating(vc1.id, y - i + 1, i % 2 ? "HTNV" : "HTTNV"));
}

function rating(staff: string, year: number, r: string, reason = "", sup: string | null = null): S["HrmAnnualRating"] & { staff: string } {
  return { id: uuid(), staff, year, rating: r, source: "conversion", void: false, supersedes_id: sup, reason, actor: "01900000-0000-7000-8000-00000000a001", recorded_at: now() } as S["HrmAnnualRating"] & { staff: string };
}

function addMonths(iso: string, m: number): string {
  const [y, mo, d] = iso.split("-").map(Number) as [number, number, number];
  const first = new Date(Date.UTC(y, mo - 1 + m, 1));
  const last = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)).getUTCDate();
  first.setUTCDate(Math.min(d, last));
  return first.toISOString().slice(0, 10);
}
const fmt = (iso: string) => `${iso.slice(8, 10)}/${iso.slice(5, 7)}/${iso.slice(0, 4)}`;

function caseState(key: string): PEvent | undefined {
  return [...ST.events].reverse().find((e) => e.key === key);
}
function statusOf(key: string, kind: Kind): { s: Status; reason: string; note: string; gone: boolean } {
  const independent=ST.decs.some(d=>!d.cancel&&d.d.kind==="nltx"&&d.d.people.some(p=>(p.case_key||`${p.staff_id}:nltx:b${p.to_step}-${p.effective_on.replaceAll("-","")}`)===key));
  if(independent)return {s:"decided",reason:"",note:"",gone:false};
  const e = caseState(key);
  if (!e) return { s: "pending", reason: "", note: "", gone: false };
  const map: Record<string, Status> = { include:"pending", propose:"pending", exclude:"pending", export:"exported", undecide:ST.events.some(x=>x.key===key&&x.action==="export")?"exported":"pending", decide:"decided", return:"exported" };
  void kind;
  const inc = [...ST.events].reverse().find((x) => x.key === key && (x.action === "include" || x.action === "propose"));
  return { s: map[e.action] ?? "pending", reason: inc?.reason ?? "", note: e.action === "return" ? e.reason : "", gone: e.action === "withdraw" };
}

function blank(st: Staff, snap: S["HrmSnapshot"], kind: Kind): Case {
  return {
    promotion_months:snap.promotion_months ?? 0, key: "", staff_id: st.id, code: st.code, name: st.profile.full_name, unit: snap.unit, unit_name: H.units().find((u) => u.code === snap.unit)?.name ?? snap.unit,
    kind, form: kind === "nltx" ? "mau1" : kind === "nlth" ? "mau2" : "mau4", due: "", grade: snap.grade, grade_name: snap.grade, steps: snap.steps,
    from_step: snap.step, to_step: snap.step, from_coeff: snap.coefficient, to_coeff: snap.coefficient, diff: "0.00", step_since: snap.step_since || null,
    reserved: snap.reserved_coeff, next_due: null, normal_due: null, early_months: 0, pct_from: null, pct_to: null, pct_since: null, cond: "ok", why: [],
    basis: [], planned_year: 0, blocked: [], status: "pending", reason: "", note: "", months_enjoyed: "1", decision_id: null, decision_no: "", award: null,
  };
}

function compute(day:string): {cands:Case[];activeVC:number;missing:S["HrmPersonBrief"][]} {
  const cands:Case[]=[], missing:S["HrmPersonBrief"][]=[];
  let activeVC=0;
  for (const st of H.staff()) {
    const snap=H.project(st.events,day);
    if (st.type!=="vien_chuc" || snap.status!=="active") continue;
    activeVC++;
    const sc=snap.grade?hrmScaleAt(snap.grade,day):null;
    if (sc && snap.step>=sc.steps) continue;
    const months=snap.promotion_months??0;
    if (!sc || !snap.step || !/^\d{4}-\d{2}-\d{2}$/.test(snap.step_since) || months<1 || months>120) {
      missing.push({id:st.id,code:st.code,type:st.type,name:st.profile.full_name,unit:snap.unit,unit_name:snap.unit,position:snap.position});continue;
    }
    const due=addMonths(snap.step_since,months), c=blank(st,snap,"nltx");
    Object.assign(c,{key:`${st.id}:nltx:b${snap.step+1}-${due.replaceAll("-","")}`,due,next_due:due,to_step:snap.step+1,to_coeff:sc.coefficients[snap.step]??"",
      diff:(Number(sc.coefficients[snap.step])-Number(sc.coefficients[snap.step-1])).toFixed(2),months_enjoyed:Number(due.slice(8))<=15?"1":"0.5"});
    c.basis=[{label:"Ngày hưởng bậc hiện tại",value:fmt(snap.step_since),result:"ok",note:""},{label:"Thời gian xét nâng lương",value:`${months} tháng (${fmt(snap.step_since)} → ${fmt(due)})`,result:"ok",note:""}];
    const status=statusOf(c.key,c.kind);Object.assign(c,{status:status.s,reason:status.reason,note:status.note});
    cands.push(c);
  }
  return {cands,activeVC,missing};
}

function caller(ctx: Ctx, write = false) {
  const c = H.caller(ctx);
  if (c instanceof Response) return c;
  if ((c.m.export ?? "none") !== "all") return problem(403, "forbidden", "nâng bậc: chỉ Cấp Tỉnh – HCTH, Admin (FN-09, FN-10)");
  if (write && (c.readOnly || (c.m["profile.edit"] ?? "none") !== "all")) return problem(403, "forbidden", "nâng bậc: cần quyền sửa hồ sơ toàn Trung tâm");
  return c;
}
const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
function pevent(c: { ref: string }, e: Omit<PEvent, "id" | "at">) {
  ST.events.push({ ...e, id: uuid(), at: now() });
  void c;
}
function decided(day: string, from: string, to: string): Case[] {
  const out: Case[] = [];
  for (const x of ST.decs) {
    if (x.cancel || x.d.kind!=="nltx") continue;
    for (const p of x.d.people) {
      if (p.effective_on < from || p.effective_on > to) continue;
      const st = H.staff().find((s) => s.id === p.staff_id);
      if (!st) continue;
      const c = blank(st, H.project(st.events, day), x.d.kind);
      Object.assign(c, { key: p.case_key || `${p.staff_id}:${x.d.kind}:b${p.to_step}-${p.effective_on.replaceAll("-", "")}`, due: p.effective_on, from_step: p.from_step, to_step: p.to_step,
        from_coeff: p.from_coeff, to_coeff: p.to_coeff, pct_from: p.pct_from || null, pct_to: p.pct_to || null, status: "decided", decision_id: x.d.id, decision_no: x.d.decision_no });
      out.push(c);
    }
  }
  return out;
}
const xlsx = (name: string, extra: Record<string,string> = {}) => new Response(new Blob([`Tệp giả lập ${name}`]), {status:200,headers:{"Content-Type":"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","Cache-Control":"no-store","Content-Disposition":`attachment; filename="${name}"`,...extra}});

function decisionOut(x: Dec, full: boolean): S["HrmDecision"] {
  return { ...x.d, status: x.cancel ? "cancelled" : "applied", cancel_reason: x.cancel ?? null, ...(full ? {} : { history: undefined }) } as S["HrmDecision"];
}

export const hrmPromotionRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const day=vnToday(), p=ctx.url.searchParams;
      for(const [k,max] of [["year",9999],["month",12]] as const){const raw=p.get(k);if(raw!==null&&(!/^[0-9]{1,4}$/.test(raw)||Number(raw)<1||Number(raw)>max))return validation([{field:k,reason:"Số nguyên ngoài phạm vi"}]);}
      if(p.has("status")&&!["pending","exported","decided"].includes(p.get("status")!))return validation([{field:"status",reason:"Trạng thái không hợp lệ"}]);
      const {cands,missing}=compute(day);
      const items=[...cands.filter(x=>x.status!=="decided"),...decided(day,"0001-01-01","9999-12-31")]
        .filter(x=>(!p.get("year")||Number(x.due.slice(0,4))===Number(p.get("year")))&&(!p.get("month")||Number(x.due.slice(5,7))===Number(p.get("month")))&&(!p.get("status")||x.status===p.get("status"))&&(!p.get("unit")||x.unit===p.get("unit"))&&(!p.get("q")||x.name.toLowerCase().includes(p.get("q")!.toLowerCase())))
        .sort((a,b)=>a.due.localeCompare(b.due)||a.code.localeCompare(b.code));
      const out:S["HrmPromotionList"]={as_of:day,items,missing};
      return json(out, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/cases\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const key = decodeURIComponent(ctx.params[0]!);
      const day = vnToday();
      const x = compute(day).cands.find((k) => k.key === key) ?? decided(day, "1900-01-01", "2200-01-01").find((k) => k.key === key);
      if (!x) return problem(404, "hrm.promotion_case_not_found", "Không có kỳ nâng bậc này (đã xử lý hoặc dữ liệu hồ sơ đã đổi)");
      const out: S["HrmPromoCaseDetail"] = {
        ...x,
        history: ST.events.filter((e) => e.key === key).map((e) => ({ id: e.id, action: e.action as S["HrmPromoEvent"]["action"], due_on: x.due, export_id: e.export_id ?? null, decision_id: e.decision_id ?? null, reason: e.reason, actor: c.ref, recorded_at: e.at })),
      };
      return json(out, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/exports$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const items: S["HrmPromoExportSummary"][] = ST.exports.map((x) => ({
        id: x.id, form: x.form, review_month: x.review_month, as_of: x.as_of, count: x.count, file_name: x.file_name, sha256: x.sha256, byte_size: x.byte_size,
        actor: x.actor, recorded_at: x.recorded_at, signer_title: x.signer_title, preparer_id: x.preparer_id,
        decision_no: ST.decs.find((d) => !d.cancel && d.d.export_id === x.id)?.d.decision_no ?? "",
      }));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/exports$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<S["HrmPromoExportInput"]>(ctx);
      if (!/^\d{4}-\d{2}$/.test(b.review_month ?? "")) return validation([{ field: "review_month", reason: "YYYY-MM" }]);
      if(!b.preparer_id) return validation([{field:"preparer_id",reason:"Chọn người lập"}]);
      const day = vnToday();
      const { cands } = compute(day);
      const sel = (b.keys ?? []).map((k) => cands.find((x) => x.key === k));
      if (sel.some((x) => !x)) return problem(404, "hrm.promotion_case_not_found", "Không có kỳ nâng bậc này");
      if(!sel.length || sel.some(x=>x!.kind!=="nltx"))return validation([{field:"keys",reason:"Chọn người nâng thường xuyên"}]);
      for(const [field,max,len,dp] of [["months",12,4,1],["reserved",100,6,2],["reserved_after",100,6,2]] as const){
        for(const raw of Object.values(b[field]??{})){if(raw!==""&&(raw.length>len||!/^\d+(?:[.,]\d{1,2})?$/.test(raw)||Number(raw.replace(",","."))>max||(raw.split(/[.,]/)[1]?.length??0)>dp))return validation([{field,reason:"Số thập phân thường ngoài phạm vi"}]);}
      }
      const out: S["HrmPromoExported"][] = [];
      for (const form of ["mau1"] as const) {
        const rows = sel.filter((x) => x!.form === form) as Case[];
        if (!rows.length) continue;
        const id = uuid();
        const file_name = `MauSo${form.slice(3)}_T${b.review_month.slice(5)}_${b.review_month.slice(0, 4)}.xlsx`;
        ST.exports.unshift({ id, form, review_month: b.review_month, as_of: day, count: rows.length, file_name, sha256: "0".repeat(64), byte_size: 4096, actor: c.ref, recorded_at: now(),
          signer_title: b.signer_title ?? "Giám đốc", preparer_id: b.preparer_id,
          items: rows.map((x) => ({ case_key: x.key, staff_id: x.staff_id, code: x.code, name: x.name, kind: x.kind, due: x.due, unit_name: x.unit_name, grade: x.grade, grade_name: x.grade_name,
            from_step: x.from_step, to_step: x.to_step, from_coeff: x.from_coeff, to_coeff: x.to_coeff, diff: x.diff, pct_from: x.pct_from ?? "", pct_to: x.pct_to ?? "",
            next_due: x.next_due ?? "", months_enjoyed: x.months_enjoyed, early_months: x.early_months, award_label: x.award?.label ?? "", note: b.notes?.[x.key] ?? "", reason: x.reason,
            status: "exported", ...(x.award ? { award_event_id: x.award.event_id } : {}) })) });
        for (const x of rows) pevent(c, { key: x.key, staff: x.staff_id, kind: x.kind, action: "export", reason: "", export_id: id, ...(x.award ? { award: x.award.event_id } : {}) });
        out.push({ id, form, file_name, sha256: "0".repeat(64), byte_size: 4096, count: rows.length, download: `/v1/hrm/promotion/exports/${id}/file` });
      }
      return json({ items: out }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/exports\/([^/]+)(\/file)?$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const x = ST.exports.find((e) => e.id === ctx.params[0]);
      if (!x) return problem(404, "hrm.promotion_export_not_found", "Không có lần xuất danh sách nâng bậc này");
      if (ctx.params[1]) return xlsx(x.file_name, { "X-Export-Drift": "false", "X-Export-Sha256": x.sha256 });
      const items = x.items.map((i) => ({ ...i, status: statusOf(i.case_key, i.kind).s }));
      return json({ ...x, items, decision_no: ST.decs.find((d) => !d.cancel && d.d.export_id === x.id)?.d.decision_no ?? "" }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decision-book$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const y = ctx.url.searchParams.get("year");
      return xlsx(y ? `SoQuyetDinhNangBac_${y}.xlsx` : "SoQuyetDinhNangBac.xlsx");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decisions$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const p = ctx.url.searchParams;
      const q = (p.get("q") ?? "").toLowerCase();
      const items = [...ST.decs]
        .reverse()
        .filter((x) => (!p.get("year") || x.d.decided_on.startsWith(p.get("year")!)) && (!p.get("kind") || x.d.kind === p.get("kind")) && (!p.get("issuer") || x.d.issuer === p.get("issuer")))
        .filter((x) => !q || x.d.decision_no.toLowerCase().includes(q) || x.d.people.some((pp) => pp.name.toLowerCase().includes(q)))
        .map((x) => decisionOut(x, false));
      return json({ items }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/decisions$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<S["HrmDecisionInput"]>(ctx);
      if (!b.decision_no?.trim() || !b.decided_on || !b.issuer || !b.people?.length)
        return validation([{ field: "decision_no", reason: "nhập số, ngày quyết định, cơ quan và người có tên" }]);
      if (!b.acknowledge_warnings && ST.decs.some((x) => !x.cancel && x.d.decision_no.toLowerCase() === b.decision_no.trim().toLowerCase()))
        return problem(409, "hrm.decision_warnings", "Quyết định có cảnh báo — gửi lại với acknowledge_warnings = true để lưu", { detail: `Số quyết định ${b.decision_no} đã có trong sổ` });
      const exp = b.export_id ? ST.exports.find((e) => e.id === b.export_id) : undefined;
      if (b.export_id && !exp) return problem(404, "hrm.promotion_export_not_found", "Không có lần xuất danh sách nâng bậc này");
      const kind: Kind = exp ? (exp.form === "mau1" ? "nltx" : exp.form === "mau2" ? "nlth" : "tnvk") : (b.kind ?? "nltx");
      const id = uuid();
      const dec: Dec = { d: { id, decision_no: b.decision_no.trim(), decided_on: b.decided_on, issuer: b.issuer, kind, export_id: b.export_id ?? null, source: exp?.file_name ?? "Nhập từ quyết định đã ban hành",
        count: b.people.length, doc_id: null, recorded_at: now(), actor: c.ref, people: [], history: [{ id: uuid(), action: "issue", doc_id: null, reason: "", actor: c.ref, recorded_at: now() }] }, steps: {}, awards: [] };
      for (const p of b.people) {
        const it = exp?.items.find((i) => i.case_key === p.case_key);
        const st = H.staff().find((s) => s.id === (it?.staff_id ?? p.staff_id));
        if (!st) return validation([{ field: "people", reason: "người không có trong danh sách đã xuất" }]);
        const eff = p.effective_on ?? it?.due ?? b.decided_on;
        const snap = H.project(st.events, eff);
        const sc = hrmScaleAt(snap.grade, eff);
        const to = it ? it.to_step : (p.to_step ?? snap.step);
        if (kind !== "tnvk" && (!sc || to < 1 || to > sc.steps)) return problem(422, "hrm.step_out_of_range", "Bậc vượt số bậc của ngạch hoặc ngạch chưa có bảng hệ số");
        dec.d.people.push({ staff_id: st.id, name: st.profile.full_name, case_key: it?.case_key ?? "", effective_on: eff, grade: snap.grade, from_step: snap.step, to_step: to,
          from_coeff: snap.coefficient, to_coeff: sc?.coefficients[to - 1] ?? snap.coefficient, pct_from: it?.pct_from ?? "", pct_to: it?.pct_to ?? p.pct_to ?? "" });
        if (kind !== "tnvk") {
          const ev = { ...st.events[st.events.length - 1]!, id: uuid(), kind: "step" as Ev["kind"], valid_from: eff, precision: "day" as const, valid_to: null, void: false, supersedes: null,
            reason: "", payload: { step: String(to), step_since: eff, decision_no: dec.d.decision_no }, version: st.events.length + 1000, recorded_at: now() };
          (st.events as Ev[]).push(ev);
          dec.steps[st.id] = ev.id;
        }
        if (it?.award_event_id) dec.awards.push(it.award_event_id);
        if (it) pevent(c, { key: it.case_key, staff: st.id, kind, action: "decide", reason: "", decision_id: id });
      }
      const returned = 0;
      ST.decs.push(dec);
      return json({ id, updated: kind === "tnvk" ? 0 : dec.d.people.length, returned, warnings: [] }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decisions\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const x = ST.decs.find((d) => d.d.id === ctx.params[0]);
      return x ? json(decisionOut(x, true), 200, { "Cache-Control": "no-store" }) : problem(404, "hrm.decision_not_found", "Không có quyết định nâng bậc này");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/decisions\/([^/]+)\/(cancel|file)$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const x = ST.decs.find((d) => d.d.id === ctx.params[0]);
      if (!x) return problem(404, "hrm.decision_not_found", "Không có quyết định nâng bậc này");
      if (x.cancel) return problem(409, "hrm.decision_cancelled", "Quyết định đã bị hủy");
      if (ctx.params[1] === "file") {
        x.d.doc_id = uuid();
        x.d.history!.push({ id: uuid(), action: "attach", doc_id: x.d.doc_id, reason: "", actor: c.ref, recorded_at: now() });
        return json({ doc_id: x.d.doc_id }, 201);
      }
      const reason = body<{ reason?: string }>(ctx).reason?.trim() ?? "";
      if (reason.length < 10) return validation([{ field: "reason", reason: "hủy quyết định cần lý do 10–500 ký tự (BR-03)" }]);
      for (const [staff, evId] of Object.entries(x.steps)) {
        const st = H.staff().find((s) => s.id === staff);
        const ev = st?.events.find((e) => e.id === evId);
        if (st && ev) (st.events as Ev[]).push({ ...ev, id: uuid(), void: true, supersedes: ev.id, reason, version: ev.version + 1, recorded_at: now() });
      }
      x.cancel = reason;
      x.d.history!.push({ id: uuid(), action: "cancel", doc_id: null, reason, actor: c.ref, recorded_at: now() });
      for (const p of x.d.people) if (p.case_key) pevent(c, { key: p.case_key, staff: p.staff_id, kind: x.d.kind, action: "undecide", reason, decision_id: x.d.id });
      return json({ id: x.d.id, status: "cancelled" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/ratings$/,
    handler: (ctx) => {
      const c = H.caller(ctx);
      if (c instanceof Response) return c;
      return json({ items: (ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]).filter((r) => r.staff === ctx.params[0]) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/ratings$/,
    handler: (ctx) => {
      const c = H.caller(ctx);
      if (c instanceof Response) return c;
      if ((c.m["profile.edit"] ?? "none") === "none" || c.readOnly) return problem(403, "forbidden", "hồ sơ nhân sự: không có quyền profile.edit (FN-20)");
      const b = body<{ year: number; rating: string; source?: string; reason?: string }>(ctx);
      if (!(b.year >= 1990 && b.year <= 2100)) return validation([{ field: "year", reason: "năm 1990–2100" }]);
      const staff = ctx.params[0]!;
      const cur = (ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]).filter((r) => r.staff === staff && r.year === b.year).pop();
      if (cur && (b.reason ?? "").trim().length < 10) return validation([{ field: "reason", reason: "sửa/hủy xếp loại đã có cần lý do 10–500 ký tự" }]);
      const r = rating(staff, b.year, b.rating, cur ? b.reason!.trim() : "", cur?.id ?? null);
      ST.ratings.push(r);
      return json(r, 201);
    },
  },
];

resetHrmPromotion();
