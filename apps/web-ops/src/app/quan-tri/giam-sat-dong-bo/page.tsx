"use client";

import { useMemo, useState } from "react";
import { dayBound, dlqAlerts, dlqLabel, LOG_STATUS, SYNC_STATUS, WINDOW_OPTIONS, type SyncIntegration } from "@/admin/syncMonitor";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

type StatusFilter = "" | SyncIntegration["status"];

/**
 * Giám sát đồng bộ tích hợp (GIS US-SYNC-01/02): tình trạng từng luồng (consumer) — số sự kiện xử lý, lần thành công /
 * lỗi gần nhất, hàng chờ lỗi (DLQ) — và nhật ký từng lần đồng bộ. Admin API, MFA; vai platform_admin, device_admin.
 */
export default function SyncMonitorPage() {
  const { adminApi } = useAuth();
  const [windowHours, setWindowHours] = useState(24);
  const [consumer, setConsumer] = useState("");
  const [status, setStatus] = useState<StatusFilter>("");
  const [version, setVersion] = useState(0);

  // Toàn bộ nguồn (không lọc) — cho danh sách chọn nguồn và cảnh báo hàng chờ lỗi.
  const all = useAsync(() => unwrap(adminApi.GET("/v1/sync/integrations", { params: { query: { window_hours: windowHours } } })), [adminApi, windowHours, version]);
  const list = useAsync(
    () =>
      consumer || status
        ? unwrap(adminApi.GET("/v1/sync/integrations", { params: { query: { window_hours: windowHours, consumer: consumer || undefined, status: status || undefined } } }))
        : Promise.resolve(null),
    [adminApi, windowHours, consumer, status, version],
  );
  const shown = consumer || status ? list.data : all.data;
  const sources = useMemo(() => (all.data?.items ?? []).map((i) => ({ consumer: i.consumer, source: i.source })), [all.data]);
  const alerts = useMemo(() => dlqAlerts(all.data?.items ?? []), [all.data]);

  return (
    <>
      <PageHead
        title="Giám sát đồng bộ"
        subtitle="Tình trạng các luồng đồng bộ tích hợp (App HTX, ERP/MKG, GIS, TMS): lần thành công, lỗi và hàng chờ lỗi"
        actions={
          <button type="button" className="btn btn-o btn-sm" onClick={() => setVersion((v) => v + 1)}>
            Làm mới
          </button>
        }
      />
      <MfaNotice />
      {alerts.length > 0 ? (
        <div className="alert alert-bad" role="alert" data-testid="dlq-banner" style={{ marginBottom: 16 }}>
          <Icon name="alert" />
          <div>
            <b>Có sự kiện trong hàng chờ lỗi — xem runbook integration-alerts</b>
            <ul style={{ margin: "6px 0 0 18px" }}>
              {alerts.map((a) => (
                <li key={a.consumer}>
                  {a.source}: {formatInt(a.dead_lettered_count)} sự kiện vào hàng chờ lỗi trong cửa sổ · hàng chờ hiện tại {dlqLabel(a.dlq_depth)}
                  {a.last_error_code ? (
                    <>
                      {" "}
                      · mã lỗi <code>{a.last_error_code}</code>
                    </>
                  ) : null}
                </li>
              ))}
            </ul>
            <p className="small" style={{ marginTop: 6 }}>
              Sự kiện trong hàng chờ lỗi không tự xử lý lại: kiểm tra nguyên nhân theo mã lỗi rồi phát lại theo runbook <code>docs/runbooks/integration-alerts.md</code>.
            </p>
          </div>
        </div>
      ) : null}
      {all.error ? <ProblemAlert problem={all.error} onRetry={all.reload} /> : null}
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}

      <section className="card sec" style={{ marginBottom: 16 }} aria-labelledby="sync-sum-h">
        <div className="sec-h">
          <h2 id="sync-sum-h">Tình trạng theo nguồn</h2>
          {all.data ? <span className="mute small">Từ {formatDateTime(all.data.since)}</span> : null}
        </div>
        <div className="row" style={{ marginBottom: 12, gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}>
          <div className="field">
            <label htmlFor="sync-source">Nguồn</label>
            <select id="sync-source" className="select" value={consumer} onChange={(e) => setConsumer(e.target.value)}>
              <option value="">Tất cả nguồn</option>
              {sources.map((s) => (
                <option key={s.consumer} value={s.consumer}>
                  {s.source}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="sync-status">Trạng thái</label>
            <select id="sync-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as StatusFilter)}>
              <option value="">Tất cả</option>
              {(Object.keys(SYNC_STATUS) as SyncIntegration["status"][]).map((s) => (
                <option key={s} value={s}>
                  {SYNC_STATUS[s].label}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="sync-window">Khoảng thời gian</label>
            <select id="sync-window" className="select" value={windowHours} onChange={(e) => setWindowHours(Number(e.target.value))}>
              {WINDOW_OPTIONS.map((w) => (
                <option key={w.hours} value={w.hours}>
                  {w.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        {(consumer || status ? list.loading : all.loading) && !shown ? <Loading /> : null}
        {shown && shown.items.length === 0 ? <Empty>Không có nguồn nào khớp bộ lọc.</Empty> : null}
        {shown && shown.items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Tình trạng đồng bộ theo nguồn">
              <thead>
                <tr>
                  <th scope="col">Nguồn</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col" className="num">Đã xử lý</th>
                  <th scope="col">Thành công gần nhất</th>
                  <th scope="col" className="num">Lỗi</th>
                  <th scope="col" className="num">Vào hàng chờ lỗi</th>
                  <th scope="col">Lỗi gần nhất</th>
                  <th scope="col" className="num">Hàng chờ lỗi (DLQ)</th>
                </tr>
              </thead>
              <tbody>
                {shown.items.map((i) => (
                  <tr key={i.consumer} data-consumer={i.consumer}>
                    <td>
                      <b>{i.source}</b>
                      <div className="mute small">
                        <code>{i.consumer}</code>
                      </div>
                    </td>
                    <td>
                      <span className={`chip ${SYNC_STATUS[i.status].cls}`} data-status={i.status}>
                        {SYNC_STATUS[i.status].label}
                      </span>
                    </td>
                    <td className="num">{formatInt(i.processed_count)}</td>
                    <td className="small">
                      {formatDateTime(i.last_processed_at)}
                      {i.last_event_type ? (
                        <div className="mute">
                          <code>{i.last_event_type}</code>
                        </div>
                      ) : null}
                    </td>
                    <td className="num">{formatInt(i.failure_count)}</td>
                    <td className="num">{formatInt(i.dead_lettered_count)}</td>
                    <td className="small">
                      {i.last_failure_at ? formatDateTime(i.last_failure_at) : "—"}
                      {i.last_error_code ? (
                        <div>
                          <code>{i.last_error_code}</code>
                        </div>
                      ) : null}
                    </td>
                    <td className="num" data-testid="dlq-depth">
                      {dlqLabel(i.dlq_depth)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {all.data && !all.data.dlq_available ? (
          <p className="small mute" style={{ marginTop: 8 }}>
            Chưa cấu hình đo độ sâu hàng chờ lỗi (RabbitMQ management API) — cột DLQ hiện “không đo được”.
          </p>
        ) : null}
      </section>

      <SyncLog sources={sources} />
    </>
  );
}

function SyncLog({ sources }: { sources: { consumer: string; source: string }[] }) {
  const { adminApi } = useAuth();
  const [consumer, setConsumer] = useState("");
  const [status, setStatus] = useState<"" | "ok" | "error">("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const rangeErr = from && to && from > to ? "Từ ngày phải trước hoặc bằng đến ngày." : null;
  const log = usePagedList(
    (cursor) =>
      rangeErr
        ? Promise.resolve({ items: [], next_cursor: null })
        : unwrap(
            adminApi.GET("/v1/sync/integrations/log", {
              params: { query: { consumer: consumer || undefined, status: status || undefined, from: dayBound(from, false), to: dayBound(to, true), cursor, limit: 25 } },
            }),
          ),
    [adminApi, consumer, status, from, to, rangeErr],
  );
  return (
    <section className="card sec" aria-labelledby="sync-log-h">
      <div className="sec-h">
        <h2 id="sync-log-h">Nhật ký đồng bộ</h2>
        <span className="mute small">Mới nhất trước · mặc định 7 ngày qua</span>
      </div>
      <div className="row" style={{ marginBottom: 12, gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}>
        <div className="field">
          <label htmlFor="log-source">Nguồn (nhật ký)</label>
          <select id="log-source" className="select" value={consumer} onChange={(e) => setConsumer(e.target.value)}>
            <option value="">Tất cả nguồn</option>
            {sources.map((s) => (
              <option key={s.consumer} value={s.consumer}>
                {s.source}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="log-status">Kết quả</label>
          <select id="log-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as "" | "ok" | "error")}>
            <option value="">Tất cả</option>
            <option value="ok">{LOG_STATUS.ok.label}</option>
            <option value="error">{LOG_STATUS.error.label}</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="log-from">Từ ngày</label>
          <input id="log-from" className="input" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="log-to">Đến ngày</label>
          <input id="log-to" className="input" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </div>
      </div>
      {rangeErr ? (
        <p className="err" role="alert">
          {rangeErr}
        </p>
      ) : null}
      {log.error ? <ProblemAlert problem={log.error} onRetry={log.reload} /> : null}
      {log.loading && log.items.length === 0 ? <Loading /> : null}
      {!log.loading && !log.error && log.items.length === 0 && !rangeErr ? <Empty>Không có lần đồng bộ nào khớp bộ lọc.</Empty> : null}
      {log.items.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Nhật ký đồng bộ">
            <thead>
              <tr>
                <th scope="col">Thời điểm</th>
                <th scope="col">Nguồn</th>
                <th scope="col">Loại sự kiện</th>
                <th scope="col">Kết quả</th>
                <th scope="col" className="num">Số bản ghi</th>
                <th scope="col" className="num">Số lần giao</th>
                <th scope="col">Mã lỗi</th>
              </tr>
            </thead>
            <tbody>
              {log.items.map((l) => (
                <tr key={`${l.event_id}-${l.at}`} data-status={l.status}>
                  <td className="small">{formatDateTime(l.at)}</td>
                  <td className="small">{l.source}</td>
                  <td className="small">
                    <code>{l.event_type}</code>
                    <div className="mute">
                      <code>{l.event_id}</code>
                    </div>
                  </td>
                  <td>
                    <span className={`chip ${LOG_STATUS[l.status].cls}`}>{LOG_STATUS[l.status].label}</span>
                    {l.dead_lettered ? (
                      <div>
                        <span className="chip c-bad" style={{ marginTop: 4 }}>
                          Đã vào hàng chờ lỗi
                        </span>
                      </div>
                    ) : null}
                  </td>
                  <td className="num">{formatInt(l.record_count)}</td>
                  <td className="num">{formatInt(l.delivery_count)}</td>
                  <td className="small">{l.error_code ? <code>{l.error_code}</code> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <LoadMore hasMore={log.hasMore} loading={log.loading} onClick={log.loadMore} />
    </section>
  );
}
