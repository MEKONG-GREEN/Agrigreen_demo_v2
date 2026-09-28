import { describe, expect, it } from "vitest";
import { accountState, lockReasonError, rolesForOrgKind } from "./accounts";

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
