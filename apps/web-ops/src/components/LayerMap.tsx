"use client";

import type { GeoJSONSource, LayerSpecification, Map as MlMap, Marker, StyleSpecification } from "maplibre-gl";
import { useEffect, useRef, useState } from "react";
import { config } from "@/config";
import { bboxParam, type DrawMode, type LngLat } from "@/gis/geometry";

type GeoJsonData = Parameters<GeoJSONSource["setData"]>[0];

/** FeatureCollection bất kỳ (hình từ API). */
export interface AnyFeatureCollection {
  type: "FeatureCollection";
  features: { type: "Feature"; id?: string | number; geometry: unknown; properties: Record<string, unknown> | null }[];
}

export interface HtmlMarker {
  id: string;
  lngLat: LngLat;
  text: string;
  title: string;
  className?: string;
  color?: string;
  onClick?: () => void;
}

export interface Viewport {
  bbox: string;
  zoom: number;
}

export interface DraftShape {
  mode: DrawMode;
  coords: LngLat[];
  /** Kéo một đỉnh (US-ROAD-03, US-WATER-03: kéo/thêm/xoá điểm). */
  onMove?: (index: number, to: LngLat) => void;
}

/**
 * Nền trơn, không gọi tile/API bên ngoài (RB-7, ADR-0007). Style chỉ lấy từ NEXT_PUBLIC_MAP_STYLE_URL (máy chủ trong lãnh thổ).
 * Nền trơn không có glyph → nhãn/% dùng marker HTML thay cho lớp symbol.
 */
const BLANK_STYLE: StyleSpecification = {
  version: 8,
  sources: {},
  layers: [{ id: "bg", type: "background", paint: { "background-color": "#e3eef2" } }],
};

const EMPTY: GeoJsonData = { type: "FeatureCollection", features: [] };

const DRAFT_LAYERS: LayerSpecification[] = [
  { id: "draft-fill", type: "fill", source: "draft", filter: ["==", ["geometry-type"], "Polygon"], paint: { "fill-color": "#1976d2", "fill-opacity": 0.18 } },
  { id: "draft-line", type: "line", source: "draft", filter: ["!=", ["geometry-type"], "Point"], paint: { "line-color": "#1565c0", "line-width": 2.5, "line-dasharray": [2, 1] } },
  {
    id: "draft-vertex",
    type: "circle",
    source: "draft",
    filter: ["==", ["geometry-type"], "Point"],
    paint: { "circle-radius": 6, "circle-color": "#ffffff", "circle-stroke-color": "#1565c0", "circle-stroke-width": 2.5 },
  },
];

function draftData(d: DraftShape | null | undefined): GeoJsonData {
  if (!d || d.coords.length === 0) return EMPTY;
  const features: unknown[] = d.coords.map((c, i) => ({ type: "Feature", properties: { idx: i }, geometry: { type: "Point", coordinates: c } }));
  if (d.mode === "line" && d.coords.length >= 2) features.push({ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: d.coords } });
  if (d.mode === "polygon" && d.coords.length >= 2) {
    const ring = [...d.coords, d.coords[0]];
    features.push({ type: "Feature", properties: {}, geometry: d.coords.length >= 3 ? { type: "Polygon", coordinates: [ring] } : { type: "LineString", coordinates: ring } });
  }
  return { type: "FeatureCollection", features } as GeoJsonData;
}

/**
 * Bản đồ nhiều lớp dùng chung cho web GIS (MapLibre, BSD-3-Clause).
 * - `sources` cập nhật dữ liệu bằng setData; `layers` đọc một lần lúc khởi tạo (paint theo thuộc tính).
 * - `onViewport` báo khung nhìn (bbox, zoom) sau mỗi lần dừng kéo/zoom → trang tải theo viewport (GIS BR-25).
 * - `draft` vẽ nét đang biên tập (điểm/tuyến/vùng); đỉnh kéo được.
 */
export function LayerMap({
  label,
  sources,
  layers,
  visibility,
  onViewport,
  onMapClick,
  onFeatureClick,
  markers,
  draft,
  focus,
  height = "100%",
  initial,
}: {
  label: string;
  sources: Record<string, AnyFeatureCollection | null | undefined>;
  layers: LayerSpecification[];
  visibility?: Record<string, boolean>;
  onViewport?: (v: Viewport) => void;
  onMapClick?: (at: LngLat) => void;
  onFeatureClick?: (layerId: string, properties: Record<string, unknown>) => void;
  markers?: HtmlMarker[];
  draft?: DraftShape | null;
  /** Bay tới khung/điểm khi `key` đổi. */
  focus?: { key: string; bounds?: [LngLat, LngLat]; center?: LngLat; zoom?: number } | null;
  height?: number | string;
  initial?: { center: LngLat; zoom: number };
}) {
  const el = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MlMap | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const cb = useRef({ onViewport, onMapClick, onFeatureClick, draft });
  useEffect(() => {
    cb.current = { onViewport, onMapClick, onFeatureClick, draft };
  });
  const initRef = useRef({ label, layers, sourceIds: Object.keys(sources), initial });

  useEffect(() => {
    let disposed = false;
    let map: MlMap | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    (async () => {
      try {
        const ml = await import("maplibre-gl");
        if (disposed || !el.current) return;
        ml.setWorkerUrl("/maplibre/maplibre-gl-worker.mjs");
        const init = initRef.current;
        map = new ml.Map({
          container: el.current,
          style: config.mapStyleUrl || BLANK_STYLE,
          center: init.initial?.center ?? [105.8, 10.1],
          zoom: init.initial?.zoom ?? 8,
          attributionControl: false,
          doubleClickZoom: false,
        });
        map.addControl(new ml.NavigationControl({ showCompass: false }), "top-right");
        map.addControl(new ml.ScaleControl({ unit: "metric" }), "bottom-left");
        map.addControl(new ml.AttributionControl({ compact: true, customAttribution: "Dữ liệu: AgriGreen" }));
        const report = () => {
          if (!map) return;
          const b = map.getBounds();
          cb.current.onViewport?.({ bbox: bboxParam({ west: b.getWest(), south: b.getSouth(), east: b.getEast(), north: b.getNorth() }), zoom: map.getZoom() });
        };
        map.on("load", () => {
          if (!map) return;
          for (const id of init.sourceIds) map.addSource(id, { type: "geojson", data: EMPTY });
          map.addSource("draft", { type: "geojson", data: EMPTY });
          for (const l of init.layers) map.addLayer(l);
          for (const l of DRAFT_LAYERS) map.addLayer(l);
          for (const l of init.layers) {
            if (l.type === "background") continue;
            map.on("click", l.id, (e) => {
              if (cb.current.draft) return;
              const p = e.features?.[0]?.properties;
              if (p) cb.current.onFeatureClick?.(l.id, p as Record<string, unknown>);
            });
            map.on("mouseenter", l.id, () => map && (map.getCanvas().style.cursor = "pointer"));
            map.on("mouseleave", l.id, () => map && (map.getCanvas().style.cursor = ""));
          }
          map.on("click", (e) => {
            // Bấm vào đỉnh đang có (để kéo) không thêm đỉnh mới.
            if (cb.current.draft && map && map.queryRenderedFeatures(e.point, { layers: ["draft-vertex"] }).length > 0) return;
            cb.current.onMapClick?.([e.lngLat.lng, e.lngLat.lat]);
          });
          // Kéo đỉnh nét vẽ.
          let dragIdx: number | null = null;
          map.on("mousedown", "draft-vertex", (e) => {
            const idx = e.features?.[0]?.properties?.idx;
            if (typeof idx !== "number" || !cb.current.draft?.onMove) return;
            e.preventDefault();
            dragIdx = idx;
            map?.dragPan.disable();
          });
          map.on("mousemove", (e) => {
            if (dragIdx === null) return;
            cb.current.draft?.onMove?.(dragIdx, [e.lngLat.lng, e.lngLat.lat]);
          });
          map.on("mouseup", () => {
            if (dragIdx === null) return;
            dragIdx = null;
            map?.dragPan.enable();
          });
          map.on("moveend", () => {
            clearTimeout(timer);
            timer = setTimeout(report, 250);
          });
          mapRef.current = map;
          if (config.mock) ((window as unknown as { __agMaps?: Record<string, MlMap> }).__agMaps ??= {})[init.label] = map;
          setReady(true);
          report();
        });
      } catch {
        if (!disposed) setFailed(true);
      }
    })();
    return () => {
      disposed = true;
      clearTimeout(timer);
      mapRef.current = null;
      map?.remove();
    };
  }, []);

  // Dữ liệu lớp.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    for (const [id, fc] of Object.entries(sources)) (map.getSource(id) as GeoJSONSource | undefined)?.setData((fc ?? EMPTY) as GeoJsonData);
  }, [ready, sources]);

  // Bật/tắt lớp.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !visibility) return;
    for (const [id, on] of Object.entries(visibility)) if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", on ? "visible" : "none");
  }, [ready, visibility]);

  // Nét vẽ.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    (map.getSource("draft") as GeoJSONSource | undefined)?.setData(draftData(draft));
    map.getCanvas().style.cursor = draft ? "crosshair" : "";
  }, [ready, draft]);

  // Marker HTML (widget % dung lượng Hub — US-HUB-01).
  const markerRefs = useRef<Marker[]>([]);
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    let cancelled = false;
    void import("maplibre-gl").then((ml) => {
      if (cancelled) return;
      markerRefs.current.forEach((m) => m.remove());
      markerRefs.current = [];
      for (const mk of markers ?? []) {
        const node = document.createElement("button");
        node.type = "button";
        node.className = `map-widget ${mk.className ?? ""}`;
        node.textContent = mk.text; // textContent: không chèn HTML từ dữ liệu
        node.title = mk.title;
        node.setAttribute("aria-label", mk.title);
        node.dataset.markerId = mk.id;
        if (mk.color) node.style.setProperty("--w-color", mk.color);
        if (mk.onClick) node.addEventListener("click", (e) => {
          e.stopPropagation();
          mk.onClick?.();
        });
        markerRefs.current.push(new ml.Marker({ element: node }).setLngLat(mk.lngLat).addTo(map));
      }
    });
    return () => {
      cancelled = true;
    };
  }, [ready, markers]);
  useEffect(() => () => markerRefs.current.forEach((m) => m.remove()), []);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !focus) return;
    if (focus.bounds) map.fitBounds(focus.bounds, { padding: 40, duration: 0, maxZoom: 15 });
    else if (focus.center) map.jumpTo({ center: focus.center, zoom: focus.zoom ?? map.getZoom() });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chỉ bay khi key đổi
  }, [ready, focus?.key]);

  return (
    <div className="map" style={{ height }} role="region" aria-label={label} data-ready={ready ? "true" : "false"}>
      <div ref={el} style={{ width: "100%", height: "100%" }} />
      {failed ? (
        <div className="empty" role="status">
          Không khởi tạo được bản đồ (trình duyệt cần hỗ trợ WebGL). Dữ liệu vẫn có trong bảng bên cạnh.
        </div>
      ) : null}
    </div>
  );
}
