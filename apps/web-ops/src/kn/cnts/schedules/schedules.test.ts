import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsCal } from "@/api/mock/kn-cnts-cal";
import { placeError } from "@/kn/schedule/model";
import {
  batchErrors,
  cellKey,
  createCntsCalApi,
  expectedDate,
  fromGrid,
  grantHref,
  householdText,
  importFileError,
  issueText,
  nextShift,
  rowChanges,
  vaccineGroups,
  rowsOf,
  shiftMonth,
  toGrid,
  uncoveredDays,
  weekdayLabel,
  type DutyCell,
} from "./api";

const F = "0190f1c4-7d3e-7000-8000-00000000c501";
const O1 = "0190f1c4-7d3e-7000-8000-0000000000a1";
const O2 = "0190f1c4-7d3e-7000-8000-0000000000a2";

describe("lịch trực KSGM BM-03 — quy tắc lưới (W11.46)", () => {
  it("bấm ô: trống → N → Đ → NĐ → trống", () => {
    expect([nextShift(""), nextShift("N"), nextShift("D"), nextShift("ND")]).toEqual(["N", "D", "ND", ""]);
  });

  it("lưới ↔ danh sách ô; bỏ ngày ngoài tháng; hàng theo cặp cơ sở, viên chức", () => {
    const cells: DutyCell[] = [
      { facility_ref: F, officer_ref: O2, day: 3, shift: "D" },
      { facility_ref: F, officer_ref: O1, day: 31, shift: "N" },
    ];
    const g = toGrid(cells);
    expect(g.get(cellKey(F, O2, 3))).toBe("D");
    expect(fromGrid(g, 30)).toEqual([{ facility_ref: F, officer_ref: O2, day: 3, shift: "D" }]);
    expect(rowsOf(cells, [[F, O1]])).toEqual([
      [F, O2],
      [F, O1],
    ]);
  });

  it("tháng trước/sau, thứ trong tuần", () => {
    expect(shiftMonth("2027-01", -1)).toBe("2026-12");
    expect(shiftMonth("2026-12", 1)).toBe("2027-01");
    expect(weekdayLabel("2027-10", 13)).toBe("T4");
  });

  it("BF-CN-DUTY: tô ngày cơ sở chưa có người trực theo lưới đang sửa; liên kết cấp quyền; tệp nạp", () => {
    const g = toGrid([
      { facility_ref: F, officer_ref: O1, day: 1, shift: "N" },
      { facility_ref: F, officer_ref: O2, day: 3, shift: "D" },
    ]);
    expect([...(uncoveredDays(g, [F], 4).get(F) ?? [])]).toEqual([2, 4]);
    expect(uncoveredDays(toGrid([1, 2].map((day) => ({ facility_ref: F, officer_ref: O1, day, shift: "N" as const }))), [F], 2).has(F)).toBe(false);
    expect(grantHref(O1, F, "30292")).toBe(`/kn/cnts/cap-quyen?vien_chuc=${O1}&co_so=${F}&xa=30292`);
    expect(importFileError({ kind: "xls", size: 10 })).toMatch(/\.xlsx/);
    expect(importFileError({ kind: "csv", size: 10 })).toMatch(/Excel/);
    expect(importFileError({ kind: "xlsx", size: 5 * 1024 * 1024 })).toMatch(/4 MB/);
    expect(importFileError({ kind: "xlsx", size: 1000 })).toBeNull();
    expect(issueText({ line: 12, field: "officer", message: "Không khớp" })).toBe("Dòng 12: Không khớp");
    expect(issueText({ line: null, field: "row", message: "Quá nhiều ô" })).toBe("Quá nhiều ô");
  });
});

describe("lịch tiêm BM-07 và địa điểm công tác (W11.46)", () => {
  it("FN-15 AC-03: nhận 19/09, Gumboro L1 7 ngày tuổi, nhận lúc 1 ngày tuổi → 25/09", () => {
    expect(expectedDate("2026-09-19", 7, 1)).toBe("2026-09-25");
  });

  it("kiểm biểu lập lịch: hộ hoặc địa bàn, tuổi lúc nhận", () => {
    expect(Object.keys(batchErrors({ commune_code: "1", model_type_code: "M", received_date: "2026-10-01", age_at_receipt: 1 }))).toEqual(["locality"]);
    expect(batchErrors({ commune_code: "1", model_type_code: "M", locality: "Ấp 1", received_date: "2026-10-01", age_at_receipt: -1 }).age_at_receipt).toBeTruthy();
  });

  it("tọa độ địa điểm: trống được, phải trong Việt Nam", () => {
    expect(placeError("", "")).toBeNull();
    expect(placeError("10.2", "")).toMatch(/đủ/);
    expect(placeError("1", "105")).toMatch(/Việt Nam/);
    expect(placeError("10.25", "105.97")).toBeNull();
  });
});

describe("giả lập lịch trực, lịch tiêm", () => {
  beforeEach(() => resetCntsCal());
  const capi = createCntsCalApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }));

  it("CNTS-ROSTER-SCOPE-001: mỗi tỉnh một lịch, vai tỉnh không chọn tỉnh khác, legacy chỉ Admin đọc", async () => {
    const peer = createCntsCalApi(createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.tinh",fetch:mockFetchNoDelay}));
    await capi.saveRoster("2027-10", {province_code:"91",base_version:0,cells:[],note:"Riêng tỉnh A"});
    const mine = await peer.roster("2027-10");
    expect(mine.province_code).toBe("82");
    expect(mine.version).toBe(0);
    expect(mine.note).toBeNull();
    await expect(peer.roster("2027-10","91")).rejects.toThrow();
    await peer.saveRoster("2027-10",{province_code:"82",base_version:0,cells:[],note:"Riêng tỉnh B"});
    expect((await capi.roster("2027-10","91")).note).toBe("Riêng tỉnh A");
    expect((await capi.roster("2027-10","91")).version).toBe(1);
    expect((await capi.roster("2027-10","legacy")).can_edit).toBe(false);
    await expect(capi.saveRoster("2027-10",{province_code:"legacy",base_version:0,cells:[]})).rejects.toThrow();
  });

  it("lưu lịch trực có khóa lạc quan; tạo lịch tiêm theo quy trình rồi ghi ngày tiêm", async () => {
    const r = await capi.roster("2027-10", "91");
    expect(r.version).toBe(0);
    const f = r.facilities[0]!.ref;
    const officer = r.officers[0]!.ref;
    const saved = await capi.saveRoster("2027-10", { province_code: "91", base_version: 0, cells: [{ facility_ref: f, officer_ref: officer, day: 1, shift: "N" }] });
    expect(saved.version).toBe(1);
    expect(saved.notified_officer_refs).toEqual([officer]);
    await expect(capi.saveRoster("2027-10", { province_code: "91", base_version: 0, cells: [] })).rejects.toThrow();

    await expect(capi.createBatch({ commune_code: "30292", model_type_code: "GA_THUONG_PHAM", locality: "Ấp 1", received_date: "2027-10-01", age_at_receipt: -1 })).rejects.toThrow(); // BF-SWEEP-CNTS
    await expect(capi.createBatch({ commune_code: "30292", model_type_code: "GA_THUONG_PHAM", locality: "Ấp 1", received_date: "2027-10-01", age_at_receipt: 1, head_count: 0 })).rejects.toThrow();
    const b = await capi.createBatch({ commune_code: "30292", model_type_code: "GA_THUONG_PHAM", locality: "Ấp 1", received_date: "2027-10-01", age_at_receipt: 1 });
    expect(b.doses.map((d) => d.expected_date)).toEqual(["2027-10-03", "2027-10-17", "2027-11-09"]);
    const u = await capi.updateDoses(b.ref, { base_version: 1, doses: [{ vaccine_code: "VX_ND_F", shot: 1, actual_date: "2027-10-03" }] });
    expect(u.event).toBe("dose_recorded");
    expect(u.versions).toHaveLength(2);
  });

  it("BF-CN-VACC: lập lịch theo hộ mô hình (xã, loại mô hình theo điểm) và bảng BM-07", async () => {
    const hh = "0190f1c4-7d3e-7000-8000-0000000c7301"; // hộ mẫu của giả lập W11.47
    await expect(capi.createBatch({ household_ref: "0190f1c4-7d3e-7000-8000-00000000ffff", received_date: "2027-10-01", age_at_receipt: 1 })).rejects.toThrow();
    await expect(capi.createBatch({ household_ref: hh, commune_code: "99999", received_date: "2027-10-01", age_at_receipt: 1 })).rejects.toThrow();
    const b = await capi.createBatch({ household_ref: hh, received_date: "2027-10-01", age_at_receipt: 1 });
    expect(b.model?.site_name).toBe("Điểm ấp Bình Hòa");
    expect(householdText(b)).toBe("Nguyễn V. B.");
    const m = await capi.matrix({ model_type_code: b.model_type_code, site_ref: b.model!.site_ref });
    expect(m.rows).toHaveLength(1);
    expect(m.rows[0]!.cells).toHaveLength(m.columns.length);
    expect(m.rows[0]!.household_text).toBe("Nguyễn V. B.");
    expect(vaccineGroups(m.columns).reduce((n, g) => n + g.span, 0)).toBe(m.columns.length);
    expect((await capi.matrix({ model_type_code: b.model_type_code, site_ref: "0190f1c4-7d3e-7000-8000-00000000eeee" })).rows).toHaveLength(0);
  });

  it("BF-CN-DUTY: xuất .xlsx BM-03 rồi nạp lại ra cùng lịch; SĐT che với người lập lịch; tệp sai bị từ chối", async () => {
    const r = await capi.roster("2027-10", "91");
    const f = r.facilities[0]!.ref;
    const o = r.officers[0]!.ref;
    const cells: DutyCell[] = [
      { facility_ref: f, officer_ref: o, day: 1, shift: "N" },
      { facility_ref: f, officer_ref: o, day: 31, shift: "ND" },
    ];
    const saved = await capi.saveRoster("2027-10", { province_code: "91", base_version: 0, cells });
    expect(saved.can_view_phone).toBe(true);
    expect(saved.officers[0]!.phone_masked).toMatch(/^\d{4}•••\d{3}$/);
    const x = await capi.exportRoster("2027-10", "91", "theo thông báo số 1/TB");
    expect(x.name).toBe("lich-truc-ksgm-91-2027-10.xlsx");
    const bytes = new Uint8Array(await x.blob.arrayBuffer());
    const imp = await capi.importRoster("2027-10", { body: bytes, contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" }, "91");
    expect(imp.issues).toEqual([]);
    expect(imp.cells).toEqual(cells);
    expect(imp.basis).toBe("(theo thông báo số 1/TB)");
    await expect(capi.importRoster("2027-11", { body: bytes, contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" }, "91")).rejects.toThrow();
    await expect(capi.importRoster("2027-10", { body: "STT,TÊN", contentType: "text/csv" }, "91")).rejects.toThrow();
    await expect(capi.exportRoster("2027-10", "91", "gọi 0912345678")).rejects.toThrow();
  });
});

describe("bảng BM-07 (BF-CN-VACC)", () => {
  it("kiểm biểu: chọn hộ mô hình thì không bắt xã, loại mô hình", () => {
    expect(batchErrors({ household_ref: "h", received_date: "2026-10-01", age_at_receipt: 1 })).toEqual({});
    expect(Object.keys(batchErrors({ received_date: "2026-10-01", age_at_receipt: 1 })).sort()).toEqual(["commune_code", "locality", "model_type_code"]);
  });

  it("gom cột theo vaccine; thay đổi của hàng (Cấp Xã không gửi ngày dự kiến)", () => {
    const cols = [
      { vaccine_code: "A", vaccine_name: "Vaccine A", shot: 1, age_days: 3 },
      { vaccine_code: "B", vaccine_name: null, shot: 1, age_days: 7 },
      { vaccine_code: "B", vaccine_name: null, shot: 2, age_days: 28 },
    ];
    expect(vaccineGroups(cols)).toEqual([{ code: "A", name: "Vaccine A", span: 1 }, { code: "B", name: "B", span: 2 }]);
    const dose = { vaccine_code: "B", vaccine_name: null, shot: 1, age_days: 7, expected_date: "2027-10-07", actual_date: null, note: null, overdue: true };
    const edit = { "B/1": { expected: "2027-10-09", actual: "2027-10-08" } };
    expect(rowChanges({ cells: [null, dose], can_plan: false }, edit)).toEqual([{ vaccine_code: "B", shot: 1, actual_date: "2027-10-08" }]);
    expect(rowChanges({ cells: [null, dose], can_plan: true }, edit)).toEqual([{ vaccine_code: "B", shot: 1, expected_date: "2027-10-09", actual_date: "2027-10-08" }]);
    expect(rowChanges({ cells: [dose], can_plan: true }, {})).toEqual([]);
  });
});
