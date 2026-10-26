// pf/accounts (Admin API) — tài khoản cán bộ trong Keycloak. Ma trận cấp vai do BACKEND trả và kiểm
// (GET /v1/accounts/grants); web chỉ lọc lựa chọn cho gọn và hiện lỗi của máy chủ.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { AccountsGrants, AccountsImportResult, AccountsUser, OrgKind } from "@/api/types";
import type { UploadFile } from "@/lib/files";
import { formatDateTime } from "@/lib/format";

/** Lý do khóa/mở khóa tối thiểu 20 ký tự (HTX US-HTXSTATUS-01 AC-3). */
export const LOCK_REASON_MIN = 20;

/** Kiểm sơ bộ phía client — máy chủ vẫn kiểm (422 validation_failed). Hợp lệ → null. */
export function lockReasonError(reason: string): string | null {
  const n = reason.trim().length;
  if (n >= LOCK_REASON_MIN) return null;
  return n === 0 ? `Nhập lý do (tối thiểu ${LOCK_REASON_MIN} ký tự).` : `Lý do tối thiểu ${LOCK_REASON_MIN} ký tự — còn thiếu ${LOCK_REASON_MIN - n} ký tự.`;
}

export type AccountState = "active" | "locked" | "locked_org" | "locked_dormant" | "pending";

export function accountState(u: Pick<AccountsUser, "enabled" | "lock_cause" | "required_actions">): AccountState {
  if (!u.enabled) {
    if (u.lock_cause === "org_deactivated") return "locked_org";
    return u.lock_cause === "dormant_45d" ? "locked_dormant" : "locked";
  }
  if (u.required_actions.includes("VERIFY_EMAIL") || u.required_actions.includes("UPDATE_PASSWORD")) return "pending";
  return "active";
}

export const ACCOUNT_STATE: Record<AccountState, { text: string; cls: string }> = {
  active: { text: "Đang hoạt động", cls: "c-ok" },
  pending: { text: "Chờ kích hoạt", cls: "c-info" },
  locked: { text: "Đã khóa", cls: "c-bad" },
  locked_org: { text: "Khóa theo tổ chức", cls: "c-mute" },
  // D24: job accounts_dormant khóa sau 45 ngày không hoạt động — Admin mở khóa (có lý do).
  locked_dormant: { text: "Khóa do không hoạt động", cls: "c-bad" },
};

/**
 * ADMIN-UNLOCK (#42/#61): khóa đăng nhập do sai mật khẩu nhiều lần (Keycloak attack-detection) — KHÁC `enabled`.
 * `login_locked` chỉ có ở GET chi tiết; thiếu field (danh sách, bản tạo) = chưa biết, không coi là đã mở.
 */
export type LoginLockState = "locked" | "clear" | "unknown";

export function loginLockState(u: Pick<AccountsUser, "login_locked">): LoginLockState {
  if (u.login_locked === true) return "locked";
  return u.login_locked === false ? "clear" : "unknown";
}

/** Thao tác của form khóa/mở khóa: mở khi bị Admin khóa HOẶC đang khóa do đăng nhập sai dù enabled=true. */
export function lockOperation(u: Pick<AccountsUser, "enabled" | "login_locked">): "lock" | "unlock" {
  return !u.enabled || u.login_locked === true ? "unlock" : "lock";
}

/** P0-VPS-OPS (#131): khóa tạm 15 phút rồi tự mở; Admin mở sớm được. Có mốc từ máy chủ thì báo giờ tự mở. */
export function loginLockText(u: Pick<AccountsUser, "login_locked" | "login_locked_until">): string | null {
  if (u.login_locked !== true) return null;
  return u.login_locked_until
    ? `Khóa do đăng nhập sai — tự mở lúc ${formatDateTime(u.login_locked_until)}`
    : "Khóa do đăng nhập sai — tự mở sau 15 phút kể từ lần sai cuối";
}

/** D24 — trạng thái TOTP của tài khoản (chỉ có ở GET /v1/accounts/users/{id}). */
export type MfaState = "configured" | "pending" | "missing" | "optional";

export function mfaState(m: AccountsUser["mfa"]): MfaState | null {
  if (!m) return null;
  if (m.configured) return "configured";
  if (m.enrollment_pending) return "pending";
  return m.required ? "missing" : "optional";
}

export const MFA_STATE: Record<MfaState, { text: string; cls: string }> = {
  configured: { text: "Đã đăng ký TOTP", cls: "c-ok" },
  pending: { text: "Chờ đăng ký TOTP ở lần đăng nhập tới", cls: "c-info" },
  missing: { text: "Bắt buộc TOTP — chưa đăng ký", cls: "c-bad" },
  optional: { text: "Không bắt buộc TOTP", cls: "c-mute" },
};

/** Đặt lại TOTP có ý nghĩa khi đã đăng ký, đang chờ đăng ký, hoặc vai bắt buộc TOTP (máy chủ vẫn kiểm). */
export function canResetTotp(u: Pick<AccountsUser, "enabled" | "mfa">): boolean {
  const s = mfaState(u.mfa);
  return u.enabled && (s === "configured" || s === "pending" || s === "missing");
}

/** Vai được cấp cho một loại tổ chức theo `role_org_kinds` (vai không ràng buộc → mọi loại). */
export function rolesForOrgKind(grants: AccountsGrants | undefined, kind: OrgKind | null | undefined): string[] {
  if (!grants) return [];
  return grants.grantable.filter((r) => {
    const kinds = grants.role_org_kinds[r];
    return !kinds || kinds.length === 0 || (!!kind && kinds.includes(kind));
  });
}

export function fullName(u: Pick<AccountsUser, "first_name" | "last_name">): string {
  return `${u.last_name} ${u.first_name}`.trim();
}

export function importAccounts(admin: AdminApiClient, file: UploadFile, idemKey: string): Promise<AccountsImportResult> {
  return unwrap(
    admin.POST("/v1/accounts/users/import", {
      params: { header: { "Idempotency-Key": idemKey } },
      // Thân là nội dung tệp nguyên văn (CSV hoặc .xlsx) — không parse ở client.
      body: file.body as unknown as string,
      bodySerializer: (b: unknown) => b as BodyInit,
      headers: { "Content-Type": file.contentType },
    }),
  );
}
