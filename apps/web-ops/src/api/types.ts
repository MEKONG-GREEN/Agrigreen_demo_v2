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

/** Body JSON thành công (200/201/202) của một POST. */
type PostOk<R> = R extends { 200: { content: { "application/json": infer X } } }
  ? X
  : R extends { 201: { content: { "application/json": infer X } } }
    ? X
    : R extends { 202: { content: { "application/json": infer X } } }
      ? X
      : never;
export type AppPost<P extends keyof AppPaths> = AppPaths[P] extends { post: { responses: infer R } } ? PostOk<R> : never;
export type AdminPost<P extends keyof AdminPaths> = AdminPaths[P] extends { post: { responses: infer R } } ? PostOk<R> : never;

/** Số thập phân dạng chuỗi (R-10). Chỉ đổi sang số khi hiển thị/vẽ. */
export type Decimal = S["Decimal"];

// pf/masterdata
export type AdminUnit = S["AdminUnit"];
export type CatalogItem = S["CatalogItem"];
export type CatalogName = AppPaths["/v1/masterdata/catalogs/{catalog}"]["get"]["parameters"]["path"]["catalog"];
export type CatalogSummary = AdminGet<"/v1/masterdata/catalogs">["items"][number];
export type ParamVersion = AS["ParamVersion"];
export type ParamProposalInput = AS["ParamProposal"];
export type ParamStatus = ParamVersion["status"];
/** G-07: một khóa trong sổ đăng ký tham số (Admin GET /v1/masterdata/params). */
export type ParamRegistryItem = AdminGet<"/v1/masterdata/params">["items"][number];

// pf/party
export type OrgKind = S["OrgKind"];
export type Org = S["Org"];
export type Cooperative = S["Cooperative"];
export type HouseholdListItem = S["HouseholdListItem"];
export type PartyOrg = AS["PartyOrg"];
export type PartyOrgEvent = AS["PartyOrgEvent"];
export type HouseholdImportResult = AdminPaths["/v1/households/import"]["post"]["responses"][200]["content"]["application/json"];
export type OrgImportResult = AdminPaths["/v1/orgs/import"]["post"]["responses"][200]["content"]["application/json"];

/** G-04 — trạng thái đồng ý hiện tại theo mục đích của một nông hộ. */
export type ConsentState = HouseholdListItem["consents"][number];

// pf/geo
export type PlotFeature = S["PlotFeature"];
export type PlotStatus = S["PlotProperties"]["status"];
export type AdminBoundaryFeature = AppGet<"/v1/geo/admin-boundaries/{code}">;
export type BoundaryConflict = S["BoundaryConflict"];
export type BoundaryConflictDetail = AppGet<"/v1/geo/conflicts/{id}">;
export type PlotImportResult = AdminPaths["/v1/geo/plots/import"]["post"]["responses"][200]["content"]["application/json"];

// pf/content
export type ContentKind = S["ContentKind"];
export type ContentDocument = S["ContentDocument"];
export type LibraryItem = S["LibraryItem"];
export type DocumentDetail = AppGet<"/v1/documents/{ref}">;

// pf/extension
export type ExtTask = S["ExtTask"];
export type ExtTaskStatus = ExtTask["status"];
export type ExtTaskPriority = NonNullable<AppPaths["/v1/extension/tasks"]["get"]["parameters"]["query"]>["priority"] & string;
export type ExtTaskDetail = AppGet<"/v1/extension/tasks/{id}">;
export type ExtKPI = S["ExtKPI"];
export type ExtDashboard = AppGet<"/v1/extension/dashboard">;
export type ExtDirective = S["ExtDirective"];
export type ExtDirectiveDetail = AppGet<"/v1/extension/directives/{id}">;

// Bảng điều hành gộp (G-02, GET /v1/dashboard)
export type UnifiedFigures = S["UnifiedFigures"];
export type UnifiedDashboard = AppGet<"/v1/dashboard">;

// pf/mechanization (Bản đồ CGH — cổng Cục KTHT, chỉ xem)
export type MechCoverageStatus = S["MechCoverageStatus"];
export type MechRun = S["MechRun"];
export type MechGroup = S["MechGroup"];
export type MechCoverageItem = S["MechCoverageItem"];
export type MechCoverage = AppGet<"/v1/mechanization/coverage">;
export type MechDashboard = AppGet<"/v1/mechanization/dashboard">;
export type MechForecastRun = S["MechForecastRun"];
export type MechForecastResult = S["MechForecastResult"];
export type MechForecast = AppGet<"/v1/mechanization/forecasts/{id}">;
export type PlotAssignResult = S["PlotAttrResult"];

// pf/notify
export type Notification = S["Notification"];
export type NudgeInput = S["NudgeInput"];
export type NudgeResult = S["NudgeResult"];
export type SyncDevice = AS["SyncDeviceAdmin"];

// pf/sync — xung đột (Admin)
export type SyncConflict = AS["SyncConflict"];
export type SyncConflictDetail = AdminGet<"/v1/sync/conflicts/{id}">;
export type SlaStatus = NonNullable<SyncConflict["sla_status"]>;

// pf/accounts (Admin)
export type AccountsUser = AS["AccountsUser"];
export type AccountsEvent = AS["AccountsEvent"];
export type AccountsGrants = AdminGet<"/v1/accounts/grants">;
export type AccountsImportResult = AdminPost<"/v1/accounts/users/import">;

// pf/reports
export type ReportTemplate = S["ReportTemplate"];
export type ReportParams = S["ReportParams"];
export type ReportExport = S["ReportExport"];
export type ReportExportListItem = AppGet<"/v1/reports/exports">["items"][number];
export type ReportDownload = AppGet<"/v1/reports/exports/{id}/download">;
/** Mẫu báo cáo như Admin API trả (schema riêng của admin.yaml). */
export type AdminReportTemplate = AdminGet<"/v1/reports/templates">["items"][number];
export type ReportSchedule = AS["ReportSchedule"];
export type ReportScheduleRun = AdminGet<"/v1/reports/schedules/{id}/runs">["items"][number];

// pf/cropcycle (App HTX: SOP, vụ, nhật ký, duyệt, sản lượng, bảng điều hành)
export type CropSOPStep = S["CropSOPStep"];
export type CropActivity = CropSOPStep["activity_code"];
export type CropSOPTemplate = S["CropSOPTemplate"];
export type CropSOPVersion = S["CropSOPVersion"];
export type CropSOP = S["CropSOP"];
export type AdminCropSOP = AS["CropSOP"];
export type CropVarietyBinding = S["CropVarietyBinding"];
export type CropSeason = S["CropSeason"];
export type CropSeasonDetail = S["CropSeasonDetail"];
export type CropSeasonEvent = S["CropSeasonEvent"];
export type CropBulkOpenResult = AppPost<"/v1/cropcycle/seasons/bulk-open">;
export type CropPreviousConfig = AppGet<"/v1/cropcycle/plots/{ref}/previous-config">;
export type CropFarmLog = S["CropFarmLog"];
export type CropFarmLogState = S["CropFarmLogState"];
export type CropFarmLogDetail = AppGet<"/v1/cropcycle/farm-logs/{id}">;
export type CropLogBatchResult = AppPost<"/v1/cropcycle/farm-logs/batch">;
export type CropHarvest = S["CropHarvest"];
export type CropBulkHarvestResult = AppPost<"/v1/cropcycle/harvests/bulk">;
export type CropDashGroup = S["CropDashGroup"];
export type CropDashboard = AppGet<"/v1/cropcycle/dashboard">;
export type CropCoopStats = AdminGet<"/v1/cropcycle/stats/cooperatives/{ref}">;

// pf/accounts — tài khoản nông dân (App HTX)
export type FarmerAccount = S["FarmerAccount"];
export type FarmerGrant = S["FarmerGrant"];
export type FarmerActivation = S["FarmerActivation"];
export type FarmerCreated = S["FarmerCreated"];
export type FarmerImportResult = AppPost<"/v1/farmer-accounts/import">;

// pf/extension — yêu cầu hỗ trợ của nông dân
export type SupportRequest = S["SupportRequest"];

// pf/masterdata — nhập danh mục (Admin)
export type CatalogImportResult = AS["CatalogImportResult"];

// pf/party — GET /v1/me (người gọi + phạm vi đã giải)
export type Me = S["Me"];
export type MeScopeLevel = Me["scope"]["level"];

/** Trang cursor (R-53) — cùng dạng mọi route danh sách của backend. */
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}
