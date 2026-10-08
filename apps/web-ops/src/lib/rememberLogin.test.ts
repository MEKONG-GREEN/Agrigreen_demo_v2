import { afterEach, describe, expect, it } from "vitest";
import { forgetIdentifier, purgeLegacyIdentifier, REMEMBER_KEY, rememberIdentifier } from "./rememberLogin";

// M19: "Ghi nhớ đăng nhập" không để dữ liệu cá nhân ở localStorage; không bao giờ lưu SĐT.
describe("rememberIdentifier", () => {
  afterEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it("chỉ lưu khi người dùng tích chọn, và chỉ ở sessionStorage", () => {
    rememberIdentifier("an.nv@angiang.gov.vn", false);
    expect(window.sessionStorage.getItem(REMEMBER_KEY)).toBeNull();
    rememberIdentifier("  an.nv@angiang.gov.vn ", true);
    expect(window.sessionStorage.getItem(REMEMBER_KEY)).toBe("an.nv@angiang.gov.vn");
    expect(window.localStorage.getItem(REMEMBER_KEY)).toBeNull();
  });

  it("không bao giờ lưu SĐT (xóa giá trị cũ)", () => {
    rememberIdentifier("an.nv@angiang.gov.vn", true);
    rememberIdentifier("0901234567", true);
    expect(window.sessionStorage.getItem(REMEMBER_KEY)).toBeNull();
  });

  it("dọn giá trị bản cũ ở localStorage; đăng xuất xóa giá trị đã nhớ", () => {
    window.localStorage.setItem(REMEMBER_KEY, "0901234567");
    purgeLegacyIdentifier();
    expect(window.localStorage.getItem(REMEMBER_KEY)).toBeNull();
    rememberIdentifier("an.nv@angiang.gov.vn", true);
    forgetIdentifier();
    expect(window.sessionStorage.getItem(REMEMBER_KEY)).toBeNull();
  });
});
