// pf/geo — ranh giới hành chính. Backend chỉ có GET /v1/geo/admin-boundaries/{code} (một đơn vị mỗi lần);
// web gọi song song có giới hạn và gộp thành FeatureCollection. Đơn vị chưa nhập ranh giới (404) bị bỏ qua
// và liệt kê trong `missing`. Route gộp theo cấp/đơn vị cha: docs/dev/web-backend-gaps.md G-05.
import type { ApiClient } from "../client";
import { unwrap } from "../client";
import { ApiProblem } from "../problem";
import { toAreaGeometry, type Feature, type FeatureCollection, type MultiPolygonGeometry, type PolygonGeometry } from "@/lib/geojson";

export interface BoundaryProps {
  code: string;
  name: string;
  level: "province" | "commune";
  /** Chuỗi thập phân (R-10). */
  area_ha: string;
  [k: string]: unknown;
}

export type BoundaryCollection = FeatureCollection<PolygonGeometry | MultiPolygonGeometry, BoundaryProps>;

export interface BoundaryResult {
  collection: BoundaryCollection;
  /** Mã chưa có ranh giới (geo.admin_boundary_not_found). */
  missing: string[];
}

async function mapLimit<T, R>(items: readonly T[], limit: number, fn: (t: T) => Promise<R>): Promise<PromiseSettledResult<R>[]> {
  const out: PromiseSettledResult<R>[] = new Array(items.length);
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const i = next++;
      try {
        out[i] = { status: "fulfilled", value: await fn(items[i] as T) };
      } catch (e) {
        out[i] = { status: "rejected", reason: e };
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return out;
}

export async function loadBoundaries(api: ApiClient, codes: readonly string[], concurrency = 6): Promise<BoundaryResult> {
  const unique = Array.from(new Set(codes));
  const settled = await mapLimit(unique, concurrency, (code) =>
    unwrap(api.GET("/v1/geo/admin-boundaries/{code}", { params: { path: { code } } })),
  );
  const features: Feature<PolygonGeometry | MultiPolygonGeometry, BoundaryProps>[] = [];
  const missing: string[] = [];
  let firstError: unknown = null;
  settled.forEach((r, i) => {
    const code = unique[i] as string;
    if (r.status === "rejected") {
      if (r.reason instanceof ApiProblem && r.reason.status === 404) missing.push(code);
      else firstError ??= r.reason;
      return;
    }
    const geometry = toAreaGeometry(r.value.geometry);
    if (!geometry) {
      missing.push(code);
      return;
    }
    const p = r.value.properties;
    features.push({ type: "Feature", id: r.value.id, geometry, properties: { code: p.code, name: p.name, level: p.level, area_ha: p.area_ha } });
  });
  // Mọi đơn vị đều lỗi hệ thống (không phải 404) → báo lỗi để UI hiện problem + trace_id.
  if (features.length === 0 && firstError) throw firstError;
  return { collection: { type: "FeatureCollection", features }, missing };
}
