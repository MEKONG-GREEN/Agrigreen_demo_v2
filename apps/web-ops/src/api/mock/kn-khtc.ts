// ============================================================================
// BACKEND GIẢ LẬP — Phòng Kế hoạch – Tài chính (W11.61, pf/extension — /v1/kn/khtc/*). Đường dẫn, tham số, hình dạng
// phản hồi theo ĐÚNG contracts/openapi/application.yaml. Quy tắc mô phỏng (rút gọn từ internal/platform/extension/khtc):
//   - Đọc: Admin, Cấp Tỉnh mọi phòng. Danh mục: Admin ghi. CT/DA + kế hoạch: Admin, Cấp Tỉnh phòng KHTC ghi.
//   - Mỗi thay đổi là một phiên bản; ngừng = active false; trùng mã → 409.
//   - Kế hoạch: base_version khác phiên bản mới nhất → 409; điều chỉnh thiếu số/ngày QĐ/ngày hiệu lực → 422;
//     hạng mục thiếu phòng phụ trách → 422; số La Mã theo thứ tự CT ở tab Các chương trình.
//   - Xuất Excel trả tệp giả; nhập Excel không đọc tệp (422) — chỉ máy chủ thật đọc .xlsx.
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

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
  STORE = seed();
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
  STORE.items.set(key, [...(vs ?? []), it]);
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
  STORE.programs.set(id, [...(vs ?? []), p]);
  return json(p, 201);
}

function savePlan(ctx: Ctx, year: number): Response {
  const deny = need(ctx, "plan");
  if (deny) return deny;
  const b = (ctx.body ?? {}) as S["KhtcPlanSave"];
  const vs = STORE.plans.get(year) ?? [];
  if (b.base_version !== vs.length) return problem(409, "extension.khtc_plan_version_conflict", "Kế hoạch đã có phiên bản mới hơn — tải lại rồi điều chỉnh", { latest_version: vs.length });
  if (vs.length && (!b.decision_no || !b.decision_date || !b.effective_from))
    return problem(422, "extension.khtc_effective_date_invalid", "Điều chỉnh theo QĐ bắt buộc số, ngày quyết định và ngày hiệu lực");
  const eff = b.effective_from || `${year}-01-01`;
  if (!eff.startsWith(String(year))) return problem(422, "extension.khtc_effective_date_invalid", "Ngày hiệu lực trong năm kế hoạch");
  const errors: { field: string; reason: string }[] = [];
  const byRef = new Map<string, EntryIn>();
  const entries: Entry[] = (b.entries ?? []).map((x, i) => {
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
  STORE.plans.set(year, [...vs, { v, entries }]);
  return planBodyCreated(year);
}
function planBodyCreated(year: number): Response {
  const vs = STORE.plans.get(year)!;
  const p = last(vs);
  return json({ year, version: p.v, latest_version: vs.length, entries: decorate(p.entries) }, 201);
}

const year = (ctx: Ctx) => Number.parseInt(ctx.params[0]!, 10);

export const khtcRoutes: Route[] = [
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
