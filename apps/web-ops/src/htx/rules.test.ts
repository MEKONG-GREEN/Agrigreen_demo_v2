import { describe, expect, it } from "vitest";
import type { CropSOPStep } from "@/api/types";
import {
  addDays,
  harvestRowError,
  locationLabel,
  MSG_HARVEST_STEP,
  MSG_SOWING_WINDOW,
  MSG_STEP_ORDER,
  moveStep,
  normalizePhone,
  parseDecimalInput,
  previewYield,
  reasonError,
  renumber,
  sowingDateError,
  toCsv,
  todayVN,
  validateSteps,
} from "./rules";

const step = (seq: number, activity: CropSOPStep["activity_code"] = "bon_phan", name = `Bước ${seq}`): CropSOPStep => ({
  seq,
  name,
  activity_code: activity,
  day_offset: seq * 10,
  window_days: 3,
  mandatory: true,
});

describe("bước SOP (HTX US-CAT-01 AC-6)", () => {
  it("seq liên tục 1..n hợp lệ", () => {
    expect(validateSteps([step(1, "lam_dat"), step(2), step(3, "thu_hoach")], true)).toEqual([]);
  });

  it("seq nhảy cóc hoặc trùng → thông báo thứ tự bước", () => {
    const issues = validateSteps([step(1), step(3), step(2), step(2)]);
    expect(issues.filter((i) => i.field === "seq").map((i) => i.index)).toEqual([1, 2, 3]);
    expect(issues[0]?.message).toBe(MSG_STEP_ORDER);
  });

  it("phát hành cần bước thu hoạch; lưu nháp thì không", () => {
    expect(validateSteps([step(1)], false)).toEqual([]);
    expect(validateSteps([step(1)], true).map((i) => i.message)).toContain(MSG_HARVEST_STEP);
  });

  it("tên trống, khoảng ngày ngoài giới hạn bị báo", () => {
    const bad = { ...step(1), name: "  ", day_offset: 400, window_days: -1, phi_days: 200 };
    expect(validateSteps([bad]).map((i) => i.field).sort()).toEqual(["day_offset", "name", "phi_days", "window_days"]);
    expect(validateSteps([]).map((i) => i.field)).toEqual(["steps"]);
  });

  it("đổi chỗ và đánh số lại giữ seq liên tục", () => {
    const moved = moveStep([step(1, "lam_dat", "A"), step(2, "gieo_sa", "B"), step(3, "thu_hoach", "C")], 2, -1);
    expect(moved.map((s) => `${s.seq}${s.name}`)).toEqual(["1A", "2C", "3B"]);
    expect(moveStep(moved, 0, -1)).toEqual(moved);
    expect(renumber([step(5), step(9)]).map((s) => s.seq)).toEqual([1, 2]);
  });
});

describe("lý do sửa/hủy (RB-1)", () => {
  it("tối thiểu 10 ký tự sau khi bỏ khoảng trắng", () => {
    expect(reasonError("   ")).toBe("Bắt buộc nhập lý do.");
    expect(reasonError("  ngắn  ")).toMatch(/tối thiểu 10/);
    expect(reasonError("Nhập nhầm giống lúa")).toBeNull();
    expect(reasonError("12345678901234567890", 20)).toBeNull();
  });
});

describe("SĐT chủ hộ (HTX US-ACC-01)", () => {
  it("chuẩn hoá về 0xxxxxxxxx", () => {
    expect(normalizePhone("0999 012 345")).toBe("0999012345");
    expect(normalizePhone("+84999012345")).toBe("0999012345");
    expect(normalizePhone("84999012345")).toBe("0999012345");
    expect(normalizePhone("0999.012.345")).toBe("0999012345");
    expect(normalizePhone("099901234")).toBeNull();
    expect(normalizePhone("abc")).toBeNull();
  });
});

describe("số thập phân nhập tay (R-10)", () => {
  it("dấu phẩy thập phân, bỏ số 0 thừa, giới hạn chữ số lẻ", () => {
    expect(parseDecimalInput("6.250,5".replace(".", ""))).toBe("6250.5");
    expect(parseDecimalInput("0012,500")).toBe("12.5");
    expect(parseDecimalInput("7")).toBe("7");
    expect(parseDecimalInput("1.2345")).toBeNull();
    expect(parseDecimalInput("-3")).toBeNull();
    expect(parseDecimalInput("-3", 3, true)).toBe("-3");
    expect(parseDecimalInput("1e3")).toBeNull();
  });
});

describe("ngày sạ (HTX US-SEASON-01 AC-2)", () => {
  it("trong [hôm nay − 15, hôm nay + 30]", () => {
    const today = "2026-09-29";
    expect(sowingDateError(today, today)).toBeNull();
    expect(sowingDateError(addDays(today, -15), today)).toBeNull();
    expect(sowingDateError(addDays(today, 30), today)).toBeNull();
    expect(sowingDateError(addDays(today, -16), today)).toBe(MSG_SOWING_WINDOW);
    expect(sowingDateError(addDays(today, 31), today)).toBe(MSG_SOWING_WINDOW);
    expect(sowingDateError("2026-02-30", today)).toBe("Ngày không hợp lệ.");
  });

  it("ngày nghiệp vụ theo giờ Việt Nam", () => {
    // 29/09/2026 18:30 UTC = 30/09/2026 01:30 giờ Việt Nam.
    expect(todayVN(new Date("2026-09-29T18:30:00Z"))).toBe("2026-09-30");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
  });
});

describe("bảng nhập nhanh sản lượng (HTX US-YIELD-02/03)", () => {
  const today = "2026-09-29";
  it("thiếu/sai số/âm/ngày tương lai bị báo; 0 kg hợp lệ (backend yêu cầu xác nhận bất thường)", () => {
    expect(harvestRowError({ mass: "", date: today }, today)).toMatch(/Nhập sản lượng/);
    expect(harvestRowError({ mass: "abc", date: today }, today)).toMatch(/phải là số/);
    expect(harvestRowError({ mass: "-5", date: today }, today)).toBe("Sản lượng không được âm.");
    expect(harvestRowError({ mass: "6500", date: "2026-10-01" }, today)).toMatch(/tương lai/);
    expect(harvestRowError({ mass: "0", date: today }, today)).toBeNull();
    expect(harvestRowError({ mass: "6500,5", date: "" }, today)).toBeNull();
  });

  it("năng suất xem trước t/ha", () => {
    expect(previewYield("6500", "1.0000")).toBe("6.50");
    expect(previewYield("6500", "0")).toBeNull();
  });
});

describe("nhãn vị trí (HTX US-LOG-05 AC-1)", () => {
  it("mismatch/missing có nhãn cảnh báo đúng chữ", () => {
    expect(locationLabel("mismatch").text).toBe("Vị trí không khớp");
    expect(locationLabel("missing").text).toBe("Thiếu GPS");
    expect(locationLabel("match").cls).toBe("c-ok");
  });
});

describe("xuất CSV (HTX US-DASH-02, xuất phía client)", () => {
  it("BOM, thoát dấu phẩy/nháy, chặn công thức", () => {
    const csv = toCsv([
      ["Giống", "Ghi chú"],
      ["OM5451", 'có "nháy", phẩy'],
      ["=HYPERLINK(1)", "-12.5"],
    ]);
    expect(csv.startsWith("﻿")).toBe(true);
    expect(csv).toContain('"có ""nháy"", phẩy"');
    expect(csv).toContain("'=HYPERLINK(1)");
    expect(csv).toContain(",-12.5\r\n");
  });
});
