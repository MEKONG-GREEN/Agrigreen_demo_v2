import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { SCALE_RULE, createCntsModelApi, decimalValue, defaultGrantValidFrom, grantErrors, money, parseLatLng, projectErrors, scaleError, scaleErrors, toggle } from "./api";

const A = "0190f1c4-7d3e-7000-8000-0000000000a1";

describe("CT/DA, hộ mô hình, cấp quyền CNTS — quy tắc biểu (W11.47)", () => {
  it("dự án: mã, tên, ngày, kinh phí số nguyên", () => {
    expect(projectErrors({ code: "DA 1", name: "", kind: "knqg", start_date: "2027-03-01", end_date: "2027-01-01", budget: "1,5" })).toEqual({
      code: expect.any(String),
      name: "Bắt buộc nhập.",
      end_date: "Không trước ngày bắt đầu.",
      budget: "Chỉ nhập số nguyên.",
    });
    expect(projectErrors({ code: "DA-01", name: "Dự án", kind: "tinh", start_date: "2027-03-01", budget: "-5" }).budget).toBe("Không được nhập số âm.");
    expect(projectErrors({ code: "DA-01", name: "Dự án", kind: "tinh", start_date: "2027-03-01" })).toEqual({});
    expect(money("450000000")).toBe("450.000.000 đ");
  });

  it("quy mô có đơn vị; tọa độ trong Việt Nam", () => {
    expect(scaleError("", "")).toBeNull();
    expect(scaleError("10", "")).toBe("Nhập cả quy mô và đơn vị.");
    expect(scaleError("-1", "con")).toBe("Không được nhập số âm.");
    expect(scaleErrors("0", "con")).toEqual({ scale: "Phải lớn hơn 0." });
    expect(scaleErrors("12,5", "")).toEqual({ scale_unit: "Nhập cả quy mô và đơn vị." });
    expect(decimalValue("1.234,5", SCALE_RULE)).toBe("1234.5");
    expect(parseLatLng("10.38, 105.43")).toEqual({ lat: 10.38, lng: 105.43 });
    expect(parseLatLng("")).toBeNull();
    expect(parseLatLng("48.8, 2.3")).toBe("invalid");
  });

  it("FN-14: phạm vi theo vai", () => {
    const base = { grantee_ref: A, commune_codes: ["91301"], valid_from: "2027-01-01" };
    expect(grantErrors({ ...base, role: "theo_doi_mo_hinh" }).site_refs).toBeTruthy();
    expect(grantErrors({ ...base, role: "ksgm", site_refs: [A] }).site_refs).toBeTruthy();
    expect(grantErrors({ ...base, role: "nong_ho", household_refs: [] }).household_refs).toBeTruthy();
    expect(grantErrors({ ...base, role: "ksgm", valid_to: "2026-01-01" }).valid_to).toBeTruthy();
    expect(grantErrors({ ...base, role: "ksgm" })).toEqual({});
    expect(toggle(["b"], "a")).toEqual(["a", "b"]);
    expect(toggle(["a", "b"], "a")).toEqual(["b"]);
  });
});

describe("giả lập CT/DA, điểm, hộ, quyền", () => {
  beforeEach(() => resetCntsModel());
  const mapi = createCntsModelApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }));

  it("tạo dự án → điểm → hộ (nhãn che bớt) → rút hộ; khóa lạc quan", async () => {
    const p = await mapi.createProject({ code: "da-x", name: "Dự án X", kind: "tinh", start_date: "2027-03-01" });
    expect(p.code).toBe("DA-X");
    await expect(mapi.createProject({ code: "DA-X", name: "Trùng", kind: "tinh", start_date: "2027-03-01" })).rejects.toThrow();
    await expect(mapi.createProject({ code: "DA-AM", name: "Kinh phí âm", kind: "tinh", start_date: "2027-03-01", budget: "-5" })).rejects.toThrow(); // BF-SWEEP-CNTS
    await expect(mapi.createSite({ project_ref: p.ref, commune_code: "91301", name: "Quy mô 0", model_type_code: "GA_THUONG_PHAM", scale: "0", scale_unit: "con" })).rejects.toThrow();
    const s = await mapi.createSite({ project_ref: p.ref, commune_code: "91301", name: "Điểm 1", model_type_code: "GA_THUONG_PHAM", location: { lat: 10.4, lng: 105.4, source: "map" } });
    expect(s.location).toEqual({ lat: 10.4, lng: 105.4, source: "map" });
    const h = await mapi.createHousehold({ site_ref: s.ref, owner_name: "Nguyễn Văn Bình", owner_phone: "0912345678", keeper_kind: "self", self_entry: true });
    expect(h.owner_label).toBe("Nguyễn V. B.");
    expect(h.owner_phone_masked).toBe("0912•••678");
    await expect(mapi.householdStatus(h.ref, true, 9, "Ngừng nuôi")).rejects.toThrow();
    const w = await mapi.householdStatus(h.ref, true, 1, "Ngừng nuôi");
    expect(w.status).toBe("withdrawn");
    expect(w.location).toBeNull();
    expect((await mapi.project(p.ref)).household_count).toBe(0);
  });

  it("cấp quyền, trùng vai bị chặn, thu hồi chuyển cho người nhận", async () => {
    const g = await mapi.createGrant({ grantee_ref: A, role: "ksgm", commune_codes: ["91301"], forms: ["bm-01"] });
    expect(g.forms).toEqual(["BM-01"]);
    expect(g.status).toBe("active");
    await expect(mapi.createGrant({ grantee_ref: A, role: "ksgm", commune_codes: ["91301"] })).rejects.toThrow();
    const heir = "0190f1c4-7d3e-7000-8000-0000000000b2";
    const r = await mapi.revokeGrant(g.ref, { base_version: 1, reason: "Chuyển công tác", successor_ref: heir });
    expect(r.status).toBe("revoked");
    const list = await mapi.grants();
    expect(list.items.find((x) => x.grantee_ref === heir)?.status).toBe("active");
  });
});

// KN-VN-DATE (#149): ngày hiệu lực mặc định và trạng thái quyền theo ngày lịch VN (00:00–07:00 VN ngày UTC còn là hôm qua).
describe("cấp quyền CNTS theo ngày VN", () => {
  const mapi = createCntsModelApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }));
  const MON_0030_VN = Date.parse("2026-10-25T17:30:00Z");
  const SUN_2359_VN = Date.parse("2026-10-25T16:59:00Z");
  beforeEach(() => resetCntsModel());
  afterEach(() => vi.useRealTimers());

  it("ô valid_from của màn mặc định là hôm nay theo giờ VN", () => {
    expect(defaultGrantValidFrom(MON_0030_VN)).toBe("2026-10-26");
    expect(defaultGrantValidFrom(SUN_2359_VN)).toBe("2026-10-25");
  });

  it("Thứ Hai 00:30 VN: valid_from mặc định là hôm nay, quyền từ hôm nay đang hiệu lực", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: MON_0030_VN });
    const g = await mapi.createGrant({ grantee_ref: A, role: "ksgm", commune_codes: ["91301"] });
    expect(g.valid_from).toBe("2026-10-26");
    expect(g.status).toBe("active");
    const g2 = await mapi.createGrant({ grantee_ref: "0190f1c4-7d3e-7000-8000-0000000000c3", role: "ksgm", commune_codes: ["91301"], valid_from: "2026-10-26" });
    expect(g2.status).toBe("active");
  });

  it("Chủ Nhật 23:59 VN: quyền từ Thứ Hai còn chờ hiệu lực", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: SUN_2359_VN });
    const g = await mapi.createGrant({ grantee_ref: A, role: "ksgm", commune_codes: ["91301"], valid_from: "2026-10-26" });
    expect(g.status).toBe("pending");
  });
});
