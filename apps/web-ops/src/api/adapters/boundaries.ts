// pf/geo — ranh giới hành chính theo cấp và đơn vị cha trong MỘT lời gọi (G-05):
// GET /v1/geo/admin-boundaries?level=province|commune&parent=. Đơn vị chưa nhập ranh giới nằm trong `missing`.
import type { ApiClient } from "../client";
import { unwrap } from "../client";
import { toAreaGeometry, type Feature, type FeatureCollection, type MultiPolygonGeometry, type PolygonGeometry } from "@/lib/geojson";

export interface BoundaryProps {
  code: string;
  name: string;
  level: "province" | "commune";
  parent_code: string | null;
  /** Chuỗi thập phân (R-10). */
  area_ha: string;
  [k: string]: unknown;
}

export type BoundaryCollection = FeatureCollection<PolygonGeometry | MultiPolygonGeometry, BoundaryProps>;

export interface BoundaryResult {
  collection: BoundaryCollection;
  /** Mã chưa có ranh giới. */
  missing: string[];
}

export type BoundaryQuery = { level: "province" } | { level: "commune"; parent: string };

export async function loadBoundaries(api: ApiClient, q: BoundaryQuery, zoom?: number): Promise<BoundaryResult> {
  const fc = await unwrap(
    api.GET("/v1/geo/admin-boundaries", { params: { query: { level: q.level, parent: q.level === "commune" ? q.parent : undefined, zoom } } }),
  );
  const features: Feature<PolygonGeometry | MultiPolygonGeometry, BoundaryProps>[] = [];
  const missing = [...fc.missing];
  for (const f of fc.features) {
    const geometry = toAreaGeometry(f.geometry);
    if (!geometry) {
      missing.push(f.properties.code);
      continue;
    }
    const p = f.properties;
    features.push({ type: "Feature", id: f.id, geometry, properties: { code: p.code, name: p.name, level: p.level, parent_code: p.parent_code, area_ha: p.area_ha } });
  }
  return { collection: { type: "FeatureCollection", features }, missing };
}

/** Gộp kết quả nhiều lời gọi (vd xã của nhiều tỉnh trong phạm vi). */
export function mergeBoundaries(parts: readonly BoundaryResult[]): BoundaryResult {
  return {
    collection: { type: "FeatureCollection", features: parts.flatMap((p) => p.collection.features) },
    missing: parts.flatMap((p) => p.missing),
  };
}
