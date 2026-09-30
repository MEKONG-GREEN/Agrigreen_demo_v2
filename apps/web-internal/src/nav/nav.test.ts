import { describe, expect, it } from "vitest";
import { buildPrincipal } from "@/auth/principal";
import { findNavItem, needsMfa, visibleNav } from "./nav";

const p = (roles: string[], amr: string[] = ["pwd", "otp"]) => buildPrincipal({ sub: "u", realm_access: { roles }, amr });

describe("menu theo vai và MFA", () => {
  it("tổ gom chỉ thấy tổng quan và thu gom", () => {
    const groups = visibleNav(p(["field_collector"])).map((g) => g.label);
    expect(groups).toEqual(["Tổng quan", "3. Thu gom hiện trường"]);
    expect(visibleNav(p(["field_collector"]))[1]!.items.map((i) => i.href)).not.toContain("/gom/kg-cuon");
  });
  it("màn hình quản trị cần phiên đã qua OTP", () => {
    const item = findNavItem("/hub/co-so");
    expect(needsMfa(item, p(["hub_operator"], ["pwd"]))).toBe(true);
    expect(needsMfa(item, p(["hub_operator"]))).toBe(false);
    expect(needsMfa(findNavItem("/hub/ton-kho"), p(["hub_operator"], ["pwd"]))).toBe(false);
  });
  it("amr otp/mfa → mfa = true", () => {
    expect(buildPrincipal({ amr: ["mfa"] }).mfa).toBe(true);
    expect(buildPrincipal({ amr: ["pwd"] }).mfa).toBe(false);
  });
});
