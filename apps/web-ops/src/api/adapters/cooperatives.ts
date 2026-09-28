// pf/party + pf/geo — HTX, nông hộ, thửa của HTX.
import type { ApiClient, ApiPaths } from "../client";
import { unwrap } from "../client";
import { collectPages } from "../pagination";
import type { Cooperative, CooperativeItem, HouseholdItem, Page, PlotAssignResult, PlotFeature } from "../types";
import type { FeatureCollection, PointGeometry } from "@/lib/geojson";

export interface CoopPointProps {
  id: string;
  code: string;
  name: string;
  commune_code: string | null;
  [k: string]: unknown;
}

export type CoopPointCollection = FeatureCollection<PointGeometry, CoopPointProps>;

/**
 * Điểm trụ sở HTX cho bản đồ. Toạ độ backend là chuỗi thập phân (Point.lat/lon, R-10);
 * chỉ đổi sang number ở đây để VẼ. HTX chưa có toạ độ (location_provisional) không có điểm.
 */
export function cooperativePoints(coops: readonly Cooperative[]): CoopPointCollection {
  return {
    type: "FeatureCollection",
    features: coops.flatMap((c) => {
      if (!c.location || c.location_provisional) return [];
      const lon = Number(c.location.lon);
      const lat = Number(c.location.lat);
      if (!Number.isFinite(lon) || !Number.isFinite(lat)) return [];
      return [
        {
          type: "Feature" as const,
          id: c.ref,
          geometry: { type: "Point" as const, coordinates: [lon, lat] },
          properties: { id: c.ref, code: c.code, name: c.name, commune_code: c.admin_unit_code },
        },
      ];
    }),
  };
}

type CoopQueryContract = NonNullable<ApiPaths["/v1/cooperatives"]["get"]["parameters"]["query"]>;

/**
 * Bộ lọc của GET /v1/cooperatives. `admin_unit` (mã tỉnh hoặc xã, G-03) backend đã nhận
 * (internal/platform/party/handler/registry.go) nhưng application.yaml chưa khai → thêm ở kiểu mở rộng này.
 */
export type CoopQuery = CoopQueryContract & { admin_unit?: string };

/** Một trang HTX kèm trường đọc bổ sung G-03 (tên xã/tỉnh, số hộ, số thửa số hoá). */
export function cooperativesPage(api: ApiClient, q: CoopQuery): Promise<Page<CooperativeItem>> {
  const query: CoopQueryContract = { ...q };
  return unwrap(api.GET("/v1/cooperatives", { params: { query } })) as Promise<Page<CooperativeItem>>;
}

export function getCooperative(api: ApiClient, ref: string): Promise<CooperativeItem> {
  return unwrap(api.GET("/v1/cooperatives/{ref}", { params: { path: { ref } } })) as Promise<CooperativeItem>;
}

/** Nông hộ thành viên kèm trạng thái đồng ý theo mục đích (G-04). */
export function householdsPage(api: ApiClient, ref: string, cursor: string | undefined, limit = 20): Promise<Page<HouseholdItem>> {
  return unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref }, query: { cursor, limit } } })) as Promise<Page<HouseholdItem>>;
}

/** Mọi HTX trong phạm vi (gom trang cursor, tối đa `max`). */
export function listAllCooperatives(api: ApiClient, opts: { includeInactive?: boolean; adminUnit?: string; max?: number } = {}): Promise<CooperativeItem[]> {
  return collectPages(
    (cursor) => cooperativesPage(api, { include_inactive: opts.includeInactive, admin_unit: opts.adminUnit, cursor, limit: 200 }),
    opts.max ?? 2000,
  );
}

/** "Xã A, Đồng Tháp" từ trường G-03; thiếu tên thì hiện mã. */
export function coopPlace(c: Pick<CooperativeItem, "admin_unit_code" | "commune_name" | "province_name">): string {
  const commune = c.commune_name ?? c.admin_unit_code ?? "—";
  return c.province_name ? `${commune}, ${c.province_name}` : commune;
}

/**
 * GET /v1/cooperatives/{ref}/plots trả FeatureCollection có `next_cursor` (không phải {items}).
 * Đổi về Page<PlotFeature> để dùng chung usePagedList / collectPages.
 */
export async function cooperativePlotsPage(api: ApiClient, ref: string, cursor: string | undefined, limit = 50): Promise<Page<PlotFeature>> {
  const fc = await unwrap(api.GET("/v1/cooperatives/{ref}/plots", { params: { path: { ref }, query: { cursor, limit } } }));
  return { items: fc.features, next_cursor: fc.next_cursor };
}

/**
 * Gán thửa cho HTX (POST /v1/plots/{ref}/assign). Chồng lấn cùng HTX/thửa nháp → 409
 * geo.overlap_needs_confirm: người dùng xác nhận thì gửi lại với `confirm = true`. Thân khác nhau nên
 * lần xác nhận dùng khóa Idempotency-Key MỚI (cùng khóa + khác thân → idempotency_key_reused).
 * geo.overlap_cross_coop (HTX khác) không bỏ qua được.
 */
export function assignPlot(
  api: ApiClient,
  ref: string,
  input: { cooperative_ref: string | null; reason?: string; confirm?: boolean },
  idemKey: string,
): Promise<PlotAssignResult> {
  return unwrap(
    api.POST("/v1/plots/{ref}/assign", {
      params: { path: { ref }, header: { "Idempotency-Key": idemKey } },
      body: { cooperative_ref: input.cooperative_ref, reason: input.reason, confirm_overlap: input.confirm ?? false },
    }),
  );
}
/** Khung bao "minLon,minLat,maxLon,maxLat" của một hình GeoJSON (chỉ để tạo tham số truy vấn). */
export function bboxOf(coordinates: unknown): string | null {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const visit = (v: unknown): void => {
    if (!Array.isArray(v)) return;
    if (typeof v[0] === "number" && typeof v[1] === "number") {
      minX = Math.min(minX, v[0]); maxX = Math.max(maxX, v[0]);
      minY = Math.min(minY, v[1]); maxY = Math.max(maxY, v[1]);
      return;
    }
    v.forEach(visit);
  };
  visit(coordinates);
  if (!Number.isFinite(minX)) return null;
  return [minX, minY, maxX, maxY].map((n) => n.toFixed(6)).join(",");
}

/**
 * Thửa NHÁP (chưa gán HTX) trong địa giới một xã: ranh giới xã (GET /v1/geo/admin-boundaries/{code})
 * → khung bao → GET /v1/plots?bbox (tối đa 1.000 thửa, `truncated` khi còn nữa).
 */
export async function draftPlotsInCommune(api: ApiClient, communeCode: string): Promise<{ items: PlotFeature[]; truncated: boolean }> {
  const boundary = await unwrap(api.GET("/v1/geo/admin-boundaries/{code}", { params: { path: { code: communeCode } } }));
  const bbox = bboxOf(boundary.geometry.coordinates);
  if (!bbox) return { items: [], truncated: false };
  const fc = await unwrap(api.GET("/v1/plots", { params: { query: { bbox, zoom: 17 } } }));
  return {
    items: fc.features.filter((f) => f.properties.status === "draft" && f.properties.cooperative_ref === null && f.properties.commune_code === communeCode),
    truncated: fc.truncated,
  };
}