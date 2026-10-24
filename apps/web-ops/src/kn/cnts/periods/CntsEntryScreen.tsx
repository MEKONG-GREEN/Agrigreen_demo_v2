"use client";

// Báo cáo kỳ CNTS — Cấp Xã (W11.42, BRD CNTS v3.7 UR-XA-06…09, 13): công việc của các xã / cơ sở giết mổ được phân công,
// nhập BM-01 (theo cơ sở, theo ngày), BM-04, BM-17 (theo xã, theo tuần), BM-05 (kết quả / kế hoạch tuần làm việc).
// Hỗ trợ: sao chép kỳ trước, báo không phát sinh, ghi chú (H08), lý do trễ (H09), lưu nháp trên máy, khóa lạc quan —
// người lưu sau được báo "Đã có người cập nhật lúc …" và phải tải bản mới (không ghi đè).
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { STATUS_LABEL, STATUS_TONE, createCntsPeriodApi, fieldErrors, groupFields, isLate, payloadValues, type FormsTask } from "./api";

interface Draft {
  values: Record<string, string>;
  nil: boolean;
  note: string;
  explanation: string;
}

const draftKey = (t: FormsTask) => `cnts-draft:${t.id}:v${t.version}`;

function loadDraft(t: FormsTask): Draft | null {
  try {
    const raw = window.localStorage.getItem(draftKey(t));
    return raw ? (JSON.parse(raw) as Draft) : null;
  } catch {
    return null;
  }
}

function saveDraft(t: FormsTask, d: Draft) {
  try {
    window.localStorage.setItem(draftKey(t), JSON.stringify(d));
  } catch {
    /* bộ nhớ trình duyệt bị chặn — bỏ qua */
  }
}

function clearDraft(t: FormsTask) {
  try {
    window.localStorage.removeItem(draftKey(t));
  } catch {
    /* bỏ qua */
  }
}

export function CntsEntryScreen() {
  const { api } = useAuth();
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const [code, setCode] = useState("");
  const forms = useLoad(capi ? () => capi.forms() : null, [capi]);
  const tasks = useLoad(capi ? () => capi.tasks(code || undefined) : null, [capi, code]);
  const facilities = useLoad(
    api ? async () => ((await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_slaughter_facility" }, query: {} } }))).items as CatalogItem[]) : null,
    [api],
  );
  const [open, setOpen] = useState<FormsTask | null>(null);
  const communeName = useCommuneNames((tasks.data ?? []).map((t) => t.parent));
  const unitName = (t: FormsTask) =>
    t.unit_kind === "facility" ? (facilities.data?.find((f) => f.attrs.facility_ref === t.unit)?.name ?? t.unit) : communeName(t.unit);

  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader title="Báo cáo kỳ — Chăn nuôi, Thủy sản">
        <p className="mute">Công việc cần làm của xã, cơ sở giết mổ được phân công (FN-08, FN-09).</p>
      </PageHeader>
      <section className="card sec">
        <select className="select" aria-label="Lọc biểu" value={code} onChange={(e) => setCode(e.target.value)}>
          <option value="">Mọi biểu</option>
          {(forms.data ?? []).map((f) => (
            <option key={f.code} value={f.code}>
              {f.bm} — {f.title}
            </option>
          ))}
        </select>
        <LoadState s={tasks}>
          {(ts: FormsTask[]) => (
            <table className="dt" aria-label="Công việc">
              <thead>
                <tr>
                  <th>Biểu</th>
                  <th>Kỳ</th>
                  <th>Đơn vị</th>
                  <th>Hạn nộp</th>
                  <th>Trạng thái</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {ts.map((t) => (
                  <tr key={t.id}>
                    <td>{t.template_title}</td>
                    <td>{t.label}</td>
                    <td>
                      {unitName(t)}
                      {t.unit_kind === "facility" ? <span className="mute"> · xã {communeName(t.parent)}</span> : null}
                    </td>
                    <td>{dt(t.due_at)}</td>
                    <td>
                      <StatusPill tone={STATUS_TONE[t.status]}>{STATUS_LABEL[t.status]}</StatusPill>
                      {t.late ? <span className="mute"> · Nộp trễ</span> : null}
                    </td>
                    <td>
                      <button className="btn btn-sm" onClick={() => setOpen(t)}>
                        {!t.open ? "Xem" : t.version > 0 ? "Sửa" : "Nhập"}
                      </button>
                    </td>
                  </tr>
                ))}
                {ts.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="mute">
                      Không có công việc.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          )}
        </LoadState>
      </section>
      {open ? (
        <Entry
          key={`${open.id}:${open.version}`}
          task={open}
          unitLabel={unitName(open)}
          nilLabel={forms.data?.find((f) => f.code === open.template_code)?.nil_label ?? null}
          capi={capi}
          onDone={async (reload) => {
            if (reload) {
              tasks.reload();
              const fresh = (await capi.tasks(open.template_code)).find((x) => x.id === open.id);
              setOpen(fresh ?? null);
            } else setOpen(null);
          }}
        />
      ) : null}
    </div>
  );
}

function Entry({
  task,
  unitLabel,
  nilLabel,
  capi,
  onDone,
}: {
  task: FormsTask;
  unitLabel: string;
  nilLabel: string | null;
  capi: ReturnType<typeof createCntsPeriodApi>;
  onDone: (reload: boolean) => void;
}) {
  const init: Draft = loadDraft(task) ?? { values: task.values, nil: task.nil_report, note: task.note, explanation: task.explanation };
  const [d, setD] = useState<Draft>(init);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [conflict, setConflict] = useState<string | null>(null);
  const act = useAction();
  const late = isLate(task);
  const update = (patch: Partial<Draft>) =>
    setD((x) => {
      const n = { ...x, ...patch };
      saveDraft(task, n);
      return n;
    });
  const readOnly = !task.open;

  const submit = async () => {
    const fe = fieldErrors(task.fields, d.values, d.nil);
    if (late && !d.explanation.trim()) fe.explanation = "Gửi sau hạn: bắt buộc ghi lý do trễ";
    setErrs(fe);
    if (Object.keys(fe).length) return;
    try {
      // P0-WEB-IDEMKEY: trong phiên thao tác của act — gửi lại sau lỗi mạng dùng lại khóa cũ.
      await act.idem(() =>
        capi.submit(task.period_id, {
          unit: task.unit,
          values: payloadValues(task.fields, d.values, d.nil),
          base_version: task.version,
          nil_report: d.nil,
          note: d.note || undefined,
          explanation: d.explanation || undefined,
        }),
      );
      clearDraft(task);
      act.setNote("Đã gửi báo cáo.");
      onDone(true);
    } catch (e) {
      if (e instanceof ApiProblem && e.problem.code === "forms.submission_conflict") {
        setConflict(e.problem.detail ?? e.problem.title);
        return;
      }
      if (e instanceof ApiProblem) {
        const m: Record<string, string> = {};
        for (const x of e.problem.errors ?? []) if (x.field) m[x.field] = x.reason ?? "";
        setErrs(m);
      }
      await act.run(() => Promise.reject(e));
    }
  };

  return (
    <section className="card sec" aria-label="Nhập báo cáo">
      <h2>
        {task.template_title} · {task.label}
      </h2>
      <p className="mute">
        {unitLabel} · hạn {dt(task.due_at)}
        {task.version > 0 ? ` · phiên bản ${task.version}, gửi lúc ${dt(task.submitted_at)}` : ""}
        {late && task.open ? " · Trễ hạn — vẫn gửi được, ghi lý do trễ" : ""}
      </p>
      {act.view}
      {conflict ? (
        <div className="alert alert-warn" role="alert">
          <span>{conflict}</span>
          <button
            className="btn btn-sm"
            onClick={() => {
              clearDraft(task);
              onDone(true);
            }}
          >
            Tải bản mới
          </button>
        </div>
      ) : null}
      {task.status === "awaiting_reopen" ? <p className="err">Chờ mở kỳ: bản gửi sau khi kỳ đã chốt — chờ Cấp Tỉnh mở lại kỳ.</p> : null}
      {!readOnly ? (
        <div className="row">
          <button
            className="btn btn-sm"
            onClick={async () => {
              const p = await act.run(() => capi.previous(task.period_id, task.unit));
              if (p) update({ values: { ...p.submission.values }, nil: p.submission.nil_report });
              else if (p === null) act.setNote("Chưa có kỳ trước để sao chép.");
            }}
          >
            Sao chép kỳ trước
          </button>
          {nilLabel ? (
            <label className="row">
              <input type="checkbox" checked={d.nil} onChange={(e) => update({ nil: e.target.checked })} /> {nilLabel}
            </label>
          ) : null}
        </div>
      ) : null}
      {groupFields(task.fields).map((g) => (
        <fieldset key={g.group} className="field">
          {g.group ? <legend>{g.group}</legend> : null}
          <div className="ws-two">
            {g.fields.map((f) => {
              const numeric = f.type === "integer" || f.type === "decimal";
              const locked = readOnly || (d.nil && numeric);
              const v = d.nil && numeric ? "0" : (d.values[f.key] ?? "");
              return (
                <label key={f.key} className="field">
                  <span>
                    {f.label}
                    {f.unit ? ` (${f.unit})` : ""}
                    {f.required ? <span className="req"> *</span> : null}
                  </span>
                  {f.type === "choice" ? (
                    <select className="select" aria-label={f.label + " " + (g.group ?? "")} disabled={locked} value={v} onChange={(e) => update({ values: { ...d.values, [f.key]: e.target.value } })}>
                      <option value="">—</option>
                      {(f.options ?? []).map((o) => (
                        <option key={o}>{o}</option>
                      ))}
                    </select>
                  ) : (
                    <input
                      className="input"
                      aria-label={f.label + " " + (g.group ?? "")}
                      inputMode={numeric ? "decimal" : undefined}
                      disabled={locked}
                      value={v}
                      onChange={(e) => update({ values: { ...d.values, [f.key]: e.target.value } })}
                    />
                  )}
                  {errs[f.key] ? <span className="err">{errs[f.key]}</span> : null}
                </label>
              );
            })}
          </div>
        </fieldset>
      ))}
      <label className="field">
        <span>H08 Ghi chú chung</span>
        <textarea className="input" aria-label="Ghi chú chung" maxLength={1000} disabled={readOnly} value={d.note} onChange={(e) => update({ note: e.target.value })} />
      </label>
      <label className="field">
        <span>H09 Giải trình / lý do trễ{late ? <span className="req"> *</span> : null}</span>
        <textarea className="input" aria-label="Giải trình" maxLength={1000} disabled={readOnly} value={d.explanation} onChange={(e) => update({ explanation: e.target.value })} />
        {errs.explanation ? <span className="err">{errs.explanation}</span> : null}
      </label>
      <div className="row">
        {!readOnly ? (
          <button className="btn btn-primary" disabled={act.busy} onClick={submit}>
            Gửi báo cáo
          </button>
        ) : null}
        <button className="btn" onClick={() => onDone(false)}>
          Đóng
        </button>
      </div>
    </section>
  );
}
