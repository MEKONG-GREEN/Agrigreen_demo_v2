"use client";

import { useMemo, useState } from "react";
import { cooperativePlotsPage, draftPlotsInCommune, getCooperative } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { PlotFeature } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MapView } from "@/components/MapView";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { StatusChip } from "@/htx/common";
import { DRAFT_LABEL, plotAreas, plotRows, type PlotStatusView } from "@/htx/plotMap";
import { activityLabel, LOG_STATUS, SEASON_STATUS } from "@/htx/rules";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { formatDecimal } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

/** Tóm tắt mùa vụ và nhật ký của lô đang chọn (pf/cropcycle, chỉ đọc). */
function PlotSummary({ plot, status, onClose }: { plot: PlotFeature; status: PlotStatusView; onClose: () => void }) {
  const { api } = useAuth();
  const p = plot.properties;
  const seasons = useAsync(
    () => unwrap(api.GET("/v1/cropcycle/seasons", { params: { query: { plot_ref: p.ref, limit: 20 } } })),
    [api, p.ref],
  );
  // Vụ đang mở (mỗi lô tối đa một, HTX US-SEASON-04), nếu không thì vụ gần nhất.
  const current = seasons.data?.items.find((x) => x.status === "open") ?? seasons.data?.items[0] ?? null;
  const logs = useAsync(
    () => (current ? unwrap(api.GET("/v1/cropcycle/farm-logs", { params: { query: { season_ref: current.season_ref, limit: 50 } } })) : Promise.resolve(null)),
    [api, current?.season_ref],
  );
  const pending = (logs.data?.items ?? []).filter((l) => l.status === "pending").length;
  return (
    <section className="panel stack" style={{ gap: 10 }} aria-labelledby="plot-sum">
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h3 id="plot-sum">Lô {p.code}</h3>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Bỏ chọn
        </button>
      </div>
      <dl style={{ display: "grid", gridTemplateColumns: "minmax(120px, 180px) 1fr", gap: "6px 12px" }}>
        <dt className="mute">Trạng thái</dt>
        <dd>
          <span className={`chip ${status.cls}`}>{status.text}</span>
        </dd>
        <dt className="mute">Diện tích</dt>
        <dd>{formatDecimal(p.area_ha, 2)} ha</dd>
        <dt className="mute">Ranh giới</dt>
        <dd>
          Phiên bản {p.version} · nguồn {p.source === "htx_app" ? "App HTX" : "Cán bộ KN"} · cập nhật {formatDateTime(p.updated_at)}
        </dd>
      </dl>
      {status.text === DRAFT_LABEL ? (
        <div className="alert alert-info small" role="note">
          <Icon name="info" />
          <span>
            Ranh giới do Cán bộ Khuyến nông vẽ trước, chưa gán HTX. Không cần vẽ lại: sửa hoặc gán trên App HTX (màn “Lô ruộng & Ranh giới GPS”) tạo phiên
            bản mới trên cùng bản ghi.
          </span>
        </div>
      ) : null}
      <b>Mùa vụ</b>
      {seasons.error ? <ProblemAlert problem={seasons.error} onRetry={seasons.reload} /> : null}
      {seasons.loading && !seasons.data ? <Loading /> : null}
      {seasons.data && !current ? <p className="mute small">Chưa có vụ trên lô này.</p> : null}
      {current ? (
        <div className="small stack" style={{ gap: 4 }} aria-label="Vụ gần nhất">
          <div>
            Vụ {current.season_code}-{current.season_year} · giống {current.variety_code} · sạ {formatDate(current.sowing_date)}{" "}
            <StatusChip map={SEASON_STATUS} value={current.status} />
          </div>
          <div>
            Dự kiến thu hoạch {formatDate(current.expected_harvest_date)} · diện tích vụ {formatDecimal(current.area_ha, 2)} ha
          </div>
        </div>
      ) : null}
      {current ? (
        <>
          <b>Nhật ký canh tác</b>
          {logs.data ? (
            <>
              <p className="small">
                {logs.data.items.length} nhật ký · {pending} chờ duyệt
              </p>
              <ul className="list-plain small">
                {logs.data.items.slice(0, 5).map((l) => (
                  <li key={l.id}>
                    {formatDateTime(l.event_time)} · {activityLabel(l.activity_code)} <StatusChip map={LOG_STATUS} value={l.status} />
                  </li>
                ))}
              </ul>
            </>
          ) : logs.loading ? (
            <Loading />
          ) : null}
        </>
      ) : null}
    </section>
  );
}

/**
 * Bản đồ lô ruộng cho Quản lý HTX (HTX US-PLOT-01): lô của HTX và lô KN vẽ trước trong xã ("Nháp - KN vẽ
 * trước"), dùng chung bản ghi pf/geo; chọn lô (trên bản đồ hoặc danh sách) → tóm tắt vụ và nhật ký.
 * Chia nhỏ lô chưa hỗ trợ ở G2 (xem g2-acceptance).
 */
export default function PlotMapPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const coopRef = scope.coopRef;
  const coop = useAsync(() => (coopRef ? getCooperative(api, coopRef) : Promise.resolve(null)), [api, coopRef]);
  const own = useAsync(
    () => (coopRef ? collectPages((c) => cooperativePlotsPage(api, coopRef, c, 200), 2000) : Promise.resolve([] as PlotFeature[])),
    [api, coopRef],
  );
  const commune = coop.data?.admin_unit_code ?? null;
  const drafts = useAsync(
    () => (commune ? draftPlotsInCommune(api, commune) : Promise.resolve({ items: [] as PlotFeature[], truncated: false })),
    [api, commune],
  );
  const [selected, setSelected] = useState<string | null>(null);
  const rows = useMemo(() => plotRows(own.data ?? [], drafts.data?.items ?? [], coopRef), [own.data, drafts.data, coopRef]);
  const areas = useMemo(() => plotAreas(own.data ?? [], drafts.data?.items ?? [], coopRef), [own.data, drafts.data, coopRef]);
  const picked = rows.find((r) => r.plot.properties.ref === selected) ?? null;
  const error = coop.error ?? own.error ?? drafts.error;

  return (
    <>
      <PageHead title="Lô ruộng & ranh giới" subtitle="Lô của HTX và ranh giới Cán bộ Khuyến nông vẽ trước — dùng chung một bản ghi, không vẽ lại" />
      <div className="card sec">
        <div className="row" style={{ marginBottom: 12 }}>
          <CoopPicker scope={scope} allowAll={false} id="plot-coop" />
        </div>
        {!coopRef ? <Empty>Chọn HTX để xem bản đồ lô ruộng.</Empty> : null}
        {error ? <ProblemAlert problem={error} onRetry={() => { coop.reload(); own.reload(); drafts.reload(); }} /> : null}
        {coopRef && (own.loading || drafts.loading) && rows.length === 0 ? <Loading /> : null}
        {coopRef ? (
          <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.5fr) minmax(280px, 1fr)", alignItems: "start" }}>
            <div className="stack" style={{ gap: 10 }}>
              <MapView label="Bản đồ lô ruộng của HTX" areas={areas} describeAreas showAreaLabels={false} onAreaClick={(ref) => setSelected((s) => (s === ref ? null : ref))} height={380} />
              <div className="row small" aria-label="Chú giải">
                <span className="chip c-ok">Lô của HTX</span>
                <span className="chip c-warn">{DRAFT_LABEL}</span>
                <span className="chip c-bad">Có chồng lấn chờ xem xét</span>
                <span className="chip c-mute">Ngừng dùng</span>
              </div>
              {drafts.data?.truncated ? <p className="mute small">Có hơn 1.000 thửa trong khung xã — chỉ hiện một phần lô nháp.</p> : null}
              <div className="tbl-wrap">
                <table className="tbl">
                  <caption className="sr-only">Danh sách lô ruộng</caption>
                  <thead>
                    <tr>
                      <th scope="col">Mã lô</th>
                      <th scope="col" className="num">Diện tích (ha)</th>
                      <th scope="col">Trạng thái</th>
                      <th scope="col">Thao tác</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map(({ plot, status }) => (
                      <tr key={plot.properties.ref} aria-selected={selected === plot.properties.ref}>
                        <td>
                          <b>{plot.properties.code}</b>
                          {plot.properties.name ? <div className="mute small">{plot.properties.name}</div> : null}
                        </td>
                        <td className="num">{formatDecimal(plot.properties.area_ha, 2)}</td>
                        <td>
                          <span className={`chip ${status.cls}`}>{status.text}</span>
                        </td>
                        <td>
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(plot.properties.ref)} aria-label={`Xem lô ${plot.properties.code}`}>
                            Xem
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {!own.loading && !drafts.loading && rows.length === 0 && !error ? <Empty>HTX chưa có lô ruộng và xã không có lô nháp.</Empty> : null}
            </div>
            <div>{picked ? <PlotSummary plot={picked.plot} status={picked.status} onClose={() => setSelected(null)} /> : <p className="mute small">Chọn một lô trên bản đồ hoặc trong danh sách để xem vụ và nhật ký.</p>}</div>
          </div>
        ) : null}
      </div>
    </>
  );
}
