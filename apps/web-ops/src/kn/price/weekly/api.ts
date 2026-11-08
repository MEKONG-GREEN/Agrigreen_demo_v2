import { unwrap, type ApiClient } from "@/api/client";
import { currentIdempotentRun, newIdempotencyKey, withIdempotentOperation } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import type { components } from "@/api/generated/application";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type WeeklyView = S["PriceWeeklyView"];
export type WeeklyReport = S["PriceWeeklyReport"];
export type WeeklyRow = S["PriceWeeklyRow"];
export type WeeklyCommand = S["PriceWeeklyCommand"];
export type WeeklyPatch = S["PriceWeeklyPatch"];
export type WeeklyNewItem = S["PriceWeeklyNewItem"];
export type WeeklyPreview = S["PriceWeeklyPreview"];
export type Program = "information" | "training";
export const PROGRAM_TITLE: Record<Program, string> = { information: "Chương trình thông tin", training: "Đào tạo huấn luyện và hợp tác" };

export interface WeeklyUpload { file: File; version: number; week_id: string; sha256?: string; confirm_overwrite?: boolean }

async function multipart(p: WeeklyUpload) {
  if (p.file.size === 0 || p.file.size > 5 * 1024 * 1024) throw new ApiProblem({ type: "about:blank", status: 422, code: "validation_failed", title: "Tệp Excel/CSV tối đa 5 MB" });
  const bytes = await p.file.arrayBuffer();
  const file = new File([bytes], p.file.name, { type: p.file.type || "application/octet-stream", lastModified: 0 });
  return stableMultipart([["file", file], ["version", String(p.version)], ["week_id", p.week_id], ["sha256", p.sha256 ?? ""], ["confirm_overwrite", String(p.confirm_overwrite === true)]]);
}

export function createWeeklyApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const file = async (response: Promise<{ data?: unknown; response: Response }>) => {
    const r = await response;
    if (!r.response.ok) throw await parseProblemResponse(r.response);
    return { blob: r.data as Blob, name: filenameFromDisposition(r.response.headers.get("Content-Disposition"), "bao-cao-tuan.xlsx") };
  };
  return {
    read: (program: Program, week_id?: string) => unwrap(api.GET("/v1/price/weekly-programs/{program}", { params: { path: { program }, query: { week_id: week_id || undefined } } })),
    write: (program: Program, body: WeeklyCommand) => unwrap(api.POST("/v1/price/weekly-programs/{program}/actions", { params: { path: { program }, header: header() }, body })),
    preview: async (program: Program, input: WeeklyUpload): Promise<WeeklyPreview> => {
      const run = currentIdempotentRun(), mp = await multipart(input);
      const send = () => unwrap(api.POST("/v1/price/weekly-programs/{program}/imports/preview", { params: { path: { program }, header: header() }, headers: { "Content-Type": mp.contentType }, body: { file: "", version: input.version, week_id: input.week_id }, bodySerializer: () => mp.body }));
      return run ? withIdempotentOperation(run, send) : send();
    },
    commit: async (program: Program, input: WeeklyUpload): Promise<WeeklyView> => {
      const run = currentIdempotentRun(), mp = await multipart(input);
      const send = () => unwrap(api.POST("/v1/price/weekly-programs/{program}/imports/confirm", { params: { path: { program }, header: header() }, headers: { "Content-Type": mp.contentType }, body: { file: "", version: input.version, week_id: input.week_id, sha256: input.sha256, confirm_overwrite: input.confirm_overwrite }, bodySerializer: () => mp.body }));
      return run ? withIdempotentOperation(run, send) : send();
    },
    export: (program: Program, week_id: string) => file(api.GET("/v1/price/weekly-programs/{program}/export", { params: { path: { program }, query: { week_id } }, parseAs: "blob" })),
    template: (program: Program, week_id: string) => file(api.GET("/v1/price/weekly-programs/{program}/template", { params: { path: { program }, query: { week_id } }, parseAs: "blob" })),
    issues: (program: Program, week_id: string) => file(api.GET("/v1/price/weekly-programs/{program}/imports/issues", { params: { path: { program }, query: { week_id } }, parseAs: "blob" })),
  };
}
export type WeeklyApi = ReturnType<typeof createWeeklyApi>;
