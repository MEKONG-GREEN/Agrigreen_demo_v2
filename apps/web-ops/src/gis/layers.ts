// Chuyển dữ liệu API → FeatureCollection vẽ được cho LayerMap (tách khỏi trang để kiểm thử Vitest).
import type { CapacityWidget, CropStageMap, GeoCoopLayer, GeoLayers, GeoRoadLayer, WaterwayLayerSegment } from "@/api/types";
import type { AnyFeatureCollection, HtmlMarker } from "@/components/LayerMap";
import { CAPACITY, FACILITY_KIND, fillLabel, stageColor, stageLabel } from "./legend";
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
export function coopLayers(layer: GeoCoopLayer | null | undefined, names: ReadonlyMap<string, string>, stages: CropStageMap | null | undefined, opts: { colorByStage: boolean; ripening: boolean }): CoopLayers {
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
      fill: opts.colorByStage && st ? stageColor(st.dominant_stage) : "#9fd8b4",
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
export function plotLayer(layers: GeoLayers | null | undefined, stages: CropStageMap | null | undefined, colorByStage: boolean): AnyFeatureCollection {
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
          fill: colorByStage && s ? stageColor(s.stage) : "#c9e7b8",
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
export function capacityMarkers(items: readonly CapacityWidget[] | null | undefined, onClick?: (ref: string) => void): HtmlMarker[] {
  return (items ?? []).map((w) => {
    const cls = CAPACITY[w.class];
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
