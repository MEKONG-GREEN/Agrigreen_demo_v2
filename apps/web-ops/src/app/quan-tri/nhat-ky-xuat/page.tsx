"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { EgressAnomaly, EgressChannel, EgressEvent } from "@/api/types";
import { CHANNEL_LABEL, COUNTERPARTY_LABEL, shortRef } from "@/apiclient/rules";
import { who } from "@/apiclient/ui";
import { useAuth } from "@/auth/AuthProvider";
import type { Principal } from "@/auth/principal";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { addDays, vnToday } from "@/lib/vnDate";
import { clientLabel, useClientOptions, type ClientOption } from "@/metering/clients";
import { formatCount } from "@/metering/money";
import { filterByCounterparty } from "@/metering/usage";

const CHANNELS = Object.keys(CHANNEL_LABEL) as EgressChannel[];

function counterpartyName(kind: string, ref: string, clients: readonly ClientOption[] | undefined, p: Principal | null): string {
  if (kind === "api_client") return clientLabel(clients, ref);
  return who(ref, p);
}

/**
 * Nhật ký xuất dữ liệu (DLP, ADR-0024): mọi lần dữ liệu rời nền tảng qua điểm kiểm soát chung — Public API,
 * gói MRV, xuất báo cáo, van MKG, chia sẻ đối tác. Chỉ dòng của tenant chủ dữ liệu của người xem (RLS).
 * Cảnh báo bất thường: lần đầu trong ngày vượt ngưỡng (× trung vị 28 ngày hoặc ngưỡng tuyệt đối).
 */
export default function EgressPage() {
  const [tab, setTab] = useState("events");
  return (
    <>
      <PageHead title="Nhật ký xuất dữ liệu" subtitle="Một điểm kiểm soát cho mọi đường xuất: nhật ký, hạn mức, cảnh báo bất thường — chỉ dữ liệu của tenant bạn" />
      <MfaNotice />
      <div className="alert alert-info" role="note" style={{ marginBottom: 16 }}>
        Chế độ <b>quan sát</b> (ĐTK 1) chỉ ghi và cảnh báo, không từ chối; chế độ <b>cưỡng chế</b> (Phase K) từ chối khi vượt hạn mức kênh. Nhật ký chỉ có mã tham chiếu và số lượng — không có
        nội dung dữ liệu.
      </div>
      <Tabs
        label="Nhật ký xuất"
        value={tab}
        onChange={setTab}
        tabs={[
          { id: "events", label: "Sổ xuất" },
          { id: "anomalies", label: "Cảnh báo bất thường" },
        ]}
      >
        <div style={{ paddingTop: 12 }}>{tab === "events" ? <EventsTab /> : <AnomaliesTab />}</div>
      </Tabs>
    </>
  );
}

function EventsTab() {
  const { adminApi, principal } = useAuth();
  const clients = useClientOptions();
  const today = vnToday();
  const [channel, setChannel] = useState<"" | EgressChannel>("");
  const [from, setFrom] = useState(addDays(today, -13));
  const [to, setTo] = useState(today);
  const [q, setQ] = useState("");
  const list = usePagedList<EgressEvent>(
    (cursor) => unwrap(adminApi.GET("/v1/metering/egress", { params: { query: { channel: channel || undefined, from: from || undefined, to: to || undefined, cursor, limit: 100 } } })),
    [adminApi, channel, from, to],
  );
  const shown = filterByCounterparty(list.items, q, (e) => counterpartyName(e.counterparty_kind, e.counterparty_ref, clients.data, principal));

  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="row" style={{ gap: 12, flexWrap: "wrap", alignItems: "flex-end" }} role="group" aria-label="Lọc sổ xuất">
        <div className="field">
          <label htmlFor="eg-ch">Kênh</label>
          <select id="eg-ch" className="input" value={channel} onChange={(e) => setChannel(e.target.value as "" | EgressChannel)}>
            <option value="">Mọi kênh</option>
            {CHANNELS.map((c) => (
              <option key={c} value={c}>
                {CHANNEL_LABEL[c]}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="eg-from">Từ ngày</label>
          <input id="eg-from" type="date" className="input" value={from} max={to || undefined} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="eg-to">Đến ngày</label>
          <input id="eg-to" type="date" className="input" value={to} min={from || undefined} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="eg-cp">Bên nhận (tên hoặc mã)</label>
          <input id="eg-cp" className="input" value={q} onChange={(e) => setQ(e.target.value)} placeholder="vd Nhà máy, 0192a5c0" />
        </div>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      {list.loading && list.items.length === 0 ? <Loading /> : null}
      {!list.loading && !list.error && shown.length === 0 ? <Empty>Không có lần xuất nào khớp bộ lọc.</Empty> : null}
      {shown.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Sổ xuất dữ liệu">
            <thead>
              <tr>
                <th scope="col">Thời điểm</th>
                <th scope="col">Kênh</th>
                <th scope="col">Bên nhận</th>
                <th scope="col">Sản phẩm</th>
                <th scope="col">Bản ghi</th>
                <th scope="col">Lũy kế ngày / hạn mức</th>
                <th scope="col">Quyết định</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((e) => (
                <tr key={e.id} data-channel={e.channel} data-decision={e.decision}>
                  <td className="small">
                    {formatDateTime(e.event_time)}
                    <div className="mute">ngày nghiệp vụ {formatDate(`${e.business_date}T00:00:00+07:00`)}</div>
                  </td>
                  <td>{CHANNEL_LABEL[e.channel]}</td>
                  <td className="small">
                    {counterpartyName(e.counterparty_kind, e.counterparty_ref, clients.data, principal)}
                    <div className="mute">
                      {COUNTERPARTY_LABEL[e.counterparty_kind]} · <code>{shortRef(e.counterparty_ref)}</code>
                    </div>
                  </td>
                  <td className="small">
                    <code>{e.product}</code> v{e.contract_version}
                  </td>
                  <td>{formatCount(e.records)}</td>
                  <td className="small">
                    {formatCount(e.daily_records)} / {e.daily_quota === 0 ? "không giới hạn riêng" : formatCount(e.daily_quota)}
                    {e.anomaly ? (
                      <div>
                        <span className="chip c-warn">bất thường</span>
                      </div>
                    ) : null}
                  </td>
                  <td className="small">
                    <span className={`chip ${e.decision === "allowed" ? "c-ok" : "c-bad"}`}>{e.decision === "allowed" ? "Cho phép" : "Từ chối"}</span>
                    <div className="mute">
                      {e.mode === "observe" ? "quan sát" : "cưỡng chế"}
                      {e.reason === "quota_exceeded" ? " · vượt hạn mức" : ""}
                    </div>
                  </td>
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

function AnomaliesTab() {
  const { adminApi, principal } = useAuth();
  const clients = useClientOptions();
  const [channel, setChannel] = useState<"" | EgressChannel>("");
  const list = usePagedList<EgressAnomaly>(
    (cursor) => unwrap(adminApi.GET("/v1/metering/egress/anomalies", { params: { query: { channel: channel || undefined, cursor, limit: 50 } } })),
    [adminApi, channel],
  );
  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="field" style={{ maxWidth: 280 }}>
        <label htmlFor="an-ch">Kênh</label>
        <select id="an-ch" className="input" value={channel} onChange={(e) => setChannel(e.target.value as "" | EgressChannel)}>
          <option value="">Mọi kênh</option>
          {CHANNELS.map((c) => (
            <option key={c} value={c}>
              {CHANNEL_LABEL[c]}
            </option>
          ))}
        </select>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      {list.loading && list.items.length === 0 ? <Loading /> : null}
      {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có cảnh báo nào.</Empty> : null}
      <ul className="list-plain stack" aria-label="Cảnh báo bất thường">
        {list.items.map((a) => (
          <li key={a.id} className="card" style={{ padding: 12 }} data-channel={a.channel}>
            <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap" }}>
              <b>
                {CHANNEL_LABEL[a.channel]} — {counterpartyName(a.counterparty_kind, a.counterparty_ref, clients.data, principal)}
              </b>
              <span className={`chip ${a.mode === "enforce" ? "c-bad" : "c-warn"}`}>{a.mode === "enforce" ? "Cưỡng chế" : "Quan sát"}</span>
            </div>
            <div className="small">
              Ngày {formatDate(`${a.business_date}T00:00:00+07:00`)}: <b>{formatCount(a.daily_records)}</b> bản ghi, ngưỡng {formatCount(a.threshold)} (
              {a.basis === "median" ? "× trung vị 28 ngày" : "ngưỡng tuyệt đối"}) · sản phẩm <code>{a.product}</code>
            </div>
            <div className="small mute">
              Phát hiện {formatDateTime(a.detected_at)} · {COUNTERPARTY_LABEL[a.counterparty_kind]} <code>{shortRef(a.counterparty_ref)}</code> · lần xuất{" "}
              <code>{shortRef(a.egress_event_id)}</code>
            </div>
          </li>
        ))}
      </ul>
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </div>
  );
}
