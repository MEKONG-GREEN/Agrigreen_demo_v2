import { describe, expect, it } from "vitest";
import type { CapacityWidget, CropStageMap, GeoCoopLayer, WaterwayLayerSegment } from "@/api/types";
import { summarize, formatVnd, savingVsBaseline } from "@/sim/result";
import { pollUntil } from "@/lib/poll";
import {
  bboxParam,
  closeRing,
  draftGeometry,
  formatAreaM2,
  formatLength,
  haversineM,
  lineLengthM,
  parseBBox,
  parseLngLat,
  polygonAreaM2,
  touchesBBox,
  validateDraft,
  type LngLat,
} from "./geometry";
import { measureDraft } from "./DrawPanel";
import { capacityMarkers, coopLayers, waterwayLayer } from "./layers";
import { fillLabel, limitingLabel, stageColor, STAGES } from "./legend";

describe("hình học trắc địa (GIS BR-41: chiều dài/diện tích tự tính theo hình vẽ)", () => {
  it("Haversine: 1° kinh độ ở xích đạo ≈ 111,2 km; 0 khi trùng điểm", () => {
    expect(haversineM([0, 0], [1, 0]) / 1000).toBeCloseTo(111.19, 1);
    expect(haversineM([105.8, 10], [105.8, 10])).toBe(0);
  });

  it("chiều dài tuyến gấp khúc = tổng đoạn (GIS US-ROAD-02, US-WATER-02)", () => {
    const line: LngLat[] = [[105.0, 10.0], [105.01, 10.0], [105.01, 10.01]];
    expect(lineLengthM(line)).toBeCloseTo(haversineM(line[0]!, line[1]!) + haversineM(line[1]!, line[2]!), 6);
  });

  it("diện tích vùng 0,01° × 0,01° quanh vĩ độ 10 ≈ 121 ha; tự khép vòng; lỗ bị trừ", () => {
    const sq: LngLat[] = [[105, 10], [105.01, 10], [105.01, 10.01], [105, 10.01]];
    const ha = polygonAreaM2([sq]) / 10_000;
    expect(ha).toBeGreaterThan(119);
    expect(ha).toBeLessThan(123);
    expect(polygonAreaM2([closeRing(sq)])).toBeCloseTo(polygonAreaM2([sq]), 3);
    const hole: LngLat[] = [[105.002, 10.002], [105.004, 10.002], [105.004, 10.004], [105.002, 10.004]];
    expect(polygonAreaM2([sq, hole])).toBeLessThan(polygonAreaM2([sq]));
  });

  it("kiểm nét vẽ: điểm đúng 1, tuyến ≥ 2, vùng ≥ 3 điểm không thẳng hàng", () => {
    expect(validateDraft("point", [[105, 10]])).toBeNull();
    expect(validateDraft("line", [[105, 10]])).toMatch(/2 điểm/);
    expect(validateDraft("line", [[105, 10], [105.1, 10]])).toBeNull();
    expect(validateDraft("polygon", [[105, 10], [105.1, 10]])).toMatch(/3 điểm/);
    expect(validateDraft("polygon", [[105, 10], [105.1, 10], [105.2, 10]])).toMatch(/thẳng hàng/);
    expect(validateDraft("polygon", [[105, 10], [105.1, 10], [105.1, 10.1]])).toBeNull();
  });

  it("GeoJSON từ nét vẽ: vùng khép vòng, toạ độ làm tròn 7 chữ số", () => {
    const g = draftGeometry("polygon", [[105.123456789, 10], [105.2, 10], [105.2, 10.1]]);
    expect(g.type).toBe("Polygon");
    const ring = (g as { coordinates: LngLat[][] }).coordinates[0]!;
    expect(ring[0]).toEqual(ring[ring.length - 1]);
    expect(ring[0]![0]).toBe(105.1234568);
    expect(draftGeometry("point", [[1, 2]])).toEqual({ type: "Point", coordinates: [1, 2] });
  });

  it("đọc toạ độ nhập tay và bbox", () => {
    expect(parseLngLat("105.78, 10.03")).toEqual([105.78, 10.03]);
    expect(parseLngLat("105.78 10.03")).toEqual([105.78, 10.03]);
    expect(parseLngLat("105,78; 10")).toBeNull();
    expect(parseLngLat("200, 10")).toBeNull();
    expect(bboxParam({ west: 105.1234567, south: 9.5, east: 106, north: 10.5 })).toBe("105.123457,9.5,106,10.5");
    expect(parseBBox("105,9,106,10")).toEqual([105, 9, 106, 10]);
    expect(parseBBox("106,9,105,10")).toBeNull();
    expect(touchesBBox([[104, 9.5], [107, 9.5]], [105, 9, 106, 10])).toBe(true);
    expect(touchesBBox([[100, 1]], [105, 9, 106, 10])).toBe(false);
  });

  it("định dạng hiển thị tiếng Việt", () => {
    expect(formatLength(850)).toBe("850 m");
    expect(formatLength("12345.6")).toBe("12,35 km");
    expect(formatLength(null)).toBe("—");
    expect(formatAreaM2(500)).toBe("500 m²");
    expect(formatAreaM2(1_215_000)).toBe("121,5 ha");
    expect(measureDraft("line", [[105, 10]])).toBeNull();
    expect(measureDraft("polygon", [[105, 10], [105.01, 10], [105.01, 10.01]])).toMatch(/^Diện tích: .+ ha · Chu vi: .+ km$/);
  });
});

describe("chú giải và lớp bản đồ", () => {
  it("GIS FN-13 B2: 5 giai đoạn nâu → xanh → vàng → đỏ → xám", () => {
    expect(STAGES.map((s) => s.code)).toEqual(["chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu"]);
    expect(stageColor("chin_ro")).toBe("#D6452F");
    expect(stageColor("không có")).toBe("#cfd8d3");
  });

  it("GIS US-SEASON-01 / US-YIELD-01: polygon HTX tô theo giai đoạn trội; viền chín rộ; HTX chưa có ranh giới → marker", () => {
    const layer = {
      type: "FeatureCollection",
      truncated: false,
      limit: 1000,
      features: [
        { type: "Feature", id: "a", geometry: { type: "Polygon", coordinates: [[[105, 10], [105.1, 10], [105.1, 10.1], [105, 10]]] }, properties: { cooperative_ref: "a", marker_source: "party_location", area_ha: "12.5" } },
        { type: "Feature", id: "b", geometry: { type: "Point", coordinates: [105.3, 10.2] }, properties: { cooperative_ref: "b", marker_source: "party_location", name: "HTX B" } },
      ],
    } as unknown as GeoCoopLayer;
    const stages = { cooperatives: [{ cooperative_ref: "a", dominant_stage: "sap_thu", ripening_plot_count: 3, ripening_expected_tons: "40.2" }], plots: [] } as unknown as CropStageMap;
    const out = coopLayers(layer, new Map([["a", "HTX A"]]), stages, { colorByStage: true, ripening: true });
    expect(out.areas.features).toHaveLength(1);
    expect(out.areas.features[0]!.properties!.fill).toBe(stageColor("sap_thu"));
    expect(out.areas.features[0]!.properties!.name).toBe("HTX A");
    expect(out.ripening.features).toHaveLength(1);
    expect(out.markers.features[0]!.properties!.name).toBe("HTX B");
    const off = coopLayers(layer, new Map(), stages, { colorByStage: false, ripening: false });
    expect(off.areas.features[0]!.properties!.fill).toBe("#9fd8b4");
    expect(off.ripening.features).toHaveLength(0);
  });

  it("GIS US-WATER-01: lọc tải trọng → tuyến đi được đánh dấu, tuyến không đạt vẽ mờ", () => {
    const seg = (passable: boolean) => ({ segment_ref: passable ? "p" : "q", passable, geometry: { type: "LineString", coordinates: [] }, waterway_class: "song" }) as unknown as WaterwayLayerSegment;
    const fc = waterwayLayer([seg(true), seg(false)], true);
    expect(fc.features.map((f) => f.properties!.state)).toEqual(["passable", "blocked"]);
    expect(waterwayLayer([seg(false)], false).features[0]!.properties!.state).toBe("normal");
    expect(limitingLabel("unknown:depth")).toBe("thiếu số liệu độ sâu");
    expect(limitingLabel("width")).toBe("chiều rộng lòng kênh");
  });

  it("GIS US-HUB-01: marker Hub hiện % lấp đầy và màu theo lớp ngưỡng", () => {
    const w = { ref: "h", kind: "hub", name: "Hub X", lon: "105.5", lat: "10.2", fill_pct: "72.4", class: "near_full", free: "800", unit: "tan", origin: "mkg" } as CapacityWidget;
    const [m] = capacityMarkers([w]);
    expect(m!.text).toBe("H 72%");
    expect(m!.color).toBe("#C27C0E");
    expect(m!.title).toMatch(/Sắp đầy, còn trống 800 tấn/);
    expect(m!.className).toBe("mkg");
    expect(fillLabel(null)).toBe("?");
  });
});

describe("mô phỏng: đọc kết quả, tiền, chờ việc", () => {
  it("đọc khối result.v1 phòng thủ (thiếu trường → null)", () => {
    const s = summarize({ delivered_tons: 42000, costs: { cost_per_ton: "512000" }, financial: { tco: 1e11, roi_pct: 12.5, payback_years: 6, payback_status: "hoan_von", baseline_cost_per_ton: 612000 }, unapproved_parameters: [{ code: "x", number: 31, name: "Đơn giá" }] });
    expect(s.deliveredTons).toBe(42000);
    expect(s.costPerTon).toBe(512000);
    expect(s.paybackYears).toBe(6);
    expect(s.unapproved).toEqual([{ code: "x", number: 31, name: "Đơn giá" }]);
    expect(summarize(null).costPerTon).toBeNull();
    const d = savingVsBaseline(s.costPerTon, s.baselineCostPerTon)!;
    expect(d.saving).toBe(true);
    expect(d.pct).toBeCloseTo(-16.34, 1);
  });

  it("định dạng tiền đồng", () => {
    expect(formatVnd(612000)).toBe("612.000 đ");
    expect(formatVnd(12_500_000_000)).toBe("12,5 tỷ đ");
    expect(formatVnd(null)).toBe("—");
  });

  it("pollUntil hỏi lại tới khi xong (202 → job)", async () => {
    const states = ["queued", "running", "completed"];
    let i = 0;
    const seen: string[] = [];
    const last = await pollUntil(async () => states[Math.min(i++, 2)]!, (s) => s === "completed", { sleep: async () => undefined, onUpdate: (s) => seen.push(s) });
    expect(last).toBe("completed");
    expect(seen).toEqual(states);
  });
});
