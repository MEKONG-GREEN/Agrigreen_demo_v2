// pf/accounts (Admin API) — tài khoản cán bộ trong Keycloak. Ma trận cấp vai do BACKEND trả và kiểm
// (GET /v1/accounts/grants); web chỉ lọc lựa chọn cho gọn và hiện lỗi của máy chủ.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { AccountsGrants, AccountsImportResult, AccountsUser, OrgKind } from "@/api/types";
import type { UploadFile } from "@/lib/files";

/** Lý do khóa/mở khóa tối thiểu 20 ký tự (HTX US-HTXSTATUS-01 AC-3). */
export const LOCK_REASON_MIN = 20;

/** Kiểm sơ bộ phía client — máy chủ vẫn kiểm (422 validation_failed). Hợp lệ → null. */
export function lockReasonError(reason: string): string | null {
  const n = reason.trim().length;
  if (n >= LOCK_REASON_MIN) return null;
  return n === 0 ? `Nhập lý do (tối thiểu ${LOCK_REASON_MIN} ký tự).` : `Lý do tối thiểu ${LOCK_REASON_MIN} ký tự — còn thiếu ${LOCK_REASON_MIN - n} ký tự.`;
}

export type AccountState = "active" | "locked" | "locked_org" | "pending";

export function accountState(u: Pick<AccountsUser, "enabled" | "lock_cause" | "required_actions">): AccountState {
  if (!u.enabled) return u.lock_cause === "org_deactivated" ? "locked_org" : "locked";
  if (u.required_actions.includes("VERIFY_EMAIL") || u.required_actions.includes("UPDATE_PASSWORD")) return "pending";
  return "active";
}

export const ACCOUNT_STATE: Record<AccountState, { text: string; cls: string }> = {
  active: { text: "Đang hoạt động", cls: "c-ok" },
  pending: { text: "Chờ kích hoạt", cls: "c-info" },
  locked: { text: "Đã khóa", cls: "c-bad" },
  locked_org: { text: "Khóa theo tổ chức", cls: "c-mute" },
};

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
