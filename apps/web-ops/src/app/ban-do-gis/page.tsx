"use client";

import type { LayerSpecification } from "maplibre-gl";
import { useMemo, useState, type FormEvent } from "react";
import { listAllCooperatives } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { WaterwayLayerSegment } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { LayerMap, type Viewport } from "@/components/LayerMap";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { formatLength } from "@/gis/geometry";
import { capacityMarkers, coopLayers, plotLayer, roadLayer, waterwayLayer } from "@/gis/layers";
import { CAPACITY, limitingLabel, RIPENING_OUTLINE, ROAD_CLASS, ROAD_COLOR, STAGES, WATERWAY_CLASS } from "@/gis/legend";

const MAP_LABEL = "Bản đồ GIS nhiều lớp";
const MAX_FEATURES = 1000;

const LAYERS: LayerSpecification[] = [
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
];

type LayerKey = "coops" | "plots" | "stages" | "ripening" | "facilities" | "roads" | "waterways";
const LAYER_LABEL: Record<LayerKey, string> = {
  coops: "Ranh giới HTX",
  plots: "Thửa ruộng (phóng to)",
  stages: "Giai đoạn mùa vụ (bản đồ nhiệt)",
  ripening: "Vùng chín rộ 1–3 ngày tới",
  facilities: "Kho / Hub (dung lượng)",
  roads: "Đường bộ",
  waterways: "Đường thủy",
};

function decimalOk(s: string): boolean {
  return /^\d+(\.\d+)?$/.test(s.trim()) && Number(s) > 0;
}

/**
 * Web GIS nhiều lớp (GIS FN-01/04/08/09/13/14/15): tải theo khung nhìn, tối đa 1.000 đối tượng mỗi lớp (BR-25),
 * thửa chỉ khi zoom ≥ ngưỡng (BR-24), nền chỉ từ NEXT_PUBLIC_MAP_STYLE_URL (không gọi tile/API ngoài — RB-7).
 */
export default function GisMapPage() {
  const { api } = useAuth();
  const [vp, setVp] = useState<Viewport | null>(null);
  const [show, setShow] = useState<Record<LayerKey, boolean>>({ coops: true, plots: true, stages: true, ripening: true, facilities: true, roads: true, waterways: true });
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

  const coopFc = useMemo(() => coopLayers(coops.data, names, stages.data, { colorByStage: show.stages, ripening: show.ripening }), [coops.data, names, stages.data, show.stages, show.ripening]);
  const plotFc = useMemo(() => plotLayer(plots.data, stages.data, show.stages), [plots.data, stages.data, show.stages]);
  const sources = useMemo(
    () => ({
      coops: coopFc.areas,
      coopMarkers: coopFc.markers,
      ripening: coopFc.ripening,
      plots: plotFc,
      roads: roadLayer(roads.data, !!roadLoad),
      waterways: waterwayLayer(waterways.data?.items, !!bargeLoad),
    }),
    [coopFc, plotFc, roads.data, roadLoad, waterways.data, bargeLoad],
  );
  const markers = useMemo(() => capacityMarkers(widgets.data?.items, (ref) => {
    const w = widgets.data?.items.find((x) => x.ref === ref);
    if (w) setSelected({ kind: "facility", props: w as unknown as Record<string, unknown> });
  }), [widgets.data]);

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
  const errors = [coops.error, plots.error, widgets.error, roads.error, waterways.error, stages.error, coopsList.error].filter((e) => !!e);

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
            {(Object.keys(LAYER_LABEL) as LayerKey[]).map((k) => (
              <label key={k} className="row small" style={{ marginBottom: 4 }}>
                <input type="checkbox" checked={show[k]} onChange={(e) => setShow((s) => ({ ...s, [k]: e.target.checked }))} />
                {LAYER_LABEL[k]}
              </label>
            ))}
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
            {STAGES.map((s) => (
              <li key={s.code} data-stage={s.code}>
                <span className="sw" style={{ background: s.color }} aria-hidden="true" />
                {s.label}
                {stageTotal > 0 ? <span className="mute small">· {formatDecimal(((stageShare.get(s.code) ?? 0) / stageTotal) * 100, 0)}%</span> : null}
              </li>
            ))}
            <li>
              <span className="sw" style={{ background: "transparent", border: `3px dashed ${RIPENING_OUTLINE}` }} aria-hidden="true" />
              Vùng chín rộ 1–3 ngày tới
            </li>
          </ul>
          <h3 className="small" style={{ marginTop: 14 }}>
            Dung lượng kho/Hub
          </h3>
          <ul className="legend" aria-label="Chú giải dung lượng kho/Hub" style={{ marginTop: 6 }}>
            {Object.entries(CAPACITY).map(([k, v]) => (
              <li key={k}>
                <span className="sw" style={{ background: v.color }} aria-hidden="true" />
                {v.label}
                {k === "near_full" && widgets.data ? <span className="mute small">≥ {widgets.data.thresholds.warn_pct}%</span> : null}
                {k === "full" && widgets.data ? <span className="mute small">≥ {widgets.data.thresholds.full_pct}%</span> : null}
              </li>
            ))}
          </ul>
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
            {truncated.length > 0 ? (
              <p className="notice" role="status" style={{ marginTop: 8 }}>
                Đã đạt giới hạn {formatInt(MAX_FEATURES)} đối tượng cho lớp {truncated.join(", ")} — phóng to để xem đủ.
              </p>
            ) : null}
          </div>
          <LayerMap
            label={MAP_LABEL}
            sources={sources}
            layers={LAYERS}
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
            }}
            markers={show.facilities ? markers : []}
            onViewport={setVp}
            onFeatureClick={(layer, props) => setSelected({ kind: layer.split("-")[0] ?? layer, props })}
          />
        </div>

        <aside className="card sec gis-side gis-right" aria-label="Chi tiết và cảnh báo">
          {selected ? <Selected sel={selected} onClose={() => setSelected(null)} /> : <p className="small mute">Bấm một đối tượng trên bản đồ để xem chi tiết.</p>}
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

function Selected({ sel, onClose }: { sel: { kind: string; props: Record<string, unknown> }; onClose: () => void }) {
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
    const cls = CAPACITY[(p.class as keyof typeof CAPACITY) ?? "unknown"] ?? CAPACITY.unknown;
    rows = [
      ["Mức lấp đầy", p.fill_pct ? `${formatDecimal(String(p.fill_pct), 1)}% — ${cls.label}` : cls.label],
      ["Sức chứa / đã dùng", p.capacity ? `${formatDecimal(String(p.capacity), 0)} / ${formatDecimal(String(p.used), 0)} tấn` : "—"],
      ["Nguồn số liệu", str("source")],
      ["Nguồn MKG", p.origin === "mkg" ? "Hub MKG (van công bố)" : "Kho của tenant"],
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
