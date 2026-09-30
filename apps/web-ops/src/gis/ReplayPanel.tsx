"use client";

import { useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { GisLayerEvent, GisLayerName, GisReplay } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { addDays, vnToday } from "@/lib/vnDate";
import { activeCount, diffAttributes, featureTitle, GIS_LAYERS, LAYER_NAME, OP_LABEL, snapshotBadge, validateReplayDate } from "./replay";

/**
 * Xem lại theo ngày (GIS US-REPLAY-01), dòng thời gian thay đổi lớp (GIS US-EVL-01) với bảng trước/sau,
 * huy hiệu ảnh chụp cuối ngày (GIS US-SNAP-01). Trạng thái as-of do máy chủ dựng; trang chỉ hiển thị.
 */
export function ReplayPanel({ replay, onReplay }: { replay: GisReplay | null; onReplay: (r: GisReplay | null) => void }) {
  const { api } = useAuth();
  const today = vnToday();
  const [input, setInput] = useState("");
  const [date, setDate] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [layer, setLayer] = useState("");
  const [selected, setSelected] = useState<GisLayerEvent | null>(null);

  const rep = useAsync(async () => {
    if (!date) return null;
    const r = await unwrap(api.GET("/v1/gis/replay", { params: { query: { date, geometry: true } } }));
    onReplay(r);
    return r;
  }, [api, date]);
  const snaps = useAsync(
    () => (date && date < today ? unwrap(api.GET("/v1/gis/snapshots/{date}", { params: { path: { date } } })) : Promise.resolve(null)),
    [api, date, today],
  );
  const eventsTo = date ?? today;
  const events = useAsync(
    () =>
      collectPages<GisLayerEvent>(
        (cursor) =>
          unwrap(
            api.GET("/v1/gis/events", {
              params: { query: { from: addDays(eventsTo, -6), to: eventsTo, layer: (layer || undefined) as GisLayerName | undefined, cursor, limit: 200 } },
            }),
          ),
        1000,
      ),
    [api, eventsTo, layer],
  );
  const timeline = useMemo(() => [...(events.data ?? [])].reverse(), [events.data]);
  const badge = date ? snapshotBadge(date, today, snaps.data?.items, rep.data?.items) : null;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const v = validateReplayDate(input, today);
    setErr(v);
    if (v) return;
    setSelected(null);
    setDate(input);
  };
  const reset = () => {
    setDate(null);
    setInput("");
    setErr(null);
    onReplay(null);
  };

  return (
    <section className="card sec" aria-labelledby="replay-h" style={{ marginTop: 16 }} data-testid="gis-replay">
      <div className="sec-h" style={{ flexWrap: "wrap" }}>
        <h2 id="replay-h">Xem lại theo ngày và nhật ký thay đổi lớp</h2>
        {badge ? (
          badge.kind === "snapshot" ? (
            <span className="chip c-ok" data-testid="snapshot-badge">
              Ảnh chụp cuối ngày {formatDate(badge.date)}
              {badge.watermarkAt ? ` · mốc ${formatDateTime(badge.watermarkAt)}` : ""}
            </span>
          ) : badge.kind === "replay" ? (
            <span className="chip c-warn" data-testid="snapshot-badge">
              Chưa có ảnh chụp ngày này — dựng lại từ {badge.baseDate ? `ảnh chụp ${formatDate(badge.baseDate)} + nhật ký` : "nhật ký"}
            </span>
          ) : (
            <span className="chip c-ok" data-testid="snapshot-badge">
              Hôm nay — trạng thái hiện tại
            </span>
          )
        ) : null}
      </div>
      <form className="row" style={{ flexWrap: "wrap", gap: 10, alignItems: "flex-end" }} onSubmit={submit} aria-label="Xem lại bản đồ theo ngày" noValidate>
        <div className="field">
          <label htmlFor="gis-replay-date">Xem lại theo ngày</label>
          <input id="gis-replay-date" className="input" type="date" max={today} value={input} onChange={(e) => setInput(e.target.value)} />
        </div>
        <button type="submit" className="btn btn-p btn-sm">
          Xem lại
        </button>
        {date ? (
          <button type="button" className="btn btn-o btn-sm" onClick={reset}>
            Về hiện tại
          </button>
        ) : null}
      </form>
      {err ? (
        <p className="err" role="alert" style={{ marginTop: 6 }}>
          {err}
        </p>
      ) : null}
      {rep.error ? <ProblemAlert problem={rep.error} onRetry={rep.reload} /> : null}
      {rep.loading && date ? <Loading label="Đang dựng trạng thái các lớp…" /> : null}
      {replay && date ? (
        <div style={{ marginTop: 10 }}>
          <p className="notice" role="status" data-testid="replay-banner">
            Bản đồ đang hiển thị mọi lớp như hệ biết lúc hết ngày {formatDate(replay.business_date)} (mốc {formatDateTime(replay.cutoff)}) — chỉ đọc. Hình học lấy
            đúng phiên bản ranh giới có hiệu lực ngày đó.
          </p>
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Trạng thái các lớp theo ngày">
              <thead>
                <tr>
                  <th scope="col">Lớp</th>
                  <th scope="col" className="num">Đang dùng</th>
                  <th scope="col" className="num">Đã ngừng</th>
                  <th scope="col">Cách dựng</th>
                  <th scope="col">Thay đổi cuối cùng tính vào</th>
                </tr>
              </thead>
              <tbody>
                {replay.items.map((s) => (
                  <tr key={s.layer} data-layer={s.layer}>
                    <td>{LAYER_NAME[s.layer] ?? s.layer}</td>
                    <td className="num" data-testid={`replay-count-${s.layer}`}>
                      {formatInt(activeCount(s))}
                    </td>
                    <td className="num">{formatInt(s.features.length - activeCount(s))}</td>
                    <td className="small">
                      {s.basis === "snapshot" ? "Ảnh chụp cuối ngày" : s.base_snapshot_date ? `Ảnh chụp ${formatDate(s.base_snapshot_date)} + nhật ký` : "Dựng từ nhật ký"}
                    </td>
                    <td className="small">{s.watermark ? formatDateTime(s.watermark.recorded_at) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}

      <div className="grid2" style={{ marginTop: 14, alignItems: "start" }}>
        <div>
          <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
            <h3 className="small" style={{ fontSize: 14 }}>
              Dòng thời gian thay đổi ({formatDate(addDays(eventsTo, -6))} – {formatDate(eventsTo)})
            </h3>
            <div className="field">
              <label htmlFor="gis-ev-layer">Lớp</label>
              <select id="gis-ev-layer" className="select" value={layer} onChange={(e) => setLayer(e.target.value)}>
                <option value="">Tất cả lớp</option>
                {GIS_LAYERS.map((l) => (
                  <option key={l} value={l}>
                    {LAYER_NAME[l]}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {events.error ? <ProblemAlert problem={events.error} onRetry={events.reload} /> : null}
          {events.loading && !events.data ? <Loading /> : null}
          {events.data && timeline.length === 0 ? <p className="small mute">Không có thay đổi nào trong 7 ngày này.</p> : null}
          <ol className="list-plain" aria-label="Nhật ký thay đổi lớp" style={{ maxHeight: 360, overflowY: "auto" }}>
            {timeline.map((e) => {
              const attrs = (e.after ?? e.before) as Record<string, unknown> | null;
              return (
                <li key={e.id} data-op={e.op}>
                  <button
                    type="button"
                    style={{ textAlign: "left", width: "100%", padding: "6px 0", fontWeight: selected?.id === e.id ? 700 : undefined }}
                    aria-pressed={selected?.id === e.id}
                    onClick={() => setSelected(e)}
                  >
                    <span className="small mute">{formatDateTime(e.recorded_at)}</span> · <b style={{ display: "inline" }}>{OP_LABEL[e.op]}</b> {LAYER_NAME[e.layer] ?? e.layer}:{" "}
                    {featureTitle(e.feature_ref, attrs)}
                    <span className="small mute"> · nguồn {e.source_module}</span>
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
        <div aria-live="polite">
          {selected ? (
            <section aria-labelledby="ev-diff-h" data-testid="event-diff">
              <h3 id="ev-diff-h" className="small" style={{ fontSize: 14 }}>
                Trước / sau: {OP_LABEL[selected.op]} {LAYER_NAME[selected.layer] ?? selected.layer} {featureTitle(selected.feature_ref, (selected.after ?? selected.before) as Record<string, unknown> | null)}
              </h3>
              <p className="small mute">
                Sự kiện nguồn <code>{selected.source_type}</code> ({selected.source_module}) · xảy ra {formatDateTime(selected.event_time)} · ghi nhận {formatDateTime(selected.recorded_at)}
              </p>
              <table className="tbl" aria-label="Bảng trước và sau thay đổi">
                <thead>
                  <tr>
                    <th scope="col">Thuộc tính</th>
                    <th scope="col">Trước</th>
                    <th scope="col">Sau</th>
                  </tr>
                </thead>
                <tbody>
                  {diffAttributes(selected.before as Record<string, unknown> | null, selected.after as Record<string, unknown> | null).map((d) => (
                    <tr key={d.key} data-changed={d.changed} style={d.changed ? { background: "var(--amber-bg)" } : undefined}>
                      <td>
                        <code>{d.key}</code>
                        {d.changed ? <span className="sr-only"> (đã đổi)</span> : null}
                      </td>
                      <td className="small">{d.before}</td>
                      <td className="small">{d.after}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          ) : (
            <p className="small mute">Chọn một dòng trong nhật ký để xem bảng trước/sau.</p>
          )}
        </div>
      </div>
      <p className="small mute" style={{ marginTop: 12 }} data-testid="retention-note">
        Lưu trữ theo tầng (GIS US-RETAIN-01, RB-1): nhật ký lớp chỉ ghi thêm, không bao giờ xoá; ảnh chụp cũ được nén/chuyển sang tầng lưu trữ lạnh, vẫn phát lại được.
        Số ngày của từng tầng là tham số tạm, chờ khách chốt.
      </p>
    </section>
  );
}
