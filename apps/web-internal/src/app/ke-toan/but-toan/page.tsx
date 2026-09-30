"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import { parseProblemResponse } from "@/api/problem";
import type { AcctbridgeJob, AcctbridgeJournal } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { KV, Money, SoD, useJobPoll, When } from "@/components/g4kit";
import { ActionStatus, Chip, Ref, Section, SelectField, SelfHint, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { AB_FAILURE_LABEL, AB_JOB_STATUS_LABEL, COST_CATEGORY_LABEL, JOURNAL_STATUS_LABEL, PUSH_OUTCOME_LABEL, SIDE_LABEL } from "@/lib/g4labels";
import { journalTotals, shortHash } from "@/lib/money";
import { sha256Text } from "@/lib/sha256";
import { useAsync } from "@/lib/useAsync";

function JobWatch({ job, onDone }: { job: AcctbridgeJob; onDone: () => void }) {
  const { api } = useAuth();
  const poll = useJobPoll(
    async () => (await unwrap(api.GET("/v1/acctbridge/jobs/{id}", { params: { path: { id: job.job_id! } } }))).job!,
    (j) => {
      const fin = j.status !== "queued";
      if (fin) onDone();
      return fin;
    },
  );
  const j = poll.job ?? job;
  return (
    <div className="panel row" aria-label="Job cầu nối kế toán" aria-live="polite">
      <b>Job {j.kind === "push" ? `đẩy lần ${j.attempt ?? ""}` : "dựng bút toán"} kỳ {j.period_code}</b> <Chip status={j.status} label={AB_JOB_STATUS_LABEL[j.status ?? ""]} />
      {j.failure_code ? <span className="chip c-bad">{AB_FAILURE_LABEL[j.failure_code] ?? j.failure_code}</span> : null}
      {poll.error ? <ProblemAlert problem={poll.error} /> : null}
    </div>
  );
}

function download(name: string, text: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function JournalPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, adminApi, principal } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/acctbridge/journals/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [job, setJob] = useState<AcctbridgeJob | null>(null);
  const [exported, setExported] = useState<{ fmt: string; sha: string | null; ok: boolean } | null>(null);
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const j = d.data!.journal!;
  const attempts = d.data!.push_attempts ?? [];
  const lines = j.lines ?? [];
  const tot = journalTotals(lines);
  const operator = hasRole(principal, ROLES.accountingBridgeOperator);
  const chief = hasRole(principal, ROLES.chiefAccountant);
  const refresh = () => {
    d.reload();
    onChanged();
  };
  const exportFile = async (fmt: "csv" | "xml") => {
    const r = await act.run(async () => {
      const res = await api.GET("/v1/acctbridge/journals/{id}/export", { params: { path: { id }, query: { format: fmt } }, parseAs: "text" });
      if (!res.response.ok) throw await parseProblemResponse(res.response);
      const text = (res.data as unknown as string) ?? "";
      const sha = res.response.headers.get("X-Content-SHA256");
      return { text, sha };
    }, `Đã tải tệp ${fmt.toUpperCase()} của bút toán kỳ ${j.period_code}`);
    if (r) {
      setExported({ fmt, sha: r.sha, ok: !!r.sha && sha256Text(r.text) === r.sha });
      download(`${j.period_code}_${j.journal_id}.${fmt}`, r.text, fmt === "csv" ? "text/csv;charset=utf-8" : "application/xml;charset=utf-8");
    }
  };
  return (
    <div className="stack">
      <KV
        label="Thông tin bút toán"
        items={[
          ["Kỳ · phiên bản", <b key="k" className="mono">{j.period_code} · v{j.version}</b>],
          ["Trạng thái", <span key="s" className="row"><Chip status={j.status} label={JOURNAL_STATUS_LABEL[j.status ?? ""]} /> {j.last_failure_code ? <span className="chip c-bad">{AB_FAILURE_LABEL[j.last_failure_code] ?? j.last_failure_code}</span> : null}</span>],
          ["Σ Nợ / Σ Có", <span key="t"><Money v={j.total_debit} strong /> / <Money v={j.total_credit} strong /> {tot.balanced && tot.debit === j.total_debit ? <span className="chip c-ok">Cân (kiểm lại ở trình duyệt)</span> : <span className="chip c-bad">KHÔNG cân</span>}</span>],
          ["source_hash (= kết quả kỳ costing)", <span key="h" className="mono small">{j.source_hash}</span>],
          ["Băm dòng · ánh xạ", <span key="l" className="mono small">{shortHash(j.lines_sha256)} · ánh xạ v{j.mapping_version}</span>],
          ["Lần đóng kỳ", <Ref key="c" id={j.close_run_id} />],
          ["Người dựng → duyệt (R-56)", <SoD key="sd" maker={j.built_by} checker={j.approved_by} makerLabel="Người dựng" />],
          ["Đẩy", j.external_ref ? <span key="p">{j.adapter} · <span className="mono small">{j.external_ref}</span> · <When at={j.pushed_at} /></span> : `${j.push_attempts ?? 0} lần thử`],
          ...(j.supersedes_journal_id || j.superseded_by ? ([["Thay / bị thay", <span key="sp"><Ref id={j.supersedes_journal_id} /> / <Ref id={j.superseded_by} /></span>]] as [string, ReactNode][]) : []),
        ]}
      />
      <ActionStatus a={act} />
      {job ? <JobWatch key={job.job_id} job={job} onDone={refresh} /> : null}
      {exported ? (
        <p className="small" role="status" aria-label="Kết quả tải tệp">
          Tệp {exported.fmt.toUpperCase()} · X-Content-SHA256 <span className="mono">{exported.sha}</span> {exported.ok ? <span className="chip c-ok">Băm khớp nội dung</span> : <span className="chip c-bad">Băm không khớp</span>}
        </p>
      ) : null}
      <div className="row">
        <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void exportFile("csv")}>
          Tải CSV
        </button>
        <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void exportFile("xml")}>
          Tải XML
        </button>
        {j.status === "built" && chief ? (
          <>
            <SelfHint proposer={j.built_by} />
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(() => unwrap(adminApi.POST("/v1/acctbridge/journals/{id}/approve", { params: { path: { id }, header: idem() } })), "Đã duyệt bút toán — job đẩy lần 1 (202)").then((r) => {
                  if (r?.job) {
                    setJob(r.job);
                    refresh();
                  }
                })
              }
            >
              Duyệt bút toán
            </button>
          </>
        ) : null}
        {j.status === "push_failed" && operator ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy}
            onClick={() =>
              void act.run(() => unwrap(api.POST("/v1/acctbridge/journals/{id}/retry-push", { params: { path: { id }, header: idem() } })), "Đã tạo job đẩy lại (cùng khoá idempotency)").then((r) => {
                if (r?.job) {
                  setJob(r.job);
                  refresh();
                }
              })
            }
          >
            Đẩy lại
          </button>
        ) : null}
        {operator && (j.status === "built" || j.status === "push_failed") ? (
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={act.busy}
            onClick={() =>
              void act.run(() => unwrap(api.POST("/v1/acctbridge/periods/{id}/journal-builds", { params: { path: { id: j.period_id! }, header: idem() } })), "Đã yêu cầu dựng lại theo ánh xạ hiện hành (202)").then((r) => {
                if (r?.job) setJob(r.job);
              })
            }
          >
            Dựng lại theo ánh xạ hiện hành
          </button>
        ) : null}
      </div>
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Dòng bút toán">
          <thead>
            <tr>
              <th>#</th>
              <th>Tài khoản</th>
              <th>Bên</th>
              <th className="num">Nợ</th>
              <th className="num">Có</th>
              <th>Nhóm</th>
              <th>Chiều</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l) => (
              <tr key={l.line_no}>
                <td>{l.line_no}</td>
                <td className="mono">{l.account}</td>
                <td>{SIDE_LABEL[l.side ?? ""]}</td>
                <td className="num">{l.side === "debit" ? <Money v={l.amount} /> : ""}</td>
                <td className="num">{l.side === "credit" ? <Money v={l.amount} /> : ""}</td>
                <td>{COST_CATEGORY_LABEL[l.category ?? ""] ?? l.category}</td>
                <td className="mono small">{l.dimension || "—"}</td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={3}>
                <b>Σ</b>
              </td>
              <td className="num">
                <Money v={tot.debit} strong />
              </td>
              <td className="num">
                <Money v={tot.credit} strong />
              </td>
              <td colSpan={2}></td>
            </tr>
          </tfoot>
        </table>
      </div>
      <h3>Các lần đẩy</h3>
      {attempts.length ? (
        <ol className="list-plain small" aria-label="Các lần đẩy">
          {attempts.map((a) => (
            <li key={`${a.attempt}-${a.job_id}`}>
              Lần {a.attempt} · <When at={a.started_at} /> · {a.adapter} · <b>{PUSH_OUTCOME_LABEL[a.outcome ?? ""] ?? a.outcome}</b>
              {a.error_code ? ` — ${AB_FAILURE_LABEL[a.error_code] ?? a.error_code}` : ""}
              {a.external_ref ? <> · <span className="mono">{a.external_ref}</span></> : null}
            </li>
          ))}
        </ol>
      ) : (
        <Empty>Chưa đẩy lần nào.</Empty>
      )}
    </div>
  );
}

export default function JournalsPage() {
  const { api } = useAuth();
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/acctbridge/journals", { params: { query: { limit: 200, ...(status ? { status: status as "built" } : {}) } } }))).items ?? [], [api, status]);
  const [sel, setSel] = useState("");
  const items: AcctbridgeJournal[] = list.data ?? [];
  return (
    <div className="stack">
      <PageHead
        title="Bút toán tổng hợp"
        subtitle="Chỉ bút toán TỔNG HỢP theo kỳ (không giao dịch chi tiết), một chiều, tháng. Dựng tất định từ kết quả đóng kỳ (Σ Nợ = Σ Có, source_hash = băm kết quả kỳ). Kế toán trưởng KHÁC người dựng duyệt → đẩy (bộ xuất tệp CSV/XML) → kỳ ghi sổ. Đẩy lỗi vĩnh viễn → DLQ, đẩy lại sau khi xử lý."
      />
      <Section title="Danh sách bút toán">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(JOURNAL_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !items.length ? <Empty>Không có bút toán.</Empty> : null}
          {items.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Bút toán tổng hợp">
                <thead>
                  <tr>
                    <th>Kỳ</th>
                    <th>Phiên bản</th>
                    <th>Trạng thái</th>
                    <th className="num">Σ Nợ</th>
                    <th className="num">Σ Có</th>
                    <th className="num">Số dòng</th>
                    <th>Ánh xạ</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((j) => (
                    <tr key={j.journal_id} aria-selected={j.journal_id === sel}>
                      <td className="mono">{j.period_code}</td>
                      <td>v{j.version}</td>
                      <td>
                        <Chip status={j.status} label={JOURNAL_STATUS_LABEL[j.status ?? ""]} />
                      </td>
                      <td className="num">
                        <Money v={j.total_debit} />
                      </td>
                      <td className="num">
                        <Money v={j.total_credit} />
                      </td>
                      <td className="num">{j.line_count}</td>
                      <td>v{j.mapping_version}</td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(j.journal_id!)} aria-label={`Mở bút toán ${j.period_code} v${j.version}`}>
                          Mở
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
      {sel ? (
        <Section title="Chi tiết bút toán">
          <JournalPanel key={sel} id={sel} onChanged={list.reload} />
        </Section>
      ) : null}
    </div>
  );
}

