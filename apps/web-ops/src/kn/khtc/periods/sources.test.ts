import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { baseRefText, createKhtcSourceApi, valueBadges, valueText, vnNum, weekText, type KhtcSourceValue } from "./sources";

const client = () =>
  createKhtcSourceApi(
    createApiClient({
      baseUrl: "http://mock",
      getAccessToken: () => "mock.tinh",
      getContextId: () => null,
      fetch: mockFetchNoDelay,
    }),
  );

describe("W11.64 — hiển thị số nguồn module", () => {
  it("số kiểu Việt Nam, gốc trong kỳ, tuần", () => {
    expect(vnNum("4947.5")).toBe("4.947,5");
    expect(vnNum("-50")).toBe("−50");
    expect(vnNum(null)).toBe("—");
    expect(weekText("2026-W48")).toBe("tuần 48/2026");
    expect(baseRefText("frozen:2026-W48")).toBe("lũy kế đã đóng tuần 48/2026");
    expect(baseRefText("current:2026-W49")).toBe("lũy kế hiện tại tuần 49/2026");
    expect(baseRefText("reset")).toBe("chuỗi lũy kế mới (gốc 0)");
  });
  it("giá min/max toàn tỉnh (D22), Tạm tính, Chưa xác nhận (D2)", () => {
    const v = {
      mode: "price",
      invalid: false,
      price_min: "8000",
      price_max: "14000",
      provisional: true,
      unconfirmed: "2",
    } as KhtcSourceValue;
    expect(valueText(v)).toBe("8.000 – 14.000");
    expect(valueBadges(v)).toEqual(["Tạm tính", "Chưa xác nhận 2"]);
    expect(valueText({ ...v, mode: "season", cumulative: "3230", increment: "180" })).toBe("Lũy kế 3.230 · trong kỳ 180");
    expect(valueBadges({ ...v, invalid: true })).toEqual(["Sai khóa nguồn"]);
  });
});

describe("W11.64 — Cập nhật số liệu (giả lập golden G03)", () => {
  it("chụp, nguồn chốt lại → Nguồn đã thay đổi (không ghi đè), cập nhật lại → lịch sử cũ → mới", async () => {
    const api = client();
    const id = "01920000-0000-7000-8000-00000000f064";
    const s0 = await api.status(id);
    expect(s0.snapshot).toBeNull();
    expect(s0.live).toBe(true);
    const s1 = await api.refresh(id);
    expect(s1.snapshot?.no).toBe(1);
    expect(s1.source_changed).toBe(true);
    expect(s1.values[0]?.cumulative).toBe("3230");
    expect(s1.changes[0]).toMatchObject({ kind: "figures", native_key: "2026-W49" });
    const s2 = await api.refresh(id);
    expect(s2.source_changed).toBe(false);
    expect(s2.values[0]).toMatchObject({ cumulative: "3190", increment: "140" });
    const h = await api.history(id);
    expect(h.map((x) => x.snapshot.no)).toEqual([2, 1]);
    expect(h[0]?.changes.map((c) => [c.field, c.old, c.new])).toEqual([
      ["cumulative", "3230", "3190"],
      ["increment", "180", "140"],
    ]);
  });
});
