"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { approvalBlockReason, proposeBlockReason, submitBlockReason } from "@/admin/approval";
import { groupParamVersions, proposeParam, submitParam, type ParamView } from "@/admin/params";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import type { ParamVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

/** Giá trị tham số: chuỗi thập phân giữ nguyên để gửi/lưu; chỉ định dạng khi hiển thị. */
function show(v: Pick<ParamVersion, "value" | "text">): string {
  return v.value !== null ? formatDecimal(v.value, 4) : (v.text ?? "—");
}

const DECIMAL_INPUT = /^\d+(\.\d+)?$/;

function ProposeForm({ param, onDone, onCancel }: { param: ParamView; onDone: (warning: ApiProblem | null) => void; onCancel: () => void }) {
  const { adminApi } = useAuth();
  const [value, setValue] = useState("");
  const [effectiveFrom, setEffectiveFrom] = useState("");
  const [sourceDoc, setSourceDoc] = useState("");
  const [correction, setCorrection] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [localErr, setLocalErr] = useState<string | null>(null);
  // Một khóa cho một thao tác: thử lại sau lỗi mạng dùng lại khóa, đổi dữ liệu thì sinh khóa mới (R-51).
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const touch = (set: (v: string) => void) => (v: string) => {
    set(v);
    setIdemKey(newIdempotencyKey());
  };
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    // Giữ nguyên CHUỖI người dùng nhập (dấu chấm thập phân) — không parseFloat (R-10).
    const v = value.trim().replace(",", ".");
    if (!DECIMAL_INPUT.test(v)) {
      setLocalErr("Giá trị phải là số thập phân không âm, dùng dấu chấm hoặc phẩy (vd 85 hoặc 90,5).");
      return;
    }
    setLocalErr(null);
    setBusy(true);
    setProblem(null);
    try {
      const res = await proposeParam(
        adminApi,
        {
          key: param.key,
          value: v,
          unit: param.unit || undefined,
          effective_from: effectiveFrom,
          source_doc: sourceDoc,
          correction_reason: correction.trim() || undefined,
        },
        idemKey,
      );
      onDone(res.submitError);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  const fid = (n: string) => `p-${param.key.replace(/[^\w]/g, "_")}-${n}`;
  const field = (name: string, label: string, input: ReactNode) => (
    <div className="field">
      <label htmlFor={fid(name)}>{label}</label>
      {input}
      {errs[name] ? (
        <span className="err" id={fid(`${name}-err`)}>
          {errs[name]}
        </span>
      ) : null}
    </div>
  );
  return (
    <form onSubmit={(e) => void submit(e)} className="stack" style={{ gap: 12, padding: 14, background: "var(--g50)", borderRadius: 12 }} noValidate>
      <b>Đề xuất phiên bản mới cho “{param.label}”</b>
      {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
      <div className="grid2">
        {field(
          "value",
          `Giá trị mới${param.unit ? ` (${param.unit})` : ""}`,
          <input id={fid("value")} className="input" inputMode="decimal" value={value} onChange={(e) => touch(setValue)(e.target.value)} aria-invalid={!!errs.value || !!localErr} required />,
        )}
        {field(
          "effective_from",
          "Ngày hiệu lực",
          <input id={fid("effective_from")} className="input" type="date" value={effectiveFrom} onChange={(e) => touch(setEffectiveFrom)(e.target.value)} aria-invalid={!!errs.effective_from} required />,
        )}
        {field(
          "source_doc",
          "Văn bản căn cứ (số/ngày)",
          <input id={fid("source_doc")} className="input" value={sourceDoc} onChange={(e) => touch(setSourceDoc)(e.target.value)} aria-invalid={!!errs.source_doc} required />,
        )}
        {field(
          "correction_reason",
          "Lý do sửa (chỉ khi trùng ngày hiệu lực đã duyệt)",
          <input id={fid("correction_reason")} className="input" value={correction} onChange={(e) => touch(setCorrection)(e.target.value)} aria-invalid={!!errs.correction_reason} />,
        )}
      </div>
      {localErr ? (
        <p className="err small" role="alert">
          {localErr}
        </p>
      ) : null}
      {problem?.code === "validation_failed" ? (
        <p className="err small" role="alert">
          {problem.message} (mã tra cứu {problem.traceId ?? "—"})
        </p>
      ) : null}
      <div className="row">
        <button type="submit" className="btn btn-p" disabled={busy}>
          {busy ? "Đang gửi…" : "Tạo và gửi duyệt"}
        </button>
        <button type="button" className="btn btn-o" onClick={onCancel}>
          Huỷ
        </button>
        <span className="mute small">Phiên bản chỉ có hiệu lực sau khi một người khác phê duyệt.</span>
      </div>
    </form>
  );
}

function VersionRow({ v, onChanged }: { v: ParamVersion; onChanged: () => void }) {
  const { adminApi, principal } = useAuth();
  const [busy, setBusy] = useState<"approve" | "reject" | "submit" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [note, setNote] = useState("");
  const isDraft = v.status === "draft";
  const block = isDraft ? submitBlockReason(v, principal) : approvalBlockReason(v, principal);
  const hintId = `hint-${v.id}`;

  const act = async (action: "approve" | "reject" | "submit") => {
    setBusy(action);
    setProblem(null);
    const header = { "Idempotency-Key": newIdempotencyKey() };
    try {
      if (action === "submit") await submitParam(adminApi, v.id, header["Idempotency-Key"]);
      else if (action === "approve")
        await unwrap(adminApi.POST("/v1/masterdata/params/versions/{id}/approve", { params: { path: { id: v.id }, header }, body: note.trim() ? { note: note.trim() } : {} }));
      else await unwrap(adminApi.POST("/v1/masterdata/params/versions/{id}/reject", { params: { path: { id: v.id }, header }, body: { note: note.trim() } }));
      onChanged();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(null);
    }
  };

  return (
    <li style={{ padding: 12, border: "1px dashed #f5d9a0", background: "var(--amber-bg)", borderRadius: 12, listStyle: "none" }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
        <div>
          <b>
            {isDraft ? "Nháp" : "Chờ duyệt"} · phiên bản {v.version}: {show(v)} {v.unit}
          </b>{" "}
          <span className="mute small">hiệu lực từ {formatDate(v.effective_from)}</span>
          <div className="small">
            Người đặt: <code>{v.setter_ref}</code> · {formatDateTime(v.submitted_at ?? v.created_at)}
          </div>
          <div className="small mute">
            Căn cứ: {v.source_doc}
            {v.correction_reason ? ` · Lý do sửa: ${v.correction_reason}` : ""}
          </div>
        </div>
        <div className="stack" style={{ gap: 6, minWidth: 240 }}>
          {isDraft ? (
            <button type="button" className="btn btn-p btn-sm" disabled={!!block || !!busy} aria-describedby={block ? hintId : undefined} onClick={() => void act("submit")}>
              {busy === "submit" ? "Đang gửi…" : "Gửi duyệt"}
            </button>
          ) : (
            <>
              <label className="small" htmlFor={`note-${v.id}`}>
                Ghi chú (bắt buộc ≥ 10 ký tự khi từ chối)
              </label>
              <input id={`note-${v.id}`} className="input" value={note} onChange={(e) => setNote(e.target.value)} disabled={!!block} />
              <div className="row">
                <button type="button" className="btn btn-p btn-sm" disabled={!!block || !!busy} aria-describedby={block ? hintId : undefined} onClick={() => void act("approve")}>
                  {busy === "approve" ? "Đang duyệt…" : "Phê duyệt"}
                </button>
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  disabled={!!block || !!busy || note.trim().length < 10}
                  aria-describedby={block ? hintId : undefined}
                  onClick={() => void act("reject")}
                >
                  {busy === "reject" ? "Đang gửi…" : "Từ chối"}
                </button>
              </div>
            </>
          )}
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

/** Tham số & phê duyệt — Admin API /v1/masterdata/params/versions (MFA bắt buộc, R-56). */
export default function ParamsPage() {
  const { adminApi, principal } = useAuth();
  const versions = useAsync(
    () => collectPages((cursor) => unwrap(adminApi.GET("/v1/masterdata/params/versions", { params: { query: { cursor, limit: 200 } } })), 5000),
    [adminApi],
  );
  const params = versions.data ? groupParamVersions(versions.data) : [];
  const [editing, setEditing] = useState<string | null>(null);
  const [warning, setWarning] = useState<ApiProblem | null>(null);
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
          mình. Mỗi phiên bản: tạo nháp → gửi duyệt → một người phê duyệt khác duyệt hoặc từ chối. Phiên bản đã duyệt không sửa được;
          sửa sai là phiên bản mới có lý do.
        </span>
      </div>
      {principal && !principal.mfa ? (
        <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status">
          <Icon name="alert" />
          <span>Phiên hiện tại chưa qua xác thực hai yếu tố — Admin API sẽ từ chối mọi thao tác (mfa_required).</span>
        </div>
      ) : null}
      {warning ? (
        <div style={{ marginBottom: 16 }}>
          <p className="small" style={{ marginBottom: 6 }}>
            Đã tạo bản nháp nhưng chưa gửi duyệt được — bấm “Gửi duyệt” ở bản nháp để thử lại.
          </p>
          <ProblemAlert problem={warning} />
        </div>
      ) : null}
      {versions.error ? <ProblemAlert problem={versions.error} onRetry={versions.reload} /> : null}
      {versions.loading && !versions.data ? <Loading /> : null}
      <div className="stack">
        {params.map((p) => (
          <section key={p.key} className="card sec" aria-labelledby={`h-${p.key}`}>
            <div className="sec-h">
              <div>
                <h2 id={`h-${p.key}`}>{p.label}</h2>
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
                Đang hiệu lực: <b style={{ fontSize: 18 }}>{show(p.current)}</b> {p.current.unit}{" "}
                <span className="mute small">
                  (phiên bản {p.current.version}, từ {formatDate(p.current.effective_from)} · {p.current.source_doc} · duyệt{" "}
                  {formatDateTime(p.current.decided_at)})
                </span>
              </p>
            ) : (
              <p className="mute">Chưa có phiên bản được phê duyệt có hiệu lực.</p>
            )}
            {p.upcoming.map((u) => (
              <p key={u.id} className="small">
                Sắp hiệu lực từ {formatDate(u.effective_from)}: <b style={{ display: "inline" }}>{show(u)}</b> {u.unit} (phiên bản {u.version})
              </p>
            ))}
            {editing === p.key ? (
              <div style={{ marginTop: 12 }}>
                <ProposeForm
                  param={p}
                  onCancel={() => setEditing(null)}
                  onDone={(w) => {
                    setEditing(null);
                    setWarning(w);
                    versions.reload();
                  }}
                />
              </div>
            ) : null}
            {p.submitted.length + p.drafts.length > 0 ? (
              <ul className="stack" style={{ gap: 8, marginTop: 12 }} aria-label="Phiên bản chờ xử lý">
                {[...p.submitted, ...p.drafts].map((v) => (
                  <VersionRow key={v.id} v={v} onChanged={versions.reload} />
                ))}
              </ul>
            ) : null}
          </section>
        ))}
      </div>
    </>
  );
}
