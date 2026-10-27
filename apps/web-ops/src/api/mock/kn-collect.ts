// ============================================================================
// BACKEND GIẢ LẬP thu thập dữ liệu Cấp Xã KN v7.7 (P7, application.yaml /v1/kn/*): HTX + nông hộ (FN-XA-03.03),
// máy móc theo HTX (FN-XA-03.04), thửa (FN-XA-03.05, danh sách), vật tư (FN-XA-03.06). Quy tắc như Go P7 ở mức màn web
// cần: phạm vi theo ngữ cảnh (Cấp Xã: xã phụ trách; Cấp Tỉnh/TW: chỉ xem), SĐT che, vô hiệu hóa thay xóa (lý do ≥ 20),
// số máy nguyên 1–1.000.000 (#113), năm SX ở hồ sơ KN riêng 1900–năm hiện tại VN (#119), trùng loại bị chặn,
// vật tư sửa = bản ghi bù có lý do.
// ============================================================================
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { KN_COOPS } from "./kn-schedules";
import { callerOf, isErr, type Caller } from "./kn-reports/store";
import { vnDate } from "@/kn/schedule/week";

interface Coop {
  ref: string;
  code: string;
  name: string;
  commune_code: string;
  area_ha: string | null;
  representative: { label: string; masked_phone: string | null } | null;
  active: boolean;
  status_reason: string | null;
  tax_code: string | null;
  head_office: string | null;
  updated_at: string;
}
interface Household {
  ref: string;
  coop: string;
  holder: { label: string; masked_phone: string | null };
  cultivated_area_ha: string | null;
  active: boolean;
  since_date: string | null;
}
interface Line {
  machine_type: string;
  machine_type_name: string;
  quantity: number;
  note: string | null;
  updated_at: string;
  updated_by: string;
}
interface Material {
  id: string;
  commune_code: string;
  recorded_on: string;
  material_code: string;
  material_name: string;
  quantity: string;
  unit: string;
  note: string | null;
  department_code: string;
  actor_ref: string;
  actor_display: string | null;
  recorded_at: string;
  supersedes_id: string | null;
  correction_reason: string | null;
  current: boolean;
}

export const MACHINE_TYPES: Record<string, string> = {
  may_cay: "Máy cày",
  may_cay_xoi: "Máy xới",
  may_gieo_sa: "Máy gieo sạ",
  may_cay_lua: "Máy cấy",
  drone: "Thiết bị bay không người lái",
  may_gat: "Máy gặt đập liên hợp",
  may_say: "Máy sấy",
};
export const MATERIALS: Record<string, { name: string; unit: string; countable: boolean }> = {
  phan_ure: { name: "Phân urê", unit: "tấn", countable: false },
  phan_npk: { name: "Phân NPK", unit: "tấn", countable: false },
  thuoc_bvtv: { name: "Thuốc BVTV", unit: "lít", countable: false },
  bay_den: { name: "Bẫy đèn", unit: "cái", countable: true },
};

const now = () => new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
/** Hôm nay theo lịch VN (now() là chuỗi UTC "Z") — KN-VN-DATE #149. */
const todayVN = () => vnDate(Date.now());
const mask = (p: string) => `${p.slice(0, 4)}•••${p.slice(-3)}`;
const shortName = (full: string) => {
  const parts = full.trim().split(/\s+/);
  return parts.length < 2 ? full : `${parts[0]} ${parts.slice(1, -1).map((x) => `${x[0]}.`).join(" ")} ${parts.at(-1)![0]}.`.replace(/\s+/g, " ");
};

let COOPS: Coop[] = [];
let HH: Household[] = [];
let MACH = new Map<string, Line[]>();
/** Hồ sơ KN riêng #119: năm sản xuất theo loại máy của HTX (cooperative_ref → loại → năm). */
let YEARS = new Map<string, Map<string, { year_made: number; updated_at: string; actor_ref: string }>>();
let MATS: Material[] = [];
let seeded = false;

function seed(): void {
  if (seeded) return;
  seeded = true;
  COOPS = KN_COOPS.map((c, i) => ({
    ref: c.ref,
    code: c.code,
    name: c.name,
    commune_code: c.admin_unit_code ?? "",
    area_ha: String(120 + i * 35),
    representative: { label: ["Nguyễn V. B.", "Trần T. C.", "Lê V. D.", "Phạm T. E.", "Võ V. G.", "Đặng T. H.", "Huỳnh V. K."][i % 7]!, masked_phone: `0913•••${String(100 + i).slice(-3)}` },
    active: true,
    status_reason: null,
    tax_code: null,
    head_office: null,
    updated_at: "2026-08-20T01:30:00Z",
  }));
  HH = COOPS.flatMap((c, i) =>
    Array.from({ length: 2 + (i % 2) }, (_, k) => ({ ref: uuidv7(), coop: c.ref, holder: { label: `Hộ ${k + 1} ${c.name.replace(/^HTX /, "")}`, masked_phone: `0987•••${String(10 * i + k).padStart(3, "0")}` }, cultivated_area_ha: String(1.5 + k), active: true, since_date: "2025-03-01" })),
  );
  MACH = new Map(COOPS.slice(0, 3).map((c, i) => [c.ref, [{ machine_type: "may_gat", machine_type_name: MACHINE_TYPES.may_gat!, quantity: 2 + i, note: null, updated_at: "2026-08-25T02:00:00Z", updated_by: "00000000-0000-7000-8000-00000000c004" }]]));
  YEARS = new Map(COOPS.slice(0, 2).map((c, i) => [c.ref, new Map([["may_gat", { year_made: 2018 + i, updated_at: "2026-08-25T02:00:00Z", actor_ref: "00000000-0000-7000-8000-00000000c004" }]])]));
  MATS = [];
}

/** Cho kiểm thử. */
export function resetKnCollect(): void {
  seeded = false;
  seed();
}

const communesOf = (c: Caller): string[] | null => (c.role === "kn_xa" ? c.communes : null);
const inScope = (c: Caller, commune: string) => {
  const s = communesOf(c);
  return !s || s.includes(commune);
};
const canWrite = (c: Caller, code: string) => (c.role === "kn_xa" || c.role === "admin" ? null : problem(403, code, "Cấp Tỉnh, Cấp Quốc gia chỉ xem"));

function wrap(h: (ctx: Ctx, c: Caller) => Response) {
  return (ctx: Ctx): Response => {
    seed();
    const c = callerOf(ctx.request);
    return isErr(c) ? c : h(ctx, c);
  };
}
const hhWire = (h: Household) => ({ ref: h.ref, holder: h.holder, cultivated_area_ha: h.cultivated_area_ha, active: h.active, since_date: h.since_date });
function matWire(m: Material) {
  const out: Partial<Material> = { ...m };
  delete out.current;
  return out;
}
const coopWire = (x: Coop) => ({ ...x, household_count: HH.filter((h) => h.coop === x.ref && h.active).length });
const findCoop = (c: Caller, ref: string | undefined) => COOPS.find((x) => x.ref === ref && inScope(c, x.commune_code));
const notFound = () => problem(404, "party.org_not_found", "Không tìm thấy HTX");
const fieldErr = (field: string, reason: string) => problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors: [{ field, reason }] });
const UUID = "([0-9a-f-]{36})";
const re = (s: string) => new RegExp(`^${s.replaceAll("{id}", UUID)}$`);
const profileWire = (x: Coop, c: Caller) => ({
  cooperative_ref: x.ref,
  commune_code: x.commune_code,
  department_code: c.dept ?? "trong_trot",
  items: [...(YEARS.get(x.ref) ?? new Map()).entries()].map(([t, v]) => ({ machine_type: t, machine_type_name: MACHINE_TYPES[t] ?? t, ...v })),
});
const coopLines = (x: Coop) => {
  const lines = MACH.get(x.ref) ?? [];
  return { cooperative_ref: x.ref, name: x.name, commune_code: x.commune_code, lines, total: lines.reduce((s, l) => s + l.quantity, 0) };
};

export const knCollectRoutes: Route[] = [
  {
    method: "GET",
    pattern: re("/v1/kn/cooperatives"),
    handler: wrap(({ url }, c) => {
      const commune = url.searchParams.get("commune");
      if (commune && !inScope(c, commune)) return problem(403, "party.out_of_scope", "Ngoài phạm vi phụ trách");
      const st = url.searchParams.get("status") ?? "active";
      const q = (url.searchParams.get("q") ?? "").toLowerCase();
      const items = COOPS.filter((x) => inScope(c, x.commune_code) && (!commune || x.commune_code === commune) && (st === "all" || (st === "active") === x.active) && (!q || `${x.name} ${x.code}`.toLowerCase().includes(q)));
      return json({ items: items.map(coopWire), next_cursor: null });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/kn/cooperatives"),
    handler: wrap(({ body }, c) => {
      const deny = canWrite(c, "party.read_only");
      if (deny) return deny;
      const b = (body ?? {}) as { name?: string; commune_code?: string; area_ha?: string | number | null; representative?: { full_name: string; phone: string } };
      const name = String(b.name ?? "").trim();
      if (!name) return fieldErr("name", "bắt buộc");
      if (!b.commune_code || !inScope(c, b.commune_code)) return problem(403, "party.out_of_scope", "Ngoài phạm vi phụ trách");
      if (COOPS.some((x) => x.name.toLowerCase() === name.toLowerCase())) return problem(409, "party.cooperative_name_taken", "Tên HTX đã tồn tại");
      const area = b.area_ha === undefined || b.area_ha === null || b.area_ha === "" ? null : String(b.area_ha);
      if (area !== null && !/^\d+(\.\d{1,4})?$/.test(area)) return fieldErr("area_ha", "decimal ≥ 0, tối đa 4 chữ số lẻ");
      const x: Coop = {
        ref: uuidv7(),
        code: `HTX-AG-${String(COOPS.length + 1).padStart(3, "0")}`,
        name,
        commune_code: b.commune_code,
        area_ha: area,
        representative: b.representative ? { label: shortName(b.representative.full_name), masked_phone: mask(b.representative.phone.replace(/\D/g, "")) } : null,
        active: true,
        status_reason: null,
        tax_code: null,
        head_office: null,
        updated_at: now(),
      };
      COOPS.push(x);
      return json(coopWire(x), 201);
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/cooperatives/{id}"),
    handler: wrap(({ params }, c) => {
      const x = findCoop(c, params[0]);
      return x ? json(coopWire(x)) : notFound();
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/kn/cooperatives/{id}/deactivate"),
    handler: wrap(({ params, body }, c) => {
      const deny = canWrite(c, "party.read_only");
      if (deny) return deny;
      const x = findCoop(c, params[0]);
      if (!x) return notFound();
      const r = String((body as { reason?: string } | undefined)?.reason ?? "").trim();
      if ([...r].length < 20) return fieldErr("reason", "bắt buộc, ít nhất 20 ký tự");
      if (!x.active) return problem(409, "party.org_state_conflict", "HTX đã vô hiệu hóa");
      Object.assign(x, { active: false, status_reason: r, updated_at: now() });
      return json({ cooperative: coopWire(x), warnings: { households: HH.filter((h) => h.coop === x.ref && h.active).length, plots: 0, machines: coopLines(x).total } });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/cooperatives/{id}/households"),
    handler: wrap(({ params }, c) => {
      const x = findCoop(c, params[0]);
      return x ? json({ items: HH.filter((h) => h.coop === x.ref).map(hhWire), next_cursor: null }) : notFound();
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/kn/cooperatives/{id}/households"),
    handler: wrap(({ params, body }, c) => {
      const deny = canWrite(c, "party.read_only");
      if (deny) return deny;
      const x = findCoop(c, params[0]);
      if (!x) return notFound();
      if (!x.active) return problem(409, "party.org_state_conflict", "HTX đã vô hiệu hóa");
      const b = (body ?? {}) as { holder?: { full_name?: string; phone?: string }; cultivated_area_ha?: string };
      const phone = String(b.holder?.phone ?? "").replace(/\D/g, "");
      if (!b.holder?.full_name || !/^0\d{9}$/.test(phone)) return fieldErr("holder", "họ tên và SĐT 10 chữ số bắt buộc");
      // BF-SWEEP-TT: diện tích canh tác như area_ha của HTX — decimal ≥ 0, tối đa 4 chữ số lẻ (R-A1, máy chủ cũng chặn).
      if (b.cultivated_area_ha !== undefined && b.cultivated_area_ha !== null && b.cultivated_area_ha !== "" && !/^\d+(\.\d{1,4})?$/.test(String(b.cultivated_area_ha))) return fieldErr("cultivated_area_ha", "decimal ≥ 0, tối đa 4 chữ số lẻ");
      const h: Household = { ref: uuidv7(), coop: x.ref, holder: { label: shortName(b.holder.full_name), masked_phone: mask(phone) }, cultivated_area_ha: b.cultivated_area_ha ?? null, active: true, since_date: todayVN() };
      HH.push(h);
      return json(hhWire(h), 201);
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/machine-profiles/{id}"),
    handler: wrap(({ params }, c) => {
      const x = findCoop(c, params[0]);
      return x ? json(profileWire(x, c)) : problem(404, "extension.kn_attrs_cooperative_not_found", "Không tìm thấy HTX");
    }),
  },
  {
    method: "PUT",
    pattern: re("/v1/kn/machine-profiles/{id}"),
    handler: wrap(({ params, body }, c) => {
      const deny = canWrite(c, "extension.read_only");
      if (deny) return deny;
      const x = findCoop(c, params[0]);
      if (!x) return problem(404, "extension.kn_attrs_cooperative_not_found", "Không tìm thấy HTX");
      if (!x.active) return fieldErr("cooperative_ref", "HTX đã ngừng hoạt động");
      const lines = ((body as { lines?: unknown[] } | undefined)?.lines ?? []) as { machine_type?: string; year_made?: unknown }[];
      const maxYear = Number(todayVN().slice(0, 4));
      const seen = new Set<string>();
      for (const l of lines) {
        const t = String(l.machine_type ?? "");
        if (!MACHINE_TYPES[t]) return problem(422, "extension.kn_attrs_machine_type_unknown", "Loại máy không có trong danh mục", { errors: [{ field: `lines.${t}.machine_type`, reason: "không có hoặc đã ngừng dùng trong danh mục" }] });
        if (seen.has(t)) return fieldErr(`lines.${t}.machine_type`, "loại máy trùng trong hồ sơ");
        seen.add(t);
        if (l.year_made === undefined || l.year_made === null) return fieldErr(`lines.${t}.year_made`, "bắt buộc");
        const y = l.year_made;
        if (typeof y !== "number" || !Number.isInteger(y) || y < 1900 || y > maxYear)
          return problem(422, "extension.kn_attrs_year_invalid", "Năm sản xuất không hợp lệ", { errors: [{ field: `lines.${t}.year_made`, reason: `năm sản xuất phải là số nguyên từ 1900 đến ${maxYear}` }] });
      }
      const prev = YEARS.get(x.ref) ?? new Map();
      YEARS.set(x.ref, new Map(lines.map((l) => {
        const old = prev.get(l.machine_type!);
        return [l.machine_type!, old && old.year_made === l.year_made ? old : { year_made: l.year_made as number, updated_at: now(), actor_ref: c.actor }];
      })));
      return json(profileWire(x, c));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/machine-counts"),
    handler: wrap(({ url }, c) => {
      const commune = url.searchParams.get("commune");
      if (commune && !inScope(c, commune)) return problem(403, "mechanization.out_of_scope", "Ngoài phạm vi phụ trách");
      const coops = COOPS.filter((x) => inScope(c, x.commune_code) && (!commune || x.commune_code === commune));
      const totals = new Map<string, { commune_code: string; machine_type: string; machine_type_name: string; quantity: number }>();
      for (const x of coops)
        for (const l of MACH.get(x.ref) ?? []) {
          const k = `${x.commune_code}|${l.machine_type}`;
          const t = totals.get(k) ?? { commune_code: x.commune_code, machine_type: l.machine_type, machine_type_name: l.machine_type_name, quantity: 0 };
          t.quantity += l.quantity;
          totals.set(k, t);
        }
      return json({ cooperatives: coops.filter((x) => (MACH.get(x.ref) ?? []).length).map(coopLines), commune_totals: [...totals.values()] });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/machine-counts/{id}"),
    handler: wrap(({ params }, c) => {
      const x = findCoop(c, params[0]);
      return x ? json(coopLines(x)) : problem(404, "mechanization.cooperative_not_found", "Không tìm thấy HTX");
    }),
  },
  {
    method: "PUT",
    pattern: re("/v1/kn/machine-counts/{id}"),
    handler: wrap(({ params, body }, c) => {
      const deny = canWrite(c, "mechanization.read_only");
      if (deny) return deny;
      const x = findCoop(c, params[0]);
      if (!x) return problem(404, "mechanization.cooperative_not_found", "Không tìm thấy HTX");
      if (!x.active) return problem(422, "mechanization.cooperative_invalid", "HTX ngừng hoạt động");
      const lines = ((body as { lines?: unknown[] } | undefined)?.lines ?? []) as { machine_type: string; quantity: unknown; note?: string | null }[];
      const seen = new Set<string>();
      for (const [i, l] of lines.entries()) {
        if (!MACHINE_TYPES[l.machine_type]) return problem(422, "mechanization.machine_type_unknown", "Loại máy không có trong danh mục");
        // #113 BF-SRV-GUARD: số 0 không còn là khai báo hợp lệ — bỏ loại thì bỏ dòng.
        if (typeof l.quantity !== "number" || !Number.isInteger(l.quantity) || l.quantity < 1 || l.quantity > 1_000_000)
          return problem(422, "mechanization.machine_quantity_invalid", "Số lượng phải là số nguyên 1–1.000.000", { errors: [{ field: `lines[${i}].quantity`, reason: "số nguyên 1–1.000.000" }] });
        if (seen.has(l.machine_type)) return problem(422, "mechanization.machine_type_duplicate", "Trùng loại máy — cập nhật dòng đã có");
        seen.add(l.machine_type);
      }
      MACH.set(x.ref, lines.map((l) => ({ machine_type: l.machine_type, machine_type_name: MACHINE_TYPES[l.machine_type]!, quantity: l.quantity as number, note: l.note ?? null, updated_at: now(), updated_by: c.actor })));
      return json(coopLines(x));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/plots"),
    handler: wrap(({ url }, c) => {
      const commune = url.searchParams.get("commune");
      if (commune && !inScope(c, commune)) return problem(403, "geo.out_of_scope", "Ngoài phạm vi phụ trách");
      // Thửa v7.7 vẽ ở field-app (P7); bản giả lập web khởi đầu một thửa mẫu mỗi HTX của xã phụ trách.
      const items = COOPS.filter((x) => inScope(c, x.commune_code) && (!commune || x.commune_code === commune) && (!url.searchParams.get("cooperative_ref") || x.ref === url.searchParams.get("cooperative_ref"))).map((x, i) => ({
        ref: `01927b3e-0000-7000-8000-0000000d${String(i + 1).padStart(4, "0")}`,
        code: `TH-${x.code}-01`,
        lot_name: `Lô 1 ${x.name.replace(/^HTX /, "")}`,
        commune_code: x.commune_code,
        cooperative_ref: x.ref,
        farmer: { label: "Nguyễn V. A." },
        note: null,
        area_ha: "2.15",
        declared_area_ha: "2.2",
        area_deviation_pct: "-2.3",
        shape: "polygon",
        department_code: "trong_trot",
        status: "active",
        needs_review: false,
        version: 1,
        boundary_id: `01927b3e-0000-7000-8000-0000000e${String(i + 1).padStart(4, "0")}`,
        updated_at: "2026-09-10T02:00:00Z",
      }));
      return json({ items, next_cursor: null });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/materials"),
    handler: wrap(({ url }, c) => {
      const commune = url.searchParams.get("commune");
      if (commune && !inScope(c, commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      return json({ items: MATS.filter((m) => m.current && inScope(c, m.commune_code) && (!commune || m.commune_code === commune)).map(matWire).reverse(), next_cursor: null });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/kn/materials"),
    handler: wrap(({ body }, c) => {
      const deny = canWrite(c, "extension.read_only");
      if (deny) return deny;
      const b = (body ?? {}) as { commune_code?: string; recorded_on?: string; lines?: { material_code: string; quantity: string; note?: string }[] };
      if (!b.commune_code || !inScope(c, b.commune_code)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      if (!b.recorded_on || b.recorded_on > todayVN()) return fieldErr("recorded_on", "bắt buộc, không ở tương lai");
      const lines = b.lines ?? [];
      if (!lines.length || lines.length > 200) return fieldErr("lines", "1–200 dòng");
      const out: Material[] = [];
      for (const l of lines) {
        const m = MATERIALS[l.material_code];
        if (!m) return problem(422, "extension.material_unknown", "Vật tư không có trong danh mục");
        if (!(m.countable ? /^\d+$/ : /^\d+(\.\d{1,4})?$/).test(String(l.quantity))) return problem(422, "extension.material_quantity_invalid", m.countable ? "ĐVT đếm được → số nguyên" : "Số ≥ 0, tối đa 4 chữ số lẻ");
        out.push({ id: uuidv7(), commune_code: b.commune_code, recorded_on: b.recorded_on, material_code: l.material_code, material_name: m.name, quantity: String(l.quantity), unit: m.unit, note: l.note ?? null, department_code: c.dept ?? "trong_trot", actor_ref: c.actor, actor_display: c.display, recorded_at: now(), supersedes_id: null, correction_reason: null, current: true });
      }
      MATS.push(...out);
      return json({ items: out.map(matWire) }, 201);
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/kn/materials/summary"),
    handler: wrap(({ url }, c) => {
      const commune = url.searchParams.get("commune");
      const agg = new Map<string, { commune_code: string; material_code: string; material_name: string; unit: string; quantity: number; records: number }>();
      for (const m of MATS.filter((x) => x.current && inScope(c, x.commune_code) && (!commune || x.commune_code === commune))) {
        const k = `${m.commune_code}|${m.material_code}`;
        const a = agg.get(k) ?? { commune_code: m.commune_code, material_code: m.material_code, material_name: m.material_name, unit: m.unit, quantity: 0, records: 0 };
        a.quantity += Number(m.quantity);
        a.records += 1;
        agg.set(k, a);
      }
      return json({ items: [...agg.values()].map((a) => ({ ...a, quantity: String(Math.round(a.quantity * 10000) / 10000) })) });
    }),
  },
];
