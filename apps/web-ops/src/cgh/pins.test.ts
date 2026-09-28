import { describe, expect, it } from "vitest";
import type { MechCoverageItem, MechGroup } from "@/api/types";
import { buildPins, communeCentroids, normalizeVi, pinsToGeoJSON, searchPins, type CoopLocation } from "./pins";

const g = (status: MechGroup["status"], color: string, label: string): MechGroup => ({
  required: "1.0000",
  required_whole: 1,
  capacity: 1,
  machines_total: 1,
  coverage_pct: status === "no_data" ? null : "90.00",
  status,
  status_label: label,
  color,
});

function item(i: number, commune: string, status: MechGroup["status"] = "sufficient"): MechCoverageItem {
  return {
    unit_code: null,
    cooperative_ref: `ref-${i}`,
    cooperative_code: `HTX92${String(i).padStart(5, "0")}`,
    name: i === 7 ? "HTX Nông nghiệp Phước Thới" : `HTX Dịch vụ ${i}`,
    province_code: "92",
    commune_code: commune,
    cooperatives: 1,
    area_source: "manual",
    area_source_label: "[Nhập tay]",
    area_locked: false,
    overall: g(status, "#1F7A38", "Đủ"),
    stages: { thu_hoach: g("shortage", "#A3372A", "Thiếu") },
  };
}

const square = (code: string, x: number, y: number) => ({
  type: "Feature" as const,
  geometry: { type: "Polygon" as const, coordinates: [[[x, y], [x + 1, y], [x + 1, y + 1], [x, y + 1], [x, y]]] },
  properties: { code },
});

describe("ghim HTX bản đồ CGH (FN-08)", () => {
  const centroids = communeCentroids({ type: "FeatureCollection", features: [square("31135", 105, 10)] });

  it("toạ độ HTX khi có; chưa có toạ độ → tâm xã, vị trí tạm; không có tâm xã → không ghim", () => {
    const locs = new Map<string, CoopLocation>([
      ["ref-1", { ref: "ref-1", admin_unit_code: "31135", commune_name: "Phường Ninh Kiều", location: { lat: "10.03", lon: "105.78" }, location_provisional: false }],
      ["ref-2", { ref: "ref-2", admin_unit_code: "31135", commune_name: "Phường Ninh Kiều", location: null, location_provisional: true }],
    ]);
    const { pins, unplaced } = buildPins([item(1, "31135"), item(2, "31135"), item(3, "99999")], locs, centroids);
    expect(unplaced).toBe(1);
    expect(pins.map((p) => [p.ref, p.lng, p.lat, p.provisional])).toEqual([
      ["ref-1", 105.78, 10.03, false],
      ["ref-2", 105.5, 10.5, true],
    ]);
    expect(pins[0]).toMatchObject({ status: "du", color: "#1F7A38", statusLabel: "Đủ", code: "HTX9200001" });
  });

  it("lọc khâu dùng mức của khâu đó", () => {
    const { pins } = buildPins([item(1, "31135")], new Map(), centroids, "thu_hoach");
    expect(pins[0]).toMatchObject({ status: "thieu", color: "#A3372A", provisional: true });
    expect(pinsToGeoJSON(pins).features[0]?.properties).toMatchObject({ id: "ref-1", provisional: true, color: "#A3372A" });
  });

  it("tra cứu không dấu theo tên, Mã HTX, xã", () => {
    const locs = new Map<string, CoopLocation>([["ref-7", { ref: "ref-7", admin_unit_code: "31135", commune_name: "Phường Ninh Kiều", location: null, location_provisional: true }]]);
    const { pins } = buildPins([item(7, "31135"), item(8, "31135")], locs, centroids);
    expect(searchPins(pins, "phuoc thoi").map((p) => p.ref)).toEqual(["ref-7"]);
    expect(searchPins(pins, "htx9200008").map((p) => p.ref)).toEqual(["ref-8"]);
    expect(searchPins(pins, "ninh kieu").map((p) => p.ref)).toEqual(["ref-7"]);
    expect(searchPins(pins, "không có")).toEqual([]);
    expect(normalizeVi("Đồng Tháp")).toBe("dong thap");
  });

  it("hiệu năng: ~1.200 HTX dựng ghim và lọc dưới 1 giây", () => {
    const n = 1200;
    const communes = Array.from({ length: 120 }, (_, i) => square(String(30000 + i), 104 + (i % 12) * 0.2, 9 + Math.floor(i / 12) * 0.2));
    const cents = communeCentroids({ type: "FeatureCollection", features: communes });
    const items = Array.from({ length: n }, (_, i) => item(i, String(30000 + (i % 120))));
    const t0 = performance.now();
    const { pins } = buildPins(items, new Map(), cents);
    let hits = 0;
    for (const q of ["htx", "dich vu 11", "3001", "phuoc", "HTX9200119"]) hits += searchPins(pins, q, 50).length;
    const elapsed = performance.now() - t0;
    expect(pins).toHaveLength(n);
    expect(hits).toBeGreaterThan(0);
    expect(elapsed).toBeLessThan(1000);
  });
});
