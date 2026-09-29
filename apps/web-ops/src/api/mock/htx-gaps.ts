// Backend giả lập cho các màn hình hoàn thiện App HTX (G2): nhập/sao chép hồ sơ HTX (HTX US-HTX-02/03),
// tìm nông hộ theo SĐT, gọi nhanh, sao chép hàng loạt nông hộ (US-HH-01/02/03), lịch báo cáo của quản lý
// HTX trên Application API (US-DASH-03) và soạn bài viết có xem trước mobile (US-CMS-01).
// Hình dạng phản hồi theo contracts/openapi/application.yaml và admin.yaml.
import { uuidv7 } from "../idempotency";
import type { ContentKind, ReportSchedule } from "../types";
import { COOPERATIVES, findCoop, householdCount, householdRefOf, householdsOf, htxCoop, syntheticPhone, type MockCooperative } from "./data";
import { headerKey, parseCsv } from "./admin-ops";
import { hasRole, inScope, isZip, json, nowIso, opaque, paginate, problem, reasonOf, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

const NOT_YOURS = "Bạn không có quyền truy cập nông hộ này.";
const NAME_TAKEN = "Tên HTX này đã tồn tại, vui lòng chọn tên khác.";
const RECIPIENT_BAD = "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.";

const managerOnly = (p: MockPersona) => hasRole(p, "coop_manager") && !hasRole(p, "kn_officer", "kn_province_admin", "kn_national_admin", "registry_admin", "platform_admin");
const admin = (p: MockPersona, ...roles: string[]) => hasRole(p, "platform_admin", ...roles);

// --------------------------------------------------------------------------
// Nông hộ: SĐT tổng hợp theo chỉ số, hộ sao chép thêm trong phiên
// --------------------------------------------------------------------------
interface CopiedHousehold {
  coop: string;
  ref: string;
  phone: string;
  label: string;
  area: string | null;
}
const COPIED: CopiedHousehold[] = [];

function locate(ref: string): { coop: MockCooperative; index: number } | null {
  for (const coop of COOPERATIVES) {
    const n = householdCount(coop);
    for (let i = 0; i < n; i++) if (householdRefOf(coop, i) === ref) return { coop, index: i };
  }
  return null;
}

function phoneTaken(phone: string): boolean {
  if (COPIED.some((h) => h.phone === phone)) return true;
  return /^09990\d{5}$/.test(phone) && COOPERATIVES.some((c) => Array.from({ length: householdCount(c) }, (_, i) => syntheticPhone(c, i)).includes(phone));
}

const maskedLabel = (name: string) => {
  const parts = name.trim().split(/\s+/);
  return parts.length > 1 ? `${parts[0]} ${parts.slice(1).map((p) => `${p[0]}.`).join(" ")}` : `${parts[0]}.`;
};

function listOf(coop: MockCooperative) {
  const extra = COPIED.filter((h) => h.coop === coop.api.ref).map((h) => ({
    household_ref: h.ref, cooperative_ref: coop.api.ref, commune_code: coop.api.admin_unit_code ?? "", cultivated_area_ha: h.area,
    member_since: nowIso().slice(0, 10), active: true, label: h.label, masked_phone: `09******${h.phone.slice(-2)}`, label_unavailable: false, consents: [],
  }));
  return [...householdsOf(coop), ...extra];
}

const normPhone = (s: string) => {
  const d = s.replace(/[\s.-]/g, "");
  return d.startsWith("+84") ? `0${d.slice(3)}` : d;
};

// --------------------------------------------------------------------------
// Lịch báo cáo của quản lý HTX (Application API)
// --------------------------------------------------------------------------
const COOP_SCHEDULES: ReportSchedule[] = [];
function recipientsOf(p: MockPersona) {
  const coop = p.me.org?.ref ?? "";
  const tail = coop.slice(-4);
  return [
    { actor_ref: `01927b3e-0ff1-7000-8000-00000000${tail}`.slice(0, 36), display_name: "Nguyễn Thị Kế Toán", masked_email: "ke•••@htx-gia-lap.vn" },
    { actor_ref: `01927b3e-0ff1-7000-8001-00000000${tail}`.slice(0, 36), display_name: "Trần Văn Quản Lý", masked_email: "qu•••@htx-gia-lap.vn" },
  ];
}

// --------------------------------------------------------------------------
// Soạn bài viết (pf/content, Admin API)
// --------------------------------------------------------------------------
interface MockVersion {
  id: string;
  version_no: number;
  status: "draft" | "review" | "published" | "superseded" | "discarded" | "returned";
  title: string;
  summary: string;
  body: string;
  body_format: "markdown" | "html";
  category: string | null;
  tags: string[];
  author_ref: string;
  recorded_at: string;
  attachments: { id: string; file_name: string; content_type: "image/jpeg" | "image/png" | "image/webp"; size: number; sha256: string; role: "attachment" | "cover"; url: string | null; url_expires_at: string | null }[];
}
interface MockDoc {
  ref: string;
  kind: ContentKind;
  created_by: string;
  created_at: string;
  versions: MockVersion[];
  uploads: Map<string, MockVersion["attachments"][number]>;
}
const DOCS_ADMIN: MockDoc[] = [];

/** Lọc nội dung như backend (allow-list): bỏ thẻ script/style, thuộc tính on*, liên kết javascript:. */
export function sanitizeMock(body: string): string {
  return body
    .replace(/<\s*(script|style|iframe)[^>]*>[\s\S]*?<\s*\/\s*\1\s*>/gi, "")
    .replace(/\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, "")
    .replace(/\(\s*javascript:[^)]*\)/gi, "(#)");
}

function docView(d: MockDoc) {
  const last = d.versions[d.versions.length - 1]!;
  const published = [...d.versions].reverse().find((v) => v.status === "published");
  const working = last.status === "draft" || last.status === "review" ? last : null;
  return {
    ref: d.ref, kind: d.kind, audience: { level: "national" as const, units: [], province_code: null }, parent_ref: null,
    status: published ? ("published" as const) : working?.status === "review" ? ("review" as const) : ("draft" as const),
    title: published?.title ?? null, summary: published?.summary ?? null, category: published?.category ?? null, tags: published?.tags ?? [],
    published_version_no: published?.version_no ?? null, published_at: published ? published.recorded_at : null, publisher_ref: null, retired_at: null, retire_reason: null,
    created_by: d.created_by, created_at: d.created_at, updated_at: last.recorded_at, last_version_no: last.version_no,
    working_version_no: working?.version_no ?? null, working_status: working ? (working.status as "draft" | "review") : null,
  };
}

const editor = (p: MockPersona) => hasRole(p, "content_editor_national", "content_editor_province");

function versionFrom(p: MockPersona, no: number, input: Record<string, unknown>, uploads: MockDoc["uploads"]): MockVersion | Response {
  const title = typeof input.title === "string" ? input.title.trim() : "";
  if (!title) return validation([{ field: "title", reason: "Bắt buộc" }]);
  const atts: MockVersion["attachments"] = [];
  for (const a of Array.isArray(input.attachments) ? (input.attachments as { id?: string; role?: string }[]) : []) {
    const f = a.id ? uploads.get(a.id) : undefined;
    if (!f) return problem(422, "content.attachment_not_found", "Attachment not found");
    atts.push({ ...f, role: a.role === "cover" ? "cover" : "attachment" });
  }
  return {
    id: uuidv7(), version_no: no, status: "draft", title, summary: typeof input.summary === "string" ? input.summary : "",
    body: sanitizeMock(typeof input.body === "string" ? input.body : ""), body_format: input.body_format === "html" ? "html" : "markdown",
    category: typeof input.category === "string" && input.category ? input.category : null, tags: [], author_ref: p.me.actor_ref, recorded_at: nowIso(), attachments: atts,
  };
}

// --------------------------------------------------------------------------
// Application API
// --------------------------------------------------------------------------
export const htxGapAppRoutes: Route[] = [
  {
    // HTX US-HH-01 AC-2: ?phone= tìm đúng một SĐT (chỉ mục mù ở backend thật).
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/households$/,
    handler: ({ params, url, persona }) => {
      const c = findCoop(params[0]);
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(404, "party.org_not_found", "Not found");
      if (managerOnly(persona) && persona.me.org?.ref !== c.api.ref) return problem(404, "party.org_not_found", "Not found");
      const all = listOf(c);
      const raw = url.searchParams.get("phone");
      if (raw === null || raw.trim() === "") return json(paginate(all, url));
      const phone = normPhone(raw.trim());
      if (!/^0\d{9}$/.test(phone)) return validation([{ field: "phone", reason: "phải là SĐT Việt Nam" }]);
      const idx = Array.from({ length: householdCount(c) }, (_, i) => syntheticPhone(c, i)).indexOf(phone);
      const copied = COPIED.find((h) => h.coop === c.api.ref && h.phone === phone);
      const hit = copied ? all.find((h) => h.household_ref === copied.ref) : idx >= 0 ? all[idx] : undefined;
      return json({ items: hit ? [hit] : [], next_cursor: null });
    },
  },
  {
    // HTX US-HH-03: SĐT đọc lúc bấm gọi, không lưu đệm.
    method: "GET",
    pattern: /^\/v1\/households\/([\w-]+)\/contact$/,
    handler: ({ params, persona }) => {
      if (!hasRole(persona, "coop_manager", "kn_officer", "kn_province_admin", "registry_admin")) return problem(403, "forbidden", "Forbidden");
      const copied = COPIED.find((h) => h.ref === params[0]);
      const at = copied ? { coop: findCoop(copied.coop)!, index: -1 } : locate(params[0] ?? "");
      if (!at || !inScope(persona, at.coop.province_code, at.coop.api.admin_unit_code)) return problem(404, "party.household_not_found", "Không tìm thấy nông hộ");
      if (managerOnly(persona) && persona.me.org?.ref !== at.coop.api.ref) return problem(403, "party.out_of_scope", "Ngoài phạm vi phụ trách", { detail: NOT_YOURS });
      // Hộ thứ 4 (chỉ số 3) giả lập chủ hộ chưa có SĐT hợp lệ trong kho định danh.
      if (at.index === 3) return problem(404, "party.household_contact_unavailable", "Nông hộ chưa có số điện thoại liên hệ");
      const phone = copied ? copied.phone : syntheticPhone(at.coop, at.index);
      return json({ household_ref: params[0], phone }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    // HTX US-HH-02 AC-2..4: sao chép hàng loạt, sửa riêng họ tên/SĐT.
    method: "POST",
    pattern: /^\/v1\/households\/copy$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, "coop_manager", "kn_officer", "kn_province_admin", "registry_admin")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as { items?: { source_ref?: string; full_name?: string; phone?: string }[] };
      const items = Array.isArray(input.items) ? input.items : [];
      if (items.length === 0 || items.length > 50) return validation([{ field: "items", reason: "1–50 hồ sơ" }]);
      const rows = items.map((it, i) => {
        const base = { index: i + 1, source_ref: it.source_ref ?? "", household_ref: null as string | null };
        const reject = (reason: string) => ({ ...base, status: "rejected" as const, reason });
        const name = (it.full_name ?? "").trim();
        const phone = normPhone((it.phone ?? "").trim());
        if (!name) return reject("Thiếu họ tên");
        if (!phone) return reject("Thiếu SĐT");
        const src = locate(it.source_ref ?? "") ?? (() => {
          const c = COPIED.find((h) => h.ref === it.source_ref);
          return c ? { coop: findCoop(c.coop)!, index: -1 } : null;
        })();
        if (!src || !inScope(persona, src.coop.province_code, src.coop.api.admin_unit_code)) return reject("Không tìm thấy nông hộ nguồn");
        if (managerOnly(persona) && persona.me.org?.ref !== src.coop.api.ref) return reject(NOT_YOURS);
        if (!/^0\d{9}$/.test(phone)) return reject("phone: phải là SĐT Việt Nam");
        if (phoneTaken(phone)) return reject("SĐT đã tồn tại");
        const ref = uuidv7();
        const area = src.index >= 0 ? (householdsOf(src.coop)[src.index]?.cultivated_area_ha ?? null) : null;
        COPIED.push({ coop: src.coop.api.ref, ref, phone, label: maskedLabel(name), area });
        return { ...base, status: "created" as const, reason: null, household_ref: ref };
      });
      return json({ created: rows.filter((r) => r.status === "created").length, rejected: rows.filter((r) => r.status === "rejected").length, rows });
    },
  },

  // --- HTX US-DASH-03: lịch của quản lý HTX ---
  {
    method: "GET",
    pattern: /^\/v1\/reports\/recipients$/,
    handler: ({ persona }) => {
      if (!managerOnly(persona) || !persona.me.org) return problem(403, "forbidden", "Forbidden");
      return json({ items: recipientsOf(persona) }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/schedules$/,
    handler: ({ persona }) => {
      if (!managerOnly(persona)) return problem(403, "forbidden", "Forbidden");
      return json({ items: COOP_SCHEDULES.filter((s) => (s.params as Record<string, unknown>).cooperative_ref === persona.me.org?.ref) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/schedules$/,
    handler: ({ persona, body }) => {
      if (!managerOnly(persona) || !persona.me.org) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      if (input.template_key !== "htx.production") return problem(403, "forbidden", "Forbidden", { detail: "vai của bạn không được xuất mẫu này" });
      const freq = input.frequency === "weekly" || input.frequency === "monthly" ? input.frequency : null;
      const hour = typeof input.hour === "number" ? input.hour : -1;
      const recipients = Array.isArray(input.recipients) ? input.recipients.filter((r): r is string => typeof r === "string") : [];
      const errors: { field: string; reason: string }[] = [];
      if (!freq) errors.push({ field: "frequency", reason: "weekly hoặc monthly" });
      if (freq === "weekly" && !(typeof input.weekday === "number" && input.weekday >= 1 && input.weekday <= 7)) errors.push({ field: "weekday", reason: "1 (thứ Hai) … 7 (Chủ nhật)" });
      if (freq === "monthly" && !(typeof input.day_of_month === "number" && input.day_of_month >= 1 && input.day_of_month <= 28)) errors.push({ field: "day_of_month", reason: "1 … 28" });
      if (hour < 0 || hour > 23) errors.push({ field: "hour", reason: "0 … 23 (giờ Việt Nam)" });
      if (recipients.length === 0) errors.push({ field: "recipients", reason: "1 … 50 tài khoản (actor_ref)" });
      if (errors.length) return validation(errors);
      const allowed = new Set(recipientsOf(persona).map((r) => r.actor_ref));
      if (recipients.some((r) => !allowed.has(r))) {
        return problem(422, "reports.recipient_not_in_cooperative", RECIPIENT_BAD, { errors: [{ field: "recipients", reason: "chỉ chọn tài khoản đã đăng ký, có email đã xác minh của HTX" }] });
      }
      const now = nowIso();
      const params = { ...((input.params as Record<string, unknown>) ?? {}), cooperative_ref: persona.me.org.ref };
      const s: ReportSchedule = {
        id: uuidv7(), template_key: "htx.production", format: input.format === "pdf" ? "pdf" : "xlsx", unit_code: null, params: opaque(params), frequency: freq!,
        weekday: freq === "weekly" ? (input.weekday as number) : null, day_of_month: freq === "monthly" ? (input.day_of_month as number) : null, hour, recipients,
        owner_ref: persona.me.actor_ref, active: true, deactivated_reason: null, created_at: now, updated_at: now,
      };
      COOP_SCHEDULES.unshift(s);
      return json(s, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/schedules\/([\w-]+)\/deactivate$/,
    handler: ({ persona, params, body }) => {
      if (!managerOnly(persona)) return problem(403, "forbidden", "Forbidden");
      const s = COOP_SCHEDULES.find((x) => x.id === params[0] && (x.params as Record<string, unknown>).cooperative_ref === persona.me.org?.ref);
      if (!s) return problem(404, "reports.schedule_not_found", "Không tìm thấy lịch gửi báo cáo");
      const reason = reasonOf(body);
      if (reason.length < 10 || reason.length > 500) return validation([{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
      Object.assign(s, { active: false, deactivated_reason: reason, updated_at: nowIso() });
      return json(s);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/schedules\/([\w-]+)\/runs$/,
    handler: ({ persona, params }) => {
      if (!managerOnly(persona)) return problem(403, "forbidden", "Forbidden");
      const s = COOP_SCHEDULES.find((x) => x.id === params[0] && (x.params as Record<string, unknown>).cooperative_ref === persona.me.org?.ref);
      if (!s) return problem(404, "reports.schedule_not_found", "Không tìm thấy lịch gửi báo cáo");
      // Mốc đầu giả lập: một người nhận thiếu email → người lập thấy cảnh báo AC-2.
      return json({
        items: [
          {
            slot_key: "2026-W39", slot_at: nowIso(), status: "queued", skip_reason: null, job_id: uuidv7(), fired_at: nowIso(),
            delivery_status: s.recipients.length > 1 ? "sent" : "skipped", delivery_note: s.recipients.length > 1 ? "1 người nhận chưa có email" : "1 người nhận chưa có email",
            sent_count: Math.max(0, s.recipients.length - 1), delivered_at: nowIso(),
          },
        ],
      });
    },
  },
];

// --------------------------------------------------------------------------
// Admin API
// --------------------------------------------------------------------------
const COOP_HEADER = ["Tên HTX", "Địa chỉ", "Người đại diện", "SĐT", "Mã xã", "Mã số thuế", "Diện tích (ha)", "Ngày thành lập", "Mã tổ chức cấp trên"];
const REQUIRED: [string, string][] = [
  ["ten_htx", "Tên HTX"],
  ["dia_chi", "Địa chỉ"],
  ["nguoi_dai_dien", "Người đại diện"],
  ["sdt", "SĐT"],
  ["ma_xa", "Mã xã"],
];
const nameTaken = (name: string) => COOPERATIVES.some((c) => c.api.name.trim().toLowerCase() === name.trim().toLowerCase());
let coopSeq = 90000;

function cloneCoop(src: MockCooperative, name: string, address: string | null): MockCooperative {
  coopSeq += 1;
  const ref = uuidv7();
  const c: MockCooperative = {
    ...src,
    member_households: 0,
    plots: 0,
    api: {
      ...src.api, ref, name: name.trim(), code: `HTX${src.province_code}${coopSeq}`, tax_code: null, location: null, location_provisional: true,
      representative_ref: null, member_households: 0, digitized_plots: 0, address, active: true, status_reason: null,
    },
  };
  COOPERATIVES.push(c);
  return c;
}

export const htxGapAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/import-template\.xlsx$/,
    handler: ({ persona }) => {
      if (!admin(persona, "registry_admin", "kn_province_admin")) return problem(403, "forbidden", "Forbidden");
      // Giả lập: CSV UTF-8 (BOM) cùng cột với tệp .xlsx thật.
      const example = ["HTX Nông nghiệp Ví dụ", "Ấp 1, xã …", "Nguyễn Văn A", "0901234567", "<mã xã>", "", "50", "2020-01-15", ""];
      return new Response(`﻿${COOP_HEADER.join(",")}\r\n${example.map((v) => `"${v}"`).join(",")}\r\n`, {
        status: 200,
        headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": 'attachment; filename="mau-nhap-htx-gia-lap.csv"', "Cache-Control": "no-store" },
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cooperatives\/import$/,
    handler: ({ persona, body, request }) => {
      if (!admin(persona, "registry_admin", "kn_province_admin")) return problem(403, "forbidden", "Forbidden");
      if (isZip(body)) {
        return json({
          created: 1, rejected: 1,
          rows: [
            { row: 2, status: "created", reason: null, org_ref: uuidv7(), code: "HTX8790001", name: "HTX minh hoạ (.xlsx giả lập)" },
            { row: 3, status: "rejected", reason: "Thiếu cột bắt buộc: SĐT", org_ref: null, code: null, name: "HTX thiếu SĐT" },
          ],
        });
      }
      if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) {
        return problem(422, "party.import_invalid", "Tệp nhập không đúng mẫu quy định", { detail: "Sai định dạng file: chỉ nhận .xlsx hoặc CSV UTF-8" });
      }
      const rows = parseCsv(typeof body === "string" ? body.replace(/^﻿/, "") : "");
      const header = (rows[0] ?? []).map(headerKey);
      const col = (k: string) => header.indexOf(k);
      const missing = REQUIRED.filter(([k]) => col(k) < 0).map(([, l]) => l);
      const data = rows.slice(1).filter((r) => r.some((c) => c.trim() !== ""));
      if (data.length === 0) return problem(422, "party.import_invalid", "Tệp nhập không đúng mẫu quy định", { detail: "Sai định dạng file: tệp không có dòng dữ liệu" });
      const out = data.map((r, i) => {
        const row = i + 2;
        const cell = (k: string) => (col(k) >= 0 ? (r[col(k)] ?? "").trim() : "");
        const name = cell("ten_htx");
        const reject = (reason: string) => ({ row, status: "rejected" as const, reason, org_ref: null, code: null, name: name || null });
        if (missing.length) return reject(`Thiếu cột bắt buộc: ${missing.join(", ")}`);
        const empty = REQUIRED.filter(([k]) => !cell(k)).map(([, l]) => l);
        if (empty.length) return reject(`Thiếu cột bắt buộc: ${empty.join(", ")}`);
        const src = COOPERATIVES.find((c) => c.api.admin_unit_code === cell("ma_xa"));
        if (!src) return reject("admin_unit_code: không có đơn vị hành chính còn hiệu lực với mã này");
        if (!inScope(persona, src.province_code, src.api.admin_unit_code)) return reject("Ngoài phạm vi phụ trách");
        if (nameTaken(name)) return reject(NAME_TAKEN);
        const c = cloneCoop(src, name, cell("dia_chi"));
        return { row, status: "created" as const, reason: null, org_ref: c.api.ref, code: c.api.code, name };
      });
      return json({ created: out.filter((x) => x.status === "created").length, rejected: out.filter((x) => x.status === "rejected").length, rows: out });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/copy$/,
    handler: ({ persona, params, body }) => {
      if (!admin(persona, "registry_admin", "kn_province_admin")) return problem(403, "forbidden", "Forbidden");
      const src = findCoop(params[0]);
      if (!src || !inScope(persona, src.province_code, src.api.admin_unit_code)) return problem(404, "party.org_not_found", "Không tìm thấy tổ chức");
      const input = (body ?? {}) as { name?: string; representative?: { full_name?: string; phone?: string } };
      const name = (input.name ?? "").trim();
      if (!name || name.length > 200) return validation([{ field: "name", reason: "name bắt buộc, tối đa 200 ký tự" }]);
      if (nameTaken(name)) return problem(409, "party.cooperative_name_taken", NAME_TAKEN);
      const rep = input.representative;
      if (rep && (!(rep.full_name ?? "").trim() || !(rep.phone ?? "").trim())) return validation([{ field: "representative", reason: "họ tên và SĐT người đại diện là bắt buộc" }]);
      const c = cloneCoop(src, name, src.api.address ?? null);
      c.api.area_ha = src.api.area_ha;
      c.api.established_on = src.api.established_on;
      c.api.representative_ref = rep ? uuidv7() : null;
      const org = { ref: c.api.ref, kind: "cooperative" as const, code: c.api.code, name: c.api.name, admin_unit_code: c.api.admin_unit_code, parent_ref: c.api.parent_ref, representative_ref: c.api.representative_ref, active: true };
      return json({ org, source_ref: src.api.ref, representative_duplicate: false }, 201);
    },
  },

  // --- pf/content: soạn bài viết có xem trước mobile (HTX US-CMS-01) ---
  {
    method: "GET",
    pattern: /^\/v1\/documents$/,
    handler: ({ persona, url }) => {
      if (!editor(persona) && !hasRole(persona, "content_reviewer")) return problem(403, "forbidden", "Forbidden");
      return json(paginate(DOCS_ADMIN.map(docView).reverse(), url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents$/,
    handler: ({ persona, body }) => {
      if (!editor(persona)) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const kind = (typeof input.kind === "string" ? input.kind : "news") as ContentKind;
      const d: MockDoc = { ref: uuidv7(), kind, created_by: persona.me.actor_ref, created_at: nowIso(), versions: [], uploads: new Map() };
      const v = versionFrom(persona, 1, input, d.uploads);
      if (v instanceof Response) return v;
      d.versions.push(v);
      DOCS_ADMIN.push(d);
      return json({ document: docView(d), version: v }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents\/([\w-]+)\/attachments$/,
    handler: ({ persona, params, body }) => {
      if (!editor(persona)) return problem(403, "forbidden", "Forbidden");
      const d = DOCS_ADMIN.find((x) => x.ref === params[0]);
      if (!d) return problem(404, "content.document_not_found", "Not found");
      const size = body instanceof Uint8Array ? body.length : 0;
      const f = { id: uuidv7(), file_name: "anh-bia.png", content_type: "image/png" as const, size, sha256: "0".repeat(64), role: "attachment" as const, url: null, url_expires_at: null };
      d.uploads.set(f.id, f);
      return json({ id: f.id, document_ref: d.ref, file_name: f.file_name, content_type: f.content_type, size, sha256: f.sha256, created_at: nowIso() }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents\/([\w-]+)\/versions$/,
    handler: ({ persona, params, body }) => {
      if (!editor(persona)) return problem(403, "forbidden", "Forbidden");
      const d = DOCS_ADMIN.find((x) => x.ref === params[0]);
      if (!d) return problem(404, "content.document_not_found", "Not found");
      const last = d.versions[d.versions.length - 1]!;
      if (last.status === "review") return problem(409, "content.invalid_transition", "Đang chờ duyệt");
      const v = versionFrom(persona, last.version_no + 1, (body ?? {}) as Record<string, unknown>, d.uploads);
      if (v instanceof Response) return v;
      if (last.status === "draft") last.status = "discarded";
      d.versions.push(v);
      return json({ document: docView(d), version: v }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/documents\/([\w-]+)\/versions\/(\d+)$/,
    handler: ({ persona, params }) => {
      if (!editor(persona) && !hasRole(persona, "content_reviewer")) return problem(403, "forbidden", "Forbidden");
      const d = DOCS_ADMIN.find((x) => x.ref === params[0]);
      const v = d?.versions.find((x) => x.version_no === Number(params[1]));
      if (!d || !v) return problem(404, "content.version_not_found", "Không có phiên bản");
      return json({ document: docView(d), version: v });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents\/([\w-]+)\/submit$/,
    handler: ({ persona, params }) => {
      if (!editor(persona)) return problem(403, "forbidden", "Forbidden");
      const d = DOCS_ADMIN.find((x) => x.ref === params[0]);
      if (!d) return problem(404, "content.document_not_found", "Not found");
      const last = d.versions[d.versions.length - 1]!;
      if (last.status !== "draft") return problem(409, "content.invalid_transition", "Không có bản nháp để gửi duyệt");
      if (!last.title.trim() || !last.category) {
        return problem(422, "validation_failed", "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản.", { errors: [{ field: "category", reason: "bắt buộc với tin tức" }] });
      }
      last.status = "review";
      return json({ document: docView(d) });
    },
  },
];
