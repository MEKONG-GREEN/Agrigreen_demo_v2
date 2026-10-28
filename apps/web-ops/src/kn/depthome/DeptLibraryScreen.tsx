"use client";

// Kho văn bản phòng (W11.54 — BRD CNTS v3.7 FN-20 UR-TINH-25): cây thư mục bên trái, danh sách văn bản lọc/tìm (loại, hiệu
// lực, từ khóa) bên phải; thêm văn bản (PDF, DOCX, XLSX ≤ 20 MB), sửa thuộc tính, tải tệp mới (giữ phiên bản cũ), xem/tải,
// ẩn ("xóa") của Lãnh đạo phòng. Văn bản hết hiệu lực vẫn tìm được, có nhãn "Hết hiệu lực". Cấp Xã chỉ xem văn bản dành cho xã.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { FilterBar, PageHeader, SearchBox, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  DEPT_NAME,
  KIND_LABEL,
  VALIDITY_LABEL,
  VALIDITY_TONE,
  createDeptHomeApi,
  docFormErrors,
  docFormOf,
  docPayload,
  emptyDocForm,
  fileError,
  fmtBytes,
  type DeptDoc,
  type DeptDocFolder,
  type DeptHomeApi,
  type DocForm,
  type DocQuery,
} from "./api";

type Mode = { kind: "new" } | { kind: "edit"; d: DeptDoc } | { kind: "file"; d: DeptDoc } | null;

export function DeptLibraryScreen({ department }: { department?: string }) {
  const { api, kn } = useAuth();
  const hapi = useMemo(() => (api ? createDeptHomeApi(api, department) : null), [api, department]);
  const [folder, setFolder] = useState("");
  const [q, setQ] = useState("");
  const [kind, setKind] = useState("");
  const [validity, setValidity] = useState<DocQuery["validity"] | "">("");
  const [showHidden, setShowHidden] = useState(false);
  const folders = useLoad(hapi ? () => hapi.folders(showHidden) : null, [hapi, showHidden]);
  const list = useLoad(
    hapi ? () => hapi.list({ folder: folder || undefined, q: q || undefined, kind: kind || undefined, validity: validity || undefined, include_hidden: showHidden ? "1" : undefined }) : null,
    [hapi, folder, q, kind, validity, showHidden],
  );
  const [mode, setMode] = useState<Mode>(null);
  const [open, setOpen] = useState<string | null>(null);
  const act = useAction();
  if (!hapi) return null;
  const dept = department ?? kn.context?.department_code ?? "";
  const canWrite = !!list.data?.can_write;
  const canManage = !!list.data?.can_manage;
  const reload = () => {
    folders.reload();
    list.reload();
  };
  const newFolder = async () => {
    const name = window.prompt("Tên thư mục mới");
    if (name?.trim() && (await act.run(() => hapi.createFolder(name.trim(), folder || null), "Đã tạo thư mục."))) reload();
  };
  return (
    <div className="page">
      <PageHeader
        title={`Quy trình, văn bản — Phòng ${DEPT_NAME[dept] ?? ""}`}
        actions={canWrite ? (
          <div className="row">
            <button className="btn btn-primary" onClick={() => setMode({ kind: "new" })}>Thêm văn bản</button>
            <button className="btn" onClick={() => void newFolder()}>{folder ? "Thêm thư mục con" : "Thêm thư mục"}</button>
          </div>
        ) : null}
      >
        <p className="mute">Nghị định, chính sách, kế hoạch, quy trình kỹ thuật, mẫu biểu của phòng (FN-20). Văn bản hết hiệu lực vẫn lưu và có nhãn.</p>
      </PageHeader>
      {act.view}
      {mode ? <DocEditor key={mode.kind + (mode.kind === "new" ? "" : mode.d.ref)} mode={mode} hapi={hapi} folders={folders.data?.items ?? []} folder={folder}
        onDone={(ref) => { setMode(null); reload(); if (ref) setOpen(ref); }} /> : null}
      <div style={{ display: "grid", gridTemplateColumns: "minmax(200px, 260px) minmax(0, 1fr)", gap: 16 }}>
        <section className="card sec" aria-label="Thư mục">
          <h2>Thư mục</h2>
          <LoadState s={folders}>
            {(fs) => (
              <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
                <li><button className={`btn btn-sm ${folder === "" ? "btn-primary" : "btn-o"}`} onClick={() => setFolder("")}>Tất cả văn bản</button></li>
                {fs.items.map((f) => (
                  <FolderRow key={f.ref} f={f} active={folder === f.ref} onPick={() => setFolder(f.ref)} hapi={hapi} canWrite={canWrite} canManage={canManage}
                    run={act.run} reload={reload} />
                ))}
              </ul>
            )}
          </LoadState>
          {canManage ? (
            <label className="mute" style={{ display: "block", marginTop: 8 }}>
              <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} /> Hiện mục đã ẩn
            </label>
          ) : null}
        </section>
        <section className="card sec" aria-label="Văn bản">
          <FilterBar onReset={() => { setQ(""); setKind(""); setValidity(""); }}>
            <SearchBox value={q} onChange={setQ} placeholder="Tìm tên, số hiệu, cơ quan ban hành" />
            <select aria-label="Loại văn bản" value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="">Mọi loại</option>
              {Object.entries(KIND_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
            <select aria-label="Hiệu lực" value={validity} onChange={(e) => setValidity(e.target.value as DocQuery["validity"] | "")}>
              <option value="">Mọi hiệu lực</option>
              {Object.entries(VALIDITY_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          </FilterBar>
          <LoadState s={list}>
            {(l) => l.items.length ? (
              <table className="dt" aria-label="Danh sách văn bản">
                <thead><tr><th>Tên văn bản</th><th>Loại / lĩnh vực</th><th>Ban hành</th><th>Hiệu lực</th><th>Tệp</th><th /></tr></thead>
                <tbody>
                  {l.items.map((d) => (
                    <DocRow key={d.ref} d={d} open={open === d.ref} onToggle={() => setOpen(open === d.ref ? null : d.ref)} hapi={hapi} run={act.run}
                      onEdit={() => setMode({ kind: "edit", d })} onFile={() => setMode({ kind: "file", d })} reload={reload} />
                  ))}
                </tbody>
              </table>
            ) : <p className="mute">Không có văn bản phù hợp.</p>}
          </LoadState>
        </section>
      </div>
    </div>
  );
}

type Run = ReturnType<typeof useAction>["run"];

function FolderRow({ f, active, onPick, hapi, canWrite, canManage, run, reload }: {
  f: DeptDocFolder; active: boolean; onPick: () => void; hapi: DeptHomeApi; canWrite: boolean; canManage: boolean; run: Run; reload: () => void;
}) {
  const rename = async () => {
    const n = window.prompt("Tên mới", f.name);
    if (n?.trim() && (await run(() => hapi.renameFolder(f.ref, n.trim(), f.version), "Đã đổi tên thư mục."))) reload();
  };
  const toggle = async () => {
    const r = window.prompt(f.hidden ? "Lý do hiện lại" : "Lý do ẩn thư mục (thư mục phải rỗng)");
    if (r && r.trim().length >= 5 && (await run(() => hapi.hideFolder(f.ref, !f.hidden, r.trim(), f.version), f.hidden ? "Đã hiện lại." : "Đã ẩn thư mục."))) reload();
  };
  return (
    <li style={{ paddingLeft: (f.path.length - 1) * 14, marginTop: 4 }}>
      <button className={`btn btn-sm ${active ? "btn-primary" : "btn-o"}`} onClick={onPick}>{f.name} ({f.docs})</button>
      {f.hidden ? <StatusPill tone="neutral">Đã ẩn</StatusPill> : null}
      {active && canWrite && !f.hidden ? <button className="btn btn-sm" onClick={() => void rename()}>Đổi tên</button> : null}
      {active && canManage ? <button className="btn btn-sm" onClick={() => void toggle()}>{f.hidden ? "Hiện lại" : "Ẩn"}</button> : null}
    </li>
  );
}

function DocRow({ d, open, onToggle, hapi, run, onEdit, onFile, reload }: {
  d: DeptDoc; open: boolean; onToggle: () => void; hapi: DeptHomeApi; run: Run; onEdit: () => void; onFile: () => void; reload: () => void;
}) {
  const detail = useLoad(open ? () => hapi.get(d.ref) : null, [open, d.ref, d.version]);
  const fetchFile = (download: boolean, version?: number) =>
    void run(() => hapi.content(d.ref, download, version), (r) => {
      if (download) {
        saveBlob(r.blob, r.name);
        return "Đã tải xuống.";
      }
      window.open(URL.createObjectURL(r.blob), "_blank", "noopener");
      return "Đã mở tệp.";
    });
  const hide = async () => {
    const r = window.prompt(d.hidden ? "Lý do hiện lại" : "Lý do ẩn (xóa) văn bản");
    if (r && r.trim().length >= 5 && (await run(() => hapi.hide(d.ref, !d.hidden, r.trim(), d.version), d.hidden ? "Đã hiện lại văn bản." : "Đã ẩn văn bản."))) reload();
  };
  return (
    <>
      <tr data-testid={`dept-doc-${d.ref}`}>
        <td>
          <button className="btn-link" onClick={onToggle}>{d.title}</button>
          {d.number ? <div className="mute">{d.number}{d.issuer ? ` · ${d.issuer}` : ""}</div> : null}
          {d.folder_path.length ? <div className="mute">{d.folder_path.join(" / ")}</div> : null}
        </td>
        <td>{KIND_LABEL[d.kind]}{d.field ? <div className="mute">{d.field}</div> : null}</td>
        <td>{d.issued_on ? d.issued_on.split("-").reverse().join("/") : "—"}</td>
        <td>
          <StatusPill tone={VALIDITY_TONE[d.validity]}>{VALIDITY_LABEL[d.validity]}</StatusPill>
          {d.hidden ? <StatusPill tone="neutral">Đã ẩn</StatusPill> : null}
          {d.audience.includes("xa") ? <div className="mute">Cấp Xã xem</div> : null}
        </td>
        <td>{d.file.name}<div className="mute">{fmtBytes(d.file.size_bytes)} · bản tệp {d.file.version}</div></td>
        <td>
          <div className="row">
            <button className="btn btn-sm" onClick={() => fetchFile(false)}>Xem</button>
            <button className="btn btn-sm" onClick={() => fetchFile(true)}>Tải</button>
            {d.can_edit && !d.hidden ? <button className="btn btn-sm" onClick={onEdit}>Sửa</button> : null}
            {d.can_edit && !d.hidden ? <button className="btn btn-sm" onClick={onFile}>Tệp mới</button> : null}
            {d.can_manage ? <button className="btn btn-sm" onClick={() => void hide()}>{d.hidden ? "Hiện lại" : "Ẩn"}</button> : null}
          </div>
        </td>
      </tr>
      {open ? (
        <tr>
          <td colSpan={6}>
            <LoadState s={detail}>
              {(x) => (
                <div>
                  {x.summary ? <p style={{ whiteSpace: "pre-line" }}>{x.summary}</p> : null}
                  <p className="mute">
                    Hiệu lực: {x.effective_from ?? "—"} → {x.effective_to ?? "không thời hạn"}{x.status_reason ? ` · Lý do: ${x.status_reason}` : ""}
                  </p>
                  <table className="dt" aria-label="Phiên bản">
                    <thead><tr><th>Bản</th><th>Thao tác</th><th>Tệp</th><th>Lúc</th><th /></tr></thead>
                    <tbody>
                      {(x.versions ?? []).map((v) => (
                        <tr key={v.version}>
                          <td>{v.version}</td>
                          <td>{({ created: "Tạo", updated: "Sửa thuộc tính", file_replaced: "Tải tệp mới", hidden: "Ẩn", restored: "Hiện lại" } as const)[v.event]}</td>
                          <td>{v.file.name} (bản tệp {v.file.version})</td>
                          <td>{dt(v.recorded_at)}</td>
                          <td><button className="btn btn-sm" onClick={() => fetchFile(true, v.version)}>Tải bản này</button></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </LoadState>
          </td>
        </tr>
      ) : null}
    </>
  );
}

function DocEditor({ mode, hapi, folders, folder, onDone }: {
  mode: Exclude<Mode, null>; hapi: DeptHomeApi; folders: DeptDocFolder[]; folder: string; onDone: (ref: string | null) => void;
}) {
  const [f, setF] = useState<DocForm>(mode.kind === "new" ? emptyDocForm(folder) : docFormOf(mode.d));
  const [file, setFile] = useState<File | null>(null);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const act = useAction();
  const set = <K extends keyof DocForm>(k: K, v: DocForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const submit = async () => {
    const e = mode.kind === "file" ? {} : docFormErrors(f);
    if (mode.kind !== "edit") {
      const fe = fileError(file);
      if (fe) (e as Record<string, string>).file = fe;
    }
    setErrs(e);
    if (Object.keys(e).length) return;
    const r = await act.run(() =>
      mode.kind === "new" ? hapi.create(docPayload(f), file!) : mode.kind === "edit" ? hapi.update(mode.d.ref, docPayload(f, mode.d.version)) : hapi.replaceFile(mode.d.ref, file!, mode.d.version),
    );
    if (r) onDone(r.ref);
  };
  const title = mode.kind === "new" ? "Thêm văn bản" : mode.kind === "edit" ? "Sửa thuộc tính" : "Tải tệp mới";
  const err = (k: string) => (errs[k] ? <span className="err">{errs[k]}</span> : null);
  return (
    <section className="card sec" aria-label={title}>
      <h2>{title}{mode.kind !== "new" ? ` — ${mode.d.title}` : ""}</h2>
      {act.view}
      {mode.kind !== "file" ? (
        <div className="form grid2" style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0,1fr))", gap: 12 }}>
          <label className="field">Tên văn bản *<input value={f.title} onChange={(e) => set("title", e.target.value)} maxLength={300} />{err("title")}</label>
          <label className="field">Loại *
            <select value={f.kind} onChange={(e) => set("kind", e.target.value as DocForm["kind"])}>
              <option value="">— Chọn —</option>
              {Object.entries(KIND_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>{err("kind")}
          </label>
          <label className="field">Thư mục
            <select value={f.folder_ref} onChange={(e) => set("folder_ref", e.target.value)}>
              <option value="">(Gốc kho)</option>
              {folders.filter((x) => !x.hidden).map((x) => <option key={x.ref} value={x.ref}>{x.path.join(" / ")}</option>)}
            </select>
          </label>
          <label className="field">Lĩnh vực<input value={f.field} onChange={(e) => set("field", e.target.value)} placeholder="Gia súc, gia cầm, thủy sản…" maxLength={100} /></label>
          <label className="field">Số hiệu<input value={f.number} onChange={(e) => set("number", e.target.value)} maxLength={100} /></label>
          <label className="field">Cơ quan ban hành<input value={f.issuer} onChange={(e) => set("issuer", e.target.value)} maxLength={200} /></label>
          <label className="field">Ngày ban hành<input type="date" value={f.issued_on} onChange={(e) => set("issued_on", e.target.value)} /></label>
          <label className="field">Có hiệu lực từ<input type="date" value={f.effective_from} onChange={(e) => set("effective_from", e.target.value)} /></label>
          <label className="field">Hết hiệu lực<input type="date" value={f.effective_to} onChange={(e) => set("effective_to", e.target.value)} />{err("effective_to")}</label>
          <fieldset className="field">
            <legend>Đối tượng xem (ngoài Cấp Tỉnh)</legend>
            <label><input type="checkbox" checked={f.xa} onChange={(e) => set("xa", e.target.checked)} /> Cấp Xã</label>{" "}
            <label><input type="checkbox" checked={f.nong_ho} onChange={(e) => set("nong_ho", e.target.checked)} /> Nông hộ (Mobile, giai đoạn 2)</label>
          </fieldset>
          <label className="field" style={{ gridColumn: "1 / -1" }}>Trích yếu<textarea rows={3} value={f.summary} onChange={(e) => set("summary", e.target.value)} maxLength={2000} /></label>
        </div>
      ) : null}
      {mode.kind !== "edit" ? (
        <label className="field">Tệp (PDF, DOCX, XLSX ≤ 20 MB) *
          <input type="file" accept=".pdf,.docx,.xlsx" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />{err("file")}
        </label>
      ) : null}
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void submit()}>Lưu</button>
        <button className="btn" onClick={() => onDone(null)}>Hủy</button>
      </div>
    </section>
  );
}
