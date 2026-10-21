import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsDiary } from "@/api/mock/kn-cnts-diary";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { accounting, createCntsDiaryApi, expectedAlive, kindsOf, metricErrors, money, soldOn, tablesOf, waterWarnings } from "./api";

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
