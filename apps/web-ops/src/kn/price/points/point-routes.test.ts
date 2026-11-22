import { validatePointFields } from "./point-form";
import { describe, expect, it } from "vitest";
import { parsePointRoute, pointFilters, pointHref } from "./point-routes";

describe("route và kiểm nhập xã báo giá182", () => {
  it("chỉ nhận route định danh và thao tác đã đăng ký", () => {
    expect(parsePointRoute(["them-moi"])).toEqual({ kind: "point-new" });
    expect(parsePointRoute(["XBG0001", "phan-cong"])).toBeNull();
    expect(parsePointRoute(["nhom-vung", "an_giang", "sua"])).toEqual({
      kind: "region",
      code: "an_giang",
      edit: true,
    });
    for (const parts of [
      [],
      ["XBG0001", "xoa"],
      ["XBG0001", "sua", "extra"],
      [".."],
      ["XBG00001"],
      ["nhom-vung", "<script>"],
      ["XBG0001/../../x"],
      ["1e100000"],
      ["1".repeat(100000)],
    ])
      expect(parsePointRoute(parts)).toBeNull();
  });
  it("giữ ngày/bộ lọc khi đi chi tiết và quay lại, không chấp nhận redirect hoặc ngày giả", () => {
    const f = pointFilters(
      new URLSearchParams("at=2026-11-05&region=an_giang&tab=regions"),
      "2026-11-06",
    );
    expect(pointHref("nhom-vung/an_giang/sua", f)).toBe(
      "/kn/bao-gia/xa-bao-gia/nhom-vung/an_giang/sua?at=2026-11-05&tab=regions&region=an_giang",
    );
    for (const at of [
      "2026-02-30",
      "1e100000",
      "1e-100000",
      "9".repeat(100000),
    ])
      expect(
        pointFilters(
          new URLSearchParams({ at, region: "https://evil.invalid", tab: "x" }),
          "2026-11-06",
        ),
      ).toEqual({ at: "2026-11-06", region: "", tab: "points" });
  });
  it("bắt required và biên trước gửi; không tạo API từ form không hợp lệ", () => {
    const b = {
      admin_unit_code: "",
      region_code: "",
      valid_from: "",
      note: "",
      reason: "",
    };
    expect(validatePointFields(b, "create")).toMatchObject({
      valid: false,
      errors: {
        admin_unit_code: "Bắt buộc nhập.",
        region_code: "Bắt buộc nhập.",
      },
    });
    expect(
      validatePointFields(
        { ...b, admin_unit_code: "30286", region_code: "an_giang" },
        "create",
      ).valid,
    ).toBe(true);
    expect(
      validatePointFields({ ...b, reason: " " }, "retire").errors.reason,
    ).toBeTruthy();
    expect(
      validatePointFields(
        { ...b, reason: "Ngưng thu thập", valid_from: "2026-02-30" },
        "retire",
      ).errors.valid_from,
    ).toBeTruthy();
    for (const value of ["9".repeat(100000), "x".repeat(501)])
      expect(
        validatePointFields({ ...b, note: value }, "revise").errors.note,
      ).toBeTruthy();
  });
});
