import { describe, expect, it } from "vitest";
import type { GisLayerEvent, MetricDefinition, MetricValue } from "@/api/types";
import { activeCount, diffAttributes, foldLayerEvents, replayFeatureCollection, snapshotBadge, validateReplayDate } from "@/gis/replay";
import { addDays, cutoffOf, isIsoDate, vnToday } from "@/lib/vnDate";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { chartGeometry, groupDefinitions, rangeFor, shapeSeries } from "./series";

function mv(p: Partial<MetricValue> & { business_date: string; value: string }): MetricValue {
  return {
    id: p.id ?? `id-${p.business_date}-${p.value}`,
    business_date: p.business_date,
    value: p.value,
    definition_version: p.definition_version ?? 1,
    cutoff: cutoffOf(p.business_date),
    watermark: null,
    computed_at: p.computed_at ?? `${p.business_date}T18:00:00Z`,
    supersedes_id: p.supersedes_id ?? null,
    correction_reason: p.correction_reason ?? null,
    current: p.current ?? true,
  };
}

describe("ngày nghiệp vụ giờ VN (R-12)", () => {
  it("vnToday theo UTC+7; cutoff(D) = 00:00 VN ngày D+1", () => {
    expect(vnToday(new Date("2026-09-29T17:30:00Z"))).toBe("2026-09-30");
    expect(vnToday(new Date("2026-09-29T16:59:59Z"))).toBe("2026-09-29");
    expect(cutoffOf("2026-09-29")).toBe("2026-09-29T17:00:00.000Z");
    expect(addDays("2026-02-28", 1)).toBe("2026-03-01");
    expect(isIsoDate("2026-02-30")).toBe(false);
  });
});

describe("dựng chuỗi lịch sử chỉ số (KN US-DASH-01/02)", () => {
  it("KN US-DASH-02: kỳ đã chốt không sửa — lấy dòng hiện hành, gom dòng bị thay kèm lý do của dòng bù", () => {
    const old = mv({ id: "old", business_date: "2026-09-10", value: "100.00", current: false });
    const fixed = mv({ id: "new", business_date: "2026-09-10", value: "110.50", supersedes_id: "old", correction_reason: "Bổ sung báo cáo muộn", computed_at: "2026-09-12T18:00:00Z" });
    const s = shapeSeries([mv({ business_date: "2026-09-11", value: "120" }), fixed, old, mv({ business_date: "2026-09-09", value: "90", definition_version: 1 })]);
    expect(s.points.map((p) => p.date)).toEqual(["2026-09-09", "2026-09-10", "2026-09-11"]);
    expect(s.points[1]).toMatchObject({ raw: "110.50", corrected: true });
    expect(s.points[0]!.corrected).toBe(false);
    expect(s.superseded).toEqual([{ date: "2026-09-10", id: "old", oldValue: "100.00", currentValue: "110.50", reason: "Bổ sung báo cáo muộn", computedAt: old.computed_at, version: 1 }]);
  });

  it("đánh dấu ngày đổi phiên bản định nghĩa", () => {
    const s = shapeSeries([mv({ business_date: "2026-09-01", value: "1" }), mv({ business_date: "2026-09-02", value: "2", definition_version: 2 })]);
    expect(s.versionChanges).toEqual([{ date: "2026-09-02", from: 1, to: 2 }]);
  });

  it("khoảng mặc định: tới hôm qua (ngày đã chốt), đủ số ngày", () => {
    expect(rangeFor(30, new Date("2026-09-30T03:00:00Z"))).toEqual({ from: "2026-08-31", to: "2026-09-29" });
  });

  it("đồ thị: trục y từ 0, không nối qua ngày thiếu giá trị", () => {
    const s = shapeSeries([mv({ business_date: "2026-09-01", value: "10" }), mv({ business_date: "2026-09-02", value: "20" }), mv({ business_date: "2026-09-05", value: "15" })]);
    const g = chartGeometry(s.points, "2026-09-01", "2026-09-05", { width: 400, height: 200, left: 40, right: 10, top: 10, bottom: 20 });
    expect(g.min).toBe(0);
    expect(g.max).toBeGreaterThanOrEqual(20);
    expect(g.path.match(/M/g)).toHaveLength(2);
    expect(g.dots[0]!.x).toBe(40);
    expect(g.dots[2]!.x).toBe(390);
  });

  it("gom định nghĩa theo khóa, phiên bản hiệu lực theo ngày", () => {
    const d = (version: number, effective_from: string): MetricDefinition => ({
      key: "kn.sown_area_ha", version, title: "t", description: "", unit: "ha", grain: "day", aggregation: "sum", source_module: "pf/extension", source: "", rule_version: `kn.dash.v${version}`, effective_from, proposed_by: "", approved_by: "", file: "",
    });
    const [g] = groupDefinitions([d(1, "2026-01-01"), d(2, "2026-10-01")], "2026-09-30");
    expect(g!.effective.version).toBe(1);
    expect(g!.versions.map((v) => v.version)).toEqual([2, 1]);
    expect(groupDefinitions([d(1, "2026-01-01"), d(2, "2026-10-01")], "2026-10-01")[0]!.effective.version).toBe(2);
  });
});

function ev(id: string, op: GisLayerEvent["op"], recorded_at: string, after: Record<string, unknown> | null, feature_ref = "f1", layer = "plot"): GisLayerEvent {
  return { id, layer, feature_ref, op, before: null, after, source_event_id: id, source_type: "x", source_module: "pf/geo", event_time: recorded_at, recorded_at };
}

describe("phát lại lớp GIS as-of (GIS US-REPLAY-01, US-SNAP-01)", () => {
  const log = [
    ev("a", "create", "2026-09-01T02:00:00Z", { name: "Thửa A", geometry_ref: "g1" }),
    ev("b", "update", "2026-09-10T02:00:00Z", { name: "Thửa A2", geometry_ref: "g2" }),
    // 23:30 giờ VN ngày 15 → vẫn thuộc ngày 15.
    ev("c", "retire", "2026-09-15T16:30:00Z", { name: "Thửa A2" }),
    ev("d", "create", "2026-09-15T17:00:00Z", { name: "Thửa B" }, "f2"),
  ];

  it("chỉ áp dòng recorded_at < Cutoff(D) theo (recorded_at, id)", () => {
    const d5 = foldLayerEvents(log, "2026-09-05").layers.get("plot")!;
    expect(d5.get("f1")).toMatchObject({ attributes: { name: "Thửa A" }, retired: false });
    const d15 = foldLayerEvents(log, "2026-09-15");
    expect(d15.layers.get("plot")!.get("f1")!.retired).toBe(true);
    expect(d15.layers.get("plot")!.has("f2")).toBe(false);
    expect(d15.watermark).toEqual({ last_event_id: "c", recorded_at: "2026-09-15T16:30:00Z" });
    expect(foldLayerEvents(log, "2026-09-16").layers.get("plot")!.has("f2")).toBe(true);
  });

  it("ảnh chụp + dòng sau watermark = dựng thẳng từ nhật ký (thứ tự nhập không quan trọng)", () => {
    const base = foldLayerEvents(log, "2026-09-10");
    const viaSnapshot = foldLayerEvents([...log].reverse(), "2026-09-16", base);
    const direct = foldLayerEvents(log, "2026-09-16");
    expect(viaSnapshot.layers).toEqual(direct.layers);
    expect(viaSnapshot.watermark).toEqual(direct.watermark);
    expect(viaSnapshot.applied).toBe(2);
  });

  it("khôi phục bỏ cờ ngừng dùng", () => {
    const l = [...log, ev("e", "restore", "2026-09-20T02:00:00Z", { name: "Thửa A2" })];
    expect(foldLayerEvents(l, "2026-09-20").layers.get("plot")!.get("f1")!.retired).toBe(false);
  });

  it("ngày tương lai/không hợp lệ bị chặn trước khi gọi", () => {
    expect(validateReplayDate("2026-10-01", "2026-09-30")).toMatch(/tương lai/);
    expect(validateReplayDate("", "2026-09-30")).not.toBeNull();
    expect(validateReplayDate("2026-09-30", "2026-09-30")).toBeNull();
  });

  it("FeatureCollection bỏ đối tượng đã ngừng và không có hình học", () => {
    const fc = replayFeatureCollection([
      {
        layer: "plot", business_date: "2026-09-15", watermark: null, basis: "replay", base_snapshot_date: null, feature_count: 3,
        features: [
          { feature_ref: "f1", attributes: { name: "A" }, retired: false, geometry: { type: "Point", coordinates: [105, 10] } },
          { feature_ref: "f2", attributes: { name: "B" }, retired: true, geometry: { type: "Point", coordinates: [105, 10] } },
          { feature_ref: "f3", attributes: null, retired: false },
        ],
      },
    ]);
    expect(fc.features).toHaveLength(1);
    expect(fc.features[0]!.properties).toMatchObject({ layer: "plot", name: "A" });
    expect(activeCount({ features: [{ feature_ref: "x", attributes: null, retired: true }] })).toBe(0);
  });

  it("GIS US-EVL-01: bảng trước/sau đánh dấu thuộc tính đổi", () => {
    const d = diffAttributes({ name: "A", area_ha: "1.0" }, { name: "A", area_ha: "1.5", status: "active" });
    expect(d.filter((x) => x.changed).map((x) => x.key)).toEqual(["area_ha", "status"]);
    expect(d.find((x) => x.key === "status")!.before).toBe("—");
  });

  it("huy hiệu ảnh chụp: có ảnh chụp ngày đó / dựng từ ảnh chụp trước + nhật ký / hôm nay", () => {
    const st = { layer: "plot", business_date: "2026-09-20", watermark: { last_event_id: "a", recorded_at: "2026-09-20T10:00:00Z" }, basis: "snapshot" as const, base_snapshot_date: "2026-09-20", feature_count: 0, features: [] };
    expect(snapshotBadge("2026-09-20", "2026-09-30", [st], null)).toEqual({ kind: "snapshot", date: "2026-09-20", watermarkAt: "2026-09-20T10:00:00Z" });
    expect(snapshotBadge("2026-09-21", "2026-09-30", [], [{ ...st, basis: "replay", base_snapshot_date: "2026-09-19" }])).toEqual({ kind: "replay", baseDate: "2026-09-19" });
    expect(snapshotBadge("2026-09-30", "2026-09-30", null, null)).toEqual({ kind: "live" });
  });
});

async function get(persona: string, path: string) {
  const res = await mockFetchNoDelay(new Request(`http://mock${path}`, { headers: { Authorization: `Bearer ${mockToken(persona)}` } }));
  return { status: res.status, body: (await res.json()) as Record<string, unknown> };
}

describe("backend giả lập pf/metrics khớp hợp đồng", () => {
  it("phạm vi: toàn tenant chỉ cho TW; tỉnh xem được tỉnh mình", async () => {
    expect((await get("tinh", "/v1/metrics/kn.sown_area_ha/values")).status).toBe(403);
    const r = await get("tinh", "/v1/metrics/kn.sown_area_ha/values?unit=82&history=true");
    expect(r.status).toBe(200);
    const items = r.body.items as MetricValue[];
    expect(items.some((i) => !i.current)).toBe(true);
    expect(items.filter((i) => i.current).every((i, k, a) => k === 0 || a[k - 1]!.business_date < i.business_date)).toBe(true);
  });

  it("chỉ số theo vụ thiếu dimension → items rỗng + danh sách vụ", async () => {
    const r = await get("cuc", "/v1/metrics/cgh.coops_shortage/values");
    expect(r.body.items).toEqual([]);
    expect((r.body.dimensions as string[]).length).toBeGreaterThan(0);
  });

  it("phát lại: ngày tương lai 422 metrics.future_date; ảnh chụp hôm qua có mốc", async () => {
    const future = await get("setter", `/v1/gis/replay?date=${addDays(vnToday(), 1)}`);
    expect(future.status).toBe(422);
    expect(future.body.code).toBe("metrics.future_date");
    const y = addDays(vnToday(), -1);
    const snap = await get("setter", `/v1/gis/snapshots/${y}`);
    expect((snap.body.items as unknown[]).length).toBe(6);
    const rep = await get("setter", `/v1/gis/replay?date=${y}&geometry=true`);
    expect((rep.body.items as { basis: string }[])[0]!.basis).toBe("snapshot");
    expect((await get("xa", `/v1/gis/replay?date=${y}`)).status).toBe(403);
  });
});
