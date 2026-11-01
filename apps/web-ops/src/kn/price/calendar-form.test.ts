import { describe, expect, it, vi } from "vitest";
import { Decimal } from "@/lib/exactDecimal";
import { createPriceConfigStore } from "@/api/mock/kn-price";
import {
  LOOKBACK_RULE,
  numberTextError,
  PERCENT_RULE,
  scheduleInput,
  validateSchedule,
  validateThreshold,
  type ThresholdForm,
} from "./calendar-form";
import { configBase, parseConfigRoute, thresholdPath } from "./calendar-routes";
import { scheduleLog, thresholdLog } from "./calendar-view";

const c = () =>
  createPriceConfigStore(() => Date.parse("2026-10-12T03:00:00Z")).calendar(
    true,
  );
const threshold = (patch: Partial<ThresholdForm> = {}): ThresholdForm => ({
  scope: "default",
  code: "",
  pct: "30",
  lookback: "7",
  flat: "1",
  survey: "40",
  ...patch,
});

describe("routes cấu hình Báo giá", () => {
  it("có cùng đích từ canonical và alias, phân biệt mã với thao tác", () => {
    expect(configBase("/kn/bao-gia/cau-hinh/lich/sua")).toBe(
      "/kn/bao-gia/cau-hinh",
    );
    expect(
      configBase(
        "/kn/phong-ban/thong_tin_tuyen_truyen/bao-gia-lich/nguong/mac-dinh",
      ),
    ).toBe("/kn/phong-ban/thong_tin_tuyen_truyen/bao-gia-lich");
    expect(parseConfigRoute(["nguong", "group", "sua"])).toEqual({
      kind: "threshold",
      scope: "group",
      code: "sua",
      edit: false,
    });
    expect(parseConfigRoute(["nguong", "group", "sua", "sua"])).toEqual({
      kind: "threshold",
      scope: "group",
      code: "sua",
      edit: true,
    });
    expect(thresholdPath("item", "MH00001")).toBe("nguong/item/MH00001");
    expect(parseConfigRoute(["ngoai-le", "them-moi"])).toEqual({
      kind: "exception-new",
    });
  });
  it.each([
    ["lich", "xoa"],
    ["nguong", "default", "anything"],
    ["nguong", "item", "x"],
    ["nguong", "group", "../x"],
    ["nhac", "sua", "extra"],
    ["ngoai-le", "2026-02-31"],
  ])("từ chối route ngoài phạm vi %j", (...parts) => {
    expect(parseConfigRoute(parts)).toBeNull();
  });
});

describe("cú pháp và biên trước chuẩn hóa số", () => {
  it.each([
    "1e100000",
    "1e-100000",
    "9".repeat(100000),
    "-0.01",
    "0",
    "100.01",
    "NaN",
    "Infinity",
    "0x10",
    "1.",
    ".5",
  ])("chặn cú pháp/biên trước parse ca %#", (raw) => {
    const parse = vi.spyOn(Decimal, "parse");
    try {
      expect(numberTextError(raw, PERCENT_RULE)).toBeTruthy();
      expect(validateThreshold(threshold({ pct: raw })).valid).toBe(false);
      expect(parse).not.toHaveBeenCalled();
    } finally {
      parse.mockRestore();
    }
  });
  it("đúng N1..60; phần trăm 0.01..100 và số lẻ chuẩn hóa", () => {
    for (const raw of ["1", "60"])
      expect(numberTextError(raw, LOOKBACK_RULE)).toBeUndefined();
    for (const raw of ["0", "61", "1.1", "1e1"])
      expect(numberTextError(raw, LOOKBACK_RULE)).toBeTruthy();
    expect(
      validateThreshold(
        threshold({ pct: "100,00", flat: "0,01", lookback: "60" }),
      ),
    ).toMatchObject({
      valid: true,
      values: { pct: "100", flat: "0.01", lookback: "60" },
    });
    expect(validateThreshold(threshold({ pct: "100.01" })).valid).toBe(false);
  });
  it("giới hạn mã đúng miền nhóm/mặt hàng", () => {
    expect(
      validateThreshold(threshold({ scope: "group", code: "rau_mau" })).valid,
    ).toBe(true);
    expect(
      validateThreshold(threshold({ scope: "item", code: "MH00001" })).valid,
    ).toBe(true);
    expect(
      validateThreshold(threshold({ scope: "item", code: "rau_mau" })).errors
        .code,
    ).toBeTruthy();
  });
});

describe("giữ quy tắc lịch và log nguồn", () => {
  it("kiểm các trường bắt buộc, thứ tự giờ và ngày áp dụng", () => {
    const form = scheduleInput(c());
    expect(validateSchedule(form, "2026-10-13", false)).toEqual({});
    expect(
      validateSchedule(
        {
          ...form,
          weekdays: [],
          respond_by: "11:30",
          valid_from: "2026-10-12",
        },
        "2026-10-13",
        false,
      ),
    ).toMatchObject({
      weekdays: expect.any(String),
      respond_by: expect.any(String),
      valid_from: expect.any(String),
    });
    expect(
      validateSchedule(
        { ...form, remind2_at: "12:00", channels: [], template: "{tên}" },
        "2026-10-13",
        true,
      ),
    ).toMatchObject({
      remind2_at: expect.any(String),
      channels: expect.any(String),
      template: expect.any(String),
    });
  });
  it("log dùng phiên bản/actor/thời điểm thật và giá trị trước → sau", () => {
    const store = createPriceConfigStore(() =>
      Date.parse("2026-10-12T03:00:00Z"),
    );
    const form = scheduleInput(store.calendar(true));
    store.saveSchedule(true, form);
    store.saveSchedule(true, { ...form, lock_at: "12:30" });
    expect(scheduleLog(store.calendar(true))[0]?.changes).toContainEqual({
      field: "Giờ khóa",
      old: "12:00",
      new: "12:30",
    });
    store.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "40" });
    store.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "45" });
    const log = thresholdLog(store.thresholds(true), "group", "rau_mau");
    expect(log[0]?.changes).toContainEqual({
      field: "Ngưỡng ±%",
      old: "40.00",
      new: "45.00",
    });
    expect(log[0]?.at).toBe("2026-10-12T03:00:00.000Z");
    expect(log[0]?.actor_display).toMatch(/^Mã cán bộ /);
    expect(thresholdLog(store.thresholds(true), "default", "")).toEqual([]);
  });
});
