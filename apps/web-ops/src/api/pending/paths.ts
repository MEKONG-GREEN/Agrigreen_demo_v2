// ĐANG CHỜ HỢP ĐỒNG — bản đồ đường dẫn viết tay theo ĐÚNG dạng openapi-typescript sinh ra,
// để openapi-fetch kiểm kiểu các route chưa có trong application.yaml. Khi route vào OpenAPI,
// xoá dòng tương ứng ở đây; kiểu sinh tự động (`generated/application.ts`) sẽ thay thế.
import type { components } from "../generated/application";
import type { Page, PageQuery, Uuid } from "./common";
import type { CghDashboard, CoverageQuery, CoverageReport, PriorityCooperative, PriorityQuery } from "./cgh";
import type { DocumentQuery, LibraryDocument } from "./content";
import type { DashboardKpis, SupportTask, TaskQuery } from "./extension";
import type { AdminBoundaryCollection, CooperativeLocationCollection, Plot, PlotQuery } from "./geo";
import type {
  CatalogItem,
  CatalogSummary,
  Param,
  ParamDecisionInput,
  ParamProposal,
  ParamProposalInput,
} from "./masterdata";
import type { AdminUnit, AdminUnitQuery, Cooperative, CooperativeQuery, HouseholdSummary, Me, Org, OrgQuery } from "./party";

type Problem = components["schemas"]["Problem"];
type Headers = { [name: string]: unknown };
type Json<T> = { headers: Headers; content: { "application/json": T } };
type ProblemResponse = { headers: Headers; content: { "application/problem+json": Problem } };
type Errors = { 400: ProblemResponse; 401: ProblemResponse; 403: ProblemResponse; 404: ProblemResponse; 422: ProblemResponse; 500: ProblemResponse };

type NoPath = { path?: never };
type WithPath<P> = { path: P };

type GetOp<Q, R, P = never> = {
  parameters: { query?: Q; header?: never; cookie?: never } & ([P] extends [never] ? NoPath : WithPath<P>);
  requestBody?: never;
  responses: { 200: Json<R> } & Errors;
};

type PostOp<B, R, P = never> = {
  parameters: {
    query?: never;
    /** Middleware tự thêm nếu thiếu (R-51). */
    header?: { "Idempotency-Key"?: string };
    cookie?: never;
  } & ([P] extends [never] ? NoPath : WithPath<P>);
  requestBody: { content: { "application/json": B } };
  responses: { 200: Json<R>; 201: Json<R> } & Errors & { 409: ProblemResponse };
};

type PathItem<Ops> = {
  parameters: { query?: never; header?: never; path?: never; cookie?: never };
} & Ops;

export interface PendingPaths {
  "/v1/me": PathItem<{ get: GetOp<never, Me> }>;
  "/v1/dashboard/kpis": PathItem<{ get: GetOp<{ season?: string }, DashboardKpis> }>;
  "/v1/orgs": PathItem<{ get: GetOp<OrgQuery, Page<Org>> }>;
  "/v1/admin-units": PathItem<{ get: GetOp<AdminUnitQuery, { items: AdminUnit[] }> }>;
  "/v1/admin-units/boundaries": PathItem<{
    get: GetOp<{ level?: "province" | "commune"; parent?: string }, AdminBoundaryCollection>;
  }>;
  "/v1/cooperatives": PathItem<{ get: GetOp<CooperativeQuery, Page<Cooperative>> }>;
  "/v1/cooperatives/locations": PathItem<{ get: GetOp<{ admin_unit?: string }, CooperativeLocationCollection> }>;
  "/v1/cooperatives/{id}": PathItem<{ get: GetOp<never, Cooperative, { id: Uuid }> }>;
  "/v1/cooperatives/{id}/households": PathItem<{
    get: GetOp<PageQuery, Page<HouseholdSummary>, { id: Uuid }>;
  }>;
  "/v1/plots": PathItem<{ get: GetOp<PlotQuery, Page<Plot>> }>;
  "/v1/tasks": PathItem<{ get: GetOp<TaskQuery, Page<SupportTask>> }>;
  "/v1/documents": PathItem<{ get: GetOp<DocumentQuery, Page<LibraryDocument>> }>;
  "/v1/catalogs": PathItem<{ get: GetOp<never, { items: CatalogSummary[] }> }>;
  "/v1/catalogs/{catalog}": PathItem<{
    get: GetOp<{ include_inactive?: boolean }, { items: CatalogItem[] }, { catalog: string }>;
  }>;
  "/v1/params": PathItem<{ get: GetOp<never, { items: Param[] }> }>;
  "/v1/params/{key}/proposals": PathItem<{
    post: PostOp<ParamProposalInput, ParamProposal, { key: string }>;
  }>;
  "/v1/params/proposals/{id}/approve": PathItem<{
    post: PostOp<ParamDecisionInput, ParamProposal, { id: Uuid }>;
  }>;
  "/v1/params/proposals/{id}/reject": PathItem<{
    post: PostOp<ParamDecisionInput, ParamProposal, { id: Uuid }>;
  }>;
  "/v1/cgh/coverage": PathItem<{ get: GetOp<CoverageQuery, CoverageReport> }>;
  "/v1/cgh/dashboard": PathItem<{ get: GetOp<{ season?: string }, CghDashboard> }>;
  "/v1/cgh/priority-cooperatives": PathItem<{ get: GetOp<PriorityQuery, Page<PriorityCooperative>> }>;
}
