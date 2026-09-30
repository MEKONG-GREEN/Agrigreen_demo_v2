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
/** HTX US-DASH-03: người nhận chọn được cho lịch của quản lý HTX (Application API). */
export type ReportRecipient = AppGet<"/v1/reports/recipients">["items"][number];
export type ReportScheduleInput = S["ReportScheduleInput"];

// pf/party — hoàn thiện App HTX (HTX US-HTX-02/03, US-HH-02/03)
export type CoopProfileImportResult = AdminPost<"/v1/cooperatives/import">;
export type CoopCopyResult = AdminPost<"/v1/cooperatives/{ref}/copy">;
export type HouseholdCopyResult = AppPost<"/v1/households/copy">;
export type HouseholdCopyItem = AppPaths["/v1/households/copy"]["post"]["requestBody"]["content"]["application/json"]["items"][number];

// pf/content — soạn bài (Admin)
export type ContentVersion = AS["ContentVersion"];
export type ContentAdminDocument = AS["ContentAdminDocument"];
export type ContentCategory = NonNullable<AS["ContentInput"]["category"]>;

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
/** Danh sách cán bộ GET /v1/extension/support-requests (quản lý HTX, khuyến nông). */
export type SupportRequestStaff = S["SupportRequestStaff"];
export type SupportCategory = SupportRequestStaff["category"];

// pf/cropcycle G3-2 — thu gom rơm MKG phía HTX (van collection_schedule / htx_collection_statement)
export type HtxCollectionSchedule = S["HtxCollectionSchedule"];
export type HtxCollectionStatement = S["HtxCollectionStatement"];
export type HtxCollectionResponse = S["HtxCollectionResponse"];

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

// pf/geo G2 — web GIS (ranh giới HTX, kho/Hub, sức chứa, đường bộ)
export type GeoCoopFeature = S["GeoCoopFeature"];
export type GeoCoopLayer = S["GeoCoopLayer"];
export type GeoCoopBoundaryVersion = S["GeoCoopBoundaryVersion"];
export type GeoFacilityFeature = S["GeoFacilityFeature"];
export type GeoFacilityLayer = S["GeoFacilityLayer"];
export type GeoFacilityEvent = S["GeoFacilityEvent"];
export type GeoCapacityView = S["GeoCapacityView"];
export type GeoCapacitySnapshot = S["GeoCapacitySnapshot"];
export type GeoCapacityLatest = S["GeoCapacityLatest"];
export type GeoRoadFeature = S["GeoRoadFeature"];
export type GeoRoadLayer = S["GeoRoadLayer"];
export type GeoRoadSegment = S["GeoRoadSegment"];
export type GeoLayers = AppGet<"/v1/geo/layers">;
export type CapacityWidgets = AppGet<"/v1/geo/capacity-widgets">;
export type CapacityWidget = CapacityWidgets["items"][number];
export type FacilityDetail = AppGet<"/v1/geo/facilities/{ref}">;
export type CoopImportResult = AdminPost<"/v1/geo/cooperative-boundaries/import">;
export type FacilityImportResult = AdminPost<"/v1/geo/facilities/import">;

// pf/cropcycle — lớp giai đoạn mùa vụ (GIS US-SEASON-01, US-YIELD-01)
export type CropStageMap = AppGet<"/v1/cropcycle/stage-map">;
export type CropStagePlot = CropStageMap["plots"][number];
export type CropStageCoop = CropStageMap["cooperatives"][number];

// pf/waterway
export type WaterwayLayerSegment = S["WaterwayLayerSegment"];
export type WaterwayLayer = AppGet<"/v1/waterway/segments">;
export type WaterwayRoute = S["WaterwayRoute"];
export type WaterwayNetworkSummary = S["WaterwayNetworkSummary"];
export type WaterwaySegment = AS["WaterwaySegment"];
export type WaterwayBuild = AS["WaterwayBuild"];
export type WaterwayNetworkVersion = AS["WaterwayNetworkVersion"];
export type WaterwayFinding = AS["WaterwayFinding"];
export type WaterwayStructure = AS["WaterwayStructure"];

// pf/simulation
export type SimCandidateHub = S["SimCandidateHub"];
export type SimScenario = S["SimScenario"];
export type SimScenarioHub = S["SimScenarioHub"];
export type SimChange = S["SimChange"];
export type SimSimulation = S["SimSimulation"];
export type SimSnapshotMeta = S["SimSnapshotMeta"];
export type SimCompareRow = S["SimCompareRow"];
export type SimCompare = AppPost<"/v1/scenarios:compare">;
export type SimResultEnvelope = AppGet<"/v1/simulations/{id}/result">;
export type SimJob = AppGet<"/v1/jobs/{id}">;
