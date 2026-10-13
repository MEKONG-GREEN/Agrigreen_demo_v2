// ============================================================================
// BACKEND GIẢ LẬP — Kho tài liệu nhân sự (W11.24, dom/hrm, BRD HCTH v3.0 FN-14). Đường dẫn, tham số, hình dạng phản hồi theo
// ĐÚNG contracts/openapi/application.yaml (/v1/hrm/docs*). Quy tắc mô phỏng (rút gọn từ internal/platform/hrm/app/docs.go,
// domain/docs.go): quyền = ma trận FN-20 (docs.view, docs.manage) của kn-hrm-staff ghi đè theo thư mục (kế thừa, người dùng
// ưu tiên, Admin cố định); gốc chỉ Admin; xóa = ẩn có lý do; xem trước/tải xuống ghi nhật ký. Tệp giữ trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { hrmCaller, hrmStaffBrief, type Caller } from "./kn-hrm-staff";

type S = components["schemas"];
type Scope = S["HrmScope"];
type DocPerms = S["HrmDocPerms"];
type DocType = S["HrmDocType"];

interface Folder {
  folder_id: string;
  parent_id: string | null;
  name: string;
  system_key: string | null;
  doc_type: string | null;
  sort_order: number;
  status: "active" | "hidden";
  acl: Record<string, DocPerms>;
  history: S["HrmFolderVersion"][];
}
interface Doc extends Omit<
  S["HrmDoc"],
  "folder_path" | "staff_codes" | "can_manage"
> {
  history: S["HrmDocDetail"]["history"];
}
interface Store {
  folders: Folder[];
  docs: Doc[];
  access: {
    doc_id: string;
    action: "preview" | "download";
    scope: "self" | "unit" | "all";
    actor_ref: string;
    recorded_at: string;
  }[];
  files: Map<string, { bytes: Uint8Array; type: string }>;
}

const ADMIN_REF = "01900000-0000-7000-8000-00000000a001";
const DOC_TYPES: DocType[] = [
  "nang_bac",
  "dieu_dong",
  "bo_nhiem",
  "khen_thuong",
  "ky_luat",
  "danh_gia",
  "khac",
];
const EXT: Record<string, { ct: string; magic: number[] }> = {
  ".pdf": { ct: "application/pdf", magic: [0x25, 0x50, 0x44, 0x46, 0x2d] },
  ".jpg": { ct: "image/jpeg", magic: [0xff, 0xd8, 0xff] },
  ".jpeg": { ct: "image/jpeg", magic: [0xff, 0xd8, 0xff] },
  ".png": { ct: "image/png", magic: [0x89, 0x50, 0x4e, 0x47] },
  ".docx": {
    ct: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    magic: [0x50, 0x4b, 3, 4],
  },
  ".xlsx": {
    ct: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    magic: [0x50, 0x4b, 3, 4],
  },
  ".doc": { ct: "application/msword", magic: [0xd0, 0xcf, 0x11, 0xe0] },
  ".xls": { ct: "application/vnd.ms-excel", magic: [0xd0, 0xcf, 0x11, 0xe0] },
};
const MAX_BYTES = 15 * 1024 * 1024 - 1024;
const DEFAULTS: [string, string, DocType][] = [
  ["nang_bac", "Quyết định nâng bậc", "nang_bac"],
  ["dieu_dong", "Quyết định điều động", "dieu_dong"],
  ["bo_nhiem", "Quyết định bổ nhiệm", "bo_nhiem"],
  ["khen_thuong", "Quyết định khen thưởng", "khen_thuong"],
  ["ky_luat", "Quyết định kỷ luật", "ky_luat"],
  ["danh_gia", "Hồ sơ đánh giá", "danh_gia"],
  ["khac", "Tài liệu nhân sự khác", "khac"],
];
const now = () => new Date().toISOString();
const uuid = () => crypto.randomUUID();
const enc = new TextEncoder();
const PDF = enc.encode(
  "%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer\n%%EOF\n",
);

function newFolder(
  name: string,
  parent: string | null,
  key: string | null,
  docType: string | null,
  sort: number,
  actor: string,
): Folder {
  return {
    folder_id: uuid(),
    parent_id: parent,
    name,
    system_key: key,
    doc_type: docType,
    sort_order: sort,
    status: "active",
    acl: {},
    history: [
      {
        version: 1,
        change: "created",
        name,
        status: "active",
        note: null,
        acl: {},
        actor_ref: actor,
        recorded_at: now(),
      },
    ],
  };
}

let STORE: Store | null = null;
function store(): Store {
  if (STORE) return STORE;
  const s: Store = { folders: [], docs: [], access: [], files: new Map() };
  DEFAULTS.forEach(([k, n, t], i) =>
    s.folders.push(newFolder(n, null, k, t, (i + 1) * 10, ADMIN_REF)),
  );
  const staff = hrmStaffBrief();
  const add = (
    folderKey: string,
    title: string,
    type: DocType,
    unit: string | null,
    staffIds: string[],
    no: string | null,
  ) => {
    const f = s.folders.find((x) => x.system_key === folderKey)!;
    const ref = uuid();
    s.files.set(ref, { bytes: PDF, type: "application/pdf" });
    const at = now();
    s.docs.push({
      doc_id: uuid(),
      folder_id: f.folder_id,
      title,
      doc_type: type,
      year: 2026,
      unit_code: unit,
      staff_ids: staffIds,
      doc_no: no,
      issued_on: "2026-07-15",
      note: null,
      file_name: `${title}.pdf`,
      content_type: "application/pdf",
      size_bytes: PDF.length,
      status: "active",
      version: 1,
      created_at: at,
      uploaded_by: ADMIN_REF,
      updated_at: at,
      history: [
        {
          version: 1,
          change: "created",
          changed_fields: [],
          status: "active",
          reason: null,
          actor_ref: ADMIN_REF,
          recorded_at: at,
        },
      ],
      file_ref: ref,
    } as Doc & { file_ref: string });
  };
  const s0 = staff[0];
  add(
    "nang_bac",
    "QĐ nâng bậc lương tháng 7-2026",
    "nang_bac",
    s0?.unit || "trong_trot",
    s0 ? [s0.id] : [],
    "123/QĐ-TTKN",
  );
  add(
    "khac",
    "Biên bản họp giao ban phòng Chăn nuôi",
    "khac",
    "chan_nuoi",
    [],
    null,
  );
  STORE = s;
  return s;
}

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetHrmDocs(): void {
  STORE = null;
}

// --- Quyền. ---

const chain = (id: string): Folder[] => {
  const out: Folder[] = [];
  let cur = store().folders.find((f) => f.folder_id === id);
  while (cur && out.length < 8) {
    out.unshift(cur);
    cur = cur.parent_id
      ? store().folders.find((f) => f.folder_id === cur!.parent_id)
      : undefined;
  }
  return out;
};
const hiddenChain = (id: string) =>
  chain(id).some((f) => f.status === "hidden");

function folderPerm(
  c: Caller,
  id: string,
): Record<"docs.view" | "docs.manage", Scope> {
  const m = {
    "docs.view": c.m["docs.view"] ?? "none",
    "docs.manage": c.m["docs.manage"] ?? "none",
  } as Record<"docs.view" | "docs.manage", Scope>;
  if (c.group === "admin") return m;
  for (const f of chain(id))
    for (const k of [`role:${c.group}`, `user:${c.ref}`])
      Object.assign(m, f.acl[k] ?? {});
  return m;
}
const sees = (m: Record<string, Scope>) =>
  m["docs.view"] !== "none" || m["docs.manage"] !== "none";
const manageAll = (c: Caller, m: Record<string, Scope>) =>
  !c.readOnly && (c.group === "admin" || m["docs.manage"] === "all");
function onDoc(c: Caller, s: Scope, d: Doc): boolean {
  const mine = new Set(
    hrmStaffBrief()
      .filter((x) => x.account_ref === c.ref)
      .map((x) => x.id),
  );
  const self = d.staff_ids.some((id) => mine.has(id));
  const unit = !!c.unit && d.unit_code === c.unit;
  return (
    s === "all" || (s === "unit" && (unit || self)) || (s === "self" && self)
  );
}
function visibleDoc(c: Caller, d: Doc): boolean {
  const m = folderPerm(c, d.folder_id);
  const manage = onDoc(c, m["docs.manage"], d);
  if (!(onDoc(c, m["docs.view"], d) || manage)) return false;
  return (d.status === "active" && !hiddenChain(d.folder_id)) || manage;
}

function docOut(c: Caller, d: Doc): S["HrmDoc"] {
  const codes = new Map(hrmStaffBrief().map((x) => [x.id, x.code]));
  const { history: _h, ...rest } = d;
  const { file_ref: _f, ...pub } = rest as typeof rest & { file_ref?: string };
  void _h;
  void _f;
  return {
    ...pub,
    folder_path: chain(d.folder_id).map((f) => f.name),
    staff_codes: d.staff_ids.map((id) => codes.get(id) ?? ""),
    can_manage:
      !c.readOnly && onDoc(c, folderPerm(c, d.folder_id)["docs.manage"], d),
  };
}
const findDoc = (id: string | undefined) =>
  store().docs.find((d) => d.doc_id === id);
const notFound = () =>
  problem(404, "hrm.doc_not_found", "Không có tài liệu này");
const folderNotFound = () =>
  problem(404, "hrm.folder_not_found", "Không có thư mục tài liệu này");
const forbid = (msg: string) => problem(403, "forbidden", msg);

function reasonOf(body: unknown): string | Response {
  const r = String((body as { reason?: string })?.reason ?? "").trim();
  return r.length < 10 || r.length > 500
    ? problem(400, "validation_failed", "Validation failed", {
        errors: [{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }],
      })
    : r;
}

function sibling(
  parent: string | null,
  name: string,
  self: string | null,
): boolean {
  return store().folders.some(
    (f) =>
      f.parent_id === parent &&
      f.folder_id !== self &&
      f.status === "active" &&
      f.name.toLowerCase() === name.toLowerCase(),
  );
}

/** Thư mục để ghi: gốc — Admin; còn lại docs.manage toàn Trung tâm. */
function folderWrite(c: Caller, id: string): Folder | Response {
  const f = store().folders.find((x) => x.folder_id === id);
  if (!f) return folderNotFound();
  const m = folderPerm(c, id);
  if (
    f.parent_id === null ? c.group !== "admin" || c.readOnly : !manageAll(c, m)
  )
    return sees(m)
      ? forbid("kho tài liệu: không có quyền quản lý thư mục này")
      : folderNotFound();
  return f;
}
function bumpFolder(
  c: Caller,
  f: Folder,
  change: S["HrmFolderVersion"]["change"],
  note: string | null,
) {
  f.history.push({
    version: f.history.length + 1,
    change,
    name: f.name,
    status: f.status,
    note,
    acl: { ...f.acl },
    actor_ref: c.ref,
    recorded_at: now(),
  });
}

// --- Multipart (thân nhị phân của mock handler). ---

function parseMultipart(
  ctx: Ctx,
): { meta: string; file?: { name: string; bytes: Uint8Array } } | null {
  const ct = ctx.request.headers.get("Content-Type") ?? "";
  const b = /boundary=([^;]+)/.exec(ct)?.[1]?.replace(/^"|"$/g, "");
  if (!b || !(ctx.body instanceof Uint8Array)) return null;
  let raw = "";
  for (const x of ctx.body) raw += String.fromCharCode(x);
  const out: { meta: string; file?: { name: string; bytes: Uint8Array } } = {
    meta: "",
  };
  const dec = new TextDecoder();
  const bytesOf = (s: string) => Uint8Array.from(s, (ch) => ch.charCodeAt(0));
  for (const part of raw.split(`--${b}`)) {
    const i = part.indexOf("\r\n\r\n");
    if (i < 0) continue;
    const head = part.slice(0, i);
    const content = part.slice(i + 4).replace(/\r\n$/, "");
    const name = /name="([^"]*)"/.exec(head)?.[1];
    const filename = /filename="([^"]*)"/.exec(head)?.[1];
    if (name === "meta") out.meta = dec.decode(bytesOf(content));
    if (name === "file")
      out.file = {
        name: dec.decode(bytesOf(filename ?? "tep")),
        bytes: bytesOf(content),
      };
  }
  return out;
}

function checkInput(c: Caller, b: S["HrmDocInput"]): Response | null {
  const errs: { field: string; reason: string }[] = [];
  if (!b.title?.trim()) errs.push({ field: "title", reason: "bắt buộc" });
  if (!DOC_TYPES.includes(b.doc_type))
    errs.push({ field: "doc_type", reason: "loại tài liệu không có" });
  if (b.year != null && (b.year < 1975 || b.year > 2100))
    errs.push({ field: "year", reason: "năm 1975–2100" });
  const ids = new Set(hrmStaffBrief().map((x) => x.id));
  (b.staff_ids ?? []).forEach(
    (id, i) =>
      !ids.has(id) &&
      errs.push({
        field: "staff_ids",
        reason: `không có hồ sơ nhân sự thứ ${i + 1}`,
      }),
  );
  void c;
  return errs.length ? validation(errs) : null;
}

function applyInput(d: Doc, b: S["HrmDocInput"]): string[] {
  const next = {
    title: b.title.trim(),
    doc_type: b.doc_type,
    year: b.year ?? (b.issued_on ? Number(b.issued_on.slice(0, 4)) : null),
    unit_code:
      b.unit_code ||
      (b.staff_ids?.length
        ? (hrmStaffBrief().find((x) => x.id === b.staff_ids![0])?.unit ?? null)
        : null) ||
      null,
    staff_ids: b.staff_ids ?? [],
    doc_no: b.doc_no || null,
    issued_on: b.issued_on || null,
    note: b.note || null,
  };
  const changed = (Object.keys(next) as (keyof typeof next)[]).filter(
    (k) => JSON.stringify(d[k]) !== JSON.stringify(next[k]),
  );
  Object.assign(d, next);
  return changed.map((k) => (k === "year" ? "year" : k));
}

export const hrmDocRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/docs\/folders$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const withHidden = ctx.url.searchParams.get("include_hidden") === "1";
      const keep = new Set<string>();
      for (const f of store().folders) {
        const m = folderPerm(c, f.folder_id);
        if (
          !sees(m) ||
          (hiddenChain(f.folder_id) && (!withHidden || !manageAll(c, m)))
        )
          continue;
        chain(f.folder_id).forEach((x) => keep.add(x.folder_id));
      }
      const items: S["HrmFolder"][] = store()
        .folders.filter((f) => keep.has(f.folder_id))
        .map((f) => {
          const m = folderPerm(c, f.folder_id);
          const admin =
            f.parent_id === null
              ? c.group === "admin" && !c.readOnly
              : manageAll(c, m);
          return {
            folder_id: f.folder_id,
            parent_id: f.parent_id,
            name: f.name,
            path: chain(f.folder_id).map((x) => x.name),
            system_key: f.system_key,
            doc_type: f.doc_type,
            sort_order: f.sort_order,
            status: f.status,
            hidden: hiddenChain(f.folder_id),
            docs: store().docs.filter(
              (d) =>
                d.folder_id === f.folder_id &&
                d.status === "active" &&
                onDoc(c, m["docs.view"], d),
            ).length,
            can_view: m["docs.view"] !== "none",
            can_manage: !c.readOnly && m["docs.manage"] !== "none",
            can_admin: admin,
            acl: admin ? f.acl : undefined,
            version: f.history.length,
            recorded_at: f.history[f.history.length - 1]!.recorded_at,
          };
        })
        .sort(
          (a, b) =>
            a.path.join("\u0000").localeCompare(b.path.join("\u0000")) ||
            a.sort_order - b.sort_order,
        );
      return json({
        records_enabled: true,
        ready: true,
        group: c.group,
        unit: c.unit,
        perms: {
          "docs.view": c.m["docs.view"] ?? "none",
          "docs.manage": c.m["docs.manage"] ?? "none",
        },
        can_create_root: c.group === "admin" && !c.readOnly,
        doc_types: DOC_TYPES,
        extensions: Object.keys(EXT),
        max_bytes: MAX_BYTES,
        items,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs\/folders$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const b = (ctx.body ?? {}) as {
        parent_id?: string | null;
        name?: string;
        doc_type?: string;
      };
      const name = String(b.name ?? "")
        .trim()
        .replace(/\s+/g, " ");
      if (!name || name.length > 120 || /[/\\]/.test(name))
        return validation([
          { field: "name", reason: "tên bắt buộc, tối đa 120 ký tự" },
        ]);
      const parent = b.parent_id ?? null;
      if (parent === null) {
        if (c.group !== "admin" || c.readOnly)
          return forbid("thư mục gốc do Admin tạo (FN-14)");
      } else {
        const p = folderWrite(c, parent);
        if (p instanceof Response) return p;
      }
      if (sibling(parent, name, null))
        return problem(
          409,
          "hrm.folder_exists",
          "Đã có thư mục cùng tên ở cùng cấp",
        );
      const f = newFolder(name, parent, null, b.doc_type ?? null, 0, c.ref);
      store().folders.push(f);
      return json({ folder_id: f.folder_id, version: 1 }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs\/folders\/defaults$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      if (c.group !== "admin" || c.readOnly)
        return forbid("thư mục gốc do Admin tạo (FN-14)");
      let n = 0;
      DEFAULTS.forEach(([k, name, t], i) => {
        if (
          store().folders.some((f) => f.system_key === k) ||
          sibling(null, name, null)
        )
          return;
        store().folders.push(newFolder(name, null, k, t, (i + 1) * 10, c.ref));
        n++;
      });
      return json({ created: n }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/docs\/folders\/([\w-]+)\/history$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const f = store().folders.find((x) => x.folder_id === ctx.params[0]);
      if (!f) return folderNotFound();
      const m = folderPerm(c, f.folder_id);
      if (c.group !== "admin" && m["docs.manage"] !== "all")
        return sees(m)
          ? forbid("cần docs.manage toàn Trung tâm")
          : folderNotFound();
      return json({ items: f.history });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs\/folders\/([\w-]+)\/rename$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const f = folderWrite(c, ctx.params[0]!);
      if (f instanceof Response) return f;
      const name = String((ctx.body as { name?: string })?.name ?? "")
        .trim()
        .replace(/\s+/g, " ");
      if (!name || name.length > 120 || /[/\\]/.test(name))
        return validation([
          { field: "name", reason: "tên bắt buộc, tối đa 120 ký tự" },
        ]);
      if (name !== f.name) {
        if (sibling(f.parent_id, name, f.folder_id))
          return problem(
            409,
            "hrm.folder_exists",
            "Đã có thư mục cùng tên ở cùng cấp",
          );
        f.name = name;
        bumpFolder(c, f, "renamed", null);
      }
      return json({ folder_id: f.folder_id, version: f.history.length });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs\/folders\/([\w-]+)\/acl$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const f = folderWrite(c, ctx.params[0]!);
      if (f instanceof Response) return f;
      const b = (ctx.body ?? {}) as {
        acl?: Record<string, Record<string, string>>;
        note?: string;
      };
      const acl: Record<string, DocPerms> = {};
      for (const [k, perms] of Object.entries(b.acl ?? {})) {
        const ok =
          /^role:(hcth|manager|tinh|xa|tw)$/.test(k) ||
          /^user:[0-9a-f-]{36}$/.test(k);
        const pv = Object.entries(perms ?? {}).every(
          ([p, s]) =>
            ["docs.view", "docs.manage"].includes(p) &&
            ["none", "self", "unit", "all"].includes(s),
        );
        if (!ok || !pv)
          return problem(
            400,
            "validation_failed",
            `quyền thư mục: khóa ${k} không hợp lệ`,
          );
        acl[k] = perms as DocPerms;
      }
      f.acl = acl;
      bumpFolder(c, f, "acl", b.note?.trim() || null);
      return json({ folder_id: f.folder_id, version: f.history.length });
    },
  },
  ...(["hide", "restore"] as const).map((op): Route => ({
    method: "POST",
    pattern: new RegExp(`^\\/v1\\/hrm\\/docs\\/folders\\/([\\w-]+)\\/${op}$`),
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const r = reasonOf(ctx.body);
      if (r instanceof Response) return r;
      const f = folderWrite(c, ctx.params[0]!);
      if (f instanceof Response) return f;
      const want = op === "hide" ? "hidden" : "active";
      if (f.status !== want) {
        if (op === "hide") {
          const busy =
            store().folders.some(
              (x) => x.parent_id === f.folder_id && x.status === "active",
            ) ||
            store().docs.some(
              (d) => d.folder_id === f.folder_id && d.status === "active",
            );
          if (busy)
            return problem(
              409,
              "hrm.folder_not_empty",
              "Thư mục còn thư mục con hoặc tài liệu đang hiện",
            );
        }
        f.status = want;
        bumpFolder(c, f, op === "hide" ? "hidden" : "restored", r);
      }
      return json({ folder_id: f.folder_id, version: f.history.length });
    },
  })),
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/docs$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const p = ctx.url.searchParams;
      const folder = p.get("folder");
      const rec = p.get("recursive") === "1";
      const q = (p.get("q") ?? "").trim().toLowerCase();
      const items = store()
        .docs.filter((d) => {
          if (
            folder &&
            d.folder_id !== folder &&
            !(rec && chain(d.folder_id).some((f) => f.folder_id === folder))
          )
            return false;
          if (p.get("doc_type") && d.doc_type !== p.get("doc_type"))
            return false;
          if (p.get("year") && String(d.year) !== p.get("year")) return false;
          if (p.get("unit") && d.unit_code !== p.get("unit")) return false;
          if (p.get("staff") && !d.staff_ids.includes(p.get("staff")!))
            return false;
          if (
            q &&
            !`${d.title}\u0000${d.doc_no ?? ""}\u0000${d.file_name}`
              .toLowerCase()
              .includes(q)
          )
            return false;
          if (d.status === "hidden" && p.get("include_hidden") !== "1")
            return false;
          return visibleDoc(c, d);
        })
        .sort((a, b) => b.created_at.localeCompare(a.created_at));
      const offset = Math.max(Number(p.get("offset") ?? 0) || 0, 0);
      const limit = Math.min(
        Math.max(Number(p.get("limit") ?? 50) || 50, 1),
        200,
      );
      return json({
        total: items.length,
        items: items.slice(offset, offset + limit).map((d) => docOut(c, d)),
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const mp = parseMultipart(ctx);
      if (!mp?.file) return validation([{ field: "file", reason: "bắt buộc" }]);
      let b: S["HrmDocInput"];
      try {
        b = JSON.parse(mp.meta) as S["HrmDocInput"];
      } catch {
        return validation([
          { field: "meta", reason: "JSON phân loại không hợp lệ" },
        ]);
      }
      if (mp.file.bytes.length > MAX_BYTES)
        return problem(
          413,
          "hrm.doc_too_large",
          "Tệp vượt dung lượng cho phép",
        );
      const ext = /\.[^.]+$/.exec(mp.file.name.toLowerCase())?.[0] ?? "";
      const fmt = EXT[ext];
      if (!fmt || !fmt.magic.every((x, i) => mp.file!.bytes[i] === x))
        return problem(422, "hrm.doc_format", "Định dạng tệp không được phép");
      const f = store().folders.find((x) => x.folder_id === b.folder_id);
      if (!f || hiddenChain(f.folder_id)) return folderNotFound();
      const bad = checkInput(c, b);
      if (bad) return bad;
      const at = now();
      const d = {
        doc_id: uuid(),
        folder_id: f.folder_id,
        file_name: mp.file.name,
        content_type: fmt.ct,
        size_bytes: mp.file.bytes.length,
        status: "active",
        version: 1,
        created_at: at,
        uploaded_by: c.ref,
        updated_at: at,
        history: [
          {
            version: 1,
            change: "created",
            changed_fields: [],
            status: "active",
            reason: null,
            actor_ref: c.ref,
            recorded_at: at,
          },
        ],
        file_ref: uuid(),
      } as unknown as Doc & { file_ref: string };
      applyInput(d, b);
      const m = folderPerm(c, f.folder_id);
      if (c.readOnly || m["docs.manage"] === "none")
        return sees(m)
          ? forbid("kho tài liệu: không có quyền quản lý thư mục này")
          : folderNotFound();
      if (!onDoc(c, m["docs.manage"], d))
        return forbid("kho tài liệu: tài liệu ngoài phạm vi quản lý");
      store().files.set(d.file_ref, { bytes: mp.file.bytes, type: fmt.ct });
      store().docs.push(d);
      return json({ doc_id: d.doc_id, version: 1 }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/docs\/([\w-]+)$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const d = findDoc(ctx.params[0]);
      if (!d || !visibleDoc(c, d)) return notFound();
      const manage = onDoc(c, folderPerm(c, d.folder_id)["docs.manage"], d);
      return json({
        doc: docOut(c, d),
        history: d.history,
        access: manage
          ? store()
              .access.filter((a) => a.doc_id === d.doc_id)
              .map(({ doc_id: _d, ...a }) => (void _d, a))
          : null,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/docs\/([\w-]+)\/classify$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const d = findDoc(ctx.params[0]);
      if (!d || !visibleDoc(c, d)) return notFound();
      if (c.readOnly || !onDoc(c, folderPerm(c, d.folder_id)["docs.manage"], d))
        return forbid("kho tài liệu: không có quyền quản lý tài liệu này");
      if (d.status !== "active")
        return problem(
          409,
          "conflict",
          "tài liệu đã ẩn — hiện lại trước khi sửa",
        );
      const b = (ctx.body ?? {}) as S["HrmDocInput"];
      const bad = checkInput(c, b);
      if (bad) return bad;
      const fields = applyInput(d, b);
      if (fields.length) {
        d.version += 1;
        d.updated_at = now();
        d.history.push({
          version: d.version,
          change: "updated",
          changed_fields: fields,
          status: "active",
          reason: null,
          actor_ref: c.ref,
          recorded_at: d.updated_at,
        });
      }
      return json({ doc_id: d.doc_id, version: d.version });
    },
  },
  ...(["hide", "restore"] as const).map((op): Route => ({
    method: "POST",
    pattern: new RegExp(`^\\/v1\\/hrm\\/docs\\/([\\w-]+)\\/${op}$`),
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const r = reasonOf(ctx.body);
      if (r instanceof Response) return r;
      const d = findDoc(ctx.params[0]);
      if (!d || !visibleDoc(c, d)) return notFound();
      if (c.readOnly || !onDoc(c, folderPerm(c, d.folder_id)["docs.manage"], d))
        return forbid("kho tài liệu: không có quyền quản lý tài liệu này");
      const want = op === "hide" ? "hidden" : "active";
      if (d.status !== want) {
        d.status = want;
        d.version += 1;
        d.updated_at = now();
        d.history.push({
          version: d.version,
          change: op === "hide" ? "hidden" : "restored",
          changed_fields: [],
          status: want,
          reason: r,
          actor_ref: c.ref,
          recorded_at: d.updated_at,
        });
      }
      return json({ doc_id: d.doc_id, version: d.version });
    },
  })),
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/docs\/([\w-]+)\/content$/,
    handler: (ctx) => {
      const c = hrmCaller(ctx);
      if (c instanceof Response) return c;
      const d = findDoc(ctx.params[0]) as
        (Doc & { file_ref: string }) | undefined;
      if (!d || !visibleDoc(c, d)) return notFound();
      const m = folderPerm(c, d.folder_id);
      const scope = (
        onDoc(c, m["docs.view"], d) ? m["docs.view"] : m["docs.manage"]
      ) as "self" | "unit" | "all";
      const download = ctx.url.searchParams.get("download") === "1";
      store().access.unshift({
        doc_id: d.doc_id,
        action: download ? "download" : "preview",
        scope,
        actor_ref: c.ref,
        recorded_at: now(),
      });
      const f = store().files.get(d.file_ref)!;
      return new Response(f.bytes as BodyInit, {
        status: 200,
        headers: {
          "Content-Type": f.type,
          "Cache-Control": "no-store",
          "Content-Disposition": `${download ? "attachment" : "inline"}; filename*=UTF-8''${encodeURIComponent(d.file_name)}`,
        },
      });
    },
  },
];
