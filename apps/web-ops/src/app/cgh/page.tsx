"use client";

import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { COVERAGE_STYLES, coverageStatus } from "@/cgh/coverage";
import { CoverageChip, CoverageLegend, SourceLabel } from "@/components/CoverageLegend";
import { MapView, type AreaCollection } from "@/components/MapView";
import { ProblemAlert } from "@/components/ProblemAlert";
import { KpiTile, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

/** Cổng Cục KTHT — CHỈ XEM (BRD CGH FN-11 BR-02). Không có nút tạo/sửa/xoá. */
export default function CghPortalPage() {
  const { api } = useAuth();
  const [season, setSeason] = useState("HT2026");
  const [stage, setStage] = useState("thu_hoach");
  const [selected, setSelected] = useState<string | null>(null);

  const seasons = useAsync(() => unwrap(api.GET("/v1/catalogs/{catalog}", { params: { path: { catalog: "season" } } })), [api]);
  const stages = useAsync(() => unwrap(api.GET("/v1/catalogs/{catalog}", { params: { path: { catalog: "farm_stage" } } })), [api]);
  const dash = useAsync(() => unwrap(api.GET("/v1/cgh/dashboard", { params: { query: { season } } })), [api, season]);
  const coverage = useAsync(
    () => unwrap(api.GET("/v1/cgh/coverage", { params: { query: { season, stage, level: "province" } } })),
    [api, season, stage],
  );
  const areas = useAsync(() => unwrap(api.GET("/v1/admin-units/boundaries", { params: { query: { level: "province" } } })), [api]);
  const priority = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cgh/priority-cooperatives", { params: { query: { season, stage, cursor, limit: 10 } } })),
    [api, season, stage],
  );

  const thresholds = coverage.data ? coverage.data.thresholds : null;
  const byUnit = useMemo(() => new Map((coverage.data?.items ?? []).map((i) => [i.admin_unit_code, i])), [coverage.data]);

  const areaFc = useMemo<AreaCollection | null>(() => {
    if (!areas.data) return null;
    return {
      type: "FeatureCollection",
      features: areas.data.features.map((f) => {
        const item = byUnit.get(f.properties.code);
        const status = coverageStatus(item?.coverage_pct ?? null, coverage.data ? thresholds : undefined);
        return { ...f, properties: { ...f.properties, fill: COVERAGE_STYLES[status].fill } };
      }),
    };
  }, [areas.data, byUnit, thresholds, coverage.data]);

  const sel = selected ? byUnit.get(selected) : undefined;

  return (
    <>
      <PageHead
        title="Bản đồ cơ giới hoá — Cổng Cục KTHT"
        subtitle={
          <>
            Cân đối cung – cầu máy theo định mức và diện tích · Chỉ xem
            {coverage.data ? <> · Cập nhật {formatDateTime(coverage.data.as_of)}</> : null}
          </>
        }
      />

      <section className="grid4" aria-label="Chỉ số tổng quan" style={{ marginBottom: 16 }}>
        <KpiTile color="green" icon="tractor" label="Tổng số máy" value={dash.data ? (dash.data.total_machines === null ? null : formatInt(dash.data.total_machines)) : "…"} />
        <KpiTile color="blue" icon="coop" label="Tổng số HTX" value={dash.data ? (dash.data.total_cooperatives === null ? null : formatInt(dash.data.total_cooperatives)) : "…"} />
        <KpiTile color="violet" icon="task" label="HTX đủ máy" value={dash.data ? (dash.data.cooperatives_sufficient === null ? null : formatInt(dash.data.cooperatives_sufficient)) : "…"} />
        <KpiTile color="red" icon="alert" label="HTX thiếu máy" value={dash.data ? (dash.data.cooperatives_short === null ? null : formatInt(dash.data.cooperatives_short)) : "…"} />
      </section>
      {dash.error ? <ProblemAlert problem={dash.error} onRetry={dash.reload} /> : null}

      <div className="cgh-grid" style={{ marginBottom: 16 }}>
        <aside className="card sec stack" aria-label="Bộ lọc và chú giải">
          <div className="field">
            <label htmlFor="cgh-season">Mùa vụ</label>
            <select id="cgh-season" className="select" value={season} onChange={(e) => setSeason(e.target.value)}>
              {(seasons.data?.items ?? [{ code: "HT2026", name: "Hè Thu 2026" }]).map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="cgh-stage">Khâu sản xuất</label>
            <select id="cgh-stage" className="select" value={stage} onChange={(e) => setStage(e.target.value)}>
              {(stages.data?.items ?? [{ code: "thu_hoach", name: "Thu hoạch" }]).map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <CoverageLegend thresholds={coverage.data ? thresholds : null} />
        </aside>

        <div className="card" style={{ minHeight: 480, position: "relative" }}>
          {coverage.error ? <ProblemAlert problem={coverage.error} onRetry={coverage.reload} /> : null}
          {areas.error ? <ProblemAlert problem={areas.error} onRetry={areas.reload} /> : null}
          <MapView label="Bản đồ mức đáp ứng máy theo tỉnh" areas={areaFc} onAreaClick={(c) => setSelected((s) => (s === c ? null : c))} height={480} />
        </div>

        <aside className="card sec cgh-right" aria-label="Chi tiết theo tỉnh">
          <div className="sec-h">
            <h2>{sel ? sel.admin_unit_name : "Theo tỉnh"}</h2>
            {sel ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(null)}>
                Tất cả
              </button>
            ) : null}
          </div>
          {coverage.loading && !coverage.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }}>
            {(sel ? [sel] : coverage.data?.items ?? []).map((i) => {
              const status = coverageStatus(i.coverage_pct, thresholds);
              return (
                <li key={i.admin_unit_code} style={{ padding: "10px 0", borderBottom: "1px solid #edf3ef" }}>
                  <button type="button" onClick={() => setSelected(i.admin_unit_code)} style={{ textAlign: "left", width: "100%" }} aria-label={`Xem ${i.admin_unit_name}`}>
                    <div className="row" style={{ justifyContent: "space-between" }}>
                      <b>{i.admin_unit_name}</b>
                      <CoverageChip status={status} />
                    </div>
                    {i.coverage_pct === null ? (
                      <div className="mute small">Chưa đủ dữ liệu để cân đối</div>
                    ) : (
                      <div className="small">
                        Đáp ứng <b style={{ display: "inline" }}>{formatDecimal(i.coverage_pct, 1)}%</b> · Cần {formatInt(i.demand_machines)} · Hoạt động{" "}
                        {formatInt(i.capacity_machines)} <SourceLabel source={i.source} />
                      </div>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </aside>
      </div>

      <section className="card sec" aria-labelledby="pri-title">
        <div className="sec-h">
          <h2 id="pri-title">HTX cần ưu tiên</h2>
          <span className="mute small">Sắp theo mức thiếu máy</span>
        </div>
        {priority.error ? <ProblemAlert problem={priority.error} onRetry={priority.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">HTX cần ưu tiên hỗ trợ cơ giới hoá</caption>
            <thead>
              <tr>
                <th scope="col">HTX</th>
                <th scope="col">Địa bàn</th>
                <th scope="col" className="num">Nhu cầu</th>
                <th scope="col" className="num">Năng lực</th>
                <th scope="col" className="num">Chênh lệch</th>
                <th scope="col" className="num">Mức đáp ứng</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Nguồn</th>
              </tr>
            </thead>
            <tbody>
              {priority.items.map((p) => (
                <tr key={p.cooperative_id}>
                  <td>
                    <b>{p.name}</b>
                    <div className="mute small">{p.code}</div>
                  </td>
                  <td>
                    {p.commune_name}
                    <div className="mute small">{p.province_name}</div>
                  </td>
                  <td className="num">{formatInt(p.demand_machines)}</td>
                  <td className="num">{formatInt(p.capacity_machines)}</td>
                  <td className="num" style={{ color: p.gap < 0 ? "var(--red)" : undefined, fontWeight: 700 }}>
                    {p.gap < 0 ? `Thiếu ${-p.gap}` : p.gap === 0 ? "Đủ" : `Dư ${p.gap}`}
                  </td>
                  <td className="num">{p.coverage_pct === null ? "—" : `${formatDecimal(p.coverage_pct, 1)}%`}</td>
                  <td>
                    <CoverageChip status={coverageStatus(p.coverage_pct, thresholds)} />
                  </td>
                  <td>
                    <SourceLabel source={p.source} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {priority.loading ? <Loading /> : null}
        <LoadMore hasMore={priority.hasMore} loading={priority.loading} onClick={priority.loadMore} />
      </section>
    </>
  );
}
