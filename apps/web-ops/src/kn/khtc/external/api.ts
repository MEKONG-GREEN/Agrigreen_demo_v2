// W11.67 — Số liệu nguồn ngoài KHTC (FN-TI-02.11): client API và hàm thuần của màn hình.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition, type UploadFile } from "@/lib/files";
type S = components["schemas"];
export type ExtIndicator = S["KHTCExternalIndicator"];
export type ExtFigure = S["KHTCExternalFigure"];
export type ExtFigureInput = S["KHTCExternalFigureInput"];
export type ExtHistory = S["KHTCExternalHistoryItem"];
export type ExtImport = S["KHTCExternalImport"];
export type ExtEvidence = S["KHTCExternalEvidence"];
export type ReportExternal = S["KHTCReportExternalFigure"];
const h = () => ({ "Idempotency-Key": newIdempotencyKey() });

export function createExternalApi(api: ApiClient) {
    const blob = async (res: { data?: unknown; response: Response }, fallback: string) => {
        if (!res.response.ok || !res.data)
            throw await parseProblemResponse(res.response);
        return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), fallback) };
    };
    return {
        indicators: async () => (await unwrap(api.GET("/v1/extension/khtc/external/indicators"))).items,
        addIndicator: (body: S["KHTCExternalIndicatorInput"]) => unwrap(api.POST("/v1/extension/khtc/external/indicators", { params: { header: h() }, body })),
        figures: async (year: number, q: { period_id?: string; agency?: string; q?: string } = {}) =>
            (await unwrap(api.GET("/v1/extension/khtc/external/figures", { params: { query: { year, ...Object.fromEntries(Object.entries(q).filter(([, v]) => v)) } } }))).items,
        save: (body: ExtFigureInput) => unwrap(api.POST("/v1/extension/khtc/external/figures", { params: { header: h() }, body })),
        withdraw: async (body: S["KHTCExternalWithdraw"]) => {
            const res = await api.POST("/v1/extension/khtc/external/figures/withdraw", { params: { header: h() }, body });
            if (!res.response.ok)
                throw await parseProblemResponse(res.response);
        },
        history: async (year: number, period_id?: string) =>
            (await unwrap(api.GET("/v1/extension/khtc/external/history", { params: { query: period_id ? { year, period_id } : { year } } }))).items,
        template: async (year: number) =>
            blob(await api.GET("/v1/extension/khtc/external/template", { params: { query: { year } }, parseAs: "blob" }), `mau-so-lieu-nguon-ngoai-${year}.xlsx`),
        stage: (year: number, file: UploadFile) =>
            unwrap(api.POST("/v1/extension/khtc/external/imports", {
                params: { query: { year, file_name: file.name }, header: h() },
                body: file.body as unknown as string,
                bodySerializer: (b: unknown) => b as BodyInit,
                headers: { "Content-Type": file.contentType },
            })),
        commit: (importID: string, overwrite: boolean, reason: string) =>
            unwrap(api.POST("/v1/extension/khtc/external/imports/{importID}/commit", { params: { path: { importID }, header: h() }, body: overwrite ? { overwrite, reason } : { overwrite } })),
        upload: (file: { name: string; body: Uint8Array }) =>
            unwrap(api.POST("/v1/extension/khtc/external/evidence", {
                params: { query: { file_name: file.name }, header: h() },
                body: file.body as unknown as string,
                bodySerializer: (b: unknown) => b as BodyInit,
                headers: { "Content-Type": "application/octet-stream" },
            })),
        evidence: async (evidenceID: string, name: string) =>
            blob(await api.GET("/v1/extension/khtc/external/evidence/{evidenceID}", { params: { path: { evidenceID } }, parseAs: "blob" }), name),
    };
}
export type ExternalApi = ReturnType<typeof createExternalApi>;

/** Ô nhập kiểu Việt Nam → chuỗi decimal của API: "48.215,5" → "48215.5", "26 400" → "26400", "12.45" giữ nguyên. Sai → null. */
export function parseVnNumber(raw: string): string | null {
    let s = raw.replace(/[\s ]/g, "");
    if (!s)
        return null;
    if (s.includes(","))
        s = s.replaceAll(".", "").replace(",", ".");
    else if ((s.match(/\./g) ?? []).length > 1 || /^-?\d{1,3}\.\d{3}$/.test(s))
        s = s.replaceAll(".", "");
    return /^-?\d{1,17}(\.\d{1,4})?$/.test(s) ? s : null;
}

/** Chuỗi decimal → hiển thị Việt Nam: "48215.5" → "48.215,5". */
export function fmtVn(value: string | null | undefined): string {
    if (value === null || value === undefined || value === "")
        return "—";
    const neg = value.startsWith("-"), [int = "0", frac] = value.replace("-", "").split(".");
    return (neg ? "-" : "") + int.replace(/\B(?=(\d{3})+(?!\d))/g, ".") + (frac ? "," + frac : "");
}

/** Nguồn hiển thị một dòng: cơ quan · văn bản/kênh, ngày. */
export function sourceText(f: { agency: string; document: string; source_date: string }): string {
    const [y, m, d] = (f.source_date || "").split("-");
    return `${f.agency} · ${f.document}${f.source_date ? `, ${d}/${m}/${y}` : ""}`;
}

/** Danh sách cơ quan cung cấp khác nhau (bộ lọc). */
export function agencies(rows: ExtFigure[]): string[] {
    return [...new Set(rows.map(r => r.agency).filter(Boolean))].sort((a, b) => a.localeCompare(b, "vi"));
}
