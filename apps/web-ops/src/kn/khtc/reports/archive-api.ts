import { parseProblemResponse } from "@/api/problem";
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
export type ArchiveRow = components["schemas"]["KHTCArchiveRow"];
export type ArchiveQuery = { year: number; period_id?: string; kind?: ArchiveRow["report_type"] | ""; q?: string; versions?: boolean; offset?: number; limit?: number };
export type ArchivePeriod = components["schemas"]["KHTCArchivePeriod"];
export const createArchiveApi = (api: ApiClient) => ({
  periods: (query: Omit<ArchiveQuery, "period_id" | "versions">) => unwrap(api.GET("/v1/extension/khtc/exported-reports/periods", { params: {query} })),
  preview: (id: string, exportID: string) => unwrap(api.GET("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/preview", { params: { path: { id, exportID } } })),
  async zip(items: {period_id: string; export_id: string}[]) {
    const res = await api.POST("/v1/extension/khtc/exported-reports/zip", { body: {items}, parseAs: "blob" });
    if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
    return {blob: res.data, name: "bao-cao-khtc.zip"};
  },
  list: (query: ArchiveQuery) => unwrap(api.GET("/v1/extension/khtc/exported-reports", { params: { query } })),
});