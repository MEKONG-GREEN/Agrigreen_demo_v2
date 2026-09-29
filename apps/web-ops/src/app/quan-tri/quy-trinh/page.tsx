"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { AdminCropSOP, CropSOPStep, CropSOPTemplate } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { ReasonDialog, useCatalog } from "@/htx/common";
import { ACTIVITIES, ACTIVITY_LABEL, moveStep, renumber, validateSteps, type StepIssue } from "@/htx/rules";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const TPL_STATUS: Record<string, { text: string; cls: string }> = {
  draft: { text: "Bản nháp", cls: "c-warn" },
  published: { text: "Đã ban hành", cls: "c-ok" },
  retired: { text: "Ngừng dùng", cls: "c-mute" },
};

const DEFAULT_STEPS: CropSOPStep[] = [
  { seq: 1, name: "Làm đất", activity_code: "lam_dat", day_offset: -3, window_days: 2, mandatory: true },
  { seq: 2, name: "Gieo sạ", activity_code: "gieo_sa", day_offset: 0, window_days: 1, mandatory: true },
  { seq: 3, name: "Thu hoạch", activity_code: "thu_hoach", day_offset: 95, window_days: 5, mandatory: true },
];

/** Soạn bước SOP — thứ tự liên tục 1..n (HTX US-CAT-01 AC-6); đổi chỗ / thêm / xoá tự đánh số lại. */
function StepsEditor({ steps, onChange, readOnly, issues }: { steps: CropSOPStep[]; onChange: (s: CropSOPStep[]) => void; readOnly: boolean; issues: StepIssue[] }) {
  const set = (i: number, patch: Partial<CropSOPStep>) => onChange(steps.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  const bad = (i: number, f: keyof CropSOPStep) => issues.some((x) => x.index === i && x.field === f);
  const num = (v: string) => (v === "" ? 0 : Math.trunc(Number(v)));
  return (
    <div className="stack" style={{ gap: 8 }}>
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Các bước của quy trình</caption>
          <thead>
            <tr>
              <th scope="col">Thứ tự</th>
              <th scope="col">Tên bước</th>
              <th scope="col">Hoạt động</th>
              <th scope="col">Ngày so với sạ</th>
              <th scope="col">Cửa sổ (ngày)</th>
              <th scope="col">Cách ly (ngày)</th>
              <th scope="col">Bắt buộc</th>
              {!readOnly ? <th scope="col">Sắp xếp</th> : null}
            </tr>
          </thead>
          <tbody>
            {steps.map((s, i) => (
              <tr key={i}>
                <td>
                  <input className="input" style={{ width: 64 }} type="number" aria-label={`Thứ tự bước ${i + 1}`} aria-invalid={bad(i, "seq")} readOnly={readOnly} value={s.seq} onChange={(e) => set(i, { seq: num(e.target.value) })} />
                </td>
                <td>
                  <input className="input" aria-label={`Tên bước ${i + 1}`} aria-invalid={bad(i, "name")} readOnly={readOnly} value={s.name} maxLength={200} onChange={(e) => set(i, { name: e.target.value })} />
                </td>
                <td>
                  <select className="select" aria-label={`Hoạt động bước ${i + 1}`} disabled={readOnly} value={s.activity_code} onChange={(e) => set(i, { activity_code: e.target.value as CropSOPStep["activity_code"] })}>
                    {ACTIVITIES.map((a) => (
                      <option key={a} value={a}>
                        {ACTIVITY_LABEL[a]}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  <input className="input grid-input" type="number" aria-label={`Ngày so với sạ bước ${i + 1}`} aria-invalid={bad(i, "day_offset")} readOnly={readOnly} value={s.day_offset} onChange={(e) => set(i, { day_offset: num(e.target.value) })} />
                </td>
                <td>
                  <input className="input grid-input" type="number" aria-label={`Cửa sổ bước ${i + 1}`} aria-invalid={bad(i, "window_days")} readOnly={readOnly} value={s.window_days} onChange={(e) => set(i, { window_days: num(e.target.value) })} />
                </td>
                <td>
                  <input
                    className="input grid-input"
                    type="number"
                    aria-label={`Cách ly bước ${i + 1}`}
                    aria-invalid={bad(i, "phi_days")}
                    readOnly={readOnly}
                    value={s.phi_days ?? ""}
                    onChange={(e) => set(i, { phi_days: e.target.value === "" ? undefined : num(e.target.value) })}
                  />
                </td>
                <td>
                  <input type="checkbox" aria-label={`Bước ${i + 1} bắt buộc`} disabled={readOnly} checked={s.mandatory} onChange={(e) => set(i, { mandatory: e.target.checked })} />
                </td>
                {!readOnly ? (
                  <td>
                    <div className="row" style={{ gap: 4, flexWrap: "nowrap" }}>
                      <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển bước ${i + 1} lên`} disabled={i === 0} onClick={() => onChange(moveStep(steps, i, -1))}>
                        ↑
                      </button>
                      <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển bước ${i + 1} xuống`} disabled={i === steps.length - 1} onClick={() => onChange(moveStep(steps, i, 1))}>
                        ↓
                      </button>
                      <button type="button" className="btn btn-o btn-sm" aria-label={`Xoá bước ${i + 1}`} onClick={() => onChange(renumber(steps.filter((_, j) => j !== i)))}>
                        ✕
                      </button>
                    </div>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!readOnly ? (
        <div className="row">
          <button type="button" className="btn btn-o btn-sm" onClick={() => onChange([...steps, { seq: steps.length + 1, name: "", activity_code: "kiem_tra", day_offset: 0, window_days: 3, mandatory: false }])}>
            + Thêm bước
          </button>
          <button type="button" className="btn btn-o btn-sm" onClick={() => onChange(renumber(steps))}>
            Đánh số lại 1..n
          </button>
        </div>
      ) : null}
      {issues.length > 0 ? (
        <ul className="err small" role="alert" style={{ margin: "0 0 0 18px" }}>
          {Array.from(new Set(issues.map((x) => (x.index >= 0 ? `Bước ${x.index + 1}: ${x.message}` : x.message)))).map((m) => (
            <li key={m}>{m}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function CreateForm({ onCreated }: { onCreated: (ref: string) => void }) {
  const { adminApi } = useAuth();
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [standard, setStandard] = useState("");
  const [steps, setSteps] = useState<CropSOPStep[]>(DEFAULT_STEPS);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const issues = touched ? validateSteps(steps) : [];
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (validateSteps(steps).length || !code.trim() || !name.trim()) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/cropcycle/sops", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { code: code.trim().toUpperCase(), name: name.trim(), scope: "system", standard: standard.trim() || undefined, steps },
        }),
      );
      onCreated(res.ref);
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Tạo quy trình SOP">
      <b>Tạo quy trình SOP mới (bản nháp phiên bản 1)</b>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field">
          <label htmlFor="sop-code">Mã quy trình</label>
          <input id="sop-code" className="input" value={code} placeholder="vd SOP-OM5451" aria-invalid={touched && !code.trim()} onChange={(e) => setCode(e.target.value)} />
        </div>
        <div className="field" style={{ flex: "1 1 280px" }}>
          <label htmlFor="sop-name">Tên quy trình</label>
          <input id="sop-name" className="input" value={name} maxLength={200} aria-invalid={touched && !name.trim()} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="sop-std">Tiêu chuẩn</label>
          <input id="sop-std" className="input" value={standard} placeholder="vd VietGAP" onChange={(e) => setStandard(e.target.value)} />
        </div>
      </div>
      <StepsEditor steps={steps} onChange={setSteps} readOnly={false} issues={issues} />
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu bản nháp"}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}

function SopDetail({ refId, onChanged, onSelect }: { refId: string; onChanged: () => void; onSelect: (ref: string) => void }) {
  const { adminApi, principal } = useAuth();
  const sop = useAsync(() => unwrap(adminApi.GET("/v1/cropcycle/sops/{ref}", { params: { path: { ref: refId } } })), [adminApi, refId]);
  const [edit, setEdit] = useState<{ id: string; steps: CropSOPStep[] } | null>(null);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [dialog, setDialog] = useState<"" | "clone" | "retire">("");
  const [cloneName, setCloneName] = useState("");
  const canAuthor = hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin);
  const canPublish = hasRole(principal, ROLES.paramApprover);

  const data: AdminCropSOP | undefined = sop.data;
  const latest = data?.versions.at(-1);
  const draft = latest?.status === "draft" ? latest : undefined;
  const steps = edit && edit.id === latest?.id ? edit.steps : (latest?.steps ?? []);
  const issues = touched ? validateSteps(steps, false) : [];

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    try {
      await fn();
      setNotice(ok);
      setEdit(null);
      setTouched(false);
      sop.reload();
      onChanged();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  const key = () => ({ "Idempotency-Key": newIdempotencyKey() });

  if (sop.error) return <ProblemAlert problem={sop.error} onRetry={sop.reload} />;
  if (!data || !latest) return <Loading />;
  return (
    <div className="stack" style={{ gap: 12 }}>
      <div className="sec-h">
        <div>
          <h2>{data.name}</h2>
          <span className="mute small">
            <code>{data.code}</code> · {data.scope === "system" ? "Quy trình hệ thống" : "Quy trình riêng của HTX"} · <span className={`chip ${TPL_STATUS[data.status]?.cls ?? "c-mute"}`}>{TPL_STATUS[data.status]?.text ?? data.status}</span>
          </span>
        </div>
        <div className="row">
          {canAuthor && data.status !== "retired" ? (
            <>
              <button type="button" className="btn btn-o btn-sm" onClick={() => { setCloneName(`${data.name} (bản sao)`); setDialog("clone"); }}>
                Nhân bản
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog("retire")}>
                Ngừng dùng
              </button>
            </>
          ) : null}
        </div>
      </div>
      <p className="small">
        Phiên bản {latest.version_no} · <b style={{ display: "inline" }}>{latest.status === "draft" ? "Bản nháp" : "Đã phát hành (bất biến)"}</b> · soạn {formatDateTime(latest.authored_at)}
        {latest.published_at ? ` · phát hành ${formatDateTime(latest.published_at)}` : ""}
        {latest.change_note ? ` — ${latest.change_note}` : ""}
      </p>
      <StepsEditor steps={steps} onChange={(s) => setEdit({ id: latest.id, steps: s })} readOnly={!draft || !canAuthor} issues={issues} />
      <div className="row">
        {draft && canAuthor ? (
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={busy}
            onClick={() => {
              setTouched(true);
              if (validateSteps(steps).length) return;
              void act(() => unwrap(adminApi.POST("/v1/cropcycle/sop-versions/{id}/steps", { params: { path: { id: draft.id }, header: key() }, body: { steps } })), "Đã lưu bản nháp.");
            }}
          >
            Lưu bản nháp
          </button>
        ) : null}
        {draft && canPublish ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={busy || !!edit}
            title={edit ? "Lưu bản nháp trước khi phát hành" : undefined}
            onClick={() => {
              const pub = validateSteps(steps, true);
              if (pub.length) {
                setTouched(true);
                setProblem(new ApiProblem({ type: "", title: pub[0]!.message, status: 422, code: "cropcycle.sop_harvest_step_missing" }));
                return;
              }
              void act(() => unwrap(adminApi.POST("/v1/cropcycle/sop-versions/{id}/publish", { params: { path: { id: draft.id }, header: key() } })), "Đã phát hành — vụ mở mới sẽ ghim phiên bản này.");
            }}
          >
            Phát hành
          </button>
        ) : null}
        {draft && !canPublish ? <span className="small mute">Phát hành cần người phê duyệt (param_approver) khác người soạn.</span> : null}
        {!draft && canAuthor && data.status !== "retired" ? (
          <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void act(() => unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/versions", { params: { path: { ref: data.ref }, header: key() }, body: {} })), "Đã tạo bản nháp phiên bản mới.")}>
            Tạo phiên bản mới
          </button>
        ) : null}
      </div>
      {notice ? <p role="status">{notice}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      <details>
        <summary className="small">Các phiên bản ({data.versions.length})</summary>
        <ul className="small" style={{ margin: "6px 0 0 18px" }}>
          {data.versions.map((v) => (
            <li key={v.id}>
              Phiên bản {v.version_no} — {v.status === "draft" ? "nháp" : "đã phát hành"} · {v.steps.length} bước · thu hoạch ngày {v.harvest_day_offset ?? "—"}
            </li>
          ))}
        </ul>
      </details>
      {dialog === "clone" ? (
        <Dialog
          title={`Nhân bản ${data.name}`}
          onClose={() => setDialog("")}
          actions={
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={busy || !cloneName.trim()}
              onClick={() =>
                void (async () => {
                  setBusy(true);
                  setProblem(null);
                  try {
                    const res = await unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/clone", { params: { path: { ref: data.ref }, header: key() }, body: { name: cloneName.trim() } }));
                    setDialog("");
                    onChanged();
                    onSelect(res.ref);
                  } catch (e) {
                    setProblem(toApiProblem(e));
                  } finally {
                    setBusy(false);
                  }
                })()
              }
            >
              Nhân bản
            </button>
          }
        >
          <p className="small mute">Bản sao giữ nguyên toàn bộ bước và thứ tự; sửa bản sao không ảnh hưởng quy trình nguồn.</p>
          <div className="field">
            <label htmlFor="clone-name">Tên quy trình mới</label>
            <input id="clone-name" className="input" value={cloneName} onChange={(e) => setCloneName(e.target.value)} />
          </div>
          {problem ? <ProblemAlert problem={problem} /> : null}
        </Dialog>
      ) : null}
      {dialog === "retire" ? (
        <ReasonDialog
          title={`Ngừng dùng ${data.name}`}
          label="Lý do (ít nhất 10 ký tự)"
          confirmText="Ngừng dùng"
          danger
          onClose={() => setDialog("")}
          run={async (reason, k) => {
            await unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/retire", { params: { path: { ref: data.ref }, header: { "Idempotency-Key": k } }, body: { reason } }));
            setDialog("");
            sop.reload();
            onChanged();
          }}
        >
          <p className="small mute">Quy trình không bị xoá; vụ đã ghim phiên bản vẫn giữ nguyên.</p>
        </ReasonDialog>
      ) : null}
    </div>
  );
}

/** Gắn giống lúa ↔ mẫu SOP hệ thống đã ban hành (tự gán khi mở vụ, HTX US-SEASON-01 AC-1). */
function Bindings({ templates, version }: { templates: CropSOPTemplate[]; version: number }) {
  const { api, adminApi, principal } = useAuth();
  const varieties = useCatalog("rice_variety");
  const bindings = useAsync(() => unwrap(api.GET("/v1/cropcycle/variety-bindings")), [api, version]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const canBind = hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin);
  const published = templates.filter((t) => t.scope === "system" && t.status === "published");
  const bind = async (variety: string, template: string) => {
    setProblem(null);
    setNotice(null);
    try {
      await unwrap(adminApi.POST("/v1/cropcycle/variety-bindings", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { variety_code: variety, template_ref: template } }));
      setNotice(`Đã gắn giống ${variety}.`);
      bindings.reload();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };
  return (
    <section className="card sec" aria-labelledby="bind-title" style={{ marginTop: 16 }}>
      <div className="sec-h">
        <h2 id="bind-title">Gắn giống lúa ↔ quy trình SOP</h2>
      </div>
      {bindings.error ? <ProblemAlert problem={bindings.error} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Gắn giống lúa với quy trình</caption>
          <thead>
            <tr>
              <th scope="col">Giống lúa</th>
              <th scope="col">Quy trình mặc định</th>
            </tr>
          </thead>
          <tbody>
            {varieties.map((v) => {
              const cur = bindings.data?.items.find((b) => b.variety_code === v.code);
              return (
                <tr key={v.code}>
                  <td>{v.name}</td>
                  <td>
                    <select className="select" aria-label={`Quy trình cho giống ${v.name}`} disabled={!canBind} value={cur?.template_ref ?? ""} onChange={(e) => e.target.value && void bind(v.code, e.target.value)}>
                      <option value="">— Chưa gắn —</option>
                      {published.map((t) => (
                        <option key={t.ref} value={t.ref}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {notice ? <p role="status">{notice}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
    </section>
  );
}

/** Quy trình SOP (Admin API, MFA): soạn bước, nhân bản, phát hành bởi người phê duyệt khác (HTX US-CAT-01/03). */
export default function SopPage() {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const [picked, setPicked] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const list = useAsync(() => collectPages((cursor) => unwrap(adminApi.GET("/v1/cropcycle/sops", { params: { query: { cursor, limit: 200 } } })), 500), [adminApi, version]);
  const templates = list.data ?? [];
  const selected = picked ?? templates[0]?.ref ?? null;
  const changed = () => setVersion((v) => v + 1);
  return (
    <>
      <PageHead
        title="Quy trình SOP"
        subtitle="Quy trình canh tác mẫu có phiên bản — bản đã phát hành bất biến, vụ mở ghim phiên bản"
        actions={
          hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin) ? (
            <button type="button" className="btn btn-p btn-sm" aria-expanded={creating} onClick={() => setCreating((v) => !v)}>
              <Icon name="sop" size={15} />
              Tạo quy trình
            </button>
          ) : null
        }
      />
      <MfaNotice />
      {creating ? (
        <CreateForm
          onCreated={(ref) => {
            setCreating(false);
            setPicked(ref);
            changed();
          }}
        />
      ) : null}
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(240px, 320px) minmax(0, 1fr)", alignItems: "start" }}>
        <nav className="card sec" aria-label="Danh sách quy trình">
          {list.loading && !list.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }} className="stack" role="list">
            {templates.map((t) => (
              <li key={t.ref} style={{ margin: 0 }}>
                <button
                  type="button"
                  className="persona"
                  aria-current={t.ref === selected ? "true" : undefined}
                  style={t.ref === selected ? { borderColor: "var(--g600)", background: "var(--g50)" } : undefined}
                  onClick={() => setPicked(t.ref)}
                >
                  <b>{t.name}</b>
                  <span className="mute small">
                    <code>{t.code}</code> · <span className={`chip ${TPL_STATUS[t.status]?.cls ?? "c-mute"}`}>{TPL_STATUS[t.status]?.text ?? t.status}</span> · v{t.latest_version}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          {!list.loading && templates.length === 0 ? <Empty>Chưa có quy trình.</Empty> : null}
        </nav>
        <section className="card sec" aria-label="Chi tiết quy trình">
          {selected ? <SopDetail key={selected} refId={selected} onChanged={changed} onSelect={setPicked} /> : <p className="mute">Chọn một quy trình.</p>}
        </section>
      </div>
      <Bindings templates={templates} version={version} />
    </>
  );
}
