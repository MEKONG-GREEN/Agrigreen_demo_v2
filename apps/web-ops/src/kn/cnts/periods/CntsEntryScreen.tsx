"use client";

// Báo cáo kỳ CNTS — Cấp Xã (W11.42, BRD CNTS v3.7 UR-XA-06…09, 13): công việc của các xã / cơ sở giết mổ được phân công,
// nhập BM-01 (theo cơ sở, theo ngày), BM-04, BM-17 (theo xã, theo tuần), BM-05 (kết quả / kế hoạch tuần làm việc).
// Hỗ trợ: sao chép kỳ trước, báo không phát sinh, ghi chú (H08), lý do trễ (H09), lưu nháp trên máy, khóa lạc quan —
// người lưu sau được báo "Đã có người cập nhật lúc …" và phải tải bản mới (không ghi đè).
import { useEffect, useMemo, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { STATUS_LABEL, STATUS_TONE, createCntsPeriodApi, fieldErrors, groupFields, isLate, numberRule, payloadValues, type CntsDraft, type FormsTask } from "./api";

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
            <DataTable
              caption="Công việc"
              rows={ts}
              rowKey={(t) => t.id}
              empty="Không có công việc."
              columns={[
                { key: "form", header: "Biểu", cell: (t) => t.template_title },
                { key: "label", header: "Kỳ", cell: (t) => t.label },
                {
                  key: "unit",
                  header: "Đơn vị",
                  cell: (t) => (
                    <>
                      {unitName(t)}
                      {t.unit_kind === "facility" ? <span className="mute"> · xã {communeName(t.parent)}</span> : null}
                    </>
                  ),
                },
                { key: "due", header: "Hạn nộp", cell: (t) => dt(t.due_at) },
                {
                  key: "st",
                  header: "Trạng thái",
                  cell: (t) => (
                    <>
                      <StatusPill tone={STATUS_TONE[t.status]}>{STATUS_LABEL[t.status]}</StatusPill>
                      {t.late ? <span className="mute"> · Nộp trễ</span> : null}
                    </>
                  ),
                },
                {
                  key: "act",
                  header: "",
                  cell: (t) => (
                    <button className="btn btn-sm" onClick={() => setOpen(t)}>
                      {!t.open ? "Xem" : t.version > 0 ? "Sửa" : "Nhập"}
                    </button>
                  ),
                },
              ]}
            />
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
  const local = loadDraft(task);
  const init: Draft = local ?? { values: task.values, nil: task.nil_report, note: task.note, explanation: task.explanation };
  const [d, setD] = useState<Draft>(init);
  // Người dùng đã sửa biểu (số liệu, không phát sinh, ghi chú, giải trình, sao chép kỳ trước) kể từ khi mở.
  const edited = useRef(false);
  // Nháp máy chủ về sau khi người dùng đã sửa: không ghi đè, chỉ báo và để người dùng chủ động nạp.
  const [serverDraft, setServerDraft] = useState<CntsDraft | null>(null);
  // BF-CN-PUBLISH (UR-XA-13): nháp đã lưu lên máy chủ (máy khác / phiên trước) khi trên máy này chưa có nháp.
  // Nháp trên máy này luôn mới hơn (lưu sau mỗi thao tác) nên không hỏi máy chủ. Nháp máy chủ không mới hơn lần gửi
  // gần nhất thì bỏ qua. Phản hồi đến sau khi người dùng đã nhập thì không ghi đè (#108 [P2] Codex).
  useEffect(() => {
    if (local || !task.open) return;
    let alive = true;
    capi.draft(task.period_id, task.unit).then(
      (s) => {
        if (!alive || !s) return;
        if (task.submitted_at && !(Date.parse(s.saved_at) > Date.parse(task.submitted_at))) return;
        if (edited.current) setServerDraft(s);
        else setD((x) => ({ ...x, values: { ...s.values }, nil: s.nil_report, note: s.note }));
      },
      () => undefined,
    );
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chỉ nạp một lần khi mở công việc
  }, [task.id]);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [conflict, setConflict] = useState<string | null>(null);
  const act = useAction();
  const late = isLate(task);
  const update = (patch: Partial<Draft>) => {
    edited.current = true;
    setD((x) => {
      const n = { ...x, ...patch };
      saveDraft(task, n);
      return n;
    });
  };
  const readOnly = !task.open;

  const submit = async () => {
    const fe = fieldErrors(task.fields, d.values, d.nil);
    if (late && !d.explanation.trim()) fe.explanation = "Gửi sau hạn: bắt buộc ghi lý do trễ.";
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
      {serverDraft && !readOnly ? (
        <div className="alert alert-info" role="status">
          <span>Có bản nháp đã lưu trên máy chủ lúc {dt(serverDraft.saved_at)}. Chưa nạp vì bạn đã sửa biểu; nạp sẽ thay số liệu, mục không phát sinh và ghi chú đang nhập.</span>
          <button
            className="btn btn-sm"
            onClick={() => {
              update({ values: { ...serverDraft.values }, nil: serverDraft.nil_report, note: serverDraft.note });
              setServerDraft(null);
            }}
          >
            Nạp nháp máy chủ
          </button>
        </div>
      ) : null}
      {task.status === "awaiting_reopen" ? <p className="err">Chờ mở kỳ: bản gửi sau khi kỳ đã chốt — chờ Cấp Tỉnh mở lại kỳ.</p> : null}
      {task.status === "returned" ? (
        <p className="alert alert-warn" role="status">
          Cấp Tỉnh trả lại, yêu cầu điều chỉnh{task.return_note ? `: ${task.return_note}` : "."} Sửa và gửi lại trước khi chốt kỳ.
        </p>
      ) : null}
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
              const set = (x: string) => update({ values: { ...d.values, [f.key]: x } });
              if (numeric)
                return (
                  <NumberField
                    key={f.key}
                    label={f.label}
                    unit={f.unit ?? undefined}
                    rule={numberRule(f)}
                    disabled={locked}
                    value={v}
                    onChange={set}
                    error={errs[f.key]}
                    onBlurError={(e) => setErrs((x) => ({ ...x, [f.key]: e ?? "" }))}
                  />
                );
              if (f.type === "choice")
                return (
                  <SelectField
                    key={f.key}
                    label={f.label}
                    required={!!f.required}
                    disabled={locked}
                    value={v}
                    onChange={set}
                    error={errs[f.key]}
                    placeholder="—"
                    options={(f.options ?? []).map((o) => ({ value: o, label: o }))}
                  />
                );
              return <TextField key={f.key} label={f.unit ? `${f.label} (${f.unit})` : f.label} required={!!f.required} disabled={locked} value={v} onChange={set} error={errs[f.key]} />;
            })}
          </div>
        </fieldset>
      ))}
      <TextField label="H08 Ghi chú chung" multiline maxLength={1000} disabled={readOnly} value={d.note} onChange={(x) => update({ note: x })} />
      <TextField
        label="H09 Giải trình / lý do trễ"
        required={late}
        multiline
        maxLength={1000}
        disabled={readOnly}
        value={d.explanation}
        onChange={(x) => update({ explanation: x })}
        error={errs.explanation}
      />
      <div className="row">
        {!readOnly ? (
          <button
            className="btn"
            disabled={act.busy}
            onClick={() =>
              void act.run(
                () => capi.saveDraft(task.period_id, { unit: task.unit, values: d.values, nil_report: d.nil, ...(d.note ? { note: d.note } : {}) }),
                "Đã lưu nháp — Cấp Tỉnh thấy trạng thái Nháp.",
              )
            }
          >
            Lưu nháp
          </button>
        ) : null}
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

// Cho kiểm thử hồi quy nạp nháp máy chủ (publish.test.tsx).
export { Entry as CntsTaskEntry };
