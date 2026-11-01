import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
export type ArchiveRow = components["schemas"]["KHTCArchiveRow"];
export type ArchiveQuery = { year: number; kind?: ArchiveRow["report_type"] | ""; q?: string; versions?: boolean; offset?: number; limit?: number };
export const createArchiveApi = (api: ApiClient) => ({
  list: (query: ArchiveQuery) => unwrap(api.GET("/v1/extension/khtc/exported-reports", { params: { query } })),
});