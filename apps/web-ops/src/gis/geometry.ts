// Hình học trắc địa phía client (WGS84) cho công cụ vẽ GIS: chiều dài tuyến (GIS BR-41, US-ROAD-02/03,
// US-WATER-02/03 — "tự động tính theo hình vẽ, không nhập tay") và diện tích vùng (US-BND-02/05).
// Con số hiển thị lúc vẽ chỉ để tham khảo; số lưu là số backend tính trong CSDL (PostGIS geography).

/** [kinh độ, vĩ độ]. */
export type LngLat = [number, number];

/** Bán kính Trái Đất trung bình (m) — cùng hằng số với turf/PostGIS sphere. */
export const EARTH_RADIUS_M = 6_371_008.8;

const rad = (d: number) => (d * Math.PI) / 180;

/** Khoảng cách Haversine giữa hai điểm (m). */
export function haversineM(a: LngLat, b: LngLat): number {
  const dLat = rad(b[1] - a[1]);
  const dLng = rad(b[0] - a[0]);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a[1])) * Math.cos(rad(b[1])) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Chiều dài trắc địa của đường gấp khúc (m). */
export function lineLengthM(coords: readonly LngLat[]): number {
  let s = 0;
  for (let i = 1; i < coords.length; i++) s += haversineM(coords[i - 1]!, coords[i]!);
  return s;
}

/** Diện tích trắc địa của một vòng (m², dương) — công thức phần dư cầu (Chamberlain & Duquette 2007, như turf.area). */
export function ringAreaM2(ring: readonly LngLat[]): number {
  const pts = closeRing(ring);
  const n = pts.length;
  if (n < 4) return 0;
  let total = 0;
  for (let i = 0; i < n - 1; i++) {
    const lo = i === n - 2 ? pts[0]! : pts[i + 1]!;
    const mid = pts[i]!;
    const prev = i === 0 ? pts[n - 2]! : pts[i - 1]!;
    total += (rad(lo[0]) - rad(prev[0])) * Math.sin(rad(mid[1]));
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

function samePoint(a: LngLat, b: LngLat): boolean {
  return a[0] === b[0] && a[1] === b[1];
}

/** Khép vòng (thêm điểm đầu vào cuối nếu chưa khép). */
export function closeRing(ring: readonly LngLat[]): LngLat[] {
  if (ring.length === 0) return [];
  const out = ring.slice() as LngLat[];
  if (!samePoint(out[0]!, out[out.length - 1]!)) out.push(out[0]!);
  return out;
}

/** Số điểm phân biệt (bỏ điểm trùng liên tiếp và điểm khép vòng). */
export function distinctCount(coords: readonly LngLat[]): number {
  const seen = new Set(coords.map((c) => `${c[0]},${c[1]}`));
  return seen.size;
}

export type DrawMode = "point" | "line" | "polygon";

/** Kiểm hình vẽ đủ điều kiện lưu; trả thông báo tiếng Việt hoặc null. */
export function validateDraft(mode: DrawMode, coords: readonly LngLat[]): string | null {
  const n = distinctCount(coords);
  if (mode === "point") return n === 1 ? null : "Chọn đúng một điểm trên bản đồ.";
  if (mode === "line") return n >= 2 ? null : "Tuyến cần ít nhất 2 điểm phân biệt.";
  if (n < 3) return "Vùng cần ít nhất 3 điểm phân biệt.";
  if (polygonAreaM2([coords as LngLat[]]) <= 0) return "Các điểm thẳng hàng — không tạo thành vùng.";
  return null;
}

/** Hình GeoJSON từ nét vẽ (toạ độ làm tròn 7 chữ số lẻ ≈ 1 cm). */
export function draftGeometry(
  mode: DrawMode,
  coords: readonly LngLat[],
): { type: "Point"; coordinates: LngLat } | { type: "LineString"; coordinates: LngLat[] } | { type: "Polygon"; coordinates: LngLat[][] } {
  const r = coords.map((c) => [round7(c[0]), round7(c[1])] as LngLat);
  if (mode === "point") return { type: "Point", coordinates: r[0] ?? [0, 0] };
  if (mode === "line") return { type: "LineString", coordinates: r };
  return { type: "Polygon", coordinates: [closeRing(r)] };
}

const round7 = (v: number) => Math.round(v * 1e7) / 1e7;

/** Đọc "kinh độ, vĩ độ" (dấu chấm thập phân; ngăn cách bằng dấu phẩy, chấm phẩy hoặc khoảng trắng); sai → null. */
export function parseLngLat(text: string): LngLat | null {
  const m = /^\s*(-?\d+(?:\.\d+)?)\s*(?:[,;]\s*|\s+)(-?\d+(?:\.\d+)?)\s*$/.exec(text);
  if (!m) return null;
  const lng = Number(m[1]);
  const lat = Number(m[2]);
  if (!Number.isFinite(lng) || !Number.isFinite(lat) || Math.abs(lng) > 180 || Math.abs(lat) > 90) return null;
  return [lng, lat];
}

/** Khung nhìn "minLon,minLat,maxLon,maxLat" (6 chữ số lẻ) cho tham số `bbox`. */
export function bboxParam(b: { west: number; south: number; east: number; north: number }): string {
  const f = (v: number) => (Math.round(v * 1e6) / 1e6).toString();
  return [b.west, b.south, b.east, b.north].map(f).join(",");
}

/** Đọc bbox "minLon,minLat,maxLon,maxLat"; sai → null. */
export function parseBBox(s: string | null | undefined): [number, number, number, number] | null {
  if (!s) return null;
  const p = s.split(",").map(Number);
  if (p.length !== 4 || p.some((v) => !Number.isFinite(v))) return null;
  const [a, b, c, d] = p as [number, number, number, number];
  if (a > c || b > d) return null;
  return [a, b, c, d];
}

/** Có ít nhất một đỉnh nằm trong bbox (lọc lớp phía mock / cắt danh sách). */
export function touchesBBox(coords: readonly LngLat[], bb: readonly [number, number, number, number]): boolean {
  const [w, s, e, n] = bb;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const [x, y] of coords) {
    if (x >= w && x <= e && y >= s && y <= n) return true;
    minX = Math.min(minX, x); maxX = Math.max(maxX, x);
    minY = Math.min(minY, y); maxY = Math.max(maxY, y);
  }
  // Hình bao phủ cả khung (đường dài cắt ngang khung).
  return minX <= e && maxX >= w && minY <= n && maxY >= s;
}

const nf = (digits: number) => new Intl.NumberFormat("vi-VN", { maximumFractionDigits: digits, minimumFractionDigits: 0 });

/** Chiều dài hiển thị: < 1 km → "850 m", còn lại "12,35 km". */
export function formatLength(m: number | string | null | undefined): string {
  const v = typeof m === "string" ? Number(m) : m;
  if (v === null || v === undefined || !Number.isFinite(v)) return "—";
  return v < 1000 ? `${nf(0).format(v)} m` : `${nf(2).format(v / 1000)} km`;
}

/** Diện tích hiển thị theo ha (m² khi < 0,1 ha). */
export function formatAreaM2(m2: number): string {
  if (!Number.isFinite(m2)) return "—";
  return m2 < 1000 ? `${nf(0).format(m2)} m²` : `${nf(2).format(m2ToHa(m2))} ha`;
}

/** Tâm gần đúng (trung bình đỉnh, bỏ điểm khép vòng). */
export function centroidOf(coords: readonly LngLat[]): LngLat {
  const pts = coords.length > 1 && samePoint(coords[0]!, coords[coords.length - 1]!) ? coords.slice(0, -1) : coords;
  if (pts.length === 0) return [0, 0];
  return [pts.reduce((s, p) => s + p[0], 0) / pts.length, pts.reduce((s, p) => s + p[1], 0) / pts.length];
}
