// Chuyển dữ liệu API → FeatureCollection vẽ được cho LayerMap (tách khỏi trang để kiểm thử Vitest).
import type { CapacityWidget, CropStageMap, GeoCoopLayer, GeoLayers, GeoRoadLayer, MechCoverageItem, WaterwayLayerSegment } from "@/api/types";
import type { CoverageStatus } from "@/cgh/coverage";
import { buildPins, type CoopLocation } from "@/cgh/pins";
import type { AnyFeatureCollection, HtmlMarker } from "@/components/LayerMap";
import { capacityStyle, DEFAULT_PALETTE, FACILITY_KIND, fillLabel, stageColor, stageLabel, type DisplayPalette } from "./legend";
import type { LngLat } from "./geometry";

const EMPTY: AnyFeatureCollection = { type: "FeatureCollection", features: [] };

export interface CoopLayers {
  areas: AnyFeatureCollection;
  markers: AnyFeatureCollection;
  ripening: AnyFeatureCollection;
}

/**
 * Ranh giới HTX (polygon) + HTX chưa có ranh giới (marker — FN-04 B2), tô theo giai đoạn trội khi bật lớp mùa vụ
 * (GIS US-SEASON-01), viền đỏ vùng chín rộ 1–3 ngày tới (US-YIELD-01).
 */
export function coopLayers(layer: GeoCoopLayer | null | undefined, names: ReadonlyMap<string, string>, stages: CropStageMap | null | undefined, opts: { colorByStage: boolean; ripening: boolean }, palette: DisplayPalette = DEFAULT_PALETTE): CoopLayers {
  if (!layer) return { areas: EMPTY, markers: EMPTY, ripening: EMPTY };
  const byCoop = new Map((stages?.cooperatives ?? []).map((c) => [c.cooperative_ref, c]));
  const areas: AnyFeatureCollection["features"] = [];
  const markers: AnyFeatureCollection["features"] = [];
  const ripening: AnyFeatureCollection["features"] = [];
  for (const f of layer.features) {
    const ref = f.properties.cooperative_ref;
    const st = byCoop.get(ref);
    const name = names.get(ref) ?? f.properties.name ?? f.properties.code ?? "HTX";
    const props = {
      cooperative_ref: ref,
      name,
      stage: st?.dominant_stage ?? null,
      stage_label: st ? stageLabel(st.dominant_stage) : null,
      fill: opts.colorByStage && st ? stageColor(st.dominant_stage, palette) : "#9fd8b4",
      ripening_tons: st?.ripening_expected_tons ?? null,
      area_ha: f.properties.area_ha ?? null,
    };
    if (f.geometry.type === "Point") {
      markers.push({ type: "Feature", id: ref, geometry: f.geometry, properties: props });
      continue;
    }
    areas.push({ type: "Feature", id: ref, geometry: f.geometry, properties: props });
    if (opts.ripening && st && st.ripening_plot_count > 0) ripening.push({ type: "Feature", id: ref, geometry: f.geometry, properties: props });
  }
  return { areas: { type: "FeatureCollection", features: areas }, markers: { type: "FeatureCollection", features: markers }, ripening: { type: "FeatureCollection", features: ripening } };
}

/** Thửa (chỉ khi zoom ≥ ngưỡng — BR-24) tô theo giai đoạn của vụ trên thửa. */
export function plotLayer(layers: GeoLayers | null | undefined, stages: CropStageMap | null | undefined, colorByStage: boolean, palette: DisplayPalette = DEFAULT_PALETTE): AnyFeatureCollection {
  const plots = layers?.plots;
  if (!plots) return EMPTY;
  const byPlot = new Map((stages?.plots ?? []).map((p) => [p.plot_ref, p]));
  return {
    type: "FeatureCollection",
    features: plots.features.map((f) => {
      const s = byPlot.get(f.properties.ref);
      return {
        type: "Feature",
        id: f.properties.ref,
        geometry: f.geometry,
        properties: {
          ref: f.properties.ref,
          name: f.properties.name ?? f.properties.code,
          code: f.properties.code,
          stage: s?.stage ?? null,
          stage_label: s ? stageLabel(s.stage) : "Chưa có vụ",
          ripening: s?.ripening ?? false,
          fill: colorByStage && s ? stageColor(s.stage, palette) : "#c9e7b8",
        },
      };
    }),
  };
}

export function roadLayer(layer: GeoRoadLayer | null | undefined, filtered: boolean): AnyFeatureCollection {
  if (!layer) return EMPTY;
  return {
    type: "FeatureCollection",
    features: layer.features.map((f) => ({
      type: "Feature",
      id: f.properties.ref,
      geometry: f.geometry,
      properties: { ...f.properties, highlight: filtered },
    })),
  };
}

export function waterwayLayer(items: readonly WaterwayLayerSegment[] | null | undefined, filtered: boolean): AnyFeatureCollection {
  if (!items) return EMPTY;
  return {
    type: "FeatureCollection",
    features: items.map((s) => ({
      type: "Feature",
      id: s.segment_ref,
      geometry: s.geometry,
      properties: {
        segment_ref: s.segment_ref,
        name: s.name,
        waterway_class: s.waterway_class,
        max_load_tons: s.max_load_tons,
        length_m: s.length_m,
        limiting: s.limiting,
        passable: s.passable,
        // Khi lọc theo tải trọng: tuyến đi được đánh dấu, tuyến không đạt vẽ mờ (US-WATER-01 AC-1/AC-2).
        state: !filtered ? "normal" : s.passable ? "passable" : "blocked",
      },
    })),
  };
}

/** Widget % dung lượng trên marker Hub/kho (GIS US-HUB-01 AC-1): màu theo lớp, % làm tròn. */
export function capacityMarkers(items: readonly CapacityWidget[] | null | undefined, onClick?: (ref: string) => void, palette: DisplayPalette = DEFAULT_PALETTE): HtmlMarker[] {
  return (items ?? []).map((w) => {
    const cls = capacityStyle(w.class, palette);
    const unit = w.unit === "tan" ? "tấn" : (w.unit ?? "");
    return {
      id: w.ref,
      lngLat: [Number(w.lon), Number(w.lat)] as LngLat,
      text: `${FACILITY_KIND[w.kind]?.charAt(0) ?? "H"} ${fillLabel(w.fill_pct)}`,
      title: `${FACILITY_KIND[w.kind] ?? w.kind} ${w.name}: ${w.fill_pct ? fillLabel(w.fill_pct) : "chưa có số liệu"} — ${cls.label}${w.free ? `, còn trống ${Number(w.free).toLocaleString("vi-VN")} ${unit}` : ""}`,
      className: w.origin === "mkg" ? "mkg" : "",
      color: cls.color,
      onClick: onClick ? () => onClick(w.ref) : undefined,
    };
  });
}

/** Thứ tự chú giải lớp CGH trên GIS (GIS US-INT-01): Thiếu / Cần chú ý / Đủ / Thừa / Chưa có dữ liệu. */
export const CGH_LEGEND_ORDER: readonly CoverageStatus[] = ["thieu", "chu_y", "du", "thua", "khong_du_lieu"];

/**
 * Lớp "Tình trạng máy CGH" (GIS US-INT-01): mỗi HTX một điểm, màu = `overall.color` của KẾT QUẢ CÂN ĐỐI ĐÃ LƯU
 * (Bản đồ CGH) — GIS chỉ hiển thị, không tính lại. Vị trí: toạ độ hồ sơ HTX, thiếu thì tâm xã (vị trí tạm).
 */
export function cghLayer(
  items: readonly MechCoverageItem[] | null | undefined,
  locations: ReadonlyMap<string, CoopLocation>,
  centroids: ReadonlyMap<string, [number, number]>,
): { fc: AnyFeatureCollection; unplaced: number } {
  if (!items) return { fc: EMPTY, unplaced: 0 };
  const { pins, unplaced } = buildPins(items, locations, centroids);
  const byRef = new Map(items.filter((i) => i.cooperative_ref).map((i) => [i.cooperative_ref!, i]));
  return {
    unplaced,
    fc: {
      type: "FeatureCollection",
      features: pins.map((p) => {
        const g = byRef.get(p.ref)?.overall;
        return {
          type: "Feature",
          id: p.ref,
          geometry: { type: "Point", coordinates: [p.lng, p.lat] },
          properties: {
            ref: p.ref,
            name: p.name,
            code: p.code,
            status: p.status,
            status_label: p.statusLabel,
            color: p.color,
            provisional: p.provisional,
            coverage_pct: g?.coverage_pct ?? null,
            required_whole: g?.required_whole ?? null,
            capacity: g?.capacity ?? null,
            machines_total: g?.machines_total ?? null,
          },
        };
      }),
    },
  };
}
