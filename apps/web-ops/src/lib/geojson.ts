// Kiểu GeoJSON (RFC 7946, EPSG:4326) dùng cho lớp bản đồ phía client.
// Hợp đồng backend chỉ khai PolygonGeometry.coordinates là mảng chung — web thu hẹp kiểu khi vẽ.

/** [kinh độ, vĩ độ]. */
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

/** Ép hình từ API (type Polygon|MultiPolygon, coordinates: unknown[]) về kiểu vẽ được; hình lạ → null. */
export function toAreaGeometry(g: { type: "Polygon" | "MultiPolygon"; coordinates: unknown[] }): PolygonGeometry | MultiPolygonGeometry | null {
  if (!Array.isArray(g.coordinates) || g.coordinates.length === 0) return null;
  if (g.type === "Polygon") return { type: "Polygon", coordinates: g.coordinates as Position[][] };
  if (g.type === "MultiPolygon") return { type: "MultiPolygon", coordinates: g.coordinates as Position[][][] };
  return null;
}
