"use client";

import { useState, type FormEvent } from "react";
import { approvalBlockReason, proposeBlockReason } from "@/admin/approval";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { Param, ParamProposal } from "@/api/pending/masterdata";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

function ProposeForm({ param, onDone, onCancel }: { param: Param; onDone: () => void; onCancel: () => void }) {
  const { adminApi } = useAuth();
  const [value, setValue] = useState("");
  const [effectiveFrom, setEffectiveFrom] = useState("");
  const [sourceDoc, setSourceDoc] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  // Một khóa cho một thao tác: thử lại sau lỗi mạng dùng lại khóa, đổi dữ liệu thì sinh khóa mới (R-51).
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const touch = <T,>(set: (v: T) => void) => (v: T) => {
    set(v);
    setIdemKey(newIdempotencyKey());
  };
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/params/{key}/proposals", {
          params: { path: { key: param.key }, header: { "Idempotency-Key": idemKey } },
          body: { value, effective_from: effectiveFrom, source_doc: sourceDoc, reason },
        }),
      );
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  const fid = (n: string) => `p-${param.key.replace(/[^\w]/g, "_")}-${n}`;
  return (
    <form onSubmit={(e) => void submit(e)} className="stack" style={{ gap: 12, padding: 14, background: "var(--g50)", borderRadius: 12 }} noValidate>
      <b>Đề xuất giá trị mới cho “{param.name}”</b>
      {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
      <div className="grid2">
        <div className="field">
          <label htmlFor={fid("value")}>Giá trị mới ({param.unit})</label>
          <input id={fid("value")} className="input" inputMode="decimal" value={value} onChange={(e) => touch(setValue)(e.target.value)} aria-invalid={!!errs.value} aria-describedby={errs.value ? fid("value-err") : undefined} required />
          {errs.value ? <span className="err" id={fid("value-err")}>{errs.value}</span> : null}
        </div>
        <div className="field">
          <label htmlFor={fid("eff")}>Ngày hiệu lực</label>
          <input id={fid("eff")} className="input" type="date" value={effectiveFrom} onChange={(e) => touch(setEffectiveFrom)(e.target.value)} aria-invalid={!!errs.effective_from} aria-describedby={errs.effective_from ? fid("eff-err") : undefined} required />
          {errs.effective_from ? <span className="err" id={fid("eff-err")}>{errs.effective_from}</span> : null}
        </div>
        <div className="field">
          <label htmlFor={fid("src")}>Văn bản căn cứ</label>
          <input id={fid("src")} className="input" value={sourceDoc} onChange={(e) => touch(setSourceDoc)(e.target.value)} aria-invalid={!!errs.source_doc} aria-describedby={errs.source_doc ? fid("src-err") : undefined} required />
          {errs.source_doc ? <span className="err" id={fid("src-err")}>{errs.source_doc}</span> : null}
        </div>
        <div className="field">
          <label htmlFor={fid("reason")}>Lý do thay đổi</label>
          <input id={fid("reason")} className="input" value={reason} onChange={(e) => touch(setReason)(e.target.value)} aria-invalid={!!errs.reason} aria-describedby={errs.reason ? fid("reason-err") : undefined} required />
          {errs.reason ? <span className="err" id={fid("reason-err")}>{errs.reason}</span> : null}
        </div>
      </div>
      {problem?.code === "validation_failed" ? (
        <p className="err small" role="alert">
          {problem.message} (mã tra cứu {problem.traceId ?? "—"})
        </p>
      ) : null}
      <div className="row">
        <button type="submit" className="btn btn-p" disabled={busy}>
          {busy ? "Đang gửi…" : "Gửi đề xuất"}
        </button>
        <button type="button" className="btn btn-o" onClick={onCancel}>
          Huỷ
        </button>
        <span className="mute small">Đề xuất chỉ có hiệu lực sau khi một người khác phê duyệt.</span>
      </div>
    </form>
  );
}

function ProposalRow({ proposal, onDecided }: { proposal: ParamProposal; onDecided: () => void }) {
  const { adminApi, principal } = useAuth();
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const block = approvalBlockReason(proposal, principal);
  const hintId = `hint-${proposal.id}`;

  const decide = async (action: "approve" | "reject") => {
    setBusy(action);
    setProblem(null);
    try {
      const path = action === "approve" ? "/v1/params/proposals/{id}/approve" : "/v1/params/proposals/{id}/reject";
      await unwrap(adminApi.POST(path, { params: { path: { id: proposal.id } }, body: {} }));
      onDecided();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(null);
    }
  };

  return (
    <li style={{ padding: 12, border: "1px dashed #f5d9a0", background: "var(--amber-bg)", borderRadius: 12, listStyle: "none" }}>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <div>
          <b>
            Đề xuất: {proposal.value ?? proposal.text} {proposal.unit}
          </b>{" "}
          <span className="mute small">từ {formatDate(proposal.effective_from)}</span>
          <div className="small">
            Người đề xuất: <b style={{ display: "inline" }}>{proposal.proposed_by_name}</b> · {formatDateTime(proposal.proposed_at)}
          </div>
          <div className="small mute">
            Căn cứ: {proposal.source_doc} · Lý do: {proposal.reason}
          </div>
        </div>
        <div className="row">
          <button type="button" className="btn btn-p btn-sm" disabled={!!block || !!busy} aria-describedby={block ? hintId : undefined} onClick={() => void decide("approve")}>
            {busy === "approve" ? "Đang duyệt…" : "Phê duyệt"}
          </button>
          <button type="button" className="btn btn-o btn-sm" disabled={!!block || !!busy} aria-describedby={block ? hintId : undefined} onClick={() => void decide("reject")}>
            {busy === "reject" ? "Đang gửi…" : "Từ chối"}
          </button>
        </div>
      </div>
      {block ? (
        <p id={hintId} className="small" style={{ marginTop: 6, color: "var(--amber)", fontWeight: 600 }}>
          <Icon name="info" size={14} /> {block}
        </p>
      ) : null}
      {problem ? (
        <div style={{ marginTop: 8 }}>
          <ProblemAlert problem={problem} />
        </div>
      ) : null}
    </li>
  );
}

export default function ParamsPage() {
  const { api, principal } = useAuth();
  const params = useAsync(() => unwrap(api.GET("/v1/params")), [api]);
  const [editing, setEditing] = useState<string | null>(null);
  const proposeBlock = proposeBlockReason(principal);

  return (
    <>
      <PageHead
        title="Tham số & phê duyệt"
        subtitle="Định mức, ngưỡng cảnh báo, SLA… có phiên bản và ngày hiệu lực. Tra theo thời điểm của sự việc."
      />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          <b style={{ display: "inline" }}>Nguyên tắc bốn mắt (R-56):</b> người đặt tham số không được tự phê duyệt thay đổi của
          mình. Mọi đề xuất cần một người có vai phê duyệt khác xác nhận. Máy chủ kiểm lại quy tắc này ở mọi lời gọi.
        </span>
      </div>
      {principal && !principal.mfa ? (
        <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status">
          <Icon name="alert" />
          <span>Phiên hiện tại chưa qua xác thực hai yếu tố — các thao tác đề xuất/phê duyệt sẽ bị máy chủ từ chối (mfa_required).</span>
        </div>
      ) : null}
      {params.error ? <ProblemAlert problem={params.error} onRetry={params.reload} /> : null}
      {params.loading && !params.data ? <Loading /> : null}
      <div className="stack">
        {params.data?.items.map((p) => (
          <section key={p.key} className="card sec" aria-labelledby={`h-${p.key}`}>
            <div className="sec-h">
              <div>
                <h2 id={`h-${p.key}`}>{p.name}</h2>
                <code className="mute small">{p.key}</code>
              </div>
              <button
                type="button"
                className="btn btn-o btn-sm"
                disabled={!!proposeBlock}
                title={proposeBlock ?? undefined}
                onClick={() => setEditing((e) => (e === p.key ? null : p.key))}
                aria-expanded={editing === p.key}
              >
                Đề xuất thay đổi
              </button>
            </div>
            {p.current ? (
              <p>
                Đang hiệu lực: <b style={{ fontSize: 18 }}>{p.current.value ?? p.current.text}</b> {p.current.unit}{" "}
                <span className="mute small">
                  (phiên bản {p.current.version}, từ {formatDate(p.current.effective_from)} · {p.current.source_doc} · duyệt bởi{" "}
                  {p.current.approved_by})
                </span>
              </p>
            ) : (
              <p className="mute">Chưa có phiên bản được phê duyệt.</p>
            )}
            {editing === p.key ? (
              <div style={{ marginTop: 12 }}>
                <ProposeForm
                  param={p}
                  onCancel={() => setEditing(null)}
                  onDone={() => {
                    setEditing(null);
                    params.reload();
                  }}
                />
              </div>
            ) : null}
            {p.pending.length > 0 ? (
              <ul className="stack" style={{ gap: 8, marginTop: 12 }} aria-label="Đề xuất chờ phê duyệt">
                {p.pending.map((pr) => (
                  <ProposalRow key={pr.id} proposal={pr} onDecided={params.reload} />
                ))}
              </ul>
            ) : null}
          </section>
        ))}
      </div>
    </>
  );
}
