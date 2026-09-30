"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { HtxCollectionStatement } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { responseBody, SCHEDULE_STATUS, STATEMENT_STATE, statementState, summarizeStatements, validateResponse, type ResponseDraft } from "@/htx/collection";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { formatDate, formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

/**
 * Thu gom rơm MKG phía HTX (G3-2, RB-6 chiều mua). MKG gửi lịch gom và bảng kê số cuộn đã gom
 * trên từng thửa qua van công bố; quản lý HTX xác nhận hoặc tranh chấp (số cuộn + lý do) theo
 * bản mới nhất — MKG kéo phản hồi về qua Application API công khai. Chỉ số cuộn: không có khối
 * lượng cân, giá hay khách hàng của MKG.
 */
export default function CollectionPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const [status, setStatus] = useState<"" | "pending" | "confirmed" | "disputed">("");
  const [open, setOpen] = useState<HtxCollectionStatement | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const schedules = useAsync(
    () =>
      collectPages(
        (cursor) => unwrap(api.GET("/v1/cropcycle/htx-collection/schedules", { params: { query: { cooperative_ref: scope.coopRef || undefined, cursor, limit: 200 } } })),
        500,
      ),
    [api, scope.coopRef],
  );
  const statements = useAsync(
    () =>
      collectPages(
        (cursor) =>
          unwrap(
            api.GET("/v1/cropcycle/htx-collection/statements", {
              params: { query: { cooperative_ref: scope.coopRef || undefined, status: status || undefined, cursor, limit: 200 } },
            }),
          ),
        1000,
      ),
    [api, scope.coopRef, status],
  );
  const items = statements.data ?? [];
  const sum = summarizeStatements(items);

  return (
    <>
      <PageHead title="Thu gom rơm MKG" subtitle="Lịch gom và bảng kê số cuộn Mekong Green gửi cho HTX — xác nhận hoặc tranh chấp số cuộn đã gom trên từng lô" />
      <div className="alert alert-info small" style={{ marginBottom: 14 }} role="note">
        <Icon name="info" />
        <span>
          Bảng kê chỉ có số cuộn MKG đã xếp ghe/xe trên từng lô. Khi MKG sửa số, bảng kê có phiên bản mới và HTX cần xác nhận lại. Phản hồi được gửi về MKG để đối soát chiều mua.
        </span>
      </div>
      {notice ? (
        <div className="alert alert-ok small" role="status" style={{ marginBottom: 14 }}>
          {notice}
        </div>
      ) : null}

      <section className="card sec" aria-labelledby="sched-h">
        <h2 id="sched-h" className="h3">
          Lịch gom
        </h2>
        <div className="row" style={{ marginBottom: 10 }}>
          <CoopPicker scope={scope} />
        </div>
        {schedules.error ? <ProblemAlert problem={schedules.error} onRetry={schedules.reload} /> : null}
        {schedules.loading && !schedules.data ? <Loading /> : null}
        {(schedules.data ?? []).length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Lịch gom rơm của MKG</caption>
              <thead>
                <tr>
                  <th scope="col">Khung ngày gom</th>
                  <th scope="col">Xã</th>
                  <th scope="col">Trạng thái</th>
                  {scope.isManager ? null : <th scope="col">HTX</th>}
                  <th scope="col">Cập nhật</th>
                </tr>
              </thead>
              <tbody>
                {(schedules.data ?? []).map((s) => (
                  <tr key={s.schedule_ref}>
                    <td>
                      {formatDate(s.window_from)} – {formatDate(s.window_to)}
                    </td>
                    <td>{s.commune_code}</td>
                    <td>
                      <span className={`chip ${SCHEDULE_STATUS[s.status].cls}`}>{SCHEDULE_STATUS[s.status].text}</span>
                    </td>
                    {scope.isManager ? null : <td>{scope.coopName(s.cooperative_ref)}</td>}
                    <td className="small mute">
                      {formatDateTime(s.published_at)} · bản {s.revision}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {!schedules.loading && !schedules.error && (schedules.data ?? []).length === 0 ? <Empty>Chưa có lịch gom của MKG.</Empty> : null}
      </section>

      <section className="card sec" aria-labelledby="stmt-h">
        <h2 id="stmt-h" className="h3">
          Bảng kê số cuộn
        </h2>
        <form className="row" role="search" aria-label="Lọc bảng kê" style={{ marginBottom: 12, alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
          <div className="field">
            <label htmlFor="st-status">Trạng thái</label>
            <select id="st-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="">Tất cả</option>
              <option value="pending">Chờ HTX xác nhận</option>
              <option value="confirmed">Đã xác nhận</option>
              <option value="disputed">Đang tranh chấp</option>
            </select>
          </div>
          <span className="small mute" role="status" style={{ paddingBottom: 8 }}>
            {sum.total} bảng kê · {sum.pending} chờ xác nhận{sum.disputed ? ` · ${sum.disputed} tranh chấp` : ""} · {sum.rolls.toLocaleString("vi-VN")} cuộn
          </span>
        </form>
        {statements.error ? <ProblemAlert problem={statements.error} onRetry={statements.reload} /> : null}
        {statements.loading && !statements.data ? <Loading /> : null}
        {items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Bảng kê số cuộn MKG đã gom</caption>
              <thead>
                <tr>
                  <th scope="col">Lô ruộng</th>
                  <th scope="col">Ngày gom</th>
                  <th scope="col">Số cuộn MKG</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Phản hồi của HTX</th>
                  <th scope="col">Thao tác</th>
                </tr>
              </thead>
              <tbody>
                {items.map((s) => {
                  const st = statementState(s);
                  return (
                    <tr key={s.statement_ref}>
                      <td>
                        <b>{s.plot_code ?? s.plot_ref.slice(0, 8)}</b>
                        {scope.isManager ? null : <div className="mute small">{scope.coopName(s.cooperative_ref)}</div>}
                      </td>
                      <td>{formatDate(s.collected_on)}</td>
                      <td>
                        {s.rolls.toLocaleString("vi-VN")} cuộn
                        <div className="mute small">bản {s.revision}</div>
                      </td>
                      <td>
                        <span className={`chip ${STATEMENT_STATE[st].cls}`}>{STATEMENT_STATE[st].text}</span>
                      </td>
                      <td className="small">
                        {s.response ? (
                          <>
                            {s.response.outcome === "confirmed" ? "Xác nhận" : `Tranh chấp: HTX đếm ${s.response.htx_rolls} cuộn`} (bản {s.response.revision})
                            {s.response.reason ? <div className="mute">Lý do: {s.response.reason}</div> : null}
                          </>
                        ) : (
                          <span className="mute">—</span>
                        )}
                      </td>
                      <td>
                        {scope.isManager ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(s)} aria-label={`Phản hồi bảng kê lô ${s.plot_code ?? s.plot_ref}`}>
                            {st === "pending" || st === "revised" ? "Xác nhận / tranh chấp" : "Sửa phản hồi"}
                          </button>
                        ) : (
                          <span className="mute small">Chỉ quản lý HTX phản hồi</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
        {!statements.loading && !statements.error && items.length === 0 ? <Empty>Chưa có bảng kê số cuộn.</Empty> : null}
      </section>

      {open ? (
        <RespondDialog
          statement={open}
          onClose={() => setOpen(null)}
          onDone={(msg) => {
            setOpen(null);
            setNotice(msg);
            statements.reload();
          }}
          onStale={() => statements.reload()}
        />
      ) : null}
    </>
  );
}

function RespondDialog({
  statement: s,
  onClose,
  onDone,
  onStale,
}: {
  statement: HtxCollectionStatement;
  onClose: () => void;
  onDone: (msg: string) => void;
  onStale: () => void;
}) {
  const { api } = useAuth();
  const [d, setD] = useState<ResponseDraft>({ outcome: "confirmed", htxRolls: "", reason: "" });
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const err = validateResponse(s, d);
  const edit = (patch: Partial<ResponseDraft>) => {
    setD((x) => ({ ...x, ...patch }));
    setKey(newIdempotencyKey());
  };
  const submit = async () => {
    setTouched(true);
    if (err) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        api.POST("/v1/cropcycle/htx-collection/statements/{ref}/responses", {
          params: { path: { ref: s.statement_ref }, header: { "Idempotency-Key": key } },
          body: responseBody(s, d),
        }),
      );
      onDone(d.outcome === "confirmed" ? `Đã xác nhận ${s.rolls} cuộn — lô ${s.plot_code ?? ""}.` : `Đã gửi tranh chấp — lô ${s.plot_code ?? ""}.`);
    } catch (e) {
      const p = toApiProblem(e);
      setProblem(p);
      if (p.code === "cropcycle.collection_statement_stale") onStale();
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={`Phản hồi bảng kê lô ${s.plot_code ?? s.plot_ref.slice(0, 8)}`}
      onClose={onClose}
      actions={
        <button type="button" className={`btn ${d.outcome === "disputed" ? "btn-d" : "btn-p"} btn-sm`} disabled={busy} onClick={() => void submit()}>
          {busy ? "Đang gửi…" : d.outcome === "confirmed" ? "Gửi xác nhận" : "Gửi tranh chấp"}
        </button>
      }
    >
      <div className="stack" style={{ gap: 10 }}>
        <p className="small">
          MKG ghi <b>{s.rolls} cuộn</b> gom ngày {formatDate(s.collected_on)} (bản {s.revision}).
        </p>
        <fieldset className="field">
          <legend>Kết quả</legend>
          <label className="row small">
            <input type="radio" name="outcome" checked={d.outcome === "confirmed"} onChange={() => edit({ outcome: "confirmed" })} /> Xác nhận đúng số MKG
          </label>
          <label className="row small">
            <input type="radio" name="outcome" checked={d.outcome === "disputed"} onChange={() => edit({ outcome: "disputed" })} /> Tranh chấp — HTX đếm khác
          </label>
        </fieldset>
        {d.outcome === "disputed" ? (
          <div className="field">
            <label htmlFor="resp-rolls">Số cuộn HTX đếm được</label>
            <input id="resp-rolls" className="input" inputMode="numeric" value={d.htxRolls} onChange={(e) => edit({ htxRolls: e.target.value })} />
          </div>
        ) : null}
        <div className="field">
          <label htmlFor="resp-reason">Lý do{d.outcome === "disputed" || s.response?.revision === s.revision ? "" : " (tùy chọn)"}</label>
          <textarea id="resp-reason" className="textarea" value={d.reason} aria-invalid={touched && !!err} aria-describedby="resp-err" onChange={(e) => edit({ reason: e.target.value })} />
        </div>
        {touched && err ? (
          <span id="resp-err" className="err" role="alert">
            {err}
          </span>
        ) : null}
        {problem ? <ProblemAlert problem={problem} /> : null}
      </div>
    </Dialog>
  );
}
