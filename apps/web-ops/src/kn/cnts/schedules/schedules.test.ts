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
  nextShift,
  rosterCsv,
  rowsOf,
  shiftMonth,
  toGrid,
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

  it("tháng trước/sau, thứ trong tuần, CSV theo mẫu", () => {
    expect(shiftMonth("2027-01", -1)).toBe("2026-12");
    expect(shiftMonth("2026-12", 1)).toBe("2027-01");
    expect(weekdayLabel("2027-10", 13)).toBe("T4");
    const csv = rosterCsv(
      { days_in_month: 2, facilities: [{ ref: F, code: "CS", name: "Lò, mổ", commune_code: "1", kind: null }], officers: [{ ref: O1, display: "An", commune_codes: [], facility_refs: [] }] },
      toGrid([{ facility_ref: F, officer_ref: O1, day: 2, shift: "ND" }]),
      [[F, O1]],
    );
    expect(csv).toBe('Cơ sở,Viên chức,1,2\n"Lò, mổ",An,,NĐ');
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
});
