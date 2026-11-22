import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
export type DesignView = components["schemas"]["CNTSDesignView"];
export type DesignImport = components["schemas"]["CNTSDesignImport"];
export type DesignField = components["schemas"]["FormsField"];
export type DesignVersion = components["schemas"]["CNTSDesignVersion"];
export type DesignSaved = components["schemas"]["CNTSDesignSaved"];
export type DesignCreateInput = components["schemas"]["CNTSDesignCreateInput"];
export const DESIGN_HEADERS = ["key", "label", "type", "required", "unit", "group", "non_negative", "options"];
export const FIELD_TYPES: DesignField["type"][] = ["integer", "decimal", "text", "date", "choice"];
export function designFileError(file: { name: string; size: number }): string | null {
  if (!/\.(xlsx|csv)$/i.test(file.name)) return "Chọn tệp .xlsx hoặc CSV UTF-8 theo mẫu.";
  if (!file.size || file.size > 2 * 1024 * 1024) return "Tệp cần có nội dung và không vượt 2 MB.";
  return null;
}
/** #192: kiểm phía máy khách trước khi lưu (máy chủ kiểm lại; mẫu đã có phiên bản chỉ thêm chỉ tiêu — UR-TINH-07). */
export function designFieldErrors(fields: DesignField[], current: DesignField[] = []): string[] {
  const out: string[] = []; const seen = new Set<string>();
  if (!fields.length || fields.length > 500) out.push("Cần 1–500 chỉ tiêu");
  fields.forEach((f, i) => {
    const at = `Chỉ tiêu ${i + 1}`;
    if (!/^[a-z][a-z0-9_]{0,63}$/.test(f.key) || seen.has(f.key)) out.push(`${at}: mã [a-z][a-z0-9_]*, không trùng`); seen.add(f.key);
    if (!f.label.trim() || [...f.label].length > 300) out.push(`${at}: tên 1–300 ký tự`);
    if (f.type === "choice" && (!f.options?.length || f.options.some(x => !x.trim()) || new Set(f.options).size !== f.options.length)) out.push(`${at}: lựa chọn không rỗng, không trùng`);
    if (f.type !== "choice" && f.options?.length) out.push(`${at}: chỉ kiểu lựa chọn có danh sách lựa chọn`);
    if (f.non_negative && f.type !== "integer" && f.type !== "decimal") out.push(`${at}: "không âm" chỉ cho kiểu số`);
  });
  for (const c of current) {
    const n = fields.find(f => f.key === c.key);
    if (!n) out.push(`Không xóa chỉ tiêu đã phát hành: ${c.label} (${c.key})`);
    else if (n.type !== c.type) out.push(`Không đổi kiểu chỉ tiêu đã phát hành: ${c.label} (${c.key})`);
  }
  return out;
}
const idem = () => ({ "Idempotency-Key": newIdempotencyKey() });
export function createDesignApi(api: ApiClient) {
  return {
    list: async (): Promise<DesignView[]> => (await unwrap(api.GET("/v1/extension/cnts/form-designs"))).items,
    preview: (bytes: Uint8Array): Promise<DesignImport> => unwrap(api.POST("/v1/extension/cnts/form-designs/import-preview", {
      body: bytes as unknown as string, bodySerializer: (body: unknown) => body as BodyInit,
      headers: { "Content-Type": "application/octet-stream" },
    })),
    history: async (code: string): Promise<DesignVersion[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/form-designs/{code}/versions", { params: { path: { code } } }))).items,
    save: (code: string, base_version: number, title: string, fields: DesignField[]): Promise<DesignSaved> =>
      unwrap(api.POST("/v1/extension/cnts/form-designs/{code}/versions", { params: { path: { code }, header: idem() }, body: { base_version, title, fields } })),
    create: (body: DesignCreateInput): Promise<DesignSaved> =>
      unwrap(api.POST("/v1/extension/cnts/form-designs", { params: { header: idem() }, body })),
  };
}
