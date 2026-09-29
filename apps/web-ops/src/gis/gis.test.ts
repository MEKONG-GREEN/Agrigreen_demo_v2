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
import { capacityMarkers, cghLayer, coopLayers, plotLayer, waterwayLayer } from "./layers";
import {
  capacityLegend,
  DEFAULT_PALETTE,
  fillLabel,
  HEX_COLOR_ERROR,
  isHexColor,
  limitingLabel,
  paletteFromConfig,
  PERCENT_ERROR,
  stageColor,
  stages as stageLegend,
  validateThresholds,
  type DisplayConfig,
} from "./legend";
import type { MechCoverageItem } from "@/api/types";

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
    expect(stageLegend().map((s) => s.code)).toEqual(["chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu"]);
    expect(stageColor("chin_ro")).toBe("#D6452F");
    expect(stageColor("không có")).toBe("#CFD8D3");
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

function dv(key: string, value: string, def: string, version = 1): DisplayConfig["ripening_outline_color"] {
  return { key, value, default: def, version, source: version > 0 ? "approved" : "default" };
}

function config(over: Partial<DisplayConfig> = {}): DisplayConfig {
  const stageDefaults = DEFAULT_PALETTE.stage;
  const capDefaults = DEFAULT_PALETTE.capacity;
  return {
    as_of: "2026-09-30T00:00:00Z",
    stage_colors: Object.fromEntries(Object.entries(stageDefaults).map(([k, v]) => [k, dv(`gis.stage_color:${k}`, v, v, 0)])),
    capacity_colors: Object.fromEntries(Object.entries(capDefaults).map(([k, v]) => [k, dv(`gis.capacity_color:${k}`, v, v, 0)])),
    ripening_outline_color: dv("gis.ripening_outline_color", "#B3261E", "#B3261E", 0),
    hub_capacity_warn_pct: dv("geo.hub_capacity_warn_pct", "80", "80", 0),
    hub_capacity_full_pct: dv("geo.hub_capacity_full_pct", "95", "95", 0),
    plot_layer_min_zoom: dv("geo.plot_layer_min_zoom", "13", "13", 0),
    ...over,
  };
}

describe("quy chuẩn hiển thị bản đồ (GIS US-CFG-01/02)", () => {
  it("GIS US-CFG-01: paletteFromConfig lấy màu đã duyệt, chuẩn hoá HEX in hoa; không có cấu hình → mặc định", () => {
    const cfg = config({
      stage_colors: { ...config().stage_colors, chin_ro: dv("gis.stage_color:chin_ro", "#1e90ff", "#D6452F", 2) },
      ripening_outline_color: dv("gis.ripening_outline_color", "#112233", "#B3261E", 1),
      hub_capacity_warn_pct: dv("geo.hub_capacity_warn_pct", "70.5", "80", 1),
    });
    const p = paletteFromConfig(cfg);
    expect(p.stage.chin_ro).toBe("#1E90FF");
    expect(stageColor("chin_ro", p)).toBe("#1E90FF");
    expect(stageLegend(p).find((s) => s.code === "chin_ro")!.color).toBe("#1E90FF");
    expect(p.ripeningOutline).toBe("#112233");
    expect(p.warnPct).toBe("70.5");
    expect(paletteFromConfig(null)).toEqual(DEFAULT_PALETTE);
    expect(paletteFromConfig(undefined).stage.sap_thu).toBe("#E0B000");
  });

  it("GIS US-CFG-01: giá trị sai định dạng → dùng mặc định của khóa đó (từng khóa); ngưỡng lệch thứ tự → cặp mặc định", () => {
    const cfg = config({
      stage_colors: { ...config().stage_colors, sap_thu: dv("gis.stage_color:sap_thu", "vàng", "#E0B000") },
      capacity_colors: { available: dv("gis.capacity_color:available", "#12345", "#1F7A38") },
      hub_capacity_warn_pct: dv("geo.hub_capacity_warn_pct", "96", "80"),
    });
    const p = paletteFromConfig(cfg);
    expect(p.stage.sap_thu).toBe("#E0B000");
    expect(p.capacity.available).toBe("#1F7A38");
    expect(p.capacity.full).toBe("#B3261E");
    expect([p.warnPct, p.fullPct]).toEqual(["80", "95"]);
  });

  it("GIS US-CFG-01: isHexColor chỉ nhận #RRGGBB; thông điệp HEX đúng AC", () => {
    expect(isHexColor("#1E90FF")).toBe(true);
    expect(isHexColor("#1e90ff")).toBe(true);
    for (const bad of ["1E90FF", "#1E90F", "#1E90FFF", "#GGGGGG", "red", "", null, undefined]) expect(isHexColor(bad)).toBe(false);
    expect(HEX_COLOR_ERROR).toBe("Mã màu không đúng định dạng HEX (ví dụ: #1E90FF).");
  });

  it("GIS US-CFG-01 AC-2: validateThresholds — tăng dần nghiêm ngặt, trong 0–100%, chuỗi thập phân", () => {
    expect(PERCENT_ERROR).toBe("Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%.");
    expect(validateThresholds("80", "95")).toBeNull();
    expect(validateThresholds("0", "100")).toBeNull();
    expect(validateThresholds("79.99", "80")).toBeNull();
    expect(validateThresholds("95", "80")).toBe(PERCENT_ERROR);
    expect(validateThresholds("80", "80")).toBe(PERCENT_ERROR);
    expect(validateThresholds("80", "100.01")).toBe(PERCENT_ERROR);
    expect(validateThresholds("-1", "50")).toBe(PERCENT_ERROR);
    expect(validateThresholds("abc", "50")).toBe(PERCENT_ERROR);
    expect(validateThresholds("", "50")).toBe(PERCENT_ERROR);
  });

  it("GIS US-CFG-01: chú giải dung lượng lấy ngưỡng từ display-config, ưu tiên ngưỡng của widget khi có", () => {
    const p = paletteFromConfig(config({ hub_capacity_warn_pct: dv("geo.hub_capacity_warn_pct", "70", "80") }));
    const l = capacityLegend(p);
    expect(l.map((x) => x.code)).toEqual(["available", "near_full", "full", "unknown"]);
    expect(l.find((x) => x.code === "near_full")!.note).toBe("≥ 70%");
    expect(l.find((x) => x.code === "full")!.note).toBe("≥ 95%");
    expect(capacityLegend(p, { warn_pct: "60", full_pct: "85" }).find((x) => x.code === "full")!.note).toBe("≥ 85%");
  });

  it("GIS US-CFG-02: màu mới áp lên lớp HTX/thửa/marker kho khi truyền bảng màu", () => {
    const p = paletteFromConfig(
      config({
        stage_colors: { ...config().stage_colors, sap_thu: dv("gis.stage_color:sap_thu", "#010203", "#E0B000") },
        capacity_colors: { ...config().capacity_colors, near_full: dv("gis.capacity_color:near_full", "#0A0B0C", "#C27C0E") },
      }),
    );
    const layer = {
      type: "FeatureCollection",
      features: [{ type: "Feature", id: "a", geometry: { type: "Polygon", coordinates: [[[105, 10], [105.1, 10], [105.1, 10.1], [105, 10]]] }, properties: { cooperative_ref: "a", marker_source: "party_location" } }],
    } as unknown as GeoCoopLayer;
    const st = { cooperatives: [{ cooperative_ref: "a", dominant_stage: "sap_thu", ripening_plot_count: 0 }], plots: [{ plot_ref: "p1", stage: "sap_thu", ripening: false }] } as unknown as CropStageMap;
    expect(coopLayers(layer, new Map(), st, { colorByStage: true, ripening: false }, p).areas.features[0]!.properties!.fill).toBe("#010203");
    const plots = { plots: { type: "FeatureCollection", features: [{ type: "Feature", geometry: null, properties: { ref: "p1", code: "T1" } }] } } as never;
    expect(plotLayer(plots, st, true, p).features[0]!.properties!.fill).toBe("#010203");
    const w = { ref: "h", kind: "hub", name: "Hub X", lon: "105.5", lat: "10.2", fill_pct: "82", class: "near_full", free: null, unit: "tan", origin: "tenant" } as unknown as CapacityWidget;
    expect(capacityMarkers([w], undefined, p)[0]!.color).toBe("#0A0B0C");
  });
});

describe("lớp Tình trạng máy CGH trên GIS (GIS US-INT-01)", () => {
  const group = (status: string, color: string, pct: string | null) => ({ required: "3.0000", required_whole: 3, capacity: 2, machines_total: 4, coverage_pct: pct, status, status_label: status === "shortage" ? "Thiếu" : "Đủ", color });
  const item = (ref: string, commune: string, status: string, color: string) =>
    ({ unit_code: null, cooperative_ref: ref, name: `HTX ${ref}`, province_code: "82", commune_code: commune, cooperatives: 1, cooperative_code: `HTX-${ref}`, overall: group(status, color, "66.67"), stages: {} }) as unknown as MechCoverageItem;

  it("GIS US-INT-01: màu lấy đúng overall.color do Bản đồ CGH trả (không tính lại); vị trí hồ sơ hoặc tâm xã (vị trí tạm)", () => {
    const items = [item("a", "82001", "shortage", "#A3372A"), item("b", "82002", "sufficient", "#1F7A38"), item("c", "99999", "sufficient", "#1F7A38")];
    const locs = new Map([
      ["a", { ref: "a", location: { lat: "10.5", lon: "105.6" }, location_provisional: false }],
      ["b", { ref: "b", location: null, location_provisional: true }],
      ["c", { ref: "c", location: null, location_provisional: true }],
    ]);
    const out = cghLayer(items, locs, new Map([["82002", [105.7, 10.4] as [number, number]]]));
    expect(out.unplaced).toBe(1);
    expect(out.fc.features).toHaveLength(2);
    const [a, b] = out.fc.features;
    expect(a!.properties).toMatchObject({ color: "#A3372A", status: "thieu", provisional: false, required_whole: 3, capacity: 2, machines_total: 4, coverage_pct: "66.67", code: "HTX-a" });
    expect(a!.geometry).toEqual({ type: "Point", coordinates: [105.6, 10.5] });
    expect(b!.properties).toMatchObject({ color: "#1F7A38", provisional: true });
    expect(cghLayer(null, locs, new Map()).fc.features).toHaveLength(0);
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
