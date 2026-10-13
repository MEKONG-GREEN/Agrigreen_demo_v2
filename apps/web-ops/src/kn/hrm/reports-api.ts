// Client có kiểu cho Báo cáo Sở NS01–NS06 và Dashboard nhân sự (W11.28, dom/hrm — /v1/hrm/reports*,
// /v1/hrm/report-exports/*, /v1/hrm/dashboard*). Dữ liệu cá nhân (họ tên, trường nhạy cảm) chỉ đi qua bộ nhớ của màn.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type HrmReportSummary = S["HrmReportSummary"];
export type HrmReportView = S["HrmReportView"];
export type HrmReportLog = S["HrmReportLog"];
export type HrmReportExported = S["HrmReportExported"];
export type HrmPersonBrief = S["HrmPersonBrief"];
export type HrmDashboard = S["HrmDashboard"];
export type ReportCode = S["HrmReportMeta"]["code"];

export const REPORT_CODES: ReportCode[] = [
  "NS01",
  "NS02",
  "NS03",
  "NS04",
  "NS05",
  "NS06",
];

export const PERIOD_LABEL: Record<string, string> = {
  month: "Hằng tháng",
  quarter: "Hằng quý",
  ad_hoc: "Khi Sở yêu cầu",
};
export const CHECK_STATUS_LABEL: Record<string, string> = {
  ready: "Sẵn sàng",
  warning: "Có cảnh báo",
  blocked: "Lỗi chặn",
};
export const AGE_LABEL: Record<string, string> = {
  age_le30: "Dưới 30",
  age_31_40: "31–40",
  age_41_50: "41–50",
  age_ge51: "Từ 51",
  unknown: "Chưa rõ",
};

export function createReportsApi(api: ApiClient) {
  return {
    list: (): Promise<{ items: HrmReportSummary[] }> =>
      unwrap(api.GET("/v1/hrm/reports")),
    preview: (code: ReportCode, at?: string): Promise<HrmReportView> =>
      unwrap(
        api.GET("/v1/hrm/reports/{code}", {
          params: { path: { code }, query: at ? { at } : {} },
        }),
      ),
    exportReport: (
      code: ReportCode,
      body: {
        as_of?: string;
        preparer_id?: string | null;
        signer_id?: string | null;
      },
    ): Promise<HrmReportExported> =>
      unwrap(
        api.POST("/v1/hrm/reports/{code}/export", {
          params: {
            path: { code },
            header: { "Idempotency-Key": newIdempotencyKey() },
          },
          body,
        }),
      ),
    history: (code: ReportCode): Promise<{ items: HrmReportLog[] }> =>
      unwrap(
        api.GET("/v1/hrm/reports/{code}/history", {
          params: { path: { code } },
        }),
      ),
    cell: (
      code: ReportCode,
      at: string,
      row: string,
      col: string,
    ): Promise<{ items: HrmPersonBrief[]; hidden: number }> =>
      unwrap(
        api.GET("/v1/hrm/reports/{code}/cells", {
          params: { path: { code }, query: { at, row, col } },
        }),
      ),
    /** Tải lại tệp (máy chủ dựng lại và so SHA-256; drift = nội dung khác bản gốc). */
    async file(
      id: string,
    ): Promise<{ blob: Blob; name: string; drift: boolean }> {
      const res = await api.GET("/v1/hrm/report-exports/{id}/file", {
        params: { path: { id } },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "bao-cao-so.xlsx",
        ),
        drift: res.response.headers.get("X-Export-Drift") === "true",
      };
    },
    dashboard: (at?: string): Promise<HrmDashboard> =>
      unwrap(
        api.GET("/v1/hrm/dashboard", { params: { query: at ? { at } : {} } }),
      ),
    members: (
      metric: string,
      at?: string,
    ): Promise<{ total: number; items: HrmPersonBrief[] }> =>
      unwrap(
        api.GET("/v1/hrm/dashboard/members", {
          params: { query: { metric, ...(at ? { at } : {}) } },
        }),
      ),
  };
}

export type ReportsApi = ReturnType<typeof createReportsApi>;
