"use client";

// Bản đồ vẽ thửa của KN (FN-XA-03.05, TT-11 #104): lớp thửa khác của xã + nét đa giác đang vẽ, trên `@/components/LayerMap`
// dùng chung (như bản đồ CNTS `kn/cnts/map`). KN không import thư viện biên tập của ứng dụng GIS (`@/gis/*`) — knboundary
// web-kn-import-app.
import type { LayerSpecification } from "maplibre-gl";
import { useId, useMemo, useState } from "react";
import { LayerMap, type AnyFeatureCollection } from "@/components/LayerMap";
import { measurePolygon, parseLngLat, validatePolygon, type LngLat } from "../plotGeometry";

const CONTEXT_LAYERS: LayerSpecification[] = [
  { id: "ctx-fill", type: "fill", source: "context", filter: ["==", ["geometry-type"], "Polygon"], paint: { "fill-color": ["coalesce", ["get", "fill"], "#9fd8b4"], "fill-opacity": 0.4 } },
  { id: "ctx-line", type: "line", source: "context", paint: { "line-color": ["coalesce", ["get", "stroke"], "#0b4a2f"], "line-width": ["coalesce", ["get", "width"], 2] } },
];

/** Bản đồ thửa: bấm thêm đỉnh, kéo đỉnh để chỉnh (khi `drawing`). */
export function PlotEditorMap({
  label,
  context,
  drawing,
  coords,
  onCoords,
  focus,
}: {
  label: string;
  context: AnyFeatureCollection | null;
  drawing: boolean;
  coords: LngLat[];
  onCoords: (c: LngLat[]) => void;
  focus?: { key: string; bounds?: [LngLat, LngLat] } | null;
}) {
  const sources = useMemo(() => ({ context }), [context]);
  const draft = useMemo(
    () => (drawing ? { mode: "polygon" as const, coords, onMove: (i: number, to: LngLat) => onCoords(coords.map((c, j) => (j === i ? to : c))) } : null),
    [drawing, coords, onCoords],
  );
  return (
    <LayerMap
      label={label}
      sources={sources}
      layers={CONTEXT_LAYERS}
      draft={draft}
      focus={focus}
      height={460}
      initial={{ center: [105.75, 10.2], zoom: 8.3 }}
      onMapClick={(p) => {
        if (drawing) onCoords([...coords, p]);
      }}
    />
  );
}

/**
 * Bảng điều khiển nét vẽ thửa: danh sách đỉnh (xoá từng đỉnh), thêm đỉnh bằng toạ độ (thay cho bấm bản đồ — bàn phím/trình
 * đọc màn hình), diện tích và chu vi tự tính.
 */
export function PlotDrawPanel({ coords, onCoords }: { coords: LngLat[]; onCoords: (c: LngLat[]) => void }) {
  const id = useId();
  const [text, setText] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const measure = measurePolygon(coords);
  const invalid = coords.length > 0 ? validatePolygon(coords) : null;
  const add = () => {
    const p = parseLngLat(text);
    if (!p) {
      setErr("Nhập dạng “kinh độ, vĩ độ”, vd 105.78, 10.03");
      return;
    }
    setErr(null);
    setText("");
    onCoords([...coords, p]);
  };
  return (
    <div className="panel">
      <div className="stack" data-testid="draw-panel">
        <p className="small mute">Bấm lần lượt các đỉnh của thửa (tự khép vòng); kéo đỉnh để chỉnh hình.</p>
        <div className="row" style={{ alignItems: "flex-end" }}>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor={`${id}-pt`}>Thêm điểm theo toạ độ (kinh độ, vĩ độ)</label>
            <input
              id={`${id}-pt`}
              className="input"
              value={text}
              placeholder="105.78, 10.03"
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  add();
                }
              }}
            />
          </div>
          <button type="button" className="btn btn-o btn-sm" onClick={add}>
            Thêm điểm
          </button>
        </div>
        {err ? <span className="err">{err}</span> : null}
        {coords.length > 0 ? (
          <ol className="small" aria-label="Các đỉnh đã vẽ" style={{ margin: "0 0 0 18px", maxHeight: 140, overflow: "auto" }}>
            {coords.map((c, i) => (
              <li key={`${i}-${c[0]}-${c[1]}`}>
                {c[0].toFixed(6)}, {c[1].toFixed(6)}{" "}
                <button type="button" className="btn btn-o btn-sm" style={{ padding: "0 6px" }} onClick={() => onCoords(coords.filter((_, j) => j !== i))} aria-label={`Xoá đỉnh ${i + 1}`}>
                  ×
                </button>
              </li>
            ))}
          </ol>
        ) : null}
        <div className="row">
          {measure ? (
            <b data-testid="draw-measure" aria-live="polite">
              {measure}
            </b>
          ) : (
            <span className="small mute">Chưa đủ điểm để đo.</span>
          )}
          {coords.length > 0 ? (
            <button type="button" className="btn btn-o btn-sm" onClick={() => onCoords([])}>
              Xoá hết điểm
            </button>
          ) : null}
        </div>
        {invalid ? <span className="small c-warn">{invalid}</span> : null}
      </div>
    </div>
  );
}
