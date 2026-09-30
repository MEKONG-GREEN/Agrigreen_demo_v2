/** Vai realm Keycloak của vùng MKG (hằng trong internal/mkg/{hub,measurement,fieldops}/port, deploy/compose/keycloak/agrigreen-realm.json). */
export const ROLES = {
  hubOperator: "hub_operator",
  hubApprover: "hub_approver",
  scaleSupervisor: "scale_supervisor",
  deviceApprover: "device_approver",
  reconciliationOfficer: "reconciliation_officer",
  disputeOfficer: "dispute_officer",
  toleranceApprover: "tolerance_approver",
  fieldCollector: "field_collector",
  fieldSupervisor: "field_supervisor",
} as const;

export const ROLE_LABEL: Readonly<Record<string, string>> = {
  hub_operator: "Vận hành hub",
  hub_approver: "Phê duyệt hub",
  scale_supervisor: "Giám sát cân",
  device_approver: "Duyệt thiết bị cân",
  reconciliation_officer: "Cán bộ đối soát",
  dispute_officer: "Xử lý tranh chấp",
  tolerance_approver: "Duyệt dung sai / định mức",
  field_collector: "Tổ gom hiện trường",
  field_supervisor: "Giám sát lệnh gom",
};

export function roleLabel(role: string): string {
  return ROLE_LABEL[role] ?? role;
}

/**
 * Người dùng hiện tại như client thấy — dựng từ claim của access token (api-internal không có /v1/me).
 * CHỈ dùng để ẩn/hiện giao diện; mọi quyền nghiệp vụ do backend kiểm (R-55). Client không xác minh chữ ký.
 */
export interface Principal {
  subject: string;
  displayName: string;
  tenantId: string | null;
  roles: string[];
  /** true khi token có amr chứa otp — bề mặt quản trị của api-internal bắt buộc MFA. */
  mfa: boolean;
}

export interface AccessTokenClaims {
  sub?: string;
  name?: string;
  preferred_username?: string;
  tenant_id?: string;
  amr?: string[];
  realm_access?: { roles?: string[] };
}

function base64UrlDecode(s: string): string {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "=");
  const bin = atob(b64);
  return new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)));
}

/** Đọc payload JWT (không xác minh — backend xác minh). Token lạ → {}. */
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

export function buildPrincipal(claims: AccessTokenClaims): Principal {
  return {
    subject: claims.sub ?? "",
    displayName: claims.name ?? claims.preferred_username ?? "Người dùng",
    tenantId: claims.tenant_id ?? null,
    roles: Array.from(new Set(claims.realm_access?.roles ?? [])),
    mfa: (claims.amr ?? []).some((a) => OTP_AMR.has(a)),
  };
}

export function hasRole(p: Pick<Principal, "roles"> | null, ...roles: string[]): boolean {
  return !!p && roles.some((r) => p.roles.includes(r));
}
