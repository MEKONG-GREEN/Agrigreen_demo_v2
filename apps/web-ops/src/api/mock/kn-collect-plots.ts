// ============================================================================
// BACKEND GIẢ LẬP thửa KN (FN-XA-03.05, TT-11 #104): /v1/kn/plots (pf/cooperative) + /v1/kn/plot-profiles (hồ sơ KN
// riêng #119). Quy tắc như Go ở mức màn web cần: Tên thửa, Chủ hộ, Xã bắt buộc; diện tích tính từ hình; mọi chồng lấn bị
// chặn (409 geo.plot_overlap — giả lập so khung bao); sửa ranh giới = phiên bản mới thay supersedes_boundary_id, lý do
// ≥ 10 ký tự; cây trồng bắt buộc, thuộc danh mục crop đang dùng, thửa ngừng dùng → 409.
// ============================================================================
import { m2ToHa, polygonAreaM2, type LngLat } from "@/kn/cycle/plotGeometry";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, type Ctx, type Route } from "./http";
import type { Caller } from "./kn-reports/store";

type Polygon = { type: "Polygon"; coordinates: LngLat[][] };
interface Plot {
  ref: string;
  code: string;
  lot_name: string | null;
  commune_code: string;
  cooperative_ref: string | null;
  farmer_label: string | null;
  note: string | null;
  area_ha: string;
  shape: "polygon" | "circle" | "square";
  department_code: string;
  status: "draft" | "active" | "retired";
  version: number;
  boundary_id: string;
  updated_at: string;
  geometry: Polygon;
  log: { id: string; event: string; actor_ref: string; actor_display: string | null; at: string; changes: { field: string; old: unknown; new: unknown }[]; reason: string | null }[];
}
interface Crop {
  crop_code: string;
  crop_name: string;
  updated_at: string;
  actor_ref: string;
}
/** HTX tối thiểu thửa cần (lấy từ sổ HTX giả lập của kn-collect). */
export interface PlotCoop {
  ref: string;
  code: string;
  name: string;
  commune_code: string;
}
type Wrap = (h: (ctx: Ctx, c: Caller) => Response) => (ctx: Ctx) => Response;

const now = () => new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
const UUID = "([0-9a-f-]{36})";
const re = (s: string) => new RegExp(`^${s.replaceAll("{id}", UUID)}$`);
const fieldErr = (field: string, reason: string) => problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors: [{ field, reason }] });
const shortName = (full: string) => {
  const parts = full.trim().split(/\s+/);
  return parts.length < 2 ? full : `${parts[0]} ${parts.slice(1, -1).map((x) => `${x[0]}.`).join(" ")} ${parts.at(-1)![0]}.`.replace(/\s+/g, " ");
};

let PLOTS: Plot[] = [];
let CROPS = new Map<string, Crop>();
let seededFor: string | null = null;

const square = (lng: number, lat: number, d = 0.0012): Polygon => ({ type: "Polygon", coordinates: [[[lng, lat], [lng + d, lat], [lng + d, lat + d], [lng, lat + d], [lng, lat]]] });
const areaHa = (g: Polygon) => m2ToHa(polygonAreaM2(g.coordinates));

function seed(coops: PlotCoop[]): void {
  const key = coops.map((c) => c.ref).join(",");
  if (seededFor === key) return;
  seededFor = key;
  // Như bản giả lập cũ: một thửa mẫu mỗi HTX (cùng ref), thêm hình để sửa/vẽ lại; nửa đầu đã có cây trồng.
  PLOTS = coops.map((x, i) => {
    const geometry = square(105.4 + i * 0.01, 10.38);
    return {
      ref: `01927b3e-0000-7000-8000-0000000d${String(i + 1).padStart(4, "0")}`,
      code: `TH-${x.code}-01`,
      lot_name: `Lô 1 ${x.name.replace(/^HTX /, "")}`,
      commune_code: x.commune_code,
      cooperative_ref: x.ref,
      farmer_label: "Nguyễn V. A.",
      note: null,
      area_ha: areaHa(geometry).toFixed(4),
      shape: "polygon",
      department_code: "trong_trot",
      status: "active",
      version: 1,
      boundary_id: `01927b3e-0000-7000-8000-0000000e${String(i + 1).padStart(4, "0")}`,
      updated_at: "2026-09-10T02:00:00Z",
      geometry,
      log: [{ id: uuidv7(), event: "created", actor_ref: "00000000-0000-7000-8000-00000000c004", actor_display: "Cán bộ xã", at: "2026-09-10T02:00:00Z", changes: [], reason: null }],
    };
  });
  const crop = CATALOGS.crop.find((c) => c.active && c.attrs.is_group !== "true");
  CROPS = new Map(crop ? PLOTS.slice(0, Math.ceil(PLOTS.length / 2)).map((p) => [p.ref, { crop_code: crop.code, crop_name: crop.name, updated_at: "2026-09-10T02:00:00Z", actor_ref: "00000000-0000-7000-8000-00000000c004" }]) : []);
}

/** Cho kiểm thử. */
export function resetKnPlots(): void {
  seededFor = null;
}

const inScope = (c: Caller, commune: string) => c.role !== "kn_xa" || c.communes.includes(commune);
const canWrite = (c: Caller) => (c.role === "kn_xa" || c.role === "admin" ? null : problem(403, "geo.read_only", "Cấp Tỉnh, Cấp Quốc gia chỉ xem"));
const find = (c: Caller, ref: string | undefined) => PLOTS.find((p) => p.ref === ref && inScope(c, p.commune_code));
const notFound = () => problem(404, "geo.plot_not_found", "Không tìm thấy thửa, hoặc thửa nằm ngoài phạm vi.");
const wire = (p: Plot) => ({
  ref: p.ref,
  code: p.code,
  lot_name: p.lot_name,
  commune_code: p.commune_code,
  cooperative_ref: p.cooperative_ref,
  farmer: p.farmer_label ? { label: p.farmer_label } : null,
  note: p.note,
  area_ha: p.area_ha,
  declared_area_ha: null,
  area_deviation_pct: null,
  shape: p.shape,
  department_code: p.department_code,
  status: p.status,
  needs_review: false,
  version: p.version,
  boundary_id: p.boundary_id,
  updated_at: p.updated_at,
});
const profileWire = (p: Plot) => {
  const c = CROPS.get(p.ref);
  return { plot_ref: p.ref, commune_code: p.commune_code, department_code: p.department_code, status: p.status, crop_code: c?.crop_code ?? null, crop_name: c?.crop_name ?? null, updated_at: c?.updated_at ?? null, actor_ref: c?.actor_ref ?? null };
};

function bbox(g: Polygon): [number, number, number, number] {
  const xs = g.coordinates[0]!.map((c) => c[0]);
  const ys = g.coordinates[0]!.map((c) => c[1]);
  return [Math.min(...xs), Math.min(...ys), Math.max(...xs), Math.max(...ys)];
}

/** Kiểm hình gửi lên; lỗi → Response 422. Hợp lệ → hình đã khép vòng. */
function checkGeometry(raw: unknown): Polygon | Response {
  const g = raw as { type?: string; coordinates?: unknown } | undefined;
  if (!g || g.type !== "Polygon" || !Array.isArray(g.coordinates) || !Array.isArray(g.coordinates[0])) return problem(422, "geo.invalid_geometry", "Hình vẽ không hợp lệ.");
  const ring = g.coordinates[0] as LngLat[];
  if (new Set(ring.map((c) => `${c[0]},${c[1]}`)).size < 3) return problem(422, "geo.too_few_points", "Hình vẽ chưa đủ điểm phân biệt hoặc các điểm thẳng hàng.");
  const poly: Polygon = { type: "Polygon", coordinates: [ring] };
  const ha = areaHa(poly);
  if (ha <= 0) return problem(422, "geo.too_few_points", "Hình vẽ chưa đủ điểm phân biệt hoặc các điểm thẳng hàng.");
  if (ha < 0.001 || ha > 1000) return problem(422, "geo.area_out_of_range", "Diện tích ngoài khoảng cho phép.");
  return poly;
}

/** Chồng lấn (giả lập: khung bao giao nhau có diện tích) với thửa còn dùng khác → 409 như Go KN trực tuyến. */
function overlapOf(g: Polygon, except?: string): Response | null {
  const [a0, a1, a2, a3] = bbox(g);
  const hits = PLOTS.filter((p) => p.ref !== except && p.status !== "retired").filter((p) => {
    const [b0, b1, b2, b3] = bbox(p.geometry);
    return a0 < b2 && b0 < a2 && a1 < b3 && b1 < a3;
  });
  if (!hits.length) return null;
  return problem(409, "geo.plot_overlap", "Ranh giới bị chồng lấn với lô đã đăng ký — không lưu được, vui lòng vẽ lại.", {
    overlaps: hits.map((p) => ({ plot_ref: p.ref, code: p.code, area_ha: p.area_ha, overlap_pct: "100", geometry: p.geometry })),
  });
}

export function knPlotRoutes(getCoops: () => PlotCoop[], wrap: Wrap): Route[] {
  const w = (h: (ctx: Ctx, c: Caller) => Response) =>
    wrap((ctx, c) => {
      seed(getCoops());
      return h(ctx, c);
    });
  return [
    {
      method: "GET",
      pattern: re("/v1/kn/plots"),
      handler: w(({ url }, c) => {
        const commune = url.searchParams.get("commune");
        if (commune && !inScope(c, commune)) return problem(403, "geo.out_of_scope", "Ngoài phạm vi phụ trách");
        const coop = url.searchParams.get("cooperative_ref");
        return json({ items: PLOTS.filter((p) => inScope(c, p.commune_code) && (!commune || p.commune_code === commune) && (!coop || p.cooperative_ref === coop)).map(wire), next_cursor: null });
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/plots/export"),
      handler: w(({ url }, c) => {
        const commune = url.searchParams.get("commune") ?? "";
        if (!commune || !inScope(c, commune)) return problem(403, "geo.out_of_scope", "Ngoài phạm vi phụ trách");
        const features = PLOTS.filter((p) => p.commune_code === commune && p.status !== "retired").map((p) => ({
          type: "Feature",
          geometry: p.geometry,
          properties: { ref: p.ref, code: p.code, lot_name: p.lot_name, commune_code: p.commune_code, cooperative_ref: p.cooperative_ref, farmer_label: p.farmer_label, note: p.note, area_ha: p.area_ha, declared_area_ha: null, shape: p.shape, status: p.status },
        }));
        return json({ type: "FeatureCollection", features }, 200, { "Content-Type": "application/geo+json" });
      }),
    },
    {
      method: "POST",
      pattern: re("/v1/kn/plots"),
      handler: w(({ body }, c) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const b = (body ?? {}) as { commune_code?: string; cooperative_ref?: string | null; farmer_name?: string; lot_name?: string; note?: string; shape?: Plot["shape"]; geometry?: unknown };
        if (!b.commune_code) return fieldErr("commune_code", "bắt buộc");
        if (!inScope(c, b.commune_code)) return problem(403, "geo.out_of_scope", "Ngoài phạm vi phụ trách");
        if (!String(b.lot_name ?? "").trim()) return fieldErr("lot_name", "bắt buộc");
        if (!String(b.farmer_name ?? "").trim()) return fieldErr("farmer_name", "bắt buộc");
        const g = checkGeometry(b.geometry);
        if (g instanceof Response) return g;
        const clash = overlapOf(g);
        if (clash) return clash;
        const at = now();
        const p: Plot = {
          ref: uuidv7(),
          code: `TH-${b.commune_code}-${String(PLOTS.length + 1).padStart(3, "0")}`,
          lot_name: String(b.lot_name).trim(),
          commune_code: b.commune_code,
          cooperative_ref: b.cooperative_ref ?? null,
          farmer_label: shortName(String(b.farmer_name)),
          note: b.note?.trim() || null,
          area_ha: areaHa(g).toFixed(4),
          shape: b.shape ?? "polygon",
          department_code: c.dept ?? "trong_trot",
          status: "active",
          version: 1,
          boundary_id: uuidv7(),
          updated_at: at,
          geometry: g,
          log: [{ id: uuidv7(), event: "created", actor_ref: c.actor, actor_display: c.display, at, changes: [], reason: null }],
        };
        PLOTS.push(p);
        return json({ ...wire(p), geometry: p.geometry, boundary: { id: p.boundary_id, version: 1 } }, 201);
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/plots/{id}"),
      handler: w(({ params }, c) => {
        const p = find(c, params[0]);
        return p ? json({ ...wire(p), geometry: p.geometry }) : notFound();
      }),
    },
    {
      method: "PUT",
      pattern: re("/v1/kn/plots/{id}"),
      handler: w(({ params, body }, c) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const p = find(c, params[0]);
        if (!p) return notFound();
        if (p.status === "retired") return problem(409, "geo.plot_state_conflict", "Thửa đã ngừng dùng");
        const b = (body ?? {}) as { lot_name?: string; note?: string | null; farmer_name?: string | null; cooperative_ref?: string | null; reason?: string };
        if (b.lot_name !== undefined && !b.lot_name.trim()) return fieldErr("lot_name", "bắt buộc");
        // #113: gửi thay đổi chủ hộ thì tên không được rỗng.
        if (b.farmer_name !== undefined && !String(b.farmer_name ?? "").trim()) return fieldErr("farmer_name", "Tên chủ hộ không được để trống");
        const changes: { field: string; old: unknown; new: unknown }[] = [];
        const apply = <K extends "lot_name" | "note" | "cooperative_ref" | "farmer_label">(k: K, v: Plot[K], field: string = k) => {
          if (p[k] !== v) changes.push({ field, old: p[k], new: v });
          p[k] = v;
        };
        if (b.lot_name !== undefined) apply("lot_name", b.lot_name.trim());
        if (b.note !== undefined) apply("note", b.note?.trim() || null);
        if (b.cooperative_ref !== undefined) apply("cooperative_ref", b.cooperative_ref);
        if (b.farmer_name) apply("farmer_label", shortName(b.farmer_name), "farmer");
        if (changes.length) {
          p.updated_at = now();
          p.log.unshift({ id: uuidv7(), event: "attributes_changed", actor_ref: c.actor, actor_display: c.display, at: p.updated_at, changes, reason: b.reason ?? null });
        }
        return json({ ...wire(p), geometry: p.geometry });
      }),
    },
    {
      method: "POST",
      pattern: re("/v1/kn/plots/{id}/boundary"),
      handler: w(({ params, body }, c) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const p = find(c, params[0]);
        if (!p) return notFound();
        if (p.status === "retired") return problem(409, "geo.plot_state_conflict", "Thửa đã ngừng dùng");
        const b = (body ?? {}) as { supersedes_boundary_id?: string; geometry?: unknown; shape?: Plot["shape"]; reason?: string };
        if (String(b.reason ?? "").trim().length < 10) return fieldErr("reason", "tối thiểu 10 ký tự");
        if (b.supersedes_boundary_id !== p.boundary_id) return problem(409, "geo.version_conflict", "Ranh giới đã được người khác cập nhật.", { current_boundary_id: p.boundary_id });
        const g = checkGeometry(b.geometry);
        if (g instanceof Response) return g;
        const clash = overlapOf(g, p.ref);
        if (clash) return clash;
        const at = now();
        p.log.unshift({ id: uuidv7(), event: "boundary_corrected", actor_ref: c.actor, actor_display: c.display, at, changes: [{ field: "area_ha", old: p.area_ha, new: areaHa(g).toFixed(4) }], reason: String(b.reason).trim() });
        Object.assign(p, { geometry: g, area_ha: areaHa(g).toFixed(4), shape: b.shape ?? "polygon", version: p.version + 1, boundary_id: uuidv7(), updated_at: at });
        return json({ ...wire(p), geometry: p.geometry, boundary: { id: p.boundary_id, version: p.version } }, 201);
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/plots/{id}/log"),
      handler: w(({ params }, c) => {
        const p = find(c, params[0]);
        return p ? json({ items: p.log, next_cursor: null }) : notFound();
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/plot-profiles"),
      handler: w(({ url }, c) => {
        const commune = url.searchParams.get("commune");
        if (commune && !inScope(c, commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
        const items = PLOTS.filter((p) => inScope(c, p.commune_code) && (!commune || p.commune_code === commune) && CROPS.has(p.ref)).map((p) => ({ ...profileWire(p) }));
        return json({ items, next_cursor: null });
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/plot-profiles/{id}"),
      handler: w(({ params }, c) => {
        const p = find(c, params[0]);
        return p ? json(profileWire(p)) : problem(404, "extension.kn_attrs_plot_not_found", "Không tìm thấy thửa");
      }),
    },
    {
      method: "PUT",
      pattern: re("/v1/kn/plot-profiles/{id}"),
      handler: w(({ params, body }, c) => {
        if (c.role !== "kn_xa" && c.role !== "admin") return problem(403, "extension.read_only", "Cấp Tỉnh, Cấp Quốc gia chỉ xem");
        const p = find(c, params[0]);
        if (!p) return problem(404, "extension.kn_attrs_plot_not_found", "Không tìm thấy thửa");
        const code = String((body as { crop_code?: string } | undefined)?.crop_code ?? "").trim();
        if (!code) return fieldErr("crop_code", "bắt buộc");
        const item = CATALOGS.crop.find((x) => x.code === code && x.active);
        if (!item) return problem(422, "extension.kn_attrs_crop_unknown", "Cây trồng không có trong danh mục", { errors: [{ field: "crop_code", reason: "không có hoặc đã ngừng dùng trong danh mục" }] });
        if (p.status === "retired") return problem(409, "extension.kn_attrs_plot_retired", "Thửa đã ngừng dùng");
        if (CROPS.get(p.ref)?.crop_code !== code) CROPS.set(p.ref, { crop_code: code, crop_name: item.name, updated_at: now(), actor_ref: c.actor });
        return json(profileWire(p));
      }),
    },
  ];
}
