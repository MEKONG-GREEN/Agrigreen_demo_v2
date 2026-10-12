// Client có kiểu cho Xã báo giá + phân công (W11.72, cap/price — /v1/price/regions*, points*, admin-units, officers) trên
// openapi-fetch sinh từ hợp đồng. Bearer, X-AG-Context và Idempotency-Key do middleware của createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type PriceRegion = S["PriceRegion"];
export type PriceRegionInput = S["PriceRegionInput"];
export type PricePoint = S["PricePoint"];
export type PricePointDetail = S["PricePointDetail"];
export type PricePointInput = S["PricePointInput"];
export type PriceOfficerInput = S["PriceOfficerInput"];
export type PriceAdminUnit = S["PriceAdminUnit"];
export type PriceOfficer = S["PriceOfficer"];
export type PriceStatus = S["PriceStatus"];
export type PriceWriteResult = S["PriceWriteResult"];

export interface PointQuery {
  at?: string;
  region?: string;
  state?: "active" | "pending" | "retired";
  mine?: boolean;
}

export const POINT_STATE_LABEL: Record<PricePoint["state"], string> = {
  active: "Hoạt động",
  pending: "Chờ hiệu lực",
  retired: "Ngưng",
};

export const OFFICER_ROLE_LABEL: Record<PriceOfficerInput["role"], string> = {
  xa: "Cán bộ Xã phụ trách chính",
  tinh: "Cán bộ Cấp Tỉnh",
};

/** "dd/mm/yyyy" từ yyyy-mm-dd. */
export function dmy(day: string | null | undefined): string {
  if (!day) return "—";
  const [y, m, d] = day.split("-");
  return `${d}/${m}/${y}`;
}

/** Nhãn cán bộ phụ trách: tên (danh bạ) hoặc mã tham chiếu rút gọn; null → nhãn trống của BRD. */
export function officerLabel(
  ref: string | null,
  name: string | null,
  empty: string,
): string {
  if (!ref) return empty;
  return name ?? `${ref.slice(0, 8)}…`;
}

/** Kiểm ở trình duyệt trước khi gửi (máy chủ vẫn kiểm lại). */
export function pointProblems(
  b: PricePointInput,
  mode: "create" | "revise" | "retire",
): string[] {
  const out: string[] = [];
  if (mode === "create" && !b.admin_unit_code) out.push("Chọn ĐVHC (xã)");
  if (mode === "create" && !b.region_code) out.push("Chọn nhóm vùng");
  if (mode === "retire" && !(b.reason ?? b.note ?? "").trim())
    out.push("Nhập lý do ngưng");
  return out;
}

export function createPricePointApi(api: ApiClient) {
  const at = (d?: string) => (d ? { at: d } : {});
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  return {
    status: (): Promise<PriceStatus> =>
      unwrap(api.GET("/v1/price/status", {})),
    regions: (day?: string): Promise<{ regions: PriceRegion[] }> =>
      unwrap(api.GET("/v1/price/regions", { params: { query: at(day) } })),
    createRegion: (body: PriceRegionInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/regions", { params: { header: header() }, body }),
      ),
    reviseRegion: (
      code: string,
      body: PriceRegionInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/regions/{code}/revise", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    retireRegion: (code: string): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/regions/{code}/retire", {
          params: { header: header(), path: { code } },
        }),
      ),
    points: (q: PointQuery): Promise<{ points: PricePoint[] }> =>
      unwrap(api.GET("/v1/price/points", { params: { query: q } })),
    point: (code: string, day?: string): Promise<PricePointDetail> =>
      unwrap(
        api.GET("/v1/price/points/{code}", {
          params: { path: { code }, query: at(day) },
        }),
      ),
    createPoint: (body: PricePointInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/points", { params: { header: header() }, body }),
      ),
    revisePoint: (
      code: string,
      body: PricePointInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/points/{code}/revise", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    retirePoint: (
      code: string,
      body: PricePointInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/points/{code}/retire", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    assignOfficer: (
      code: string,
      body: PriceOfficerInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/points/{code}/officers", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    adminUnits: (day?: string): Promise<{ admin_units: PriceAdminUnit[] }> =>
      unwrap(api.GET("/v1/price/admin-units", { params: { query: at(day) } })),
    officers: (
      level: "XA" | "TINH",
    ): Promise<{ officers: PriceOfficer[]; directory: boolean }> =>
      unwrap(api.GET("/v1/price/officers", { params: { query: { level } } })),
  };
}

export type PricePointApi = ReturnType<typeof createPricePointApi>;
