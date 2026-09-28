// GIẢ LẬP danh bạ trực pf/extension (KN US-DIR-01/02/03) — hình dạng theo contracts/openapi/application.yaml:
// GET /v1/extension/roster?unit=, GET /v1/extension/roster/{id}/contact, POST /roster/status, POST /roster/shifts.
// Tên và số điện thoại là DỮ LIỆU TỔNG HỢP (đầu số 0999x như R-95), không phải người thật. Trạng thái giữ trong bộ nhớ tab.
import type { AppGet } from "../types";
import { COMMUNES, PROVINCES } from "./data";
import { hasRole, inScope, json, nowIso, problem, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

type RosterEntry = AppGet<"/v1/extension/roster">["items"][number];
type Staff = Omit<RosterEntry, "duty_status" | "status_source" | "until">;

const READERS = ["kn_officer", "kn_province_admin", "kn_national_admin", "coop_manager", "platform_admin"];
const SUPERVISORS = ["kn_province_admin", "kn_national_admin", "platform_admin"];
const UPDATED = "2026-09-01T08:00:00+07:00";

/** Cán bộ cấp tỉnh: actor_ref của persona TTKN tỉnh Đồng Tháp là mock-tinh-0082. */
const provinceActor = (code: string) => `mock-tinh-00${code}`;
const officerActor = (commune: string, n: 1 | 2) => (commune === "82101" && n === 1 ? "mock-xa-0001" : `mock-knv-${commune}-${n}`);

// Tây Ninh (80) chưa có cán bộ trong danh bạ giả lập (như chỉ đạo khẩn).
const STAFF: Staff[] = [
  ...PROVINCES.filter((p) => p.code !== "80").map(
    (p): Staff => ({
      actor_ref: provinceActor(p.code),
      unit_code: p.code,
      province_code: p.code,
      role_title: "Cán bộ TTKN tỉnh",
      display_name: `Cán bộ TTKN ${p.name} (giả lập)`,
      active: true,
      updated_at: UPDATED,
    }),
  ),
  ...COMMUNES.filter((c) => c.province_code !== "80").flatMap((c) =>
    ([1, 2] as const).map(
      (n): Staff => ({
        actor_ref: officerActor(c.code, n),
        unit_code: c.code,
        province_code: c.province_code,
        role_title: "Khuyến nông viên xã",
        display_name: `KNV ${c.name.replace(/^Xã /, "")} ${n} (giả lập)`,
        active: true,
        updated_at: UPDATED,
      }),
    ),
  ),
];

/** Số điện thoại tổng hợp theo actor (0999 + 6 chữ số). Một cán bộ chưa khai số để thử extension.contact_unavailable. */
export const NO_PHONE_ACTOR = "mock-knv-82101-2";
function phoneOf(actor: string): string | null {
  if (actor === NO_PHONE_ACTOR) return null;
  let h = 0;
  for (const ch of actor) h = (h * 31 + ch.charCodeAt(0)) % 1_000_000;
  return `0999${String(h).padStart(6, "0")}`;
}

interface Manual {
  status: "on_duty" | "off_duty";
  setAt: number;
  until: number;
}
const manual = new Map<string, Manual>();
const shifts: { actor: string; starts: number; ends: number }[] = [];

// Trạng thái ban đầu: xã 82101 có người tự khai + người theo ca; xã 82102 không ai trực → dự phòng cấp tỉnh.
(function seed() {
  const now = Date.now();
  manual.set("mock-xa-0001", { status: "on_duty", setAt: now - 3_600_000, until: now + 7 * 3_600_000 });
  shifts.push({ actor: "mock-knv-82101-2", starts: now - 2 * 3_600_000, ends: now + 6 * 3_600_000 });
  shifts.push({ actor: provinceActor("82"), starts: now - 3_600_000, ends: now + 10 * 3_600_000 });
  manual.set("mock-knv-82102-1", { status: "off_duty", setAt: now - 3_600_000, until: now + 3_600_000 });
  for (const c of COMMUNES) {
    if (c.province_code === "80" || c.code === "82101" || c.code === "82102") continue;
    if (Number(c.code.slice(-1)) % 2 === 1) manual.set(officerActor(c.code, 1), { status: "on_duty", setAt: now - 1_800_000, until: now + 8 * 3_600_000 });
  }
})();

function entry(s: Staff, now: number): RosterEntry {
  const m = manual.get(s.actor_ref);
  if (m && now >= m.setAt && now < m.until) return { ...s, duty_status: m.status, status_source: "manual", until: new Date(m.until).toISOString() };
  const sh = shifts.find((x) => x.actor === s.actor_ref && now >= x.starts && now < x.ends);
  if (sh) return { ...s, duty_status: "on_duty", status_source: "shift", until: new Date(sh.ends).toISOString() };
  return { ...s, duty_status: "unknown", status_source: "none", until: null };
}

const visible = (p: MockPersona, s: Staff) => inScope(p, s.province_code, s.unit_code);
/** Cán bộ cấp tỉnh là nơi dự phòng của mọi xã trong tỉnh — cán bộ xã trong tỉnh liên hệ được. */
const contactVisible = (p: MockPersona, s: Staff) =>
  visible(p, s) || (s.unit_code === s.province_code && p.me.scope.units.some((u) => u === s.province_code || u.startsWith(s.province_code)));

function roster(persona: MockPersona, unit: string): Response {
  if (!hasRole(persona, ...READERS)) return problem(403, "forbidden", "Forbidden");
  const isProvince = PROVINCES.some((p) => p.code === unit);
  const commune = COMMUNES.find((c) => c.code === unit);
  if (unit && !isProvince && !commune) return problem(422, "extension.unit_invalid", "Mã đơn vị hành chính không có hiệu lực");
  if (unit && !inScope(persona, isProvince ? unit : commune?.province_code, isProvince ? null : unit)) {
    return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
  }
  const now = Date.now();
  const items = STAFF.filter((s) => (unit ? s.unit_code === unit || (isProvince && s.province_code === unit) : visible(persona, s))).map((s) => entry(s, now));
  const onDuty = items.filter((e) => e.duty_status === "on_duty").length;
  const fallback =
    onDuty === 0 && commune ? STAFF.filter((s) => s.unit_code === commune.province_code).map((s) => entry(s, now)) : [];
  return json({ unit: unit || null, on_duty: onDuty, items, fallback });
}

export const rosterRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/extension\/roster$/, handler: ({ persona, url }) => roster(persona, url.searchParams.get("unit") ?? "") },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/roster\/([^/]+)\/contact$/,
    handler: ({ persona, params }) => {
      if (!hasRole(persona, ...READERS)) return problem(403, "forbidden", "Forbidden");
      const s = STAFF.find((x) => x.actor_ref === params[0]);
      if (!s || !s.active || !contactVisible(persona, s)) return problem(404, "extension.staff_not_found", "Cán bộ chưa có trong danh bạ");
      const phone = phoneOf(s.actor_ref);
      if (!phone) return problem(404, "extension.contact_unavailable", "Cán bộ chưa khai số điện thoại liên hệ");
      return json({ actor_ref: s.actor_ref, phone }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/roster\/status$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "kn_officer", "kn_province_admin", "kn_national_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const status = (body as { status?: string } | undefined)?.status;
      if (status !== "on_duty" && status !== "off_duty") return validation([{ field: "status", reason: "on_duty hoặc off_duty" }]);
      if (!STAFF.some((s) => s.actor_ref === persona.me.actor_ref)) return problem(404, "extension.staff_not_found", "Cán bộ chưa có trong danh bạ");
      const now = Date.now();
      const until = now + 12 * 3_600_000; // extension.duty_status_ttl_hours mặc định 12
      manual.set(persona.me.actor_ref, { status, setAt: now, until });
      return json({ status, set_at: nowIso(), valid_until: new Date(until).toISOString() }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/roster\/shifts$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, ...SUPERVISORS)) return problem(403, "forbidden", "Forbidden");
      const b = (body ?? {}) as { actor_ref?: string; starts_at?: string; ends_at?: string };
      const starts = Date.parse(b.starts_at ?? "");
      const ends = Date.parse(b.ends_at ?? "");
      if (Number.isNaN(starts) || Number.isNaN(ends)) return validation([{ field: "starts_at", reason: "phải theo RFC 3339 có múi giờ" }]);
      if (ends <= starts) return validation([{ field: "ends_at", reason: "phải sau starts_at" }]);
      if (ends - starts > 7 * 86_400_000) return validation([{ field: "ends_at", reason: "một ca tối đa 7 ngày" }]);
      const s = STAFF.find((x) => x.actor_ref === b.actor_ref);
      if (!s || !visible(persona, s)) return problem(404, "extension.staff_not_found", "Cán bộ chưa có trong danh bạ");
      shifts.push({ actor: s.actor_ref, starts, ends });
      return json(
        { shift_id: `mock-shift-${shifts.length}`, actor_ref: s.actor_ref, starts_at: new Date(starts).toISOString(), ends_at: new Date(ends).toISOString() },
        201,
      );
    },
  },
];
