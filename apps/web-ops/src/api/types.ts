// Tên gọn cho kiểu SINH TỰ ĐỘNG từ contracts/openapi (không viết tay).
// Application API: generated/application.ts · Admin API (MFA): generated/admin.ts.
import type { components as AdminComponents, paths as AdminPaths } from "./generated/admin";
import type { components as AppComponents, paths as AppPaths } from "./generated/application";

type S = AppComponents["schemas"];
type AS = AdminComponents["schemas"];

/** Body 200 của một GET trên Application API. */
export type AppGet<P extends keyof AppPaths> = AppPaths[P] extends {
  get: { responses: { 200: { content: { "application/json": infer R } } } };
}
  ? R
  : never;

/** Body 200 của một GET trên Admin API. */
export type AdminGet<P extends keyof AdminPaths> = AdminPaths[P] extends {
  get: { responses: { 200: { content: { "application/json": infer R } } } };
}
  ? R
  : never;

/** Số thập phân dạng chuỗi (R-10). Chỉ đổi sang số khi hiển thị/vẽ. */
export type Decimal = S["Decimal"];

// pf/masterdata
export type AdminUnit = S["AdminUnit"];
export type CatalogItem = S["CatalogItem"];
export type CatalogName = AppPaths["/v1/masterdata/catalogs/{catalog}"]["get"]["parameters"]["path"]["catalog"];
export type ParamVersion = AS["ParamVersion"];
export type ParamProposalInput = AS["ParamProposal"];
export type ParamStatus = ParamVersion["status"];

// pf/party
export type OrgKind = S["OrgKind"];
export type Org = S["Org"];
export type Cooperative = S["Cooperative"];
export type HouseholdListItem = S["HouseholdListItem"];
export type PartyOrgEvent = AS["PartyOrgEvent"];
export type HouseholdImportResult = AdminPaths["/v1/households/import"]["post"]["responses"][200]["content"]["application/json"];

// pf/geo
export type PlotFeature = S["PlotFeature"];
export type PlotStatus = S["PlotProperties"]["status"];
export type AdminBoundaryFeature = AppGet<"/v1/geo/admin-boundaries/{code}">;

// pf/content
export type ContentKind = S["ContentKind"];
export type ContentDocument = S["ContentDocument"];

// pf/extension
export type ExtTask = S["ExtTask"];
export type ExtTaskStatus = ExtTask["status"];
export type ExtTaskPriority = NonNullable<AppPaths["/v1/extension/tasks"]["get"]["parameters"]["query"]>["priority"] & string;
export type ExtKPI = S["ExtKPI"];
export type ExtDashboard = AppGet<"/v1/extension/dashboard">;

// pf/mechanization (Bản đồ CGH — cổng Cục KTHT, chỉ xem)
export type MechCoverageStatus = S["MechCoverageStatus"];
export type MechRun = S["MechRun"];
export type MechGroup = S["MechGroup"];
export type MechCoverageItem = S["MechCoverageItem"];
export type MechCoverage = AppGet<"/v1/mechanization/coverage">;
export type MechDashboard = AppGet<"/v1/mechanization/dashboard">;
export type PlotAssignResult = S["PlotAttrResult"];

// pf/party — GET /v1/me (người gọi + phạm vi đã giải)
export type Me = S["Me"];
export type MeScopeLevel = Me["scope"]["level"];

/** Trang cursor (R-53) — cùng dạng mọi route danh sách của backend. */
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}
