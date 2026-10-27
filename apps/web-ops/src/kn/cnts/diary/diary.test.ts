import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsDiary } from "@/api/mock/kn-cnts-diary";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import {
  accounting,
  ageWeek,
  cost6Label,
  createCntsDiaryApi,
  expectedAlive,
  growthTable,
  kgPerHead,
  kindsOf,
  lineAmount,
  metricErrors,
  money,
  soldOn,
  SUPPLY_ITEMS,
  TABLE_FIELDS,
  tablesOf,
  TECH_TARGETS,
  waterWarnings,
} from "./api";

describe("nhật ký nuôi BM-08/09/19 — quy tắc biểu (W11.48)", () => {
  it("chỉ số theo mẫu và loại lần ghi", () => {
    expect(kindsOf("BM-08")).toEqual(["daily"]);
    expect(kindsOf("BM-19")).toEqual(["growth_env", "env", "harvest"]);
    expect(metricErrors("BM-08", "daily", { alive: "", feed_kg: "1.234" })).toEqual({ alive: "Bắt buộc (NC-05)", feed_kg: expect.any(String) });
    expect(
      metricErrors("BM-19", "growth_env", {
        loss: "0",
        ph: "7.55",
        do_mgl: "5",
        feed_kg: "2",
      }),
    ).toMatchObject({
      sample_count: expect.any(String),
      weight_avg_g: expect.any(String),
      length_avg_cm: expect.any(String),
      ph: expect.any(String),
    });
    expect(
      metricErrors("BM-19", "env", {
        loss: "0",
        ph: "7.8",
        do_mgl: "5",
        feed_kg: "2",
      }),
    ).toEqual({});
    expect(tablesOf("BM-09")).not.toContain("sales");
    expect(tablesOf("BM-19")).toEqual([]);
  });

  it("NC-05, môi trường nước, hạch toán NC-15", () => {
    expect(expectedAlive(498, 1, 10)).toBe(487);
    expect(
      soldOn(
        {
          sales: [
            {
              date: "2027-10-12",
              heads: 10,
              weight_kg: "18",
              unit_price: "70000",
            },
            { date: "2027-10-11", heads: 3, weight_kg: "5", unit_price: "1" },
          ],
        },
        "2027-10-12",
      ),
    ).toBe(10);
    expect(waterWarnings({ ph: "8.7", do_mgl: "3.5" })).toHaveLength(2);
    expect(waterWarnings({ ph: "7.8", do_mgl: "5" })).toEqual([]);
    expect(
      accounting({
        cost_seed: "5000000",
        cost_feed: "30000000",
        cost_vet: "1500000",
        revenue_amount: "58800000",
      }),
    ).toEqual({ total: 36500000, profit: 22300000 });
    expect(accounting({ cost_seed: "1" }).profit).toBeNull();
    expect(money("-1500000")).toBe("−1.500.000 đ");
  });
});

describe("giả lập nhật ký nuôi", () => {
  beforeEach(() => (resetCntsModel(), resetCntsDiary()));
  const dapi = createCntsDiaryApi(
    createApiClient({
      baseUrl: "http://mock",
      getAccessToken: () => "mock.setter",
      getContextId: () => null,
      fetch: mockFetchNoDelay,
    }),
  );
  const HH = "0190f1c4-7d3e-7000-8000-0000000c7301";

  it("lứa mẫu, lần ghi bị chặn rồi gửi được, rà soát xác thực, đã xác thực không sửa", async () => {
    const l = await dapi.cycles({ household_ref: HH });
    expect(l.items).toHaveLength(1);
    const c = l.items[0]!;
    expect(c.suggest.current_heads).toBe(497);
    await expect(
      dapi.createCycle({
        household_ref: HH,
        breed: "Gà",
        received_date: "2027-10-01",
        initial_count: 10,
      }),
    ).rejects.toThrow();
    await expect(
      dapi.createEntry({
        cycle_ref: c.ref,
        entry_kind: "daily",
        metrics: { alive: "495", dead: "2", feed_kg: "12" },
        submit: true,
      }),
    ).rejects.toThrow();
    const e = await dapi.createEntry({
      cycle_ref: c.ref,
      entry_kind: "daily",
      metrics: { alive: "495", dead: "2", death_cause: "yeu", feed_kg: "12" },
      location: { lat: 10.3801, lng: 105.4301, accuracy_m: "5" },
      submit: true,
    });
    expect(e.status).toBe("submitted");
    expect(e.location.far).toBe(false);
    const queue = await dapi.entries({ review: true });
    const far = queue.find((x) => x.location.far)!;
    expect(far).toBeTruthy();
    await expect(dapi.giveBack(far.ref, far.version, "")).rejects.toThrow();
    const v = await dapi.verify(far.ref, far.version);
    expect(v.status).toBe("verified");
    await expect(
      dapi.updateEntry(far.ref, {
        base_version: v.version,
        entry_kind: "daily",
        metrics: far.metrics,
      }),
    ).rejects.toThrow();
    const co = await dapi.correct(far.ref, {
      base_version: v.version,
      entry_kind: "daily",
      metrics: { ...far.metrics, feed_kg: "13.5" },
      reason: "Theo sổ giấy",
    });
    expect(co.status).toBe("submitted");
    expect(co.versions).toHaveLength(3);
  });

  it("kết thúc lứa với hạch toán, lợi nhuận trung bình theo điểm", async () => {
    const c = (await dapi.cycles({ household_ref: HH })).items[0]!;
    await expect(dapi.closeCycle(c.ref, { base_version: c.version, revenue_amount: "" })).rejects.toThrow();
    const done = await dapi.closeCycle(c.ref, {
      base_version: c.version,
      cost_seed: "5000000",
      cost_feed: "30000000",
      revenue_output_kg: "840",
      revenue_amount: "58800000",
    });
    expect(done.status).toBe("closed");
    expect(done.accounting.profit).toBe("23800000");
    expect((await dapi.cycles({ site_ref: c.site_ref })).avg_profit).toBe("23800000");
  });
});

// KN-VN-DATE (#149): ngày ghi mặc định và hạn sửa 7 ngày theo ngày lịch VN (00:00–07:00 VN ngày UTC còn là hôm qua).
describe("nhật ký nuôi theo ngày VN", () => {
  const dapi = createCntsDiaryApi(
    createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }),
  );
  const HH = "0190f1c4-7d3e-7000-8000-0000000c7301";
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"], now: Date.parse("2026-10-25T17:30:00Z") }); // Thứ Hai 26/10 00:30 VN
    resetCntsModel();
    resetCntsDiary();
  });
  afterEach(() => vi.useRealTimers());

  it("Thứ Hai 00:30 VN: lần ghi không kèm ngày mang ngày hôm nay theo giờ VN", async () => {
    const c = (await dapi.cycles({ household_ref: HH })).items[0]!;
    const e = await dapi.createEntry({ cycle_ref: c.ref, entry_kind: "daily", metrics: { alive: "495", dead: "0", feed_kg: "12" } });
    expect(e.entry_date).toBe("2026-10-26");
    expect(e.editable_until).toBe("2026-11-02");
  });
});

describe("BF-CN-DIARY #109 — đủ trường sổ gốc BM-08/BM-09 2026", () => {
  it("mục 4 mua nguyên liệu thức ăn có ở sổ gà và sổ vịt, không có ở BM-19; khoản chi 6 theo sổ", () => {
    expect(tablesOf("BM-08")).toContain("feed_materials");
    expect(tablesOf("BM-09")).toEqual(["feed_purchases", "feed_materials", "vaccinations", "officer_notes"]);
    expect(tablesOf("BM-19")).toEqual([]);
    expect(cost6Label("BM-08")).toBe("Chế phẩm");
    expect(cost6Label("BM-09")).toBe("Vaccine");
    expect(SUPPLY_ITEMS["BM-09"]).toEqual(["Con giống", "Thức ăn vịt con", "Thức ăn vịt lớn", "Vaccine"]);
    expect(TABLE_FIELDS.officer_notes.map((f) => f.key)).toEqual(["date", "officer", "content", "farmer_opinion"]);
    expect(TABLE_FIELDS.vaccinations.map((f) => f.key)).toEqual(["date", "vaccine", "dosage", "method", "mfg_date", "shot", "note"]);
    expect(TABLE_FIELDS.treatments.map((f) => f.key)).toEqual(["date", "condition", "symptoms", "drug", "dosage", "treater", "withdrawal_days", "result"]);
  });

  it("thành tiền và P (kg/con) tự tính", () => {
    expect(lineAmount("10.5", "8000")).toBe(84000);
    expect(lineAmount("", "8000")).toBeNull();
    expect(kgPerHead("1650", 1000)).toBe(1.65);
    expect(kgPerHead(null, 10)).toBeNull();
  });

  it("tuần tuổi: theo 'Tuần tuổi (khi cân)' nếu có, không thì từ ngày tuổi lúc nhận", () => {
    const c = { received_date: "2027-01-01", age_days: 1 };
    expect(ageWeek(c, { entry_date: "2027-01-01", metrics: {} })).toBe(1);
    expect(ageWeek(c, { entry_date: "2027-01-07", metrics: {} })).toBe(1);
    expect(ageWeek(c, { entry_date: "2027-01-08", metrics: {} })).toBe(2);
    expect(ageWeek(c, { entry_date: "2027-01-08", metrics: { age_week: "5" } })).toBe(5);
  });

  it("chỉ tiêu sinh trưởng theo tuần (tuần 1, 2, 3, 4, 6, 8, 10, 12) và yêu cầu kỹ thuật", () => {
    const c = { received_date: "2027-01-01", age_days: 1, initial_count: 100, initial_weight_g: "40" };
    const e = (d: string, alive: string, dead: string, feed: string, extra: Record<string, string> = {}, status: "submitted" | "draft" = "submitted") => ({
      entry_date: d,
      status,
      metrics: { alive, dead, feed_kg: feed, ...extra },
    });
    const g = growthTable(c, [
      e("2027-01-02", "99", "1", "2", { death_cause: "yeu" }),
      e("2027-01-07", "98", "1", "3", { death_cause: "benh", weight_avg_g: "120" }),
      e("2027-01-29", "96", "2", "20", { death_cause: "yeu", weight_avg_g: "700" }), // ngày tuổi 29 → tuần 5 → dòng tuần 6
      e("2027-01-30", "10", "0", "999", {}, "draft"), // nháp không tính
    ]);
    expect(g.rows.map((r) => r.week)).toEqual([1, 2, 3, 4, 6, 8, 10, 12]);
    expect(g.rows[0]).toMatchObject({ alive: 98, survival: 98, dead: 2, causes: "Yếu, Bệnh", feedKg: 5, weightKg: 0.12 });
    expect(g.rows[1]).toMatchObject({ alive: null, survival: null, dead: 0, feedKg: 0 });
    expect(g.rows[4]).toMatchObject({ week: 6, alive: 96, survival: 96, dead: 2, weightKg: 0.7 });
    // Tiêu tốn lũy kế = 25 kg / (96 × 0,7 − 100 × 0,04) = 25 / 63,2.
    expect(g.rows[4]!.fcr).toBe(0.4);
    expect(g.total).toMatchObject({ dead: 4, feedKg: 25, alive: 96, survival: 96, fcr: 0.4 });
    expect(g.avg).toEqual({ weightKg: 0.41, feedPerHead: 0.25 });
    const t = TECH_TARGETS["BM-08"]!;
    expect(t.map((x) => x.target)).toEqual(["95", "90", "2,8", "1,6–1,8"]);
    expect(t[0]!.actual(g)).toBeNull(); // tuần 4 chưa có lần ghi
    expect(t[2]!.ok(t[2]!.actual(g)!)).toBe(true);
    expect(TECH_TARGETS["BM-09"]!.map((x) => x.label)).toEqual([
      "Tỉ lệ nuôi sống đến 70 ngày",
      "Trọng lượng TB lúc 70 ngày",
      "Tiêu tốn thức ăn (thức ăn hỗn hợp hoàn chỉnh)",
    ]);
    // Quá tuần 12 thêm dòng mỗi 2 tuần.
    expect(growthTable(c, [e("2027-03-30", "90", "0", "1")]).rows.map((r) => r.week).slice(-2)).toEqual([12, 14]);
  });
});
