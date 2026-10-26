import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
type S = components["schemas"];
export type ReportDetail = S["KHTCReportDetail"];
export type ReportExport = S["KHTCReportExport"];
export type ReportSave = S["KHTCReportSave"];
export type ReportFilter = S["KHTCReportFilter"];
export type ReportRow = S["KHTCReportRow"];
export type ReportWord = S["KHTCReportWord"];
const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
export function createReportApi(api: ApiClient) {
    return {
        get: (id: string) => unwrap(api.GET("/v1/extension/khtc/periods/{id}/report", { params: { path: { id } } })),
        save: (id: string, body: ReportSave) => unwrap(api.POST("/v1/extension/khtc/periods/{id}/report/draft", { params: { path: { id }, header: header() }, body })),
        refresh: (id: string, version: number) => unwrap(api.POST("/v1/extension/khtc/periods/{id}/report/refresh", { params: { path: { id }, header: header() }, body: { base_version: version } })),
        history: async (id: string) => (await unwrap(api.GET("/v1/extension/khtc/periods/{id}/report/exports", { params: { path: { id } } }))).items,
        export: (id: string, version: number, acknowledge: boolean, filter: ReportFilter) => unwrap(api.POST("/v1/extension/khtc/periods/{id}/report/exports", { params: { path: { id }, header: header() }, body: { base_version: version, acknowledge_provisional: acknowledge, filter } })),
        final: (id: string, exportID: string) => unwrap(api.POST("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/final", { params: { path: { id, exportID }, header: header() }, body: {} })),
        async download(id: string, exportID: string) {
            const res = await api.GET("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/download", { params: { path: { id, exportID } }, parseAs: "blob" });
            if (!res.response.ok || !res.data)
                throw await parseProblemResponse(res.response);
            return { blob: res.data as unknown as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), "bao-cao-khtc.xlsx") };
        },
        /** W10.17: Word (.docx) từ nội dung đóng băng của bản Excel, qua `cap/export` (số {n}/BC-TTKN tự cấp khi ô số trống). */
        word: (id: string, exportID: string) => unwrap(api.POST("/v1/extension/khtc/periods/{id}/report/exports/{exportID}/word", { params: { path: { id, exportID }, header: header() }, body: {} })),
        words: async (id: string) => (await unwrap(api.GET("/v1/extension/khtc/periods/{id}/report/words", { params: { path: { id } } }))).items,
        async downloadWord(id: string, runID: string) {
            const res = await api.GET("/v1/extension/khtc/periods/{id}/report/words/{runID}/download", { params: { path: { id, runID } }, parseAs: "blob" });
            if (!res.response.ok || !res.data)
                throw await parseProblemResponse(res.response);
            return { blob: res.data as unknown as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), "bao-cao-khtc.docx") };
        },
    };
}
export type ReportApi = ReturnType<typeof createReportApi>;
/** Match filters against items, keeping only their ancestors for the preview. */
export function filterRows(rows: ReportRow[], f: ReportFilter): ReportRow[] {
    if (!f.department && !f.program && !f.commune && !f.level)
        return rows;
    const byId = new Map(rows.map(r => [r.ref, r]));
    const keep = new Set<string>();
    for (const row of rows) {
        if (row.kind !== "item" || (f.level && row.level !== f.level) || (f.department && row.department !== f.department) || (f.commune && !row.communes?.includes(f.commune)))
            continue;
        const chain = [row];
        const seen = new Set([row.ref]);
        let parent = row.parent_ref && byId.get(row.parent_ref);
        while (parent && !seen.has(parent.ref)) {
            chain.push(parent);
            seen.add(parent.ref);
            parent = parent.parent_ref && byId.get(parent.parent_ref);
        }
        if (f.program && !chain.some(r => r.ref === f.program || r.program_ref === f.program))
            continue;
        chain.forEach(r => keep.add(r.ref));
    }
    return rows.filter(r => keep.has(r.ref));
}
