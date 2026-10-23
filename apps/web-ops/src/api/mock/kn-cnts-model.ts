// GIẢ LẬP CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS (W11.47, BRD CNTS v3.7 FN-10 phần hộ, FN-14, FN-19 phần CT/DA)
// theo hợp đồng /v1/extension/cnts/projects*, /demo-sites*, /model-households*, /grants*, /grant-candidates, /khtc-programs.
// Mỗi thay đổi là một phiên bản (khóa lạc quan base_version). Chủ hộ hiển thị nhãn che bớt như kho định danh. Dữ liệu
// tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { ADMIN_UNITS, CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";

// Như máy chủ (cntsmodel/domain): kinh phí số nguyên không âm; quy mô số dương (BF-SWEEP-CNTS: web chặn trước, mock chặn lại).
const budgetBad = (v: string | null | undefined) => !!v && !/^\d{1,15}$/.test(v);
const scaleBad = (b: { scale?: string | null; scale_unit?: string | null }) =>
  (!!b.scale !== !!b.scale_unit && "nhập cả quy mô và đơn vị") || (!!b.scale && !(/^\d{1,12}(\.\d+)?$/.test(b.scale) && Number(b.scale) > 0) && "phải dương") || null;
import { knContext, personaForContext } from "./kn";
import { AG_COMMUNES } from "./kn-reports/store";

type S = components["schemas"];
type ProjectCore = Omit<S["CNTSProjectVersion"], "actor_display">;
type SiteCore = Omit<S["CNTSSiteVersion"], "actor_display">;
type HouseholdCore = Omit<S["CNTSHouseholdVersion"], "actor_display">;
type GrantCore = Omit<S["CNTSGrantVersion"], "actor_display">;
type Owner = { label: string; phone: string };

const PROJECTS = new Map<string, ProjectCore[]>();
const SITES = new Map<string, SiteCore[]>();
const HOUSEHOLDS = new Map<string, HouseholdCore[]>();
const GRANTS = new Map<string, GrantCore[]>();
const POINTS = new Map<string, S["CNTSPoint"]>();
const OWNERS = new Map<string, Owner>();

const OFFICER = "0190f1c4-7d3e-7000-8000-00000000a0c1";
const NAMES: Record<string, string> = { [OFFICER]: "Viên chức xã (giả lập)" };
const PROGRAMS: S["CNTSKHTCProgram"][] = [
  { ref: "0190f1c4-7d3e-7000-8000-0000000c7001", code: "CT-KNQG-01", name: "Chương trình khuyến nông quốc gia 2027", kind: "project", active: true },
];

/** Xóa trạng thái (kiểm thử). */
export function resetCntsModel() {
  for (const m of [PROJECTS, SITES, HOUSEHOLDS, GRANTS, POINTS, OWNERS]) m.clear();
  seeded = false;
}

function who(ctx: Ctx) {
  const id = ctx.request.headers.get("X-AG-Context");
  const kc = knContext(id ?? "");
  const p = personaForContext(ctx.persona, id);
  const role = kc?.role ?? (p.me.roles.includes("platform_admin") ? "admin" : p.me.roles.includes("kn_national_admin") ? "kn_tw" : p.me.scope.level === "commune" ? "kn_xa" : "kn_tinh");
  const communes = role === "kn_xa" ? (kc?.communes.map((c) => c.code) ?? p.me.scope.units) : null;
  return { role, communes, actor: p.me.actor_ref, readOnly: !!kc?.read_only, group: kc?.province_group ?? "", manager: !!kc?.manager };
}
type Who = ReturnType<typeof who>;

const now = () => new Date().toISOString();
const meta = (w: Who, version: number) => ({ version, updated_at: now(), updated_by: w.actor });
const last = <T,>(m: Map<string, T[]>, ref: string) => m.get(ref)?.at(-1);
const push = <T,>(m: Map<string, T[]>, ref: string, v: T) => m.set(ref, [...(m.get(ref) ?? []), v]);
const sees = (w: Who, commune: string) => w.communes === null || w.communes.includes(commune);
const editProject = (w: Who, p: ProjectCore) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && (w.manager || w.group === "lanh_dao_phong" || p.manager_ref === w.actor)));
const canCreate = (w: Who) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && ["", "chu_nhiem", "lanh_dao_phong"].includes(w.group)));
const granter = (w: Who) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && ["", "tong_hop", "chu_nhiem", "lanh_dao_phong"].includes(w.group)));
const COMMUNE_OK = (code: string) => AG_COMMUNES.some((c) => c.code === code) || ADMIN_UNITS.some((u) => u.code === code && u.level === "commune");
const conflict = (what: string) => problem(409, `extension.cnts_${what}_version_conflict`, "Đã có người cập nhật — tải lại bản mới");
const reasonBad = (r: unknown) => typeof r !== "string" || r.trim().length < 5;

function projectView(w: Who, p: ProjectCore): S["CNTSProject"] {
  const sites = [...SITES.values()].map((v) => v.at(-1)!).filter((s) => s.project_ref === p.ref);
  const hhs = [...HOUSEHOLDS.values()].map((v) => v.at(-1)!).filter((h) => h.project_ref === p.ref && h.event !== "withdrawn");
  return { ...p, status: p.event === "closed" ? "closed" : "active", site_count: sites.filter((s) => s.event !== "closed").length,
    household_count: hhs.length, can_edit: editProject(w, p) && p.event !== "closed", manager_display: NAMES[p.manager_ref] ?? null };
}
function siteView(w: Who, s: SiteCore): S["CNTSSite"] {
  const p = last(PROJECTS, s.project_ref)!;
  const open = p.event !== "closed" && s.event !== "closed";
  const follows = w.role === "kn_xa" && !w.readOnly && (s.officer_ref === w.actor || activeGrants(w.actor).some((g) => g.site_refs.includes(s.ref)));
  return { ...s, project_code: p.code, project_name: p.name, status: s.event === "closed" ? "closed" : "active", location: POINTS.get(s.ref) ?? null,
    household_count: [...HOUSEHOLDS.values()].filter((v) => v.at(-1)!.site_ref === s.ref && v.at(-1)!.event !== "withdrawn").length,
    can_edit: editProject(w, p) && p.event !== "closed", can_add_households: open && (editProject(w, p) || follows),
    officer_display: s.officer_ref ? NAMES[s.officer_ref] ?? null : null,
    model_type_name: CATALOGS.cnts_model_type.find((m) => m.code === s.model_type_code)?.name ?? null };
}
function householdView(w: Who, h: HouseholdCore): S["CNTSHousehold"] {
  const s = last(SITES, h.site_ref)!;
  const sv = siteView(w, s);
  const o = OWNERS.get(h.person_ref);
  return { ...h, site_name: s.name, project_code: sv.project_code, status: h.event === "withdrawn" ? "withdrawn" : "active",
    location: POINTS.get(h.ref) ?? null, can_edit: sv.can_add_households, owner_label: o?.label ?? null, owner_phone_masked: o?.phone ?? null,
    keeper_display: h.keeper_ref ? NAMES[h.keeper_ref] ?? null : null };
}
function grantStatus(g: GrantCore): S["CNTSGrant"]["status"] {
  const today = now().slice(0, 10);
  if (g.event === "revoked") return "revoked";
  if (today < g.valid_from) return "pending";
  if (g.valid_to && today > g.valid_to) return "expired";
  return "active";
}
function activeGrants(actor: string) {
  return [...GRANTS.values()].map((v) => v.at(-1)!).filter((g) => g.grantee_ref === actor && g.role === "theo_doi_mo_hinh" && grantStatus(g) === "active");
}
function grantView(w: Who, g: GrantCore): S["CNTSGrant"] {
  const status = grantStatus(g);
  return { ...g, status, can_edit: granter(w) && status !== "revoked", grantee_display: NAMES[g.grantee_ref] ?? null,
    updated_by_display: NAMES[g.updated_by] ?? null, successor_display: g.successor_ref ? NAMES[g.successor_ref] ?? null : null };
}
const withActor = <T extends { updated_by: string }>(v: T) => ({ ...v, actor_display: NAMES[v.updated_by] ?? null });

function projectDetail(w: Who, ref: string): S["CNTSProjectDetail"] {
  const p = last(PROJECTS, ref)!;
  const sites = [...SITES.values()].map((v) => v.at(-1)!).filter((s) => s.project_ref === ref && sees(w, s.commune_code)).map((s) => siteView(w, s));
  return { ...projectView(w, p), sites, versions: [...PROJECTS.get(ref)!].reverse().map(withActor),
    program: PROGRAMS.find((x) => x.ref === p.program_ref) ?? null };
}
function siteDetail(w: Who, ref: string): S["CNTSSiteDetail"] {
  const hhs = [...HOUSEHOLDS.values()].map((v) => v.at(-1)!).filter((h) => h.site_ref === ref).map((h) => householdView(w, h));
  return { ...siteView(w, last(SITES, ref)!), households: hhs, versions: [...SITES.get(ref)!].reverse().map(withActor) };
}
const householdDetail = (w: Who, ref: string): S["CNTSHouseholdDetail"] => ({ ...householdView(w, last(HOUSEHOLDS, ref)!), versions: [...HOUSEHOLDS.get(ref)!].reverse().map(withActor) });
const grantDetail = (w: Who, ref: string): S["CNTSGrantDetail"] => ({ ...grantView(w, last(GRANTS, ref)!), versions: [...GRANTS.get(ref)!].reverse().map(withActor) });

const maskName = (n: string) => { const ws = n.trim().split(/\s+/); return ws.length < 2 ? `${ws[0]?.[0] ?? ""}.` : `${ws[0]} ${ws.slice(1, -1).map((x) => x[0] + ".").join(" ")} ${ws.at(-1)![0]}.`.replace(/\s+/g, " "); };
const maskPhone = (p: string) => (p.length >= 7 ? `${p.slice(0, 4)}•••${p.slice(-3)}` : "");

let seeded = false;
function seed() {
  if (seeded) return;
  seeded = true;
  const sys = { version: 1, updated_at: now(), updated_by: OFFICER };
  const pref = "0190f1c4-7d3e-7000-8000-0000000c7101";
  PROJECTS.set(pref, [{ ref: pref, event: "created", province_code: "91", code: "DA-GA-2027", name: "Mô hình chăn nuôi gà an toàn sinh học", kind: "knqg",
    basis: "Hợp đồng 12/HĐ-KNQG", scale_note: "3 điểm, 15 hộ", start_date: "2027-03-01", end_date: "2027-12-31", budget: "450000000",
    manager_ref: OFFICER, program_ref: PROGRAMS[0]!.ref, training_note: "2 lớp tập huấn", supplies_note: "Giống gà, thức ăn, vaccine",
    attachment_refs: [], status_reason: null, ...sys }]);
  const sref = "0190f1c4-7d3e-7000-8000-0000000c7201";
  SITES.set(sref, [{ ref: sref, event: "created", project_ref: pref, commune_code: AG_COMMUNES[0]!.code, name: "Điểm ấp Bình Hòa",
    model_type_code: CATALOGS.cnts_model_type[0]?.code ?? "GA_THUONG_PHAM", scale: "1500", scale_unit: "con", officer_ref: OFFICER, diary_form: "BM-08",
    cooperative_ref: null, note: null, status_reason: null, ...sys }]);
  POINTS.set(sref, { lat: 10.38, lng: 105.43, source: "map" });
  const href = "0190f1c4-7d3e-7000-8000-0000000c7301";
  const person = "0190f1c4-7d3e-7000-8000-0000000c7401";
  OWNERS.set(person, { label: "Nguyễn V. B.", phone: "0912•••678" });
  HOUSEHOLDS.set(href, [{ ref: href, event: "created", site_ref: sref, project_ref: pref, commune_code: AG_COMMUNES[0]!.code, person_ref: person,
    scale: "500", scale_unit: "con", keeper_kind: "officer", keeper_ref: OFFICER, self_entry: false, joined_on: "2027-03-05", note: null,
    status_reason: null, ...sys }]);
}

function parsePoint(b: { location?: S["CNTSPoint"] | null }) {
  const l = b.location;
  if (!l) return null;
  return l.lat < 5 || l.lat > 25 || l.lng < 100 || l.lng > 118 ? "tọa độ ngoài phạm vi Việt Nam" : null;
}

export const cntsModelRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/projects$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const st = ctx.url.searchParams.get("status");
      const items = [...PROJECTS.values()].map((v) => projectView(w, v.at(-1)!)).filter((p) => (!st || p.status === st) &&
        (w.communes === null || [...SITES.values()].some((s) => s.at(-1)!.project_ref === p.ref && sees(w, s.at(-1)!.commune_code))));
      return json({ items, can_create: canCreate(w) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/projects$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      if (!canCreate(w)) return problem(403, "forbidden", "chỉ chủ nhiệm, lãnh đạo phòng Cấp Tỉnh (hoặc Admin) tạo chương trình, dự án");
      const b = (ctx.body ?? {}) as S["CNTSProjectSave"];
      const errs = [!b.code?.trim() && { field: "code", reason: "bắt buộc" }, !b.name?.trim() && { field: "name", reason: "bắt buộc" },
        !b.start_date && { field: "start_date", reason: "bắt buộc" }, b.end_date && b.start_date && b.end_date < b.start_date && { field: "end_date", reason: "không trước ngày bắt đầu" },
        budgetBad(b.budget) && { field: "budget", reason: "số đồng nguyên, không âm" }]
        .filter(Boolean) as { field: string; reason: string }[];
      if (errs.length) return validation(errs);
      const code = b.code.trim().toUpperCase();
      if ([...PROJECTS.values()].some((v) => v.at(-1)!.code === code)) return problem(409, "extension.cnts_project_code_taken", "Mã dự án đã được dùng");
      const ref = b.id ?? uuidv7();
      PROJECTS.set(ref, [{ ref, event: "created", province_code: b.province_code ?? "91", code, name: b.name.trim(), kind: b.kind, basis: b.basis ?? null,
        scale_note: b.scale_note ?? null, start_date: b.start_date, end_date: b.end_date ?? null, budget: b.budget ?? null,
        manager_ref: b.manager_ref ?? w.actor, program_ref: b.program_ref ?? null, training_note: b.training_note ?? null,
        supplies_note: b.supplies_note ?? null, attachment_refs: b.attachment_refs ?? [], status_reason: null, ...meta(w, 1) }]);
      return json(projectDetail(w, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/projects\/([^/]+)$/,
    handler: (ctx) => (seed(), PROJECTS.has(ctx.params[0]!) ? json(projectDetail(who(ctx), ctx.params[0]!)) : problem(404, "extension.cnts_project_not_found", "Không có chương trình, dự án")),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/projects\/([^/]+)(?:\/(close|reopen))?$/,
    handler: (ctx) => {
      const w = who(ctx);
      const cur = last(PROJECTS, ctx.params[0]!);
      if (!cur) return problem(404, "extension.cnts_project_not_found", "Không có chương trình, dự án");
      if (!editProject(w, cur)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin sửa dự án (FN-19 BR-01)");
      const b = (ctx.body ?? {}) as S["CNTSProjectSave"] & { reason?: string };
      if (b.base_version !== cur.version) return conflict("project");
      const action = ctx.params[1];
      if (action) {
        if (reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
        push(PROJECTS, cur.ref, { ...cur, event: action === "close" ? "closed" : "reopened", status_reason: b.reason!, ...meta(w, cur.version + 1) });
      } else {
        if (cur.event === "closed") return problem(409, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa");
        if (budgetBad(b.budget)) return validation([{ field: "budget", reason: "số đồng nguyên, không âm" }]);
        push(PROJECTS, cur.ref, { ...cur, event: "updated", code: b.code.trim().toUpperCase(), name: b.name, kind: b.kind, basis: b.basis ?? null,
          scale_note: b.scale_note ?? null, start_date: b.start_date, end_date: b.end_date ?? null, budget: b.budget ?? null,
          manager_ref: b.manager_ref ?? cur.manager_ref, program_ref: b.program_ref ?? null, training_note: b.training_note ?? null,
          supplies_note: b.supplies_note ?? null, attachment_refs: b.attachment_refs ?? [], status_reason: null, ...meta(w, cur.version + 1) });
      }
      return json(projectDetail(w, cur.ref));
    },
  },
  { method: "GET", pattern: /^\/v1\/extension\/cnts\/khtc-programs$/, handler: () => json({ items: PROGRAMS }) },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/demo-sites$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const pr = ctx.url.searchParams.get("project_ref");
      const items = [...SITES.values()].map((v) => v.at(-1)!).filter((s) => sees(w, s.commune_code) && (!pr || s.project_ref === pr)).map((s) => siteView(w, s));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/demo-sites$/,
    handler: (ctx) => {
      const w = who(ctx);
      const b = (ctx.body ?? {}) as S["CNTSSiteSave"];
      const p = b.project_ref ? last(PROJECTS, b.project_ref) : undefined;
      if (!p) return problem(404, "extension.cnts_project_not_found", "Không có chương trình, dự án");
      if (!editProject(w, p)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin thêm điểm");
      if (p.event === "closed") return problem(409, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa");
      if (!COMMUNE_OK(b.commune_code)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      const bad = parsePoint(b);
      if (bad || !b.name?.trim()) return validation([{ field: bad ? "location" : "name", reason: bad ?? "bắt buộc" }]);
      if (scaleBad(b)) return validation([{ field: "scale", reason: scaleBad(b)! }]);
      const ref = b.id ?? uuidv7();
      SITES.set(ref, [{ ref, event: "created", project_ref: p.ref, commune_code: b.commune_code, name: b.name.trim(), model_type_code: b.model_type_code,
        scale: b.scale ?? null, scale_unit: b.scale_unit ?? null, officer_ref: b.officer_ref ?? null, diary_form: b.diary_form ?? null,
        cooperative_ref: b.cooperative_ref ?? null, note: b.note ?? null, status_reason: null, ...meta(w, 1) }]);
      if (b.location) POINTS.set(ref, b.location);
      return json(siteDetail(w, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/demo-sites\/([^/]+)$/,
    handler: (ctx) => (seed(), SITES.has(ctx.params[0]!) ? json(siteDetail(who(ctx), ctx.params[0]!)) : problem(404, "extension.cnts_site_not_found", "Không có điểm trình diễn")),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/demo-sites\/([^/]+)(?:\/(close|reopen))?$/,
    handler: (ctx) => {
      const w = who(ctx);
      const cur = last(SITES, ctx.params[0]!);
      if (!cur) return problem(404, "extension.cnts_site_not_found", "Không có điểm trình diễn");
      const p = last(PROJECTS, cur.project_ref)!;
      if (!editProject(w, p)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin");
      if (p.event === "closed") return problem(409, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa");
      const b = (ctx.body ?? {}) as S["CNTSSiteSave"] & { reason?: string };
      if (b.base_version !== cur.version) return conflict("site");
      if (ctx.params[1]) {
        if (reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
        push(SITES, cur.ref, { ...cur, event: ctx.params[1] === "close" ? "closed" : "reopened", status_reason: b.reason!, ...meta(w, cur.version + 1) });
      } else {
        if (cur.event === "closed") return problem(409, "extension.cnts_site_closed", "Điểm trình diễn đã đóng");
        if (scaleBad(b)) return validation([{ field: "scale", reason: scaleBad(b)! }]);
        push(SITES, cur.ref, { ...cur, event: "updated", commune_code: b.commune_code, name: b.name, model_type_code: b.model_type_code, scale: b.scale ?? null,
          scale_unit: b.scale_unit ?? null, officer_ref: b.officer_ref ?? null, diary_form: b.diary_form ?? null, cooperative_ref: b.cooperative_ref ?? null,
          note: b.note ?? null, status_reason: null, ...meta(w, cur.version + 1) });
        if (b.location) POINTS.set(cur.ref, b.location);
        else if (b.remove_location) POINTS.delete(cur.ref);
      }
      return json(siteDetail(w, cur.ref));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/model-households$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const site = ctx.url.searchParams.get("site_ref");
      const st = ctx.url.searchParams.get("status");
      const items = [...HOUSEHOLDS.values()].map((v) => householdView(w, v.at(-1)!))
        .filter((h) => sees(w, h.commune_code) && (!site || h.site_ref === site) && (!st || h.status === st));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/model-households$/,
    handler: (ctx) => {
      const w = who(ctx);
      const b = (ctx.body ?? {}) as S["CNTSHouseholdSave"];
      const s = b.site_ref ? last(SITES, b.site_ref) : undefined;
      if (!s || !sees(w, s.commune_code)) return problem(404, "extension.cnts_site_not_found", "Không có điểm trình diễn");
      if (!siteView(w, s).can_add_households) return problem(403, "forbidden", "chỉ người quản lý dự án hoặc cán bộ theo dõi điểm thêm hộ mô hình");
      if (!b.person_ref && !b.owner_name?.trim()) return validation([{ field: "owner_name", reason: "nhập họ tên chủ hộ (lưu ở kho định danh)" }]);
      if (b.keeper_kind !== "self" && !b.keeper_ref) return validation([{ field: "keeper_ref", reason: "chọn cán bộ ghi nhật ký (hoặc chọn hộ tự ghi)" }]);
      if (scaleBad(b)) return validation([{ field: "scale", reason: scaleBad(b)! }]);
      const bad = parsePoint(b);
      if (bad) return validation([{ field: "location", reason: bad }]);
      const person = b.person_ref ?? uuidv7();
      if (b.owner_name) OWNERS.set(person, { label: maskName(b.owner_name), phone: maskPhone(b.owner_phone ?? "") });
      const ref = b.id ?? uuidv7();
      HOUSEHOLDS.set(ref, [{ ref, event: "created", site_ref: s.ref, project_ref: s.project_ref, commune_code: s.commune_code, person_ref: person,
        scale: b.scale ?? null, scale_unit: b.scale_unit ?? null, keeper_kind: b.keeper_kind ?? "officer", keeper_ref: b.keeper_kind === "self" ? null : b.keeper_ref ?? null,
        self_entry: !!b.self_entry, joined_on: b.joined_on ?? null, note: b.note ?? null, status_reason: null, ...meta(w, 1) }]);
      if (b.location) POINTS.set(ref, b.location);
      return json(householdDetail(w, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/model-households\/([^/]+)$/,
    handler: (ctx) => (seed(), HOUSEHOLDS.has(ctx.params[0]!) ? json(householdDetail(who(ctx), ctx.params[0]!)) : problem(404, "extension.cnts_household_not_found", "Không có hộ mô hình")),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/model-households\/([^/]+)(?:\/(withdraw|reinstate))?$/,
    handler: (ctx) => {
      const w = who(ctx);
      const cur = last(HOUSEHOLDS, ctx.params[0]!);
      if (!cur) return problem(404, "extension.cnts_household_not_found", "Không có hộ mô hình");
      if (!householdView(w, cur).can_edit) return problem(403, "forbidden", "chỉ người quản lý dự án hoặc cán bộ theo dõi điểm sửa hộ mô hình");
      const b = (ctx.body ?? {}) as S["CNTSHouseholdSave"] & { reason?: string };
      if (b.base_version !== cur.version) return conflict("household");
      if (ctx.params[1]) {
        if (reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
        push(HOUSEHOLDS, cur.ref, { ...cur, event: ctx.params[1] === "withdraw" ? "withdrawn" : "reinstated", status_reason: b.reason!, ...meta(w, cur.version + 1) });
        if (ctx.params[1] === "withdraw") POINTS.delete(cur.ref);
      } else {
        if (cur.event === "withdrawn") return problem(409, "extension.cnts_household_withdrawn", "Hộ đã rút khỏi mô hình");
        if (scaleBad(b)) return validation([{ field: "scale", reason: scaleBad(b)! }]);
        push(HOUSEHOLDS, cur.ref, { ...cur, event: "updated", scale: b.scale ?? null, scale_unit: b.scale_unit ?? null, keeper_kind: b.keeper_kind ?? cur.keeper_kind,
          keeper_ref: b.keeper_kind === "self" ? null : b.keeper_ref ?? cur.keeper_ref, self_entry: !!b.self_entry, joined_on: b.joined_on ?? cur.joined_on,
          note: b.note ?? null, status_reason: null, ...meta(w, cur.version + 1) });
        if (b.location) POINTS.set(cur.ref, b.location);
        else if (b.remove_location) POINTS.delete(cur.ref);
      }
      return json(householdDetail(w, cur.ref));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/grants$/,
    handler: (ctx) => {
      const w = who(ctx);
      const mine = ctx.url.searchParams.get("mine") === "true";
      if (!mine && w.role !== "admin" && w.role !== "kn_tinh") return problem(403, "forbidden", "Cấp Xã chỉ xem quyền của mình (mine=true)");
      const items = [...GRANTS.values()].map((v) => grantView(w, v.at(-1)!)).filter((g) => !mine || g.grantee_ref === w.actor);
      return json({ items, can_grant: granter(w) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/grants$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (!granter(w)) return problem(403, "forbidden", "chỉ Cấp Tỉnh (tổng hợp, chủ nhiệm, lãnh đạo phòng) hoặc Admin cấp quyền");
      const b = (ctx.body ?? {}) as S["CNTSGrantSave"];
      if (!b.grantee_ref) return validation([{ field: "grantee_ref", reason: "bắt buộc" }]);
      if (!b.commune_codes?.length) return validation([{ field: "commune_codes", reason: "chọn ít nhất một xã" }]);
      if (b.commune_codes.some((c) => !COMMUNE_OK(c))) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      if (w.group === "chu_nhiem" && !w.manager && b.role !== "theo_doi_mo_hinh" && b.role !== "nong_ho")
        return problem(403, "forbidden", "chủ nhiệm chỉ cấp vai cán bộ theo dõi mô hình, nông hộ trong dự án mình (FN-14 BR-03)");
      if (b.role === "theo_doi_mo_hinh" && !b.site_refs?.length && !b.household_refs?.length) return validation([{ field: "site_refs", reason: "chọn điểm trình diễn hoặc hộ mô hình được theo dõi" }]);
      if ([...GRANTS.values()].some((v) => { const g = v.at(-1)!; return g.grantee_ref === b.grantee_ref && g.role === b.role && ["active", "pending"].includes(grantStatus(g)); }))
        return problem(409, "extension.cnts_grant_exists", "Tài khoản đã có quyền còn hiệu lực với vai này — sửa quyền đó");
      const ref = uuidv7();
      GRANTS.set(ref, [{ ref, event: "granted", grantee_ref: b.grantee_ref, role: b.role!, province_code: "91", commune_codes: [...b.commune_codes].sort(),
        facility_refs: b.facility_refs ?? [], site_refs: b.site_refs ?? [], household_refs: b.household_refs ?? [],
        forms: (b.forms ?? []).map((f) => f.toUpperCase()).sort(), valid_from: b.valid_from ?? now().slice(0, 10), valid_to: b.valid_to ?? null,
        successor_ref: null, successor_grant_ref: null, reason: null, ...meta(w, 1) }]);
      return json(grantDetail(w, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/grants\/([^/]+)$/,
    handler: (ctx) => (GRANTS.has(ctx.params[0]!) ? json(grantDetail(who(ctx), ctx.params[0]!)) : problem(404, "extension.cnts_grant_not_found", "Không có quyền đã cấp")),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/grants\/([^/]+)(?:\/(revoke))?$/,
    handler: (ctx) => {
      const w = who(ctx);
      const cur = last(GRANTS, ctx.params[0]!);
      if (!cur) return problem(404, "extension.cnts_grant_not_found", "Không có quyền đã cấp");
      if (cur.event === "revoked") return problem(409, "extension.cnts_grant_revoked", "Quyền đã thu hồi");
      if (!granter(w)) return problem(403, "forbidden", "không được sửa quyền này");
      const b = (ctx.body ?? {}) as S["CNTSGrantSave"] & S["CNTSGrantRevoke"];
      if (b.base_version !== cur.version) return conflict("grant");
      if (ctx.params[1]) {
        if (reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
        let succGrant: string | null = null;
        if (b.successor_ref) {
          succGrant = uuidv7();
          GRANTS.set(succGrant, [{ ...cur, ref: succGrant, event: "granted", grantee_ref: b.successor_ref, reason: `Nhận việc khi thu hồi quyền ${cur.ref}`,
            valid_from: now().slice(0, 10), successor_ref: null, successor_grant_ref: null, ...meta(w, 1) }]);
        }
        push(GRANTS, cur.ref, { ...cur, event: "revoked", reason: b.reason, successor_ref: b.successor_ref ?? null, successor_grant_ref: succGrant, ...meta(w, cur.version + 1) });
      } else {
        push(GRANTS, cur.ref, { ...cur, event: "updated", commune_codes: [...(b.commune_codes ?? cur.commune_codes)].sort(), facility_refs: b.facility_refs ?? [],
          site_refs: b.site_refs ?? [], household_refs: b.household_refs ?? [], forms: (b.forms ?? []).map((f) => f.toUpperCase()).sort(),
          valid_from: b.valid_from ?? cur.valid_from, valid_to: b.valid_to ?? null, ...meta(w, cur.version + 1) });
      }
      return json(grantDetail(w, cur.ref));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/grant-candidates$/,
    handler: () => json({ items: [{ ref: OFFICER, display: NAMES[OFFICER] ?? null, commune_codes: AG_COMMUNES.slice(0, 3).map((c) => c.code) }] }),
  },
];
