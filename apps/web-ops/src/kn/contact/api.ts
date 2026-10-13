// Client có kiểu cho Contact (W11.23, pf/contact `cap/directory` — /v1/contacts/*) trên openapi-fetch sinh từ hợp đồng.
// Máy chủ LỌC TRƯỜNG theo vai (BR-03): bộ "basic" (Cấp TW, Cấp Xã) chỉ có họ tên, năm sinh, SĐT. Dữ liệu cá nhân chỉ đi
// qua bộ nhớ (phản hồi no-store) — không lưu vào localStorage/sessionStorage.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type ContactAccess = S["ContactAccess"];
export type ContactLevel = S["ContactLevel"];
export type ContactPersonBasic = S["ContactPersonBasic"];
export type ContactPersonFull = S["ContactPersonFull"];
export type ContactPersonList = S["ContactPersonList"];
export type ContactPersonDetail = S["ContactPersonDetail"];
export type ContactPersonWrite = S["ContactPersonWrite"];
export type ContactUnit = S["ContactUnit"];
export type ContactUnitList = S["ContactUnitList"];
export type ContactUnitWrite = S["ContactUnitWrite"];
export type ContactPerson = ContactPersonBasic | ContactPersonFull;

export interface PersonFilter {
  q?: string;
  level?: ContactLevel;
  unit?: string;
  title?: string;
  include_hidden?: boolean;
  offset?: number;
  limit?: number;
}

export const LEVEL_LABEL: Record<ContactLevel, string> = {
  tw: "Trung ương",
  tinh: "Tỉnh",
  xa: "Xã",
  khac: "Đơn vị khác",
};

export const FIELD_LABEL: Record<string, string> = {
  full_name: "Họ tên",
  birth_year: "Năm sinh",
  phone: "Số điện thoại",
  title: "Chức danh",
  unit: "Đơn vị công tác",
  level: "Cấp",
  email: "Email",
  organization: "Cơ quan",
  hidden: "Ngừng hiển thị",
};

/** Dòng của bộ full (có source) — bộ basic không có trường này. */
export function isFull(p: ContactPerson): p is ContactPersonFull {
  return "source" in p;
}

export function createContactApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const clean = (f: PersonFilter) =>
    Object.fromEntries(Object.entries(f).filter(([, v]) => v !== undefined && v !== "" && v !== false)) as PersonFilter;
  return {
    access: (): Promise<ContactAccess> => unwrap(api.GET("/v1/contacts/access")),
    persons: (f: PersonFilter): Promise<ContactPersonList> =>
      unwrap(api.GET("/v1/contacts/persons", { params: { query: clean(f) } })),
    person: (id: string): Promise<ContactPersonDetail> =>
      unwrap(api.GET("/v1/contacts/persons/{id}", { params: { path: { id } } })),
    createPerson: (body: ContactPersonWrite): Promise<ContactPersonDetail> =>
      unwrap(api.POST("/v1/contacts/persons", { params: { header: header() }, body })),
    revisePerson: (id: string, body: ContactPersonWrite): Promise<ContactPersonDetail> =>
      unwrap(api.POST("/v1/contacts/persons/{id}/revise", { params: { header: header(), path: { id } }, body })),
    units: (q?: string, level?: ContactLevel, includeHidden?: boolean): Promise<ContactUnitList> =>
      unwrap(
        api.GET("/v1/contacts/units", {
          params: { query: clean({ q, level, include_hidden: includeHidden }) as { q?: string; level?: ContactLevel; include_hidden?: boolean } },
        }),
      ),
    createUnit: (body: ContactUnitWrite): Promise<ContactUnit> =>
      unwrap(api.POST("/v1/contacts/units", { params: { header: header() }, body })),
    reviseUnit: (id: string, body: ContactUnitWrite): Promise<ContactUnit> =>
      unwrap(api.POST("/v1/contacts/units/{id}/revise", { params: { header: header(), path: { id } }, body })),
  };
}

export type ContactApi = ReturnType<typeof createContactApi>;
