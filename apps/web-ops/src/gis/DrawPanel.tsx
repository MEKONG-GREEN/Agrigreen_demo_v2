"use client";

import { useId, useState } from "react";
import { formatAreaM2, formatLength, lineLengthM, parseLngLat, polygonAreaM2, validateDraft, type DrawMode, type LngLat } from "./geometry";

const MODE_HINT: Record<DrawMode, string> = {
  point: "Bấm lên bản đồ để đặt điểm (bấm lại để dời).",
  line: "Bấm lần lượt các điểm của tuyến; kéo đỉnh để chỉnh hình.",
  polygon: "Bấm lần lượt các đỉnh của vùng (tự khép vòng); kéo đỉnh để chỉnh hình.",
};

/** Số đo trắc địa của nét vẽ để hiển thị (chiều dài tuyến, diện tích + chu vi vùng). */
export function measureDraft(mode: DrawMode, coords: readonly LngLat[]): string | null {
  if (mode === "line" && coords.length >= 2) return `Chiều dài: ${formatLength(lineLengthM(coords))}`;
  if (mode === "polygon" && coords.length >= 3) {
    const ring = [...coords, coords[0]!];
    return `Diện tích: ${formatAreaM2(polygonAreaM2([ring]))} · Chu vi: ${formatLength(lineLengthM(ring))}`;
  }
  return null;
}

/**
 * Bảng điều khiển nét vẽ: danh sách đỉnh (xoá từng đỉnh), thêm đỉnh bằng toạ độ (thay cho bấm bản đồ — bàn phím/trình đọc
 * màn hình), số đo trắc địa trực tiếp (GIS BR-41: chiều dài tự tính theo hình vẽ, không nhập tay).
 */
export function DrawPanel({ mode, coords, onChange }: { mode: DrawMode; coords: LngLat[]; onChange: (c: LngLat[]) => void }) {
  const id = useId();
  const [text, setText] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const measure = measureDraft(mode, coords);
  const invalid = coords.length > 0 ? validateDraft(mode, coords) : null;

  const add = () => {
    const p = parseLngLat(text);
    if (!p) {
      setErr("Nhập dạng “kinh độ, vĩ độ”, vd 105.78, 10.03");
      return;
    }
    setErr(null);
    setText("");
    onChange(mode === "point" ? [p] : [...coords, p]);
  };

  return (
    <div className="stack" data-testid="draw-panel">
      <p className="small mute">{MODE_HINT[mode]}</p>
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
              <button type="button" className="btn btn-o btn-sm" style={{ padding: "0 6px" }} onClick={() => onChange(coords.filter((_, j) => j !== i))} aria-label={`Xoá đỉnh ${i + 1}`}>
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
          <button type="button" className="btn btn-o btn-sm" onClick={() => onChange([])}>
            Xoá hết điểm
          </button>
        ) : null}
      </div>
      {invalid ? <span className="small c-warn">{invalid}</span> : null}
    </div>
  );
}
