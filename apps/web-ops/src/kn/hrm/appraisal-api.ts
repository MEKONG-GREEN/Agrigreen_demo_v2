import { unwrap, type ApiClient } from "@/api/client";
import { filenameFromDisposition } from "@/lib/files";
import type { components, paths } from "@/api/generated/application";

type S = components["schemas"];
export type AppraisalItem = S["HrmAppraisalItem"];
export type AppraisalPeriod = S["HrmAppraisalPeriod"];
export type AppraisalTemplate = S["HrmAppraisalTemplate"];
export type AppraisalEntryChange = S["HrmAppraisalEntryChange"];
export type AppraisalPeriodInput = paths["/v1/hrm/appraisals/periods"]["post"]["requestBody"]["content"]["application/json"];
export type AppraisalPeriodChange = paths["/v1/hrm/appraisals/periods/{period}"]["post"]["requestBody"]["content"]["application/json"];
export type AppraisalTemplateChange = paths["/v1/hrm/appraisals/templates/{kind}/{code}"]["post"]["requestBody"]["content"]["application/json"];
export const APPRAISAL_BASE = "/kn/nhan-su/danh-gia";
export const APPRAISAL_STATUS: Record<AppraisalItem["status"], string> = {
  not_started: "Chưa tự chấm", self_submitted: "Đã tự nộp", manager_submitted: "Quản lý đã hoàn tất",
  sent: "Đã gửi HCTH", change_requested: "Cần điều chỉnh", closed: "Đã chốt",
};
export const APPRAISAL_ACTION: Record<string, string> = {
  open: "Mở đợt", self_draft: "Lưu nháp tự chấm", self_submit: "Tự nộp phiếu", proxy_self: "Nhập thay từ bản scan",
  manager_draft: "Lưu nháp cấp quản lý", manager_submit: "Hoàn tất đề xuất", send: "Gửi HCTH", return: "Yêu cầu điều chỉnh",
  official: "Ghi kết luận chính thức", reopen: "Mở lại riêng người", close: "Chốt kết quả", draft: "Lưu nháp", publish: "Phát hành",
};

export function createAppraisalApi(api: ApiClient) {
  return {
    periods: () => unwrap(api.GET("/v1/hrm/appraisals/periods")),
    access: () => unwrap(api.GET("/v1/hrm/access")),
    templates: (kind: "summary" | "form") => unwrap(api.GET("/v1/hrm/appraisals/templates/{kind}", { params: { path: { kind } } })),
    templateHistory: (kind: "summary" | "form", code: string) => unwrap(api.GET("/v1/hrm/appraisals/templates/{kind}/{code}/history", { params: { path: { kind, code } } })),
    saveTemplate: (kind: "summary" | "form", code: string, body: AppraisalTemplateChange, key: string) => unwrap(api.POST("/v1/hrm/appraisals/templates/{kind}/{code}", { params: { path: { kind, code }, header: { "Idempotency-Key": key } }, body })),
    open: (body: AppraisalPeriodInput, key: string) => unwrap(api.POST("/v1/hrm/appraisals/periods", { params: { header: { "Idempotency-Key": key } }, body })),
    changePeriod: (period: string, body: AppraisalPeriodChange, key: string) => unwrap(api.POST("/v1/hrm/appraisals/periods/{period}", { params: { path: { period }, header: { "Idempotency-Key": key } }, body })),
    entries: (period: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/entries", { params: { path: { period } } })),
    detail: (period: string, staff: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/entries/{staff}", { params: { path: { period, staff } } })),
    change: (period: string, staff: string, body: AppraisalEntryChange, key: string) => unwrap(api.POST("/v1/hrm/appraisals/periods/{period}/entries/{staff}", { params: { path: { period, staff }, header: { "Idempotency-Key": key } }, body })),
    history: (period: string, staff: string, before?: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/entries/{staff}/history", { params: { path: { period, staff }, query: { before } } })),
    paper: (period: string, kind: "MS05" | "MS06", unit?: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/paper/{kind}", {params:{path:{period,kind},query:{unit}}})),
    exports: (period: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/exports", {params:{path:{period}}})),
    export: (period: string, body: {kind:"MS05"|"MS06"|"PHIEU_DIEM";format:"xlsx"|"docx";unit?:string;version:number}, key: string) => unwrap(api.POST("/v1/hrm/appraisals/periods/{period}/exports", {params:{path:{period},header:{"Idempotency-Key":key}},body})),
    file: async (period: string, id: string) => {
      const r = await api.GET("/v1/hrm/appraisals/periods/{period}/exports/{export}/file", {params:{path:{period,export:id}},parseAs:"blob"});
      return {blob:await unwrap(Promise.resolve(r)),name:filenameFromDisposition(r.response.headers.get("Content-Disposition"),"danh-gia"),drift:r.response.headers.get("X-Data-Drift") === "true"};
    },
    version: (period: string, staff: string, version: string) => unwrap(api.GET("/v1/hrm/appraisals/periods/{period}/entries/{staff}/versions/{version}", { params: { path: { period, staff, version } } })),
  };
}
export type AppraisalApi = ReturnType<typeof createAppraisalApi>;
export type AppraisalDetail = Awaited<ReturnType<AppraisalApi["detail"]>>;

export function appraisalPeriodPath(id: string) { return `${APPRAISAL_BASE}/dot/${encodeURIComponent(id)}`; }
export function appraisalEntryPath(period: string, staff: string) { return `${appraisalPeriodPath(period)}/phieu/${encodeURIComponent(staff)}`; }
