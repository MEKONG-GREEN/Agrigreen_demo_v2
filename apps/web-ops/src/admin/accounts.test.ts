import { describe, expect, it } from "vitest";
import { formatDateTime } from "@/lib/format";
import { accountState, canResetTotp, lockOperation, lockReasonError, loginLockState, loginLockText, mfaState, rolesForOrgKind } from "./accounts";

describe("pf/accounts — kiểm sơ bộ phía client", () => {
  it("lý do khóa/mở khóa tối thiểu 20 ký tự (đếm sau khi bỏ khoảng trắng)", () => {
    expect(lockReasonError("")).toMatch(/tối thiểu 20/);
    expect(lockReasonError("   nghỉ phép        ")).toMatch(/còn thiếu 11/);
    expect(lockReasonError("Nghỉ phép dài hạn theo QĐ 12")).toBeNull();
  });

  it("trạng thái tài khoản: khóa theo tổ chức, chờ kích hoạt", () => {
    expect(accountState({ enabled: false, lock_cause: "org_deactivated", required_actions: [] })).toBe("locked_org");
    expect(accountState({ enabled: false, lock_cause: "manual", required_actions: [] })).toBe("locked");
    expect(accountState({ enabled: true, lock_cause: null, required_actions: ["VERIFY_EMAIL"] })).toBe("pending");
    expect(accountState({ enabled: true, lock_cause: null, required_actions: [] })).toBe("active");
    expect(accountState({ enabled: false, lock_cause: "dormant_45d", required_actions: [] })).toBe("locked_dormant");
  });

  it("D24 — trạng thái TOTP và điều kiện đặt lại", () => {
    expect(mfaState(undefined)).toBeNull();
    expect(mfaState({ required: true, configured: true, enrollment_pending: false })).toBe("configured");
    expect(mfaState({ required: true, configured: false, enrollment_pending: true })).toBe("pending");
    expect(mfaState({ required: true, configured: false, enrollment_pending: false })).toBe("missing");
    expect(mfaState({ required: false, configured: false, enrollment_pending: false })).toBe("optional");
    expect(canResetTotp({ enabled: true, mfa: { required: true, configured: true, enrollment_pending: false } })).toBe(true);
    expect(canResetTotp({ enabled: false, mfa: { required: true, configured: true, enrollment_pending: false } })).toBe(false);
    expect(canResetTotp({ enabled: true, mfa: { required: false, configured: false, enrollment_pending: false } })).toBe(false);
  });

  it("vai cho loại tổ chức theo role_org_kinds do backend trả", () => {
    const grants = {
      grantable: ["kn_officer", "coop_manager", "reports_admin"],
      mfa_roles: [],
      role_org_kinds: { kn_officer: ["extension_team"], coop_manager: ["cooperative"] },
    };
    expect(rolesForOrgKind(grants, "cooperative")).toEqual(["coop_manager", "reports_admin"]);
    expect(rolesForOrgKind(grants, "extension_team")).toEqual(["kn_officer", "reports_admin"]);
    expect(rolesForOrgKind(undefined, "cooperative")).toEqual([]);
  });

  it("ADMIN-UNLOCK (#61): khóa đăng nhập sai tách khỏi enabled; thiếu field = chưa biết, không coi là mở", () => {
    expect(loginLockState({})).toBe("unknown");
    expect(loginLockState({ login_locked: false })).toBe("clear");
    expect(loginLockState({ login_locked: true })).toBe("locked");
    // enabled=true nhưng Keycloak đang khóa do sai mật khẩu → thao tác là MỞ khóa.
    expect(lockOperation({ enabled: true, login_locked: true })).toBe("unlock");
    expect(lockOperation({ enabled: true, login_locked: false })).toBe("lock");
    expect(lockOperation({ enabled: true })).toBe("lock");
    expect(lockOperation({ enabled: false, login_locked: false })).toBe("unlock");
  });

  it("ADMIN-UNLOCK (#61): khóa tạm 15 phút (P0-VPS-OPS) — báo giờ tự mở khi máy chủ trả mốc", () => {
    const until = "2026-10-26T03:15:00Z";
    const t = loginLockText({ login_locked: true, login_locked_until: until })!;
    expect(t).toContain("Khóa do đăng nhập sai");
    expect(t).toContain(`tự mở lúc ${formatDateTime(until)}`);
    expect(t).toContain("10:15");
    expect(loginLockText({ login_locked: true })).toBe("Khóa do đăng nhập sai — tự mở sau 15 phút kể từ lần sai cuối");
    expect(loginLockText({ login_locked: false })).toBeNull();
    expect(loginLockText({})).toBeNull();
  });
});
