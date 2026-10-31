import { useMemo } from "react";
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { useAuth } from "@/auth/AuthProvider";

type S = components["schemas"];
export type TemplateKey = S["KHTCTemplateKey"];
export type TemplateDefinition = S["KHTCTemplateDefinition"];
export type TemplateNode = S["KHTCTemplateNode"];
export type TemplateBinding = S["KHTCTemplateBinding"];
export type TemplateDetail = S["KHTCTemplateDetail"];
export type TemplateVersion = S["KHTCTemplateVersion"];
export type TemplatePreview = S["KHTCTemplatePreview"];
export type PreviewInput = S["KHTCTemplatePreviewInput"];
export { TEMPLATE_KEYS, TEMPLATE_TITLES } from "./model";
const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
export function createTemplateApi(api: ApiClient) {
  return {
    catalog: async () => (await unwrap(api.GET("/v1/extension/khtc/templates"))).items,
    get: (key: TemplateKey) => unwrap(api.GET("/v1/extension/khtc/templates/{key}", { params: { path: { key } } })),
    save: (key: TemplateKey, body: S["KHTCTemplateSave"]) => unwrap(api.POST("/v1/extension/khtc/templates/{key}/versions", { params: { path: { key }, header: header() }, body })),
    restore: (key: TemplateKey, body: S["KHTCTemplateRestore"]) => unwrap(api.POST("/v1/extension/khtc/templates/{key}/restore", { params: { path: { key }, header: header() }, body })),
    publish: (key: TemplateKey, body: S["KHTCTemplatePublish"]) => unwrap(api.POST("/v1/extension/khtc/templates/{key}/publish", { params: { path: { key }, header: header() }, body })),
    preview: (key: TemplateKey, body: PreviewInput) => unwrap(api.POST("/v1/extension/khtc/templates/{key}/preview", { params: { path: { key }, header: header() }, body })),
  };
}
export function useTemplateApi() { const { api } = useAuth(); return useMemo(() => createTemplateApi(api), [api]); }
export type TemplateApi = ReturnType<typeof createTemplateApi>;
