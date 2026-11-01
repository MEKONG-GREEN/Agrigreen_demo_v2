import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
export type DesignView = components["schemas"]["CNTSDesignView"];
export type DesignImport = components["schemas"]["CNTSDesignImport"];
export type DesignField = components["schemas"]["FormsField"];
export const DESIGN_HEADERS = ["key", "label", "type", "required", "unit", "group", "non_negative", "options"];
export function designFileError(file: { name: string; size: number }): string | null {
  if (!/\.(xlsx|csv)$/i.test(file.name)) return "Chọn tệp .xlsx hoặc CSV UTF-8 theo mẫu.";
  if (!file.size || file.size > 2 * 1024 * 1024) return "Tệp cần có nội dung và không vượt 2 MB.";
  return null;
}
export function createDesignApi(api: ApiClient) {
  return {
    list: async (): Promise<DesignView[]> => (await unwrap(api.GET("/v1/extension/cnts/form-designs"))).items,
    preview: (bytes: Uint8Array): Promise<DesignImport> => unwrap(api.POST("/v1/extension/cnts/form-designs/import-preview", {
      body: bytes as unknown as string, bodySerializer: (body: unknown) => body as BodyInit,
      headers: { "Content-Type": "application/octet-stream" },
    })),
  };
}
