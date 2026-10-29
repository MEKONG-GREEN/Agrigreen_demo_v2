"use client";

// Mẫu biểu dự án KNQG (W11.53 — BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01; prototype A "project_reports" BM-12…16):
// danh sách biên bản/báo cáo theo dự án, mẫu, trạng thái; lập biểu từ dự án (số liệu điền sẵn); soạn theo đúng mục, bảng của
// mẫu Word gốc; trình duyệt/duyệt/trả lại (Mẫu 01), trình ký/tải bản đã ký (03, 04, 05), gửi Quốc gia và TW xác nhận/trả lại
// (Mẫu 07); xuất Word đúng mẫu. Máy chủ quyết quyền và thao tác được phép (trường actions, can_edit).
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { fieldErrorMap } from "@/api/problem";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { createCntsModelApi } from "@/kn/cnts/model/api";
import { ReasonBox } from "@/kn/cnts/model/parts";
import {
  ACTION_LABEL,
  FORM_LABEL,
  NEEDS_REASON,
  STATUS_LABEL,
  STATUS_TONE,
  contentErrors,
  contentForSave,
  createErrors,
  createKnqgApi,
  docFromSearch,
  emptyRow,
  periodLabel,
  photoError,
  saveBlob,
  type Action,
  type Content,
  type Doc,
  type DocDetail,
  type KnqgApi,
  type PhotoList,
  type Row,
  type Spec,
  type SpecTable,
} from "./api";

const FORMS = ["01", "03", "04", "05", "07"] as const;
const KNQG_PATH = "/kn/cnts/du-an-mau-bieu";

export function KnqgScreen() {
  const { api, kn } = useAuth();
  const kapi = useMemo(() => (api ? createKnqgApi(api) : null), [api]);
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const role = kn.context?.role;
  const [project, setProject] = useState("");
  const [form, setForm] = useState<"" | Doc["form"]>("");
  const [status, setStatus] = useState("");
  const specs = useLoad(kapi ? () => kapi.specs() : null, [kapi]);
  const projects = useLoad(mapi && role !== "kn_xa" ? async () => (await mapi.projects()).items.filter((p) => p.kind === "knqg") : null, [mapi, role]);
  const list = useLoad(
    kapi ? () => kapi.list({ ...(project ? { project_ref: project } : {}), ...(form ? { form } : {}), ...(status ? { status } : {}) }) : null,
    [kapi, project, form, status],
  );
  // W11.53b: thông báo hộp thư mở thẳng biểu (/kn/cnts/du-an-mau-bieu?doc=<mã>). Theo dõi tham số URL (không chỉ lúc
  // tải trang) để bấm thông báo khi đang ở chính màn này vẫn mở đúng biểu; đóng/chọn biểu khác thì bỏ ?doc= khỏi URL để
  // bấm lại cùng thông báo vẫn mở được.
  const router = useRouter();
  const linked = docFromSearch(useSearchParams()?.toString() ?? "");
  const [open, setOpenState] = useState<string | null>(linked);
  const [seenLinked, setSeenLinked] = useState(linked);
  if (linked !== seenLinked) {
    setSeenLinked(linked);
    if (linked) setOpenState(linked);
  }
  const setOpen = (ref: string | null) => {
    setOpenState(ref);
    if (linked && ref !== linked) router.replace(KNQG_PATH);
  };
  const [creating, setCreating] = useState(false);
  const loadSites = useCallback(async (ref: string) => (mapi ? (await mapi.project(ref)).sites.filter((s) => s.status === "active") : []), [mapi]);
  if (!kapi || !mapi) return null;
  const canCreate = role === "kn_tinh" || role === "admin";
  return (
    <div className="page">
      <PageHeader
        title="Mẫu biểu dự án KNQG"
        actions={
          canCreate && !creating ? (
            <button className="btn btn-primary" onClick={() => setCreating(true)}>
              Lập biểu
            </button>
          ) : null
        }
      >
        <p className="mute">
          Mẫu 01 tiến độ tháng, 03 kiểm tra đánh giá, 04 nghiệm thu tập huấn, 05 nghiệm thu điểm trình diễn, 07 nghiệm thu năm — đúng mẫu TTKN Quốc gia.
          {role === "kn_tw" ? " Cấp TW xem dự án KNQG, xác nhận hoặc trả lại Mẫu 07." : ""}
          {role === "kn_xa" ? " Cấp Xã xem biên bản nghiệm thu (Mẫu 05) của điểm trình diễn thuộc xã mình." : ""}
        </p>
      </PageHeader>
      {creating && specs.data ? (
        <CreateForm
          kapi={kapi}
          specs={specs.data}
          projects={projects.data ?? []}
          loadSites={loadSites}
          onDone={(ref) => {
            setCreating(false);
            if (ref) {
              list.reload();
              setOpen(ref);
            }
          }}
        />
      ) : null}
      <div className="row" style={{ gap: 12, flexWrap: "wrap" }}>
        {role !== "kn_xa" ? (
          <label>
            Dự án{" "}
            <select value={project} onChange={(e) => setProject(e.target.value)}>
              <option value="">Tất cả</option>
              {(projects.data ?? []).map((p) => (
                <option key={p.ref} value={p.ref}>
                  {p.code} — {p.name}
                </option>
              ))}
            </select>
          </label>
        ) : null}
        <label>
          Mẫu{" "}
          <select value={form} onChange={(e) => setForm(e.target.value as typeof form)}>
            <option value="">Tất cả</option>
            {FORMS.map((f) => (
              <option key={f} value={f}>
                {FORM_LABEL[f]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Trạng thái{" "}
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">Tất cả</option>
            {(Object.keys(STATUS_LABEL) as Doc["status"][]).map((s) => (
              <option key={s} value={s}>
                {STATUS_LABEL[s]}
              </option>
            ))}
          </select>
        </label>
      </div>
      <LoadState s={list}>
        {(items) => (
          <DataTable
            caption="Biên bản, báo cáo dự án KNQG"
            rows={items}
            rowKey={(d) => d.ref}
            empty="Chưa có biểu nào."
            columns={[
              { key: "stt", header: "STT", cell: (d) => items.indexOf(d) + 1 },
              {
                key: "form",
                header: "Mẫu",
                cell: (d) => (
                  <button className="link" aria-expanded={open === d.ref} onClick={() => setOpen(open === d.ref ? null : d.ref)}>
                    {FORM_LABEL[d.form]}
                  </button>
                ),
              },
              { key: "project", header: "Dự án", cell: (d) => `${d.project_code} — ${d.project_name}` },
              { key: "where", header: "Điểm / kỳ", cell: (d) => d.site_name ?? periodLabel(d.period) },
              { key: "rev", header: "Bản", cell: (d) => (d.revision > 1 ? `Điều chỉnh lần ${d.revision - 1}` : "Bản đầu") },
              { key: "st", header: "Trạng thái", cell: (d) => <StatusPill tone={STATUS_TONE[d.status]}>{STATUS_LABEL[d.status]}</StatusPill> },
              { key: "at", header: "Cập nhật", cell: (d) => `${new Date(d.updated_at).toLocaleDateString("vi-VN")}${d.actor_display ? ` · ${d.actor_display}` : ""}` },
            ]}
          />
        )}
      </LoadState>
      {open && specs.data ? <DocPanel key={open} kapi={kapi} docRef={open} specs={specs.data} onChanged={list.reload} /> : null}
    </div>
  );
}

function CreateForm({
  kapi,
  specs,
  projects,
  loadSites,
  onDone,
}: {
  kapi: KnqgApi;
  specs: Spec[];
  projects: { ref: string; code: string; name: string }[];
  loadSites: (projectRef: string) => Promise<{ ref: string; name: string }[]>;
  onDone: (ref?: string) => void;
}) {
  const act = useAction();
  const [v, setV] = useState({ form: "01", project_ref: projects[0]?.ref ?? "", site_ref: "", period: "" });
  const [sites, setSites] = useState<{ ref: string; name: string }[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const spec = specs.find((s) => s.form === v.form);
  useEffect(() => {
    if (!v.project_ref || !spec?.per_site) return;
    let live = true;
    loadSites(v.project_ref).then((s) => live && setSites(s), () => live && setSites([]));
    return () => {
      live = false;
    };
  }, [v.project_ref, spec?.per_site, loadSites]);
  const submit = async () => {
    const e = createErrors(spec, v);
    setErrors(e);
    if (Object.keys(e).length) return;
    const d = await act.run(() =>
      kapi.create({ form: v.form as Doc["form"], project_ref: v.project_ref, ...(spec?.per_site ? { site_ref: v.site_ref } : {}), ...(spec?.period ? { period: v.period } : {}) }),
    );
    if (d) onDone(d.ref);
    else if (act.problem) setErrors(fieldErrorMap(act.problem));
  };
  return (
    <section className="card" aria-label="Lập biểu">
      <h2>Lập biểu từ dự án</h2>
      <div className="row" style={{ gap: 12, flexWrap: "wrap" }}>
        <label>
          Mẫu{" "}
          <select value={v.form} onChange={(e) => setV({ ...v, form: e.target.value, period: "", site_ref: "" })}>
            {specs.map((s) => (
              <option key={s.form} value={s.form}>
                {s.code} — {s.title} ({s.bm})
              </option>
            ))}
          </select>
        </label>
        <label>
          Dự án KNQG{" "}
          <select value={v.project_ref} onChange={(e) => setV({ ...v, project_ref: e.target.value, site_ref: "" })}>
            <option value="">— chọn —</option>
            {projects.map((p) => (
              <option key={p.ref} value={p.ref}>
                {p.code} — {p.name}
              </option>
            ))}
          </select>
          {errors.project_ref ? <span className="field-error">{errors.project_ref}</span> : null}
        </label>
        {spec?.per_site ? (
          <label>
            Điểm trình diễn{" "}
            <select value={v.site_ref} onChange={(e) => setV({ ...v, site_ref: e.target.value })}>
              <option value="">— chọn —</option>
              {sites.map((s) => (
                <option key={s.ref} value={s.ref}>
                  {s.name}
                </option>
              ))}
            </select>
            {errors.site_ref ? <span className="field-error">{errors.site_ref}</span> : null}
          </label>
        ) : null}
        {spec?.period === "month" ? (
          <label>
            Tháng <input type="month" value={v.period} onChange={(e) => setV({ ...v, period: e.target.value })} />
            {errors.period ? <span className="field-error">{errors.period}</span> : null}
          </label>
        ) : null}
        {spec?.period === "year" ? (
          <label>
            Năm <input inputMode="numeric" maxLength={4} value={v.period} onChange={(e) => setV({ ...v, period: e.target.value.trim() })} />
            {errors.period ? <span className="field-error">{errors.period}</span> : null}
          </label>
        ) : null}
      </div>
      {act.view}
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={submit}>
          Lập biểu
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy
        </button>
      </div>
    </section>
  );
}

function DocPanel({ kapi, docRef, specs, onChanged }: { kapi: KnqgApi; docRef: string; specs: Spec[]; onChanged: () => void }) {
  const doc = useLoad(() => kapi.get(docRef), [kapi, docRef]);
  return <LoadState s={doc}>{(d) => <DocEditor key={d.version} kapi={kapi} doc={d} spec={specs.find((s) => s.form === d.form)!} onSaved={() => (doc.reload(), onChanged())} />}</LoadState>;
}

function DocEditor({ kapi, doc, spec, onSaved }: { kapi: KnqgApi; doc: DocDetail; spec: Spec; onSaved: () => void }) {
  const act = useAction();
  const [c, setC] = useState<Content>(() => structuredClone(doc.content));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [reasonFor, setReasonFor] = useState<Action | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const words = useLoad(() => kapi.words(doc.ref), [kapi, doc.ref, doc.version]);
  const edit = doc.can_edit;
  const issues = Object.fromEntries(doc.issues.map((i) => [i.field, i.reason]));
  const err = (k: string) => errors[k] ?? issues[k];
  const setField = (k: string, val: string) => setC({ ...c, fields: { ...c.fields, [k]: val } });
  const setRows = (k: string, rows: Row[]) => setC({ ...c, tables: { ...c.tables, [k]: rows } });

  const save = async () => {
    const e = contentErrors(spec, c);
    setErrors(e);
    if (Object.keys(e).length) return false;
    const r = await act.run(() => kapi.update(doc.ref, doc.version, contentForSave(spec, c)), "Đã lưu.");
    if (!r && act.problem) setErrors(fieldErrorMap(act.problem));
    if (r) onSaved();
    return !!r;
  };
  const run = async (a: Action, reason?: string) => {
    const r = await act.run(async (idem) => {
      let base = doc.version;
      // Đang sửa dở mà bấm trình/gửi: lưu trước (cùng lượt thao tác).
      if (edit && a !== "adjust" && JSON.stringify(contentForSave(spec, c)) !== JSON.stringify(contentForSave(spec, doc.content))) {
        base = (await idem(() => kapi.update(doc.ref, doc.version, contentForSave(spec, c)))).version;
      }
      return idem(() => kapi.act(doc.ref, a as Exclude<Action, "sign">, base, reason));
    }, `${ACTION_LABEL[a]}: xong.`);
    if (!r && act.problem) setErrors(fieldErrorMap(act.problem));
    setReasonFor(null);
    if (r) onSaved();
  };
  const sign = async () => {
    if (!file) return setErrors({ file: "Chọn tệp scan (PDF, JPG, PNG ≤ 10 MB)." });
    if (file.size > 10 * 1024 * 1024) return setErrors({ file: "Tệp quá 10 MB." });
    const r = await act.run(() => kapi.sign(doc.ref, doc.version, file), "Đã ký — đã lưu bản scan.");
    if (r) onSaved();
  };
  const word = async () => {
    const run = await act.run(() => kapi.exportWord(doc.ref), (w) => `Đã xuất Word${w.doc_number ? ` số ${w.doc_number}` : ""}.`);
    if (run) {
      words.reload();
      saveBlob(await kapi.download(doc.ref, run.id), run.file_name);
    }
  };

  return (
    <section className="card" aria-label={`${spec.code} ${spec.title}`}>
      <h2>
        {spec.code} — {spec.title} <span className="mute">({spec.bm})</span>
      </h2>
      <p>
        <StatusPill tone={STATUS_TONE[doc.status]}>{STATUS_LABEL[doc.status]}</StatusPill> · {doc.project_code} — {doc.project_name}
        {doc.site_name ? ` · ${doc.site_name}` : ""}
        {doc.period ? ` · ${periodLabel(doc.period)}` : ""}
        {doc.revision > 1 ? ` · Bản điều chỉnh lần ${doc.revision - 1}` : ""}
      </p>
      {doc.status_reason ? (
        <div className="alert alert-warn" role="note">
          Ý kiến / lý do: {doc.status_reason}
        </div>
      ) : null}
      {doc.blockers.length ? (
        <div className="alert alert-warn" role="note">
          <strong>Chưa gửi được Mẫu 07 (cần Mẫu 05 đã ký ở mọi điểm):</strong>
          <ul>
            {doc.blockers.map((b) => (
              <li key={b}>{b}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {doc.warnings.map((w) => (
        <div key={w} className="alert alert-warn" role="note">
          {w}
        </div>
      ))}
      {spec.sections.map((sec) => (
        <fieldset key={sec.title} className="card-section">
          <legend>{sec.title}</legend>
          {(sec.fields ?? []).map((k) => {
            const f = spec.fields.find((x) => x.key === k)!;
            const val = c.fields[k] ?? "";
            const ro = !edit || f.auto;
            return (
              <label key={k} className="field" style={{ display: "block", marginBottom: 8 }}>
                <span>
                  {f.label}
                  {f.required ? " *" : ""}
                  {f.auto ? <span className="mute"> (tự điền)</span> : null}
                </span>
                {f.kind === "long" ? (
                  <textarea rows={3} value={val} readOnly={ro} maxLength={4000} onChange={(e) => setField(k, e.target.value)} />
                ) : f.kind === "choice" ? (
                  <select value={val} disabled={ro} onChange={(e) => setField(k, e.target.value)}>
                    <option value="">—</option>
                    {(f.options ?? []).map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={f.kind === "date" ? "date" : "text"}
                    inputMode={f.kind === "number" || f.kind === "int" ? "decimal" : undefined}
                    value={val}
                    readOnly={ro}
                    maxLength={300}
                    onChange={(e) => setField(k, e.target.value)}
                  />
                )}
                {f.hint ? <small className="mute">{f.hint}</small> : null}
                {err(`content.fields.${k}`) ? <span className="field-error">{err(`content.fields.${k}`)}</span> : null}
              </label>
            );
          })}
          {(sec.tables ?? []).map((k) => (
            <TableEditor key={k} t={spec.tables.find((x) => x.key === k)!} rows={c.tables[k] ?? []} edit={edit} err={err} onChange={(rows) => setRows(k, rows)} />
          ))}
        </fieldset>
      ))}
      {act.view}
      {errors.file ? <span className="field-error">{errors.file}</span> : null}
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        {edit ? (
          <button className="btn btn-primary" disabled={act.busy} onClick={save}>
            Lưu
          </button>
        ) : null}
        {doc.actions
          .filter((a) => a !== "sign")
          .map((a) => (
            <button key={a} className="btn" disabled={act.busy} onClick={() => (NEEDS_REASON.includes(a) ? setReasonFor(a) : run(a))}>
              {ACTION_LABEL[a]}
            </button>
          ))}
        {doc.actions.includes("sign") ? (
          <span>
            <input type="file" aria-label="Bản scan đã ký" accept=".pdf,.jpg,.jpeg,.png" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            <button className="btn" disabled={act.busy} onClick={sign}>
              {ACTION_LABEL.sign}
            </button>
          </span>
        ) : null}
        <button className="btn" disabled={act.busy} onClick={word}>
          Xuất Word
        </button>
        {doc.signed ? (
          <button className="btn" disabled={act.busy} onClick={async () => saveBlob(await kapi.scan(doc.ref), doc.signed!.name)}>
            Tải bản đã ký
          </button>
        ) : null}
      </div>
      {doc.form === "03" ? <PhotoSection kapi={kapi} docRef={doc.ref} version={doc.version} /> : null}
      {reasonFor ? (
        <ReasonBox label={ACTION_LABEL[reasonFor]} busy={act.busy} onConfirm={(r) => run(reasonFor, r)} onCancel={() => setReasonFor(null)} />
      ) : null}
      <details>
        <summary>Lịch sử phiên bản, lần xuất Word</summary>
        <ul>
          {doc.versions.map((x) => (
            <li key={x.version}>
              {new Date(x.at).toLocaleString("vi-VN")} — {STATUS_LABEL[x.status as Doc["status"]] ?? x.status} (bản {x.revision})
              {x.actor_display ? ` · ${x.actor_display}` : ""}
              {x.status_reason ? ` · ${x.status_reason}` : ""}
            </li>
          ))}
        </ul>
        <ul>
          {(words.data ?? []).map((w) => (
            <li key={w.id}>
              <button className="link" onClick={async () => saveBlob(await kapi.download(doc.ref, w.id), w.file_name)}>
                {w.file_name}
              </button>
              {w.doc_number ? ` · số ${w.doc_number}` : ""} · {new Date(w.at).toLocaleString("vi-VN")}
            </li>
          ))}
        </ul>
      </details>
    </section>
  );
}

function TableEditor({ t, rows, edit, err, onChange }: { t: SpecTable; rows: Row[]; edit: boolean; err: (k: string) => string | undefined; onChange: (rows: Row[]) => void }) {
  const cols = t.columns.filter((c) => c.key !== "ma");
  const ro = !edit || !!t.auto;
  return (
    <div style={{ overflowX: "auto", marginBottom: 12 }}>
      <table className="table" aria-label={t.label}>
        <caption>
          {t.label}
          {t.hint ? <span className="mute"> — {t.hint}</span> : null}
        </caption>
        <thead>
          <tr>
            <th>TT</th>
            {cols.map((c) => (
              <th key={c.key}>{c.label}</th>
            ))}
            {!ro ? <th /> : null}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={cols.length + 2} className="mute">
                (không có)
              </td>
            </tr>
          ) : null}
          {rows.map((r, i) => (
            <tr key={i}>
              <td>{i + 1}</td>
              {cols.map((c) => {
                const k = `content.tables.${t.key}.${i}.${c.key}`;
                const v = r[c.key] ?? "";
                const cellRo = ro || !!c.auto;
                return (
                  <td key={c.key}>
                    {c.kind === "choice" ? (
                      <select aria-label={`${c.label} dòng ${i + 1}`} value={v} disabled={cellRo} onChange={(e) => onChange(rows.map((x, j) => (j === i ? { ...x, [c.key]: e.target.value } : x)))}>
                        <option value="">—</option>
                        {(c.options ?? []).map((o) => (
                          <option key={o.value} value={o.value}>
                            {o.label}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        aria-label={`${c.label} dòng ${i + 1}`}
                        value={v}
                        readOnly={cellRo}
                        maxLength={300}
                        inputMode={c.kind === "number" || c.kind === "int" ? "decimal" : undefined}
                        onChange={(e) => onChange(rows.map((x, j) => (j === i ? { ...x, [c.key]: e.target.value } : x)))}
                      />
                    )}
                    {err(k) ? <span className="field-error">{err(k)}</span> : null}
                  </td>
                );
              })}
              {!ro ? (
                <td>
                  <button className="link" aria-label={`Xóa dòng ${i + 1}`} onClick={() => onChange(rows.filter((_, j) => j !== i))}>
                    Xóa
                  </button>
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
      {!ro && rows.length < t.max_rows ? (
        <button className="btn btn-sm" onClick={() => onChange([...rows, emptyRow(t)])}>
          Thêm dòng
        </button>
      ) : null}
    </div>
  );
}

// W11.53b (UR-TINH-21 "kiểm tra thực địa (kèm ảnh)"): ảnh thực địa của báo cáo Mẫu 03 — người sửa được biểu thêm/gỡ khi
// đang lập; người xem được biểu xem ảnh. Máy chủ quyết quyền (can_edit) và kiểm lại kiểu, cỡ, số ảnh.
function PhotoSection({ kapi, docRef, version }: { kapi: KnqgApi; docRef: string; version: number }) {
  const photos = useLoad(() => kapi.photos(docRef), [kapi, docRef, version]);
  const act = useAction();
  const [file, setFile] = useState<File | null>(null);
  const [caption, setCaption] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<{ ref: string; url: string } | null>(null);
  useEffect(() => () => (preview ? URL.revokeObjectURL(preview.url) : undefined), [preview]);
  const done = (l: PhotoList | undefined) => {
    if (!l) return;
    photos.reload();
  };
  const add = async () => {
    const e = photoError(file, caption);
    setError(e);
    if (e || !file) return;
    const l = await act.run(() => kapi.addPhoto(docRef, file, caption), "Đã thêm ảnh thực địa.");
    if (l) {
      setFile(null);
      setCaption("");
    }
    done(l ?? undefined);
  };
  const show = async (ref: string) => {
    const blob = await act.run(() => kapi.photo(docRef, ref));
    if (blob) setPreview({ ref, url: URL.createObjectURL(blob) });
  };
  return (
    <fieldset className="card-section" aria-label="Ảnh thực địa">
      <legend>Ảnh thực địa (kèm báo cáo kiểm tra)</legend>
      <LoadState s={photos}>
        {(l) => (
          <>
            {l.items.length === 0 ? <p className="mute">Chưa có ảnh.</p> : null}
            <ul>
              {l.items.map((p) => (
                <li key={p.ref}>
                  <button className="link" disabled={act.busy} onClick={() => show(p.ref)}>
                    {p.name}
                  </button>
                  {p.caption ? ` — ${p.caption}` : ""} · {Math.max(1, Math.round(p.size / 1024))} KB · {new Date(p.uploaded_at).toLocaleString("vi-VN")}
                  {p.uploaded_by_name ? ` · ${p.uploaded_by_name}` : ""}
                  {l.can_edit ? (
                    <button className="btn btn-sm" disabled={act.busy} onClick={async () => done((await act.run(() => kapi.removePhoto(docRef, p.ref), "Đã gỡ ảnh.")) ?? undefined)}>
                      Gỡ
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
            {preview ? (
              <figure>
                {/* eslint-disable-next-line @next/next/no-img-element -- ảnh blob đã tải có xác thực, next/image không áp dụng */}
                <img src={preview.url} alt={l.items.find((p) => p.ref === preview.ref)?.caption ?? "Ảnh thực địa"} style={{ maxWidth: "100%", maxHeight: 360 }} />
                <figcaption>
                  <button className="link" onClick={() => setPreview(null)}>
                    Đóng ảnh
                  </button>
                </figcaption>
              </figure>
            ) : null}
            {l.can_edit && l.items.length < l.max_photos ? (
              <div className="row" style={{ gap: 8, flexWrap: "wrap", alignItems: "end" }}>
                <input type="file" aria-label="Chọn ảnh thực địa" accept=".jpg,.jpeg,.png" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
                <label>
                  Chú thích{" "}
                  <input type="text" value={caption} maxLength={300} onChange={(e) => setCaption(e.target.value)} />
                </label>
                <button className="btn" disabled={act.busy} onClick={add}>
                  Thêm ảnh
                </button>
              </div>
            ) : null}
            {l.can_edit ? <small className="mute">JPG, PNG ≤ 5 MB; tối đa {l.max_photos} ảnh. Đã trình ký thì không thêm/gỡ được.</small> : null}
          </>
        )}
      </LoadState>
      {error ? <span className="field-error">{error}</span> : null}
      {act.view}
    </fieldset>
  );
}
