// Lời gọi API mẫu báo cáo dạng lưới (admin.yaml / application.yaml mục pf/reports) qua client sinh từ OpenAPI.
// Mọi POST mang Idempotency-Key (R-51). Lỗi → ApiProblem (RFC 9457).
import type { AdminApiClient, ApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition, type UploadFile } from "@/lib/files";
import type { FormEvaluator } from "@/kn/grid/useFormEvaluation";
import type { FormDefinition, FormEvalInput, FormOp, FormReportType } from "./types";

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export interface FormFilter {
  department?: string;
  level?: "commune" | "province";
  status?: "active" | "retired";
  report_type?: FormReportType;
}

export const listForms = (a: AdminApiClient, q: FormFilter) => unwrap(a.GET("/v1/reports/forms", { params: { query: q } }));
export const getForm = (a: AdminApiClient, id: string) => unwrap(a.GET("/v1/reports/forms/{id}", { params: { path: { id } } }));
export const getFormVersion = (a: AdminApiClient, id: string, v: number) => unwrap(a.GET("/v1/reports/forms/{id}/versions/{v}", { params: { path: { id, v } } }));
export const formLog = (a: AdminApiClient, id: string, cursor?: string) =>
  unwrap(a.GET("/v1/reports/forms/{id}/log", { params: { path: { id }, query: { cursor, limit: 100 } } }));

export const createForm = (
  a: AdminApiClient,
  body: { code: string; name: string; department_code: string; entry_level: "commune" | "province"; definition?: FormDefinition; clone_from_version_id?: string },
) => unwrap(a.POST("/v1/reports/forms", { params: idem(), body }));

export const seedForms = (a: AdminApiClient, department_code: string) => unwrap(a.POST("/v1/reports/forms/seed", { params: idem(), body: { department_code } }));

export const retireForm = (a: AdminApiClient, id: string, reason: string) => unwrap(a.POST("/v1/reports/forms/{id}/retire", { params: { ...idem(), path: { id } }, body: { reason } }));

export const newFormVersion = (a: AdminApiClient, id: string, body: { from_version?: number; restore?: boolean }) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions", { params: { ...idem(), path: { id } }, body }));

export const saveDraft = (a: AdminApiClient, id: string, v: number, body: { definition?: FormDefinition; effective_from?: string; effective_label?: string }) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/draft", { params: { ...idem(), path: { id, v } }, body }));

export const applyFormOps = (a: AdminApiClient, id: string, v: number, ops: FormOp[]) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/ops", { params: { ...idem(), path: { id, v } }, body: { ops } }));

export const proposeVersion = (a: AdminApiClient, id: string, v: number, body: { effective_from?: string; effective_label?: string }) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/propose", { params: { ...idem(), path: { id, v } }, body }));

export const withdrawVersion = (a: AdminApiClient, id: string, v: number, reason?: string) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/withdraw", { params: { ...idem(), path: { id, v } }, body: reason ? { reason } : {} }));

export const publishVersion = (a: AdminApiClient, id: string, v: number) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/publish", { params: { ...idem(), path: { id, v } }, body: {} }));

export const retireVersion = (a: AdminApiClient, id: string, v: number, reason: string) =>
  unwrap(a.POST("/v1/reports/forms/{id}/versions/{v}/retire", { params: { ...idem(), path: { id, v } }, body: { reason } }));

export const listAssignments = (a: AdminApiClient, q: { template_id?: string; department?: string; level?: "commune" | "province"; active?: boolean }) =>
  unwrap(a.GET("/v1/reports/form-assignments", { params: { query: q } }));

export const assignForm = (a: AdminApiClient, template_id: string, report_type: FormReportType) =>
  unwrap(a.POST("/v1/reports/form-assignments", { params: idem(), body: { template_id, report_type } }));

export const revokeAssignment = (a: AdminApiClient, id: string, reason: string) =>
  unwrap(a.POST("/v1/reports/form-assignments/{id}/revoke", { params: { ...idem(), path: { id } }, body: { reason } }));

export interface ImportQuery {
  template_id?: string;
  code?: string;
  name?: string;
  department_code?: string;
  entry_level?: "commune" | "province";
  sheet?: string;
  header_start?: number;
  data_row?: number;
}

/** Nạp .xlsx: thân là byte tệp nguyên văn (không parse ở client). */
export const importForm = (a: AdminApiClient, file: UploadFile, q: ImportQuery) =>
  unwrap(
    a.POST("/v1/reports/forms/import", {
      params: { ...idem(), query: { ...q, file_name: file.name } },
      body: file.body as unknown as string,
      bodySerializer: (b: unknown) => b as BodyInit,
      headers: { "Content-Type": file.contentType },
    }),
  );

/** Tính thử bằng Admin API (mọi trạng thái, kể cả nháp — xem trước của trình thiết kế). */
export function adminEvaluator(a: AdminApiClient, versionId: string): FormEvaluator {
  return (body: FormEvalInput) => unwrap(a.POST("/v1/reports/form-versions/{id}/evaluate", { params: { ...idem(), path: { id: versionId } }, body }));
}

/** Tính bằng Application API (P4: màn nhập Cấp Xã — chỉ phiên bản đã phát hành). */
export function appEvaluator(api: ApiClient, versionId: string): FormEvaluator {
  return (body: FormEvalInput) => unwrap(api.POST("/v1/reports/form-versions/{id}/evaluate", { params: { ...idem(), path: { id: versionId } }, body }));
}

export const formLayout = (api: ApiClient, id: string, v: number, q: { period_label?: string[]; season?: string } = {}) =>
  unwrap(api.GET("/v1/reports/forms/{id}/versions/{v}/layout", { params: { path: { id, v }, query: q } }));

export const availableForms = (api: ApiClient, q: { department: string; level?: "commune" | "province"; report_type?: FormReportType; at?: string }) =>
  unwrap(api.GET("/v1/reports/forms/available", { params: { query: q } }));

/** Tải mẫu Excel đúng bố cục (Application API). */
export async function downloadTemplateXlsx(api: ApiClient, id: string, v: number, fallbackName: string): Promise<{ blob: Blob; name: string }> {
  const res = await api.GET("/v1/reports/forms/{id}/versions/{v}/template.xlsx", { params: { path: { id, v } }, parseAs: "blob" });
  if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
  return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), fallbackName) };
}
