"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { MeteringUsageEvent } from "@/api/types";
import { formatPeriod, PRODUCT_LABEL, shortRef } from "@/apiclient/rules";
import { useAuth } from "@/auth/AuthProvider";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, KpiTile, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { vnToday } from "@/lib/vnDate";
import { useClientOptions } from "@/metering/clients";
import { formatCount } from "@/metering/money";
import { CALL_KIND_LABEL, periodRange, recentPeriods, summarizeUsage } from "@/metering/usage";
import { ParamsTable } from "@/metering/ParamsTable";

/**
 * Sử dụng Public API (pf/metering, ADR-0023): tổng lời gọi/đơn vị theo khách hàng × sản phẩm × kỳ (bộ đếm),
 * sổ sử dụng từng lời gọi (chứng từ, cursor), tham số đo lường hiệu lực. Chỉ xem.
 */
export default function UsagePage() {
  const clients = useClientOptions();
  const [clientId, setClientId] = useState<string>("");
  const [tab, setTab] = useState("usage");
  const today = vnToday();
  const periods = recentPeriods(today, 6);
  const [period, setPeriod] = useState(periods[0]!);
  const options = clients.data ?? [];
  const current = clientId || options[0]?.id || "";

  return (
    <>
      <PageHead title="Sử dụng Public API" subtitle="Sổ đo lường: mỗi lời gọi đã xác thực một dòng; đơn vị theo quy tắc metering.units.v1" />
      <MfaNotice />
      <div className="row" style={{ gap: 12, flexWrap: "wrap", marginBottom: 12 }}>
        <div className="field">
          <label htmlFor="us-client">Khách hàng</label>
          <select id="us-client" className="input" value={current} onChange={(e) => setClientId(e.target.value)} disabled={options.length === 0}>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
        {tab === "usage" ? (
          <div className="field">
            <label htmlFor="us-period">Kỳ</label>
            <select id="us-period" className="input" value={period} onChange={(e) => setPeriod(e.target.value)}>
              {periods.map((p) => (
                <option key={p} value={p}>
                  {formatPeriod(p)}
                </option>
              ))}
            </select>
          </div>
        ) : null}
      </div>
      {clients.error ? <ProblemAlert problem={clients.error} onRetry={clients.reload} /> : null}
      {clients.loading && !clients.data ? <Loading /> : null}
      {clients.data && options.length === 0 ? <Empty>Chưa có khách hàng nào có dữ liệu sử dụng.</Empty> : null}
      <Tabs
        label="Sử dụng Public API"
        value={tab}
        onChange={setTab}
        tabs={[
          { id: "usage", label: "Tổng theo kỳ" },
          { id: "events", label: "Sổ sử dụng (từng lời gọi)" },
          { id: "params", label: "Tham số đo lường" },
        ]}
      >
        <div style={{ paddingTop: 12 }}>
          {tab === "usage" && current ? <UsageSummary key={`${current}|${period}`} clientId={current} period={period} today={today} /> : null}
          {tab === "events" && current ? <UsageEvents key={current} clientId={current} /> : null}
          {tab === "params" ? <ParamsTable /> : null}
        </div>
      </Tabs>
    </>
  );
}

function UsageSummary({ clientId, period, today }: { clientId: string; period: string; today: string }) {
  const { adminApi } = useAuth();
  const range = periodRange(period, today);
  const usage = useAsync(() => unwrap(adminApi.GET("/v1/metering/usage", { params: { query: { client_id: clientId, from: range.from, to: range.to } } })), [adminApi, clientId, range.from, range.to]);
  const days = usage.data?.items ?? [];
  const sum = summarizeUsage(days);
  if (usage.error) return <ProblemAlert problem={usage.error} onRetry={usage.reload} />;
  if (usage.loading && !usage.data) return <Loading />;
  return (
    <div className="stack" style={{ gap: 12 }}>
      <div className="grid4" role="group" aria-label="Tổng sử dụng trong kỳ">
        <KpiTile label="Lời gọi trong kỳ" value={formatCount(sum.calls)} color="blue" icon="chart" />
        <KpiTile label="Đơn vị tính" value={formatCount(sum.units)} color="green" icon="report" footer="metering.units.v1" />
        <KpiTile label="Sản phẩm có dùng" value={formatCount(sum.products.length)} color="violet" icon="key" />
        <KpiTile label="Khoảng ngày" value={`${formatDate(range.from)} – ${formatDate(range.to)}`} color="orange" icon="clock" />
      </div>
      {days.length === 0 ? <Empty>Không có lời gọi nào trong kỳ.</Empty> : null}
      {sum.products.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Tổng theo sản phẩm">
            <thead>
              <tr>
                <th scope="col">Sản phẩm</th>
                <th scope="col">Số ngày có gọi</th>
                <th scope="col">Lời gọi</th>
                <th scope="col">Đơn vị</th>
              </tr>
            </thead>
            <tbody>
              {sum.products.map((p) => (
                <tr key={p.product}>
                  <td>
                    {PRODUCT_LABEL(p.product)} <code className="small">{p.product}</code>
                  </td>
                  <td>{formatCount(p.days)}</td>
                  <td>{formatCount(p.calls)}</td>
                  <td>{formatCount(p.units)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {days.length > 0 ? (
        <details>
          <summary className="small">Chi tiết theo ngày ({days.length} dòng)</summary>
          <div className="tbl-wrap" style={{ marginTop: 8 }}>
            <table className="tbl" aria-label="Sử dụng theo ngày">
              <thead>
                <tr>
                  <th scope="col">Ngày</th>
                  <th scope="col">Sản phẩm</th>
                  <th scope="col">Lời gọi</th>
                  <th scope="col">Đơn vị</th>
                </tr>
              </thead>
              <tbody>
                {[...days].reverse().map((d) => (
                  <tr key={`${d.date}|${d.product}`}>
                    <td>{formatDate(`${d.date}T00:00:00+07:00`)}</td>
                    <td>
                      <code className="small">{d.product}</code>
                    </td>
                    <td>{formatCount(d.calls)}</td>
                    <td>{formatCount(d.units)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      ) : null}
    </div>
  );
}

function UsageEvents({ clientId }: { clientId: string }) {
  const { adminApi } = useAuth();
  const list = usePagedList<MeteringUsageEvent>(
    (cursor) => unwrap(adminApi.GET("/v1/metering/usage-events", { params: { query: { client_id: clientId, cursor, limit: 50 } } })),
    [adminApi, clientId],
  );
  return (
    <div className="stack" style={{ gap: 8 }}>
      <p className="small mute">
        Chứng từ sổ sử dụng (mới nhất trước). Không có bí mật khóa hay giá trị truy vấn — chỉ tiền tố khóa, mẫu đường dẫn, mã trạng thái và số lượng.
      </p>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      {list.loading && list.items.length === 0 ? <Loading /> : null}
      {!list.loading && list.items.length === 0 && !list.error ? <Empty>Chưa có lời gọi nào.</Empty> : null}
      {list.items.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Sổ sử dụng">
            <thead>
              <tr>
                <th scope="col">#</th>
                <th scope="col">Thời điểm</th>
                <th scope="col">Khóa</th>
                <th scope="col">Loại</th>
                <th scope="col">Đường dẫn</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Dòng</th>
                <th scope="col">Đơn vị</th>
                <th scope="col">Độ trễ</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((e) => (
                <tr key={e.id} data-seq={e.seq}>
                  <td className="small">{e.seq}</td>
                  <td className="small">{formatDateTime(e.event_time)}</td>
                  <td className="small">
                    <code>{e.key_prefix}</code>
                    {e.auth_method !== "api_key" ? <div className="mute">OAuth</div> : null}
                  </td>
                  <td className="small">{CALL_KIND_LABEL[e.call_kind]}</td>
                  <td className="small">
                    <code>{e.route_template}</code>
                    {e.data_tenant_id ? <div className="mute">dữ liệu của {shortRef(e.data_tenant_id)}</div> : null}
                  </td>
                  <td className="small">
                    <span className={`chip ${e.status < 400 ? "c-ok" : e.status === 429 ? "c-warn" : "c-bad"}`}>{e.status}</span>
                    {e.problem_code ? <div className="mute">{e.problem_code}</div> : null}
                    {e.replayed ? <div className="mute">gọi lặp (0 đơn vị)</div> : null}
                  </td>
                  <td>{formatCount(e.rows_out)}</td>
                  <td>{formatCount(e.units)}</td>
                  <td className="small">{formatCount(e.latency_ms)} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </div>
  );
}
