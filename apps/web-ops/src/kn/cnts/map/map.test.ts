import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsMap } from "@/api/mock/kn-cnts-map";
import {
  addDays,
  circlePolygon,
  containsPoint,
  createCntsMapApi,
  distanceM,
  draftToGeometry,
  geometryFromGeoJSON,
  geometryToDraft,
  impactText,
  sumImpact,
  zoneErrors,
  zonesGeoJSON,
  type Zone,
} from "./api";

describe("hình học bản đồ CNTS (W11.52)", () => {
  it("vòng tròn bán kính chứa điểm trong, loại điểm ngoài", () => {
    const g = circlePolygon([105.1, 10.4], 3000);
    expect(g.type).toBe("Polygon");
    const ring = g.coordinates[0] as number[][];
    expect(ring[0]).toEqual(ring.at(-1));
    expect(Math.abs(distanceM([105.1, 10.4], ring[0] as [number, number]) - 3000)).toBeLessThan(30);
    expect(containsPoint(g, [105.1, 10.41])).toBe(true); // ~1,1 km
    expect(containsPoint(g, [105.1, 10.445])).toBe(false); // ~5 km
  });

  it("đa giác có lỗ", () => {
    const g = { type: "Polygon" as const, coordinates: [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]], [[4, 4], [6, 4], [6, 6], [4, 6], [4, 4]]] };
    expect(containsPoint(g, [2, 2])).toBe(true);
    expect(containsPoint(g, [5, 5])).toBe(false);
  });

  it("đỉnh vẽ ↔ GeoJSON, nhập GeoJSON", () => {
    const g = draftToGeometry("polygon", [[105, 10], [105.1, 10], [105.1, 10.1]])!;
    expect((g.coordinates[0] as number[][]).length).toBe(4);
    expect(geometryToDraft(g)).toHaveLength(3);
    expect(draftToGeometry("line", [[105, 10]])).toBeNull();
    expect(geometryFromGeoJSON(JSON.stringify({ type: "FeatureCollection", features: [{ type: "Feature", geometry: { type: "Point", coordinates: [1, 2] } }, { type: "Feature", geometry: g }] }))).toEqual(g);
    expect(geometryFromGeoJSON("không phải json")).toBeNull();
  });

  it("kiểm biểu vẽ vùng", () => {
    expect(zoneErrors({ kind: "vung_dich", name: "x", communes: ["91301"], draw_mode: "radius" }, 0)).toHaveProperty("center");
    expect(zoneErrors({ kind: "huong_di_chuyen", name: "x", communes: ["91301"], draw_mode: "polygon" }, 3)).toHaveProperty("draw_mode");
    expect(zoneErrors({ kind: "vung_chan_nuoi", name: "x", communes: ["91301"], draw_mode: "polygon" }, 3)).toEqual({});
    expect(addDays("2027-10-13", -13)).toBe("2027-09-30");
  });

  it("số hộ, diện tích thủy sản bị ảnh hưởng của vùng (FN-18 BR-02)", () => {
    const i = sumImpact([
      { affected_households: 3, aqua_farmed_ha: "2.00", aqua_wild_ha: "0.50" },
      { affected_households: 1, aqua_farmed_ha: null, aqua_wild_ha: null },
      { affected_households: 4, aqua_farmed_ha: "0.75", aqua_wild_ha: null },
    ]);
    expect(i).toEqual({ outbreak_count: 3, affected_households: 8, aqua_farmed_ha: "2.75", aqua_wild_ha: "0.50" });
    expect(impactText(i)).toContain("8 hộ bị ảnh hưởng (3 ổ dịch)");
    expect(impactText(sumImpact([]))).toBe("0 hộ bị ảnh hưởng (0 ổ dịch)");
  });

  it("xuất GeoJSON", () => {
    const z = { ref: "r", version: 1, kind: "vung_dem", name: "Đệm", communes: ["91301"], effective_on: "2027-10-10", radius_m: 10000, area_ha: "1",
      outbreak_ref: null, status: "active", geometry: circlePolygon([105, 10], 100) } as unknown as Zone;
    const fc = zonesGeoJSON([z]);
    expect(fc.features[0]!.properties.kind_label).toBe("Vùng đệm");
  });
});

describe("giả lập /v1/extension/cnts/map", () => {
  beforeEach(() => resetCntsMap());
  const mapi = createCntsMapApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }));

  it("vùng theo ngày, phiên bản có lý do, đối tượng trong vùng, khoanh từ ổ dịch", async () => {
    const day = addDays(new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10), -1);
    const l = await mapi.layers(day);
    expect(l).not.toHaveProperty("plot_stats");
    expect(l).not.toHaveProperty("plots_ready");
    expect(l.zones).toHaveLength(3);
    expect((await mapi.layers(addDays(day, -10))).zones).toHaveLength(1);
    const threat = l.zones.find((z) => z.kind === "vung_uy_hiep")!;
    const inside = await mapi.contents(threat.ref, day);
    expect(inside.outbreak_count).toBe(1);
    expect(inside.site_count).toBe(1);
    expect(inside.farm_count).toBe(1);
    expect(inside.impact?.affected_households).toBe(3);
    expect(l.farms.map((f) => f.code)).toEqual(["HCN0000001", "HCN0000002"]);
    expect(l.zone_impacts.map((z) => z.zone_ref).sort()).toEqual(l.zones.filter((z) => z.kind === "vung_dich" || z.kind === "vung_uy_hiep").map((z) => z.ref).sort());
    const fish = l.zones.find((z) => z.kind === "vung_thuy_san")!;
    const body = { kind: fish.kind, name: fish.name, communes: fish.communes, draw_mode: "polygon" as const, geometry: fish.geometry, reason: "Điều chỉnh ranh" };
    await expect(mapi.updateZone(fish.ref, { ...body, base_version: 9 })).rejects.toThrow();
    const ok = await mapi.updateZone(fish.ref, { ...body, base_version: 1 });
    expect(ok.version).toBe(2);
    expect((await mapi.zone(fish.ref)).versions).toHaveLength(2);
    const r = await mapi.zoneStatus(fish.ref, true, 2, "Giải thể vùng nuôi");
    expect(r.status).toBe("retired");
    const made = await mapi.fromOutbreak(l.outbreaks[0]!.ref);
    expect(made.map((z) => z.radius_m)).toEqual([500, 3000, 10000]);
  });
});
