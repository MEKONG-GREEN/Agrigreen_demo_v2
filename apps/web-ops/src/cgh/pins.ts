import type { MechCoverageItem, MechGroup } from "@/api/types";
import type { FeatureCollection, MultiPolygonGeometry, PointGeometry, PolygonGeometry, Position } from "@/lib/geojson";
import { COVERAGE_STYLES, fromMechStatus, type CoverageStatus } from "./coverage";

/**
 * Ghim HTX trên bản đồ CGH (BRD CGH FN-08 B2/B4/B5). Mức đáp ứng lấy từ KẾT QUẢ CÂN ĐỐI ĐÃ LƯU cấp HTX
 * (GET /v1/mechanization/coverage?level=cooperative); vị trí lấy từ hồ sơ HTX (pf/party `location`).
 * HTX chưa có toạ độ (`location_provisional`) → đặt ở tâm xã, nhãn "vị trí tạm".
 */
export interface CoopPin {
  ref: string;
  code: string | null;
  name: string;
  communeCode: string | null;
  communeName: string | null;
  provinceCode: string | null;
  lng: number;
  lat: number;
  provisional: boolean;
  status: CoverageStatus;
  statusLabel: string;
  color: string;
  /** Chuỗi tra cứu đã bỏ dấu, chữ thường: tên, Mã HTX, tên xã, mã xã. */
  search: string;
}

/** Thông tin vị trí của hồ sơ HTX (tập con của Cooperative). */
export interface CoopLocation {
  ref: string;
  code?: string | null;
  admin_unit_code?: string | null;
  commune_name?: string | null;
  location: { lat: string; lon: string } | null;
  location_provisional: boolean;
}

/** Bỏ dấu tiếng Việt, chữ thường — tra cứu không phân biệt dấu (như pf/content). */
export function normalizeVi(s: string): string {
  return s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").toLowerCase().trim();
}

function ringCentroid(ring: Position[] | undefined): [number, number] | null {
  if (!ring || ring.length === 0) return null;
  const pts = ring.length > 1 ? ring.slice(0, -1) : ring;
  let x = 0;
  let y = 0;
  for (const p of pts) {
    x += p[0] ?? 0;
    y += p[1] ?? 0;
  }
  return [x / pts.length, y / pts.length];
}

/** Tâm (xấp xỉ) của từng xã từ ranh giới hành chính — chỉ để đặt "vị trí tạm", không dùng tính toán. */
export function communeCentroids(fc: FeatureCollection<PolygonGeometry | MultiPolygonGeometry, { code: string }> | null | undefined): Map<string, [number, number]> {
  const out = new Map<string, [number, number]>();
  for (const f of fc?.features ?? []) {
    const ring = f.geometry.type === "Polygon" ? f.geometry.coordinates[0] : f.geometry.coordinates[0]?.[0];
    const c = ringCentroid(ring);
    if (c) out.set(f.properties.code, c);
  }
  return out;
}

function groupFor(item: MechCoverageItem, stage: string): MechGroup {
  return (stage ? item.stages[stage] : undefined) ?? item.overall;
}

/**
 * Dựng ghim từ kết quả cân đối cấp HTX + vị trí HTX. HTX không có toạ độ và không có tâm xã → không ghim
 * (trả trong `unplaced` để hiển thị số lượng).
 */
export function buildPins(
  items: readonly MechCoverageItem[],
  locations: ReadonlyMap<string, CoopLocation>,
  centroids: ReadonlyMap<string, [number, number]>,
  stage = "",
): { pins: CoopPin[]; unplaced: number } {
  const pins: CoopPin[] = [];
  let unplaced = 0;
  for (const it of items) {
    if (!it.cooperative_ref) continue;
    const loc = locations.get(it.cooperative_ref);
    const commune = it.commune_code ?? loc?.admin_unit_code ?? null;
    let lng: number | null = null;
    let lat: number | null = null;
    let provisional = true;
    if (loc?.location && !loc.location_provisional) {
      lng = Number(loc.location.lon);
      lat = Number(loc.location.lat);
      provisional = !Number.isFinite(lng) || !Number.isFinite(lat);
    }
    if (provisional) {
      const c = commune ? centroids.get(commune) : undefined;
      if (!c) {
        unplaced++;
        continue;
      }
      [lng, lat] = c;
    }
    const g = groupFor(it, stage);
    const status = fromMechStatus(g.status);
    const code = it.cooperative_code ?? loc?.code ?? null;
    const communeName = loc?.commune_name ?? null;
    pins.push({
      ref: it.cooperative_ref,
      code,
      name: it.name,
      communeCode: commune,
      communeName,
      provinceCode: it.province_code,
      lng: lng!,
      lat: lat!,
      provisional,
      status,
      statusLabel: g.status_label,
      color: g.color || COVERAGE_STYLES[status].fill,
      search: normalizeVi([it.name, code ?? "", communeName ?? "", commune ?? ""].join(" ")),
    });
  }
  return { pins, unplaced };
}

/** Tra cứu nhanh HTX theo tên / Mã HTX / xã: mọi từ khoá phải có trong chuỗi tra cứu. Tên bắt đầu bằng từ khoá lên trước. */
export function searchPins(pins: readonly CoopPin[], q: string, limit = 8): CoopPin[] {
  const terms = normalizeVi(q).split(/\s+/).filter(Boolean);
  if (terms.length === 0) return [];
  const out: CoopPin[] = [];
  for (const p of pins) {
    if (terms.every((t) => p.search.includes(t))) out.push(p);
  }
  const head = terms.join(" ");
  out.sort((a, b) => Number(!a.search.startsWith(head)) - Number(!b.search.startsWith(head)) || a.name.localeCompare(b.name, "vi"));
  return out.slice(0, limit);
}

/** Ghim → GeoJSON cho nguồn điểm (có cụm) của MapView. */
export function pinsToGeoJSON(pins: readonly CoopPin[]): FeatureCollection<PointGeometry, { id: string; name: string; color: string; provisional: boolean; status_label: string }> {
  return {
    type: "FeatureCollection",
    features: pins.map((p) => ({
      type: "Feature",
      id: p.ref,
      geometry: { type: "Point", coordinates: [p.lng, p.lat] },
      properties: { id: p.ref, name: p.name, color: p.color, provisional: p.provisional, status_label: p.statusLabel },
    })),
  };
}
