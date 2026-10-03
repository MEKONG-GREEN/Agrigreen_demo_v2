"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import type { MeteringStatement, MeteringStatementJob, MeteringStatementRequest, MeteringVerify } from "@/api/types";
import {
  approveStatementBlock,
  canDownloadStatement,
  formatPeriod,
  lastEndedPeriod,
  periodEnded,
  PRODUCT_LABEL,
  requestStatementBlock,
  STATEMENT_STATUS,
  statementFileName,
  shortRef,
} from "@/apiclient/rules";
import { Chip, GuardedButton, who } from "@/apiclient/ui";
import { useAuth } from "@/auth/AuthProvider";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { saveBlob } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { JOB_STATUS, pollUntil } from "@/lib/poll";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { vnToday } from "@/lib/vnDate";
import { clientLabel, useClientOptions, type ClientOption } from "@/metering/clients";
import { formatCount, formatUnitPrice, formatVnd } from "@/metering/money";
import { recentPeriods } from "@/metering/usage";

/**
 * Bảng kê sử dụng theo tháng (pf/metering, ADR-0023 mục 4): billing_officer yêu cầu lập (202 + job, ảnh chụp
 * bất biến có băm), billing_approver KHÁC người yêu cầu duyệt (đóng băng), kiểm lại băm, tải CSV tất định
 * (qua điểm kiểm soát xuất kênh report_export). Lập hóa đơn NGOÀI hệ thống. Sửa = bảng kê bù.
 */
export default function StatementsPage() {
  const { adminApi, principal } = useAuth();
  const clients = useClientOptions();
  const [period, setPeriod] = useState("");
  const [version, setVersion] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const list = usePagedList<MeteringStatement>(
    (cursor) => unwrap(adminApi.GET("/v1/metering/statements", { params: { query: { period: period || undefined, cursor, limit: 50 } } })),
    [adminApi, period, version],
  );
  const reload = () => setVersion((v) => v + 1);
  const periods = recentPeriods(vnToday(), 12);
  const canRequest = requestStatementBlock(principal) === null;

  return (
    <>
      <PageHead title="Bảng kê sử dụng" subtitle="Chứng từ lập hóa đơn ngoài hệ thống — kỳ tháng, ảnh chụp bất biến có băm, người duyệt khác người yêu cầu" />
      <MfaNotice />
      <div className="alert alert-warn" role="note" style={{ marginBottom: 16 }}>
        Giá là <b>tùy chọn, chờ khách</b> (Q5-4): khi chưa đặt tham số giá, bảng kê chỉ có lời gọi và đơn vị. Tiền hiển thị từ chuỗi thập phân của máy chủ, không làm tròn lại.
      </div>
      {canRequest ? <RequestForm clients={clients.data ?? []} onDone={(id) => {
            reload();
            if (id) setSelected(id);
          }} /> : null}
      <section className="card sec" aria-labelledby="st-list" style={{ marginTop: 16 }}>
        <div className="sec-h" style={{ flexWrap: "wrap", gap: 8 }}>
          <h2 id="st-list">Các bảng kê</h2>
          <div className="row">
            <label className="small" htmlFor="st-filter">
              Kỳ
            </label>
            <select id="st-filter" className="input" value={period} onChange={(e) => setPeriod(e.target.value)}>
              <option value="">Mọi kỳ</option>
              {periods.map((p) => (
                <option key={p} value={p}>
                  {formatPeriod(p)}
                </option>
              ))}
            </select>
          </div>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.loading && list.items.length === 0 ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Chưa có bảng kê nào.</Empty> : null}
        {list.items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Bảng kê sử dụng">
              <thead>
                <tr>
                  <th scope="col">Kỳ</th>
                  <th scope="col">Khách hàng</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Đơn vị</th>
                  <th scope="col">Tổng tiền</th>
                  <th scope="col">Yêu cầu / duyệt</th>
                  <th scope="col">
                    <span className="sr-only">Thao tác</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {list.items.map((s) => (
                  <tr key={s.id} data-statement={s.id} data-status={s.status}>
                    <td>{formatPeriod(s.period)}</td>
                    <td>{clientLabel(clients.data, s.client_id)}</td>
                    <td>
                      <Chip s={STATEMENT_STATUS[s.status]} />
                      {s.superseded_by ? <div className="small mute">đã bị thay bởi {shortRef(s.superseded_by)}</div> : null}
                      {s.supersedes_id ? <div className="small mute">bảng kê bù của {shortRef(s.supersedes_id)}</div> : null}
                    </td>
                    <td>{formatCount(s.lines.reduce((a, l) => a + l.units, 0))}</td>
                    <td>{s.total_vnd ? formatVnd(s.total_vnd) : <span className="mute small">chưa có giá</span>}</td>
                    <td className="small">
                      {who(s.requested_by, principal)}
                      <div className="mute">{s.approved_by ? `Duyệt: ${who(s.approved_by, principal)}` : "Chưa duyệt"}</div>
                    </td>
                    <td>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(s.id)} aria-label={`Xem bảng kê ${formatPeriod(s.period)} — ${clientLabel(clients.data, s.client_id)}`}>
                        Chi tiết
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </section>
      {selected ? <StatementDetail key={selected} id={selected} clients={clients.data ?? []} onChanged={reload} onClose={() => setSelected(null)} /> : null}
    </>
  );
}

function RequestForm({ clients, onDone }: { clients: readonly ClientOption[]; onDone: (statementId: string | null) => void }) {
  const { adminApi } = useAuth();
  const now = new Date();
  const periods = recentPeriods(vnToday(now), 12).filter((p) => periodEnded(p, now));
  const [period, setPeriod] = useState(lastEndedPeriod(now));
  const [clientId, setClientId] = useState("");
  const [correct, setCorrect] = useState(false);
  const [supersedes, setSupersedes] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [job, setJob] = useState<MeteringStatementJob | null>(null);
  const abort = useRef<AbortController | null>(null);
  useEffect(() => () => abort.current?.abort(), []);

  const candidates = useAsync(
    () =>
      correct && clientId
        ? unwrap(adminApi.GET("/v1/metering/statements", { params: { query: { period, client_id: clientId, limit: 50 } } })).then((p) => p.items.filter((s) => !s.superseded_by))
        : Promise.resolve([] as MeteringStatement[]),
    [adminApi, correct, clientId, period],
  );

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const body: MeteringStatementRequest = { period, client_id: clientId || null };
    if (correct) {
      body.supersedes_id = supersedes || null;
      body.correction_reason = reason.trim() || null;
    }
    setBusy(true);
    setJob(null);
    try {
      const j = await unwrap(adminApi.POST("/v1/metering/statements", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body }));
      setJob(j);
      abort.current?.abort();
      const ctrl = new AbortController();
      abort.current = ctrl;
      const last = await pollUntil(
        () => unwrap(adminApi.GET("/v1/metering/statement-jobs/{id}", { params: { path: { id: j.id } } })),
        (v) => v.status !== "queued",
        { intervalMs: 700, timeoutMs: 60_000, onUpdate: setJob, signal: ctrl.signal },
      );
      if (!ctrl.signal.aborted) onDone(last.status === "completed" ? (last.statement_ids[0] ?? null) : null);
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card sec stack" onSubmit={submit} aria-labelledby="st-new" noValidate>
      <h2 id="st-new" style={{ fontSize: 15.5 }}>
        Yêu cầu lập bảng kê
      </h2>
      <div className="row" style={{ gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}>
        <div className="field">
          <label htmlFor="st-period">Kỳ (đã kết thúc, giờ Việt Nam)</label>
          <select id="st-period" className="input" value={period} onChange={(e) => setPeriod(e.target.value)}>
            {periods.map((p) => (
              <option key={p} value={p}>
                {formatPeriod(p)}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="st-client">Khách hàng</label>
          <select id="st-client" className="input" value={clientId} onChange={(e) => setClientId(e.target.value)}>
            <option value="">Mọi khách hàng có sử dụng trong kỳ</option>
            {clients.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
        </div>
        <label className="row small">
          <input type="checkbox" checked={correct} onChange={(e) => setCorrect(e.target.checked)} /> Bảng kê bù (thay bảng kê đã có)
        </label>
      </div>
      {correct ? (
        <div className="grid2">
          <div className="field">
            <label htmlFor="st-sup">Bảng kê cần thay</label>
            <select id="st-sup" className="input" value={supersedes} onChange={(e) => setSupersedes(e.target.value)} disabled={!clientId}>
              <option value="">{clientId ? "— chọn —" : "Chọn khách hàng trước"}</option>
              {(candidates.data ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {shortRef(s.id)} · {STATEMENT_STATUS[s.status].label}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="st-reason">Lý do sửa (10–500 ký tự)</label>
            <textarea id="st-reason" className="input" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
          </div>
        </div>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {job ? (
        <div className={`alert ${job.status === "failed" ? "alert-bad" : "alert-info"}`} role="status" aria-live="polite" data-testid="statement-job">
          <span>
            Yêu cầu {shortRef(job.id)} — {formatPeriod(job.period)}: <b>{JOB_STATUS[job.status] ?? job.status}</b>
            {job.status === "completed" ? ` · ${job.statement_ids.length} bảng kê` : ""}
            {job.status === "completed" && job.statement_ids.length === 0 ? " (không có sử dụng trong kỳ)" : ""}
            {job.error ? ` · ${job.error}` : ""}
          </span>
        </div>
      ) : null}
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lập…" : "Yêu cầu lập bảng kê"}
        </button>
      </div>
    </form>
  );
}

function StatementDetail({ id, clients, onChanged, onClose }: { id: string; clients: readonly ClientOption[]; onChanged: () => void; onClose: () => void }) {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const st = useAsync(() => unwrap(adminApi.GET("/v1/metering/statements/{id}", { params: { path: { id } } })), [adminApi, id, version]);
  const [busy, setBusy] = useState<"approve" | "verify" | "csv" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [verify, setVerify] = useState<MeteringVerify | null>(null);
  const [downloaded, setDownloaded] = useState<string | null>(null);
  const s = st.data;

  const approve = async () => {
    setBusy("approve");
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/metering/statements/{id}/approve", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      setVersion((v) => v + 1);
      onChanged();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };
  const doVerify = async () => {
    setBusy("verify");
    setProblem(null);
    try {
      setVerify(await unwrap(adminApi.GET("/v1/metering/statements/{id}/verify", { params: { path: { id } } })));
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };
  const csv = async () => {
    if (!s) return;
    setBusy("csv");
    setProblem(null);
    try {
      const res = await adminApi.GET("/v1/metering/statements/{id}/export.csv", { params: { path: { id } }, parseAs: "blob" });
      if (!res.response.ok) throw await parseProblemResponse(res.response);
      const blob = res.data as Blob;
      const name = statementFileName(s);
      saveBlob(blob, name);
      setDownloaded(`${name} · SHA-256 ảnh chụp ${res.response.headers.get("AG-Snapshot-SHA256")?.slice(0, 12) ?? "—"}…`);
    } catch (x) {
      setProblem(x instanceof ApiProblem ? x : toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };

  return (
    <section className="card sec" aria-labelledby="st-detail" style={{ marginTop: 16 }} data-testid="statement-detail">
      <div className="sec-h" style={{ flexWrap: "wrap", gap: 8 }}>
        <h2 id="st-detail">{s ? `Bảng kê ${formatPeriod(s.period)} — ${clientLabel(clients, s.client_id)}` : "Chi tiết bảng kê"}</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng chi tiết
        </button>
      </div>
      {st.error ? <ProblemAlert problem={st.error} onRetry={st.reload} /> : null}
      {st.loading && !s ? <Loading /> : null}
      {s ? (
        <>
          <dl className="kv small">
            <dt>Trạng thái</dt>
            <dd>
              <Chip s={STATEMENT_STATUS[s.status]} />
              {s.superseded_by ? <span className="mute"> · đã bị thay bởi {shortRef(s.superseded_by)}</span> : null}
            </dd>
            {s.supersedes_id ? (
              <>
                <dt>Bảng kê bù của</dt>
                <dd>
                  {shortRef(s.supersedes_id)} — {s.correction_reason}
                </dd>
              </>
            ) : null}
            <dt>Yêu cầu lập</dt>
            <dd>
              {who(s.requested_by, principal)} · {formatDateTime(s.requested_at)}
            </dd>
            <dt>Duyệt</dt>
            <dd>{s.approved_by ? `${who(s.approved_by, principal)} · ${formatDateTime(s.approved_at)}` : "Chưa duyệt"}</dd>
            <dt>Dòng sổ</dt>
            <dd>
              #{s.first_seq} – #{s.last_seq} · quy tắc <code>{s.rule_version}</code>
            </dd>
            <dt>SHA-256 ảnh chụp</dt>
            <dd>
              <code className="link-once">{s.snapshot_sha256}</code>
            </dd>
            <dt>Đầu chuỗi sổ</dt>
            <dd>
              <code className="link-once">{s.chain_head_hash}</code>
            </dd>
          </dl>
          <div className="tbl-wrap" style={{ marginTop: 12 }}>
            <table className="tbl" aria-label="Dòng bảng kê">
              <thead>
                <tr>
                  <th scope="col">Sản phẩm</th>
                  <th scope="col">Lớp trạng thái</th>
                  <th scope="col">Lời gọi</th>
                  <th scope="col">Đơn vị</th>
                  <th scope="col">Đơn giá</th>
                  <th scope="col">Thành tiền</th>
                </tr>
              </thead>
              <tbody>
                {s.lines.map((l) => (
                  <tr key={`${l.product}|${l.status_class}`}>
                    <td>
                      {PRODUCT_LABEL(l.product)} <code className="small">{l.product}</code>
                    </td>
                    <td>{l.status_class}</td>
                    <td>{formatCount(l.calls)}</td>
                    <td>{formatCount(l.units)}</td>
                    <td>{formatUnitPrice(l.unit_price_vnd)}</td>
                    <td>{formatVnd(l.amount_vnd)}</td>
                  </tr>
                ))}
                {s.subscription_vnd ? (
                  <tr>
                    <td colSpan={5}>Thuê bao tháng</td>
                    <td>{formatVnd(s.subscription_vnd)}</td>
                  </tr>
                ) : null}
                <tr>
                  <th scope="row" colSpan={2}>
                    Tổng
                  </th>
                  <td>{formatCount(s.lines.reduce((a, l) => a + l.calls, 0))}</td>
                  <td>{formatCount(s.lines.reduce((a, l) => a + l.units, 0))}</td>
                  <td className="small mute">{s.price_version ?? "chưa có giá"}</td>
                  <td data-testid="statement-total">
                    <b>{formatVnd(s.total_vnd)}</b>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div className="row" role="group" aria-label="Thao tác bảng kê" style={{ gap: 12, flexWrap: "wrap", alignItems: "flex-start", marginTop: 12 }}>
            {s.status === "draft" && !s.superseded_by ? (
              <GuardedButton id={`st-ap-${s.id}`} label="Duyệt bảng kê" busyLabel="Đang duyệt…" block={approveStatementBlock(s, principal)} busy={busy === "approve"} onClick={() => void approve()} />
            ) : null}
            <button type="button" className="btn btn-o btn-sm" disabled={!!busy} onClick={() => void doVerify()}>
              {busy === "verify" ? "Đang kiểm…" : "Kiểm lại băm từ sổ"}
            </button>
            {canDownloadStatement(principal) ? (
              <button type="button" className="btn btn-o btn-sm" disabled={!!busy} onClick={() => void csv()}>
                {busy === "csv" ? "Đang tải…" : "Tải CSV"}
              </button>
            ) : null}
          </div>
          {verify ? (
            <div className={`alert ${verify.match ? "alert-info" : "alert-bad"}`} role="status" style={{ marginTop: 8 }} data-testid="verify-result">
              {verify.match ? "Khớp: ảnh chụp dựng lại từ sổ có cùng SHA-256." : "KHÔNG khớp: ảnh chụp dựng lại khác bản đã lưu — báo vận hành."}{" "}
              <code className="small">{verify.rebuilt_sha256.slice(0, 16)}…</code>
            </div>
          ) : null}
          {downloaded ? (
            <p className="small mute" role="status">
              Đã tải {downloaded}. Lần tải được ghi vào nhật ký xuất (kênh Xuất báo cáo).
            </p>
          ) : null}
          {problem ? <ProblemAlert problem={problem} /> : null}
        </>
      ) : null}
    </section>
  );
}
