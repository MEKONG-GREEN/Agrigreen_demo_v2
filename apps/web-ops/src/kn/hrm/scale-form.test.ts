import { describe, expect, it } from "vitest";
import { effectiveScale, scalePath, validScaleDay, validateScale, type ScaleValues } from "./scale-form";
import type { HrmScale } from "./api";

const valid = (): ScaleValues => ({ from: "2026-10-12", hold: "36", coefficients: ["2,34", "2,67"], note: "Căn cứ" });
describe("FN19 — giới hạn trước chuẩn hóa số", () => {
  it("chuẩn hóa hệ số Việt Nam, giữ thứ tự và tháng nguyên", () => {
    expect(validateScale(valid()).body).toEqual({ valid_from: "2026-10-12", hold_months: 36, coefficients: ["2.34", "2.67"], note: "Căn cứ" });
  });
  it("chặn số mũ, chuỗi 100k và số rất lớn tại từng ô của cả 20 bậc và tháng", () => {
    for (const hostile of ["1e100000", "1e-100000", "9".repeat(100_000), "99999999999999999999", "Infinity", "NaN"]) {
      const values = valid(); values.coefficients = Array.from({ length: 20 }, (_, i) => String(i + 1));
      expect(validateScale({ ...values, hold: hostile }).errors.hold).toBeTruthy();
      for (let index = 0; index < 20; index++) {
        const coefficients = [...values.coefficients]; coefficients[index] = hostile;
        expect(validateScale({ ...values, coefficients }).errors[`coefficient_${index}`]).toBeTruthy();
      }
    }
  });
  it("biên domain: tháng 1..120, 1..20 bậc, hệ số dương <=99,99, 2 chữ số và tăng dần", () => {
    for (const hold of ["", "0", "-1", "1.5", "121"]) expect(validateScale({ ...valid(), hold }).valid).toBe(false);
    for (const hold of ["1", "120"]) expect(validateScale({ ...valid(), hold }).valid).toBe(true);
    for (const coefficients of [[], Array(21).fill("1"), ["0"], ["100"], ["1.234"], ["2", "2"], ["3", "2"], [""]]) expect(validateScale({ ...valid(), coefficients }).valid).toBe(false);
    expect(validateScale({ ...valid(), coefficients: ["0,01", "99,99"] }).valid).toBe(true);
    expect(validateScale({ ...valid(), note: "ấ".repeat(501) }).errors.note).toBeTruthy();
  });
  it("ngày lịch thật, ngày zero Go và dữ liệu chưa chọn", () => {
    for (const day of ["", "0001-01-01", "2026-02-29", "2026-13-01", "2026-10-32", "2026-1-01"]) { expect(validScaleDay(day)).toBe(false); expect(validateScale({ ...valid(), from: day }).errors.from).toBeTruthy(); }
    expect(validScaleDay("2028-02-29")).toBe(true);
  });
  it("chọn đúng hiệu lực mà không sửa lịch sử API; giữ prefix phòng ban", () => {
    const versions = [{ valid_from: "2026-01-01", version: 1 }, { valid_from: "2027-01-01", version: 3 }, { valid_from: "2026-01-01", version: 2 }] as HrmScale[];
    const before = JSON.stringify(versions);
    expect(effectiveScale(versions, "2026-10-12")?.version).toBe(2);
    expect(effectiveScale(versions, "2025-12-31")).toBeNull();
    expect(JSON.stringify(versions)).toBe(before);
    expect(scalePath("/kn/phong-ban/hanh_chinh/danh-muc-nhan-su", "A/B", "2026-10-12", true)).toBe("/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/he-so/A%2FB/them-moi?at=2026-10-12");
  });
});
