// Client bản đồ vùng nuôi, vùng dịch CNTS (W11.52 — BRD CNTS v3.7 FN-18, UR-TINH-17): /v1/extension/cnts/map/*. Hình học
// thuần (vòng tròn bán kính, điểm trong đa giác, GeoJSON) ở đây để kiểm thử đơn vị và dùng cho giả lập; số lưu (diện tích,
// vùng đệm, đối tượng trong vùng) do máy chủ tính bằng PostGIS.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
/** [kinh độ, vĩ độ] WGS84. */
export type LngLat = [number, number];
export type Zone = S["CNTSMapZone"];
export type ZoneDetail = S["CNTSMapZoneDetail"];
export type ZoneSave = S["CNTSMapZoneSave"];
export type ZoneKind = Zone["kind"];
export type MapLayers = S["CNTSMapLayers"];
export type MapOutbreak = S["CNTSMapOutbreak"];
export type MapPlace = S["CNTSMapPlace"];
export type ZoneContents = S["CNTSMapZoneContents"];
export type Geometry = S["CNTSMapGeometry"];

export const KINDS: readonly ZoneKind[] = ["vung_chan_nuoi", "vung_thuy_san", "vung_dich", "vung_uy_hiep", "vung_dem", "huong_di_chuyen"];
export const KIND_LABEL: Record<ZoneKind, string> = {
  vung_chan_nuoi: "Vùng chăn nuôi",
  vung_thuy_san: "Vùng nuôi thủy sản",
  vung_dich: "Vùng dịch",
  vung_uy_hiep: "Vùng bị uy hiếp",
  vung_dem: "Vùng đệm",
  huong_di_chuyen: "Hướng di chuyển",
};
/** Màu lớp theo loại vùng (đỏ: dịch; cam: uy hiếp; vàng: đệm; xanh: vùng nuôi). */
export const KIND_COLOR: Record<ZoneKind, string> = {
  vung_chan_nuoi: "#15803d",
  vung_thuy_san: "#0e7490",
  vung_dich: "#dc2626",
  vung_uy_hiep: "#ea580c",
  vung_dem: "#ca8a04",
  huong_di_chuyen: "#7c3aed",
};
export const EPIDEMIC: readonly ZoneKind[] = ["vung_dich", "vung_uy_hiep", "vung_dem", "huong_di_chuyen"];
export const OUTBREAK_STATUS_LABEL: Record<MapOutbreak["status"], string> = { reported: "Đã gửi", monitoring: "Đang theo dõi", closed: "Đã kết thúc" };
/** Bán kính mặc định khi khoanh vùng từ ổ dịch (m) — giả định W11.52, máy chủ dùng cùng giá trị. */
export const DEFAULT_RADII = { vung_dich: 500, vung_uy_hiep: 3000, vung_dem: 10000 } as const;
export const WARNING_LABEL: Record<string, string> = { outside_communes: "Vùng nằm ngoài địa giới các xã đã chọn — kiểm tra lại hình vẽ" };

/** Ngày YYYY-MM-DD → DD/MM/YYYY. */
export const dmy = (d: string | null | undefined) => (d ? d.split("-").reverse().join("/") : "—");

/** Cộng n ngày vào YYYY-MM-DD. */
export function addDays(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

/** Vòng tròn (đa giác 64 cạnh) bán kính r mét quanh tâm — để xem trước và cho giả lập (máy chủ dùng ST_Buffer). */
export function circlePolygon(center: LngLat, radiusM: number, steps = 64): Geometry {
  const [lng, lat] = center;
  const dLat = radiusM / 111_320;
  const dLng = radiusM / (111_320 * Math.cos((lat * Math.PI) / 180));
  const ring: LngLat[] = [];
  for (let i = 0; i < steps; i++) {
    const a = (2 * Math.PI * i) / steps;
    ring.push([+(lng + dLng * Math.cos(a)).toFixed(7), +(lat + dLat * Math.sin(a)).toFixed(7)]);
  }
  ring.push(ring[0]!);
  return { type: "Polygon", coordinates: [ring] };
}

function inRing(p: LngLat, ring: readonly LngLat[]): boolean {
  let inside = false;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const [xi, yi] = ring[i]!;
    const [xj, yj] = ring[j]!;
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

/** Điểm nằm trong Polygon/MultiPolygon (vòng ngoài trừ lỗ). */
export function containsPoint(g: Geometry, p: LngLat): boolean {
  const polys = (g.type === "Polygon" ? [g.coordinates] : g.type === "MultiPolygon" ? g.coordinates : []) as LngLat[][][];
  return polys.some((rings) => rings.length > 0 && inRing(p, rings[0]!) && !rings.slice(1).some((h) => inRing(p, h)));
}

/** Tâm (trung bình đỉnh vòng ngoài) của hình. */
export function centerOf(g: Geometry): LngLat {
  const pts = (g.type === "LineString" ? g.coordinates : g.type === "Polygon" ? (g.coordinates[0] as unknown[]) : ((g.coordinates[0] as unknown[][])?.[0] ?? [])) as LngLat[];
  const body = g.type === "LineString" ? pts : pts.slice(0, -1);
  if (body.length === 0) return [0, 0];
  return [body.reduce((s, p) => s + p[0], 0) / body.length, body.reduce((s, p) => s + p[1], 0) / body.length];
}

/** Khoảng cách Haversine giữa hai điểm (m) — bán kính Trái Đất trung bình như PostGIS sphere. */
export function distanceM(a: LngLat, b: LngLat): number {
  const rad = (d: number) => (d * Math.PI) / 180;
  const h = Math.sin(rad(b[1] - a[1]) / 2) ** 2 + Math.cos(rad(a[1])) * Math.cos(rad(b[1])) * Math.sin(rad(b[0] - a[0]) / 2) ** 2;
  return 2 * 6_371_008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Kiểm biểu vẽ vùng phía máy khách (máy chủ kiểm lại, kể cả ST_IsValid). */
export function zoneErrors(z: Partial<ZoneSave>, draftCount: number): Record<string, string> {
  const e: Record<string, string> = {};
  if (!z.kind) e.kind = "Chọn loại vùng";
  if (!z.name?.trim()) e.name = "Nhập tên vùng";
  if (!z.communes?.length) e.communes = "Chọn ít nhất một xã";
  if (z.draw_mode === "radius") {
    if (!z.center) e.center = "Bấm lên bản đồ để chọn tâm";
    if (!z.radius_m || z.radius_m < 10 || z.radius_m > 50000) e.radius_m = "Bán kính 10–50.000 m";
  } else if (z.draw_mode === "line") {
    if (draftCount < 2) e.geometry = "Vẽ ít nhất 2 điểm của hướng di chuyển";
  } else if (draftCount < 3 && !z.geometry) e.geometry = "Vẽ ít nhất 3 đỉnh";
  if (z.kind === "huong_di_chuyen" && z.draw_mode !== "line") e.draw_mode = "Hướng di chuyển vẽ bằng đường";
  if (z.kind && z.kind !== "huong_di_chuyen" && z.draw_mode === "line") e.draw_mode = "Vùng vẽ bằng đa giác hoặc bán kính";
  return e;
}

/** Hình từ các đỉnh đang vẽ. */
export function draftToGeometry(mode: "polygon" | "line", coords: readonly LngLat[]): Geometry | null {
  if (mode === "line") return coords.length >= 2 ? { type: "LineString", coordinates: coords.map((c) => [...c]) } : null;
  if (coords.length < 3) return null;
  return { type: "Polygon", coordinates: [[...coords.map((c) => [...c]), [...coords[0]!]]] };
}

/** Đỉnh (vòng ngoài, bỏ điểm khép) của hình để sửa. */
export function geometryToDraft(g: Geometry | null | undefined): LngLat[] {
  if (!g) return [];
  if (g.type === "LineString") return (g.coordinates as LngLat[]).map((c) => [c[0], c[1]] as LngLat);
  const ring = (g.type === "Polygon" ? g.coordinates[0] : (g.coordinates[0] as unknown[])?.[0]) as LngLat[] | undefined;
  return (ring ?? []).slice(0, -1).map((c) => [c[0], c[1]] as LngLat);
}

/** FeatureCollection GeoJSON của các vùng (tải về — FN-18 luồng phụ). */
export function zonesGeoJSON(zones: readonly Zone[]) {
  return {
    type: "FeatureCollection" as const,
    features: zones.map((z) => ({
      type: "Feature" as const,
      id: z.ref,
      geometry: z.geometry,
      properties: { ref: z.ref, version: z.version, kind: z.kind, kind_label: KIND_LABEL[z.kind], name: z.name, communes: z.communes,
        effective_on: z.effective_on, radius_m: z.radius_m, area_ha: z.area_ha, outbreak_ref: z.outbreak_ref, status: z.status },
    })),
  };
}

/** Đọc hình đầu tiên (Polygon/MultiPolygon/LineString) trong tệp GeoJSON tải lên. */
export function geometryFromGeoJSON(text: string): Geometry | null {
  try {
    const v = JSON.parse(text) as { type?: string; geometry?: Geometry; features?: { geometry?: Geometry }[] };
    const cands: (Geometry | undefined)[] =
      v.type === "FeatureCollection" ? (v.features ?? []).map((f) => f.geometry) : v.type === "Feature" ? [v.geometry] : [v as unknown as Geometry];
    return cands.find((g) => g && ["Polygon", "MultiPolygon", "LineString"].includes(g.type)) ?? null;
  } catch {
    return null;
  }
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createCntsMapApi(api: ApiClient) {
  return {
    layers: (date?: string): Promise<MapLayers> => unwrap(api.GET("/v1/extension/cnts/map/layers", { params: { query: date ? { date } : {} } })),
    zone: (ref: string): Promise<ZoneDetail> => unwrap(api.GET("/v1/extension/cnts/map/zones/{ref}", { params: { path: { ref } } })),
    contents: (ref: string, date?: string): Promise<ZoneContents> =>
      unwrap(api.GET("/v1/extension/cnts/map/zones/{ref}/contents", { params: { path: { ref }, query: date ? { date } : {} } })),
    createZone: (body: ZoneSave): Promise<Zone> => unwrap(api.POST("/v1/extension/cnts/map/zones", { params: idem(), body })),
    updateZone: (ref: string, body: ZoneSave): Promise<Zone> =>
      unwrap(api.POST("/v1/extension/cnts/map/zones/{ref}", { params: { path: { ref }, ...idem() }, body })),
    zoneStatus: (ref: string, retire: boolean, base_version: number, reason: string): Promise<Zone> =>
      retire
        ? unwrap(api.POST("/v1/extension/cnts/map/zones/{ref}/retire", { params: { path: { ref }, ...idem() }, body: { base_version, reason } }))
        : unwrap(api.POST("/v1/extension/cnts/map/zones/{ref}/restore", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    fromOutbreak: async (outbreak_ref: string, radii?: Partial<Record<"vung_dich" | "vung_uy_hiep" | "vung_dem", number>>): Promise<Zone[]> =>
      (await unwrap(api.POST("/v1/extension/cnts/map/zones/from-outbreak", { params: idem(), body: { outbreak_ref, ...(radii ? { radii } : {}) } }))).items,
  };
}

export type CntsMapApi = ReturnType<typeof createCntsMapApi>;
