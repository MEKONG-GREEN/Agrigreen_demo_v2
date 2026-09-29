"use client";

import type { LayerSpecification } from "maplibre-gl";
import { useMemo, useState, type FormEvent } from "react";
import { loadBoundaries, mergeBoundaries } from "@/api/adapters/boundaries";
import { listAllCooperatives } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { WaterwayLayerSegment } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { COVERAGE_STYLES, fromMechStatus, type CoverageStatus } from "@/cgh/coverage";
import { communeCentroids } from "@/cgh/pins";
import { LayerMap, type Viewport } from "@/components/LayerMap";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { formatLength } from "@/gis/geometry";
import { useDisplayPalette } from "@/gis/displayConfig";
import { capacityMarkers, cghLayer, CGH_LEGEND_ORDER, coopLayers, plotLayer, roadLayer, waterwayLayer } from "@/gis/layers";
import { capacityLegend, capacityStyle, limitingLabel, ROAD_CLASS, ROAD_COLOR, stages as stageLegend, WATERWAY_CLASS, type DisplayPalette } from "@/gis/legend";

const MAP_LABEL = "Bản đồ GIS nhiều lớp";
const MAX_FEATURES = 1000;

/** Lớp vẽ theo bảng màu quy chuẩn hiển thị đang hiệu lực (GIS US-CFG-01) — tính lại khi bảng màu đổi. */
function mapLayers(palette: DisplayPalette): LayerSpecification[] {
  const RIPENING_OUTLINE = palette.ripeningOutline;
  return [
  { id: "coops-fill", type: "fill", source: "coops", paint: { "fill-color": ["coalesce", ["get", "fill"], "#9fd8b4"], "fill-opacity": 0.55 } },
  { id: "coops-line", type: "line", source: "coops", paint: { "line-color": "#0b4a2f", "line-width": 1.2 } },
  { id: "ripening-line", type: "line", source: "ripening", paint: { "line-color": RIPENING_OUTLINE, "line-width": 4, "line-dasharray": [1.5, 1] } },
  { id: "plots-fill", type: "fill", source: "plots", paint: { "fill-color": ["coalesce", ["get", "fill"], "#c9e7b8"], "fill-opacity": 0.8 } },
  { id: "plots-line", type: "line", source: "plots", paint: { "line-color": ["case", ["==", ["get", "ripening"], true], RIPENING_OUTLINE, "#35533f"], "line-width": ["case", ["==", ["get", "ripening"], true], 2.5, 0.8] } },
  {
    id: "waterways-line",
    type: "line",
    source: "waterways",
    paint: {
      "line-color": ["match", ["get", "state"], "passable", "#0057b8", "blocked", "#9aa5a0", "#3f8fd2"],
      "line-width": ["match", ["get", "state"], "passable", 5, "blocked", 1.5, ["match", ["get", "waterway_class"], "song", 4, "kenh_chinh", 3, 2]],
      "line-opacity": ["match", ["get", "state"], "blocked", 0.6, 0.9],
    },
  },
  {
    id: "roads-line",
    type: "line",
    source: "roads",
    paint: {
      "line-color": ["match", ["get", "road_class"], ...Object.entries(ROAD_COLOR).flat(), "#7d6608"] as unknown as string,
      "line-width": ["case", ["==", ["get", "highlight"], true], 5, 2.2],
    },
  },
  {
    id: "coop-markers",
    type: "circle",
    source: "coopMarkers",
    paint: { "circle-radius": 6, "circle-color": ["coalesce", ["get", "fill"], "#f0871a"], "circle-stroke-color": "#0b4a2f", "circle-stroke-width": 2 },
  },
  {
    // GIS US-INT-01: tình trạng máy CGH theo HTX — màu do Bản đồ CGH trả (GIS chỉ hiển thị, không tính lại).
    id: "cgh-markers",
    type: "circle",
    source: "cgh",
    paint: {
      "circle-radius": 8,
      "circle-color": ["coalesce", ["get", "color"], "#9AA5A0"],
      "circle-stroke-color": ["case", ["==", ["get", "provisional"], true], "#ffffff", "#1d2b24"],
      "circle-stroke-width": 2,
      "circle-opacity": 0.9,
    },
  },
  ];
}

type LayerKey = "coops" | "plots" | "stages" | "ripening" | "facilities" | "roads" | "waterways" | "cgh";
const LAYER_LABEL: Record<LayerKey, string> = {
  coops: "Ranh giới HTX",
  plots: "Thửa ruộng (phóng to)",
  stages: "Giai đoạn mùa vụ (bản đồ nhiệt)",
  ripening: "Vùng chín rộ 1–3 ngày tới",
  facilities: "Kho / Hub (dung lượng)",
  roads: "Đường bộ",
  waterways: "Đường thủy",
  cgh: "Tình trạng máy CGH (thiếu/thừa)",
};

function decimalOk(s: string): boolean {
  return /^\d+(\.\d+)?$/.test(s.trim()) && Number(s) > 0;
}

/**
 * Web GIS nhiều lớp (GIS FN-01/04/08/09/13/14/15): tải theo khung nhìn, tối đa 1.000 đối tượng mỗi lớp (BR-25),
 * thửa chỉ khi zoom ≥ ngưỡng (BR-24), nền chỉ từ NEXT_PUBLIC_MAP_STYLE_URL (không gọi tile/API ngoài — RB-7).
 */
export default function GisMapPage() {
  const { api, principal } = useAuth();
  const { palette } = useDisplayPalette();
  const layers = useMemo(() => mapLayers(palette), [palette]);
  // Lớp CGH đọc API pf/mechanization — chỉ vai xem/quản trị Bản đồ CGH (không gọi API khi thiếu vai).
  const canCgh = hasRole(principal, ROLES.cghViewer, ROLES.cghAdmin, ROLES.platformAdmin);
  const [vp, setVp] = useState<Viewport | null>(null);
  const [show, setShow] = useState<Record<LayerKey, boolean>>({ coops: true, plots: true, stages: true, ripening: true, facilities: true, roads: true, waterways: true, cgh: false });
  const [roadInput, setRoadInput] = useState("");
  const [roadLoad, setRoadLoad] = useState<string | null>(null);
  const [bargeInput, setBargeInput] = useState("");
  const [bargeLoad, setBargeLoad] = useState<string | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [date, setDate] = useState("");
  const [selected, setSelected] = useState<{ kind: string; props: Record<string, unknown> } | null>(null);
  const zoom = Math.floor(vp?.zoom ?? 0);
  const bbox = vp?.bbox ?? null;

  const coopsList = useAsync(() => listAllCooperatives(api, { includeInactive: true }), [api]);
  const names = useMemo(() => new Map((coopsList.data ?? []).map((c) => [c.ref, c.name])), [coopsList.data]);

  const coops = useAsync(
    () => (bbox && show.coops ? unwrap(api.GET("/v1/geo/cooperative-boundaries", { params: { query: { bbox, zoom, limit: MAX_FEATURES } } })) : Promise.resolve(null)),
    [api, bbox, show.coops],
  );
  const plots = useAsync(
    () => (bbox && show.plots ? unwrap(api.GET("/v1/geo/layers", { params: { query: { bbox, zoom, layers: "plots", limit: MAX_FEATURES } } })) : Promise.resolve(null)),
    [api, bbox, zoom, show.plots],
  );
  const widgets = useAsync(
    () => (bbox && show.facilities ? unwrap(api.GET("/v1/geo/capacity-widgets", { params: { query: { bbox, limit: MAX_FEATURES } } })) : Promise.resolve(null)),
    [api, bbox, show.facilities],
  );
  const roads = useAsync(
    () => (bbox && show.roads ? unwrap(api.GET("/v1/geo/roads", { params: { query: { bbox, zoom, limit: MAX_FEATURES, min_load_tons: roadLoad ?? undefined } } })) : Promise.resolve(null)),
    [api, bbox, show.roads, roadLoad],
  );
  const waterways = useAsync(async () => {
    if (!bbox || !show.waterways) return null;
    type Head = { total: number; passable_count: number; version_no: number | null; message: string | null };
    const box: { head: Head | null } = { head: null };
    const items = await collectPages<WaterwayLayerSegment>(async (cursor) => {
      const page = await unwrap(api.GET("/v1/waterway/segments", { params: { query: { bbox, cursor, limit: 200, min_load_tons: bargeLoad ?? undefined } } }));
      box.head ??= page;
      return page;
    }, MAX_FEATURES);
    return { items, head: box.head };
  }, [api, bbox, show.waterways, bargeLoad]);
  const stages = useAsync(
    () => (show.stages || show.ripening ? unwrap(api.GET("/v1/cropcycle/stage-map", { params: { query: { date: date || undefined } } })) : Promise.resolve(null)),
    [api, show.stages, show.ripening, date],
  );

  const cghOn = show.cgh && canCgh;
  // Kết quả cân đối đã lưu mới nhất cấp HTX (GET /v1/mechanization/coverage?level=cooperative).
  const cgh = useAsync(
    () => (cghOn ? unwrap(api.GET("/v1/mechanization/coverage", { params: { query: { level: "cooperative" } } })) : Promise.resolve(null)),
    [api, cghOn],
  );
  const coopLocs = useMemo(() => new Map((coopsList.data ?? []).map((c) => [c.ref, c])), [coopsList.data]);
  // HTX chưa có toạ độ → tâm xã (như Bản đồ CGH): chỉ tải ranh giới xã của tỉnh cần "vị trí tạm".
  const needProvinces = useMemo(() => {
    if (!cgh.data || !coopsList.data) return "";
    const set = new Set<string>();
    for (const i of cgh.data.items) {
      const l = i.cooperative_ref ? coopLocs.get(i.cooperative_ref) : undefined;
      if ((!l?.location || l.location_provisional) && i.province_code) set.add(i.province_code);
    }
    return Array.from(set).sort().join(",");
  }, [cgh.data, coopLocs, coopsList.data]);
  const communeAreas = useAsync(
    async () => (needProvinces ? mergeBoundaries(await Promise.all(needProvinces.split(",").map((p) => loadBoundaries(api, { level: "commune", parent: p }, 9)))) : null),
    [api, needProvinces],
  );
  const centroids = useMemo(() => communeCentroids(communeAreas.data?.collection), [communeAreas.data]);
  const cghFc = useMemo(() => cghLayer(cghOn ? cgh.data?.items : null, coopLocs, centroids), [cghOn, cgh.data, coopLocs, centroids]);
  const cghLegend = useMemo(() => {
    const server = new Map<CoverageStatus, string>();
    for (const l of cgh.data?.legend ?? []) if (!server.has(fromMechStatus(l.status))) server.set(fromMechStatus(l.status), l.color);
    return CGH_LEGEND_ORDER.map((st) => ({ status: st, label: COVERAGE_STYLES[st].label, color: server.get(st) ?? COVERAGE_STYLES[st].fill }));
  }, [cgh.data]);

  const coopFc = useMemo(() => coopLayers(coops.data, names, stages.data, { colorByStage: show.stages, ripening: show.ripening }, palette), [coops.data, names, stages.data, show.stages, show.ripening, palette]);
  const plotFc = useMemo(() => plotLayer(plots.data, stages.data, show.stages, palette), [plots.data, stages.data, show.stages, palette]);
  const sources = useMemo(
    () => ({
      coops: coopFc.areas,
      coopMarkers: coopFc.markers,
      ripening: coopFc.ripening,
      plots: plotFc,
      roads: roadLayer(roads.data, !!roadLoad),
      waterways: waterwayLayer(waterways.data?.items, !!bargeLoad),
      cgh: cghFc.fc,
    }),
    [coopFc, plotFc, roads.data, roadLoad, waterways.data, bargeLoad, cghFc],
  );
  const markers = useMemo(() => capacityMarkers(widgets.data?.items, (ref) => {
    const w = widgets.data?.items.find((x) => x.ref === ref);
    if (w) setSelected({ kind: "facility", props: w as unknown as Record<string, unknown> });
  }, palette), [widgets.data, palette]);

  const ripeCoops = useMemo(() => (stages.data?.cooperatives ?? []).filter((c) => c.ripening_plot_count > 0).sort((a, b) => (a.earliest_ripening_date ?? "").localeCompare(b.earliest_ripening_date ?? "")), [stages.data]);
  const stageShare = useMemo(() => {
    const total = new Map<string, number>();
    for (const p of stages.data?.plots ?? []) total.set(p.stage, (total.get(p.stage) ?? 0) + Number(p.area_ha));
    return total;
  }, [stages.data]);
  const stageTotal = Array.from(stageShare.values()).reduce((s, v) => s + v, 0);

  const applyLoads = (e: FormEvent) => {
    e.preventDefault();
    if ((roadInput && !decimalOk(roadInput)) || (bargeInput && !decimalOk(bargeInput))) {
      setLoadErr("Tải trọng phải là số lớn hơn 0 (dấu chấm thập phân).");
      return;
    }
    setLoadErr(null);
    setRoadLoad(roadInput.trim() || null);
    setBargeLoad(bargeInput.trim() || null);
  };

  const truncated = [coops.data?.truncated && "ranh giới HTX", plots.data?.plots?.truncated && "thửa", widgets.data?.truncated && "kho/Hub", roads.data?.truncated && "đường bộ", (waterways.data?.head?.total ?? 0) > MAX_FEATURES && "đường thủy"].filter(Boolean);
  // Lớp CGH: chưa có lần cân đối đã lưu → thông báo riêng, không coi là lỗi.
  const cghNoRun = cgh.error?.code === "mechanization.run_not_found";
  const errors = [coops.error, plots.error, widgets.error, roads.error, waterways.error, stages.error, coopsList.error, cghNoRun ? null : cgh.error].filter((e) => !!e);
  const legendStages = stageLegend(palette);
  const legendCapacity = capacityLegend(palette, widgets.data?.thresholds);

  return (
    <>
      <PageHead title="Bản đồ GIS nhiều lớp" subtitle="Ranh giới HTX, thửa, kho/Hub, đường bộ, đường thủy, giai đoạn mùa vụ và vùng chín rộ — tải theo khung nhìn" />
      {errors.map((e, i) => (
        <ProblemAlert key={`${e!.code}-${i}`} problem={e!} />
      ))}
      <div className="gis-grid">
        <aside className="card sec gis-side" aria-label="Lớp bản đồ và chú giải">
          <fieldset className="plain">
            <legend>Lớp bản đồ</legend>
            {(Object.keys(LAYER_LABEL) as LayerKey[]).map((k) => {
              const locked = k === "cgh" && !canCgh;
              return (
                <label key={k} className="row small" style={{ marginBottom: 4 }}>
                  <input
                    type="checkbox"
                    checked={show[k] && !locked}
                    disabled={locked}
                    aria-describedby={locked ? "gis-cgh-locked" : undefined}
                    onChange={(e) => setShow((s) => ({ ...s, [k]: e.target.checked }))}
                  />
                  {LAYER_LABEL[k]}
                </label>
              );
            })}
            {!canCgh ? (
              <p id="gis-cgh-locked" className="small mute" data-testid="cgh-locked">
                Lớp Tình trạng máy CGH cần vai Xem hoặc Quản trị Bản đồ CGH.
              </p>
            ) : null}
          </fieldset>
          <form className="stack" style={{ marginTop: 12 }} onSubmit={applyLoads} aria-label="Tra cứu tải trọng tuyến">
            <div className="field">
              <label htmlFor="gis-road-load">Tải trọng phương tiện đường bộ (tấn)</label>
              <input id="gis-road-load" className="input" inputMode="decimal" value={roadInput} onChange={(e) => setRoadInput(e.target.value)} placeholder="vd 10" />
            </div>
            <div className="field">
              <label htmlFor="gis-barge-load">Tải trọng sà lan đường thủy (tấn)</label>
              <input id="gis-barge-load" className="input" inputMode="decimal" value={bargeInput} onChange={(e) => setBargeInput(e.target.value)} placeholder="vd 100" />
            </div>
            {loadErr ? <span className="err">{loadErr}</span> : null}
            <div className="row">
              <button type="submit" className="btn btn-p btn-sm">
                Tìm tuyến phù hợp
              </button>
              <button
                type="button"
                className="btn btn-o btn-sm"
                onClick={() => {
                  setRoadInput("");
                  setBargeInput("");
                  setRoadLoad(null);
                  setBargeLoad(null);
                  setLoadErr(null);
                }}
              >
                Xoá lọc
              </button>
            </div>
          </form>
          <div className="field" style={{ marginTop: 12 }}>
            <label htmlFor="gis-date">Ngày xem giai đoạn mùa vụ</label>
            <input id="gis-date" className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
          </div>
          <h3 className="small" style={{ marginTop: 14 }}>
            Chú giải giai đoạn mùa vụ
          </h3>
          <ul className="legend" aria-label="Chú giải giai đoạn mùa vụ" style={{ marginTop: 6 }}>
            {legendStages.map((s) => (
              <li key={s.code} data-stage={s.code} data-color={s.color}>
                <span className="sw" style={{ background: s.color }} aria-hidden="true" />
                {s.label}
                {stageTotal > 0 ? <span className="mute small">· {formatDecimal(((stageShare.get(s.code) ?? 0) / stageTotal) * 100, 0)}%</span> : null}
              </li>
            ))}
            <li>
              <span className="sw" style={{ background: "transparent", border: `3px dashed ${palette.ripeningOutline}` }} aria-hidden="true" />
              Vùng chín rộ 1–3 ngày tới
            </li>
          </ul>
          <h3 className="small" style={{ marginTop: 14 }}>
            Dung lượng kho/Hub
          </h3>
          <ul className="legend" aria-label="Chú giải dung lượng kho/Hub" style={{ marginTop: 6 }}>
            {legendCapacity.map((v) => (
              <li key={v.code} data-capacity={v.code} data-color={v.color}>
                <span className="sw" style={{ background: v.color }} aria-hidden="true" />
                {v.label}
                {v.note ? <span className="mute small">{v.note}</span> : null}
              </li>
            ))}
          </ul>
          {cghOn ? (
            <>
              <h3 className="small" style={{ marginTop: 14 }}>
                Tình trạng máy CGH theo HTX
              </h3>
              <ul className="legend" aria-label="Chú giải tình trạng máy CGH" style={{ marginTop: 6 }}>
                {cghLegend.map((l) => (
                  <li key={l.status} data-status={l.status} data-color={l.color}>
                    <span className="sw" style={{ background: l.color, borderRadius: 8 }} aria-hidden="true" />
                    {l.label}
                  </li>
                ))}
              </ul>
              <p className="small mute" style={{ marginTop: 6 }}>
                Kết quả cân đối đã lưu của Bản đồ CGH{cgh.data?.run ? ` (lúc ${formatDateTime(cgh.data.run.computed_at)})` : ""} — GIS chỉ hiển thị, không tự tính lại. Vòng viền trắng = vị trí tạm (tâm xã).
              </p>
            </>
          ) : null}
          <h3 className="small" style={{ marginTop: 14 }}>
            Đường bộ / đường thủy
          </h3>
          <ul className="legend" aria-label="Chú giải giao thông" style={{ marginTop: 6 }}>
            {Object.entries(ROAD_CLASS).map(([k, v]) => (
              <li key={k}>
                <span className="sw" style={{ background: ROAD_COLOR[k], height: 5 }} aria-hidden="true" />
                {v}
              </li>
            ))}
            <li>
              <span className="sw" style={{ background: "#0057b8", height: 5 }} aria-hidden="true" />
              Tuyến thủy đi được với tải trọng đã chọn
            </li>
            <li>
              <span className="sw" style={{ background: "#9aa5a0", height: 3 }} aria-hidden="true" />
              Tuyến thủy không đạt
            </li>
          </ul>
        </aside>

        <div className="stack">
          <div className="card" style={{ padding: 10 }}>
            <div className="row small" data-testid="gis-status" style={{ flexWrap: "wrap", gap: 12 }}>
              <span>Mức zoom: {vp ? formatDecimal(vp.zoom, 1) : "—"}</span>
              <span data-testid="count-coops">HTX trong khung nhìn: {formatInt((coops.data?.features.length ?? 0))}</span>
              <span data-testid="count-plots">Thửa: {formatInt(plots.data?.plots?.features.length ?? 0)}</span>
              <span data-testid="count-facilities">Kho/Hub: {formatInt(widgets.data?.items.length ?? 0)}</span>
              <span data-testid="count-roads">Tuyến đường bộ: {formatInt(roads.data?.features.length ?? 0)}</span>
              {cghOn ? (
                <span data-testid="count-cgh">
                  HTX có tình trạng máy CGH: {formatInt(cghFc.fc.features.length)}
                  {cghFc.unplaced > 0 ? ` (${formatInt(cghFc.unplaced)} chưa định vị)` : ""}
                </span>
              ) : null}
              <span data-testid="count-waterways">
                Tuyến thủy: {formatInt(waterways.data?.items.length ?? 0)}
                {bargeLoad && waterways.data?.head ? ` (đi được ${formatInt(waterways.data.head.passable_count)})` : ""}
              </span>
            </div>
            {show.plots && plots.data?.plots_notice ? (
              <p className="notice" role="status" data-testid="plots-notice" style={{ marginTop: 8 }}>
                {plots.data.plots_notice}
              </p>
            ) : null}
            {roads.data?.none_matched ? (
              <p className="notice" role="status" data-testid="roads-notice" style={{ marginTop: 8 }}>
                {roads.data.notice ?? "Không tìm thấy tuyến phù hợp."}
              </p>
            ) : null}
            {bargeLoad && waterways.data?.head?.message ? (
              <p className="notice" role="status" data-testid="waterways-notice" style={{ marginTop: 8 }}>
                {waterways.data.head.message}
              </p>
            ) : null}
            {cghOn && cghNoRun ? (
              <p className="notice" role="status" data-testid="cgh-notice" style={{ marginTop: 8 }}>
                Chưa có kết quả cân đối máy CGH đã lưu — lớp Tình trạng máy CGH chưa có dữ liệu.
              </p>
            ) : null}
            {truncated.length > 0 ? (
              <p className="notice" role="status" style={{ marginTop: 8 }}>
                Đã đạt giới hạn {formatInt(MAX_FEATURES)} đối tượng cho lớp {truncated.join(", ")} — phóng to để xem đủ.
              </p>
            ) : null}
          </div>
          <LayerMap
            label={MAP_LABEL}
            sources={sources}
            layers={layers}
            initial={{ center: [105.75, 10.2], zoom: 8.3 }}
            visibility={{
              "coops-fill": show.coops,
              "coops-line": show.coops,
              "coop-markers": show.coops,
              "ripening-line": show.coops && show.ripening,
              "plots-fill": show.plots,
              "plots-line": show.plots,
              "roads-line": show.roads,
              "waterways-line": show.waterways,
              "cgh-markers": cghOn,
            }}
            markers={show.facilities ? markers : []}
            onViewport={setVp}
            onFeatureClick={(layer, props) => setSelected({ kind: layer.split("-")[0] ?? layer, props })}
          />
        </div>

        <aside className="card sec gis-side gis-right" aria-label="Chi tiết và cảnh báo">
          {selected ? <Selected sel={selected} palette={palette} onClose={() => setSelected(null)} /> : <p className="small mute">Bấm một đối tượng trên bản đồ để xem chi tiết.</p>}
          <section aria-labelledby="ripe-h" style={{ marginTop: 14 }}>
            <h2 id="ripe-h" className="small" style={{ fontSize: 14 }}>
              Cảnh báo chín rộ 1–3 ngày tới ({formatInt(ripeCoops.length)} vùng)
            </h2>
            <p className="small mute">Sản lượng dự kiến do App HTX gửi — GIS không tự dự báo (BR-48).</p>
            {ripeCoops.length === 0 ? <p className="small mute">Không có vùng chín rộ trong 1–3 ngày tới.</p> : null}
            <ul className="list-plain" aria-label="Vùng chín rộ">
              {ripeCoops.slice(0, 30).map((c) => (
                <li key={c.cooperative_ref} data-coop={c.cooperative_ref}>
                  <b>{names.get(c.cooperative_ref) ?? c.cooperative_ref}</b>
                  <div className="small">
                    Dự kiến thu {formatDate(c.earliest_ripening_date)} · {formatDecimal(c.ripening_area_ha, 1)} ha ·{" "}
                    {c.ripening_expected_tons ? `${formatDecimal(c.ripening_expected_tons, 1)} tấn dự kiến` : "App HTX chưa gửi sản lượng dự kiến"}
                  </div>
                </li>
              ))}
            </ul>
          </section>
          {roadLoad && roads.data && roads.data.features.length > 0 ? (
            <section aria-labelledby="road-h" style={{ marginTop: 14 }}>
              <h2 id="road-h" className="small" style={{ fontSize: 14 }}>
                Tuyến đường bộ đạt ≥ {roadLoad} tấn ({formatInt(roads.data.matched)})
              </h2>
              <ul className="list-plain" aria-label="Tuyến đường bộ phù hợp">
                {roads.data.features.slice(0, 50).map((f) => (
                  <li key={f.properties.ref} className="small">
                    <b>{f.properties.name}</b> · {ROAD_CLASS[f.properties.road_class]} · tối đa {formatDecimal(f.properties.max_load_tons, 1)} tấn · {formatLength(f.properties.length_m)}
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          {bargeLoad && waterways.data && waterways.data.items.some((s) => s.passable) ? (
            <section aria-labelledby="ww-h" style={{ marginTop: 14 }}>
              <h2 id="ww-h" className="small" style={{ fontSize: 14 }}>
                Tuyến thủy đi được với sà lan {bargeLoad} tấn
              </h2>
              <ul className="list-plain" aria-label="Tuyến thủy phù hợp">
                {waterways.data.items
                  .filter((s) => s.passable)
                  .slice(0, 50)
                  .map((s) => (
                    <li key={s.segment_ref} className="small">
                      <b>{s.name}</b> · {WATERWAY_CLASS[s.waterway_class]} · tối đa {s.max_load_tons ? `${formatDecimal(s.max_load_tons, 0)} tấn` : "—"} · {formatLength(s.length_m)}
                    </li>
                  ))}
              </ul>
            </section>
          ) : null}
        </aside>
      </div>
    </>
  );
}

function Selected({ sel, palette, onClose }: { sel: { kind: string; props: Record<string, unknown> }; palette: DisplayPalette; onClose: () => void }) {
  const p = sel.props;
  const str = (k: string) => (p[k] === null || p[k] === undefined || p[k] === "null" ? "—" : String(p[k]));
  let title = str("name");
  let rows: [string, string][] = [];
  if (sel.kind === "coops" || sel.kind === "coop") {
    rows = [
      ["Giai đoạn trội", str("stage_label")],
      ["Diện tích ranh giới", p.area_ha ? `${formatDecimal(String(p.area_ha), 1)} ha` : "—"],
      ["Sản lượng chín rộ dự kiến", p.ripening_tons && p.ripening_tons !== "null" ? `${formatDecimal(String(p.ripening_tons), 1)} tấn` : "—"],
    ];
  } else if (sel.kind === "plots") {
    title = `Thửa ${str("code")}`;
    rows = [["Giai đoạn", str("stage_label")], ["Chín rộ 1–3 ngày tới", p.ripening === true ? "Có" : "Không"]];
  } else if (sel.kind === "roads") {
    rows = [
      ["Phân loại", ROAD_CLASS[str("road_class")] ?? str("road_class")],
      ["Tải trọng tối đa", `${formatDecimal(str("max_load_tons"), 1)} tấn`],
      ["Chiều dài", formatLength(str("length_m"))],
    ];
  } else if (sel.kind === "waterways") {
    rows = [
      ["Phân loại", WATERWAY_CLASS[str("waterway_class")] ?? str("waterway_class")],
      ["Tải trọng lưu thông", p.max_load_tons && p.max_load_tons !== "null" ? `${formatDecimal(String(p.max_load_tons), 0)} tấn` : "chưa kết luận"],
      ["Ràng buộc bất lợi nhất", limitingLabel(str("limiting"))],
      ["Chiều dài", formatLength(str("length_m"))],
    ];
  } else if (sel.kind === "facility") {
    const cls = capacityStyle(typeof p.class === "string" ? p.class : null, palette);
    rows = [
      ["Mức lấp đầy", p.fill_pct ? `${formatDecimal(String(p.fill_pct), 1)}% — ${cls.label}` : cls.label],
      ["Sức chứa / đã dùng", p.capacity ? `${formatDecimal(String(p.capacity), 0)} / ${formatDecimal(String(p.used), 0)} tấn` : "—"],
      ["Nguồn số liệu", str("source")],
      ["Nguồn MKG", p.origin === "mkg" ? "Hub MKG (van công bố)" : "Kho của tenant"],
    ];
  } else if (sel.kind === "cgh") {
    const num = (k: string) => (typeof p[k] === "number" ? formatInt(p[k] as number) : "—");
    rows = [
      ["Mã HTX", str("code")],
      ["Tình trạng máy", str("status_label")],
      ["Mức đáp ứng", p.coverage_pct && p.coverage_pct !== "null" ? `${formatDecimal(String(p.coverage_pct), 1)}%` : "—"],
      ["Nhu cầu (máy)", num("required_whole")],
      ["Máy hoạt động (năng lực)", num("capacity")],
      ["Tổng số máy", num("machines_total")],
      ["Vị trí", p.provisional === true ? "Vị trí tạm (tâm xã)" : "Theo hồ sơ HTX"],
    ];
  }
  return (
    <section aria-labelledby="sel-h" data-testid="gis-selected">
      <div className="sec-h">
        <h2 id="sel-h" style={{ fontSize: 15 }}>
          {title}
        </h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      </div>
      <dl className="dl-inline">
        {rows.map(([k, v]) => (
          <div key={k} style={{ display: "contents" }}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
