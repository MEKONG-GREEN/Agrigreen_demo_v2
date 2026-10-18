// Client tổng hợp CNTS Cấp Tỉnh (W11.44 — BRD CNTS v3.7 UR-TINH-14, UR-TINH-15, FN-16): BM-02 KSGM tuần/tháng + dòng điều
// chỉnh, BM-18 Phụ lục 2 + nuôi trồng, chốt/mở lại bảng tổng hợp, phiên bản tệp chia sẻ Chi cục (Excel).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";

type S = components["schemas"];
export type CntsBM02 = S["CntsBM02"];
export type CntsBM18 = S["CntsBM18"];
export type CntsShare = S["CntsShare"];
export type CntsAggState = S["CntsAggregateState"];
export type CntsCols = S["CntsCols"];
export type Basis = "submitted" | "closed";
export type AggForm = "CNTS-BM02" | "CNTS-BM18";

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export const AGG_STATE: Record<CntsAggState["state"], [string, "neutral" | "ok" | "warn"]> = {
  none: ["Chưa chốt", "neutral"],
  closed: ["Đã chốt", "ok"],
  reopened: ["Đã mở lại", "warn"],
};

export interface AquaEntry {
  object: string;
  area: string | null;
  output: string | null;
  note?: string;
}

export function createCntsAggApi(api: ApiClient) {
  return {
    bm02: (q: { period_type?: "ksgm_week" | "month"; key?: string; basis?: Basis }): Promise<CntsBM02> =>
      unwrap(api.GET("/v1/extension/cnts/bm02", { params: { query: q } })),
    adjust: (body: { week_key: string; facility_ref: string; deltas: Record<string, string>; reason: string }) =>
      unwrap(api.POST("/v1/extension/cnts/bm02/adjustments", { params: idem(), body })),
    bm18: (q: { week?: string; basis?: Basis }): Promise<CntsBM18> => unwrap(api.GET("/v1/extension/cnts/bm18", { params: { query: q } })),
    saveAqua: (week: string, entries: AquaEntry[]): Promise<CntsBM18> =>
      unwrap(api.POST("/v1/extension/cnts/bm18/aquaculture", { params: idem(), body: { week, entries } })),
    close: (form: AggForm, period_key: string): Promise<CntsAggState> =>
      unwrap(api.POST("/v1/extension/cnts/aggregates/close", { params: idem(), body: { form, period_key } })),
    reopen: (form: AggForm, period_key: string, reason: string): Promise<CntsAggState> =>
      unwrap(api.POST("/v1/extension/cnts/aggregates/reopen", { params: idem(), body: { form, period_key, reason } })),
    share: (form: AggForm, period_type: "ksgm_week" | "month" | "week", period_key: string) =>
      unwrap(api.POST("/v1/extension/cnts/shares", { params: idem(), body: { form, period_type, period_key } })),
    download: async (id: string): Promise<Blob> => {
      const res = await api.GET("/v1/extension/cnts/shares/{id}/xlsx", { params: { path: { id } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
  };
}

export type CntsAggApi = ReturnType<typeof createCntsAggApi>;

/** Kiểm dòng điều chỉnh phía máy khách (máy chủ kiểm lại): có ít nhất một số khác 0, số hợp lệ, lý do 10–500 ký tự. */
export function adjustmentErrors(deltas: Record<string, string>, reason: string): Record<string, string> {
  const errs: Record<string, string> = {};
  let any = false;
  for (const [k, v] of Object.entries(deltas)) {
    const t = v.trim();
    if (!t) continue;
    if (!/^-?\d{1,12}(\.\d{1,3})?$/.test(t)) errs[k] = "Số (có thể âm, dấu chấm)";
    else if (Number(t) !== 0) any = true;
  }
  if (!any && Object.keys(errs).length === 0) errs.deltas = "Nhập ít nhất một số khác 0";
  const r = reason.trim().length;
  if (r < 10 || r > 500) errs.reason = "Lý do 10–500 ký tự";
  return errs;
}

/** Ô nuôi trồng: rỗng hoặc số không âm (tối đa 4 chữ số thập phân). */
export function aquaValueError(v: string): string | null {
  const t = v.trim();
  if (!t) return null;
  return /^\d{1,12}(\.\d{1,4})?$/.test(t) ? null : "Số không âm (dấu chấm)";
}
