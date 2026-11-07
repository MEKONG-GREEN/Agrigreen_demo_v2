// CNTS-V4-HOUSEHOLD-LIST (#251, BRD CNTS v4.0 FN-10): quy tắc thuần của màn danh sách hộ chăn nuôi — kiểm biểu mẫu như máy
// chủ (HCN-02…HCN-06), thân gửi, check-in GPS (BR-03: không nhập tay; không bắt được GPS → báo thử lại).
import { describe, expect, it } from "vitest";
import { accuracyText, captureCheckin, emptyForm, formOf, payload, validateHousehold, type HouseholdDetail, type HouseholdForm } from "./api";

const good = (): HouseholdForm => ({
  owner_name: "  Nguyễn  Văn An ",
  owner_phone: "0912000123",
  commune_code: "30001",
  hamlet: " Ấp Mỹ Hòa ",
  livestock_type: "nong_ho",
  animals: [
    { species_code: "GA", head_count: "120" },
    { species_code: "VIT", head_count: "0" },
  ],
});

describe("validateHousehold", () => {
  it("nhận biểu mẫu hợp lệ", () => {
    expect(validateHousehold(good(), "create")).toEqual({});
  });
  it("bắt buộc họ tên, xã, loại, vật nuôi khi thêm", () => {
    const e = validateHousehold(emptyForm(), "create");
    expect(Object.keys(e).sort()).toEqual(["animals[0].head_count", "animals[0].species_code", "commune_code", "livestock_type", "owner_name"]);
  });
  it("sửa: họ tên trống = giữ nguyên", () => {
    expect(validateHousehold({ ...good(), owner_name: "" }, "edit")).toEqual({});
  });
  it("chặn số con âm, thập phân, vật nuôi trùng, SĐT có chữ, quá 200 ký tự", () => {
    const f: HouseholdForm = {
      ...good(),
      owner_name: "a".repeat(201),
      owner_phone: "09a",
      animals: [
        { species_code: "GA", head_count: "-1" },
        { species_code: "GA", head_count: "1,5" },
      ],
    };
    const e = validateHousehold(f, "create");
    expect(e.owner_name).toBeTruthy();
    expect(e.owner_phone).toBe("Chỉ nhập chữ số.");
    expect(e["animals[0].head_count"]).toBeTruthy();
    expect(e["animals[1].head_count"]).toBeTruthy();
    expect(e["animals[1].species_code"]).toBe("Vật nuôi trùng dòng khác.");
  });
});

describe("payload", () => {
  it("chuẩn hóa họ tên, ấp; số con là số nguyên; kèm check-in và xác nhận hộ khác", () => {
    const checkin = { lat: 10.1, lon: 105.2, accuracy_m: "12.5", mocked: false, captured_at: "2027-10-13T03:00:00.000Z" };
    expect(payload(good(), { checkin, confirm_distinct: true })).toEqual({
      owner_name: "Nguyễn Văn An",
      owner_phone: "0912000123",
      commune_code: "30001",
      hamlet: "Ấp Mỹ Hòa",
      livestock_type: "nong_ho",
      animals: [
        { species_code: "GA", head_count: 120 },
        { species_code: "VIT", head_count: 0 },
      ],
      checkin,
      confirm_distinct: true,
    });
  });
  it("sửa: không gửi họ tên/SĐT trống, có base_version", () => {
    const b = payload({ ...good(), owner_name: "", owner_phone: "" }, { base_version: 3 });
    expect(b.owner_name).toBeUndefined();
    expect(b.owner_phone).toBeUndefined();
    expect(b.base_version).toBe(3);
  });
  it("formOf lấy họ tên đầy đủ, để trống SĐT (máy chủ chỉ trả SĐT che)", () => {
    const d = {
      owner_name: "Nguyễn Văn An",
      commune_code: "30001",
      hamlet: null,
      livestock_type: "trang_trai_nho",
      animals: [{ species_code: "GA", species_name: "Gà", head_count: 5 }],
    } as unknown as HouseholdDetail;
    expect(formOf(d)).toEqual({ owner_name: "Nguyễn Văn An", owner_phone: "", commune_code: "30001", hamlet: "", livestock_type: "trang_trai_nho", animals: [{ species_code: "GA", head_count: "5" }] });
  });
});

describe("check-in GPS", () => {
  it("độ chính xác ≤ 2 chữ số lẻ, không dạng mũ", () => {
    expect(accuracyText(12.3456)).toBe("12.35");
    expect(accuracyText(80)).toBe("80");
    expect(accuracyText(1e21)).not.toMatch(/e/);
    expect(accuracyText(Number.NaN)).toBe("0");
  });
  it("lấy tọa độ từ thiết bị", async () => {
    const geo = {
      getCurrentPosition: (ok: PositionCallback) => ok({ coords: { latitude: 10.5, longitude: 105.1, accuracy: 7.25 }, timestamp: Date.UTC(2027, 9, 13) } as GeolocationPosition),
    } as unknown as Geolocation;
    await expect(captureCheckin(geo)).resolves.toEqual({ lat: 10.5, lon: 105.1, accuracy_m: "7.25", mocked: false, captured_at: "2027-10-13T00:00:00.000Z" });
  });
  it("không bắt được GPS hoặc không có GPS → báo thử lại", async () => {
    const geo = { getCurrentPosition: (_: PositionCallback, fail: PositionErrorCallback) => fail({} as GeolocationPositionError) } as unknown as Geolocation;
    await expect(captureCheckin(geo)).rejects.toThrow(/thử lại/);
    await expect(captureCheckin(undefined)).rejects.toThrow(/GPS/);
  });
});
