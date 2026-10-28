// pf/export (`cap/export`, W10.17): mẫu văn bản có phiên bản và lịch sử lần xuất .docx của đơn vị — chỉ đọc.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type ExportTemplate = S["ExportTemplate"];
export type ExportRun = S["ExportRun"];
export const DOCX_TYPE = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";

export function createExportApi(api: ApiClient) {
  return {
    templates: async () => (await unwrap(api.GET("/v1/exports/templates"))).items,
    runs: async (filter: { template?: string; subject?: string; limit?: number } = {}) =>
      (await unwrap(api.GET("/v1/exports/runs", { params: { query: filter } }))).items,
    async download(id: string) {
      const res = await api.GET("/v1/exports/runs/{id}/download", { params: { path: { id } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return { blob: res.data as unknown as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), "van-ban.docx") };
    },
  };
}
export type ExportApi = ReturnType<typeof createExportApi>;

/** Mã băm rút gọn để hiển thị (đủ đối chiếu bằng mắt; bản đầy đủ ở title). */
export function shortHash(h: string): string {
  return h.length > 12 ? `${h.slice(0, 12)}…` : h;
}
