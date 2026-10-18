import { describe, expect, it } from "vitest";
import { movedTotal, normalizeMST } from "./htxAnchors";

describe("D2 neo HTX — chuẩn hóa MST như máy chủ", () => {
  it("bỏ khoảng trắng, gạch; chi nhánh 13 số → 10-3", () => {
    expect(normalizeMST(" 0312 345-678 ")).toBe("0312345678");
    expect(normalizeMST("0312345678001")).toBe("0312345678-001");
    expect(normalizeMST("0312345678-002")).toBe("0312345678-002");
  });
  it("sai dạng → null", () => {
    for (const bad of [
      "",
      "031234567",
      "0312345678-000",
      "03123A5678",
      "0312.345.678",
    ])
      expect(normalizeMST(bad)).toBeNull();
  });
  it("tổng dòng chuyển chủ", () => {
    const counts = {
      plots: 2,
      memberships: 3,
      seasons: 1,
      farm_logs: 4,
      season_areas: 0,
    };
    expect(
      movedTotal({
        moved: [
          { org_ref: "a", counts },
          { org_ref: "b", counts },
        ],
      }),
    ).toBe(20);
  });
});
