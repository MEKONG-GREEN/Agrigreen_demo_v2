// GIẢ LẬP web GIS (pf/geo G2 + lớp giai đoạn pf/cropcycle) — hình dạng theo contracts/openapi/application.yaml và admin.yaml.
// Hình học là DỮ LIỆU TỔNG HỢP quanh các xã giả lập vùng ĐBSCL (lục giác, đoạn thẳng nối tâm xã) — không phải bản đồ thật.
// Trạng thái giữ trong bộ nhớ tab. Diện tích/chiều dài tính trắc địa bằng src/gis/geometry (backend thật tính trong PostGIS).
import type {
  CapacityWidget,
  CropStageCoop,
  CropStageMap,
  CropStagePlot,
  GeoCapacitySnapshot,
  GeoCapacityView,
  GeoCoopBoundaryVersion,
  GeoCoopFeature,
  GeoFacilityEvent,
  GeoFacilityFeature,
  GeoRoadFeature,
  GeoRoadSegment,
  PlotFeature,
} from "../types";
import { uuidv7 } from "../idempotency";
import { centroidOf, lineLengthM, parseBBox, polygonAreaM2, touchesBBox, type LngLat } from "@/gis/geometry";
import { COMMUNES, COOPERATIVES, hexagon, mulberry32, PROVINCES, plotsOf, type MockCooperative } from "./data";
import { hasRole, inScope, isZip, iso, json, nowIso, problem, reasonOf, validation, type Ctx, type Route } from "./http";
import type { MockPersona } from "./personas";

const MAX = 1000;
const PLOT_MIN_ZOOM = 13;
const THRESHOLDS = { warn_pct: "60", full_pct: "85" };
const GEO_WRITERS = ["geo_admin", "platform_admin"];
const BOUNDARY_WRITERS = ["plot_editor", "coop_manager", "geo_admin", "platform_admin"];

const hex = (n: number, prefix: string) => `01927b3e-${prefix}-7000-8000-${n.toString(16).padStart(12, "0")}`;
const fixed = (v: number, d: number) => v.toFixed(d);

function bboxOf(ctx: Ctx): [number, number, number, number] | Response {
  const bb = parseBBox(ctx.url.searchParams.get("bbox"));
  if (!bb) return validation([{ field: "bbox", reason: "minLon,minLat,maxLon,maxLat (WGS84)" }]);
  return bb;
}

function limitOf(ctx: Ctx, max = MAX): number {
  const n = Number(ctx.url.searchParams.get("limit") ?? max);
  return Math.min(Math.max(Number.isFinite(n) ? n : max, 1), max);
}

function ringOf(g: { coordinates: unknown }): LngLat[] {
  const c = g.coordinates as LngLat[][];
  return c[0] ?? [];
}

const areaHa = (ring: LngLat[]) => fixed(polygonAreaM2([ring]) / 10_000, 4);

// ---------------------------------------------------------------------------
// Ranh giới HTX (GIS US-BND-02/03/07)
// ---------------------------------------------------------------------------
interface CoopState {
  coop: MockCooperative;
  versions: GeoCoopBoundaryVersion[];
  status: "active" | "retired" | null;
  revision: number;
}

const COOP_STATE = new Map<string, CoopState>();
COOPERATIVES.forEach((c, i) => {
  const st: CoopState = { coop: c, versions: [], status: null, revision: 0 };
  // Mỗi HTX thứ 5 chưa có ranh giới → chỉ marker (FN-04 B2).
  if (i % 5 !== 4 && c.province_code !== "80") {
    const ring = hexagon(c.lnglat, 0.012)[0] as LngLat[];
    st.versions.push({
      boundary_id: hex(i + 1, "b0c0"),
      cooperative_ref: c.api.ref,
      version: 1,
      action: "recorded",
      area_ha: areaHa(ring),
      point_count: ring.length - 1,
      source: i % 3 === 0 ? "htx_app" : "geo_web",
      commune_code: c.api.admin_unit_code ?? "",
      event_time: iso(90 - (i % 30)),
      recorded_at: iso(90 - (i % 30)),
      supersedes_id: null,
      correction_reason: null,
      actor_ref: "mock-admin-setter",
      geometry: { type: "Polygon", coordinates: [ring] },
    });
    st.status = "active";
    st.revision = 1;
  }
  COOP_STATE.set(c.api.ref, st);
});

function coopVisible(p: MockPersona, c: MockCooperative): boolean {
  return inScope(p, c.province_code, c.api.admin_unit_code);
}

/** forLayer: ranh giới đã ngừng dùng ẩn khỏi bản đồ dùng chung (chỉ còn marker); đọc chi tiết vẫn trả hình kèm status. */
function coopFeature(st: CoopState, forLayer = false): GeoCoopFeature {
  const c = st.coop;
  const cur = st.versions[st.versions.length - 1];
  const marker = { type: "Point" as const, coordinates: [+c.lnglat[0].toFixed(6), +c.lnglat[1].toFixed(6)] };
  if (!cur || (forLayer && st.status !== "active")) {
    return {
      type: "Feature",
      id: c.api.ref,
      geometry: marker,
      properties: { cooperative_ref: c.api.ref, code: c.api.code, name: c.api.name, commune_code: c.api.admin_unit_code ?? "", status: null, marker, marker_source: "party_location" },
    };
  }
  return {
    type: "Feature",
    id: c.api.ref,
    geometry: cur.geometry ?? { type: "Polygon", coordinates: [] },
    properties: {
      cooperative_ref: c.api.ref,
      boundary_id: cur.boundary_id,
      version: cur.version,
      status: st.status === "retired" ? "retired" : "active",
      status_reason: st.status === "retired" ? cur.correction_reason : null,
      area_ha: cur.area_ha,
      source: cur.source,
      commune_code: cur.commune_code,
      revision: st.revision,
      marker,
      marker_source: c.api.location ? "party_location" : "point_on_surface",
      created_at: st.versions[0]?.recorded_at,
      updated_at: cur.recorded_at,
    },
  };
}

function coopInBBox(st: CoopState, bb: [number, number, number, number]): boolean {
  const cur = st.versions[st.versions.length - 1];
  if (cur?.geometry && st.status === "active") return touchesBBox(ringOf(cur.geometry), bb);
  return touchesBBox([st.coop.lnglat], bb);
}

function coopLayer(p: MockPersona, bb: [number, number, number, number], limit: number) {
  const all = Array.from(COOP_STATE.values()).filter((s) => coopVisible(p, s.coop) && coopInBBox(s, bb) && s.coop.api.active);
  return { type: "FeatureCollection" as const, features: all.slice(0, limit).map((s) => coopFeature(s, true)), truncated: all.length > limit, limit };
}

/** Kiểm vùng GeoJSON từ client: ≥ 4 đỉnh (3 phân biệt), vòng khép. */
function polygonInput(g: unknown): LngLat[] | null {
  const geom = g as { type?: string; coordinates?: unknown } | undefined;
  if (!geom || geom.type !== "Polygon" || !Array.isArray(geom.coordinates)) return null;
  const ring = (geom.coordinates[0] ?? []) as LngLat[];
  if (!Array.isArray(ring) || new Set(ring.map((c) => `${c[0]},${c[1]}`)).size < 3) return null;
  if (polygonAreaM2([ring]) <= 0) return null;
  return ring;
}

function lineInput(g: unknown): LngLat[] | null {
  const geom = g as { type?: string; coordinates?: unknown } | undefined;
  if (!geom || geom.type !== "LineString" || !Array.isArray(geom.coordinates)) return null;
  const pts = geom.coordinates as LngLat[];
  if (new Set(pts.map((c) => `${c[0]},${c[1]}`)).size < 2) return null;
  return pts;
}

function recordBoundary(st: CoopState, ring: LngLat[], persona: MockPersona, source: GeoCoopBoundaryVersion["source"], reason: string | null) {
  const cur = st.versions[st.versions.length - 1];
  const closed = ring[0]![0] === ring[ring.length - 1]![0] && ring[0]![1] === ring[ring.length - 1]![1] ? ring : [...ring, ring[0]!];
  const v: GeoCoopBoundaryVersion = {
    boundary_id: uuidv7(),
    cooperative_ref: st.coop.api.ref,
    version: (cur?.version ?? 0) + 1,
    action: "recorded",
    area_ha: areaHa(closed),
    point_count: closed.length - 1,
    source,
    commune_code: st.coop.api.admin_unit_code ?? "",
    event_time: nowIso(),
    recorded_at: nowIso(),
    supersedes_id: cur?.boundary_id ?? null,
    correction_reason: reason,
    actor_ref: persona.me.actor_ref,
    geometry: { type: "Polygon", coordinates: [closed] },
  };
  st.versions.push(v);
  st.status = "active";
  st.revision++;
  return v;
}

// ---------------------------------------------------------------------------
// Kho / Hub / nhà máy + sức chứa (GIS US-BND-05/06/08, US-WH-01, US-HUB-01, BR-52)
// ---------------------------------------------------------------------------
interface FacilityState {
  ref: string;
  origin: "tenant" | "mkg";
  events: GeoFacilityEvent[];
  footprint: LngLat[] | null;
  snapshots: GeoCapacitySnapshot[];
  revision: number;
}

const FACILITIES: FacilityState[] = [];

function addFacility(i: number, origin: "tenant" | "mkg", kind: "kho" | "hub" | "nha_may", name: string, at: LngLat, province: string, commune: string, cap: number, used: number | null, opts: { manual?: [number, number]; polygon?: boolean } = {}) {
  const ref = hex(i + 1, "fac0");
  const ev: GeoFacilityEvent = {
    id: hex(i + 1, "fae0"),
    facility_ref: ref,
    version: 1,
    action: "created",
    kind,
    code: `${kind.toUpperCase()}-${String(i + 1).padStart(3, "0")}`,
    name,
    lon: fixed(at[0], 6),
    lat: fixed(at[1], 6),
    has_polygon: !!opts.polygon,
    province_code: province,
    commune_code: commune,
    capacity: String(cap),
    unit: "tan",
    status: "active",
    event_time: iso(120),
    recorded_at: iso(120),
    supersedes_id: null,
    correction_reason: null,
    actor_ref: "mock-admin-setter",
  };
  const st: FacilityState = { ref, origin, events: [ev], footprint: opts.polygon ? (hexagon(at, 0.0025)[0] as LngLat[]) : null, snapshots: [], revision: 1 };
  if (used !== null) st.snapshots.push(snap(st, "valve", cap, used, iso(1)));
  if (opts.manual) st.snapshots.push(snap(st, "manual", opts.manual[0], opts.manual[1], iso(3)));
  FACILITIES.push(st);
}

function snap(st: FacilityState, source: GeoCapacitySnapshot["source"], cap: number | string, used: number | string, asOf: string, action: GeoCapacitySnapshot["action"] = "snapshot", reason: string | null = null, actor = "mock-admin-setter"): GeoCapacitySnapshot {
  return {
    id: uuidv7(),
    subject_kind: st.origin === "mkg" ? "hub" : "facility",
    subject_ref: st.ref,
    seq: st.snapshots.length + 1,
    action,
    source,
    as_of: asOf,
    capacity: String(cap),
    used: String(used),
    unit: "tan",
    recorded_at: asOf,
    supersedes_id: null,
    correction_reason: reason,
    actor_ref: source === "valve" ? "mkg-valve" : actor,
  };
}

(function seedFacilities() {
  const dt = COMMUNES.filter((c) => c.province_code === "82");
  const ag = COMMUNES.filter((c) => c.province_code === "91");
  const ct = COMMUNES.filter((c) => c.province_code === "92");
  const vl = COMMUNES.filter((c) => c.province_code === "86");
  const off = (c: { center: [number, number] }, dx: number, dy: number): LngLat => [c.center[0] + dx, c.center[1] + dy];
  addFacility(0, "tenant", "kho", "Kho lúa Mỹ An", off(dt[0]!, 0.02, 0.01), "82", dt[0]!.code, 5000, 2100, { polygon: true });
  addFacility(1, "tenant", "hub", "Hub rơm Tân Hòa", off(dt[1]!, -0.015, 0.02), "82", dt[1]!.code, 3000, 2280);
  addFacility(2, "mkg", "hub", "Hub MKG Phú Thành", off(dt[2]!, 0.01, -0.02), "82", dt[2]!.code, 8000, 7200);
  // Admin nhập tay, van có số mới khác → sync_differs (BR-52).
  addFacility(3, "tenant", "kho", "Kho Long Hưng", off(dt[3]!, 0.02, 0.015), "82", dt[3]!.code, 4000, 1000, { manual: [4200, 3900] });
  addFacility(4, "tenant", "hub", "Hub An Giang 1", off(ag[0]!, 0.01, 0.01), "91", ag[0]!.code, 6000, 2400, { polygon: true });
  addFacility(5, "mkg", "hub", "Hub MKG Tri Tôn", off(ag[2]!, -0.01, 0.01), "91", ag[2]!.code, 10000, 8900);
  addFacility(6, "tenant", "nha_may", "Nhà máy viên nén Cần Thơ", off(ct[0]!, 0.02, 0.0), "92", ct[0]!.code, 20000, 9000, { polygon: true });
  addFacility(7, "tenant", "kho", "Kho Thới Lai", off(ct[1]!, 0.01, -0.01), "92", ct[1]!.code, 2500, null);
  addFacility(8, "tenant", "hub", "Hub Vĩnh Long", off(vl[0]!, 0.0, 0.02), "86", vl[0]!.code, 3500, 1400);
})();

function currentEvent(st: FacilityState): GeoFacilityEvent {
  return st.events[st.events.length - 1]!;
}

/** Sức chứa hiệu lực BR-52: số Admin (manual/import) mới nhất chưa nhả, nếu không có thì số của van. */
export function capacityView(st: FacilityState): GeoCapacityView {
  const released = new Set<string>();
  let releasedAt: string | null = null;
  for (const s of st.snapshots) if (s.action === "release") {
    releasedAt = s.recorded_at;
    for (const x of st.snapshots) if (x.action === "snapshot" && x.source !== "valve" && x.recorded_at <= s.recorded_at) released.add(x.id);
  }
  const latest = (pred: (s: GeoCapacitySnapshot) => boolean) => [...st.snapshots].reverse().find((s) => s.action === "snapshot" && pred(s));
  const manual = latest((s) => s.source !== "valve" && !released.has(s.id));
  const valve = latest((s) => s.source === "valve");
  const applied = manual ?? valve;
  const bySource = (["manual", "import", "valve"] as const)
    .map((src) => latest((s) => s.source === src))
    .filter((s): s is GeoCapacitySnapshot => !!s)
    .map((s) => ({ source: s.source, snapshot_id: s.id, as_of: s.as_of, capacity: s.capacity ?? "0", used: s.used ?? "0", unit: "tan" as const, released: released.has(s.id), released_at: released.has(s.id) ? releasedAt : null }));
  if (!applied) return { capacity: null, used: null, unit: null, source: null, as_of: null, snapshot_id: null, fill_pct: null, class: "unknown", sync_differs: false, pending_sync: null, by_source: bySource };
  const cap = Number(applied.capacity);
  const used = Number(applied.used);
  const pct = cap > 0 ? (used / cap) * 100 : null;
  const cls = pct === null ? "unknown" : pct >= Number(THRESHOLDS.full_pct) ? "full" : pct >= Number(THRESHOLDS.warn_pct) ? "near_full" : "available";
  const differs = !!manual && !!valve && valve.as_of > manual.as_of && (valve.capacity !== manual.capacity || valve.used !== manual.used);
  const pending = differs && valve ? bySource.find((b) => b.snapshot_id === valve.id) ?? null : null;
  return {
    capacity: applied.capacity,
    used: applied.used,
    unit: "tan",
    source: applied.source,
    as_of: applied.as_of,
    snapshot_id: applied.id,
    fill_pct: pct === null ? null : pct.toFixed(2),
    class: cls,
    sync_differs: differs,
    pending_sync: pending,
    by_source: bySource,
  };
}

function facilityFeature(st: FacilityState): GeoFacilityFeature {
  const e = currentEvent(st);
  return {
    type: "Feature",
    id: st.ref,
    geometry: { type: "Point", coordinates: [Number(e.lon), Number(e.lat)] },
    properties: {
      origin: st.origin,
      ref: st.ref,
      kind: e.kind,
      code: e.code,
      name: e.name,
      unit: e.unit,
      status: e.status,
      status_reason: e.status === "retired" ? e.correction_reason : null,
      version: e.version,
      province_code: e.province_code,
      commune_code: e.commune_code,
      nominal_capacity: e.capacity,
      area_ha: st.footprint ? areaHa(st.footprint) : null,
      footprint: st.footprint ? { type: "Polygon", coordinates: [st.footprint] } : null,
      current_event_id: e.id,
      capacity: capacityView(st),
      revision: st.revision,
      created_at: st.events[0]!.recorded_at,
      updated_at: e.recorded_at,
    },
  };
}

function facilityVisible(p: MockPersona, st: FacilityState): boolean {
  const e = currentEvent(st);
  return inScope(p, e.province_code, e.commune_code);
}

function facilitiesIn(ctx: Ctx, bb: [number, number, number, number]) {
  const kind = ctx.url.searchParams.get("kind");
  const retired = ctx.url.searchParams.get("include_retired") === "true";
  return FACILITIES.filter((st) => {
    const e = currentEvent(st);
    return facilityVisible(ctx.persona, st) && (!kind || e.kind === kind) && (retired || e.status === "active") && touchesBBox([[Number(e.lon), Number(e.lat)]], bb);
  });
}

function findFacility(ref: string | undefined): FacilityState | undefined {
  return FACILITIES.find((f) => f.ref === ref);
}

function facilityFields(body: Record<string, unknown>, create: boolean): { error?: Response; name?: string; capacity?: string; point?: LngLat; polygon?: LngLat[] | null } {
  const name = typeof body.name === "string" ? body.name.trim() : undefined;
  const province = typeof body.province_code === "string" ? body.province_code.trim() : undefined;
  const commune = typeof body.commune_code === "string" ? body.commune_code.trim() : undefined;
  if (create && (!name || !province || !commune)) return { error: problem(422, "validation_failed", "Validation failed", { detail: "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ.", errors: [{ field: !name ? "name" : "commune_code", reason: "bắt buộc" }] }) };
  if (!create && (name === "" || province === "" || commune === "")) return { error: problem(422, "validation_failed", "Validation failed", { detail: "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ." }) };
  let capacity: string | undefined;
  if (body.capacity !== undefined || create) {
    const c = String(body.capacity ?? "").trim();
    if (!/^\d+(\.\d+)?$/.test(c) || Number(c) <= 0) return { error: problem(422, "validation_failed", "Validation failed", { detail: "Sức chứa phải là số lớn hơn 0.", errors: [{ field: "capacity", reason: "> 0" }] }) };
    capacity = c;
  }
  const pt = body.point as { type?: string; coordinates?: LngLat } | undefined;
  const poly = body.polygon ? polygonInput(body.polygon) : null;
  if (body.polygon && !poly) return { error: problem(422, "geo.invalid_geometry", "Invalid geometry") };
  const point = pt?.type === "Point" && Array.isArray(pt.coordinates) ? (pt.coordinates.slice(0, 2) as LngLat) : poly ? centroidOf(poly) : undefined;
  if (create && !point) return { error: validation([{ field: "point", reason: "Gắn marker hoặc vẽ khuôn viên" }]) };
  return { name, capacity, point, polygon: poly };
}

// ---------------------------------------------------------------------------
// Đường bộ (GIS US-ROAD-01/02/03)
// ---------------------------------------------------------------------------
interface RoadState {
  ref: string;
  versions: GeoRoadSegment[];
  status: "active" | "retired";
  revision: number;
}
const ROADS: RoadState[] = [];
const ROAD_LOAD: Record<string, string> = { quoc_lo: "30", tinh_lo: "16", huyen_lo: "10", truc_xa: "5", bo_ruong: "1.5" };

function addRoad(name: string, cls: GeoRoadSegment["road_class"], coords: LngLat[], province: string) {
  const ref = hex(ROADS.length + 1, "r0ad");
  const g = coords.map((c) => [+c[0].toFixed(6), +c[1].toFixed(6)] as LngLat);
  ROADS.push({
    ref,
    status: "active",
    revision: 1,
    versions: [
      {
        segment_id: hex(ROADS.length + 1, "r0a5"),
        road_ref: ref,
        version: 1,
        action: "recorded",
        name,
        road_class: cls,
        max_load_tons: ROAD_LOAD[cls]!,
        length_m: lineLengthM(g).toFixed(1),
        point_count: g.length,
        province_code: province,
        event_time: iso(200),
        recorded_at: iso(200),
        supersedes_id: null,
        correction_reason: null,
        actor_ref: "mock-admin-setter",
        geometry: { type: "LineString", coordinates: g },
      },
    ],
  });
}

(function seedRoads() {
  const order: GeoRoadSegment["road_class"][] = ["tinh_lo", "huyen_lo", "truc_xa", "bo_ruong"];
  for (const p of PROVINCES) {
    const cs = COMMUNES.filter((c) => c.province_code === p.code);
    cs.forEach((c, i) => {
      const next = cs[(i + 1) % cs.length]!;
      const mid: LngLat = [(c.center[0] + next.center[0]) / 2 + 0.01, (c.center[1] + next.center[1]) / 2 - 0.008];
      const cls = order[i % order.length]!;
      addRoad(`${cls === "tinh_lo" ? "ĐT" : cls === "huyen_lo" ? "ĐH" : cls === "truc_xa" ? "Trục xã" : "Bờ ruộng"} ${c.name.replace(/^Xã /, "")} – ${next.name.replace(/^Xã /, "")}`, cls, [c.center, mid, next.center], p.code);
    });
  }
  const pts = PROVINCES.filter((p) => p.code !== "80").map((p) => p.center);
  addRoad("Quốc lộ 30 (minh hoạ)", "quoc_lo", [pts[1]!, pts[0]!, pts[3]!], "82");
  addRoad("Quốc lộ 91 (minh hoạ)", "quoc_lo", [pts[1]!, pts[2]!, pts[4]!], "92");
})();

function roadFeature(st: RoadState): GeoRoadFeature {
  const v = st.versions[st.versions.length - 1]!;
  return {
    type: "Feature",
    id: st.ref,
    geometry: v.geometry ?? { type: "LineString", coordinates: [] },
    properties: {
      ref: st.ref,
      name: v.name,
      road_class: v.road_class,
      max_load_tons: v.max_load_tons,
      length_m: v.length_m,
      province_code: v.province_code,
      status: st.status,
      status_reason: st.status === "retired" ? v.correction_reason : null,
      version: v.version,
      segment_id: v.segment_id,
      revision: st.revision,
      created_at: st.versions[0]!.recorded_at,
      updated_at: v.recorded_at,
    },
  };
}

function roadLayer(ctx: Ctx, bb: [number, number, number, number]) {
  const minLoad = ctx.url.searchParams.get("min_load_tons");
  const cls = ctx.url.searchParams.get("road_class");
  const limit = limitOf(ctx);
  if (minLoad !== null && (!/^\d+(\.\d+)?$/.test(minLoad) || Number(minLoad) <= 0)) return validation([{ field: "min_load_tons", reason: "Tải trọng phương tiện phải là số lớn hơn 0" }]);
  const inView = ROADS.filter((r) => {
    const v = r.versions[r.versions.length - 1]!;
    return r.status === "active" && inScope(ctx.persona, v.province_code) && (!cls || v.road_class === cls) && touchesBBox((v.geometry?.coordinates ?? []) as LngLat[], bb);
  });
  const matched = minLoad === null ? inView : inView.filter((r) => Number(r.versions[r.versions.length - 1]!.max_load_tons) >= Number(minLoad));
  const none = minLoad !== null && matched.length === 0;
  return {
    type: "FeatureCollection" as const,
    features: matched.slice(0, limit).map(roadFeature),
    truncated: matched.length > limit,
    limit,
    matched: matched.length,
    none_matched: none,
    notice: none ? `Không tìm thấy tuyến đường bộ phù hợp tải trọng ${minLoad} tấn trong khung nhìn.` : null,
  };
}

function roadFields(body: Record<string, unknown>, create: boolean): Response | { name?: string; cls?: GeoRoadSegment["road_class"]; load?: string; line?: LngLat[] } {
  const name = typeof body.name === "string" ? body.name.trim() : undefined;
  const cls = typeof body.road_class === "string" ? (body.road_class as GeoRoadSegment["road_class"]) : undefined;
  if ((create && (!name || !cls)) || name === "" || (cls !== undefined && !(cls in ROAD_LOAD))) return problem(422, "validation_failed", "Validation failed", { detail: "Vui lòng nhập đầy đủ Tên tuyến và Phân loại." });
  let load: string | undefined;
  if (body.max_load_tons !== undefined || create) {
    const l = String(body.max_load_tons ?? "").trim();
    if (!/^\d+(\.\d+)?$/.test(l) || Number(l) <= 0) return problem(422, "validation_failed", "Validation failed", { detail: "Tải trọng tối đa phải là số lớn hơn 0." });
    load = l;
  }
  let line: LngLat[] | undefined;
  if (body.geometry !== undefined || create) {
    const l = lineInput(body.geometry);
    if (!l) return problem(422, "geo.too_few_points", "Too few points");
    line = l;
  }
  return { name, cls, load, line };
}

// ---------------------------------------------------------------------------
// Lớp thửa + giai đoạn mùa vụ (GIS BR-24, US-SEASON-01, US-YIELD-01)
// ---------------------------------------------------------------------------
const PLOTS: { coop: MockCooperative; plot: PlotFeature }[] = COOPERATIVES.filter((c) => c.province_code !== "80").flatMap((coop) => plotsOf(coop).map((plot) => ({ coop, plot })));

function plotsIn(p: MockPersona, bb: [number, number, number, number], limit: number) {
  const all = PLOTS.filter(({ coop, plot }) => plot.properties.status !== "retired" && coopVisible(p, coop) && touchesBBox(ringOf(plot.geometry), bb));
  return { type: "FeatureCollection" as const, features: all.slice(0, limit).map((x) => x.plot), truncated: all.length > limit };
}

const DAY = 86_400_000;
const vnDay = (t: number) => new Date(t + 7 * 3_600_000).toISOString().slice(0, 10);
const dayNum = (s: string) => Date.parse(`${s}T00:00:00Z`);

/** Ngày sạ lệch theo HTX để HTX có giai đoạn trội khác nhau; vài HTX chín rộ trong 1–3 ngày tới. */
const COOP_SOW_DAYS_AGO = [30, 93, 60, -8, 97, 92, 15, 120, 45, 94];

function stageMap(ctx: Ctx): Response {
  const dateQ = ctx.url.searchParams.get("date");
  const today = vnDay(Date.now());
  if (dateQ !== null && !/^\d{4}-\d{2}-\d{2}$/.test(dateQ)) return validation([{ field: "date", reason: "phải có dạng YYYY-MM-DD" }]);
  const date = dateQ ?? today;
  if (Math.abs(dayNum(date) - dayNum(today)) > 366 * DAY) return validation([{ field: "date", reason: "trong vòng một năm so với hôm nay" }]);
  const coopFilter = ctx.url.searchParams.get("cooperative_ref");
  const commune = ctx.url.searchParams.get("commune_code");
  const d = dayNum(date);
  const plots: CropStagePlot[] = [];
  COOPERATIVES.forEach((coop, ci) => {
    if (coop.province_code === "80" || !coop.api.active || !coopVisible(ctx.persona, coop)) return;
    if (coopFilter && coop.api.ref !== coopFilter) return;
    if (commune && coop.api.admin_unit_code !== commune) return;
    const r = mulberry32(ci + 11);
    const base = COOP_SOW_DAYS_AGO[ci % COOP_SOW_DAYS_AGO.length]!;
    plotsOf(coop).forEach((plot, pi) => {
      if (plot.properties.status !== "active" || pi % 6 === 5) return;
      const jitter = Math.floor(r() * 3) - 1;
      const sow = dayNum(today) - (base + jitter) * DAY;
      const open = sow + 90 * DAY;
      const exp = sow + 95 * DAY;
      const completed = base >= 110;
      const stage = completed ? "da_thu" : d < sow ? "chuan_bi" : d < open ? "sinh_truong" : d < exp ? "sap_thu" : "chin_ro";
      const dth = Math.round((exp - d) / DAY);
      const ripening = !completed && dth >= 1 && dth <= 3;
      const area = plot.properties.area_ha;
      plots.push({
        plot_ref: plot.properties.ref,
        plot_code: plot.properties.code,
        season_ref: hex(ci * 1000 + pi, "5ea5"),
        cooperative_ref: coop.api.ref,
        commune_code: coop.api.admin_unit_code ?? "",
        variety_code: pi % 2 === 0 ? "OM5451" : "DT8",
        season_status: completed ? "completed" : "open",
        stage,
        sowing_date: vnDay(sow - 7 * 3_600_000),
        harvest_open_date: vnDay(open - 7 * 3_600_000),
        expected_harvest_date: vnDay(exp - 7 * 3_600_000),
        days_to_harvest: dth,
        ripening,
        area_ha: area,
        // Năng suất dự kiến App HTX gửi (6,2 t/ha) × diện tích; một số thửa chưa có dự kiến.
        expected_tons: pi % 7 === 3 ? null : (Number(area) * 6.2).toFixed(3),
      });
    });
  });
  const truncated = plots.length > MAX;
  const kept = plots.slice(0, MAX);
  const byCoop = new Map<string, CropStagePlot[]>();
  for (const p of kept) byCoop.set(p.cooperative_ref, [...(byCoop.get(p.cooperative_ref) ?? []), p]);
  const order = ["chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu"] as const;
  const coops: CropStageCoop[] = Array.from(byCoop.entries()).map(([ref, ps]) => {
    const area: Record<string, number> = {};
    for (const p of ps) area[p.stage] = (area[p.stage] ?? 0) + Number(p.area_ha);
    const dominant = order.reduce<(typeof order)[number] | null>((best, s) => (area[s] !== undefined && (best === null || area[s]! > area[best]!) ? s : best), null) ?? "sinh_truong";
    const ripe = ps.filter((p) => p.ripening);
    const tons = ripe.filter((p) => p.expected_tons !== null);
    return {
      cooperative_ref: ref,
      commune_code: ps[0]!.commune_code,
      dominant_stage: dominant,
      stage_area_ha: Object.fromEntries(Object.entries(area).map(([k, v]) => [k, v.toFixed(4)])),
      plot_count: ps.length,
      ripening_plot_count: ripe.length,
      ripening_area_ha: ripe.reduce((s, p) => s + Number(p.area_ha), 0).toFixed(4),
      ripening_expected_tons: tons.length ? tons.reduce((s, p) => s + Number(p.expected_tons), 0).toFixed(3) : null,
      earliest_ripening_date: ripe.length ? ripe.map((p) => p.expected_harvest_date).sort()[0]! : null,
    };
  });
  const body: CropStageMap = {
    date,
    stages: order.map((code) => ({ code, label: { chuan_bi: "Chuẩn bị đất", sinh_truong: "Sinh trưởng", sap_thu: "Sắp thu hoạch", chin_ro: "Chín rộ / đang thu hoạch", da_thu: "Đã thu hoạch" }[code] })),
    ripening_days: { from: 1, to: 3 },
    plots: kept,
    cooperatives: coops,
    truncated,
    limit: MAX,
  };
  return json(body);
}

// ---------------------------------------------------------------------------
// Nhập tệp (GeoJSON/KML/Shapefile) — kết quả theo đối tượng (GIS US-BND-07/08)
// ---------------------------------------------------------------------------
interface ParsedFeature {
  name: string | null;
  props: Record<string, unknown>;
  geometry: { type: string; coordinates: unknown } | null;
}

function parseSpatial(ctx: Ctx): { format: "geojson" | "kml" | "shapefile"; features: ParsedFeature[] } | Response {
  const type = ctx.request.headers.get("Content-Type") ?? "";
  if (isZip(ctx.body)) {
    // Shapefile .zip: giả lập không giải nén — kết quả minh hoạ 3 đối tượng.
    return {
      format: "shapefile",
      features: [
        { name: "Đối tượng 1 (Shapefile)", props: {}, geometry: { type: "Polygon", coordinates: hexagon([105.95, 10.5], 0.01) } },
        { name: "Đối tượng 2 (Shapefile)", props: {}, geometry: { type: "Polygon", coordinates: hexagon([105.85, 10.42], 0.01) } },
        { name: null, props: {}, geometry: null },
      ],
    };
  }
  if (type.includes("kml") || (typeof ctx.body === "string" && ctx.body.trimStart().startsWith("<"))) {
    const text = typeof ctx.body === "string" ? ctx.body : "";
    if (/EPSG:(?!4326)\d+|VN-?2000/i.test(text)) return problem(422, "geo.crs_unsupported", "CRS unsupported");
    const marks = Array.from(text.matchAll(/<Placemark[\s\S]*?<\/Placemark>/g)).map((m) => m[0]);
    if (marks.length === 0) return problem(422, "geo.format_invalid", "No features");
    return {
      format: "kml",
      features: marks.map((pm): ParsedFeature => {
        const name = /<name>([\s\S]*?)<\/name>/.exec(pm)?.[1]?.trim() ?? null;
        const data: Record<string, unknown> = {};
        for (const m of pm.matchAll(/<Data name="([^"]+)">\s*<value>([\s\S]*?)<\/value>/g)) data[m[1]!] = m[2]!.trim();
        for (const m of pm.matchAll(/<SimpleData name="([^"]+)">([\s\S]*?)<\/SimpleData>/g)) data[m[1]!] = m[2]!.trim();
        const coords = /<coordinates>([\s\S]*?)<\/coordinates>/.exec(pm)?.[1]?.trim().split(/\s+/).map((t) => t.split(",").slice(0, 2).map(Number) as LngLat) ?? [];
        const isPoint = /<Point>/.test(pm);
        return { name, props: data, geometry: coords.length === 0 ? null : isPoint ? { type: "Point", coordinates: coords[0] } : { type: "Polygon", coordinates: [coords] } };
      }),
    };
  }
  const doc = typeof ctx.body === "string" ? safeJson(ctx.body) : (ctx.body as unknown);
  const fc = doc as { type?: string; features?: { properties?: Record<string, unknown>; geometry?: { type: string; coordinates: unknown } }[]; crs?: { properties?: { name?: string } } } | undefined;
  if (!fc || fc.type !== "FeatureCollection" || !Array.isArray(fc.features)) return problem(422, "geo.format_unsupported", "Unsupported format");
  const crs = fc.crs?.properties?.name;
  if (crs && !/4326|CRS84/.test(crs)) return problem(422, "geo.crs_unsupported", "CRS unsupported");
  if (fc.features.length === 0 || fc.features.length > 200) return problem(422, "geo.format_invalid", "Feature count out of range");
  return {
    format: "geojson",
    features: fc.features.map((f) => {
      const props = f.properties ?? {};
      const name = (props.name ?? props.ten ?? props.TEN ?? null) as string | null;
      return { name, props, geometry: f.geometry ?? null };
    }),
  };
}

function safeJson(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return undefined;
  }
}

function importCoops(ctx: Ctx): Response {
  if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
  const parsed = parseSpatial(ctx);
  if (parsed instanceof Response) return parsed;
  const reason = ctx.url.searchParams.get("reason") || `Nhập từ tệp ${ctx.url.searchParams.get("file_name") ?? "ranh giới"}`;
  const out = parsed.features.map((f, index) => {
    const base = { index, name: f.name, replaced: false, cooperative_ref: null as string | null, boundary_id: null as string | null, version: 0, area_ha: null as string | null };
    const code = String(f.props.cooperative_code ?? f.props.ma_htx ?? f.props.MA_HTX ?? "").trim();
    const ref = String(f.props.cooperative_ref ?? "").trim();
    const st = Array.from(COOP_STATE.values()).find((s) => (ref && s.coop.api.ref === ref) || (code && s.coop.api.code === code));
    if (!f.geometry) return { ...base, status: "rejected" as const, code: "geo.invalid_geometry", reason: "Đối tượng không có hình học" };
    if (!st) return { ...base, status: "rejected" as const, code: "geo.cooperative_invalid", reason: "Không tìm thấy HTX theo Mã HTX/cooperative_ref" };
    const ring = polygonInput(f.geometry);
    if (!ring) return { ...base, cooperative_ref: st.coop.api.ref, status: "rejected" as const, code: "geo.invalid_geometry", reason: "Hình không phải vùng khép kín hợp lệ" };
    const had = st.status === "active";
    const v = recordBoundary(st, ring, ctx.persona, "import_file", had ? reason : null);
    return { ...base, status: "accepted" as const, replaced: had, code: null, reason: null, cooperative_ref: st.coop.api.ref, boundary_id: v.boundary_id, version: v.version, area_ha: v.area_ha };
  });
  return json({ format: parsed.format, accepted: out.filter((x) => x.status === "accepted").length, duplicate: 0, rejected: out.filter((x) => x.status === "rejected").length, features: out });
}

function importFacilities(ctx: Ctx): Response {
  if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
  const parsed = parseSpatial(ctx);
  if (parsed instanceof Response) return parsed;
  const q = ctx.url.searchParams;
  const out = parsed.features.map((f, index) => {
    const p = f.props;
    const name = String(p.name ?? p.ten ?? f.name ?? "").trim();
    const codeIn = String(p.code ?? p.ma ?? "").trim();
    const province = String(p.province_code ?? p.ma_tinh ?? q.get("province_code") ?? "").trim();
    const commune = String(p.commune_code ?? p.ma_xa ?? q.get("commune_code") ?? "").trim();
    const cap = String(p.capacity ?? p.suc_chua ?? "").trim();
    const kind = (String(p.kind ?? p.loai ?? q.get("kind") ?? "hub").trim() || "hub") as "kho" | "hub" | "nha_may";
    const base = { index, name: name || f.name, facility_ref: null as string | null, facility_code: null as string | null };
    if (!name || !province || !commune) return { ...base, status: "rejected" as const, code: "validation_failed", reason: "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ." };
    if (!/^\d+(\.\d+)?$/.test(cap) || Number(cap) <= 0) return { ...base, status: "rejected" as const, code: "validation_failed", reason: "Sức chứa phải là số lớn hơn 0." };
    if (!f.geometry) return { ...base, status: "rejected" as const, code: "geo.invalid_geometry", reason: "Đối tượng không có hình học" };
    const dup = codeIn ? FACILITIES.find((x) => currentEvent(x).code === codeIn) : undefined;
    if (dup) return { ...base, status: "duplicate" as const, code: null, reason: "Mã đã có — dùng lại bản ghi cũ", facility_ref: dup.ref, facility_code: codeIn };
    const at: LngLat = f.geometry.type === "Point" ? (f.geometry.coordinates as LngLat) : centroidOf((f.geometry.coordinates as LngLat[][])[0] ?? []);
    addFacility(FACILITIES.length, "tenant", kind, name, at, province, commune, Number(cap), null, { polygon: f.geometry.type !== "Point" });
    const st = FACILITIES[FACILITIES.length - 1]!;
    if (codeIn) currentEvent(st).code = codeIn;
    currentEvent(st).actor_ref = ctx.persona.me.actor_ref;
    return { ...base, status: "accepted" as const, code: null, reason: null, facility_ref: st.ref, facility_code: currentEvent(st).code };
  });
  return json({ format: parsed.format, accepted: out.filter((x) => x.status === "accepted").length, duplicate: out.filter((x) => x.status === "duplicate").length, rejected: out.filter((x) => x.status === "rejected").length, features: out });
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------
function withBBox(fn: (ctx: Ctx, bb: [number, number, number, number]) => Response) {
  return (ctx: Ctx) => {
    const bb = bboxOf(ctx);
    return bb instanceof Response ? bb : fn(ctx, bb);
  };
}

function facilityWritten(st: FacilityState, status = 200) {
  return json({ facility: facilityFeature(st), event: currentEvent(st) }, status);
}

function reviseFacility(ctx: Ctx, st: FacilityState, patch: Partial<GeoFacilityEvent>, action: GeoFacilityEvent["action"], reason: string | null) {
  const cur = currentEvent(st);
  const ev: GeoFacilityEvent = { ...cur, ...patch, id: uuidv7(), version: cur.version + 1, action, event_time: nowIso(), recorded_at: nowIso(), supersedes_id: cur.id, correction_reason: reason, actor_ref: ctx.persona.me.actor_ref };
  st.events.push(ev);
  st.revision++;
}

export const gisAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/geo\/layers$/,
    handler: withBBox((ctx, bb) => {
      const zoom = Math.round(Number(ctx.url.searchParams.get("zoom") ?? 12));
      const want = new Set((ctx.url.searchParams.get("layers") ?? "cooperatives,plots,facilities,roads").split(","));
      const limit = limitOf(ctx);
      const plotsOk = zoom >= PLOT_MIN_ZOOM;
      const roads = want.has("roads") ? roadLayer(ctx, bb) : null;
      if (roads instanceof Response) return roads;
      return json({
        zoom,
        plot_min_zoom: PLOT_MIN_ZOOM,
        plots_notice: want.has("plots") && !plotsOk ? `Phóng to tới mức ${PLOT_MIN_ZOOM} (khoảng 1:10.000) để tải polygon thửa ruộng.` : null,
        cooperatives: want.has("cooperatives") ? coopLayer(ctx.persona, bb, limit) : null,
        plots: want.has("plots") && plotsOk ? plotsIn(ctx.persona, bb, limit) : null,
        facilities: want.has("facilities") ? { type: "FeatureCollection", features: facilitiesIn(ctx, bb).slice(0, limit).map(facilityFeature), truncated: false, limit, thresholds: THRESHOLDS } : null,
        roads,
      });
    }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/cooperative-boundaries$/,
    handler: withBBox((ctx, bb) => json(coopLayer(ctx.persona, bb, limitOf(ctx)))),
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/cooperative-boundaries$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, ...BOUNDARY_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const b = (body ?? {}) as { cooperative_ref?: string | null; geometry?: unknown };
      const st = b.cooperative_ref ? COOP_STATE.get(b.cooperative_ref) : undefined;
      if (!st) return problem(422, "validation_failed", "Validation failed", { detail: "Vui lòng nhập đầy đủ Tên HTX và Địa chỉ.", errors: [{ field: "cooperative_ref", reason: "Chọn HTX" }] });
      if (!coopVisible(persona, st.coop)) return problem(403, "geo.out_of_scope", "Out of scope");
      if (st.status !== null) {
        const cur = st.versions[st.versions.length - 1]!;
        return problem(409, "geo.cooperative_boundary_exists", "Boundary exists", { current_boundary_id: cur.boundary_id, current_version: cur.version, status: st.status });
      }
      const ring = polygonInput(b.geometry);
      if (!ring) return problem(422, "geo.too_few_points", "Too few points");
      const v = recordBoundary(st, ring, persona, "geo_web", null);
      return json({ cooperative: coopFeature(st), boundary: v }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/cooperative-boundaries\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      const st = COOP_STATE.get(params[0] ?? "");
      if (!st || !coopVisible(persona, st.coop) || st.versions.length === 0) return problem(404, "geo.cooperative_boundary_not_found", "Not found");
      return json({ cooperative: coopFeature(st), versions: st.versions });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/cooperative-boundaries\/([\w-]+)\/versions$/,
    handler: ({ params, body, persona }) => {
      if (!hasRole(persona, ...BOUNDARY_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const st = COOP_STATE.get(params[0] ?? "");
      if (!st || !coopVisible(persona, st.coop) || st.versions.length === 0) return problem(404, "geo.cooperative_boundary_not_found", "Not found");
      const b = (body ?? {}) as { supersedes_id?: string; correction_reason?: string; geometry?: unknown };
      const cur = st.versions[st.versions.length - 1]!;
      if (st.status === "retired") return problem(409, "geo.feature_state_conflict", "Retired");
      if (b.supersedes_id !== cur.boundary_id) return problem(409, "geo.version_conflict", "Version conflict", { current_boundary_id: cur.boundary_id, current_version: cur.version });
      if (reasonOf(body, "correction_reason").length < 10) return validation([{ field: "correction_reason", reason: "Lý do ít nhất 10 ký tự" }]);
      const ring = polygonInput(b.geometry);
      if (!ring) return problem(422, "geo.too_few_points", "Too few points");
      const v = recordBoundary(st, ring, persona, "geo_web", reasonOf(body, "correction_reason"));
      return json({ cooperative: coopFeature(st), boundary: v }, 201);
    },
  },
  ...(["retire", "restore"] as const).map(
    (op): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/geo/cooperative-boundaries/([\\w-]+)/${op}$`),
      handler: ({ params, body, persona }) => {
        if (!hasRole(persona, ...BOUNDARY_WRITERS)) return problem(403, "forbidden", "Forbidden");
        const st = COOP_STATE.get(params[0] ?? "");
        if (!st || !coopVisible(persona, st.coop) || st.versions.length === 0) return problem(404, "geo.cooperative_boundary_not_found", "Not found");
        if (reasonOf(body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
        if ((op === "retire" && st.status !== "active") || (op === "restore" && st.status !== "retired")) return problem(409, "geo.feature_state_conflict", "State conflict");
        const cur = st.versions[st.versions.length - 1]!;
        const v: GeoCoopBoundaryVersion = { ...cur, boundary_id: uuidv7(), version: cur.version + 1, action: op === "retire" ? "retired" : "restored", supersedes_id: cur.boundary_id, correction_reason: reasonOf(body), actor_ref: persona.me.actor_ref, event_time: nowIso(), recorded_at: nowIso() };
        st.versions.push(v);
        st.status = op === "retire" ? "retired" : "active";
        st.revision++;
        return json({ cooperative: coopFeature(st), boundary: v });
      },
    }),
  ),
  {
    method: "GET",
    pattern: /^\/v1\/geo\/facilities$/,
    handler: withBBox((ctx, bb) => {
      const all = facilitiesIn(ctx, bb);
      const limit = limitOf(ctx);
      return json({ type: "FeatureCollection", features: all.slice(0, limit).map(facilityFeature), truncated: all.length > limit, limit, thresholds: THRESHOLDS });
    }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/facilities\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      const st = findFacility(params[0]);
      if (!st || !facilityVisible(persona, st)) return problem(404, "geo.facility_not_found", "Not found");
      return json({ facility: facilityFeature(st), events: st.events, capacity: capacityView(st), capacity_history: st.snapshots });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/hubs\/([\w-]+)\/capacity$/,
    handler: ({ params }) => {
      const st = findFacility(params[0]);
      if (!st || st.origin !== "mkg") return problem(404, "geo.hub_not_found", "Not found");
      return json({ hub_ref: st.ref, capacity: capacityView(st), capacity_history: st.snapshots });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/capacity-widgets$/,
    handler: withBBox((ctx, bb) => {
      const all = facilitiesIn(ctx, bb);
      const limit = limitOf(ctx);
      const items: CapacityWidget[] = all.slice(0, limit).map((st) => {
        const e = currentEvent(st);
        const c = capacityView(st);
        return {
          origin: st.origin,
          ref: st.ref,
          kind: e.kind,
          code: e.code,
          name: e.name,
          lon: e.lon,
          lat: e.lat,
          capacity: c.capacity,
          used: c.used,
          free: c.capacity !== null && c.used !== null ? String(Number(c.capacity) - Number(c.used)) : null,
          unit: c.unit,
          fill_pct: c.fill_pct,
          class: c.class,
          source: c.source,
          sync_differs: c.sync_differs,
        };
      });
      return json({ items, truncated: all.length > limit, thresholds: THRESHOLDS });
    }),
  },
  { method: "GET", pattern: /^\/v1\/geo\/roads$/, handler: withBBox((ctx, bb) => {
    const r = roadLayer(ctx, bb);
    return r instanceof Response ? r : json(r);
  }) },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/roads\/([\w-]+)$/,
    handler: ({ params }) => {
      const st = ROADS.find((r) => r.ref === params[0]);
      return st ? json({ road: roadFeature(st), versions: st.versions }) : problem(404, "geo.road_not_found", "Not found");
    },
  },
  { method: "GET", pattern: /^\/v1\/cropcycle\/stage-map$/, handler: stageMap },
];

export const gisAdminRoutes: Route[] = [
  { method: "POST", pattern: /^\/v1\/geo\/cooperative-boundaries\/import$/, handler: importCoops },
  { method: "POST", pattern: /^\/v1\/geo\/facilities\/import$/, handler: importFacilities },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/facilities$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as Record<string, unknown>;
      const f = facilityFields(b, true);
      if (f.error) return f.error;
      const code = typeof b.code === "string" && b.code.trim() ? b.code.trim() : undefined;
      if (code && FACILITIES.some((x) => currentEvent(x).code === code)) return problem(409, "geo.facility_code_exists", "Code exists");
      const kind = (b.kind as "kho" | "hub" | "nha_may" | undefined) ?? "hub";
      addFacility(FACILITIES.length, "tenant", kind, f.name!, f.point!, String(b.province_code), String(b.commune_code), Number(f.capacity), null, { polygon: !!f.polygon });
      const st = FACILITIES[FACILITIES.length - 1]!;
      const e = currentEvent(st);
      if (code) e.code = code;
      e.unit = (b.unit as GeoFacilityEvent["unit"] | undefined) ?? "tan";
      e.actor_ref = ctx.persona.me.actor_ref;
      e.event_time = e.recorded_at = nowIso();
      st.footprint = f.polygon ?? null;
      return facilityWritten(st, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/facilities\/([\w-]+)\/versions$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const st = findFacility(ctx.params[0]);
      if (!st || st.origin !== "tenant") return problem(404, "geo.facility_not_found", "Not found");
      const b = (ctx.body ?? {}) as Record<string, unknown>;
      const cur = currentEvent(st);
      if (cur.status === "retired") return problem(409, "geo.feature_state_conflict", "Retired");
      if (b.supersedes_id !== cur.id) return problem(409, "geo.version_conflict", "Version conflict", { current_event_id: cur.id });
      if (reasonOf(b, "correction_reason").length < 10) return validation([{ field: "correction_reason", reason: "Lý do ít nhất 10 ký tự" }]);
      const f = facilityFields(b, false);
      if (f.error) return f.error;
      const patch: Partial<GeoFacilityEvent> = {};
      if (f.name) patch.name = f.name;
      if (f.capacity) patch.capacity = f.capacity;
      if (typeof b.province_code === "string" && b.province_code) patch.province_code = b.province_code;
      if (typeof b.commune_code === "string" && b.commune_code) patch.commune_code = b.commune_code;
      if (typeof b.kind === "string") patch.kind = b.kind as GeoFacilityEvent["kind"];
      if (f.point) {
        patch.lon = fixed(f.point[0], 6);
        patch.lat = fixed(f.point[1], 6);
      }
      if (f.polygon) st.footprint = f.polygon;
      if (b.remove_polygon === true) st.footprint = null;
      patch.has_polygon = !!st.footprint;
      reviseFacility(ctx, st, patch, "updated", reasonOf(b, "correction_reason"));
      return facilityWritten(st, 201);
    },
  },
  ...(["retire", "restore"] as const).map(
    (op): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/geo/facilities/([\\w-]+)/${op}$`),
      handler: (ctx) => {
        if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
        const st = findFacility(ctx.params[0]);
        if (!st || st.origin !== "tenant") return problem(404, "geo.facility_not_found", "Not found");
        if (reasonOf(ctx.body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
        const cur = currentEvent(st);
        if ((op === "retire") !== (cur.status === "active")) return problem(409, "geo.feature_state_conflict", "State conflict");
        reviseFacility(ctx, st, { status: op === "retire" ? "retired" : "active" }, op === "retire" ? "retired" : "restored", reasonOf(ctx.body));
        return facilityWritten(st);
      },
    }),
  ),
  ...(["facilities", "hubs"] as const).flatMap((kind): Route[] => [
    {
      method: "POST",
      pattern: new RegExp(`^/v1/geo/${kind}/([\\w-]+)/capacity$`),
      handler: (ctx) => {
        if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
        const st = findFacility(ctx.params[0]);
        if (!st || (kind === "hubs") !== (st.origin === "mkg")) return problem(404, kind === "hubs" ? "geo.hub_not_found" : "geo.facility_not_found", "Not found");
        const b = (ctx.body ?? {}) as Record<string, unknown>;
        const cap = String(b.capacity ?? "").trim();
        const used = String(b.used ?? "").trim();
        if (!/^\d+(\.\d+)?$/.test(cap) || Number(cap) <= 0) return problem(422, "validation_failed", "Validation failed", { detail: "Sức chứa phải là số lớn hơn 0.", errors: [{ field: "capacity", reason: "> 0" }] });
        if (!/^\d+(\.\d+)?$/.test(used) || Number(used) < 0) return validation([{ field: "used", reason: "Dung lượng đã dùng phải là số ≥ 0" }]);
        if (Number(used) > Number(cap)) return validation([{ field: "used", reason: "Đã dùng không vượt quá sức chứa" }]);
        const s = snap(st, "manual", cap, used, typeof b.as_of === "string" && b.as_of ? b.as_of : nowIso(), "snapshot", typeof b.correction_reason === "string" ? b.correction_reason : null, ctx.persona.me.actor_ref);
        st.snapshots.push(s);
        return json({ snapshot: s, capacity: capacityView(st) }, 201);
      },
    },
    {
      method: "POST",
      pattern: new RegExp(`^/v1/geo/${kind}/([\\w-]+)/capacity/release$`),
      handler: (ctx) => {
        if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
        const st = findFacility(ctx.params[0]);
        if (!st || (kind === "hubs") !== (st.origin === "mkg")) return problem(404, kind === "hubs" ? "geo.hub_not_found" : "geo.facility_not_found", "Not found");
        if (reasonOf(ctx.body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
        const view = capacityView(st);
        if (view.source !== "manual" && view.source !== "import") return problem(409, "geo.capacity_nothing_to_release", "Nothing to release");
        const s = snap(st, view.source, view.capacity ?? "0", view.used ?? "0", nowIso(), "release", reasonOf(ctx.body), ctx.persona.me.actor_ref);
        st.snapshots.push(s);
        return json({ snapshot: s, capacity: capacityView(st) });
      },
    },
  ]),
  {
    method: "POST",
    pattern: /^\/v1\/geo\/capacity\/import$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const items = ((ctx.body ?? {}) as { items?: { facility_code?: string; hub_ref?: string; capacity?: string; used?: string }[] }).items ?? [];
      const out = items.map((it, index) => {
        const st = FACILITIES.find((f) => (it.facility_code && currentEvent(f).code === it.facility_code) || (it.hub_ref && f.ref === it.hub_ref));
        if (!st) return { index, status: "rejected" as const, code: "geo.facility_not_found", reason: "Không tìm thấy kho/Hub", snapshot_id: null };
        if (!(Number(it.capacity) > 0)) return { index, status: "rejected" as const, code: "validation_failed", reason: "Sức chứa phải là số lớn hơn 0.", snapshot_id: null };
        const s = snap(st, "import", String(it.capacity), String(it.used ?? "0"), nowIso(), "snapshot", null, ctx.persona.me.actor_ref);
        st.snapshots.push(s);
        return { index, status: "accepted" as const, code: null, reason: null, snapshot_id: s.id };
      });
      return json({ accepted: out.filter((o) => o.status === "accepted").length, duplicate: 0, rejected: out.filter((o) => o.status === "rejected").length, items: out });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/roads$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as Record<string, unknown>;
      const f = roadFields(b, true);
      if (f instanceof Response) return f;
      const province = typeof b.province_code === "string" && b.province_code ? b.province_code : "82";
      addRoad(f.name!, f.cls!, f.line!, province);
      const st = ROADS[ROADS.length - 1]!;
      const v = st.versions[0]!;
      v.max_load_tons = f.load!;
      v.actor_ref = ctx.persona.me.actor_ref;
      v.event_time = v.recorded_at = nowIso();
      return json({ road: roadFeature(st), segment: v }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/roads\/([\w-]+)\/versions$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
      const st = ROADS.find((r) => r.ref === ctx.params[0]);
      if (!st) return problem(404, "geo.road_not_found", "Not found");
      const b = (ctx.body ?? {}) as Record<string, unknown>;
      const cur = st.versions[st.versions.length - 1]!;
      if (st.status === "retired") return problem(409, "geo.feature_state_conflict", "Retired");
      if (b.supersedes_id !== cur.segment_id) return problem(409, "geo.version_conflict", "Version conflict", { current_segment_id: cur.segment_id });
      if (reasonOf(b, "correction_reason").length < 10) return validation([{ field: "correction_reason", reason: "Lý do ít nhất 10 ký tự" }]);
      const f = roadFields(b, false);
      if (f instanceof Response) return f;
      const line = f.line ?? ((cur.geometry?.coordinates ?? []) as LngLat[]);
      const v: GeoRoadSegment = {
        ...cur,
        segment_id: uuidv7(),
        version: cur.version + 1,
        action: "recorded",
        name: f.name ?? cur.name,
        road_class: f.cls ?? cur.road_class,
        max_load_tons: f.load ?? cur.max_load_tons,
        geometry: { type: "LineString", coordinates: line },
        length_m: lineLengthM(line).toFixed(1),
        point_count: line.length,
        supersedes_id: cur.segment_id,
        correction_reason: reasonOf(b, "correction_reason"),
        actor_ref: ctx.persona.me.actor_ref,
        event_time: nowIso(),
        recorded_at: nowIso(),
      };
      st.versions.push(v);
      st.revision++;
      return json({ road: roadFeature(st), segment: v }, 201);
    },
  },
  ...(["retire", "restore"] as const).map(
    (op): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/geo/roads/([\\w-]+)/${op}$`),
      handler: (ctx) => {
        if (!hasRole(ctx.persona, ...GEO_WRITERS)) return problem(403, "forbidden", "Forbidden");
        const st = ROADS.find((r) => r.ref === ctx.params[0]);
        if (!st) return problem(404, "geo.road_not_found", "Not found");
        if (reasonOf(ctx.body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
        if ((op === "retire") !== (st.status === "active")) return problem(409, "geo.feature_state_conflict", "State conflict");
        const cur = st.versions[st.versions.length - 1]!;
        st.versions.push({ ...cur, segment_id: uuidv7(), version: cur.version + 1, action: op === "retire" ? "retired" : "restored", supersedes_id: cur.segment_id, correction_reason: reasonOf(ctx.body), actor_ref: ctx.persona.me.actor_ref, event_time: nowIso(), recorded_at: nowIso() });
        st.status = op === "retire" ? "retired" : "active";
        st.revision++;
        return json({ road: roadFeature(st), segment: st.versions[st.versions.length - 1] });
      },
    }),
  ),
];

/** Cho kiểm thử: danh sách cơ sở (đọc). */
export const mockFacilities = () => FACILITIES.map(facilityFeature);
