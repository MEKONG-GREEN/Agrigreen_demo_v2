// GIẢ LẬP Trang chủ phòng + kho văn bản phòng (W11.54 — D4 CN14; BRD CNTS v3.7 FN-17, FN-20) theo hợp đồng
// /v1/extension/dept-home*, /v1/extension/dept-docs*: thẻ theo phòng, giới thiệu có phiên bản (Lãnh đạo phòng sửa),
// thư mục, văn bản có phiên bản + tệp (≤ 20 MB, PDF/DOCX/XLSX), ẩn = xóa, hết hiệu lực vẫn tìm được. Dữ liệu tổng hợp.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Doc = S["DeptDoc"];
type Folder = S["DeptDocFolder"];
type Profile = S["DeptProfile"];

const DEPTS = ["trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"];
const KINDS: Doc["kind"][] = ["quy_trinh_ky_thuat", "van_ban_phap_quy", "van_ban_chi_dao", "ke_hoach", "mau_bieu", "tai_lieu_tap_huan", "khac"];
const MAX = 20 * 1024 * 1024;
const FORMATS: Record<string, { ct: string; magic: number[] }> = {
  ".pdf": { ct: "application/pdf", magic: [0x25, 0x50, 0x44, 0x46, 0x2d] },
  ".docx": { ct: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", magic: [0x50, 0x4b, 3, 4] },
  ".xlsx": { ct: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", magic: [0x50, 0x4b, 3, 4] },
};

interface Store {
  docs: Map<string, Doc[]>; // ref → phiên bản (mới nhất cuối)
  folders: Map<string, Folder>;
  profiles: Map<string, Profile[]>;
  files: Map<string, Uint8Array>; // sha giả → nội dung
}
let STORE: Store | null = null;

const now = () => new Date().toISOString();
const today = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const enc = (s: string) => new TextEncoder().encode(s);
const pdf = (s: string) => enc(`%PDF-1.4\n% ${s}\n%%EOF`);
const sys = { ref: "00000000-0000-7000-8000-000000000001", display: "Cán bộ tổng hợp (giả lập)" };

function validity(d: Pick<Doc, "effective_from" | "effective_to">): Doc["validity"] {
  const t = today();
  if (d.effective_to && d.effective_to < t) return "expired";
  if (d.effective_from && d.effective_from > t) return "pending";
  return "active";
}

function seedDoc(s: Store, dept: string, folder: string | null, title: string, kind: Doc["kind"], extra: Partial<Doc> = {}) {
  const ref = uuidv7();
  const bytes = pdf(title);
  const sha = `${ref.replace(/-/g, "")}`.padEnd(64, "0").slice(0, 64);
  s.files.set(sha, bytes);
  const d: Doc = {
    ref, version: 1, event: "created", department_code: dept, folder_ref: folder, title, kind, field: null, number: null, issuer: null,
    summary: null, issued_on: null, effective_from: null, effective_to: null, audience: ["xa"], hidden: false, status_reason: null,
    file: { version: 1, name: `${title.slice(0, 40)}.pdf`, content_type: "application/pdf", size_bytes: bytes.length, sha256: sha },
    recorded_at: now(), actor: sys, validity: "active", folder_path: [], can_edit: false, can_manage: false, ...extra,
  };
  d.validity = validity(d);
  s.docs.set(ref, [d]);
}

function store(): Store {
  if (STORE) return STORE;
  const s: Store = { docs: new Map(), folders: new Map(), profiles: new Map(), files: new Map() };
  for (const dept of DEPTS) {
    const f: Folder = { ref: uuidv7(), department_code: dept, parent_ref: null, name: "Văn bản pháp quy", path: ["Văn bản pháp quy"], hidden: false,
      status_reason: null, docs: 0, version: 1, event: "created", recorded_at: now() };
    const q: Folder = { ...f, ref: uuidv7(), name: "Quy trình kỹ thuật", path: ["Quy trình kỹ thuật"] };
    s.folders.set(f.ref, f);
    s.folders.set(q.ref, q);
    seedDoc(s, dept, f.ref, "Nghị định 83/2018/NĐ-CP về khuyến nông", "van_ban_phap_quy",
      { number: "83/2018/NĐ-CP", issuer: "Chính phủ", issued_on: "2018-05-24", effective_from: "2018-07-10", audience: ["nong_ho", "xa"], field: "Khuyến nông" });
    seedDoc(s, dept, null, "Kế hoạch công tác năm 2025", "ke_hoach", { effective_from: "2025-01-01", effective_to: "2025-12-31", audience: [] });
    if (dept === "chan_nuoi_thuy_san") {
      seedDoc(s, dept, q.ref, "Quy trình tiêm phòng gia cầm", "quy_trinh_ky_thuat", { field: "Gia cầm" });
      seedDoc(s, dept, q.ref, "Quy trình nuôi tôm thẻ chân trắng", "quy_trinh_ky_thuat", { field: "Thủy sản" });
    }
    if (dept === "trong_trot") seedDoc(s, dept, q.ref, "Quy trình canh tác lúa giảm phát thải", "quy_trinh_ky_thuat", { field: "Lúa" });
  }
  s.profiles.set("chan_nuoi_thuy_san", [{ department_code: "chan_nuoi_thuy_san", version: 1, recorded_at: now(), actor: sys,
    intro: "Phòng Chăn nuôi – Thủy sản, Trung tâm Khuyến nông tỉnh An Giang.",
    duties: "1. Theo dõi tổng đàn, tiêm phòng, dịch bệnh.\n2. Tổng hợp báo cáo KSGM, khai thác thủy sản.\n3. Triển khai mô hình, dự án KNQG." }]);
  STORE = s;
  return s;
}

/** Xóa trạng thái (kiểm thử). */
export function resetDeptHome() {
  STORE = null;
}

interface Who { role: string; dept: string; manager: boolean; readOnly: boolean; actor: string }

function who(ctx: Ctx): Who | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const role = kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
  const q = ctx.url.searchParams.get("department") ?? "";
  let dept = kc?.department_code ?? "trong_trot";
  if (role === "admin") {
    if (!q) return validation([{ field: "department", reason: "Admin chọn phòng ban" }]);
    dept = q;
  } else if (q && q !== dept) return problem(403, "forbidden", "chỉ xem được phòng ban của ngữ cảnh đang chọn");
  if (!DEPTS.includes(dept)) return validation([{ field: "department", reason: "phòng ban KN không hợp lệ" }]);
  return { role, dept, manager: role === "admin" || (role === "kn_tinh" && !!kc?.manager), readOnly: !!kc?.read_only, actor: ctx.persona.me.actor_ref };
}

const writer = (w: Who) => !w.readOnly && (w.role === "admin" || w.role === "kn_tinh");
const canManage = (w: Who) => writer(w) && w.manager;

function guard(ctx: Ctx, roles: string[], need: "read" | "write" | "manage" = "read"): Who | Response {
  const w = who(ctx);
  if (w instanceof Response) return w;
  if (!roles.includes(w.role)) return problem(403, "forbidden", "Không có quyền");
  if (need === "read") return w;
  if (w.role === "kn_xa") return problem(403, "forbidden", "Cấp Xã chỉ xem, tải văn bản (FN-20)");
  if (w.readOnly) return problem(403, "extension.read_only", "Ngữ cảnh chỉ xem");
  if (need === "manage" && !w.manager) return problem(403, "forbidden", "chỉ Lãnh đạo phòng ẩn, hiện lại văn bản và thư mục");
  return w;
}

const LIB = ["admin", "kn_tinh", "kn_xa"];
const HOME = ["admin", "kn_tinh"];

function folderPath(ref: string | null): string[] {
  const out: string[] = [];
  let cur = ref ? store().folders.get(ref) : undefined;
  for (let i = 0; cur && i < 7; i++) {
    out.unshift(cur.name);
    cur = cur.parent_ref ? store().folders.get(cur.parent_ref) : undefined;
  }
  return out;
}
const folderHidden = (ref: string | null): boolean => {
  let cur = ref ? store().folders.get(ref) : undefined;
  for (let i = 0; cur && i < 7; i++) {
    if (cur.hidden) return true;
    cur = cur.parent_ref ? store().folders.get(cur.parent_ref) : undefined;
  }
  return false;
};

function view(w: Who, vs: Doc[], withVersions = false): Doc {
  const d = vs.at(-1)!;
  const out: Doc = { ...d, validity: validity(d), folder_path: folderPath(d.folder_ref), can_edit: writer(w), can_manage: canManage(w) };
  if (withVersions) out.versions = [...vs].reverse().map((v) => {
    const rest: Partial<Doc> = { ...v };
    for (const k of ["validity", "folder_path", "can_edit", "can_manage", "versions"] as const) delete rest[k];
    return rest as S["DeptDocVersion"];
  });
  else delete out.versions;
  return out;
}

const visible = (w: Who, d: Doc) => d.department_code === w.dept && (w.role !== "kn_xa" || d.audience.includes("xa"));
const shown = (w: Who, d: Doc) => visible(w, d) && ((!d.hidden && !folderHidden(d.folder_ref)) || w.manager);
const notFound = () => problem(404, "extension.dept_doc_not_found", "Không có văn bản này");

function parseMultipart(ctx: Ctx): Record<string, { text: string; bytes: Uint8Array; filename?: string }> | null {
  const ct = ctx.request.headers.get("Content-Type") ?? "";
  const b = /boundary=([^;]+)/.exec(ct)?.[1]?.replace(/^"|"$/g, "");
  if (!b || !(ctx.body instanceof Uint8Array)) return null;
  let raw = "";
  for (const x of ctx.body) raw += String.fromCharCode(x);
  const dec = new TextDecoder();
  const out: Record<string, { text: string; bytes: Uint8Array; filename?: string }> = {};
  for (const part of raw.split(`--${b}`)) {
    const i = part.indexOf("\r\n\r\n");
    if (i < 0) continue;
    const head = part.slice(0, i);
    const bytes = Uint8Array.from(part.slice(i + 4).replace(/\r\n$/, ""), (ch) => ch.charCodeAt(0));
    const name = /name="([^"]*)"/.exec(head)?.[1];
    const fn = /filename="([^"]*)"/.exec(head)?.[1];
    if (name) out[name] = { text: dec.decode(bytes), bytes, filename: fn ? dec.decode(Uint8Array.from(fn, (c) => c.charCodeAt(0))) : undefined };
  }
  return out;
}

function fileOf(part: { bytes: Uint8Array; filename?: string } | undefined, version: number): Doc["file"] | Response {
  if (!part?.filename) return validation([{ field: "file", reason: "bắt buộc" }]);
  if (part.bytes.length > MAX) return problem(413, "extension.dept_doc_too_large", "Tệp vượt 20 MB");
  const ext = /\.[^.]+$/.exec(part.filename.toLowerCase())?.[0] ?? "";
  const f = FORMATS[ext];
  if (!f || !f.magic.every((x, i) => part.bytes[i] === x)) return problem(422, "extension.dept_doc_file_type", "Định dạng tệp không được phép");
  const sha = uuidv7().replace(/-/g, "").padEnd(64, "0");
  store().files.set(sha, part.bytes);
  return { version, name: part.filename, content_type: f.ct, size_bytes: part.bytes.length, sha256: sha };
}

function checkMeta(m: S["DeptDocInput"]): Response | null {
  const e: { field: string; reason: string }[] = [];
  if (!m.title?.trim()) e.push({ field: "title", reason: "bắt buộc, tối đa 300 ký tự" });
  if (!KINDS.includes(m.kind)) e.push({ field: "kind", reason: "loại văn bản không hợp lệ" });
  if (m.effective_from && m.effective_to && m.effective_to < m.effective_from) e.push({ field: "effective_to", reason: "ngày hết hiệu lực trước ngày có hiệu lực" });
  if (m.folder_ref && (!store().folders.get(m.folder_ref) || folderHidden(m.folder_ref))) return problem(404, "extension.dept_folder_not_found", "Không có thư mục này");
  return e.length ? problem(422, "extension.dept_doc_invalid", "Văn bản không hợp lệ", { errors: e }) : null;
}

const metaOf = (m: S["DeptDocInput"]) => ({
  title: m.title.trim(), kind: m.kind, folder_ref: m.folder_ref ?? null, field: m.field?.trim() || null, number: m.number?.trim() || null,
  issuer: m.issuer?.trim() || null, summary: m.summary?.trim() || null, issued_on: m.issued_on || null, effective_from: m.effective_from || null,
  effective_to: m.effective_to || null, audience: [...new Set(m.audience ?? [])].sort(),
});

function push(w: Who, ref: string, event: Doc["event"], patch: Partial<Doc>): Doc {
  const vs = store().docs.get(ref)!;
  const cur = vs.at(-1)!;
  vs.push({ ...cur, ...patch, version: cur.version + 1, event, recorded_at: now(), actor: { ref: w.actor, display: null } });
  return view(w, vs, true);
}

function kpis(w: Who, docs: Doc[]): S["DeptKPI"][] {
  const out: S["DeptKPI"][] = [{ key: "schedules_week", label: "Lịch công tác tuần này", value: 6, tone: "info" }];
  if (w.manager) out.push({ key: "schedules_pending", label: "Lịch công tác chờ duyệt", value: 2, tone: "warn" });
  const by: Record<string, S["DeptKPI"][]> = {
    chan_nuoi_thuy_san: [
      { key: "alerts_open", label: "Cảnh báo cần xử lý", value: 5, tone: "warn" },
      { key: "alerts_overdue", label: "Cảnh báo quá hạn", value: 1, tone: "bad" },
      { key: "outbreaks_active", label: "Ổ dịch đang theo dõi", value: 2, tone: "bad" },
    ],
    trong_trot: [
      { key: "tasks_open", label: "Nhiệm vụ đang mở", value: 9, tone: "info" },
      { key: "tasks_overdue", label: "Nhiệm vụ quá hạn", value: 1, tone: "bad" },
      { key: "field_reports_week", label: "Báo cáo hiện trường 7 ngày", value: 24, tone: "info" },
    ],
    ke_hoach_tai_chinh: [
      { key: "khtc_periods_year", label: "Kỳ báo cáo đã mở trong năm", value: 14, tone: "info" },
      { key: "khtc_exports", label: "Lần xuất báo cáo Mẫu 1–5", value: 11, tone: "info" },
    ],
    thong_tin_truyen_thong: [
      { key: "price_items", label: "Mặt hàng trên bảng giá đã duyệt gần nhất", value: 42, tone: "info" },
      { key: "price_tickets_week", label: "Phiếu báo giá tuần gần nhất", value: 118, tone: "info" },
    ],
  };
  out.push(...(by[w.dept] ?? []));
  out.push({ key: "docs_total", label: "Văn bản trong kho", value: docs.length, tone: "info" },
    { key: "docs_expired", label: "Văn bản hết hiệu lực", value: docs.filter((d) => d.validity === "expired").length, tone: "neutral" });
  return out;
}

function closedPeriods(dept: string): S["DeptClosedPeriod"][] {
  if (dept !== "chan_nuoi_thuy_san" && dept !== "trong_trot") return [];
  const form = dept === "trong_trot" ? "TT-TUAN" : "CNTS-BM01";
  return [0, 1, 2, 3].map((i) => {
    const to = new Date(Date.now() - (i * 7 + 1) * 86_400_000).toISOString().slice(0, 10);
    return { form_code: form, period_type: "week", period_key: `W-${i}`, period_label: `Tuần kết thúc ${to.split("-").reverse().join("/")}`,
      period_to: to, units_total: 102, units_with_data: i === 0 ? 87 : 102, closed: i > 0, closed_at: i > 0 ? now() : null };
  });
}

export const deptHomeRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-home$/,
    handler: (ctx) => {
      const w = guard(ctx, HOME);
      if (w instanceof Response) return w;
      const docs = [...store().docs.values()].map((vs) => view(w, vs)).filter((d) => d.department_code === w.dept && !d.hidden && !folderHidden(d.folder_ref));
      const prof = store().profiles.get(w.dept)?.at(-1) ?? null;
      const body: S["DeptHome"] = {
        department_code: w.dept, as_of: now(), manager: w.role === "admin" || w.manager || false, read_only: w.readOnly,
        can_edit_profile: canManage(w), profile: prof, kpis: kpis(w, docs), closed_periods: closedPeriods(w.dept),
        price: w.dept === "thong_tin_truyen_thong" ? { board_date: today(), items: 42, week_key: "2026-W42", tickets: 118, week_days_approved: 3 } : null,
        officers: [
          { actor: { ref: uuidv7(), display: "Trưởng phòng (giả lập)" }, mgmt_level: "truong_phong", leader: true },
          { actor: { ref: uuidv7(), display: "Phó trưởng phòng (giả lập)" }, mgmt_level: "pho_truong_phong", leader: true },
          { actor: { ref: uuidv7(), display: "Viên chức A (giả lập)" }, mgmt_level: "chuyen_vien", leader: false },
          { actor: { ref: uuidv7(), display: "Viên chức B (giả lập)" }, mgmt_level: "chuyen_vien", leader: false },
        ],
        library: { total: docs.length, expired: docs.filter((d) => d.validity === "expired").length, new_30d: docs.length },
        latest_docs: docs.slice(0, 5),
      };
      return json(body);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/dept-home\/profile$/,
    handler: (ctx) => {
      const w = guard(ctx, HOME, "write");
      if (w instanceof Response) return w;
      if (!w.manager) return problem(403, "forbidden", "chỉ Lãnh đạo phòng sửa giới thiệu, chức năng nhiệm vụ (FN-17)");
      const b = (ctx.body ?? {}) as S["DeptProfileInput"];
      const list = store().profiles.get(w.dept) ?? [];
      const cur = list.at(-1)?.version ?? 0;
      if (b.base_version !== cur) return problem(409, "extension.dept_profile_version_conflict", "Giới thiệu phòng đã có người cập nhật — tải lại bản mới");
      if (!b.intro?.trim() && !b.duties?.trim()) return problem(422, "extension.dept_profile_invalid", "Giới thiệu phòng không hợp lệ", { errors: [{ field: "intro", reason: "nhập giới thiệu hoặc chức năng nhiệm vụ" }] });
      const p: Profile = { department_code: w.dept, intro: b.intro?.trim() ?? "", duties: b.duties?.trim() ?? "", version: cur + 1, recorded_at: now(), actor: { ref: w.actor, display: null } };
      store().profiles.set(w.dept, [...list, p]);
      return json(p, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-home\/profile\/history$/,
    handler: (ctx) => {
      const w = guard(ctx, HOME);
      if (w instanceof Response) return w;
      return json({ items: [...(store().profiles.get(w.dept) ?? [])].reverse() });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-docs\/folders$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB);
      if (w instanceof Response) return w;
      const inc = ctx.url.searchParams.get("include_hidden") === "1" && canManage(w);
      const items = [...store().folders.values()].filter((f) => f.department_code === w.dept && (inc || !folderHidden(f.ref))).map((f) => ({
        ...f, path: folderPath(f.ref), hidden: folderHidden(f.ref),
        docs: [...store().docs.values()].filter((vs) => { const d = vs.at(-1)!; return d.folder_ref === f.ref && !d.hidden && visible(w, d); }).length,
      })).sort((a, b) => a.path.join("/").localeCompare(b.path.join("/"), "vi"));
      return json({ department_code: w.dept, can_write: writer(w), can_manage: canManage(w), items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/dept-docs\/folders$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB, "write");
      if (w instanceof Response) return w;
      const b = (ctx.body ?? {}) as S["DeptDocFolderInput"];
      const name = (b.name ?? "").trim().replace(/\s+/g, " ");
      if (!name || /[/\\]/.test(name)) return problem(422, "extension.dept_folder_invalid", "Thư mục không hợp lệ", { errors: [{ field: "name", reason: "bắt buộc, không có '/' '\\'" }] });
      const sib = [...store().folders.values()].some((f) => f.department_code === w.dept && (f.parent_ref ?? null) === (b.parent_ref ?? null) && f.name.toLowerCase() === name.toLowerCase());
      if (sib) return problem(409, "extension.dept_folder_exists", "Đã có thư mục cùng tên ở cùng cấp");
      if (b.parent_ref && !store().folders.get(b.parent_ref)) return problem(404, "extension.dept_folder_not_found", "Không có thư mục này");
      const f: Folder = { ref: uuidv7(), department_code: w.dept, parent_ref: b.parent_ref ?? null, name, path: [], hidden: false, status_reason: null, docs: 0,
        version: 1, event: "created", recorded_at: now() };
      store().folders.set(f.ref, f);
      return json({ ...f, path: folderPath(f.ref) }, 201);
    },
  },
  ...(["rename", "hide", "restore"] as const).map((act): Route => ({
    method: "POST",
    pattern: new RegExp(`^/v1/extension/dept-docs/folders/([^/]+)/${act}$`),
    handler: (ctx) => {
      const w = guard(ctx, LIB, act === "rename" ? "write" : "manage");
      if (w instanceof Response) return w;
      const f = store().folders.get(ctx.params[0]!);
      if (!f || f.department_code !== w.dept) return problem(404, "extension.dept_folder_not_found", "Không có thư mục này");
      const b = (ctx.body ?? {}) as S["DeptDocFolderChange"];
      if (b.base_version !== f.version) return problem(409, "extension.dept_folder_version_conflict", "Thư mục đã có người cập nhật — tải lại bản mới");
      if (act !== "rename" && (b.reason ?? "").trim().length < 5) return problem(422, "extension.dept_folder_invalid", "Thư mục không hợp lệ", { errors: [{ field: "reason", reason: "bắt buộc, 5–1000 ký tự" }] });
      if (act === "hide") {
        const busy = [...store().folders.values()].some((x) => x.parent_ref === f.ref && !x.hidden) ||
          [...store().docs.values()].some((vs) => vs.at(-1)!.folder_ref === f.ref && !vs.at(-1)!.hidden);
        if (busy) return problem(409, "extension.dept_folder_not_empty", "Thư mục còn thư mục con hoặc văn bản đang hiện");
      }
      const next: Folder = { ...f, version: f.version + 1, recorded_at: now(), event: act === "rename" ? "renamed" : act === "hide" ? "hidden" : "restored",
        name: act === "rename" ? (b.name ?? f.name).trim() : f.name, hidden: act === "hide" ? true : act === "restore" ? false : f.hidden,
        status_reason: act === "rename" ? null : (b.reason ?? "").trim() };
      store().folders.set(f.ref, next);
      return json({ ...next, path: folderPath(f.ref) });
    },
  })),
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-docs$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB);
      if (w instanceof Response) return w;
      const p = ctx.url.searchParams;
      const inc = p.get("include_hidden") === "1" && canManage(w);
      const q = (p.get("q") ?? "").trim().toLowerCase();
      const items = [...store().docs.values()].map((vs) => view(w, vs)).filter((d) =>
        visible(w, d) && (inc || (!d.hidden && !folderHidden(d.folder_ref))) && (!p.get("folder") || d.folder_ref === p.get("folder")) &&
        (!p.get("kind") || d.kind === p.get("kind")) && (!p.get("validity") || d.validity === p.get("validity")) &&
        (!q || [d.title, d.number, d.issuer, d.field, d.summary, d.file.name].join(" ").toLowerCase().includes(q)));
      items.sort((a, b) => b.recorded_at.localeCompare(a.recorded_at));
      const body: S["DeptDocList"] = { department_code: w.dept, can_write: writer(w), can_manage: canManage(w), total: items.length, kinds: KINDS,
        extensions: Object.keys(FORMATS), max_bytes: MAX, items };
      return json(body);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/dept-docs$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB, "write");
      if (w instanceof Response) return w;
      const mp = parseMultipart(ctx);
      if (!mp) return validation([{ field: "file", reason: "thân phải là multipart/form-data có phần meta và file" }]);
      let m: S["DeptDocInput"];
      try {
        m = JSON.parse(mp.meta?.text ?? "") as S["DeptDocInput"];
      } catch {
        return validation([{ field: "meta", reason: "JSON thuộc tính không hợp lệ" }]);
      }
      const bad = checkMeta(m);
      if (bad) return bad;
      const file = fileOf(mp.file, 1);
      if (file instanceof Response) return file;
      const ref = uuidv7();
      const d: Doc = { ref, version: 1, event: "created", department_code: w.dept, ...metaOf(m), file, hidden: false, status_reason: null, recorded_at: now(),
        actor: { ref: w.actor, display: null }, validity: "active", folder_path: [], can_edit: true, can_manage: false } as Doc;
      store().docs.set(ref, [d]);
      return json(view(w, [d], true), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-docs\/([^/]+)$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB);
      if (w instanceof Response) return w;
      const vs = store().docs.get(ctx.params[0]!);
      if (!vs || !shown(w, vs.at(-1)!)) return notFound();
      return json(view(w, vs, true));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/dept-docs\/([^/]+)\/update$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB, "write");
      if (w instanceof Response) return w;
      const vs = store().docs.get(ctx.params[0]!);
      if (!vs || !shown(w, vs.at(-1)!)) return notFound();
      const m = (ctx.body ?? {}) as S["DeptDocInput"];
      if (m.base_version !== vs.at(-1)!.version) return problem(409, "extension.dept_doc_version_conflict", "Văn bản đã có người cập nhật — tải lại bản mới");
      if (vs.at(-1)!.hidden) return problem(409, "extension.dept_doc_state", "Văn bản đang ẩn — hiện lại trước khi sửa");
      const bad = checkMeta(m);
      if (bad) return bad;
      return json(push(w, ctx.params[0]!, "updated", metaOf(m)));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/dept-docs\/([^/]+)\/file$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB, "write");
      if (w instanceof Response) return w;
      const vs = store().docs.get(ctx.params[0]!);
      if (!vs || !shown(w, vs.at(-1)!)) return notFound();
      const mp = parseMultipart(ctx);
      const cur = vs.at(-1)!;
      if (Number(mp?.base_version?.text) !== cur.version) return problem(409, "extension.dept_doc_version_conflict", "Văn bản đã có người cập nhật — tải lại bản mới");
      const file = fileOf(mp?.file, cur.file.version + 1);
      if (file instanceof Response) return file;
      return json(push(w, ctx.params[0]!, "file_replaced", { file }));
    },
  },
  ...(["hide", "restore"] as const).map((act): Route => ({
    method: "POST",
    pattern: new RegExp(`^/v1/extension/dept-docs/([^/]+)/${act}$`),
    handler: (ctx) => {
      const w = guard(ctx, LIB, "manage");
      if (w instanceof Response) return w;
      const vs = store().docs.get(ctx.params[0]!);
      if (!vs || !shown(w, vs.at(-1)!)) return notFound();
      const b = (ctx.body ?? {}) as S["DeptDocHide"];
      const cur = vs.at(-1)!;
      if (b.base_version !== cur.version) return problem(409, "extension.dept_doc_version_conflict", "Văn bản đã có người cập nhật — tải lại bản mới");
      if ((b.reason ?? "").trim().length < 5) return problem(422, "extension.dept_doc_invalid", "Văn bản không hợp lệ", { errors: [{ field: "reason", reason: "bắt buộc, 5–1000 ký tự" }] });
      if (cur.hidden === (act === "hide")) return problem(409, "extension.dept_doc_state", "Văn bản đã ở trạng thái này");
      return json(push(w, ctx.params[0]!, act === "hide" ? "hidden" : "restored", { hidden: act === "hide", status_reason: b.reason.trim() }));
    },
  })),
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dept-docs\/([^/]+)\/content$/,
    handler: (ctx) => {
      const w = guard(ctx, LIB);
      if (w instanceof Response) return w;
      const vs = store().docs.get(ctx.params[0]!);
      if (!vs || !shown(w, vs.at(-1)!)) return notFound();
      const ver = Number(ctx.url.searchParams.get("version") ?? 0);
      const d = ver ? vs.find((v) => v.version === ver) : vs.at(-1);
      if (!d) return notFound();
      const bytes = store().files.get(d.file.sha256) ?? pdf(d.title);
      const download = ctx.url.searchParams.get("download") === "1";
      return new Response(bytes as BodyInit, {
        status: 200,
        headers: { "Content-Type": d.file.content_type, "Cache-Control": "no-store",
          "Content-Disposition": `${download ? "attachment" : "inline"}; filename*=UTF-8''${encodeURIComponent(d.file.name)}` },
      });
    },
  },
];
