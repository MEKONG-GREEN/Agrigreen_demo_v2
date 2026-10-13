// ============================================================================
// BACKEND GIẢ LẬP — Contact (W11.23, pf/contact `cap/directory`, BRD HCTH v3.0 FN-13). Đường dẫn, tham số, hình dạng phản
// hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/contacts/*). Quy tắc mô phỏng (rút gọn từ
// internal/platform/contact/app):
//   - Bộ trường theo vai của ngữ cảnh phiên: Cấp TW, Cấp Xã → "basic" (CHỈ id, họ tên, năm sinh, SĐT; không chi tiết, không
//     ghi, bộ lọc trường ẩn bị bỏ qua); Cấp Tỉnh, Admin → "full". Cập nhật: Admin, Cấp Tỉnh ở phòng HCTH (không chỉ xem).
//   - Cán bộ thuộc tỉnh chỉ đọc (sửa tại hồ sơ FN-07); liên hệ ngoài tỉnh có lịch sử tên trường đã đổi; trùng SĐT → 409.
//   - Dữ liệu cá nhân ở đây là TỔNG HỢP (tên giả), trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Level = S["ContactLevel"];

interface Person {
  id: string;
  source: "staff" | "external";
  full_name: string;
  birth_year: string;
  phone: string;
  email: string;
  title: string;
  unit: string;
  unit_code?: string;
  organization: string;
  level: Level;
  hidden: boolean;
  history: S["ContactHistoryEntry"][];
}
interface Unit {
  id: string;
  name: string;
  level: Level;
  phone: string;
  email: string;
  address: string;
  hidden: boolean;
}

const HCTH = "hanh_chinh_tong_hop";
const ACTOR = "01900000-0000-7000-8000-00000000a001";
const LEVEL_LABEL: Record<Level, string> = { tw: "Trung ương", tinh: "Tỉnh", xa: "Xã", khac: "Đơn vị khác" };
const BASIC = ["full_name", "birth_year", "phone"];
const FULL = [...BASIC, "title", "unit", "level", "email"];

let seq = 0;
const nid = () => `01930000-0000-7000-8000-${String(++seq).padStart(12, "0")}`;
const at = () => new Date().toISOString();

function seedPersons(): Person[] {
  const staff = (full_name: string, birth_year: string, phone: string, email: string, title: string, unit: string, level: Level): Person => ({
    id: nid(),
    source: "staff",
    full_name,
    birth_year,
    phone,
    email,
    title,
    unit,
    unit_code: "",
    organization: "",
    level,
    hidden: false,
    history: [],
  });
  const ext: Person = {
    id: nid(),
    source: "external",
    full_name: "Hoàng Văn Trung",
    birth_year: "1975",
    phone: "0243000999",
    email: "trung@khuyennong.gov.vn",
    title: "Phó Giám đốc",
    unit: "Trung tâm Khuyến nông Quốc gia",
    organization: "Trung tâm Khuyến nông Quốc gia",
    level: "tw",
    hidden: false,
    history: [{ version: 1, change: "created", changed_fields: [], at: at(), actor_ref: ACTOR }],
  };
  return [
    staff("Nguyễn Thị Mai", "1990", "0912000111", "mai.nt@ttkn.angiang.gov.vn", "Trưởng phòng", "Phòng Trồng trọt", "tinh"),
    staff("Trần Văn Hùng", "1984", "0912000112", "hung.tv@ttkn.angiang.gov.vn", "Viên chức", "Phòng Hành chính - Tổng hợp", "tinh"),
    staff("Lê Văn Bình", "1985", "0912000222", "", "Cán bộ khuyến nông xã", "Xã Mỹ Hòa Hưng · Phòng Trồng trọt", "xa"),
    ext,
  ];
}

function seedUnits(): Unit[] {
  return [
    { id: nid(), name: "Trung tâm Khuyến nông Quốc gia", level: "tw", phone: "02438694444", email: "vp@khuyennong.gov.vn", address: "Hà Nội", hidden: false },
    { id: nid(), name: "Trung tâm Khuyến nông tỉnh An Giang", level: "tinh", phone: "02963852111", email: "ttkn@angiang.gov.vn", address: "Long Xuyên", hidden: false },
  ];
}

let persons = seedPersons();
let units = seedUnits();

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetContact() {
  seq = 0;
  persons = seedPersons();
  units = seedUnits();
}

interface Caller {
  full: boolean;
  manage: boolean;
}

function caller(ctx: Ctx): Caller | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc) return problem(403, "forbidden", "Contact: cần ngữ cảnh phiên Khuyến nông");
  const full = kc.role === "admin" || kc.role === "kn_tinh";
  const manage = kc.role === "admin" || (kc.role === "kn_tinh" && kc.department_code === HCTH && !kc.read_only);
  return { full, manage };
}

const fold = (s: string) =>
  s
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/đ/g, "d");

function match(q: string, p: Person, full: boolean): boolean {
  q = q.trim();
  if (!q) return true;
  const d = q.replace(/[\s.+-]/g, "");
  if (/^\d{3,}$/.test(d)) return p.phone.includes(d);
  const f = fold(q);
  return [p.full_name, ...(full ? [p.unit, p.title, p.email] : [])].some((x) => fold(x).includes(f));
}

function view(p: Person, full: boolean) {
  if (!full) return { id: p.id, full_name: p.full_name, birth_year: p.birth_year, phone: p.phone };
  return {
    id: p.id,
    source: p.source,
    full_name: p.full_name,
    birth_year: p.birth_year,
    phone: p.phone,
    title: p.title,
    unit: p.unit,
    unit_code: p.unit_code ?? "",
    level: p.level,
    level_label: LEVEL_LABEL[p.level],
    email: p.email,
    hidden: p.hidden,
    erased: false,
  };
}

function detail(p: Person, c: Caller) {
  return { ...view(p, true), organization: p.organization, can_edit: c.manage && p.source === "external", history: p.history };
}

const NO_STORE = { "Cache-Control": "no-store" };

function checkPerson(b: Record<string, unknown>, create: boolean): Response | null {
  const errs: { field: string; reason: string }[] = [];
  const s = (k: string) => (typeof b[k] === "string" ? (b[k] as string).trim() : undefined);
  if ((create || "full_name" in b) && !s("full_name")) errs.push({ field: "full_name", reason: "bắt buộc" });
  if ((create || "phone" in b) && !/^0\d{9,10}$/.test((s("phone") ?? "").replace(/\s/g, ""))) errs.push({ field: "phone", reason: "SĐT Việt Nam" });
  if (s("birth_year") && !/^(19|20)\d{2}$/.test(s("birth_year")!)) errs.push({ field: "birth_year", reason: "4 chữ số" });
  if (s("email") && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(s("email")!)) errs.push({ field: "email", reason: "email không hợp lệ" });
  const lv = s("level") ?? "";
  if ((create || "level" in b) && !(lv in LEVEL_LABEL)) errs.push({ field: "level", reason: "tw, tinh, xa, khac" });
  return errs.length ? validation(errs) : null;
}

const FIELDS = ["full_name", "phone", "birth_year", "email", "level", "organization", "title"] as const;

export const contactRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/contacts\/access$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      return json({ field_set: c.full ? "full" : "basic", fields: c.full ? FULL : BASIC, can_manage: c.manage, can_detail: c.full, staff_source: "enabled" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/contacts\/persons$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const u = ctx.url.searchParams;
      const level = c.full ? u.get("level") : null;
      const unit = c.full ? fold(u.get("unit") ?? "") : "";
      const title = c.full ? fold(u.get("title") ?? "") : "";
      const hidden = c.manage && u.get("include_hidden") === "true";
      const rows = persons.filter(
        (p) =>
          (!p.hidden || hidden) &&
          (!level || p.level === level) &&
          (!unit || fold(p.unit).includes(unit)) &&
          (!title || fold(p.title).includes(title)) &&
          match(u.get("q") ?? "", p, c.full),
      );
      const off = Number(u.get("offset") ?? 0);
      const lim = Number(u.get("limit") ?? 50);
      return json(
        {
          field_set: c.full ? "full" : "basic",
          fields: c.full ? FULL : BASIC,
          total: rows.length,
          staff_source: "enabled",
          items: rows.slice(off, off + lim).map((p) => view(p, c.full)),
        },
        200,
        NO_STORE,
      );
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/contacts\/persons$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (!c.manage) return problem(403, "contact.manage_forbidden", "Chỉ Cấp Tỉnh – HCTH và Admin cập nhật liên hệ ngoài tỉnh");
      const b = (ctx.body ?? {}) as Record<string, string>;
      const bad = checkPerson(b, true);
      if (bad) return bad;
      if (persons.some((p) => p.phone === b.phone!.replace(/\s/g, ""))) return problem(409, "contact.duplicate_person", "Số điện thoại đã thuộc một người trong danh bạ");
      const p: Person = {
        id: nid(),
        source: "external",
        full_name: b.full_name!.trim(),
        birth_year: b.birth_year ?? "",
        phone: b.phone!.replace(/\s/g, ""),
        email: b.email ?? "",
        title: b.title ?? "",
        organization: b.organization ?? "",
        unit: b.organization ?? "",
        level: b.level as Level,
        hidden: false,
        history: [{ version: 1, change: "created", changed_fields: [], at: at(), actor_ref: ACTOR }],
      };
      persons.push(p);
      return json(detail(p, c), 201, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/contacts\/persons\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (!c.full) return problem(403, "contact.detail_forbidden", "Vai trò này chỉ xem họ tên, năm sinh, số điện thoại trong danh sách");
      const p = persons.find((x) => x.id === ctx.params[0]);
      if (!p || (p.hidden && !c.manage)) return problem(404, "contact.not_found", "Không có liên hệ này trong danh bạ");
      return json(detail(p, c), 200, NO_STORE);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/contacts\/persons\/([^/]+)\/revise$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (!c.manage) return problem(403, "contact.manage_forbidden", "Chỉ Cấp Tỉnh – HCTH và Admin cập nhật liên hệ ngoài tỉnh");
      const p = persons.find((x) => x.id === ctx.params[0]);
      if (!p) return problem(404, "contact.not_found", "Không có liên hệ này trong danh bạ");
      if (p.source === "staff") return problem(422, "contact.staff_read_only", "Liên hệ của cán bộ thuộc tỉnh đồng bộ từ hồ sơ nhân sự — sửa tại hồ sơ (FN-07)");
      const b = (ctx.body ?? {}) as Record<string, unknown>;
      const bad = checkPerson(b, false);
      if (bad) return bad;
      const changed: string[] = [];
      for (const k of FIELDS) {
        const v = b[k];
        if (typeof v === "string" && v.trim() !== String(p[k] ?? "")) {
          (p as unknown as Record<string, string>)[k] = v.trim();
          changed.push(k);
        }
      }
      if (changed.includes("organization")) p.unit = p.organization;
      if (typeof b.hidden === "boolean" && b.hidden !== p.hidden) {
        p.hidden = b.hidden;
        changed.push("hidden");
      }
      if (changed.length) {
        const change = changed.length === 1 && changed[0] === "hidden" ? (p.hidden ? "hidden" : "shown") : "updated";
        p.history.push({ version: p.history.length + 1, change, changed_fields: changed, at: at(), actor_ref: ACTOR });
      }
      return json(detail(p, c), 200, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/contacts\/units$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const u = ctx.url.searchParams;
      const q = fold(u.get("q") ?? "");
      const hidden = c.manage && u.get("include_hidden") === "true";
      const items = units
        .filter((x) => (!x.hidden || hidden) && (!u.get("level") || x.level === u.get("level")) && (!q || fold(x.name).includes(q) || x.phone.includes(q)))
        .map((x) => {
          const b = { id: x.id, name: x.name, level: x.level, level_label: LEVEL_LABEL[x.level], phone: x.phone };
          return c.full ? { ...b, email: x.email, address: x.address, org_ref: null, hidden: x.hidden } : b;
        });
      return json({ field_set: c.full ? "full" : "basic", items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/contacts\/units(?:\/([^/]+)\/revise)?$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (!c.manage) return problem(403, "contact.manage_forbidden", "Chỉ Cấp Tỉnh – HCTH và Admin cập nhật thẻ Đơn vị");
      const b = (ctx.body ?? {}) as Partial<Unit> & { phone?: string };
      if (!b.name?.trim() || !b.level || !(b.level in LEVEL_LABEL)) return validation([{ field: "name", reason: "bắt buộc" }]);
      const phone = (b.phone ?? "").replace(/[\s.()-]/g, "");
      if (phone && !/^\d{8,11}$/.test(phone)) return validation([{ field: "phone", reason: "số máy cơ quan 8–11 chữ số" }]);
      const id = ctx.params[0];
      let x = id ? units.find((u) => u.id === id) : undefined;
      if (id && !x) return problem(404, "contact.not_found", "Không có đơn vị này");
      if (!x) {
        x = { id: nid(), name: "", level: "tw", phone: "", email: "", address: "", hidden: false };
        units.push(x);
      }
      Object.assign(x, { name: b.name.trim(), level: b.level, phone, email: b.email ?? "", address: b.address ?? "", hidden: b.hidden ?? x.hidden });
      return json(
        { id: x.id, name: x.name, level: x.level, level_label: LEVEL_LABEL[x.level], phone: x.phone, email: x.email, address: x.address, org_ref: null, hidden: x.hidden },
        id ? 200 : 201,
      );
    },
  },
];
