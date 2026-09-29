"use client";

import type { LayerSpecification } from "maplibre-gl";
import { useId, useMemo, useState, type ChangeEvent } from "react";
import { LayerMap, type AnyFeatureCollection } from "@/components/LayerMap";
import { formatInt } from "@/lib/decimal";
import { formatBytes, readUpload, type UploadFile } from "@/lib/files";
import { DrawPanel } from "./DrawPanel";
import type { DrawMode, LngLat } from "./geometry";

/** Khung toàn vùng ĐBSCL cho danh sách quản trị (API lớp bản đồ luôn cần bbox). */
export const REGION_BBOX = "104.0,8.4,107.2,11.4";

const CONTEXT_LAYERS: LayerSpecification[] = [
  { id: "ctx-fill", type: "fill", source: "context", filter: ["==", ["geometry-type"], "Polygon"], paint: { "fill-color": ["coalesce", ["get", "fill"], "#9fd8b4"], "fill-opacity": 0.4 } },
  { id: "ctx-line", type: "line", source: "context", paint: { "line-color": ["coalesce", ["get", "stroke"], "#0b4a2f"], "line-width": ["coalesce", ["get", "width"], 2] } },
  { id: "ctx-point", type: "circle", source: "context", filter: ["==", ["geometry-type"], "Point"], paint: { "circle-radius": 6, "circle-color": ["coalesce", ["get", "fill"], "#f0871a"], "circle-stroke-color": "#fff", "circle-stroke-width": 2 } },
];

/**
 * Bản đồ biên tập (GIS US-BND-02/03/05/06, US-ROAD-02/03, US-WATER-02/03): lớp ngữ cảnh + nét vẽ đang sửa.
 * Vẽ bằng sự kiện MapLibre (bấm thêm đỉnh, kéo đỉnh) — không thêm thư viện vẽ.
 */
export function EditorMap({
  label,
  context,
  drawing,
  mode,
  coords,
  onCoords,
  onPick,
  onAnyClick,
  focus,
}: {
  label: string;
  context: AnyFeatureCollection | null;
  drawing: boolean;
  mode: DrawMode;
  coords: LngLat[];
  onCoords: (c: LngLat[]) => void;
  onPick?: (props: Record<string, unknown>) => void;
  /** Mọi cú bấm bản đồ (vd đặt điểm đi/đến khi thử định tuyến). */
  onAnyClick?: (at: LngLat) => void;
  focus?: { key: string; bounds?: [LngLat, LngLat]; center?: LngLat; zoom?: number } | null;
}) {
  const sources = useMemo(() => ({ context }), [context]);
  const draft = useMemo(
    () => (drawing ? { mode, coords, onMove: (i: number, to: LngLat) => onCoords(coords.map((c, j) => (j === i ? to : c))) } : null),
    [drawing, mode, coords, onCoords],
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
        onAnyClick?.(p);
        if (!drawing) return;
        onCoords(mode === "point" ? [p] : [...coords, p]);
      }}
      onFeatureClick={(_, props) => onPick?.(props)}
    />
  );
}

/** Nét vẽ + bảng điều khiển (dùng chung các màn hình biên tập). */
export function DrawSection({ mode, coords, onCoords }: { mode: DrawMode; coords: LngLat[]; onCoords: (c: LngLat[]) => void }) {
  return (
    <div className="panel">
      <DrawPanel mode={mode} coords={coords} onChange={onCoords} />
    </div>
  );
}

/** Khung bao của một dãy toạ độ (để bay tới đối tượng đang sửa). */
export function boundsOf(coords: readonly LngLat[]): [LngLat, LngLat] | undefined {
  if (coords.length === 0) return undefined;
  const xs = coords.map((c) => c[0]);
  const ys = coords.map((c) => c[1]);
  return [
    [Math.min(...xs), Math.min(...ys)],
    [Math.max(...xs), Math.max(...ys)],
  ];
}

export function ReasonField({ value, onChange, label = "Lý do (ít nhất 10 ký tự)", min = 10 }: { value: string; onChange: (v: string) => void; label?: string; min?: number }) {
  const id = useId();
  const short = value.trim().length > 0 && value.trim().length < min;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <textarea id={id} className="textarea" value={value} onChange={(e) => onChange(e.target.value)} aria-invalid={short} style={{ minHeight: 60 }} />
      {short ? <span className="err">Lý do cần ít nhất {min} ký tự.</span> : null}
    </div>
  );
}

const FORMAT: Record<string, "geojson" | "kml" | "shapefile" | undefined> = { geojson: "geojson", kml: "kml", zip: "shapefile" };

/** Chọn tệp GeoJSON/KML/Shapefile (.zip) cho các màn hình nhập hàng loạt (GIS US-BND-07/08). */
export function SpatialFileInput({ id, onFile }: { id: string; onFile: (f: UploadFile | null, format: "geojson" | "kml" | "shapefile" | undefined) => void }) {
  const [file, setFile] = useState<UploadFile | null>(null);
  const format = file ? FORMAT[file.kind] : undefined;
  const change = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    const up = f ? await readUpload(f) : null;
    setFile(up);
    onFile(up, up ? FORMAT[up.kind] : undefined);
  };
  return (
    <div className="field">
      <label htmlFor={id}>Tệp GeoJSON / KML / Shapefile (.zip)</label>
      <input id={id} type="file" accept=".geojson,.json,.kml,.zip,application/geo+json,application/vnd.google-earth.kml+xml,application/zip" onChange={(e) => void change(e)} />
      {file ? (
        <span className="small">
          {file.name} · {formatBytes(file.size)} · {format ? `định dạng ${format === "shapefile" ? "Shapefile (.zip)" : format.toUpperCase()}` : "không nhận ra định dạng"}
        </span>
      ) : null}
      {file && !format ? <span className="err">Chỉ nhận GeoJSON, KML hoặc Shapefile đóng gói .zip.</span> : null}
    </div>
  );
}

export const IMPORT_STATUS = {
  accepted: { text: "Đã nạp", cls: "c-ok" },
  duplicate: { text: "Trùng (dùng lại)", cls: "c-mute" },
  rejected: { text: "Bị loại", cls: "c-bad" },
} as const;

/** Bảng kết quả theo đối tượng của lượt nhập tệp. */
export function ImportResult({
  title,
  result,
  extra,
}: {
  title: string;
  result: { format: string; accepted: number; duplicate: number; rejected: number; features: { index: number; name: string | null; status: keyof typeof IMPORT_STATUS; code: string | null; reason: string | null }[] };
  extra?: (f: { index: number }) => string;
}) {
  return (
    <section className="card sec" style={{ marginTop: 16 }} aria-label={title}>
      <div className="sec-h">
        <h2>
          {title} ({result.format})
        </h2>
        <span className="small">
          <span className="chip c-ok">{formatInt(result.accepted)} đã nạp</span> <span className="chip c-mute">{formatInt(result.duplicate)} trùng</span>{" "}
          <span className="chip c-bad">{formatInt(result.rejected)} bị loại</span>
        </span>
      </div>
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">{title}</caption>
          <thead>
            <tr>
              <th scope="col" className="num">#</th>
              <th scope="col">Tên</th>
              <th scope="col">Kết quả</th>
              {extra ? <th scope="col">Bản ghi</th> : null}
              <th scope="col">Lý do</th>
            </tr>
          </thead>
          <tbody>
            {result.features.map((f) => (
              <tr key={f.index}>
                <td className="num">{f.index + 1}</td>
                <td>{f.name ?? "—"}</td>
                <td>
                  <span className={`chip ${IMPORT_STATUS[f.status].cls}`}>{IMPORT_STATUS[f.status].text}</span>
                </td>
                {extra ? <td className="small">{extra(f)}</td> : null}
                <td className="small">
                  {f.reason ?? "—"}
                  {f.code ? (
                    <div className="mute">
                      <code>{f.code}</code>
                    </div>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

/** Gửi nguyên văn nội dung tệp qua openapi-fetch (JSON/KML là chuỗi, Shapefile .zip là byte). */
export function rawBody(file: UploadFile) {
  return {
    body: file.body as unknown as Record<string, never>,
    bodySerializer: (b: unknown) => b as BodyInit,
    headers: { "Content-Type": file.contentType },
  };
}
