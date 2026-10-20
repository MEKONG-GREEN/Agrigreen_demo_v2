// Client có kiểu cho Điều tra giá thành sản xuất theo vụ (W11.78, cap/price — /v1/price/surveys/*, /v1/price/survey-report*)
// trên openapi-fetch sinh từ hợp đồng: đợt điều tra (FN-TI-02.10), nhập tệp hai bước (FN-TI-02.11), rà soát phiếu + chốt đợt
// (FN-TI-02.12), báo cáo giá thành (FN-TW-01.04). Tiền, % là chuỗi decimal (R-10: không float).
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import type { components } from "@/api/generated/application";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type SurveyRound = S["PriceSurveyRound"];
export type SurveyRoundInput = S["PriceSurveyRoundInput"];
export type SurveyRoundDetail = S["PriceSurveyRoundDetail"];
export type SurveyRoundAction = S["PriceSurveyRoundAction"];
export type SurveyImportPreview = S["PriceSurveyImportPreview"];
export type SurveyImportLine = S["PriceSurveyImportLine"];
export type SurveyImportResult = S["PriceSurveyImportResult"];
export type SurveyFormList = S["PriceSurveyFormList"];
export type SurveyFormRow = S["PriceSurveyFormRow"];
export type SurveyForm = S["PriceSurveyForm"];
export type SurveyFormAction = S["PriceSurveyFormAction"];
export type SurveyCostReport = S["PriceSurveyCostReport"];
export type SurveyCostBlock = S["PriceSurveyCostBlock"];
export type SurveyCostRow = S["PriceSurveyCostRow"];
export type Crop = "lua" | "rau_mau" | "nam" | "cay_an_trai";

export const CROPS: Crop[] = ["lua", "rau_mau", "nam", "cay_an_trai"];
export const CROP_LABEL: Record<string, string> = {
  lua: "Lúa",
  rau_mau: "Rau màu",
  nam: "Nấm",
  cay_an_trai: "Cây ăn trái",
};
export const SEASON_LABEL: Record<string, string> = {
  dong_xuan: "Đông Xuân",
  he_thu: "Hè Thu",
  thu_dong: "Thu Đông",
  khac: "Khác",
};
/** Trạng thái đợt (bảng 3.4). */
export const ROUND_STATUS: Record<
  string,
  { label: string; tone: "neutral" | "ok" | "warn" | "bad" | "info" }
> = {
  draft: { label: "Nháp", tone: "neutral" },
  collecting: { label: "Đang thu thập", tone: "ok" },
  reviewing: { label: "Đang rà soát", tone: "warn" },
  closed: { label: "Đã chốt", tone: "info" },
};
/** Trạng thái phiếu (bảng 3.4). */
export const FORM_STATUS: Record<
  string,
  { label: string; tone: "neutral" | "ok" | "warn" | "bad" | "info" }
> = {
  imported: { label: "Đã nhập", tone: "neutral" },
  verify: { label: "Cần xác minh", tone: "warn" },
  verified: { label: "Đã xác minh", tone: "info" },
  approved: { label: "Đã duyệt", tone: "ok" },
  rejected: { label: "Loại", tone: "bad" },
  deleted: { label: "Đã xóa", tone: "neutral" },
};
export const FLAG_LABEL: Record<string, string> = {
  high: "▲ Cao bất thường",
  low: "▼ Thấp bất thường",
  "": "",
};
export const COST_KEYS = [
  "seed",
  "fertilizer",
  "pesticide",
  "labor",
  "machine",
  "other",
] as const;
export const COST_LABEL: Record<string, string> = {
  seed: "Giống",
  fertilizer: "Phân bón",
  pesticide: "Thuốc BVTV",
  labor: "Nhân công",
  machine: "Máy móc",
  other: "Chi khác",
};

/** Phần tệp + ánh xạ của một lần nhập (bước 1–3). */
export interface ImportParts {
  file: File;
  mapping?: Record<string, number>;
  names?: { item?: Record<string, string>; commune?: Record<string, string> };
  sha256?: string;
  saveSynonyms?: boolean;
}

export type FormStatus = "imported" | "verify" | "verified" | "approved" | "rejected";
export type FormQuery = {
  crop?: Crop;
  item?: string;
  commune?: string;
  flag?: "flag" | "noflag";
  status?: FormStatus;
  limit?: number;
  offset?: number;
};
export type CostQuery = {
  round?: string;
  compare?: string;
  crop?: Crop;
  item?: string;
  region?: string;
  commune?: string;
};

function clean<T extends Record<string, unknown>>(q: T): T {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(q))
    if (v !== undefined && v !== "" && v !== false) out[k] = v;
  return out as T;
}

type Blob2 = { blob: Blob; name: string };

function multipart(p: ImportParts): FormData {
  const fd = new FormData();
  fd.append("file", p.file, p.file.name);
  if (p.mapping) fd.append("mapping", JSON.stringify(p.mapping));
  if (p.names) fd.append("names", JSON.stringify(p.names));
  if (p.sha256) fd.append("sha256", p.sha256);
  if (p.saveSynonyms) fd.append("save_synonyms", "true");
  return fd;
}

export function createSurveyApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
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
  const id = (v: string) => ({ path: { id: v } });
  return {
    rounds: () => unwrap(api.GET("/v1/price/surveys", {})),
    round: (r: string) =>
      unwrap(api.GET("/v1/price/surveys/{id}", { params: id(r) })),
    create: (body: SurveyRoundInput) =>
      unwrap(
        api.POST("/v1/price/surveys", { params: { header: header() }, body }),
      ),
    update: (r: string, body: SurveyRoundInput) =>
      unwrap(
        api.PUT("/v1/price/surveys/{id}", {
          params: { header: header(), path: { id: r } },
          body,
        }),
      ),
    act: (r: string, body: SurveyRoundAction) =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/actions", {
          params: { header: header(), path: { id: r } },
          body,
        }),
      ),
    clone: (r: string) =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/clone", {
          params: { header: header(), path: { id: r } },
        }),
      ),
    preview: (r: string, p: ImportParts): Promise<SurveyImportPreview> =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/imports/preview", {
          params: id(r),
          body: { file: "" },
          bodySerializer: () => multipart(p),
        }),
      ),
    commit: (r: string, p: ImportParts): Promise<SurveyImportResult> =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/imports", {
          params: { header: header(), path: { id: r } },
          body: { file: "" },
          bodySerializer: () => multipart(p),
        }),
      ),
    imports: (r: string) =>
      unwrap(api.GET("/v1/price/surveys/{id}/imports", { params: id(r) })),
    issues: (r: string, imp: string) =>
      file(
        api.GET("/v1/price/surveys/{id}/imports/{imp}/issues", {
          params: { path: { id: r, imp } },
          parseAs: "blob",
        }),
        "danh-sach-loi.xlsx",
      ),
    forms: (r: string, q: FormQuery) =>
      unwrap(
        api.GET("/v1/price/surveys/{id}/forms", {
          params: { path: { id: r }, query: clean(q) },
        }),
      ),
    formsExport: (r: string) =>
      file(
        api.GET("/v1/price/surveys/{id}/forms/export", {
          params: id(r),
          parseAs: "blob",
        }),
        "phieu-dieu-tra.xlsx",
      ),
    approveUnflagged: (r: string) =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/forms/approve-unflagged", {
          params: { header: header(), path: { id: r } },
        }),
      ),
    form: (r: string, f: string) =>
      unwrap(
        api.GET("/v1/price/surveys/{id}/forms/{form}", {
          params: { path: { id: r, form: f } },
        }),
      ),
    formAct: (r: string, f: string, body: SurveyFormAction) =>
      unwrap(
        api.POST("/v1/price/surveys/{id}/forms/{form}/actions", {
          params: { header: header(), path: { id: r, form: f } },
          body,
        }),
      ),
    report: (q: CostQuery) =>
      unwrap(api.GET("/v1/price/survey-report", { params: { query: clean(q) } })),
    reportExport: (q: CostQuery) =>
      file(
        api.GET("/v1/price/survey-report/export", {
          params: { query: clean(q) },
          parseAs: "blob",
        }),
        "gia-thanh-san-xuat.xlsx",
      ),
  };
}

export type SurveyApi = ReturnType<typeof createSurveyApi>;
