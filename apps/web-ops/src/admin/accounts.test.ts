import { describe, expect, it } from "vitest";
import { accountState, canResetTotp, lockReasonError, mfaState, rolesForOrgKind } from "./accounts";

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
});
