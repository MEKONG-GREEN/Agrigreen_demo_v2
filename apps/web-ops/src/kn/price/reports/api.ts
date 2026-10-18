// Client có kiểu cho Báo cáo giá (W11.77, cap/price — /v1/price/reports/*) trên openapi-fetch sinh từ hợp đồng: Tiến độ
// nhập giá (FN-TI-02.01), Bảng giá tổng hợp (FN-TW-01.01), Biến động giá (FN-TW-01.02), Dashboard theo vùng (FN-TW-01.03),
// Thống kê phiếu (FN-TI-02.08) + khóa tháng (Q-085). Giá, % là chuỗi decimal (R-10: không float).
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import type { components } from "@/api/generated/application";
import { formatDecimalVi } from "@/kn/grid/decimal";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type PriceProgress = S["PriceProgress"];
export type PriceProgressRow = S["PriceProgressRow"];
export type PriceSummary = S["PriceSummary"];
export type PriceSummaryLine = S["PriceSummaryLine"];
export type PricePointPrice = S["PricePointPrice"];
export type PriceMovement = S["PriceMovement"];
export type PriceMovementLine = S["PriceMovementLine"];
export type PriceDashboard = S["PriceDashboard"];
export type PriceDashboardLine = S["PriceDashboardLine"];
export type PriceStats = S["PriceStats"];
export type PriceStatRow = S["PriceStatRow"];
export type PriceStatDay = S["PriceStatDay"];
export type PriceTrend = S["PriceTrend"];

/** Tăng / Giảm / Đi ngang (CT-04): mũi tên xanh tăng, đỏ giảm (thiết kế mẫu FN-TW-01.01). */
export const TREND_LABEL: Record<PriceTrend, string> = {
  up: "▲ Tăng",
  down: "▼ Giảm",
  flat: "→ Đi ngang",
  "": "—",
};
export const TREND_TONE: Record<
  PriceTrend,
  "neutral" | "ok" | "warn" | "bad" | "info"
> = { up: "ok", down: "bad", flat: "neutral", "": "neutral" };

/** % có dấu kiểu Việt ("+1,56%"), rỗng → "—". */
export function signedPct(v: string | null | undefined): string {
  if (v === null || v === undefined || v === "") return "—";
  const s = formatDecimalVi(v, { maxDecimals: 2 });
  return (v.startsWith("-") ? s : `+${s}`) + "%";
}

type Blob2 = { blob: Blob; name: string };

export type ProgressQuery = {
  day?: string;
  region?: string;
  status?: string;
  point?: string;
  mine?: boolean;
};
export type SummaryQuery = {
  day?: string;
  region?: string;
  group?: string;
  view?: "item" | "point";
  item?: string;
};
export type MovementQuery = {
  period?: "day" | "week" | "month";
  day?: string;
  region?: string;
  point?: string;
  group?: string;
  item?: string;
};
export type DashboardQuery = {
  day?: string;
  group?: string;
  item?: string;
  region?: string;
  all?: boolean;
};
export type StatsQuery = {
  month?: string;
  from?: string;
  to?: string;
  region?: string;
  point?: string;
};

function clean<T extends Record<string, unknown>>(q: T): T {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(q))
    if (v !== undefined && v !== "" && v !== false) out[k] = v;
  return out as T;
}

export function createPriceReportApi(api: ApiClient) {
  const file = async (
    p: Promise<{ data?: unknown; error?: unknown; response: Response }>,
    fallback: string,
  ): Promise<Blob2> => {
    const res = await p;
    if (!res.response.ok) throw await parseProblemResponse(res.response);
    return {
      blob: res.data as Blob,
      name: filenameFromDisposition(
        res.response.headers.get("Content-Disposition"),
        fallback,
      ),
    };
  };
  return {
    progress: (q: ProgressQuery) =>
      unwrap(
        api.GET("/v1/price/reports/progress", { params: { query: clean(q) } }),
      ),
    progressExport: (q: ProgressQuery) =>
      file(
        api.GET("/v1/price/reports/progress/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        "tien-do-nhap-gia.xlsx",
      ),
    summary: (q: SummaryQuery) =>
      unwrap(
        api.GET("/v1/price/reports/summary", { params: { query: clean(q) } }),
      ),
    summaryExport: (q: SummaryQuery) =>
      file(
        api.GET("/v1/price/reports/summary/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        "bang-gia-tong-hop.xlsx",
      ),
    movement: (q: MovementQuery) =>
      unwrap(
        api.GET("/v1/price/reports/movement", { params: { query: clean(q) } }),
      ),
    movementExport: (q: MovementQuery & { format: "xlsx" | "pdf" }) =>
      file(
        api.GET("/v1/price/reports/movement/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        `bien-dong-gia.${q.format}`,
      ),
    dashboard: (q: DashboardQuery) =>
      unwrap(
        api.GET("/v1/price/reports/dashboard", { params: { query: clean(q) } }),
      ),
    dashboardExport: (q: DashboardQuery) =>
      file(
        api.GET("/v1/price/reports/dashboard/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        "dashboard-gia-theo-vung.xlsx",
      ),
    stats: (q: StatsQuery) =>
      unwrap(
        api.GET("/v1/price/reports/stats", { params: { query: clean(q) } }),
      ),
    statsExport: (q: StatsQuery) =>
      file(
        api.GET("/v1/price/reports/stats/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        "thong-ke-phieu-bao-gia.xlsx",
      ),
    statsDetail: (q: StatsQuery & { officer: string }) =>
      unwrap(
        api.GET("/v1/price/reports/stats/detail", {
          params: { query: clean(q) },
        }),
      ),
    closeMonth: (month: string, seq: number) =>
      unwrap(
        api.POST("/v1/price/reports/stats/close", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { month, seq },
        }),
      ),
    reopenMonth: (month: string, seq: number, reason: string) =>
      unwrap(
        api.POST("/v1/price/reports/stats/reopen", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { month, seq, reason },
        }),
      ),
  };
}

export type PriceReportApi = ReturnType<typeof createPriceReportApi>;
