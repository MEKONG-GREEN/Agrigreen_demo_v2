// GIẢ LẬP mẫu biểu dự án KNQG (W11.53, BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01) theo hợp đồng
// /v1/extension/cnts/knqg-specs, /knqg-docs*: đặc tả lấy từ fixtures/knqg-specs.json (sinh từ đặc tả Go), vòng đời như máy
// chủ (01 duyệt; 03/04/05 ký có scan; 07 gửi Quốc gia — TW xác nhận/trả lại), Mẫu 07 chỉ gửi khi mọi điểm có Mẫu 05 đã ký.
// Thực tế từ nhật ký của Mẫu 05 không giả lập (máy chủ đọc nhật ký đã xác thực). Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import fixture from "./fixtures/knqg-specs.json";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext, personaForContext } from "./kn";
import { KN_ACCOUNTS } from "./kn-accounts";
import { cntsMockProjectInfo } from "./kn-cnts-model";

/** Cấp quản lý có quyền lãnh đạo (như máy chủ: accport.IsManager) — ngữ cảnh giả lập chưa mang cờ manager. */
const MANAGER_LEVELS = ["Phụ trách", "Phó trưởng phòng", "Trưởng phòng", "Lãnh đạo đơn vị", "Phó Giám đốc", "Giám đốc"];
const managerByLevel = (assignmentId: string | null | undefined) =>
  !!assignmentId &&
  KN_ACCOUNTS.some((a) => a.assignments.some((x) => x.assignment_id === assignmentId && MANAGER_LEVELS.includes(x.mgmt_level ?? "")));

type S = components["schemas"];
type Spec = S["CNTSKNQGSpec"];
type Doc = S["CNTSKNQGDocDetail"];
type Content = S["CNTSKNQGContent"];
type Row = Record<string, string>;

const SPECS = fixture.specs as unknown as Spec[];
const INDICATORS = fixture.indicator_rows as Record<string, Row[]>;
const CHECKLIST = fixture.doc_checklist as string[];
const DOCS = new Map<string, Doc[]>(); // ref → phiên bản (mới nhất cuối)
const WORDS = new Map<string, S["CNTSKNQGWordRun"][]>();
const PHOTOS = new Map<string, S["CNTSKNQGPhoto"][]>(); // W11.53b: ảnh thực địa hiện hành của Mẫu 03
const MAX_PHOTOS = 20;
let reportNo = 0;

/** Xóa trạng thái (kiểm thử). */
export function resetCntsKnqg() {
  DOCS.clear();
  WORDS.clear();
  PHOTOS.clear();
  reportNo = 0;
}

function who(ctx: Ctx) {
  const id = ctx.request.headers.get("X-AG-Context");
  const kc = knContext(id ?? "");
  const p = personaForContext(ctx.persona, id);
  const role = kc?.role ?? (p.me.roles.includes("platform_admin") ? "admin" : p.me.roles.includes("kn_national_admin") ? "kn_tw" : p.me.scope.level === "commune" ? "kn_xa" : "kn_tinh");
  const communes = role === "kn_xa" ? (kc?.communes.map((c) => c.code) ?? p.me.scope.units) : null;
  return { role, communes, actor: p.me.actor_ref, readOnly: !!kc?.read_only, manager: !!kc?.manager || kc?.province_group === "lanh_dao_phong" || managerByLevel(kc?.assignment_id) };
}
type Who = ReturnType<typeof who>;
type Info = NonNullable<ReturnType<typeof cntsMockProjectInfo>>;

const spec = (form: string) => SPECS.find((s) => s.form === form);
const last = (ref: string) => DOCS.get(ref)?.at(-1);
const editable = (s: string) => s === "draft" || s === "returned";
const editor = (w: Who, info: Info) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && (w.manager || info.project.manager_ref === w.actor)));
const approver = (w: Who) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && w.manager));
const national = (w: Who, info: Info) => (w.role === "kn_tw" && info.project.kind === "knqg") || (w.role === "admin" && !w.readOnly);

const TRANSITIONS: Record<string, [string, string, string, boolean?][]> = {
  // [thao tác, từ trạng thái (| = nhiều), sang trạng thái, cần lý do]
  approve: [["submit", "draft|returned", "submitted"], ["approve", "submitted", "approved"], ["return", "submitted", "returned", true],
    ["adjust", "approved", "draft", true]],
  sign: [["request_sign", "draft|returned", "awaiting_sign"], ["unrequest", "awaiting_sign", "draft"], ["sign", "awaiting_sign", "signed"],
    ["adjust", "signed", "draft", true]],
  national: [["send", "draft|returned", "sent"], ["confirm", "sent", "confirmed"], ["return", "sent", "returned", true]],
};
const EVENT: Record<string, Doc["event"]> = { submit: "submitted", approve: "approved", return: "returned", request_sign: "sign_requested",
  unrequest: "unrequested", sign: "signed", adjust: "adjusted", send: "sent", confirm: "confirmed" };

function step(lc: string, status: string, act: string) {
  return TRANSITIONS[lc]?.find(([a, from]) => a === act && from.split("|").includes(status));
}

function may(w: Who, info: Info, form: string, act: string) {
  if (act === "approve") return approver(w);
  if (act === "return") return form === "07" ? national(w, info) : approver(w);
  if (act === "confirm") return national(w, info);
  return editor(w, info);
}

function sees(w: Who, info: Info, d: Pick<Doc, "form" | "status" | "commune_code">) {
  if (w.role === "kn_xa") return d.form === "05" && !!d.commune_code && !!w.communes?.includes(d.commune_code);
  if (w.role === "kn_tw") return info.project.kind === "knqg" && !(d.form === "07" && d.status === "draft");
  return true;
}

function blockers(info: Info): string[] {
  const out: string[] = [];
  let signed = 0;
  for (const s of info.sites.filter((x) => !x.closed)) {
    const d = [...DOCS.values()].map((v) => v.at(-1)!).find((x) => x.form === "05" && x.site_ref === s.ref);
    if (!d) out.push(`${s.name}: chưa lập Mẫu 05`);
    else if (d.status !== "signed") out.push(`${s.name}: Mẫu 05 chưa ký (bản ${d.revision})`);
    else signed++;
  }
  if (!signed && !out.length) out.push("dự án chưa có điểm trình diễn nào có Mẫu 05 đã ký");
  return out;
}

function issues(sp: Spec, c: Content) {
  const out: { field: string; reason: string }[] = [];
  for (const f of sp.fields) if (f.required && !(c.fields[f.key] ?? "").trim()) out.push({ field: `content.fields.${f.key}`, reason: `bắt buộc (${f.label})` });
  if (sp.form === "05") {
    (c.tables.chi_tieu ?? []).forEach((r, i) => {
      if (r.thuc_te_nhat_ky && r.thuc_te && r.thuc_te !== r.thuc_te_nhat_ky && !r.ghi_chu) {
        out.push({ field: `content.tables.chi_tieu.${i}.ghi_chu`, reason: `"${r.chi_tieu}": thực tế khác số từ nhật ký — ghi chú lý do` });
      }
      if (r.dat === "khong" && !r.ghi_chu) out.push({ field: `content.tables.chi_tieu.${i}.ghi_chu`, reason: `"${r.chi_tieu}": chỉ tiêu không đạt bắt buộc ghi chú` });
    });
  }
  return out;
}

function warnings(sp: Spec, c: Content) {
  if (sp.form !== "01") return [];
  return (c.tables.vat_tu ?? []).filter((r) => r.ke_hoach && r.da_cap && Number(r.da_cap) > Number(r.ke_hoach))
    .map((r) => `Vật tư ${r.ten || "(chưa đặt tên)"}: đã cấp ${r.da_cap} vượt kế hoạch ${r.ke_hoach} (quy tắc 11)`);
}

function fill(sp: Spec, c: Content, info: Info) {
  const p = info.project;
  if (sp.form === "01") {
    Object.assign(c.fields, { ten_du_an: p.name, ma_du_an: p.code, thoi_gian_du_an: `${p.start_date} – ${p.end_date ?? ""}` });
    if (info.manager) c.fields.chu_nhiem = info.manager;
    if (p.budget) c.fields.kinh_phi = p.budget;
  }
  if (sp.form === "07") {
    c.tables.diem = [...DOCS.values()].map((v) => v.at(-1)!).filter((d) => d.form === "05" && d.project_ref === p.ref && d.status === "signed")
      .map((d) => {
        const row = (ma: string) => d.content.tables.chi_tieu?.find((r) => r.ma === ma)?.thuc_te ?? "";
        return { dia_diem: d.content.fields.dia_diem ?? "", quy_mo: row("quy_mo"), so_ho: row("so_ho"), thoi_gian: row("thoi_gian") };
      });
  }
}

function prefill(sp: Spec, info: Info, siteRef: string | null): Content {
  const p = info.project;
  const site = info.sites.find((s) => s.ref === siteRef);
  const c: Content = { fields: {}, tables: {} };
  const basis = p.basis ?? "";
  if (sp.form === "01") {
    if (basis) c.fields.can_cu = basis;
    c.tables.mo_hinh = info.sites.filter((s) => !s.closed).map((s) => ({ dia_diem: s.name, quy_mo_ke_hoach: s.scale, so_ho_ke_hoach: String(s.households.length) }));
    c.tables.tap_huan = [{ noi_dung: "Tập huấn hộ tham gia mô hình" }, { noi_dung: "Tập huấn nhân rộng mô hình" }];
  } else if (sp.form === "03") {
    if (basis) c.fields.can_cu = `Căn cứ ${basis};`;
    c.tables.doi_tuong = [{ ten: p.name, chu_tri: info.manager, quy_mo: `${info.sites.length} điểm trình diễn` }];
    c.tables.ho_so = CHECKLIST.map((ten) => ({ ten }));
  } else if (sp.form === "04") {
    if (basis) c.fields.hop_dong = basis;
    c.fields.ten_lop = `Tập huấn nhân rộng mô hình ${p.name}`;
  } else if (sp.form === "05") {
    if (basis) c.fields.hop_dong = basis;
    c.fields.ten_mo_hinh = p.name;
    if (site) c.fields.dia_diem = site.name;
    c.tables.chi_tieu = (INDICATORS["05"] ?? []).map((r) => ({ ...r, ...(r.ma === "quy_mo" && site ? { yeu_cau: site.scale } : {}),
      ...(r.ma === "so_ho" && site ? { yeu_cau: String(site.households.length) } : {}) }));
    c.tables.phu_luc_1 = (site?.households ?? []).map((h) => ({ chu_ho: h.label, quy_mo: h.scale }));
  } else {
    if (basis) c.fields.hop_dong = basis;
    c.fields.ten_mo_hinh = p.name;
    c.tables.chi_tieu = (INDICATORS["07"] ?? []).map((r) => ({ ...r }));
  }
  fill(sp, c, info);
  return c;
}

/** Bỏ ô/cột/bảng tự điền và giá trị rỗng; khóa lạ → lỗi (như máy chủ). */
function normalize(sp: Spec, c: Content): { content: Content; errors: { field: string; reason: string }[] } {
  const errors: { field: string; reason: string }[] = [];
  const out: Content = { fields: {}, tables: {} };
  for (const [k, v] of Object.entries(c.fields ?? {})) {
    const f = sp.fields.find((x) => x.key === k);
    if (!f) errors.push({ field: `content.fields.${k}`, reason: "ô không có trong mẫu" });
    else if (!f.auto && v.trim()) out.fields[k] = v.trim();
  }
  for (const [k, rows] of Object.entries(c.tables ?? {})) {
    const t = sp.tables.find((x) => x.key === k);
    if (!t) { errors.push({ field: `content.tables.${k}`, reason: "bảng không có trong mẫu" }); continue; }
    if (t.auto) continue;
    if (rows.length > t.max_rows) { errors.push({ field: `content.tables.${k}`, reason: `tối đa ${t.max_rows} dòng` }); continue; }
    out.tables[k] = rows.map((r) => Object.fromEntries(Object.entries(r).filter(([col, v]) => v.trim() && t.columns.some((cc) => cc.key === col && !cc.auto))))
      .filter((r) => Object.keys(r).length > 0);
  }
  if (Object.values(out.fields).some((v) => /(?:\+?84|0)\d{9,10}/.test(v))) errors.push({ field: "content", reason: "không ghi số điện thoại, CCCD trong văn bản" });
  return { content: out, errors };
}

function view(w: Who, info: Info, d: Doc): Doc {
  const sp = spec(d.form)!;
  const open = info.project.event !== "closed";
  const actions = (TRANSITIONS[sp.lifecycle] ?? []).filter(([a, from]) => from.split("|").includes(d.status) && may(w, info, d.form, a) &&
    (w.role !== "kn_tw" || (d.form === "07" && (a === "confirm" || a === "return"))) && (open || a === "confirm" || a === "return"))
    .map(([a]) => a as Doc["actions"][number]);
  return { ...d, can_edit: open && editable(d.status) && editor(w, info), actions, warnings: warnings(sp, d.content),
    issues: editable(d.status) || d.status === "awaiting_sign" ? issues(sp, d.content) : [],
    blockers: d.form === "07" && editable(d.status) ? blockers(info) : [] };
}

function detail(w: Who, ref: string): Doc {
  const vs = DOCS.get(ref)!;
  const cur = vs.at(-1)!;
  const info = cntsMockProjectInfo(cur.project_ref)!;
  return { ...view(w, info, cur), versions: [...vs].reverse().map((x) => ({ version: x.version, revision: x.revision, event: x.event, status: x.status,
    status_reason: x.status_reason, signed: x.signed, at: x.updated_at, actor_ref: x.updated_by, actor_display: x.actor_display })) };
}

function push(w: Who, cur: Doc, patch: Partial<Doc>): Doc {
  const next: Doc = { ...cur, ...patch, version: cur.version + 1, updated_at: new Date().toISOString(), updated_by: w.actor, actor_display: null };
  DOCS.set(cur.ref, [...DOCS.get(cur.ref)!, next]);
  return next;
}

function load(ctx: Ctx, w: Who) {
  const cur = last(ctx.params[0]!);
  const info = cur ? cntsMockProjectInfo(cur.project_ref) : null;
  if (!cur || !info || !sees(w, info, cur)) return null;
  return { cur, info, sp: spec(cur.form)! };
}

const notFound = () => problem(404, "extension.cnts_knqg_not_found", "Không có biên bản/báo cáo dự án KNQG");
const readOnly = () => problem(403, "extension.read_only", "Ngữ cảnh chỉ xem");
const base = (ctx: Ctx) => (ctx.body as { base_version?: number } | undefined)?.base_version;

function multipartMeta(ctx: Ctx): { base?: number; name?: string; caption?: string; size?: number } {
  if (!(ctx.body instanceof Uint8Array)) return {};
  let raw = "";
  for (const x of ctx.body.slice(0, 4096)) raw += String.fromCharCode(x);
  const b = /"base_version"\s*:\s*(\d+)/.exec(raw)?.[1];
  const cap = /name="caption"\r\n\r\n([^\r]*)\r\n/.exec(raw)?.[1];
  return { base: b ? Number(b) : undefined, name: /filename="([^"]+)"/.exec(raw)?.[1], caption: cap ? new TextDecoder().decode(Uint8Array.from(cap, (c) => c.charCodeAt(0))) : undefined,
    size: ctx.body.length };
}

function photoList(w: Who, l: NonNullable<ReturnType<typeof load>>, replayed = false): S["CNTSKNQGPhotoList"] {
  return { items: PHOTOS.get(l.cur.ref) ?? [], can_edit: l.cur.form === "03" && editable(l.cur.status) && editor(w, l.info), max_photos: MAX_PHOTOS, replayed };
}

export const cntsKnqgRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/extension\/cnts\/knqg-specs$/, handler: () => json({ items: SPECS }) },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs$/,
    handler: (ctx) => {
      const w = who(ctx);
      const q = ctx.url.searchParams;
      const items = [...DOCS.values()].map((v) => v.at(-1)!).flatMap((d) => {
        const info = cntsMockProjectInfo(d.project_ref);
        if (!info || !sees(w, info, d) || (q.get("project_ref") && d.project_ref !== q.get("project_ref")) ||
          (q.get("form") && d.form !== q.get("form")) || (q.get("status") && d.status !== q.get("status"))) return [];
        const v: Partial<Doc> = view(w, info, d);
        for (const k of ["content", "issues", "blockers", "versions"] as const) delete v[k];
        return [v as S["CNTSKNQGDoc"]];
      });
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.readOnly || w.role === "kn_tw") return readOnly();
      const b = (ctx.body ?? {}) as S["CNTSKNQGCreate"];
      const sp = spec(b.form);
      const info = cntsMockProjectInfo(b.project_ref);
      if (!sp) return problem(422, "extension.cnts_knqg_invalid", "Mẫu biểu dự án KNQG không hợp lệ");
      if (!info) return notFound();
      if (info.project.kind !== "knqg") return problem(422, "extension.cnts_knqg_project_kind", "Mẫu biểu KNQG chỉ lập cho dự án loại khuyến nông quốc gia");
      if (!editor(w, info)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin lập, sửa mẫu biểu dự án (FN-19 BR-01)");
      if (info.project.event === "closed") return problem(409, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa");
      const period = b.period ?? null;
      if (sp.period === "month" && !/^\d{4}-(0[1-9]|1[0-2])$/.test(period ?? "")) return validation([{ field: "period", reason: "tháng dạng YYYY-MM" }]);
      if (sp.period === "year" && !/^\d{4}$/.test(period ?? "")) return validation([{ field: "period", reason: "năm dạng YYYY" }]);
      const site = sp.per_site ? info.sites.find((s) => s.ref === b.site_ref && !s.closed) : undefined;
      if (sp.per_site && !site) return validation([{ field: "site_ref", reason: "Mẫu 05 lập cho một điểm trình diễn của dự án" }]);
      const dupl = [...DOCS.values()].map((v) => v.at(-1)!).some((d) => d.project_ref === b.project_ref && d.form === b.form && sp.unique &&
        (sp.per_site ? d.site_ref === b.site_ref : d.period === period));
      if (dupl) return problem(409, "extension.cnts_knqg_exists", "Đã có biểu hiệu lực cho dự án ở kỳ/điểm này");
      const ref = b.id ?? uuidv7();
      const d: Doc = { ref, form: sp.form, project_ref: info.project.ref, project_code: info.project.code, project_name: info.project.name,
        site_ref: site?.ref ?? null, site_name: site?.name ?? null, commune_code: site?.commune_code ?? null, period, revision: 1, event: "created",
        status: "draft", status_reason: null, signed: null, version: 1, updated_at: new Date().toISOString(), updated_by: w.actor, actor_display: null,
        can_edit: true, actions: [], warnings: [], content: prefill(sp, info, site?.ref ?? null), issues: [], blockers: [], versions: [] };
      DOCS.set(ref, [d]);
      return json(detail(w, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      return load(ctx, w) ? json(detail(w, ctx.params[0]!)) : notFound();
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.readOnly || w.role === "kn_tw") return readOnly();
      const l = load(ctx, w);
      if (!l) return notFound();
      if (!editor(w, l.info)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin sửa biểu (FN-19 BR-01)");
      if (!editable(l.cur.status)) return problem(409, "extension.cnts_knqg_locked", "Biểu đã duyệt/ký/xác nhận — không sửa được");
      if (base(ctx) !== l.cur.version) return problem(409, "extension.cnts_knqg_version_conflict", "Biểu đã có người cập nhật — tải lại bản mới");
      const { content, errors } = normalize(l.sp, (ctx.body as S["CNTSKNQGUpdate"]).content);
      if (errors.length) return problem(422, "extension.cnts_knqg_invalid", "Mẫu biểu dự án KNQG không hợp lệ", { errors });
      if (l.sp.form === "05") content.tables.chi_tieu = (content.tables.chi_tieu ?? []).map((r) => ({ ...r }));
      fill(l.sp, content, l.info);
      push(w, l.cur, { event: "updated", content, status_reason: null });
      return json(detail(w, l.cur.ref));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/actions\/([a-z_]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const act = ctx.params[1]!;
      const l = load(ctx, w);
      if (!l) return notFound();
      if (w.readOnly || (w.role === "kn_tw" && !(l.cur.form === "07" && (act === "confirm" || act === "return")))) return readOnly();
      const st = step(l.sp.lifecycle, l.cur.status, act);
      if (!st || act === "sign") return problem(409, "extension.cnts_knqg_state", "Thao tác không hợp lệ ở trạng thái hiện tại của biểu");
      if (!may(w, l.info, l.cur.form, act)) return problem(403, "forbidden", "Không có quyền thao tác này");
      const reason = ((ctx.body as { reason?: string } | undefined)?.reason ?? "").trim();
      if (st[3] && reason.length < 5) return validation([{ field: "reason", reason: "ý kiến/lý do 5–1000 ký tự" }]);
      if (base(ctx) !== l.cur.version) return problem(409, "extension.cnts_knqg_version_conflict", "Biểu đã có người cập nhật — tải lại bản mới");
      const content = structuredClone(l.cur.content);
      fill(l.sp, content, l.info);
      if (["submit", "request_sign", "send"].includes(act)) {
        const errs = issues(l.sp, content);
        if (errs.length) return problem(422, "extension.cnts_knqg_incomplete", "Biểu chưa đủ nội dung bắt buộc", { errors: errs });
      }
      if (act === "send") {
        const b = blockers(l.info);
        if (b.length) return problem(409, "extension.cnts_knqg_needs_signed_05", "Mẫu 07 chỉ gửi khi mọi điểm có Mẫu 05 đã ký", { detail: b.join("; ") });
      }
      push(w, l.cur, { event: EVENT[act], status: st[2] as Doc["status"], content, status_reason: reason || null,
        ...(act === "adjust" ? { revision: l.cur.revision + 1, signed: null } : {}) });
      return json(detail(w, l.cur.ref));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/signed$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.readOnly || w.role === "kn_tw") return readOnly();
      const l = load(ctx, w);
      if (!l) return notFound();
      if (l.cur.status !== "awaiting_sign") return problem(409, "extension.cnts_knqg_state", "chỉ ký biểu đang Chờ ký");
      if (!editor(w, l.info)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin tải bản đã ký");
      const m = multipartMeta(ctx);
      if (!m.name || !/\.(pdf|jpe?g|png)$/i.test(m.name)) return problem(422, "extension.cnts_knqg_scan_invalid", "Bản scan phải là PDF, JPG hoặc PNG, tối đa 10 MB");
      if (m.base !== l.cur.version) return problem(409, "extension.cnts_knqg_version_conflict", "Biểu đã có người cập nhật — tải lại bản mới");
      push(w, l.cur, { event: "signed", status: "signed", signed: { name: m.name, content_type: "application/pdf", size: 1024, sha256: "0".repeat(64) } });
      return json(detail(w, l.cur.ref));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/signed$/,
    handler: (ctx) => {
      const w = who(ctx);
      const l = load(ctx, w);
      const scan = l ? [...DOCS.get(l.cur.ref)!].reverse().find((x) => x.signed)?.signed : null;
      if (!scan) return problem(404, "extension.cnts_knqg_scan_not_found", "Biểu chưa có bản scan đã ký");
      return new Response(new Blob(["%PDF-1.4\n%%EOF\n"]), { status: 200, headers: { "Content-Type": "application/pdf",
        "Content-Disposition": `attachment; filename="${encodeURIComponent(scan.name)}"` } });
    },
  },
  // W11.53b: ảnh thực địa Mẫu 03 (giả lập: chỉ giữ siêu dữ liệu, tải ảnh trả PNG 1×1).
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/photos$/,
    handler: (ctx) => {
      const w = who(ctx);
      const l = load(ctx, w);
      return l ? json(photoList(w, l)) : notFound();
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/photos$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.readOnly || w.role === "kn_tw") return readOnly();
      const l = load(ctx, w);
      if (!l) return notFound();
      if (l.cur.form !== "03") return problem(422, "extension.cnts_knqg_photo_invalid", "chỉ báo cáo kiểm tra Mẫu 03 có ảnh thực địa");
      if (!editor(w, l.info)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin thêm/gỡ ảnh");
      if (!editable(l.cur.status)) return problem(409, "extension.cnts_knqg_locked", "báo cáo đã trình ký/ký — lập bản điều chỉnh để thêm/gỡ ảnh");
      const m = multipartMeta(ctx);
      const items = PHOTOS.get(l.cur.ref) ?? [];
      if (!m.name || !/\.(jpe?g|png)$/i.test(m.name) || (m.size ?? 0) > 5 * 1024 * 1024 || items.length >= MAX_PHOTOS)
        return problem(422, "extension.cnts_knqg_photo_invalid", "Ảnh thực địa phải là JPG hoặc PNG, tối đa 5 MB; tối đa 20 ảnh mỗi báo cáo");
      PHOTOS.set(l.cur.ref, [...items, { ref: uuidv7(), name: m.name, content_type: /\.png$/i.test(m.name) ? "image/png" : "image/jpeg",
        size: m.size ?? 0, sha256: "0".repeat(64), caption: m.caption?.trim() || null, uploaded_at: new Date().toISOString(), uploaded_by: w.actor,
        uploaded_by_name: null }]);
      return json(photoList(w, l));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/photos\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const l = load(ctx, w);
      const ph = l ? (PHOTOS.get(l.cur.ref) ?? []).find((x) => x.ref === ctx.params[1]) : null;
      if (!ph) return problem(404, "extension.cnts_knqg_photo_not_found", "Không có ảnh thực địa này trong báo cáo");
      const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="), (c) => c.charCodeAt(0));
      return new Response(new Blob([png]), { status: 200, headers: { "Content-Type": "image/png" } });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/photos\/([^/]+)\/remove$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.readOnly || w.role === "kn_tw") return readOnly();
      const l = load(ctx, w);
      if (!l) return notFound();
      if (!editor(w, l.info)) return problem(403, "forbidden", "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin thêm/gỡ ảnh");
      if (!editable(l.cur.status)) return problem(409, "extension.cnts_knqg_locked", "báo cáo đã trình ký/ký — lập bản điều chỉnh để thêm/gỡ ảnh");
      const items = PHOTOS.get(l.cur.ref) ?? [];
      if (!items.some((x) => x.ref === ctx.params[1])) return problem(404, "extension.cnts_knqg_photo_not_found", "Không có ảnh thực địa này trong báo cáo");
      PHOTOS.set(l.cur.ref, items.filter((x) => x.ref !== ctx.params[1]));
      return json(photoList(w, l));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/word$/,
    handler: (ctx) => {
      const w = who(ctx);
      const l = load(ctx, w);
      if (!l) return notFound();
      const runs = WORDS.get(l.cur.ref) ?? [];
      const no = l.sp.numbered ? (runs[0]?.doc_number || `${++reportNo}/BC-TTKN`) : "";
      const run = { id: uuidv7(), seq: runs.length + 1, template: `cnts.knqg_${l.cur.form}`, template_version: 1, doc_number: no, doc_number_auto: !!no,
        file_name: `mau-${l.cur.form}-${l.cur.ref}-r${l.cur.revision}-v${l.cur.version}.docx`, body_hash: "0".repeat(64), size: 4, actor_ref: w.actor,
        at: new Date().toISOString() };
      WORDS.set(l.cur.ref, [run, ...runs]);
      return json(run, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/words$/,
    handler: (ctx) => json({ items: WORDS.get(ctx.params[0]!) ?? [] }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/knqg-docs\/([^/]+)\/words\/([^/]+)\/download$/,
    handler: (ctx) => {
      const run = (WORDS.get(ctx.params[0]!) ?? []).find((r) => r.id === ctx.params[1]);
      if (!run) return problem(404, "export.run_not_found", "Không có lần xuất này");
      return new Response(new Blob(["PK\u0003\u0004"]), { status: 200, headers: {
        "Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Content-Disposition": `attachment; filename="${run.file_name}"` } });
    },
  },
];
