import { describe, expect, it } from "vitest";
import { resetPriceConfig } from "@/api/mock/kn-price";
import {
  renderTemplate,
  scheduleProblems,
  unknownVars,
  validPct,
  type PriceScheduleInput,
} from "./api";

// Thứ Hai 12/10/2026 10:00 giờ Việt Nam.
const T0 = Date.parse("2026-10-12T03:00:00Z");

const input = (
  patch: Partial<PriceScheduleInput> = {},
): PriceScheduleInput => ({
  weekdays: ["mon", "tue", "wed", "thu", "fri"],
  lock_at: "12:00",
  respond_by: "13:30",
  remind1_at: "07:30",
  remind2_at: "11:00",
  channels: ["in_app"],
  template: "Nhập giá {ngày} cho {xã} trước {giờ khóa}",
  valid_from: "2026-10-13",
  ...patch,
});

describe("W11.74 cấu hình Báo giá — quy tắc phía máy khách", () => {
  it("FN-AD-04.04 AC-05, FN-AD-04.05 AC-01: thứ tự giờ; biến mẫu lạ bị chỉ ra", () => {
    expect(scheduleProblems(input())).toEqual([]);
    expect(scheduleProblems(input({ respond_by: "11:30" }))).toContain(
      "Hạn phản hồi xác minh phải sau giờ khóa.",
    );
    expect(scheduleProblems(input({ remind2_at: "12:00" })).join(" ")).toMatch(
      /lần 2 phải trước giờ khóa/,
    );
    expect(unknownVars("Nhắc {ngày} {tên} {xã} {tên}")).toEqual(["{tên}"]);
    expect(
      renderTemplate(
        "Ngày {ngày}, {xã}, trước {giờ khóa}",
        "2026-10-12",
        "",
        "12:00",
      ),
    ).toBe("Ngày 12/10/2026, xã, trước 12:00");
  });

  it("FN-AD-04.06: ngưỡng 0 < % ≤ 100, tối đa 2 số lẻ, dạng chuỗi", () => {
    for (const ok of ["30", "30.5", "0.01", "100", "100.00", "40.25"])
      expect(validPct(ok), ok).toBe(true);
    for (const bad of ["0", "0.00", "100.01", "101", "30.123", "-5", "abc", ""])
      expect(validPct(bad), bad).toBe(false);
  });
});

describe("W11.74 backend giả lập /v1/price/calendar*, /v1/price/thresholds*", () => {
  const store = () => resetPriceConfig(() => T0);

  it("lịch mặc định T2–T6, khóa 12:00; đổi trong ngày báo giá chỉ từ ngày mai; ngoại lệ khớp lịch", () => {
    const s = store();
    const c = s.calendar(true);
    expect(c.current.lock_at).toBe("12:00");
    expect(c.earliest_valid_from).toBe("2026-10-13");
    expect(c.today.lock_at).toBe("2026-10-12T05:00:00Z");
    expect(c.days[5]).toMatchObject({
      day: "2026-10-17",
      kind: "off",
      quote: false,
    });
    expect(
      s.saveSchedule(true, input({ valid_from: "2026-10-12" })),
    ).toMatchObject({ ok: false, code: "price.valid_from" });
    expect(s.saveSchedule(false, input())).toMatchObject({
      ok: false,
      status: 403,
    });
    expect(
      s.saveSchedule(
        true,
        input({
          lock_at: "11:00",
          remind2_at: "10:30",
          valid_from: "2026-10-14",
        }),
      ),
    ).toMatchObject({ ok: true, status: 201 });
    const c2 = s.calendar(true);
    expect(c2.days[1]!.lock_at).toBe("2026-10-13T05:00:00Z");
    expect(c2.days[2]!.lock_at).toBe("2026-10-14T04:00:00Z");
    expect(
      s.addException(true, {
        day: "2026-10-15",
        kind: "holiday",
        reason: "Nghỉ lễ",
      }),
    ).toMatchObject({ ok: true });
    expect(
      s.addException(true, { day: "2026-10-15", kind: "holiday", reason: "x" }),
    ).toMatchObject({ code: "price.exception_exists" });
    expect(
      s.addException(true, { day: "2026-10-17", kind: "holiday", reason: "x" }),
    ).toMatchObject({ code: "price.exception_mismatch" });
    expect(s.calendar(true).days[3]).toMatchObject({
      kind: "holiday",
      quote: false,
    });
  });

  it("ngưỡng: mặc định 30 %, không xóa được mặc định; nhóm thêm/xóa có lịch sử", () => {
    const s = store();
    expect(s.thresholds(true)).toMatchObject({
      default: { pct: "30.00", lookback_days: 7 },
      default_stored: false,
    });
    expect(
      s.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "40" }),
    ).toMatchObject({ ok: true, body: { pct: "40.00" } });
    expect(
      s.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "0" }),
    ).toMatchObject({ ok: false, status: 422 });
    expect(s.retireThreshold(true, "default", "")).toMatchObject({
      code: "price.default_threshold",
    });
    expect(s.retireThreshold(true, "group", "rau_mau")).toMatchObject({
      ok: true,
      status: 204,
    });
    expect(s.thresholds(true).items).toHaveLength(0);
    expect(s.thresholds(true).history).toHaveLength(2);
  });
});
