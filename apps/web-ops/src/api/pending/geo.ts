// ĐANG CHỜ HỢP ĐỒNG — module pf/geo (thửa, ranh giới hành chính, lớp bản đồ).
import type { DateTime, Decimal, PageQuery, Uuid } from "./common";

/** [kinh độ, vĩ độ] theo GeoJSON (RFC 7946), EPSG:4326. */
export type Position = number[];

export interface PolygonGeometry {
  type: "Polygon";
  coordinates: Position[][];
}
export interface MultiPolygonGeometry {
  type: "MultiPolygon";
  coordinates: Position[][][];
}
export interface PointGeometry {
  type: "Point";
  coordinates: Position;
}

export interface Feature<G, P> {
  type: "Feature";
  id?: string | number;
  geometry: G;
  properties: P;
}

export interface FeatureCollection<G, P> {
  type: "FeatureCollection";
  features: Feature<G, P>[];
}

/** GET /v1/admin-units/boundaries?level=province|commune — application/geo+json, EPSG:4326. */
export type AdminBoundaryCollection = FeatureCollection<
  PolygonGeometry | MultiPolygonGeometry,
  { code: string; name: string; level: "province" | "commune"; parent_code: string | null }
>;

/** GET /v1/cooperatives/locations — điểm trụ sở HTX trong phạm vi. */
export type CooperativeLocationCollection = FeatureCollection<
  PointGeometry,
  { id: Uuid; code: string; name: string; province_code: string; commune_code: string; area_ha: Decimal }
>;

/** GET /v1/plots */
export interface Plot {
  id: Uuid;
  /** Mã hiển thị, vd SPOT-82-000123 (nhãn, không mang nghĩa địa lý bất biến — ADR-0009). */
  label: string;
  cooperative_id: Uuid | null;
  commune_code: string;
  area_ha: Decimal;
  digitized: boolean;
  crop: string | null;
  updated_at: DateTime;
}

export interface PlotQuery extends PageQuery {
  cooperative_id?: Uuid;
  admin_unit?: string;
}
