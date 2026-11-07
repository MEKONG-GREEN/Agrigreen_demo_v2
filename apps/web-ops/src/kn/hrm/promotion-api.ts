// Client có kiểu cho Nâng bậc lương (W11.25, dom/hrm — /v1/hrm/promotion*, /v1/hrm/staff/{id}/ratings): danh sách
// nâng bậc NL01–NL04 (FN-09), xuất Mẫu số 1/2/4, sổ quyết định (FN-10). Họ tên chỉ đi qua bộ nhớ của màn.
import { unwrap, type ApiClient } from "@/api/client";
import { currentIdempotentRun, newIdempotencyKey, withIdempotentOperation } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";
import { prepareHrmFile } from "./upload-file";

type S = components["schemas"];
export type PromoKind = S["HrmPromoKind"];
export type PromoStatus = S["HrmPromoStatus"];
export type PromoCond = S["HrmPromoCond"];
export type PromoCase = S["HrmPromoCase"];
export type PromoCaseDetail = S["HrmPromoCaseDetail"];
export type PromotionList = S["HrmPromotionList"];
export type PromoExportInput = S["HrmPromoExportInput"];
export type PromoExported = S["HrmPromoExported"];
export type PromoExportSummary = S["HrmPromoExportSummary"];
export type PromoExportDetail = S["HrmPromoExportDetail"];
export type Decision = S["HrmDecision"];
export type DecisionInput = S["HrmDecisionInput"];
export type AnnualRating = S["HrmAnnualRating"];

export const KIND_LABEL: Record<PromoKind, string> = {
  nltx: "Thường xuyên",
  tnvk: "Vượt khung",
  nlth: "Trước hạn",
};
export const KIND_FORM: Record<PromoKind, string> = {
  nltx: "Mẫu số 1 (NLTX)",
  nlth: "Mẫu số 2 (NLTH)",
  tnvk: "Mẫu số 4 (TNVK)",
};
export const FORM_LABEL: Record<string, string> = {
  mau1: "Mẫu số 1 · NLTX",
  mau2: "Mẫu số 2 · NLTH",
  mau4: "Mẫu số 4 · TNVK",
};
export const STATUS_LABEL: Record<PromoStatus, string> = {
  pending: "Chưa xử lý",
  exported: "Chờ quyết định",
  decided: "Đã có quyết định",
};
export const COND_LABEL: Record<PromoCond, string> = {
  ok: "Đủ",
  review: "Cần xem xét",
  fail: "Chưa đủ",
};
export const DECISION_KIND_LABEL: Record<PromoKind, string> = {
  nltx: "Nâng bậc thường xuyên",
  nlth: "Nâng bậc trước hạn",
  tnvk: "Phụ cấp thâm niên vượt khung",
};
export const ISSUERS = [
  "Sở NN&MT",
  "UBND tỉnh An Giang",
  "TTKN tỉnh An Giang",
];
export const RATINGS = ["HTXSNV", "HTTNV", "HTNV", "KHTNV"];

/** Hệ số "3.66" → "3,66" (mẫu Sở dùng dấu phẩy). */
export function vnNum(s: string | null | undefined): string {
  return (s ?? "").replace(".", ",");
}

/** Mô tả hiện tại → sau nâng của một kỳ. */
export function fromTo(c: {
  kind: PromoKind;
  from_step: number;
  to_step: number;
  from_coeff: string;
  to_coeff: string;
  pct_from?: string | null;
  pct_to?: string | null;
}): [string, string] {
  if (c.kind === "tnvk")
    return [
      c.pct_from ? `TNVK ${c.pct_from}%` : "Chưa hưởng TNVK",
      `TNVK ${c.pct_to ?? ""}%`,
    ];
  return [
    `Bậc ${c.from_step} · ${vnNum(c.from_coeff)}`,
    `Bậc ${c.to_step} · ${vnNum(c.to_coeff)}`,
  ];
}

export function createPromotionApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  return {
    list: (query: {year?:number;month?:number;status?:PromoStatus;unit?:string;q?:string} = {}): Promise<PromotionList> =>
      unwrap(api.GET("/v1/hrm/promotion", {params:{query}})),
    detail: (key: string): Promise<PromoCaseDetail> =>
      unwrap(
        api.GET("/v1/hrm/promotion/cases/{key}", {
          params: { path: { key } },
        }),
      ),
    exportLists: (
      body: PromoExportInput,
    ): Promise<{ items: PromoExported[] }> =>
      unwrap(
        api.POST("/v1/hrm/promotion/exports", {
          params: { header: header() },
          body,
        }),
      ),
    exports: (): Promise<{ items: PromoExportSummary[] }> =>
      unwrap(api.GET("/v1/hrm/promotion/exports", {})),
    exportDetail: (id: string): Promise<PromoExportDetail> =>
      unwrap(
        api.GET("/v1/hrm/promotion/exports/{id}", {
          params: { path: { id } },
        }),
      ),
    /** Tải lại tệp (máy chủ dựng lại và so SHA-256; drift = nội dung khác bản gốc). */
    async file(
      id: string,
    ): Promise<{ blob: Blob; name: string; drift: boolean }> {
      const res = await api.GET("/v1/hrm/promotion/exports/{id}/file", {
        params: { path: { id } },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "MauSo.xlsx",
        ),
        drift: res.response.headers.get("X-Export-Drift") === "true",
      };
    },
    decisions: (q: {
      year?: number;
      kind?: PromoKind;
      issuer?: string;
      q?: string;
    }): Promise<{ items: Decision[] }> =>
      unwrap(api.GET("/v1/hrm/promotion/decisions", { params: { query: q } })),
    decision: (id: string): Promise<Decision> =>
      unwrap(
        api.GET("/v1/hrm/promotion/decisions/{id}", {
          params: { path: { id } },
        }),
      ),
    createDecision: (
      body: DecisionInput,
    ): Promise<{
      id: string;
      updated: number;
      returned: number;
      warnings: string[];
    }> =>
      unwrap(
        api.POST("/v1/hrm/promotion/decisions", {
          params: { header: header() },
          body,
        }),
      ),
    cancelDecision: (id: string, reason: string) =>
      unwrap(
        api.POST("/v1/hrm/promotion/decisions/{id}/cancel", {
          params: { header: header(), path: { id } },
          body: { reason },
        }),
      ),
    attachFile: async (id: string, file: File): Promise<{ doc_id: string }> => {
      const run = currentIdempotentRun();
      const mp = stableMultipart([["file", await prepareHrmFile(file)]]);
      const send = () => unwrap(
        api.POST("/v1/hrm/promotion/decisions/{id}/file", {
          params: { header: header(), path: { id } },
          headers: { "Content-Type": mp.contentType },
          body: { file: file as unknown as string },
          bodySerializer: () => mp.body,
        }),
      );
      return run ? withIdempotentOperation(run, send) : send();
    },
    async decisionBook(year?: number): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/hrm/promotion/decision-book", {
        params: { query: year ? { year } : {} },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "SoQuyetDinhNangBac.xlsx",
        ),
      };
    },
    setRating: (
      staff: string,
      body: { year: number; rating: string; source?: "manual" | "conversion"; reason?: string },
    ): Promise<AnnualRating> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/ratings", {
          params: { header: header(), path: { id: staff } },
          body: body as { year: number; rating: "" | "HTXSNV" | "HTTNV" | "HTNV" | "KHTNV" },
        }),
      ),
  };
}

export type PromotionApi = ReturnType<typeof createPromotionApi>;
