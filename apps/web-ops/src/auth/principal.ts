import type { Me } from "@/api/pending/party";
import type { ScopeLevel } from "@/api/pending/common";

/** Vai realm Keycloak (deploy/compose/keycloak/agrigreen-realm.json). */
export const ROLES = {
  platformAdmin: "platform_admin",
  paramSetter: "param_setter",
  paramApprover: "param_approver",
  consentWriter: "consent_writer",
  consentReader: "consent_reader",
} as const;

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

/** Dựng Principal từ claim token + /v1/me (nếu backend đã có). */
export function buildPrincipal(claims: AccessTokenClaims, me: Me | null): Principal {
  const tokenRoles = claims.realm_access?.roles ?? [];
  const roles = Array.from(new Set([...tokenRoles, ...(me?.roles ?? [])]));
  return {
    subject: me?.subject ?? claims.sub ?? "",
    displayName: me?.display_name ?? claims.name ?? claims.preferred_username ?? "Người dùng",
    tenantId: me?.tenant_id ?? claims.tenant_id ?? null,
    orgRef: me?.org?.ref ?? claims.org_ref ?? null,
    roles,
    mfa: (claims.amr ?? []).some((a) => OTP_AMR.has(a)),
    level: me?.scope.level ?? null,
    unitName: me?.scope.unit_name ?? null,
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
