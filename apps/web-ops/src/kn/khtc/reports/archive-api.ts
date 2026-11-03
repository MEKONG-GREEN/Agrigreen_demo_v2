import { parseProblemResponse } from "@/api/problem";
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
export type ArchiveWord = components["schemas"]["KHTCArchiveWord"];
export type ArchiveFile = {period_id: string; export_id: string; format?: "excel" | "word"; run_id?: string};
export type ArchiveRow = components["schemas"]["KHTCArchiveRow"];
export type ArchiveQuery = { year: number; period_id?: string; kind?: ArchiveRow["report_type"] | ""; q?: string; versions?: boolean; offset?: number; limit?: number };
export type ArchivePeriod = components["schemas"]["KHTCArchivePeriod"];
export const createArchiveApi = (api: ApiClient) => ({
  periods: (query: Omit<ArchiveQuery, "period_id" | "versions">) => unwrap(api.GET("/v1/extension/khtc/exported-reports/periods", { params: {query} })),
  preview: (id: string, exportID: string) => unwrap(api.GET("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/preview", { params: { path: { id, exportID } } })),
  async zip(items: ArchiveFile[]) {
    const res = await api.POST("/v1/extension/khtc/exported-reports/zip", { body: {items}, parseAs: "blob" });
    if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
    return {blob: res.data, name: "bao-cao-khtc.zip"};
  },
  words: (id: string, exportID: string, offset = 0) => unwrap(api.GET("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/words", {params: {path: {id, exportID}, query: {offset, limit: 20}}})),
  async word(id: string, exportID: string, runID: string) {
    const res = await api.GET("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/words/{runID}/download", {params: {path: {id, exportID, runID}}, parseAs: "blob"});
    if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
    return {blob: res.data, name: "bao-cao-khtc.docx"};
  },
  list: (query: ArchiveQuery) => unwrap(api.GET("/v1/extension/khtc/exported-reports", { params: { query } })),
});