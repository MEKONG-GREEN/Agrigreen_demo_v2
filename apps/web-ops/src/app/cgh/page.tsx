"use client";

import { useMemo, useState } from "react";
import { loadBoundaries, mergeBoundaries } from "@/api/adapters/boundaries";
import { unitLabel } from "@/api/adapters/units";
import { ForecastSection } from "@/cgh/ForecastSection";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import { AreaFreshness } from "@/cgh/AreaFreshness";
import { CoopPanel } from "@/cgh/CoopPanel";
import { buildPins, communeCentroids, pinsToGeoJSON, searchPins } from "@/cgh/pins";
import type { MechCoverageItem, MechGroup } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { COVERAGE_STYLES, fromMechStatus, thresholdsFromRun } from "@/cgh/coverage";
import { CoverageChip, CoverageLegend, SourceLabel } from "@/components/CoverageLegend";
import { MapView, type AreaCollection } from "@/components/MapView";
import { ProblemAlert } from "@/components/ProblemAlert";
import { KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { useUnits } from "@/lib/useUnits";
import { MetricHistory, type MetricOption } from "@/metrics/MetricHistory";

/** Chỉ số CGH theo vụ (pf/metrics, grain = season): xu hướng nhiều ngày của kết quả cân đối đã lưu (FN-10/11). */
const CGH_METRICS: MetricOption[] = [
  { key: "cgh.coop_sufficiency_pct", label: "Tỷ lệ HTX đủ máy (%)", digits: 1 },
  { key: "cgh.coops_shortage", label: "Số HTX thiếu máy", digits: 0 },
];

const SEASON_NAME: Record<string, string> = { DX: "Đông Xuân", HT: "Hè Thu", TD: "Thu Đông" };

/** Nhóm của khâu đang lọc (nếu có), không thì mức chung (= khâu thấp nhất). */
function groupFor(item: MechCoverageItem, stage: string): MechGroup {
  return (stage ? item.stages[stage] : undefined) ?? item.overall;
}

function GroupLine({ g, source }: { g: MechGroup; source?: string | null }) {
  if (g.coverage_pct === null) return <div className="mute small">{g.status_label}</div>;
  return (
    <div className="small">
      Đáp ứng <b style={{ display: "inline" }}>{formatDecimal(g.coverage_pct, 1)}%</b> · Cần {formatInt(g.required_whole)} · Hoạt động{" "}
      {formatInt(g.capacity)} {source !== undefined ? <SourceLabel source={source} /> : null}
    </div>
  );
}

/**
 * Cổng Cục KTHT — CHỈ XEM (BRD CGH FN-11 BR-02). Đọc KẾT QUẢ CÂN ĐỐI ĐÃ LƯU theo vụ (pf/mechanization),
 * không tính lại ở client: mức đáp ứng, màu và nhãn do máy chủ trả.
 */
export default function CghPortalPage() {
  const { api } = useAuth();
  const [runId, setRunId] = useState("");
  const [stage, setStage] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [coopSel, setCoopSel] = useState<string | null>(null);
  const [focus, setFocus] = useState<{ lng: number; lat: number; zoom?: number; key: string } | null>(null);
  const [query, setQuery] = useState("");

  const runs = useAsync(() => unwrap(api.GET("/v1/mechanization/balance-runs")), [api]);
  const stages = useAsync(() => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "farm_stage" } } })), [api]);
  const dash = useAsync(() => unwrap(api.GET("/v1/mechanization/dashboard", { params: { query: { run_id: runId || undefined } } })), [api, runId]);
  const coverage = useAsync(
    () =>
      unwrap(
        api.GET("/v1/mechanization/coverage", {
          params: { query: { level: "province", run_id: runId || undefined, farm_stage: stage || undefined } },
        }),
      ),
    [api, runId, stage],
  );
  const areas = useAsync(() => loadBoundaries(api, { level: "province" }), [api]);
  // Kết quả cân đối cấp HTX (G-10: tên, Mã HTX) — ghim bản đồ (FN-08 B2) và bảng dự báo.
  const coopLevel = useAsync(
    () =>
      unwrap(
        api.GET("/v1/mechanization/coverage", { params: { query: { level: "cooperative", run_id: runId || undefined, farm_stage: stage || undefined } } }),
      ),
    [api, runId, stage],
  );
  // Vị trí HTX từ hồ sơ pf/party (Cục đọc được hồ sơ HTX, chỉ xem).
  const coopLocs = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/cooperatives", { params: { query: { cursor, limit: 200 } } })), 5000),
    [api],
  );
  const locMap = useMemo(() => new Map((coopLocs.data ?? []).map((c) => [c.ref, c])), [coopLocs.data]);
  // HTX chưa có toạ độ → tâm xã: chỉ tải ranh giới xã của các tỉnh cần đặt "vị trí tạm".
  const needProvinces = useMemo(() => {
    if (!coopLocs.data) return "";
    const set = new Set<string>();
    for (const i of coopLevel.data?.items ?? []) {
      const l = i.cooperative_ref ? locMap.get(i.cooperative_ref) : undefined;
      if ((!l?.location || l.location_provisional) && i.province_code) set.add(i.province_code);
    }
    return Array.from(set).sort().join(",");
  }, [coopLevel.data, locMap, coopLocs.data]);
  const communeAreas = useAsync(
    async () =>
      needProvinces
        ? mergeBoundaries(await Promise.all(needProvinces.split(",").map((p) => loadBoundaries(api, { level: "commune", parent: p }, 9))))
        : null,
    [api, needProvinces],
  );
  const centroids = useMemo(() => communeCentroids(communeAreas.data?.collection), [communeAreas.data]);
  const { pins, unplaced } = useMemo(() => buildPins(coopLevel.data?.items ?? [], locMap, centroids, stage), [coopLevel.data, locMap, centroids, stage]);
  const pinsFc = useMemo(() => pinsToGeoJSON(pins), [pins]);
  const pinByRef = useMemo(() => new Map(pins.map((p) => [p.ref, p])), [pins]);
  const hits = useMemo(() => searchPins(pins, query), [pins, query]);
  const stageNames = useMemo(() => new Map((stages.data?.items ?? []).map((s) => [s.code, s.name])), [stages.data]);
  const provisionalCount = useMemo(() => pins.filter((p) => p.provisional).length, [pins]);
  const openCoop = (ref: string, fly: boolean) => {
    setCoopSel(ref);
    const p = pinByRef.get(ref);
    if (fly && p) setFocus((f) => ({ lng: p.lng, lat: p.lat, zoom: 13, key: `${ref}#${Number(f?.key.split("#")[1] ?? 0) + 1}` }));
  };
  const coopNames = useMemo(
    () => new Map((coopLevel.data?.items ?? []).filter((i) => i.cooperative_ref).map((i) => [i.cooperative_ref!, { name: i.name, code: i.cooperative_code ?? null }])),
    [coopLevel.data],
  );

  const run = coverage.data?.run ?? dash.data?.run ?? null;
  const thresholds = thresholdsFromRun(run);
  const byUnit = useMemo(() => new Map((coverage.data?.items ?? []).map((i) => [i.unit_code ?? "", i])), [coverage.data]);
  const priority = dash.data?.priority ?? [];
  const units = useUnits(api, priority.map((p) => p.commune_code));

  const areaFc = useMemo<AreaCollection | null>(() => {
    if (!areas.data) return null;
    return {
      type: "FeatureCollection",
      features: areas.data.collection.features.map((f) => {
        const item = byUnit.get(f.properties.code);
        // Màu tô do backend trả (FN-08 BR-01); chưa có kết quả → xám "Chưa có dữ liệu".
        const g = item ? groupFor(item, stage) : null;
        return { ...f, properties: { ...f.properties, fill: g ? g.color : COVERAGE_STYLES.khong_du_lieu.fill, status_label: g?.status_label ?? "Chưa có dữ liệu" } };
      }),
    };
  }, [areas.data, byUnit, stage]);

  const sel = selected ? byUnit.get(selected) : undefined;
  const kpis = dash.data?.kpis;
  const noRun = coverage.error?.code === "mechanization.run_not_found";

  return (
    <>
      <PageHead
        title="Bản đồ cơ giới hoá — Cổng Cục KTHT"
        subtitle={
          <>
            Cân đối cung – cầu máy theo định mức và diện tích · Chỉ xem
            {run ? (
              <>
                {" "}
                · Vụ {SEASON_NAME[run.season_code] ?? run.season_code} {run.season_year} · Mốc {formatDate(run.reference_date)} · Cân đối lúc{" "}
                {formatDateTime(run.computed_at)}
              </>
            ) : null}
          </>
        }
      />

      <AreaFreshness season={run ? { season_code: run.season_code, season_year: run.season_year } : null} coopNames={coopNames} />

      <section className="grid4" aria-label="Chỉ số tổng quan" style={{ marginBottom: 16 }}>
        <KpiTile color="green" icon="tractor" label="Tổng số máy" value={kpis ? formatInt(kpis.total_machines) : dash.loading ? "…" : null} />
        <KpiTile color="blue" icon="coop" label="Tổng số HTX" value={kpis ? formatInt(kpis.total_cooperatives) : dash.loading ? "…" : null} />
        <KpiTile color="violet" icon="task" label="HTX đủ máy" value={kpis ? formatInt(kpis.cooperatives_sufficient) : dash.loading ? "…" : null} />
        <KpiTile color="red" icon="alert" label="HTX thiếu máy" value={kpis ? formatInt(kpis.cooperatives_shortage) : dash.loading ? "…" : null} />
      </section>
      {dash.error ? <ProblemAlert problem={dash.error} onRetry={dash.reload} /> : null}

      <div className="cgh-grid" style={{ marginBottom: 16 }}>
        <aside className="card sec stack" aria-label="Bộ lọc và chú giải">
          <div className="field">
            <label htmlFor="cgh-run">Mùa vụ (lần cân đối đã lưu)</label>
            <select id="cgh-run" className="select" value={runId} onChange={(e) => setRunId(e.target.value)}>
              <option value="">Mới nhất</option>
              {(runs.data?.items ?? []).map((r) => (
                <option key={r.id} value={r.id}>
                  {SEASON_NAME[r.season_code] ?? r.season_code} {r.season_year} — {formatDate(r.computed_at)}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="cgh-stage">Khâu sản xuất</label>
            <select id="cgh-stage" className="select" value={stage} onChange={(e) => setStage(e.target.value)}>
              <option value="">Tất cả khâu (mức của khâu thấp nhất)</option>
              {(stages.data?.items ?? []).map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <CoverageLegend thresholds={thresholds} legend={coverage.data?.legend} />
          <p className="small mute">
            Ghim HTX: vòng rỗng = <b style={{ display: "inline" }}>vị trí tạm</b> (chưa có toạ độ, đặt ở tâm xã). Phóng to để tách cụm.
          </p>
          <div className="field">
            <label htmlFor="cgh-search">Tra cứu nhanh HTX (tên, Mã HTX, xã)</label>
            <input id="cgh-search" className="input" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="VD: HTX8200001, Mỹ An" />
          </div>
          {query.trim() ? (
            hits.length === 0 ? (
              <p className="small" role="status" style={{ color: "var(--amber)" }}>
                Không có kết quả phù hợp
              </p>
            ) : (
              <ul className="list-plain" aria-label="Kết quả tra cứu HTX">
                {hits.map((h) => (
                  <li key={h.ref}>
                    <button type="button" style={{ textAlign: "left", width: "100%" }} onClick={() => openCoop(h.ref, true)}>
                      <b style={{ display: "inline" }}>{h.name}</b> <span className="mute small">{h.code ?? ""} · {h.communeName ?? h.communeCode ?? ""}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )
          ) : null}
          <p className="small mute" data-testid="pin-count">
            {formatInt(pins.length)} HTX trên bản đồ · {formatInt(provisionalCount)} vị trí tạm{unplaced > 0 ? ` · ${formatInt(unplaced)} chưa định vị được` : ""}
          </p>
          {run && run.missing_norms.length > 0 ? (
            <p className="small" style={{ color: "var(--amber)" }}>
              Chưa cấu hình định mức: {run.missing_norms.join(", ")}
            </p>
          ) : null}
        </aside>

        <div className="card" style={{ minHeight: 480, position: "relative" }}>
          {noRun ? (
            <div className="alert alert-info" role="status">
              Chưa có kết quả cân đối đã lưu cho vụ này. Bản đồ chỉ hiển thị kết quả đã lưu, không tính lại.
            </div>
          ) : coverage.error ? (
            <ProblemAlert problem={coverage.error} onRetry={coverage.reload} />
          ) : null}
          {areas.error ? <ProblemAlert problem={areas.error} onRetry={areas.reload} /> : null}
          <MapView
            label="Bản đồ mức đáp ứng máy theo tỉnh"
            areas={areaFc}
            points={pinsFc}
            clusterPoints
            onPointClick={(ref) => openCoop(ref, false)}
            focus={focus}
            describeAreas
            onAreaClick={(c) => {
              setCoopSel(null);
              setSelected((s) => (s === c ? null : c));
            }}
            height={480}
          />
        </div>

        <aside className="card sec cgh-right" aria-label="Chi tiết theo tỉnh">
          {coopSel && pinByRef.get(coopSel) ? (
            <CoopPanel pin={pinByRef.get(coopSel)!} runId={runId} stageNames={stageNames} onClose={() => setCoopSel(null)} />
          ) : (
          <>
          <div className="sec-h">
            <h2>{sel ? sel.name : "Theo tỉnh"}</h2>
            {sel ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(null)}>
                Tất cả
              </button>
            ) : null}
          </div>
          {coverage.loading && !coverage.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }}>
            {(sel ? [sel] : (coverage.data?.items ?? [])).map((i) => {
              const g = groupFor(i, stage);
              return (
                <li key={i.unit_code ?? i.name} style={{ padding: "10px 0", borderBottom: "1px solid #edf3ef" }}>
                  <button type="button" onClick={() => setSelected(i.unit_code)} style={{ textAlign: "left", width: "100%" }} aria-label={`Xem ${i.name}`}>
                    <div className="row" style={{ justifyContent: "space-between" }}>
                      <b>{i.name}</b>
                      <CoverageChip status={fromMechStatus(g.status)} />
                    </div>
                    <GroupLine g={g} />
                    <div className="mute small">{formatInt(i.cooperatives)} HTX</div>
                  </button>
                </li>
              );
            })}
          </ul>
          </>
          )}
        </aside>
      </div>

      <section className="card sec" aria-labelledby="pri-title">
        <div className="sec-h">
          <h2 id="pri-title">HTX cần ưu tiên</h2>
          <span className="mute small">Mức đáp ứng thấp nhất trước</span>
        </div>
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">HTX cần ưu tiên hỗ trợ cơ giới hoá</caption>
            <thead>
              <tr>
                <th scope="col">Mã HTX</th>
                <th scope="col">HTX</th>
                <th scope="col">Địa bàn</th>
                <th scope="col" className="num">Nhu cầu (máy)</th>
                <th scope="col" className="num">Năng lực</th>
                <th scope="col" className="num">Chênh lệch</th>
                <th scope="col" className="num">Mức đáp ứng</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Nguồn</th>
              </tr>
            </thead>
            <tbody>
              {priority.map((p) => {
                const gap = p.overall.capacity - p.overall.required_whole;
                return (
                  <tr key={p.cooperative_ref ?? p.name}>
                    <td>
                      <code>{p.cooperative_code ?? "—"}</code>
                    </td>
                    <td>
                      <b>{p.name}</b>
                    </td>
                    <td>{unitLabel(units, p.commune_code)}</td>
                    <td className="num">{formatInt(p.overall.required_whole)}</td>
                    <td className="num">{formatInt(p.overall.capacity)}</td>
                    <td className="num" style={{ color: gap < 0 ? "var(--red)" : undefined, fontWeight: 700 }}>
                      {gap < 0 ? `Thiếu ${-gap}` : gap === 0 ? "Đủ" : `Dư ${gap}`}
                    </td>
                    <td className="num">{p.overall.coverage_pct === null ? "—" : `${formatDecimal(p.overall.coverage_pct, 1)}%`}</td>
                    <td>
                      <CoverageChip status={fromMechStatus(p.overall.status)} />
                    </td>
                    <td>
                      <SourceLabel source={p.area_source} label={p.area_source_label} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {dash.loading && !dash.data ? <Loading /> : null}
        {dash.data && priority.length === 0 ? <p className="mute small">Chưa có HTX nào trong kết quả cân đối.</p> : null}
      </section>
      <div style={{ marginTop: 16 }}>
        <MetricHistory
          title="Xu hướng cân đối máy theo vụ (lịch sử đã chốt)"
          metrics={CGH_METRICS}
          seasonal
          units={[{ code: "", name: "Toàn vùng" }, ...(areas.data?.collection.features ?? []).map((f) => ({ code: f.properties.code, name: f.properties.name }))]}
        />
      </div>
      <ForecastSection coopNames={coopNames} />
    </>
  );
}
