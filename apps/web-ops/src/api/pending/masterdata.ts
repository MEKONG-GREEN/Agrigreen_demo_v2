// ĐANG CHỜ HỢP ĐỒNG — module pf/masterdata (danh mục, tham số có phê duyệt).
// Đọc qua Application API; ghi (đề xuất/phê duyệt) qua Admin API — bắt buộc MFA.
import type { DateOnly, DateTime, Decimal, Uuid } from "./common";

/** GET /v1/catalogs */
export interface CatalogSummary {
  catalog: string;
  name: string;
  item_count: number;
}

/** GET /v1/catalogs/{catalog} — mục không bị xoá, chỉ ngừng dùng (active=false). */
export interface CatalogItem {
  catalog: string;
  code: string;
  name: string;
  attrs: Record<string, string>;
  active: boolean;
}

export interface ParamVersion {
  version: number;
  value: Decimal | null;
  text: string | null;
  unit: string;
  effective_from: DateOnly;
  source_doc: string;
  approved_by: string;
  approved_at: DateTime;
}

export type ProposalStatus = "pending" | "approved" | "rejected";

export interface ParamProposal {
  id: Uuid;
  key: string;
  value: Decimal | null;
  text: string | null;
  unit: string;
  effective_from: DateOnly;
  source_doc: string;
  reason: string;
  status: ProposalStatus;
  /** subject (sub) của người đề xuất — so với người gọi để áp R-56 trên UI. */
  proposed_by: string;
  proposed_by_name: string;
  proposed_at: DateTime;
  decided_by: string | null;
  decided_by_name: string | null;
  decided_at: DateTime | null;
  decision_note: string | null;
}

/** GET /v1/params — mỗi khóa: phiên bản đang hiệu lực + đề xuất chờ duyệt. */
export interface Param {
  key: string;
  name: string;
  unit: string;
  kind: "decimal" | "text";
  current: ParamVersion | null;
  pending: ParamProposal[];
}

/** POST /v1/params/{key}/proposals (Admin API, vai param_setter) */
export interface ParamProposalInput {
  value?: Decimal;
  text?: string;
  effective_from: DateOnly;
  source_doc: string;
  reason: string;
}

/** POST /v1/params/proposals/{id}/approve | /reject (Admin API, vai param_approver) */
export interface ParamDecisionInput {
  note?: string;
}
