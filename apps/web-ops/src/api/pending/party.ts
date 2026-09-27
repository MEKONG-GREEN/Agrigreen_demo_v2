// ĐANG CHỜ HỢP ĐỒNG — module pf/party (tổ chức, HTX, nông hộ) và /v1/me.
import type { AdminUnitLevel, DateOnly, DateTime, Decimal, OrgKind, PageQuery, ScopeLevel, Uuid } from "./common";

/** GET /v1/me — người gọi + phạm vi đã giải (party/port.ScopeResolver). */
export interface Me {
  subject: string;
  /** Tên hiển thị của cán bộ (không phải nông hộ). */
  display_name: string;
  tenant_id: Uuid;
  roles: string[];
  org: {
    ref: Uuid;
    kind: OrgKind;
    name: string;
    admin_unit_code: string | null;
  } | null;
  scope: {
    level: ScopeLevel | null;
    all: boolean;
    /** Mã đơn vị hành chính (tỉnh/xã) người gọi thấy; rỗng khi all = true. */
    units: string[];
    unit_name: string | null;
  };
}

/** GET /v1/orgs — danh sách phẳng, client dựng cây theo parent_ref. */
export interface Org {
  ref: Uuid;
  kind: OrgKind;
  code: string;
  name: string;
  admin_unit_code: string | null;
  parent_ref: Uuid | null;
  active: boolean;
}

export interface OrgQuery extends PageQuery {
  parent_ref?: Uuid;
  kind?: OrgKind;
}

export type CooperativeStatus = "active" | "inactive";

/** GET /v1/cooperatives, GET /v1/cooperatives/{id} */
export interface Cooperative {
  id: Uuid;
  /** Mã nghiệp vụ, vd mã số thuế HTX. */
  code: string;
  name: string;
  kind: "htx" | "tht";
  province_code: string;
  province_name: string;
  commune_code: string;
  commune_name: string;
  member_households: number;
  area_ha: Decimal;
  digitized_plots: number;
  status: CooperativeStatus;
  founded_on: DateOnly | null;
  /** Nhãn cán bộ khuyến nông phụ trách (tên cán bộ, không phải nông hộ). */
  extension_officer: string | null;
  updated_at: DateTime;
}

export interface CooperativeQuery extends PageQuery {
  q?: string;
  admin_unit?: string;
  status?: CooperativeStatus;
}

/**
 * GET /v1/cooperatives/{id}/households — CHỈ nhãn đã che.
 * Họ tên/SĐT/CCCD nằm trong vault (R-04); backend tự che trước khi trả.
 */
export interface HouseholdSummary {
  ref: Uuid;
  /** Nhãn đã che, vd "Hộ #7F3A · N*** V** A". */
  masked_label: string;
  commune_name: string;
  plots: number;
  area_ha: Decimal;
  consent: "granted" | "withdrawn" | "none";
  joined_on: DateOnly | null;
}

/** GET /v1/admin-units */
export interface AdminUnit {
  code: string;
  name: string;
  level: AdminUnitLevel;
  parent_code: string | null;
  valid_from: DateOnly;
  valid_to: DateOnly | null;
}

export interface AdminUnitQuery {
  level?: AdminUnitLevel;
  parent?: string;
  /** Tra theo thời điểm (mặc định hôm nay). */
  at?: DateOnly;
}
