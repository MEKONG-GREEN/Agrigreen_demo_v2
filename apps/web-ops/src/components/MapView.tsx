"use client";

import type { GeoJSONSource, Map as MlMap, StyleSpecification } from "maplibre-gl";
import { useEffect, useRef, useState } from "react";
import type { FeatureCollection, MultiPolygonGeometry, PointGeometry, PolygonGeometry, Position } from "@/lib/geojson";
import { config } from "@/config";

type GeoJsonData = Parameters<GeoJSONSource["setData"]>[0];

export type AreaCollection = FeatureCollection<PolygonGeometry | MultiPolygonGeometry, { code: string; name: string; fill?: string; [k: string]: unknown }>;
export type PointCollection = FeatureCollection<PointGeometry, { id: string; name: string; [k: string]: unknown }>;

/** Nền trơn, không gọi tile bên ngoài (ADR-0007). Có NEXT_PUBLIC_MAP_STYLE_URL thì dùng style đó. */
const BLANK_STYLE: StyleSpecification = {
  version: 8,
  sources: {},
  layers: [{ id: "bg", type: "background", paint: { "background-color": "#e3eef2" } }],
};

function bbox(fc: AreaCollection | null | undefined): [[number, number], [number, number]] | null {
  if (!fc || fc.features.length === 0) return null;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const visit = ([x = 0, y = 0]: Position) => {
    minX = Math.min(minX, x); maxX = Math.max(maxX, x);
    minY = Math.min(minY, y); maxY = Math.max(maxY, y);
  };
  for (const f of fc.features) {
    if (f.geometry.type === "Polygon") f.geometry.coordinates.forEach((r) => r.forEach(visit));
    else f.geometry.coordinates.forEach((poly) => poly.forEach((r) => r.forEach(visit)));
  }
  return [[minX, minY], [maxX, maxY]];
}

function centroid(f: AreaCollection["features"][number]): Position {
  const ring = f.geometry.type === "Polygon" ? f.geometry.coordinates[0] : f.geometry.coordinates[0]?.[0];
  if (!ring || ring.length === 0) return [0, 0];
  const pts = ring.slice(0, -1);
  return [pts.reduce((s, p) => s + (p[0] ?? 0), 0) / pts.length, pts.reduce((s, p) => s + (p[1] ?? 0), 0) / pts.length];
}

export function MapView({
  areas,
  points,
  label,
  onAreaClick,
  showAreaLabels = true,
  describeAreas = false,
  height,
}: {
  areas?: AreaCollection | null;
  points?: PointCollection | null;
  label: string;
  onAreaClick?: (code: string) => void;
  showAreaLabels?: boolean;
  /** Kèm danh sách vùng cho trình đọc màn hình: tên, `status_label` và màu tô (data-fill). */
  describeAreas?: boolean;
  height?: number | string;
}) {
  const el = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MlMap | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const clickRef = useRef(onAreaClick);
  useEffect(() => {
    clickRef.current = onAreaClick;
  });

  // Khởi tạo bản đồ một lần (maplibre chỉ chạy trên trình duyệt có WebGL).
  useEffect(() => {
    let disposed = false;
    let map: MlMap | null = null;
    (async () => {
      try {
        const ml = await import("maplibre-gl");
        if (disposed || !el.current) return;
        // Worker ESM được chép vào public/maplibre/ (scripts/copy-maplibre-worker.mjs).
        ml.setWorkerUrl("/maplibre/maplibre-gl-worker.mjs");
        map = new ml.Map({
          container: el.current,
          style: config.mapStyleUrl || BLANK_STYLE,
          center: [105.8, 10.1],
          zoom: 7,
          attributionControl: false,
          cooperativeGestures: false,
        });
        map.addControl(new ml.NavigationControl({ showCompass: false }), "top-right");
        map.addControl(new ml.AttributionControl({ compact: true, customAttribution: "Ranh giới: AgriGreen (minh hoạ khi giả lập)" }));
        map.on("load", () => {
          if (!map) return;
          map.addSource("areas", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addSource("points", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
          map.addLayer({ id: "areas-fill", type: "fill", source: "areas", paint: { "fill-color": ["coalesce", ["get", "fill"], "#9fd8b4"], "fill-opacity": 0.72 } });
          map.addLayer({ id: "areas-line", type: "line", source: "areas", paint: { "line-color": "#0b4a2f", "line-width": 1.4 } });
          map.addLayer({
            id: "points",
            type: "circle",
            source: "points",
            paint: { "circle-radius": 5.5, "circle-color": "#f0871a", "circle-stroke-color": "#fff", "circle-stroke-width": 1.6 },
          });
          map.on("click", "areas-fill", (e) => {
            const code = e.features?.[0]?.properties?.code;
            if (typeof code === "string") clickRef.current?.(code);
          });
          map.on("click", "points", (e) => {
            const f = e.features?.[0];
            if (!f || !map) return;
            const name = String(f.properties?.name ?? "");
            const node = document.createElement("div");
            node.textContent = name; // textContent: không chèn HTML từ dữ liệu
            new ml.Popup({ closeButton: true }).setLngLat(e.lngLat).setDOMContent(node).addTo(map);
          });
          for (const id of ["areas-fill", "points"]) {
            map.on("mouseenter", id, () => map && (map.getCanvas().style.cursor = "pointer"));
            map.on("mouseleave", id, () => map && (map.getCanvas().style.cursor = ""));
          }
          mapRef.current = map;
          setReady(true);
        });
      } catch {
        if (!disposed) setFailed(true);
      }
    })();
    return () => {
      disposed = true;
      mapRef.current = null;
      map?.remove();
    };
  }, []);

  // Cập nhật dữ liệu lớp.
  const labelMarkers = useRef<{ remove: () => void }[]>([]);
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    (map.getSource("areas") as GeoJSONSource | undefined)?.setData((areas ?? { type: "FeatureCollection", features: [] }) as unknown as GeoJsonData);
    (map.getSource("points") as GeoJSONSource | undefined)?.setData((points ?? { type: "FeatureCollection", features: [] }) as unknown as GeoJsonData);
    const b = bbox(areas);
    if (b) map.fitBounds(b, { padding: 32, duration: 0 });

    labelMarkers.current.forEach((m) => m.remove());
    labelMarkers.current = [];
    if (showAreaLabels && areas) {
      void import("maplibre-gl").then((ml) => {
        for (const f of areas.features) {
          const node = document.createElement("div");
          node.className = "map-label";
          node.textContent = f.properties.name;
          node.style.cssText = "font:600 11.5px/1.2 system-ui,sans-serif;color:#0b4a2f;background:rgba(255,255,255,.85);padding:2px 6px;border-radius:6px;pointer-events:none;white-space:nowrap";
          labelMarkers.current.push(new ml.Marker({ element: node }).setLngLat(centroid(f) as [number, number]).addTo(map));
        }
      });
    }
  }, [ready, areas, points, showAreaLabels]);

  return (
    <div className="map" style={height ? { height } : { height: "100%" }} role="region" aria-label={label}>
      <div ref={el} style={{ width: "100%", height: "100%" }} />
      {failed ? (
        <div className="empty" role="status">
          Không khởi tạo được bản đồ (trình duyệt cần hỗ trợ WebGL). Dữ liệu vẫn có trong bảng bên cạnh.
        </div>
      ) : null}
      {describeAreas && areas && areas.features.length > 0 ? (
        <ul className="sr-only" aria-label={`${label}: danh sách vùng`} data-testid="map-areas">
          {areas.features.map((f) => (
            <li key={f.properties.code} data-code={f.properties.code} data-fill={f.properties.fill ?? ""}>
              {f.properties.name}
              {typeof f.properties.status_label === "string" ? `: ${f.properties.status_label}` : ""}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
