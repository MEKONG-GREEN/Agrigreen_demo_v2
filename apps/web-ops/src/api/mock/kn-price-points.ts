// ============================================================================
// BACKEND GIẢ LẬP — Xã báo giá + phân công (W11.72, cap/price, BRD Báo giá nông sản v1.3 FN-AD-04.02, FN-AD-04.01).
// Đường dẫn, tham số, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/price/status, regions*,
// points*, admin-units, officers). Quy tắc mô phỏng (rút gọn từ internal/platform/price):
//   - Xã có phiên bản theo valid_from (vắng → ngày mai = ngày báo giá kế tiếp; ngày đã qua → 422 price.valid_from); mã
//     XBG#### tự sinh; ĐVHC đã gắn với xã khác đang hoạt động → 409 price.admin_unit_taken.
//   - Phân công cán bộ Xã / cán bộ Tỉnh theo ngày hiệu lực, lịch sử giữ; null = bỏ phân công.
//   - Vai: Admin ghi; Cấp Tỉnh, Cấp Quốc gia, Cấp Xã chỉ xem (tên cán bộ chỉ Admin, Cấp Tỉnh).
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { addDay, vnToday } from "./kn-price-items";

type S = components["schemas"];
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";

interface PointVer {
  seq: number;
  from: string;
  status: "active" | "retired";
  au: string;
  name: string;
  region: string;
  note: string | null;
  at: string;
}
interface OfficerVer {
  seq: number;
  role: "xa" | "tinh";
  ref: string | null;
  from: string;
  at: string;
}
interface RegionVer {
  seq: number;
  from: string;
  status: "active" | "retired";
  name: string;
  sort: number;
}

const ACTOR = "0190c0de-0000-7000-8000-00000000ad01";
const OFFICERS: S["PriceOfficer"][] = [
  { ref: "0190c0de-0000-7000-8000-0000000c7a01", name: "Nguyễn Văn An", level: "XA", communes: ["30280"] },
  { ref: "0190c0de-0000-7000-8000-0000000c7a02", name: "Trần Thị Bình", level: "XA", communes: ["30283"] },
  { ref: "0190c0de-0000-7000-8000-0000000c7a03", name: "Lê Văn Cường", level: "XA", communes: ["31123"] },
  { ref: "0190c0de-0000-7000-8000-0000000c7b01", name: "Phạm Thu Hà", level: "TINH", communes: [] },
];
const UNITS: [string, string][] = [
  ["30280", "Xã Mỹ Chánh"],
  ["30283", "Xã Mỹ Phú"],
  ["30286", "Xã Bình Hòa"],
  ["31123", "Xã Thạnh Lộc"],
  ["31126", "Xã Bình An"],
];

interface Store {
  regions: Map<string, RegionVer[]>;
  points: Map<string, PointVer[]>;
  officers: Map<string, OfficerVer[]>; // khóa = mã xã
  next: number;
}
let STORE: Store = seed();

function seed(): Store {
  const from = addDay(vnToday(), -60);
  const at = new Date().toISOString();
  const s: Store = { regions: new Map(), points: new Map(), officers: new Map(), next: 1 };
  s.regions.set("an_giang", [{ seq: 1, from, status: "active", name: "An Giang", sort: 10 }]);
  s.regions.set("kien_giang", [{ seq: 1, from, status: "active", name: "Kiên Giang", sort: 20 }]);
  const add = (au: string, region: string, xa: string | null) => {
    const code = `XBG${String(s.next++).padStart(4, "0")}`;
    const name = UNITS.find(([c]) => c === au)![1];
    s.points.set(code, [{ seq: 1, from, status: "active", au, name, region, note: null, at }]);
    s.officers.set(code, xa ? [{ seq: 1, role: "xa", ref: xa, from, at }] : []);
  };
  add("30280", "an_giang", OFFICERS[0]!.ref);
  add("30283", "an_giang", OFFICERS[1]!.ref);
  add("31123", "kien_giang", null);
  return s;
}

/** Đặt lại trạng thái giả lập (kiểm thử). */
export function resetPricePoints(): void {
  STORE = seed();
}

const isDay = (s: unknown): s is string => typeof s === "string" && /^\d{4}-\d{2}-\d{2}$/.test(s);

function eff<T extends { from: string; seq: number }>(vs: T[] | undefined, at: string): T | undefined {
  let out: T | undefined;
  for (const v of vs ?? []) {
    if (v.from > at) continue;
    if (!out || v.from > out.from || (v.from === out.from && v.seq > out.seq)) out = v;
  }
  return out;
}

function roleOf(ctx: Ctx): Role | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (kc) return kc.role;
  const r = ctx.persona.me.roles;
  if (r.includes("platform_admin")) return "admin";
  if (r.includes("kn_province_admin")) return "kn_tinh";
  if (r.includes("kn_officer")) return "kn_xa";
  return problem(403, "forbidden", "vai của người gọi không thuộc cấu hình giá kn");
}

const atOf = (url: URL) => (isDay(url.searchParams.get("at")) ? url.searchParams.get("at")! : vnToday());
const written = (code: string, version: number, from: string, n = 1) =>
  json({ code, version, valid_from: from, written: n } satisfies S["PriceWriteResult"], 201);
const notAdmin = () => problem(403, "forbidden", "chỉ vai quản trị (Admin) quản lý xã báo giá, nhóm vùng, phân công");

function fromDay(raw: unknown): string | Response {
  const today = vnToday();
  if (raw === undefined || raw === null || raw === "") return addDay(today, 1);
  if (!isDay(raw)) return problem(422, "validation_failed", "valid_from phải là ngày YYYY-MM-DD");
  if (raw < today) return problem(422, "price.valid_from", `ngày hiệu lực ${raw} đã qua`);
  return raw;
}

function pointState(code: string, at: string): { v: PointVer; state: S["PricePoint"]["state"]; since: string; to: string | null } | null {
  const vs = STORE.points.get(code);
  if (!vs) return null;
  const e = eff(vs, at);
  const since = vs.filter((v) => v.status === "active").map((v) => v.from).sort()[0] ?? null;
  const later = vs.filter((v) => v.status === "retired" && v.from > at).map((v) => v.from).sort()[0];
  if (e) {
    if (e.status === "retired") return { v: e, state: "retired", since: since!, to: addDay(e.from, -1) };
    return { v: e, state: "active", since: since!, to: later ? addDay(later, -1) : null };
  }
  const up = [...vs].sort((a, b) => (a.from === b.from ? b.seq - a.seq : a.from < b.from ? -1 : 1))[0]!;
  return { v: up, state: "pending", since: since!, to: later ? addDay(later, -1) : null };
}

function officerAt(code: string, role: "xa" | "tinh", at: string): OfficerVer | undefined {
  return eff((STORE.officers.get(code) ?? []).filter((o) => o.role === role), at);
}

function nameOf(ref: string | null, role: Role): string | null {
  if (!ref || (role !== "admin" && role !== "kn_tinh")) return null;
  return OFFICERS.find((o) => o.ref === ref)?.name ?? null;
}

function view(code: string, at: string, role: Role): S["PricePoint"] | null {
  const st = pointState(code, at);
  if (!st) return null;
  const xa = officerAt(code, "xa", at);
  const tinh = officerAt(code, "tinh", at);
  return {
    code,
    name: st.v.name,
    admin_unit_code: st.v.au,
    admin_unit_name: UNITS.find(([c]) => c === st.v.au)?.[1] ?? st.v.name,
    admin_unit_valid: true,
    successor_codes: [],
    region_code: st.v.region,
    region_name: eff(STORE.regions.get(st.v.region), at)?.name ?? st.v.region,
    state: st.state,
    version: st.v.seq,
    valid_from: st.v.from,
    since: st.since,
    valid_to: st.to,
    note: st.v.note,
    officer_ref: xa?.ref ?? null,
    officer_name: nameOf(xa?.ref ?? null, role),
    officer_since: xa?.ref ? xa.from : null,
    owner_ref: tinh?.ref ?? null,
    owner_name: nameOf(tinh?.ref ?? null, role),
    owner_since: tinh?.ref ? tinh.from : null,
    item_count: st.state === "active" ? 32 : 0,
  };
}

function takenBy(au: string, self: string | null, from: string): string | null {
  for (const code of STORE.points.keys()) {
    if (code === self) continue;
    const st = pointState(code, from);
    if (st && st.state !== "retired" && st.v.au === au) return code;
  }
  return null;
}

function regionRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/regions$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        const at = atOf(ctx.url);
        const regions: S["PriceRegion"][] = [];
        for (const [code, vs] of STORE.regions) {
          const v = eff(vs, at);
          if (!v) continue;
          const points = [...STORE.points.keys()].filter((p) => {
            const st = pointState(p, at);
            return st?.state === "active" && st.v.region === code;
          }).length;
          regions.push({ code, name: v.name, sort_order: v.sort, version: v.seq, valid_from: v.from, status: v.status, points });
        }
        regions.sort((a, b) => a.sort_order - b.sort_order);
        return json({ regions });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/regions$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        if (role !== "admin") return notAdmin();
        const b = ctx.body as S["PriceRegionInput"];
        if (!b?.name?.trim()) return problem(422, "validation_failed", "Nhập tên nhóm vùng");
        let code = b.code;
        if (!code) for (let n = STORE.regions.size + 1; STORE.regions.has((code = `vung_${n}`)); n++);
        if (STORE.regions.has(code)) return problem(409, "conflict", `mã nhóm vùng ${code} đã có`);
        const sort = Math.max(0, ...[...STORE.regions.values()].map((vs) => vs.at(-1)!.sort)) + 10;
        STORE.regions.set(code, [{ seq: 1, from: vnToday(), status: "active", name: b.name.trim(), sort }]);
        return written(code, 1, vnToday());
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/regions\/([a-z][a-z0-9_]*)\/(revise|retire)$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        if (role !== "admin") return notAdmin();
        const code = ctx.params[0] ?? "";
        const op = ctx.params[1] ?? "";
        const vs = STORE.regions.get(code);
        const cur = eff(vs, vnToday());
        if (!vs || !cur || cur.status !== "active") return problem(422, "price.region_not_found", `nhóm vùng ${code}`);
        if (op === "retire") {
          const used = [...STORE.points.keys()].find((p) => {
            const st = pointState(p, vnToday());
            return st && st.state !== "retired" && st.v.region === code;
          });
          if (used) return problem(409, "price.region_in_use", `còn xã báo giá ${used}`);
        }
        const b = (ctx.body ?? {}) as S["PriceRegionInput"];
        const v: RegionVer = {
          ...cur,
          seq: vs.length + 1,
          from: vnToday(),
          status: op === "retire" ? "retired" : "active",
          name: op === "revise" ? b.name?.trim() || cur.name : cur.name,
        };
        vs.push(v);
        return written(code, v.seq, v.from);
      },
    },
  ];
}

function pointRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/status$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        const level = role === "kn_tw" ? "TW" : role === "kn_tinh" ? "TINH" : role === "kn_xa" ? "XA" : "";
        return json({
          profile: "kn",
          department: "thong_tin_truyen_thong",
          level,
          can_manage: role === "admin",
          can_steward: role === "admin" || role === "kn_tinh",
          rule_version: "price.v1",
          day: vnToday(),
          units: 8,
          groups: 5,
        } satisfies S["PriceStatus"]);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/points$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        const at = atOf(ctx.url);
        const region = ctx.url.searchParams.get("region");
        const state = ctx.url.searchParams.get("state");
        const order = (r: string) => eff(STORE.regions.get(r), at)?.sort ?? 1 << 30;
        const points = [...STORE.points.keys()]
          .map((c) => view(c, at, role))
          .filter((p): p is S["PricePoint"] => !!p && (!region || p.region_code === region) && (!state || p.state === state))
          .sort((a, b) => order(a.region_code) - order(b.region_code) || (a.code < b.code ? -1 : 1));
        return json({ points });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/points$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        if (role !== "admin") return notAdmin();
        const b = (ctx.body ?? {}) as S["PricePointInput"];
        const from = fromDay(b.valid_from);
        if (from instanceof Response) return from;
        const unit = UNITS.find(([c]) => c === b.admin_unit_code);
        if (!unit) return problem(422, "price.admin_unit_invalid", `mã ĐVHC ${b.admin_unit_code ?? ""}`);
        if (!b.region_code || eff(STORE.regions.get(b.region_code), from)?.status !== "active")
          return problem(422, "price.region_not_found", `nhóm vùng ${b.region_code ?? ""}`);
        const other = takenBy(unit[0], null, from);
        if (other) return problem(409, "price.admin_unit_taken", `mã ĐVHC ${unit[0]} đang gắn với xã báo giá ${other}`);
        const code = `XBG${String(STORE.next++).padStart(4, "0")}`;
        STORE.points.set(code, [
          { seq: 1, from, status: "active", au: unit[0], name: unit[1], region: b.region_code, note: b.note ?? null, at: new Date().toISOString() },
        ]);
        STORE.officers.set(code, []);
        return written(code, 1, from);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/points\/(XBG\d{4})$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        const code = ctx.params[0] ?? "";
        const p = view(code, atOf(ctx.url), role);
        if (!p) return problem(404, "price.point_not_found", `xã báo giá ${code}`);
        return json({
          point: p,
          versions: STORE.points.get(code)!.map((v) => ({
            version: v.seq,
            valid_from: v.from,
            status: v.status,
            admin_unit_code: v.au,
            name: v.name,
            region_code: v.region,
            note: v.note,
            event_time: v.at,
            actor_ref: ACTOR,
          })),
          officers: (STORE.officers.get(code) ?? [])
            .slice()
            .sort((a, b) => (a.from === b.from ? a.seq - b.seq : a.from < b.from ? -1 : 1))
            .map((o) => ({
              role: o.role,
              officer_ref: o.ref,
              name: nameOf(o.ref, role),
              valid_from: o.from,
              version: o.seq,
              event_time: o.at,
              actor_ref: ACTOR,
            })),
        } satisfies S["PricePointDetail"]);
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/points\/(XBG\d{4})\/(revise|retire|officers)$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        if (role !== "admin") return notAdmin();
        const code = ctx.params[0] ?? "";
        const op = ctx.params[1] ?? "";
        const vs = STORE.points.get(code);
        if (!vs) return problem(404, "price.point_not_found", `xã báo giá ${code}`);
        const b = (ctx.body ?? {}) as S["PricePointInput"] & S["PriceOfficerInput"];
        const from = fromDay(b.valid_from);
        if (from instanceof Response) return from;
        const st = pointState(code, from)!;
        if (st.state === "retired") return problem(422, "price.point_inactive", `xã báo giá ${code} đã ngưng tại ${from}`);
        const at = new Date().toISOString();
        if (op === "officers") {
          if (b.role !== "xa" && b.role !== "tinh") return problem(422, "validation_failed", "vai phụ trách phải là xa | tinh");
          const ref = b.officer_ref ?? null;
          if (ref && !OFFICERS.some((o) => o.ref === ref && o.level === (b.role === "xa" ? "XA" : "TINH")))
            return problem(422, "price.officer_invalid", "Người được phân công không phải cán bộ đúng cấp");
          const os = STORE.officers.get(code)!;
          if ((officerAt(code, b.role, from)?.ref ?? null) === ref) return written(code, 0, from, 0);
          const seq = os.filter((o) => o.role === b.role).length + 1;
          os.push({ seq, role: b.role, ref, from, at });
          return written(code, seq, from);
        }
        const v: PointVer = { ...st.v, seq: vs.length + 1, from, at, note: (b.note ?? b.reason ?? "").trim() || null };
        if (op === "retire") {
          if (!v.note) return problem(422, "validation_failed", "ngưng xã báo giá phải có lý do");
          v.status = "retired";
        } else {
          if (b.admin_unit_code) {
            const unit = UNITS.find(([c]) => c === b.admin_unit_code);
            if (!unit) return problem(422, "price.admin_unit_invalid", `mã ĐVHC ${b.admin_unit_code}`);
            const other = takenBy(unit[0], code, from);
            if (other) return problem(409, "price.admin_unit_taken", `mã ĐVHC ${unit[0]} đang gắn với xã báo giá ${other}`);
            v.au = unit[0];
            v.name = unit[1];
          }
          if (b.region_code) {
            if (eff(STORE.regions.get(b.region_code), from)?.status !== "active")
              return problem(422, "price.region_not_found", `nhóm vùng ${b.region_code}`);
            v.region = b.region_code;
          }
        }
        vs.push(v);
        return written(code, v.seq, from);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/admin-units$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        const at = atOf(ctx.url);
        return json({
          admin_units: UNITS.map(([code, name]) => ({
            code,
            name,
            parent_code: "89",
            valid_from: "2025-07-01",
            valid_to: null,
            taken_by: takenBy(code, null, at),
          })),
        });
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/officers$/,
      handler: (ctx) => {
        const role = roleOf(ctx);
        if (role instanceof Response) return role;
        if (role !== "admin" && role !== "kn_tinh")
          return problem(403, "forbidden", "chỉ vai quản trị, Cấp Tỉnh xem danh bạ cán bộ");
        const level = ctx.url.searchParams.get("level");
        if (level !== "XA" && level !== "TINH") return problem(422, "validation_failed", "level phải là XA | TINH");
        return json({ officers: OFFICERS.filter((o) => o.level === level), directory: true });
      },
    },
  ];
}

export const pricePointRoutes: Route[] = [...regionRoutes(), ...pointRoutes()];

/** W11.75 (phiếu báo giá giả lập): xã báo giá đang hoạt động tại ngày at + người phụ trách. */
export function mockPointsAt(at: string): {
  code: string;
  name: string;
  region_code: string;
  region_name: string;
  officer_ref: string | null;
  officer_name: string | null;
  owner_ref: string | null;
  owner_name: string | null;
}[] {
  const out = [];
  for (const code of [...STORE.points.keys()].sort()) {
    const st = pointState(code, at);
    if (!st || st.state !== "active") continue;
    const off = officerAt(code, "xa", at)?.ref ?? null;
    const own = officerAt(code, "tinh", at)?.ref ?? null;
    out.push({
      code,
      name: st.v.name,
      region_code: st.v.region,
      region_name: eff(STORE.regions.get(st.v.region), at)?.name ?? st.v.region,
      officer_ref: off,
      officer_name: OFFICERS.find((o) => o.ref === off)?.name ?? null,
      owner_ref: own,
      owner_name: OFFICERS.find((o) => o.ref === own)?.name ?? null,
    });
  }
  return out;
}

/** Cán bộ Xã giả lập của persona Cấp Xã (phụ trách XBG0001). */
export const MOCK_XA_OFFICER = OFFICERS[0]!.ref;
