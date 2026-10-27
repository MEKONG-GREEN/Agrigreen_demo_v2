// Hình học thửa ruộng phía client của KN (FN-XA-03.05, TT-11 #104) — chỉ đa giác.
// KN là hệ độc lập (ADR-0055, knboundary web-kn-import-app): không import thư viện vẽ của ứng dụng GIS (`@/gis/*`);
// như bản đồ CNTS (`kn/cnts/map/api.ts`), KN giữ phần hình học nhỏ mình cần và vẽ trên `@/components/LayerMap` dùng chung.
// Số tính ở web chỉ để xem trước; máy chủ tính lại trên hình (PostGIS geography).

/** [kinh độ, vĩ độ]. */
export type LngLat = [number, number];

/** Bán kính Trái Đất trung bình (m) — như turf/PostGIS sphere. */
const EARTH_RADIUS_M = 6_371_008.8;
const rad = (d: number) => (d * Math.PI) / 180;
const samePoint = (a: LngLat, b: LngLat) => a[0] === b[0] && a[1] === b[1];

/** Khép vòng (thêm điểm đầu vào cuối nếu chưa khép). */
export function closeRing(ring: readonly LngLat[]): LngLat[] {
  if (ring.length === 0) return [];
  const out = ring.slice() as LngLat[];
  if (!samePoint(out[0]!, out[out.length - 1]!)) out.push(out[0]!);
  return out;
}

/** Diện tích trắc địa của một vòng (m², dương) — công thức phần dư cầu (Chamberlain & Duquette 2007, như turf.area). */
function ringAreaM2(ring: readonly LngLat[]): number {
  const pts = closeRing(ring);
  const n = pts.length;
  if (n < 4) return 0;
  let total = 0;
  for (let i = 0; i < n - 1; i++) {
    const lo = i === n - 2 ? pts[0]! : pts[i + 1]!;
    const prev = i === 0 ? pts[n - 2]! : pts[i - 1]!;
    total += (rad(lo[0]) - rad(prev[0])) * Math.sin(rad(pts[i]![1]));
  }
  return Math.abs((total * EARTH_RADIUS_M * EARTH_RADIUS_M) / 2);
}

/** Diện tích đa giác (vòng ngoài trừ lỗ), m². */
export function polygonAreaM2(rings: readonly (readonly LngLat[])[]): number {
  if (rings.length === 0) return 0;
  let a = ringAreaM2(rings[0]!);
  for (const hole of rings.slice(1)) a -= ringAreaM2(hole);
  return Math.max(0, a);
}

export const m2ToHa = (m2: number) => m2 / 10_000;

/** Khoảng cách Haversine (m). */
function haversineM(a: LngLat, b: LngLat): number {
  const h = Math.sin(rad(b[1] - a[1]) / 2) ** 2 + Math.cos(rad(a[1])) * Math.cos(rad(b[1])) * Math.sin(rad(b[0] - a[0]) / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Chu vi trắc địa của nét vẽ đa giác (m). */
export function perimeterM(coords: readonly LngLat[]): number {
  const ring = closeRing(coords);
  let s = 0;
  for (let i = 1; i < ring.length; i++) s += haversineM(ring[i - 1]!, ring[i]!);
  return s;
}

/** Kiểm nét vẽ đa giác đủ điều kiện lưu; trả thông báo tiếng Việt hoặc null. */
export function validatePolygon(coords: readonly LngLat[]): string | null {
  if (new Set(coords.map((c) => `${c[0]},${c[1]}`)).size < 3) return "Vùng cần ít nhất 3 điểm phân biệt.";
  if (polygonAreaM2([coords]) <= 0) return "Các điểm thẳng hàng — không tạo thành vùng.";
  return null;
}

const round7 = (v: number) => Math.round(v * 1e7) / 1e7;

/** Polygon GeoJSON từ nét vẽ (toạ độ làm tròn 7 chữ số lẻ ≈ 1 cm, vòng khép). */
export function polygonGeometry(coords: readonly LngLat[]): { type: "Polygon"; coordinates: LngLat[][] } {
  return { type: "Polygon", coordinates: [closeRing(coords.map((c) => [round7(c[0]), round7(c[1])] as LngLat))] };
}

/** Khung bao của một dãy toạ độ (để bay tới thửa). */
export function boundsOf(coords: readonly LngLat[]): [LngLat, LngLat] | undefined {
  if (coords.length === 0) return undefined;
  const xs = coords.map((c) => c[0]);
  const ys = coords.map((c) => c[1]);
  return [
    [Math.min(...xs), Math.min(...ys)],
    [Math.max(...xs), Math.max(...ys)],
  ];
}

/** Đọc "kinh độ, vĩ độ" (dấu chấm thập phân; ngăn cách bằng dấu phẩy, chấm phẩy hoặc khoảng trắng); sai → null. */
export function parseLngLat(text: string): LngLat | null {
  const m = /^\s*(-?\d+(?:\.\d+)?)\s*(?:[,;]\s*|\s+)(-?\d+(?:\.\d+)?)\s*$/.exec(text);
  if (!m) return null;
  const lng = Number(m[1]);
  const lat = Number(m[2]);
  if (!Number.isFinite(lng) || !Number.isFinite(lat) || Math.abs(lng) > 180 || Math.abs(lat) > 90) return null;
  return [lng, lat];
}

const nf = (digits: number) => new Intl.NumberFormat("vi-VN", { maximumFractionDigits: digits, minimumFractionDigits: 0 });

/** Diện tích + chu vi hiển thị khi vẽ (< 3 đỉnh → null). */
export function measurePolygon(coords: readonly LngLat[]): string | null {
  if (coords.length < 3) return null;
  const m2 = polygonAreaM2([coords]);
  const area = m2 < 1000 ? `${nf(0).format(m2)} m²` : `${nf(2).format(m2ToHa(m2))} ha`;
  const p = perimeterM(coords);
  const len = p < 1000 ? `${nf(0).format(p)} m` : `${nf(2).format(p / 1000)} km`;
  return `Diện tích: ${area} · Chu vi: ${len}`;
}
