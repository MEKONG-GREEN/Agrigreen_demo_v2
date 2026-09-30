"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { CostingCloseJob } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { KV, Money, useJobPoll, When } from "@/components/g4kit";
import { ActionStatus, Chip, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { CLOSE_JOB_STATUS_LABEL, COST_CATEGORY_LABEL, DIMENSION_LABEL, PERIOD_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg } from "@/lib/kg";
import { addMoney } from "@/lib/money";
import { useAsync } from "@/lib/useAsync";
import { LIST_PAGE_SIZE, usePaged } from "@/lib/usePaged";

function JobProgress({ job, onDone }: { job: CostingCloseJob; onDone: () => void }) {
  const { api } = useAuth();
  const poll = useJobPoll(
    () => unwrap(api.GET("/v1/costing/period-close-jobs/{id}", { params: { path: { id: job.job_id! } } })),
    (j) => {
      const fin = j.status !== "queued";
      if (fin) onDone();
      return fin;
    },
  );
  const j = poll.job ?? job;
  return (
    <div className="panel stack" aria-label="Job đóng kỳ" aria-live="polite">
      <div className="row">
        <b>Job đóng kỳ {j.period_code}</b> <Chip status={j.status} label={CLOSE_JOB_STATUS_LABEL[j.status ?? ""]} />
        {j.status === "queued" ? <span className="spinner" aria-hidden="true" /> : null}
      </div>
      <KV
        items={[
          ["Mã job", <Ref key="j" id={j.job_id} />],
          ["Ảnh chụp đầu vào (bất biến)", <span key="h" className="mono small">{j.input_snapshot_sha256}</span>],
          ["Số dòng chụp", `${j.snapshot_counts?.entries ?? 0} bút toán · ${j.snapshot_counts?.settlements ?? 0} quyết toán · ${j.snapshot_counts?.carbon ?? 0} carbon`],
          ["Yêu cầu", <span key="r"><Ref id={j.requested_by} /> · <When at={j.requested_at} /></span>],
          ["Xong lúc", <When key="c" at={j.completed_at} />],
          ...(j.error_code ? ([["Lỗi", j.error_code]] as [string, string][]) : []),
        ]}
      />
      {poll.error ? <ProblemAlert problem={poll.error} /> : null}
    </div>
  );
}

function PeriodDetail({ id }: { id: string }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/costing/periods/{id}", { params: { path: { id } } })), [api, id]);
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const p = d.data!;
  const s = p.summary;
  return (
    <div className="stack">
      <KV
        label="Thông tin kỳ"
        items={[
          ["Kỳ", <b key="k" className="mono">{p.code}</b>],
          ["Trạng thái", <Chip key="s" status={p.status} label={PERIOD_STATUS_LABEL[p.status ?? ""]} />],
          ["Khoảng", <span key="t"><When at={p.from} /> → <When at={p.to} /> (giờ Việt Nam)</span>],
          ["Người đóng · lúc", <span key="c"><Ref id={p.closed_by} /> · <When at={p.closed_at} /></span>],
          ["Bút toán kế toán", p.journal_ref ? <span key="j"><Ref id={p.journal_ref} /> · ghi sổ <When at={p.posted_at} /></span> : "—"],
        ]}
      />
      {s ? (
        <>
          <KV
            label="Kết quả đóng kỳ"
            items={[
              ["Lần đóng kỳ", <Ref key="r" id={s.close_run_id} />],
              ["SHA-256 ảnh chụp đầu vào", <span key="i" className="mono small">{s.input_snapshot_sha256}</span>],
              ["SHA-256 kết quả (= source_hash bút toán)", <span key="o" className="mono small">{s.result_set_sha256}</span>],
              ["Quy tắc", <span key="v" className="mono">{s.rule_version}</span>],
            ]}
          />
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Dòng tổng hợp kỳ">
              <thead>
                <tr>
                  <th>Nhóm</th>
                  <th>Chiều</th>
                  <th className="num">Số tiền</th>
                  <th className="num">Khối lượng</th>
                  <th className="num">Số bút toán</th>
                </tr>
              </thead>
              <tbody>
                {(s.lines ?? []).map((l, i) => (
                  <tr key={i}>
                    <td>{COST_CATEGORY_LABEL[l.category ?? ""] ?? l.category}</td>
                    <td>
                      {DIMENSION_LABEL[l.dimension_kind ?? ""] ?? l.dimension_kind} <Ref id={l.dimension_ref} />
                    </td>
                    <td className="num">
                      <Money v={l.amount} />
                    </td>
                    <td className="num">{formatKg(l.mass_kg)}</td>
                    <td className="num">{l.entries}</td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td colSpan={2}>
                    <b>Σ {s.line_count} dòng</b>
                  </td>
                  <td className="num">
                    <Money v={addMoney(...(s.lines ?? []).map((l) => l.amount))} strong />
                  </td>
                  <td colSpan={2}></td>
                </tr>
              </tfoot>
            </table>
          </div>
        </>
      ) : (
        <Empty>Kỳ chưa đóng — chưa có kết quả tổng hợp.</Empty>
      )}
    </div>
  );
}

export default function PeriodsPage() {
  const { api, principal } = useAuth();
  const list = useAsync(async () => (await unwrap(api.GET("/v1/costing/periods", { params: { query: { limit: 24 } } }))).items ?? [], [api]);
  const [sel, setSel] = useState("");
  const [code, setCode] = useState("");
  const [job, setJob] = useState<CostingCloseJob | null>(null);
  const act = useAction();
  const closer = hasRole(principal, ROLES.periodCloser);
  const [jobStatus, setJobStatus] = useState("");
  const jobs = usePaged(
    (cursor) => unwrap(api.GET("/v1/costing/period-close-jobs", { params: { query: { limit: LIST_PAGE_SIZE, ...(jobStatus ? { status: jobStatus as "queued" } : {}), ...(cursor ? { cursor } : {}) } } })),
    [api, jobStatus],
  );
  const refresh = () => {
    list.reload();
    jobs.reload();
  };
  return (
    <div className="stack">
      <PageHead
        title="Kỳ giá thành"
        subtitle="Kỳ tháng: đang mở → đang đóng → đã đóng → đã ghi sổ kế toán. Đóng kỳ = 202 + job (ảnh chụp đầu vào bất biến có băm). Kỳ đã đóng KHÔNG mở lại — sai sót ghi bút toán điều chỉnh ở kỳ đang mở. Người đóng kỳ ≠ người lập bút toán điều chỉnh của kỳ (R-56)."
      />
      <ActionStatus a={act} />
      {job ? <JobProgress key={job.job_id} job={job} onDone={refresh} /> : null}
      <Section title="Các kỳ">
        <div className="stack">
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Kỳ giá thành">
                <thead>
                  <tr>
                    <th>Kỳ</th>
                    <th>Trạng thái</th>
                    <th>Người đóng</th>
                    <th>Đóng lúc</th>
                    <th>Ghi sổ</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.map((p) => (
                    <tr key={p.id} aria-selected={p.id === sel}>
                      <td className="mono">{p.code}</td>
                      <td>
                        <Chip status={p.status} label={PERIOD_STATUS_LABEL[p.status ?? ""]} />
                      </td>
                      <td>
                        <Ref id={p.closed_by} />
                      </td>
                      <td>
                        <When at={p.closed_at} />
                      </td>
                      <td>
                        <When at={p.posted_at} />
                      </td>
                      <td className="row">
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(p.id!)} aria-label={`Xem kỳ ${p.code}`}>
                          Xem
                        </button>
                        {closer && p.status === "open" ? (
                          <button
                            type="button"
                            className="btn btn-p btn-sm"
                            disabled={act.busy}
                            onClick={() =>
                              void act.run(() => unwrap(api.POST("/v1/costing/periods/{id}/close", { params: { path: { id: p.id! }, header: idem() } })), `Đã nhận yêu cầu đóng kỳ ${p.code} (202) — worker đang tổng hợp`).then((j) => {
                                if (j) {
                                  setJob(j);
                                  setSel(p.id!);
                                  refresh();
                                }
                              })
                            }
                          >
                            Đóng kỳ {p.code}
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          {hasRole(principal, ROLES.costAccountant, ROLES.periodCloser) ? (
            <div className="row">
              <TextField label="Mở kỳ tháng (YYYY-MM)" value={code} onChange={setCode} hint="Kỳ cũng tự mở khi có bút toán đầu tiên" />
              <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !/^\d{4}-(0[1-9]|1[0-2])$/.test(code.trim())} onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/periods", { params: { header: idem() }, body: { code: code.trim() } })), (p) => `Kỳ ${p.code}: ${PERIOD_STATUS_LABEL[p.status ?? ""]}`).then((r) => r && refresh())}>
                Mở kỳ
              </button>
            </div>
          ) : null}
        </div>
      </Section>
      <Section title="Danh sách job đóng kỳ">
        <div className="stack">
          <SelectField label="Lọc trạng thái job" value={jobStatus} onChange={setJobStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(CLOSE_JOB_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {jobs.loading ? <Loading /> : null}
          {jobs.error ? <ProblemAlert problem={jobs.error} onRetry={jobs.reload} /> : null}
          {jobs.items && !jobs.items.length ? <Empty>Chưa có job đóng kỳ.</Empty> : null}
          {jobs.items?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Job đóng kỳ đã chạy">
                <thead>
                  <tr>
                    <th>Kỳ</th>
                    <th>Trạng thái</th>
                    <th>Yêu cầu</th>
                    <th>Xong lúc</th>
                    <th>Ảnh chụp đầu vào</th>
                    <th className="num">Bút toán chụp</th>
                    <th>Lỗi</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.items.map((j) => (
                    <tr key={j.job_id}>
                      <td className="mono">{j.period_code}</td>
                      <td>
                        <Chip status={j.status} label={CLOSE_JOB_STATUS_LABEL[j.status ?? ""]} />
                      </td>
                      <td className="small">
                        <Ref id={j.requested_by} /> · <When at={j.requested_at} />
                      </td>
                      <td>
                        <When at={j.completed_at} />
                      </td>
                      <td className="mono small" title={j.input_snapshot_sha256}>
                        {(j.input_snapshot_sha256 ?? "").slice(0, 12)}…
                      </td>
                      <td className="num">{j.snapshot_counts?.entries ?? 0}</td>
                      <td className="small">{j.error_code ?? "—"}</td>
                      <td>
                        {j.status === "queued" ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setJob(j)} aria-label={`Theo dõi job ${j.period_code}`}>
                            Theo dõi
                          </button>
                        ) : j.period_id ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(j.period_id!)} aria-label={`Xem kỳ của job ${j.period_code}`}>
                            Xem kỳ
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          <LoadMore hasMore={jobs.hasMore} loading={jobs.loadingMore} onClick={jobs.loadMore} />
        </div>
      </Section>
      {sel ? (
        <Section title="Chi tiết kỳ">
          <PeriodDetail key={`${sel}-${job?.job_id ?? ""}-${list.data?.find((p) => p.id === sel)?.status ?? ""}`} id={sel} />
        </Section>
      ) : null}
    </div>
  );
}
