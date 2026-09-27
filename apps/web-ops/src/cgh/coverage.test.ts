import { describe, expect, it } from "vitest";
import { compareDecimal, toScaled } from "@/lib/decimal";
import { coverageStatus, DEFAULT_THRESHOLDS } from "./coverage";

describe("coverageStatus — ngưỡng BRD CGH (Đủ ≥85, Chú ý 60–<85, Thiếu <60, Thừa ≥120)", () => {
  it.each([
    ["0", "thieu"],
    ["59.99", "thieu"],
    ["59.9999", "thieu"],
    ["60", "chu_y"],
    ["60.00", "chu_y"],
    ["84.99", "chu_y"],
    ["84.9999", "chu_y"],
    ["85", "du"],
    ["85.0000", "du"],
    ["100", "du"],
    ["119.99", "du"],
    ["119.9999", "du"],
    ["120", "thua"],
    ["120.0001", "thua"],
    ["250", "thua"],
  ] as const)("%s%% → %s", (pct, expected) => {
    expect(coverageStatus(pct)).toBe(expected);
  });

  it("nhận cả số", () => {
    expect(coverageStatus(60)).toBe("chu_y");
    expect(coverageStatus(85)).toBe("du");
    expect(coverageStatus(120)).toBe("thua");
    expect(coverageStatus(59.5)).toBe("thieu");
  });

  it("không có dữ liệu → xám, không tính vào mức đáp ứng", () => {
    expect(coverageStatus(null)).toBe("khong_du_lieu");
    expect(coverageStatus(undefined)).toBe("khong_du_lieu");
    expect(coverageStatus("")).toBe("khong_du_lieu");
  });

  it("chưa cấu hình ngưỡng → không phân loại", () => {
    expect(coverageStatus("50", null)).toBe("chua_cau_hinh");
    // nhưng vẫn ưu tiên báo thiếu dữ liệu
    expect(coverageStatus(null, null)).toBe("khong_du_lieu");
  });

  it("ngưỡng tuỳ chỉnh theo vụ", () => {
    const t = { ...DEFAULT_THRESHOLDS, sufficient_pct: "90", surplus_pct: "130" };
    expect(coverageStatus("89.99", t)).toBe("chu_y");
    expect(coverageStatus("90", t)).toBe("du");
    expect(coverageStatus("129.99", t)).toBe("du");
    expect(coverageStatus("130", t)).toBe("thua");
  });
});

describe("compareDecimal — không qua float", () => {
  it("so chính xác quanh ngưỡng", () => {
    expect(compareDecimal("84.999999", "85")).toBe(-1);
    expect(compareDecimal("85.000000", "85")).toBe(0);
    expect(compareDecimal("0.1", "0.10")).toBe(0);
    expect(compareDecimal("-1", "0")).toBe(-1);
  });
  it("chuỗi không hợp lệ", () => {
    expect(toScaled("abc")).toBeNull();
    expect(toScaled(".")).toBeNull();
    expect(() => compareDecimal("x", "1")).toThrow();
  });
});
