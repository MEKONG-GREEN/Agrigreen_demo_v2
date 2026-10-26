"use client";

// Bản đồ vùng nuôi, vùng dịch (W11.52 — BRD CNTS v3.7 FN-18, UR-TINH-17; prototype A "map-zones"):
// - Lớp: vùng chăn nuôi/thủy sản, vùng dịch, vùng bị uy hiếp, vùng đệm, hướng di chuyển; ổ dịch BM-10 (W11.49); điểm
//   trình diễn, hộ mô hình có tọa độ (W11.47); thống kê thửa HTX theo xã (qua công bố của HTX). Bật/tắt lớp, bấm đối tượng.
// - Thanh trượt ngày: chọn ngày cũ thấy vùng đã lưu của ngày đó (BR-01, AC-03).
// - Cán bộ phụ trách dịch Cấp Tỉnh: vẽ đa giác, theo bán kính, hướng di chuyển; khoanh vùng 500 m/3 km/10 km từ ổ dịch;
//   sửa (phiên bản mới có lý do), ngưng/khôi phục; tải GeoJSON lên/xuống. Cấp Xã chỉ xem xã mình (AC-02).
// Nền bản đồ: LayerMap dùng chung (NEXT_PUBLIC_MAP_STYLE_URL trong lãnh thổ, mặc định nền trơn — RB-7).
import type { LayerSpecification } from "maplibre-gl";
import { useMemo, useState, type ChangeEvent } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { LayerMap, type AnyFeatureCollection } from "@/components/LayerMap";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  DEFAULT_RADII,
  EPIDEMIC,
  KINDS,
  KIND_COLOR,
  KIND_LABEL,
  OUTBREAK_STATUS_LABEL,
  WARNING_LABEL,
  addDays,
  circlePolygon,
  createCntsMapApi,
  dmy,
  draftToGeometry,
  geometryFromGeoJSON,
  geometryToDraft,
  zoneErrors,
  zonesGeoJSON,
  type CntsMapApi,
  type LngLat,
  type MapLayers,
  type Zone,
  type ZoneKind,
  type ZoneSave,
} from "./api";

const LAYERS: LayerSpecification[] = [
  { id: "zone-fill", type: "fill", source: "zones", filter: ["==", ["geometry-type"], "Polygon"], paint: { "fill-color": ["get", "color"], "fill-opacity": 0.22 } },
  { id: "zone-line", type: "line", source: "zones", paint: { "line-color": ["get", "color"], "line-width": ["case", ["==", ["get", "kind"], "huong_di_chuyen"], 4, 2] } },
  { id: "preview-fill", type: "fill", source: "preview", paint: { "fill-color": "#1976d2", "fill-opacity": 0.15 } },
  { id: "preview-line", type: "line", source: "preview", paint: { "line-color": "#1565c0", "line-width": 2, "line-dasharray": [2, 1] } },
  { id: "sites", type: "circle", source: "sites", paint: { "circle-radius": 6, "circle-color": "#0f766e", "circle-stroke-color": "#fff", "circle-stroke-width": 1.5 } },
  { id: "households", type: "circle", source: "households", paint: { "circle-radius": 4, "circle-color": "#2563eb", "circle-stroke-color": "#fff", "circle-stroke-width": 1 } },
  {
    id: "outbreaks",
    type: "circle",
    source: "outbreaks",
    paint: { "circle-radius": 8, "circle-color": ["case", ["==", ["get", "status"], "closed"], "#9ca3af", "#b91c1c"], "circle-stroke-color": "#fff", "circle-stroke-width": 2 },
  },
];

const TOGGLES: { key: string; label: string; layers: string[] }[] = [
  { key: "zones", label: "Vùng", layers: ["zone-fill", "zone-line"] },
  { key: "outbreaks", label: "Ổ dịch", layers: ["outbreaks"] },
  { key: "sites", label: "Điểm trình diễn", layers: ["sites"] },
  { key: "households", label: "Hộ mô hình", layers: ["households"] },
];

const today = () => new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10);

function fc(features: AnyFeatureCollection["features"]): AnyFeatureCollection {
  return { type: "FeatureCollection", features };
}

function download(name: string, body: unknown) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(body, null, 2)], { type: "application/geo+json" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

type Draw = { mode: "polygon" | "radius" | "line"; coords: LngLat[]; center: LngLat | null; radius?: number };

export function MapScreen() {
  const { api, kn } = useAuth();
  const mapi = useMemo(() => (api ? createCntsMapApi(api) : null), [api]);
  const [day, setDay] = useState(today());
  const layers = useLoad(mapi ? () => mapi.layers(day) : null, [mapi, day]);
  if (!mapi) return null;
  const role = kn.context?.role;
  return (
    <div className="page">
      <PageHeader title="Bản đồ vùng nuôi, vùng dịch">
        <p className="mute">
          {role === "kn_xa"
            ? "Vùng nuôi, vùng dịch, ổ dịch và hộ mô hình của xã phụ trách (chỉ xem)."
            : "Vùng chăn nuôi, nuôi thủy sản, vùng dịch và vùng đệm theo ngày; ổ dịch đã xác nhận ở Báo cáo dịch bệnh hiện ngay trên bản đồ."}
        </p>
      </PageHeader>
      <DayBar day={day} setDay={setDay} />
      <LoadState s={layers}>{(d) => <MapBody mapi={mapi} d={d} day={day} reload={layers.reload} />}</LoadState>
    </div>
  );
}

function DayBar({ day, setDay }: { day: string; setDay: (d: string) => void }) {
  const end = today();
  const span = 180;
  const offset = Math.max(0, Math.round((Date.parse(end) - Date.parse(day)) / 86_400_000));
  return (
    <div className="row" style={{ gap: 12, alignItems: "center", flexWrap: "wrap" }}>
      <label>
        Ngày xem{" "}
        <input type="date" value={day} max={end} onChange={(e) => e.target.value && setDay(e.target.value)} aria-label="Ngày xem bản đồ" />
      </label>
      <input
        type="range"
        min={0}
        max={span}
        value={span - Math.min(offset, span)}
        onChange={(e) => setDay(addDays(end, Number(e.target.value) - span))}
        aria-label="Thanh trượt ngày (180 ngày gần nhất)"
        style={{ flex: "1 1 240px" }}
      />
      <button className="btn" onClick={() => setDay(end)} disabled={day === end}>
        Hôm nay
      </button>
    </div>
  );
}

function MapBody({ mapi, d, day, reload }: { mapi: CntsMapApi; d: MapLayers; day: string; reload: () => void }) {
  const { kn } = useAuth();
  const communeName = useCommuneNames([...d.zones.flatMap((z) => z.communes), ...d.outbreaks.map((o) => o.commune_code), ...d.places.map((p) => p.commune_code)]);
  const [show, setShow] = useState<Record<string, boolean>>({ zones: true, outbreaks: true, sites: true, households: true });
  const [kinds, setKinds] = useState<ZoneKind[]>([...KINDS]);
  const [selected, setSelected] = useState<{ type: "zone" | "outbreak" | "place"; ref: string } | null>(null);
  const [editing, setEditing] = useState<Zone | "new" | null>(null);
  const [draw, setDraw] = useState<Draw | null>(null);
  const zones = d.zones.filter((z) => kinds.includes(z.kind));
  const visibility = Object.fromEntries(TOGGLES.flatMap((t) => t.layers.map((l) => [l, !!show[t.key]])));
  const sources = useMemo(
    () => ({
      zones: fc(zones.map((z) => ({ type: "Feature", id: z.ref, geometry: z.geometry, properties: { ref: z.ref, kind: z.kind, color: KIND_COLOR[z.kind], name: z.name } }))),
      outbreaks: fc(d.outbreaks.map((o) => ({ type: "Feature", geometry: { type: "Point", coordinates: [o.lng, o.lat] }, properties: { ref: o.ref, status: o.status } }))),
      sites: fc(d.places.filter((p) => p.kind === "site").map((p) => ({ type: "Feature", geometry: { type: "Point", coordinates: [p.lng, p.lat] }, properties: { ref: p.ref } }))),
      households: fc(d.places.filter((p) => p.kind === "household").map((p) => ({ type: "Feature", geometry: { type: "Point", coordinates: [p.lng, p.lat] }, properties: { ref: p.ref } }))),
      preview: fc(draw?.mode === "radius" && draw.center ? [{ type: "Feature", geometry: circlePolygon(draw.center, draw.radius ?? 3000), properties: {} }] : []),
    }),
    [zones, d.outbreaks, d.places, draw],
  );
  const knownCommunes = [...new Set([...(kn.context?.communes ?? []).map((c) => c.code), ...d.outbreaks.map((o) => o.commune_code), ...d.places.map((p) => p.commune_code), ...d.zones.flatMap((z) => z.communes)])].sort();
  const focus = useMemo(() => {
    const pts: LngLat[] = [...d.outbreaks.map((o) => [o.lng, o.lat] as LngLat), ...d.places.map((p) => [p.lng, p.lat] as LngLat)];
    if (!pts.length) return null;
    const xs = pts.map((p) => p[0]), ys = pts.map((p) => p[1]);
    return { key: "init", bounds: [[Math.min(...xs) - 0.05, Math.min(...ys) - 0.05], [Math.max(...xs) + 0.05, Math.max(...ys) + 0.05]] as [LngLat, LngLat] };
  }, [d.outbreaks, d.places]);
  const onClick = (at: LngLat) => {
    if (!draw) return;
    if (draw.mode === "radius") setDraw({ ...draw, center: at, radius: draw.radius ?? 3000 });
    else setDraw({ ...draw, coords: [...draw.coords, at] });
  };
  return (
    <div className="gis-grid" style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) 360px", gap: 12 }}>
      <div>
        <div className="row" style={{ gap: 10, flexWrap: "wrap", marginBottom: 8 }} role="group" aria-label="Lớp bản đồ">
          {TOGGLES.map((t) => (
            <label key={t.key} className="check">
              <input type="checkbox" checked={!!show[t.key]} onChange={() => setShow({ ...show, [t.key]: !show[t.key] })} /> {t.label}
            </label>
          ))}
          <span className="mute">|</span>
          {KINDS.map((k) => (
            <label key={k} className="check" style={{ color: KIND_COLOR[k] }}>
              <input type="checkbox" checked={kinds.includes(k)} onChange={() => setKinds(kinds.includes(k) ? kinds.filter((x) => x !== k) : [...kinds, k])} /> {KIND_LABEL[k]}
            </label>
          ))}
        </div>
        <LayerMap
          label="Bản đồ vùng nuôi, vùng dịch CNTS"
          sources={sources}
          layers={LAYERS}
          visibility={visibility}
          onMapClick={onClick}
          onFeatureClick={(layer, p) => {
            const ref = String(p.ref ?? "");
            if (!ref) return;
            setSelected({ type: layer.startsWith("zone") ? "zone" : layer === "outbreaks" ? "outbreak" : "place", ref });
          }}
          draft={draw && draw.mode !== "radius" ? { mode: draw.mode, coords: draw.coords, onMove: (i, to) => setDraw({ ...draw, coords: draw.coords.map((c, j) => (j === i ? to : c)) }) } : null}
          focus={focus}
          height="calc(100vh - 260px)"
        />
        {d.plots_ready && d.plot_stats.length > 0 ? (
          <details style={{ marginTop: 8 }}>
            <summary>Thửa HTX đã số hóa theo xã (dữ liệu công bố của HTX)</summary>
            <table className="table">
              <thead>
                <tr>
                  <th>Xã</th>
                  <th>Số thửa</th>
                  <th>Diện tích (ha)</th>
                </tr>
              </thead>
              <tbody>
                {d.plot_stats.map((p) => (
                  <tr key={p.commune_code}>
                    <td>{communeName(p.commune_code)}</td>
                    <td>{p.plots}</td>
                    <td>{p.area_ha}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </details>
        ) : null}
      </div>
      <aside className="card" style={{ padding: 12, overflow: "auto", maxHeight: "calc(100vh - 220px)" }}>
        {editing ? (
          <ZoneEditor
            mapi={mapi}
            zone={editing === "new" ? null : editing}
            draw={draw}
            setDraw={setDraw}
            communes={knownCommunes}
            communeName={communeName}
            onDone={() => {
              setEditing(null);
              setDraw(null);
              reload();
            }}
          />
        ) : selected?.type === "zone" ? (
          <ZonePanel mapi={mapi} refId={selected.ref} day={day} communeName={communeName} onClose={() => setSelected(null)} onEdit={(z) => (setEditing(z), setDraw({ mode: z.draw_mode, coords: geometryToDraft(z.geometry), center: z.center ? [z.center.lng, z.center.lat] : null, radius: z.radius_m ?? undefined }))} reload={reload} />
        ) : selected?.type === "outbreak" ? (
          <OutbreakPanel mapi={mapi} d={d} refId={selected.ref} canDraw={d.can_draw} communeName={communeName} onClose={() => setSelected(null)} reload={reload} />
        ) : selected?.type === "place" ? (
          <PlacePanel d={d} refId={selected.ref} communeName={communeName} onClose={() => setSelected(null)} />
        ) : (
          <ZoneList d={d} zones={zones} communeName={communeName} onPick={(ref) => setSelected({ type: "zone", ref })} onNew={() => (setEditing("new"), setDraw({ mode: "polygon", coords: [], center: null }))} />
        )}
      </aside>
    </div>
  );
}

function ZoneList({ d, zones, communeName, onPick, onNew }: { d: MapLayers; zones: Zone[]; communeName: (c: string) => string; onPick: (ref: string) => void; onNew: () => void }) {
  return (
    <>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <h3 style={{ margin: 0 }}>Vùng ngày {dmy(d.date)}</h3>
        {d.can_draw ? (
          <button className="btn btn-primary" onClick={onNew}>
            Vẽ vùng
          </button>
        ) : null}
      </div>
      <p className="mute">
        {d.outbreaks.length} ổ dịch · {d.places.filter((p) => p.kind === "site").length} điểm trình diễn · {d.places.filter((p) => p.kind === "household").length} hộ mô hình
      </p>
      <div className="row" style={{ gap: 8 }}>
        <button className="btn" onClick={() => download(`vung-cnts-${d.date}.geojson`, zonesGeoJSON(zones))} disabled={!zones.length}>
          Tải GeoJSON
        </button>
      </div>
      {zones.length === 0 ? <p className="mute">Chưa có vùng ở ngày này.</p> : null}
      <ul className="list" style={{ listStyle: "none", padding: 0 }}>
        {zones.map((z) => (
          <li key={z.ref} style={{ borderLeft: `4px solid ${KIND_COLOR[z.kind]}`, paddingLeft: 8, margin: "8px 0" }}>
            <button className="link" onClick={() => onPick(z.ref)}>
              {z.name}
            </button>
            <div className="mute">
              {KIND_LABEL[z.kind]} · {z.communes.map(communeName).join(", ")}
              {z.radius_m ? ` · bán kính ${z.radius_m.toLocaleString("vi-VN")} m` : ""}
              {z.area_ha ? ` · ${Number(z.area_ha).toLocaleString("vi-VN")} ha` : ""} · từ {dmy(z.effective_on)} (bản {z.version})
            </div>
          </li>
        ))}
      </ul>
    </>
  );
}

function ZonePanel({ mapi, refId, day, communeName, onClose, onEdit, reload }: { mapi: CntsMapApi; refId: string; day: string; communeName: (c: string) => string; onClose: () => void; onEdit: (z: Zone) => void; reload: () => void }) {
  const z = useLoad(() => mapi.zone(refId), [mapi, refId]);
  const inside = useLoad(() => mapi.contents(refId, day), [mapi, refId, day]);
  const act = useAction();
  const [reason, setReason] = useState("");
  return (
    <LoadState s={z}>
      {(zone) => (
        <>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <h3 style={{ margin: 0, color: KIND_COLOR[zone.kind] }}>{zone.name}</h3>
            <button className="btn" onClick={onClose} aria-label="Đóng">
              ✕
            </button>
          </div>
          <p>
            {KIND_LABEL[zone.kind]} {zone.status === "retired" ? <StatusPill tone="neutral">Đã ngưng</StatusPill> : null}
            <br />
            Xã: {zone.communes.map(communeName).join(", ")}
            {zone.radius_m ? <><br />Bán kính {zone.radius_m.toLocaleString("vi-VN")} m</> : null}
            {zone.area_ha ? <><br />Diện tích {Number(zone.area_ha).toLocaleString("vi-VN")} ha</> : null}
            <br />Áp dụng từ {dmy(zone.effective_on)} · bản {zone.version}
          </p>
          {act.view}
          <LoadState s={inside}>
            {(c) => (
              <div>
                <strong>Trong vùng ngày {dmy(c.date)}:</strong> {c.site_count} điểm trình diễn, {c.household_count} hộ mô hình, {c.outbreak_count} ổ dịch
                <ul>
                  {c.places.slice(0, 50).map((p) => (
                    <li key={p.ref}>
                      {p.kind === "site" ? "Điểm" : "Hộ"} · {p.name} · {communeName(p.commune_code)}
                      {p.distance_m != null ? ` · cách tâm ${Math.round(p.distance_m).toLocaleString("vi-VN")} m` : ""}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </LoadState>
          {zone.can_edit ? (
            <div style={{ marginTop: 8 }}>
              {zone.status === "active" ? (
                <button className="btn" onClick={() => onEdit(zone)}>
                  Sửa hình, thông tin
                </button>
              ) : null}
              <label style={{ display: "block", marginTop: 8 }}>
                Lý do {zone.status === "active" ? "ngưng hiển thị" : "khôi phục"}
                <input value={reason} onChange={(e) => setReason(e.target.value)} />
              </label>
              <button
                className="btn"
                disabled={act.busy || reason.trim().length < 5}
                onClick={async () => {
                  if (await act.run(() => mapi.zoneStatus(zone.ref, zone.status === "active", zone.version, reason.trim()))) {
                    setReason("");
                    z.reload();
                    reload();
                  }
                }}
              >
                {zone.status === "active" ? "Ngưng hiển thị" : "Khôi phục"}
              </button>
            </div>
          ) : null}
          <details style={{ marginTop: 8 }}>
            <summary>Lịch sử phiên bản ({zone.versions.length})</summary>
            <ol>
              {zone.versions.map((v) => (
                <li key={v.version}>
                  Bản {v.version} · {v.event} · từ {dmy(v.effective_on)} · {v.actor_display ?? v.updated_by.slice(0, 8)}
                  {v.reason ? ` — ${v.reason}` : ""}
                </li>
              ))}
            </ol>
          </details>
        </>
      )}
    </LoadState>
  );
}

function OutbreakPanel({ mapi, d, refId, canDraw, communeName, onClose, reload }: { mapi: CntsMapApi; d: MapLayers; refId: string; canDraw: boolean; communeName: (c: string) => string; onClose: () => void; reload: () => void }) {
  const o = d.outbreaks.find((x) => x.ref === refId);
  const act = useAction();
  const [radii, setRadii] = useState<Record<"vung_dich" | "vung_uy_hiep" | "vung_dem", number>>({ ...DEFAULT_RADII });
  if (!o) return null;
  const linked = d.zones.filter((z) => z.outbreak_ref === o.ref);
  return (
    <>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h3 style={{ margin: 0 }}>Ổ dịch {o.disease_name ?? o.disease_code}</h3>
        <button className="btn" onClick={onClose} aria-label="Đóng">
          ✕
        </button>
      </div>
      <p>
        {communeName(o.commune_code)} · {o.species_name ?? o.species_code} · {OUTBREAK_STATUS_LABEL[o.status]}
        <br />
        Diễn biến ngày {dmy(o.as_of)}: đàn {o.herd_size}, chết {o.dead}
      </p>
      {linked.length ? <p className="mute">Đã khoanh: {linked.map((z) => KIND_LABEL[z.kind]).join(", ")}</p> : null}
      {act.view}
      {canDraw && !linked.length ? (
        <div>
          <strong>Khoanh vùng theo bán kính (m)</strong>
          {(Object.keys(DEFAULT_RADII) as (keyof typeof DEFAULT_RADII)[]).map((k) => (
            <label key={k} style={{ display: "block" }}>
              {KIND_LABEL[k]}
              <input type="number" min={10} max={50000} value={radii[k]} onChange={(e) => setRadii({ ...radii, [k]: Number(e.target.value) })} />
            </label>
          ))}
          <button
            className="btn btn-primary"
            disabled={act.busy || !(radii.vung_dich < radii.vung_uy_hiep && radii.vung_uy_hiep < radii.vung_dem)}
            onClick={async () => {
              if (await act.run(() => mapi.fromOutbreak(o.ref, radii), "Đã khoanh vùng dịch, vùng bị uy hiếp, vùng đệm")) reload();
            }}
          >
            Khoanh vùng
          </button>
        </div>
      ) : null}
    </>
  );
}

function PlacePanel({ d, refId, communeName, onClose }: { d: MapLayers; refId: string; communeName: (c: string) => string; onClose: () => void }) {
  const p = d.places.find((x) => x.ref === refId);
  if (!p) return null;
  return (
    <>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h3 style={{ margin: 0 }}>{p.kind === "site" ? "Điểm trình diễn" : "Hộ mô hình"}</h3>
        <button className="btn" onClick={onClose} aria-label="Đóng">
          ✕
        </button>
      </div>
      <p>
        {p.name} · {communeName(p.commune_code)}
        <br />
        {p.model_type_name ?? p.model_type_code}
        {p.scale ? ` · ${p.scale} ${p.scale_unit ?? ""}` : ""}
        {p.project_name ? <><br />Dự án: {p.project_name}</> : null}
      </p>
    </>
  );
}

function ZoneEditor({ mapi, zone, draw, setDraw, communes, communeName, onDone }: {
  mapi: CntsMapApi;
  zone: Zone | null;
  draw: Draw | null;
  setDraw: (d: Draw | null) => void;
  communes: string[];
  communeName: (c: string) => string;
  onDone: () => void;
}) {
  const act = useAction();
  const [f, setF] = useState({
    kind: (zone?.kind ?? "vung_chan_nuoi") as ZoneKind,
    name: zone?.name ?? "",
    communes: (zone?.communes ?? []).join(", "),
    radius: zone?.radius_m ?? 3000,
    effective_on: today(),
    note: zone?.note ?? "",
    reason: "",
    imported: null as ZoneSave["geometry"],
  });
  const mode = draw?.mode ?? "polygon";
  const geometry = f.imported ?? (mode === "radius" ? null : draftToGeometry(mode, draw?.coords ?? []));
  const body: ZoneSave = {
    kind: f.kind,
    name: f.name.trim(),
    communes: f.communes.split(/[,\s]+/).map((c) => c.trim()).filter(Boolean),
    draw_mode: f.imported ? (f.imported.type === "LineString" ? "line" : "polygon") : mode,
    geometry: mode === "radius" && !f.imported ? null : geometry,
    center: mode === "radius" && !f.imported && draw?.center ? { lng: draw.center[0], lat: draw.center[1] } : null,
    radius_m: mode === "radius" && !f.imported ? f.radius : null,
    effective_on: f.effective_on,
    note: f.note.trim() || null,
    outbreak_ref: zone?.outbreak_ref ?? null,
    parent_zone_ref: zone?.parent_zone_ref ?? null,
    species_code: zone?.species_code ?? null,
    disease_code: zone?.disease_code ?? null,
    ...(zone ? { base_version: zone.version, reason: f.reason.trim() } : {}),
  };
  const errs = zoneErrors(body, f.imported ? 3 : (draw?.coords.length ?? 0));
  if (zone && f.reason.trim().length < 5) errs.reason = "Lý do sửa (≥ 5 ký tự)";
  const setMode = (m: Draw["mode"]) => setDraw({ mode: m, coords: m === mode ? (draw?.coords ?? []) : [], center: null, radius: f.radius });
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const g = geometryFromGeoJSON(await file.text());
    if (g) {
      setF({ ...f, imported: g });
      setDraw({ mode: g.type === "LineString" ? "line" : "polygon", coords: geometryToDraft(g), center: null });
    } else act.setNote("Tệp không có Polygon/MultiPolygon/LineString GeoJSON");
  };
  const pick = (k: ZoneKind) => {
    setF({ ...f, kind: k });
    if (k === "huong_di_chuyen" && mode !== "line") setMode("line");
    if (k !== "huong_di_chuyen" && mode === "line") setMode("polygon");
  };
  return (
    <>
      <h3 style={{ marginTop: 0 }}>{zone ? `Sửa vùng (bản ${zone.version})` : "Vẽ vùng mới"}</h3>
      {act.view}
      <label>
        Loại vùng
        <select value={f.kind} onChange={(e) => pick(e.target.value as ZoneKind)} disabled={!!zone && EPIDEMIC.includes(zone.kind) && !!zone.outbreak_ref}>
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {KIND_LABEL[k]}
            </option>
          ))}
        </select>
      </label>
      <div className="row" role="radiogroup" aria-label="Cách vẽ" style={{ gap: 6, margin: "6px 0" }}>
        {(f.kind === "huong_di_chuyen" ? (["line"] as const) : (["polygon", "radius"] as const)).map((m) => (
          <button key={m} className={mode === m ? "btn btn-primary" : "btn"} onClick={() => (setF({ ...f, imported: null }), setMode(m))} aria-pressed={mode === m}>
            {m === "polygon" ? "Vẽ tay" : m === "radius" ? "Theo bán kính" : "Vẽ hướng"}
          </button>
        ))}
      </div>
      <p className="mute">
        {mode === "radius" ? "Bấm lên bản đồ chọn tâm." : `Bấm lên bản đồ thêm đỉnh (${draw?.coords.length ?? 0}); kéo đỉnh để chỉnh.`}{" "}
        <button className="link" onClick={() => setDraw({ mode, coords: (draw?.coords ?? []).slice(0, -1), center: draw?.center ?? null })}>
          Bỏ đỉnh cuối
        </button>
      </p>
      {mode === "radius" ? (
        <label>
          Bán kính (m)
          <input
            type="number"
            min={10}
            max={50000}
            value={f.radius}
            onChange={(e) => {
              setF({ ...f, radius: Number(e.target.value) });
              if (draw) setDraw({ ...draw, radius: Number(e.target.value) });
            }}
          />
          {errs.radius_m ? <span className="err">{errs.radius_m}</span> : null}
          {errs.center ? <span className="err">{errs.center}</span> : null}
        </label>
      ) : null}
      <label>
        Tải GeoJSON lên <input type="file" accept=".geojson,.json,application/geo+json" onChange={onFile} />
      </label>
      <label>
        Tên vùng
        <input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
        {errs.name ? <span className="err">{errs.name}</span> : null}
      </label>
      <label>
        Xã (mã, cách nhau dấu phẩy)
        <input value={f.communes} onChange={(e) => setF({ ...f, communes: e.target.value })} list="cnts-map-communes" />
        <datalist id="cnts-map-communes">
          {communes.map((c) => (
            <option key={c} value={c}>
              {communeName(c)}
            </option>
          ))}
        </datalist>
        {errs.communes ? <span className="err">{errs.communes}</span> : null}
      </label>
      <label>
        Ngày áp dụng
        <input type="date" value={f.effective_on} max={today()} onChange={(e) => setF({ ...f, effective_on: e.target.value })} />
      </label>
      <label>
        Ghi chú
        <textarea value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} />
      </label>
      {zone ? (
        <label>
          Lý do sửa
          <input value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} />
          {errs.reason ? <span className="err">{errs.reason}</span> : null}
        </label>
      ) : null}
      {errs.geometry ? <p className="err">{errs.geometry}</p> : null}
      {errs.draw_mode ? <p className="err">{errs.draw_mode}</p> : null}
      <div className="row" style={{ gap: 8, marginTop: 8 }}>
        <button
          className="btn btn-primary"
          disabled={act.busy || Object.keys(errs).length > 0}
          onClick={async () => {
            const r = await act.run(() => (zone ? mapi.updateZone(zone.ref, body) : mapi.createZone(body)));
            if (r) {
              if (r.warnings.length) window.alert(r.warnings.map((w) => WARNING_LABEL[w] ?? w).join("\n"));
              onDone();
            }
          }}
        >
          Lưu
        </button>
        <button className="btn" onClick={onDone}>
          Hủy
        </button>
      </div>
    </>
  );
}
