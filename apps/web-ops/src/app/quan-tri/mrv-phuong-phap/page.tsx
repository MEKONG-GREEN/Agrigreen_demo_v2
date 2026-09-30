"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { MrvMethodVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { methodDecisionBlock, validateMethodDraft, type PinDraft } from "@/mrv/rules";

const STATUS: Record<MrvMethodVersion["status"], { label: string; chip: string }> = {
  submitted: { label: "Chờ duyệt", chip: "c-warn" },
  approved: { label: "Đã duyệt", chip: "c-ok" },
  rejected: { label: "Từ chối", chip: "c-bad" },
};

const ACTOR_NAME: Record<string, string> = {
  "mock-admin-setter": "Quản trị viên A (giả lập)",
  "mock-admin-approver": "Quản trị viên B (giả lập)",
};
const who = (ref: string | null) => (ref ? (ACTOR_NAME[ref] ?? ref) : "—");

/**
 * Phương pháp ước tính CO2 (Admin API /v1/mrv/methods): param_setter đề xuất (ghim phiên bản tham số đã duyệt),
 * param_approver KHÁC người đề xuất duyệt/từ chối (R-56). Ước tính chỉ "chính thức" khi có phương pháp đã duyệt.
 */
export default function MrvMethodsPage() {
  const { adminApi, principal } = useAuth();
  const canPropose = hasRole(principal, ROLES.paramSetter);
  const [version, setVersion] = useState(0);
  const list = useAsync(() => collectPages<MrvMethodVersion>((cursor) => unwrap(adminApi.GET("/v1/mrv/methods", { params: { query: { cursor, limit: 100 } } })), 500), [adminApi, version]);
  const reload = () => setVersion((v) => v + 1);

  return (
    <>
      <PageHead title="Phương pháp ước tính MRV" subtitle="Đề xuất và phê duyệt phương pháp tính giảm phát thải — người duyệt phải khác người đề xuất" />
      <MfaNotice />
      <div className="alert alert-warn" role="note" style={{ marginBottom: 16 }}>
        Tạm — chờ khách ký phương pháp (C-1). Khi chưa có phương pháp đã duyệt ghim đúng phiên bản tham số đã duyệt, mọi ước tính CO2 hiển thị nhãn “TẠM — chờ khách
        ký phương pháp”.
      </div>
      {canPropose ? <ProposeForm onSaved={reload} /> : null}
      <section className="card sec" aria-labelledby="mm-list" style={{ marginTop: 16 }}>
        <div className="sec-h">
          <h2 id="mm-list">Các phiên bản phương pháp</h2>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.loading && !list.data ? <Loading /> : null}
        {list.data && list.data.length === 0 ? <p className="small mute">Chưa có phiên bản nào.</p> : null}
        <ul className="list-plain stack" aria-label="Phiên bản phương pháp">
          {(list.data ?? []).map((m) => (
            <MethodRow key={m.id} m={m} onChanged={reload} />
          ))}
        </ul>
      </section>
    </>
  );
}

function ProposeForm({ onSaved }: { onSaved: () => void }) {
  const { adminApi } = useAuth();
  const [rule, setRule] = useState("mrv.co2.estimate.v1");
  const [note, setNote] = useState("");
  const [pins, setPins] = useState<PinDraft[]>([{ key: "mrv.co2.open_burning_kg_per_t", version: "1" }]);
  const [errors, setErrors] = useState<{ field: string; reason: string }[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const err = (f: string) => errors.find((e) => e.field === f)?.reason;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs = validateMethodDraft(rule, note, pins);
    setErrors(errs);
    if (errs.length) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/mrv/methods", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { rule_version: rule.trim(), source_note: note.trim(), params: pins.map((p) => ({ key: p.key.trim(), version: Number(p.version) })) },
        }),
      );
      setNote("");
      onSaved();
    } catch (x) {
      setProblem(x instanceof ApiProblem ? x : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card sec stack" onSubmit={submit} aria-labelledby="mm-new" noValidate>
      <h2 id="mm-new" style={{ fontSize: 15.5 }}>
        Đề xuất phiên bản phương pháp
      </h2>
      <div className="field">
        <label htmlFor="mm-rule">Mã quy tắc tính</label>
        <input id="mm-rule" className="input" value={rule} onChange={(e) => setRule(e.target.value)} />
        {err("rule_version") ? <span className="err">{err("rule_version")}</span> : null}
      </div>
      <div className="field">
        <label htmlFor="mm-note">Văn bản phương pháp đã ký</label>
        <textarea id="mm-note" className="input" rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Số/ngày văn bản, bên ký" />
        {err("source_note") ? <span className="err">{err("source_note")}</span> : null}
      </div>
      <fieldset className="plain">
        <legend>Tham số ghim (khóa + phiên bản đã duyệt)</legend>
        {pins.map((p, i) => (
          <div key={i} className="row" style={{ gap: 8, marginBottom: 6, flexWrap: "wrap" }}>
            <label className="small" htmlFor={`mm-pk-${i}`}>
              Khóa tham số {i + 1}
            </label>
            <input id={`mm-pk-${i}`} className="input" style={{ minWidth: 280 }} value={p.key} onChange={(e) => setPins((l) => l.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} />
            <label className="small" htmlFor={`mm-pv-${i}`}>
              Phiên bản {i + 1}
            </label>
            <input id={`mm-pv-${i}`} className="input" style={{ width: 80 }} inputMode="numeric" value={p.version} onChange={(e) => setPins((l) => l.map((x, j) => (j === i ? { ...x, version: e.target.value } : x)))} />
            {pins.length > 1 ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setPins((l) => l.filter((_, j) => j !== i))}>
                Bỏ tham số {i + 1}
              </button>
            ) : null}
            {err(`params[${i}].key`) ? <span className="err">{err(`params[${i}].key`)}</span> : null}
            {err(`params[${i}].version`) ? <span className="err">{err(`params[${i}].version`)}</span> : null}
          </div>
        ))}
        {err("params") ? <span className="err">{err("params")}</span> : null}
        <button type="button" className="btn btn-o btn-sm" onClick={() => setPins((l) => [...l, { key: "", version: "1" }])} disabled={pins.length >= 20}>
          Thêm tham số
        </button>
      </fieldset>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang gửi…" : "Đề xuất và gửi duyệt"}
        </button>
      </div>
    </form>
  );
}

function MethodRow({ m, onChanged }: { m: MrvMethodVersion; onChanged: () => void }) {
  const { adminApi, principal } = useAuth();
  const canApprove = hasRole(principal, ROLES.paramApprover);
  const block = methodDecisionBlock(m, principal?.subject, canApprove);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const st = STATUS[m.status];

  const act = async (action: "approve" | "reject") => {
    setBusy(action);
    setProblem(null);
    const header = { "Idempotency-Key": newIdempotencyKey() };
    try {
      if (action === "approve") await unwrap(adminApi.POST("/v1/mrv/methods/{id}/approve", { params: { path: { id: m.id }, header }, body: note.trim() ? { note: note.trim() } : {} }));
      else await unwrap(adminApi.POST("/v1/mrv/methods/{id}/reject", { params: { path: { id: m.id }, header }, body: { note: note.trim() } }));
      onChanged();
    } catch (x) {
      setProblem(x instanceof ApiProblem ? x : null);
    } finally {
      setBusy(null);
    }
  };

  return (
    <li className="card" style={{ padding: 12 }} data-version={m.version} data-status={m.status}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", flexWrap: "wrap" }}>
        <div>
          <b>
            <code>{m.rule_version}</code> · phiên bản {m.version}
          </b>{" "}
          <span className={`chip ${st.chip}`}>{st.label}</span>
          <div className="small">
            Tham số ghim: {m.params.map((p) => `${p.key} v${p.version}`).join(", ")}
          </div>
          <div className="small mute">
            Căn cứ: {m.source_note} · Đề xuất: {who(m.setter_ref)} · {formatDateTime(m.created_at)}
          </div>
          {m.decided_at ? (
            <div className="small mute">
              {st.label}: {who(m.approver_ref)} · {formatDateTime(m.decided_at)}
              {m.decision_note ? ` · “${m.decision_note}”` : ""}
            </div>
          ) : null}
        </div>
        {m.status === "submitted" ? (
          <div className="stack" style={{ gap: 6, minWidth: 260 }}>
            <label className="small" htmlFor={`mm-dn-${m.id}`}>
              Ghi chú (bắt buộc ≥ 5 ký tự khi từ chối)
            </label>
            <input id={`mm-dn-${m.id}`} className="input" value={note} onChange={(e) => setNote(e.target.value)} disabled={!!block} />
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!!block || !!busy} aria-describedby={block ? `mm-b-${m.id}` : undefined} onClick={() => void act("approve")}>
                {busy === "approve" ? "Đang duyệt…" : "Phê duyệt"}
              </button>
              <button type="button" className="btn btn-o btn-sm" disabled={!!block || !!busy || note.trim().length < 5} onClick={() => void act("reject")}>
                {busy === "reject" ? "Đang từ chối…" : "Từ chối"}
              </button>
            </div>
            {block ? (
              <span id={`mm-b-${m.id}`} className="small mute">
                {block}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </li>
  );
}
