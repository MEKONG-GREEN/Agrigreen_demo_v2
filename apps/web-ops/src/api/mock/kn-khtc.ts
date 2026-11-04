// ============================================================================
// BACKEND GIẢ LẬP — Phòng Kế hoạch – Tài chính (W11.61, pf/extension — /v1/kn/khtc/*). Đường dẫn, tham số, hình dạng
// phản hồi theo ĐÚNG contracts/openapi/application.yaml. Quy tắc mô phỏng (rút gọn từ internal/platform/extension/khtc):
//   - Đọc: Admin, Cấp Tỉnh mọi phòng. Danh mục: Admin ghi. CT/DA + kế hoạch: Admin, Cấp Tỉnh phòng KHTC ghi.
//   - Mỗi thay đổi là một phiên bản; ngừng = active false; trùng mã → 409.
//   - Kế hoạch: base_version khác phiên bản mới nhất → 409; điều chỉnh thiếu số/ngày QĐ/ngày hiệu lực → 422;
//     hạng mục thiếu phòng phụ trách → 422; số La Mã theo thứ tự CT ở tab Các chương trình.
//   - Xuất Excel trả tệp giả; nhập Excel không đọc tệp (422) — chỉ máy chủ thật đọc .xlsx.
// Dữ liệu GIẢ LẬP của họ kế hoạch được giữ trong sessionStorage của tab; không phải dữ liệu máy chủ thật.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore } from "./kn-khtc-periods";
import { hex64 } from "./g5";

type S = components["schemas"];
type Item = S["KhtcCatalogItem"];
type Program = S["KhtcProgram"];
type Entry = S["KhtcPlanEntry"];
type Version = S["KhtcPlanVersion"];
type EntryIn = S["KhtcPlanEntryInput"];

const KINDS = ["report_type", "unit", "field", "funding_source", "signer", "recipient", "basis"] as const;
const LABEL: Record<string, string> = {
  report_type: "Loại báo cáo",
  unit: "Đơn vị tính",
  field: "Lĩnh vực",
  funding_source: "Nguồn kinh phí",
  signer: "Người ký",
  recipient: "Nơi nhận",
  basis: "Căn cứ",
};
const KHTC = "ke_hoach_tai_chinh";
const ACTOR = { ref: "01990000-0000-7000-8000-00000000a001", display: "Cán bộ KHTC (giả lập)" };
const now = () => new Date().toISOString();
const uuid = () => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `0199${Math.random().toString(16).slice(2, 10)}-0000-7000-8000-000000000000`);

interface Store {
  items: Map<string, Item[]>;
  programs: Map<string, Program[]>;
  plans: Map<number, { v: Version; entries: Entry[] }[]>;
}

function seed(): Store {
  const s: Store = { items: new Map(), programs: new Map(), plans: new Map() };
  const put = (kind: string, code: string, name: string, attrs: Record<string, string> = {}, default_for: string[] = []) =>
    s.items.set(`${kind}:${code}`, [
      { kind: kind as Item["kind"], code, name, sort_order: 0, active: true, default_for, attrs, version: 1, version_id: uuid(), recorded_at: now(), actor: ACTOR },
    ]);
  put("unit", "ha", "Héc ta", { symbol: "ha" });
  put("unit", "mo_hinh", "Mô hình");
  put("unit", "lop", "Lớp tập huấn");
  put("field", "trong_trot", "Trồng trọt");
  put("field", "chan_nuoi", "Chăn nuôi");
  put("funding_source", "ns_tinh", "Ngân sách tỉnh");
  put("funding_source", "ns_tw", "Ngân sách Trung ương");
  put("signer", "giam_doc", "Nguyễn Văn A (giả lập)", { title: "Giám đốc" }, ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"]);
  put("report_type", "tuan", "Báo cáo tuần");
  const prog = (code: string, name: string, kind: Program["kind"], field: string | null) => {
    const ref = uuid();
    s.programs.set(ref, [
      { ref, code, name, kind, field_code: field, owner_department: null, start_year: null, end_year: null, active: true, note: null, version: 1, recorded_at: now(), actor: ACTOR },
    ]);
    return ref;
  };
  const p1 = prog("CT-I", "Chương trình khuyến nông trồng trọt", "program", "trong_trot");
  const p2 = prog("CT-II", "Chương trình khuyến nông chăn nuôi", "program", "chan_nuoi");
  const p3 = prog("DA-TW-01", "Dự án KN TW nuôi tôm ít thay nước", "central_project", null);
  const year = new Date(Date.now() + 7 * 3_600_000).getUTCFullYear();
  const e = (p: Partial<Entry> & Pick<Entry, "tab" | "kind" | "name">): Entry => ({
    ref: uuid(),
    parent_ref: null,
    program_ref: null,
    program_code: null,
    program_name: null,
    roman: null,
    sort_order: 10,
    code: null,
    unit_code: null,
    planned_qty: null,
    scale_note: null,
    budget: null,
    funding_code: null,
    field_code: null,
    department_code: null,
    communes: [],
    points_count: null,
    data_source: p.kind === "item" ? "form" : null,
    source_key: null,
    tracking: "tracking",
    stopped_from: null,
    stop_decision: null,
    note: null,
    ...p,
  });
  const c1 = e({ tab: "programs", kind: "program", name: "Chương trình khuyến nông trồng trọt", program_ref: p1, sort_order: 1 });
  const g1 = e({ tab: "programs", kind: "group", name: "Xây dựng mô hình", parent_ref: c1.ref, program_ref: p1 });
  const i1 = e({ tab: "programs", kind: "item", name: "Mô hình lúa chất lượng cao", parent_ref: g1.ref, program_ref: p1, unit_code: "ha", planned_qty: "120.5", budget: "350000000", funding_code: "ns_tinh", department_code: "trong_trot", communes: ["30001"] });
  const c2 = e({ tab: "programs", kind: "program", name: "Chương trình khuyến nông chăn nuôi", program_ref: p2, sort_order: 2 });
  const i2 = e({ tab: "programs", kind: "item", name: "Tập huấn chăn nuôi an toàn sinh học", parent_ref: c2.ref, program_ref: p2, unit_code: "lop", planned_qty: "12", department_code: "chan_nuoi_thuy_san" });
  const c3 = e({ tab: "central_project", kind: "program", name: "Dự án KN TW nuôi tôm ít thay nước", program_ref: p3 });
  const i3 = e({ tab: "central_project", kind: "item", name: "Điểm trình diễn", parent_ref: c3.ref, program_ref: p3, unit_code: "mo_hinh", planned_qty: "3", department_code: "chan_nuoi_thuy_san", data_source: "module", source_key: "cnts.demo_points" });
  const entries = [c1, g1, i1, c2, i2, c3, i3];
  s.plans.set(year, [
    {
      v: { id: uuid(), year, version: 1, basis: "initial", decision_no: "123/QĐ-SNN", decision_date: `${year}-01-05`, effective_from: `${year}-01-01`, copied_from_year: null, note: null, entries_count: entries.length, recorded_at: now(), actor: ACTOR },
      entries,
    },
  ]);
  return s;
}

let STORE: Store = seed();
/** Kiểm thử: đặt lại dữ liệu giả lập. */
export function resetKhtcMock(): void {
  window.sessionStorage.removeItem(STORAGE_KEY225);
  STORE = seed();
  INITIAL225.clear();RECEIPTS225.clear();persistedRaw225=null;
}

const last = <T>(xs: T[]): T => xs[xs.length - 1]!;

function access(ctx: Ctx): { catalog: boolean; plan: boolean } | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc || (kc.role !== "admin" && kc.role !== "kn_tinh")) return problem(403, "forbidden", "Chỉ Admin và Cấp Tỉnh xem dữ liệu KHTC");
  if (kc.read_only) return { catalog: false, plan: false };
  if (kc.role === "admin") return { catalog: true, plan: true };
  return { catalog: false, plan: kc.department_code === KHTC };
}
function need(ctx: Ctx, what: "catalog" | "plan"): Response | null {
  const a = access(ctx);
  if (a instanceof Response) return a;
  return a[what] ? null : problem(403, "forbidden", what === "catalog" ? "Chỉ Admin quản lý Danh mục KHTC" : "Chỉ Phòng KHTC lập, điều chỉnh kế hoạch năm và CT/DA");
}

function roman(n: number): string {
  const v = [1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1];
  const r = ["M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"];
  let out = "";
  v.forEach((x, i) => {
    while (n >= x) {
      out += r[i];
      n -= x;
    }
  });
  return out;
}
function decorate(entries: Entry[]): Entry[] {
  const progs = entries
    .map((e, i) => ({ e, i }))
    .filter((x) => x.e.kind === "program" && x.e.tab === "programs")
    .sort((a, b) => a.e.sort_order - b.e.sort_order || a.i - b.i);
  const num = new Map(progs.map((x, i) => [x.e.ref, roman(i + 1)]));
  return entries.map((e) => {
    const p = e.program_ref ? STORE.programs.get(e.program_ref) : undefined;
    return { ...e, roman: num.get(e.ref) ?? null, program_code: p ? last(p).code : null, program_name: p ? last(p).name : null };
  });
}
function planBody(year: number, idx?: number): Response {
  const vs = STORE.plans.get(year);
  if (!vs?.length || (idx !== undefined && (idx < 1 || idx > vs.length))) return problem(404, "extension.khtc_plan_not_found", "Chưa có kế hoạch năm (hoặc phiên bản) này");
  const p = vs[(idx ?? vs.length) - 1]!;
  return json({ year, version: p.v, latest_version: vs.length, entries: decorate(p.entries) });
}

function catalogWrite(ctx: Ctx, kind: string, code: string, create: boolean): Response {
  const deny = need(ctx, "catalog");
  if (deny) return deny;
  if (!(KINDS as readonly string[]).includes(kind)) return problem(422, "validation_failed", "Loại danh mục không hợp lệ");
  const b = (ctx.body ?? {}) as S["KhtcCatalogInput"];
  const c = (create ? (b.code ?? "") : code).trim().toLowerCase();
  if (!/^[a-z0-9][a-z0-9._-]{0,39}$/.test(c) || !b.name?.trim()) return problem(422, "validation_failed", "Mã, tên không hợp lệ", { errors: [{ field: "code", reason: "bắt buộc" }] });
  const key = `${kind}:${c}`;
  const vs = STORE.items.get(key);
  if (create && vs) return problem(409, "extension.khtc_catalog_exists", "Mã đã có trong danh mục (kể cả mục đã ngừng dùng)");
  if (!create && !vs) return problem(404, "extension.khtc_catalog_not_found", "Không có mục danh mục KHTC");
  const active = b.active ?? true;
  const df = [...new Set(b.default_for ?? [])].sort();
  if (kind === "signer" && active)
    for (const [k, xs] of STORE.items) {
      const o = last(xs);
      if (k !== key && o.kind === "signer" && o.active && o.default_for.some((t) => df.includes(t)))
        return problem(409, "extension.khtc_default_conflict", "Mẫu đã có người ký mặc định đang dùng");
    }
  const it: Item = {
    kind: kind as Item["kind"],
    code: c,
    name: b.name.trim(),
    sort_order: b.sort_order ?? 0,
    active,
    default_for: df,
    attrs: b.attrs ?? {},
    version: (vs?.length ?? 0) + 1,
    version_id: uuid(),
    recorded_at: now(),
    actor: ACTOR,
  };
  const items=new Map(STORE.items);items.set(key,[...(vs??[]),it]);
  const failed=commitState225({...STORE,items});if(failed)return failed;
  return json(it, 201);
}

function programWrite(ctx: Ctx, ref: string | null): Response {
  const deny = need(ctx, "plan");
  if (deny) return deny;
  const b = (ctx.body ?? {}) as S["KhtcProgramInput"];
  if (!b.code?.trim() || !b.name?.trim()) return problem(422, "validation_failed", "Mã, tên bắt buộc");
  const id = ref ?? uuid();
  const vs = STORE.programs.get(id);
  if (ref && !vs) return problem(404, "extension.khtc_program_not_found", "Không có CT/DA trong danh sách KHTC");
  for (const [k, xs] of STORE.programs) if (k !== id && last(xs).code.toLowerCase() === b.code.trim().toLowerCase()) return problem(409, "extension.khtc_program_exists", "Mã CT/DA đã có trong danh sách");
  const p: Program = {
    ref: id,
    code: b.code.trim(),
    name: b.name.trim(),
    kind: b.kind,
    field_code: b.field_code || null,
    owner_department: b.owner_department || null,
    start_year: b.start_year || null,
    end_year: b.end_year || null,
    active: b.active ?? true,
    note: b.note || null,
    version: (vs?.length ?? 0) + 1,
    recorded_at: now(),
    actor: ACTOR,
  };
  const programs=new Map(STORE.programs);programs.set(id,[...(vs??[]),p]);
  const failed=commitState225({...STORE,programs});if(failed)return failed;
  return json(p, 201);
}

function materializePlanRows225(input: EntryIn[]) {
  const errors: { field: string; reason: string }[] = [];
  const byRef = new Map<string, EntryIn>();
  const entries: Entry[] = input.map((x, i) => {
    if (x.kind === "item" && !x.department_code) errors.push({ field: `entries.${i}.department_code`, reason: "hạng mục có đúng một phòng phụ trách" });
    if (x.kind !== "program" && (!x.parent_ref || !byRef.has(x.parent_ref))) errors.push({ field: `entries.${i}.parent_ref`, reason: "cha phải đứng trước" });
    byRef.set(x.ref, x);
    const parent = x.parent_ref ? byRef.get(x.parent_ref) : undefined;
    let pref = x.kind === "program" ? (x.program_ref ?? null) : null;
    for (let p = parent; !pref && p; p = p.parent_ref ? byRef.get(p.parent_ref) : undefined) pref = p.program_ref ?? null;
    const prog = pref ? STORE.programs.get(pref) : undefined;
    return {
      ref: x.ref,
      tab: x.tab,
      kind: x.kind,
      parent_ref: x.parent_ref ?? null,
      program_ref: pref,
      program_code: null,
      program_name: null,
      roman: null,
      sort_order: x.sort_order ?? 0,
      code: x.code || null,
      name: x.name?.trim() || (prog ? last(prog).name : ""),
      unit_code: x.unit_code || null,
      planned_qty: x.planned_qty ?? null,
      scale_note: x.scale_note || null,
      budget: x.budget ?? null,
      funding_code: x.funding_code || null,
      field_code: x.field_code || null,
      department_code: x.department_code || null,
      communes: x.communes ?? [],
      points_count: x.points_count ?? null,
      data_source: x.kind === "item" ? ((x.data_source || "form") as "form" | "module") : null,
      source_key: x.source_key || null,
      tracking: (x.tracking || "tracking") as "tracking" | "stopped",
      stopped_from: x.stopped_from ?? null,
      stop_decision: x.stop_decision || null,
      note: x.note || null,
    };
  });
  return {entries,errors};
}

function savePlan(ctx: Ctx, year: number): Response {
  const deny = need(ctx, "plan");
  if (deny) return deny;
  if(hasActiveInitialDraft225(year,ctx.persona.me.tenant_id))return problem(409,"extension.khtc_initial_draft_active","Đã có nháp máy chủ của năm này");
  const b = (ctx.body ?? {}) as S["KhtcPlanSave"];
  const vs = STORE.plans.get(year) ?? [];
  if (b.base_version !== vs.length) return problem(409, "extension.khtc_plan_version_conflict", "Kế hoạch đã có phiên bản mới hơn — tải lại rồi điều chỉnh", { latest_version: vs.length });
  if (vs.length && (!b.decision_no || !b.decision_date || !b.effective_from))
    return problem(422, "extension.khtc_effective_date_invalid", "Điều chỉnh theo QĐ bắt buộc số, ngày quyết định và ngày hiệu lực");
  const eff = b.effective_from || `${year}-01-01`;
  if (!eff.startsWith(String(year))) return problem(422, "extension.khtc_effective_date_invalid", "Ngày hiệu lực trong năm kế hoạch");
  const {entries,errors}=materializePlanRows225(b.entries??[]);
  if (errors.length) return problem(422, "extension.khtc_plan_invalid", "Kế hoạch năm không hợp lệ", { errors });
  const v: Version = {
    id: uuid(),
    year,
    version: vs.length + 1,
    basis: vs.length ? "adjust" : "initial",
    decision_no: b.decision_no || null,
    decision_date: b.decision_date || null,
    effective_from: eff,
    copied_from_year: b.copied_from_year || null,
    note: b.note || null,
    entries_count: entries.length,
    recorded_at: now(),
    actor: ACTOR,
  };
  const plans=new Map(STORE.plans);plans.set(year,[...vs,{v,entries}]);
  const failed=commitState225({...STORE,plans});if(failed)return failed;
  return planBodyCreated(year);
}
function planBodyCreated(year: number): Response {
  const vs = STORE.plans.get(year)!;
  const p = last(vs);
  return json({ year, version: p.v, latest_version: vs.length, entries: decorate(p.entries) }, 201);
}

const year = (ctx: Ctx) => Number.parseInt(ctx.params[0]!, 10);


// These exports are lazy reads. Neither module's initialization calls its peer.
export function hasPublishedOwnerPlan225(year:number,cutoff:string):boolean {
 return (STORE.plans.get(year)??[]).some(x=>/^\d{4}-\d{2}-\d{2}$/.test(x.v.effective_from)&&x.v.effective_from<=cutoff);
}
type Draft225=S["KhtcInitialDraft225"];
type State225={revision:number;draft:Draft225|null;events:Draft225[]};
let INITIAL225=new Map<string,State225>();
type Receipt225={authority:string;request:string;receipt:Draft225};
let RECEIPTS225=new Map<string,Receipt225>();
const draftKey225=(tenant:string,year:number)=>`${tenant}:${year}`;
export function hasActiveInitialDraft225(year:number,tenant:string):boolean{return !!INITIAL225.get(draftKey225(tenant,year))?.draft;}
export function isInitialDraftMockPath225(path:string){return /^\/v1\/kn\/khtc\/plans\/[^/]+\/draft(?:\/(?:discard|history|compare))?\/?$/.test(path);}
/** Current policy and complete available mock authority facts. Context identity/expiry alone are ephemeral. */
export function initialDraftMockPolicy225(request:Request,persona:Ctx["persona"]):{authority:string;denied:Response|null}{
 const k=knContext(request.headers.get("X-AG-Context")??"");
 const deny=(code="forbidden")=>({authority:"",denied:problem(403,code,"Không có quyền nháp máy chủ trong phiên hiện tại")});
 if(!k||!(k.role==="admin"||k.role==="kn_tinh"&&k.department_code===KHTC))return deny();
 if(!Number.isFinite(Date.parse(k.expires_at))||Date.parse(k.expires_at)<=Date.now())return deny("accounts.context_expired");
 if((k.role==="admin"||k.support_session_id)&&!persona.amr.includes("otp"))return deny("mfa_required");
 if(request.method!=="GET"&&request.method!=="HEAD"&&k.read_only)return deny("accounts.context_read_only");
 const authority=JSON.stringify([persona.me.tenant_id,persona.me.actor_ref,[...persona.me.roles].sort(),[...persona.amr].sort(),k.assignment_id,k.role,k.department_code,k.org_ref,k.communes.map(x=>x.code).sort(),!!k.read_only,!!k.manager,k.mgmt_level_code,k.province_group,k.internal_unit_code,[...(k.duties??[])].sort(),[...(k.powers??[])].sort(),k.support_session_id,k.on_behalf_of]);
 return {authority,denied:restoreState225()};
}
function currentState225(ctx:Ctx,year:number){
 const stored=INITIAL225.get(draftKey225(ctx.persona.me.tenant_id,year))??{revision:0,draft:null,events:[]};
 // Every owned status counts, including cancelled/closed; never infer absence from a page.
 const referenced=khtcMockStore().some(x=>x.p.year===year);
 const available=!(STORE.plans.get(year)?.length)&&!referenced;
 return {stored,body:{year,revision:stored.revision,draft:stored.draft?structuredClone(stored.draft):null,available,unavailable_reason:available?null:"Năm đã có phiên bản hoặc tham chiếu kỳ"}};
}
const plain225=(x:unknown):x is Record<string,unknown>=>!!x&&typeof x==="object"&&!Array.isArray(x);
const uuid225=(x:unknown)=>typeof x==="string"&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(x)&&x!=="00000000-0000-0000-0000-000000000000";
function decimal225(x:unknown,scale:number,digits:number):boolean {
 if(x==null)return true;
 if(typeof x!=="string"||x.length>66||! /^-?[0-9]{1,32}(\.[0-9]{1,32})?$/.test(x))return false;
 const [whole,rawFraction=""]=x.replace(/^-/ ,"").split(".");const integer=whole!.replace(/^0+/,"")||"0",fraction=rawFraction.replace(/0+$/,"");
 return !(x.startsWith("-")&&(integer!=="0"||fraction!==""))&&integer.length<=digits&&fraction.length<=scale;
}
function boundedDraftRows225(input:unknown):input is EntryIn[]{
 if(!Array.isArray(input)||input.length>5000)return false;
 let bytes=0;const refs=new Map<string,EntryIn>();
 const fields=new Set(["ref","tab","kind","parent_ref","program_ref","sort_order","code","name","unit_code","planned_qty","scale_note","budget","funding_code","field_code","department_code","communes","points_count","data_source","source_key","tracking","stopped_from","stop_decision","note"]);
 for(const unknownRow of input){
  if(!plain225(unknownRow)||Object.keys(unknownRow).some(k=>!fields.has(k)))return false;
  const x=unknownRow as EntryIn;if(!uuid225(x.ref)||refs.has(x.ref)||!["programs","prev_year","central_project","cooperation","research","advisory"].includes(x.tab)||!["program","group","item"].includes(x.kind))return false;
  if(!Number.isSafeInteger(x.sort_order??0)||(x.sort_order??0)<0||(x.sort_order??0)>100000)return false;
  if(x.points_count!=null&&(!Number.isSafeInteger(x.points_count)||x.points_count<0||x.points_count>100000))return false;
  if(!decimal225(x.planned_qty,4,16)||!decimal225(x.budget,2,18))return false;
  for(const [key,max] of [["code",40],["name",1000],["unit_code",40],["scale_note",1000],["funding_code",40],["field_code",40],["department_code",40],["data_source",10],["source_key",100],["stop_decision",100],["note",2000]] as const){const value=x[key];if(value!=null&&(typeof value!=="string"||value.includes("\u0000")||Array.from(value).length>max))return false;}
  if(x.communes!=null&&(!Array.isArray(x.communes)||x.communes.length>500||x.communes.some(c=>typeof c!=="string"||c.includes("\u0000")||c.length>40)))return false;
  if(x.kind==="program"){if(x.parent_ref||!uuid225(x.program_ref)||!STORE.programs.get(x.program_ref!)?.at(-1)?.active)return false;}
  else {const parent=x.parent_ref?refs.get(x.parent_ref):null;if(!parent||parent.tab!==x.tab||parent.kind==="item"||x.kind==="group"&&parent.kind!=="program")return false;if(typeof x.name!=="string"||!x.name.trim())return false;}
  if(x.kind==="item"&&(!x.department_code||!["trong_trot","chan_nuoi_thuy_san","thong_tin_truyen_thong","hanh_chinh_tong_hop"].includes(x.department_code)))return false;
  bytes+=new TextEncoder().encode(JSON.stringify(x)).length;if(bytes>1<<20)return false;refs.set(x.ref,x);
 }
 return true;
}
function stableJSON225(x:unknown):string {if(Array.isArray(x))return `[${x.map(stableJSON225).join(",")}]`;if(plain225(x))return `{${Object.keys(x).sort().map(k=>`${JSON.stringify(k)}:${stableJSON225(x[k])}`).join(",")}}`;return JSON.stringify(x);}
function draftRequest225(ctx:Ctx,discard=false):Response {
 const policy=initialDraftMockPolicy225(ctx.request,ctx.persona);if(policy.denied)return policy.denied;
 const rawYear=ctx.params[0]??"";if(!/^(20\d{2}|2100)$/.test(rawYear))return problem(422,"validation_failed","Năm không hợp lệ");const year=Number(rawYear);
 const b=ctx.body;if(!plain225(b)||Object.keys(b).some(k=>!(discard?["draft_id","expected_revision","note"]:["draft_id","expected_revision","note","entries"]).includes(k)))return problem(422,"validation_failed","Thân nháp không hợp lệ");
 if(!Number.isSafeInteger(b.expected_revision)||(b.expected_revision as number)<0||b.draft_id!=null&&!uuid225(b.draft_id)||discard&&!uuid225(b.draft_id)||b.note!=null&&(typeof b.note!=="string"||b.note.includes("\u0000")||Array.from(b.note).length>1000)||!discard&&!boundedDraftRows225(b.entries))return problem(422,"validation_failed","Nội dung nháp vượt giới hạn hoặc không hợp lệ");
 const key=ctx.request.headers.get("Idempotency-Key");if(!key||key.length>200)return problem(400,"idempotency_key_required","Thiếu khóa thao tác");
 // This exact bounded canonical string is the receipt binding, NEVER the weak display fingerprint.
 const request=stableJSON225({year,discard,body:b});const receiptKey=`${ctx.persona.me.tenant_id}:${key}`;const previous=RECEIPTS225.get(receiptKey);
 if(previous)return previous.authority===policy.authority&&previous.request===request?json(structuredClone(previous.receipt),200):problem(409,"extension.khtc_draft_operation_conflict","Khóa thao tác đã thuộc nội dung hoặc quyền khác");
 const {stored,body}=currentState225(ctx,year);if(!body.available)return problem(409,"extension.khtc_initial_draft_year_in_use","Năm đã có dữ liệu tham chiếu");
 if(stored.revision!==b.expected_revision||(b.draft_id??null)!==(stored.draft?.draft_id??null))return problem(409,"extension.khtc_draft_revision_conflict","Nháp đã thay đổi");
 const rows=discard?[]:materializePlanRows225(b.entries as EntryIn[]).entries;
 const action=discard?"discard":stored.draft?"save":"create";
 const receipt:Draft225={event_id:uuid(),draft_id:stored.draft?.draft_id??uuid(),year,revision:stored.revision+1,action,state:discard?"discarded":"active",base_published_version:0,entries_count:rows.length,
 // DEMO fingerprint only; not SHA-256 or a security/authority claim. Native tests prove real hashes.
 entries_hash:hex64(stableJSON225(rows)),note:typeof b.note==="string"?b.note.trim():"",event_time:now(),recorded_at:now(),actor_ref:ctx.persona.me.actor_ref,entries:structuredClone(rows)};
 const next:State225={revision:receipt.revision,draft:discard?null:structuredClone(receipt),events:[...stored.events,structuredClone(receipt)]};const initial=new Map(INITIAL225),receipts=new Map(RECEIPTS225);initial.set(draftKey225(ctx.persona.me.tenant_id,year),next);receipts.set(receiptKey,{authority:policy.authority,request,receipt:structuredClone(receipt)});const failed=commitState225(STORE,initial,receipts);return failed??json(receipt,201);
}
const initialDraftRoutes225:Route[]=[
 {method:"GET",pattern:/^\/v1\/kn\/khtc\/plans\/([^/]+)\/draft(?:\/(history|compare))?$/,handler:ctx=>{
  const policy=initialDraftMockPolicy225(ctx.request,ctx.persona);if(policy.denied)return policy.denied;const raw=ctx.params[0]??"";if(!/^(20\d{2}|2100)$/.test(raw))return problem(422,"validation_failed","Năm không hợp lệ");const year=Number(raw);const {stored,body}=currentState225(ctx,year);
  if(ctx.params[1]==="history"){const limitRaw=ctx.url.searchParams.get("limit")??"50",beforeRaw=ctx.url.searchParams.get("before_revision")??"0";if(!/^[0-9]{1,3}$/.test(limitRaw)||!/^[0-9]{1,19}$/.test(beforeRaw))return problem(422,"validation_failed","Phân trang không hợp lệ");const limit=Number(limitRaw),before=Number(beforeRaw);if(limit<1||limit>100||!Number.isSafeInteger(before)||before<0)return problem(422,"validation_failed","Phân trang không hợp lệ");const all=[...stored.events].reverse().filter(x=>!before||x.revision<before),page=all.slice(0,limit);return json({items:page.map(({entries,...e})=>{void entries;return e;}),next_before_revision:all.length>limit?page.at(-1)!.revision:null});}
  return json(ctx.params[1]==="compare"?{...body,baseline_entries:[],baseline_published_version:0}:body);
 }},
 {method:"POST",pattern:/^\/v1\/kn\/khtc\/plans\/([^/]+)\/draft$/,handler:ctx=>draftRequest225(ctx)},
 {method:"POST",pattern:/^\/v1\/kn\/khtc\/plans\/([^/]+)\/draft\/discard$/,handler:ctx=>draftRequest225(ctx,true)},
];

const rawKhtcRoutes225: Route[] = [
 ...initialDraftRoutes225,
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/catalogs$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      const kind = ctx.url.searchParams.get("kind");
      const retired = ctx.url.searchParams.get("include_retired") === "true";
      const cur = [...STORE.items.values()].map(last);
      const counts = KINDS.map((k) => ({ kind: k, label: LABEL[k]!, active: cur.filter((x) => x.kind === k && x.active).length, total: cur.filter((x) => x.kind === k).length }));
      const items = cur.filter((x) => (!kind || x.kind === kind) && (retired || x.active)).sort((a, b) => a.kind.localeCompare(b.kind) || a.sort_order - b.sort_order || a.code.localeCompare(b.code));
      return json({ counts, items });
    },
  },
  { method: "POST", pattern: /^\/v1\/kn\/khtc\/catalogs\/([a-z_]+)$/, handler: (ctx) => catalogWrite(ctx, ctx.params[0]!, "", true) },
  { method: "POST", pattern: /^\/v1\/kn\/khtc\/catalogs\/([a-z_]+)\/([^/]+)\/revise$/, handler: (ctx) => catalogWrite(ctx, ctx.params[0]!, decodeURIComponent(ctx.params[1]!), false) },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/catalogs\/([a-z_]+)\/([^/]+)\/history$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      const vs = STORE.items.get(`${ctx.params[0]}:${decodeURIComponent(ctx.params[1]!)}`);
      return vs ? json({ items: [...vs].reverse() }) : problem(404, "extension.khtc_catalog_not_found", "Không có mục danh mục KHTC");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/programs$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      const retired = ctx.url.searchParams.get("include_retired") === "true";
      const kind = ctx.url.searchParams.get("kind");
      const items = [...STORE.programs.values()].map(last).filter((p) => (retired || p.active) && (!kind || p.kind === kind));
      return json({ items: items.sort((a, b) => a.code.localeCompare(b.code)) });
    },
  },
  { method: "POST", pattern: /^\/v1\/kn\/khtc\/programs$/, handler: (ctx) => programWrite(ctx, null) },
  { method: "POST", pattern: /^\/v1\/kn\/khtc\/programs\/([^/]+)\/revise$/, handler: (ctx) => programWrite(ctx, ctx.params[0]!) },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/programs\/([^/]+)\/history$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      const vs = STORE.programs.get(ctx.params[0]!);
      return vs ? json({ items: [...vs].reverse() }) : problem(404, "extension.khtc_program_not_found", "Không có CT/DA trong danh sách KHTC");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      const v = ctx.url.searchParams.get("version");
      return planBody(year(ctx), v ? Number.parseInt(v, 10) : undefined);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})\/versions$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      return json({ items: (STORE.plans.get(year(ctx)) ?? []).map((p) => p.v) });
    },
  },
  { method: "POST", pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})\/versions$/, handler: (ctx) => savePlan(ctx, year(ctx)) },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})\/copy-draft$/,
    handler: (ctx) => {
      const deny = need(ctx, "plan");
      if (deny) return deny;
      const y = year(ctx) - 1;
      const vs = STORE.plans.get(y);
      if (!vs?.length) return problem(404, "extension.khtc_plan_not_found", `Năm ${y} chưa có kế hoạch để sao chép`);
      const map = new Map<string, string>();
      const out: Entry[] = [];
      for (const e of last(vs).entries) {
        if (e.tracking === "stopped" || (e.parent_ref && !map.has(e.parent_ref))) continue;
        const ref = uuid();
        map.set(e.ref, ref);
        out.push({ ...e, ref, parent_ref: e.parent_ref ? map.get(e.parent_ref)! : null, stopped_from: null, stop_decision: null });
      }
      return json({ from_year: y, from_version: vs.length, entries: decorate(out), problems: [] });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})\/export$/,
    handler: (ctx) => {
      const a = access(ctx);
      if (a instanceof Response) return a;
      if (!STORE.plans.get(year(ctx))?.length) return problem(404, "extension.khtc_plan_not_found", "Chưa có kế hoạch năm này");
      return new Response(new Uint8Array([0x50, 0x4b, 0x03, 0x04]), {
        status: 200,
        headers: {
          "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Content-Disposition": `attachment; filename="ke-hoach-nam-${year(ctx)}.xlsx"`,
        },
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/kn\/khtc\/plans\/(\d{4})\/import-draft$/,
    handler: (ctx) => need(ctx, "plan") ?? problem(422, "extension.khtc_import_invalid", "Tệp Excel kế hoạch năm không đọc được", { detail: "Bản giả lập không đọc tệp .xlsx" }),
  },
];


// Only semantic KHTC data and receipt bindings; never AuthProvider/token/session payloads.
const STORAGE_KEY225="ag.kn.khtc.initial225.v1";
let persistedRaw225:string|null=null;
type Envelope225={version:1;items:[string,Item[]][];programs:[string,Program[]][];plans:[number,{v:Version;entries:Entry[]}[]][];initial:[string,State225][];receipts:[string,Receipt225][]};
const storageFailure225=()=>problem(503,"extension.khtc_draft_storage_unavailable","Không đọc hoặc ghi được dữ liệu kế hoạch trong tab này; chưa xác nhận thao tác");
const integer225=(v:unknown):v is number=>typeof v==="number"&&Number.isSafeInteger(v)&&v>=0;
function entriesStored225(value:unknown):value is Entry[]{
 return Array.isArray(value)&&value.length<=5000&&value.every(x=>plain225(x)&&uuid225(x.ref)&&typeof x.name==="string"&&typeof x.kind==="string"&&typeof x.tab==="string"&&integer225(x.sort_order)&&Array.isArray(x.communes)&&x.communes.every(c=>typeof c==="string")&&decimal225(x.planned_qty,4,16)&&decimal225(x.budget,2,18));
}
function eventStored225(x:unknown):x is Draft225{
 return plain225(x)&&uuid225(x.event_id)&&uuid225(x.draft_id)&&integer225(x.year)&&integer225(x.revision)&&x.revision>0&&["create","save","discard"].includes(String(x.action))&&x.state===(x.action==="discard"?"discarded":"active")&&x.base_published_version===0&&typeof x.entries_hash==="string"&&/^[0-9a-f]{64}$/.test(x.entries_hash)&&typeof x.note==="string"&&typeof x.actor_ref==="string"&&typeof x.event_time==="string"&&typeof x.recorded_at==="string"&&entriesStored225(x.entries)&&x.entries_count===x.entries.length;
}
function pairsStored225(value:unknown,key:(x:unknown)=>boolean,row:(x:unknown)=>boolean):boolean{
 if(!Array.isArray(value))return false;const seen=new Set<unknown>();
 return value.every(pair=>Array.isArray(pair)&&pair.length===2&&key(pair[0])&&!seen.has(pair[0])&&(seen.add(pair[0]),row(pair[1])));
}
function envelopeStored225(value:unknown):value is Envelope225{
 if(!plain225(value)||value.version!==1||Object.keys(value).sort().join(",")!=="initial,items,plans,programs,receipts,version")return false;
 const text=(x:unknown)=>typeof x==="string";
 if(!pairsStored225(value.items,text,x=>Array.isArray(x)&&x.length>0&&x.every(y=>plain225(y)&&typeof y.kind==="string"&&typeof y.code==="string"&&typeof y.name==="string"&&integer225(y.version)&&uuid225(y.version_id))))return false;
 if(!pairsStored225(value.programs,uuid225,x=>Array.isArray(x)&&x.length>0&&x.every(y=>plain225(y)&&uuid225(y.ref)&&typeof y.code==="string"&&typeof y.name==="string"&&integer225(y.version))))return false;
 if(!pairsStored225(value.plans,integer225,x=>Array.isArray(x)&&x.length>0&&x.every(y=>plain225(y)&&plain225(y.v)&&uuid225(y.v.id)&&integer225(y.v.version)&&typeof y.v.effective_from==="string"&&entriesStored225(y.entries))))return false;
 if(!pairsStored225(value.initial,text,x=>plain225(x)&&integer225(x.revision)&&Array.isArray(x.events)&&x.events.length===x.revision&&x.events.every((e,i)=>eventStored225(e)&&e.revision===i+1)&&(x.draft===null?x.revision===0||x.events.at(-1)?.action==="discard":eventStored225(x.draft)&&x.draft.state==="active"&&JSON.stringify(x.draft)===JSON.stringify(x.events.at(-1)))))return false;
 const events=new Set((value.initial as [string,State225][]).flatMap(([,state])=>state.events.map(e=>JSON.stringify(e))));
 return pairsStored225(value.receipts,text,x=>plain225(x)&&typeof x.authority==="string"&&typeof x.request==="string"&&eventStored225(x.receipt)&&events.has(JSON.stringify(x.receipt)));
}
function snapshot225(store:Store,initial:Map<string,State225>,receipts:Map<string,Receipt225>):Envelope225{
 return {version:1,items:[...store.items],programs:[...store.programs],plans:[...store.plans],initial:[...initial],receipts:[...receipts]};
}
function restoreState225():Response|null{
 try{
  if(typeof window==="undefined")return storageFailure225();
  const raw=window.sessionStorage.getItem(STORAGE_KEY225);
  // Missing only means a fresh tab. Removing existing persisted state is not a reset.
  if(raw===null)return persistedRaw225===null?null:storageFailure225();
  if(raw===persistedRaw225)return null;
  const saved:unknown=JSON.parse(raw);if(!envelopeStored225(saved))return storageFailure225();
  STORE={items:new Map(saved.items),programs:new Map(saved.programs),plans:new Map(saved.plans)};INITIAL225=new Map(saved.initial);RECEIPTS225=new Map(saved.receipts);persistedRaw225=raw;return null;
 }catch{return storageFailure225();}
}
function commitState225(store:Store,initial=INITIAL225,receipts=RECEIPTS225):Response|null{
 try{
  const candidate=JSON.stringify(snapshot225(store,initial,receipts));
  window.sessionStorage.setItem(STORAGE_KEY225,candidate);
  // No RAM mutation or ACK until the complete candidate is durably accepted by storage.
  STORE=store;INITIAL225=initial;RECEIPTS225=receipts;persistedRaw225=candidate;return null;
 }catch{return storageFailure225();}
}
export function publishedOwnerPlanGuard225(year:number,cutoff:string):Response|null{
 const failed=restoreState225();return failed??(hasPublishedOwnerPlan225(year,cutoff)?null:problem(409,"extension.khtc_plan_required","Năm chưa có kế hoạch tại ngày chốt"));
}
export const khtcRoutes:Route[]=rawKhtcRoutes225.map(route=>({...route,handler:ctx=>{
 const failed=restoreState225();return failed??route.handler(ctx);
}}));
