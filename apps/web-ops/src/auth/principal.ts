import type { Me } from "@/api/types";

/** Cấp phạm vi hiển thị trên web: TW → Tỉnh → Xã (ADR-0009) + Cục KTHT (xem toàn vùng). */
export type ScopeLevel = "tw" | "tinh" | "xa" | "cuc";

/** Vai realm Keycloak (deploy/compose/keycloak/agrigreen-realm.json). */
export const ROLES = {
  platformAdmin: "platform_admin",
  paramSetter: "param_setter",
  paramApprover: "param_approver",
  masterdataAdmin: "masterdata_admin",
  registryAdmin: "registry_admin",
  cghViewer: "cgh_viewer",
  cghAdmin: "cgh_admin",
  consentWriter: "consent_writer",
  consentReader: "consent_reader",
  knOfficer: "kn_officer",
  knProvinceAdmin: "kn_province_admin",
  knNationalAdmin: "kn_national_admin",
  accountAdmin: "account_admin",
  accountAdminProvince: "account_admin_province",
  reportsAdmin: "reports_admin",
  conflictResolver: "sync_conflict_resolver",
  deviceAdmin: "device_admin",
  geoAdmin: "geo_admin",
  plotEditor: "plot_editor",
  coopManager: "coop_manager",
  waterwayEditor: "waterway_editor",
  waterwayPublisher: "waterway_publisher",
  simulationAnalyst: "simulation_analyst",
  sopAdmin: "sop_admin",
  contentEditorNational: "content_editor_national",
  contentEditorProvince: "content_editor_province",
  contentReviewer: "content_reviewer",
  farmer: "farmer",
  mrvCollector: "mrv_collector",
  mrvReviewer: "mrv_reviewer",
  mrvViewer: "mrv_viewer",
  mrvExporter: "mrv_exporter",
  metricsViewer: "metrics_viewer",
  plotViewer: "plot_viewer",
  // G5 (apiclientport / meteringport): tài khoản của tenant đơn vị vận hành Public API.
  apiClientAdmin: "api_client_admin",
  apiClientApprover: "api_client_approver",
  meteringViewer: "metering_viewer",
  billingOfficer: "billing_officer",
  billingApprover: "billing_approver",
} as const;

/** Vai quản trị khách hàng Public API (ADR-0022): đề xuất ≠ duyệt (R-56). */
export const API_CLIENT_ROLES: readonly string[] = [ROLES.apiClientAdmin, ROLES.apiClientApprover];
/** Vai đọc sổ sử dụng và bảng kê (pf/metering statementReaders). */
export const METERING_READ_ROLES: readonly string[] = [ROLES.meteringViewer, ROLES.billingOfficer, ROLES.billingApprover];

/** Vai đọc màn hình MRV (pf/mrv): thu, duyệt, xem, xuất. */
export const MRV_ROLES: readonly string[] = [ROLES.mrvCollector, ROLES.mrvReviewer, ROLES.mrvViewer, ROLES.mrvExporter];

/** Vai đọc nhật ký/ảnh chụp/phát lại lớp GIS (GET /v1/gis/events|snapshots|replay). */
export const GIS_HISTORY_ROLES: readonly string[] = [
  ROLES.metricsViewer,
  ROLES.platformAdmin,
  ROLES.geoAdmin,
  ROLES.plotViewer,
  ROLES.plotEditor,
  ROLES.knNationalAdmin,
  ROLES.knProvinceAdmin,
  ROLES.cghViewer,
];

/** Vai cán bộ khuyến nông — với dữ liệu mùa vụ HTX chỉ được XEM trong phạm vi (pf/cropcycle). */
export const KN_ROLES: readonly string[] = [ROLES.knOfficer, ROLES.knProvinceAdmin, ROLES.knNationalAdmin];

/** Nhãn tiếng Việt của vai realm (hiển thị; mã vai vẫn gửi nguyên văn). */
export const ROLE_LABEL: Readonly<Record<string, string>> = {
  platform_admin: "Quản trị nền tảng",
  param_setter: "Người đặt tham số",
  param_approver: "Người phê duyệt tham số",
  masterdata_admin: "Quản trị danh mục",
  registry_admin: "Quản trị cơ cấu tổ chức",
  cgh_viewer: "Xem Bản đồ CGH (Cục)",
  cgh_admin: "Quản trị Bản đồ CGH",
  kn_officer: "Khuyến nông viên xã",
  kn_province_admin: "Quản lý TTKN tỉnh",
  kn_national_admin: "Quản lý TTKN Quốc gia",
  coop_manager: "Quản lý HTX",
  account_admin: "Quản trị tài khoản",
  account_admin_province: "Quản trị tài khoản tỉnh",
  reports_admin: "Quản trị mẫu báo cáo",
  sync_conflict_resolver: "Xử lý xung đột đồng bộ",
  device_admin: "Quản trị thiết bị",
  geo_admin: "Quản trị dữ liệu bản đồ",
  plot_editor: "Vẽ ranh giới thửa",
  waterway_editor: "Biên tập mạng đường thủy",
  waterway_publisher: "Phát hành mạng đường thủy",
  simulation_analyst: "Phân tích mô phỏng Hub",
  sop_admin: "Soạn quy trình SOP",
  farmer: "Nông dân (App HTX)",
  mrv_collector: "Thu minh chứng MRV",
  mrv_reviewer: "Duyệt minh chứng MRV",
  mrv_viewer: "Xem minh chứng MRV",
  mrv_exporter: "Xuất gói bằng chứng MRV",
  metrics_viewer: "Xem chỉ số & lịch sử",
  plot_viewer: "Xem thửa",
  vault_eraser: "Xoá định danh (RB-3)",
  consent_writer: "Ghi đồng ý",
  consent_reader: "Xem đồng ý",
  api_client_admin: "Quản trị khách hàng Public API",
  api_client_approver: "Duyệt khách hàng Public API",
  metering_viewer: "Xem đo lường & nhật ký xuất",
  billing_officer: "Lập bảng kê sử dụng",
  billing_approver: "Duyệt bảng kê sử dụng",
};

export function roleLabel(role: string): string {
  return ROLE_LABEL[role] ?? role;
}

/**
 * Người dùng hiện tại như client thấy. CHỈ dùng để ẩn/hiện giao diện —
 * mọi quyền nghiệp vụ do backend kiểm (R-55). Client không xác minh chữ ký token.
 */
export interface Principal {
  subject: string;
  displayName: string;
  tenantId: string | null;
  orgRef: string | null;
  roles: string[];
  /** true khi token có amr chứa otp (đã qua yếu tố thứ hai) — bề mặt Admin cần MFA. */
  mfa: boolean;
  level: ScopeLevel | null;
  unitName: string | null;
  orgName: string | null;
  scopeUnits: string[];
  scopeAll: boolean;
}

export interface AccessTokenClaims {
  sub?: string;
  name?: string;
  preferred_username?: string;
  tenant_id?: string;
  org_ref?: string;
  amr?: string[];
  realm_access?: { roles?: string[] };
}

function base64UrlDecode(s: string): string {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "=");
  const bin = atob(b64);
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

/** Đọc payload JWT (không xác minh — backend xác minh, R-54). Token lạ → {}. */
export function decodeAccessToken(token: string): AccessTokenClaims {
  const part = token.split(".")[1];
  if (!part) return {};
  try {
    const parsed: unknown = JSON.parse(base64UrlDecode(part));
    return typeof parsed === "object" && parsed !== null ? (parsed as AccessTokenClaims) : {};
  } catch {
    return {};
  }
}

const OTP_AMR = new Set(["otp", "mfa", "totp", "hwk"]);

/**
 * Cấp phạm vi của GET /v1/me → cấp hiển thị. Cục KTHT (tổ chức dcrd) có phạm vi toàn quốc
 * nhưng dùng cổng riêng (BRD CGH FN-11), nên tách thành "cuc". Quản lý HTX (cooperative) và
 * "none" không có màn hình khuyến nông → null (menu chỉ theo vai).
 */
export function scopeLevelOf(me: Pick<Me, "org" | "scope">): ScopeLevel | null {
  if (me.org?.kind === "dcrd") return "cuc";
  switch (me.scope.level) {
    case "national":
      return "tw";
    case "province":
      return "tinh";
    case "commune":
      return "xa";
    default:
      return null;
  }
}

function unitNameOf(me: Pick<Me, "org" | "scope">, level: ScopeLevel | null): string | null {
  if (level === "cuc") return "Toàn vùng";
  if (me.scope.all) return "Toàn quốc";
  const code = me.org?.admin_unit_code ?? me.scope.units[0];
  return code ? (me.scope.unit_names[code] ?? code) : null;
}

/** Dựng Principal từ claim token + GET /v1/me. Me không chứa tên người (R-04) → tên lấy từ claim. */
export function buildPrincipal(claims: AccessTokenClaims, me: Me | null): Principal {
  const tokenRoles = claims.realm_access?.roles ?? [];
  const roles = Array.from(new Set([...tokenRoles, ...(me?.roles ?? [])]));
  const level = me ? scopeLevelOf(me) : null;
  return {
    subject: me?.actor_ref ?? claims.sub ?? "",
    displayName: claims.name ?? claims.preferred_username ?? "Người dùng",
    tenantId: me?.tenant_id ?? claims.tenant_id ?? null,
    orgRef: me?.org?.ref ?? claims.org_ref ?? null,
    roles,
    mfa: (me?.mfa ?? false) || (claims.amr ?? []).some((a) => OTP_AMR.has(a)),
    level,
    unitName: me ? unitNameOf(me, level) : null,
    orgName: me?.org?.name ?? null,
    scopeUnits: me?.scope.units ?? [],
    scopeAll: me?.scope.all ?? false,
  };
}

export function hasRole(p: Pick<Principal, "roles"> | null, ...roles: string[]): boolean {
  return !!p && roles.some((r) => p.roles.includes(r));
}

export const LEVEL_LABEL: Record<ScopeLevel, string> = {
  tw: "Trung ương",
  tinh: "Tỉnh",
  xa: "Xã",
  cuc: "Cục KTHT",
};
