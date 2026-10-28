"use client";

// Chương trình, dự án, điểm trình diễn (W11.47 — BRD CNTS v3.7 UR-TINH-19, FN-19 phần CT/DA; bố cục prototype A
// "r-projects"): danh sách dự án, tạo/sửa (chủ nhiệm sửa dự án mình — FN-19 BR-01), chi tiết dự án với điểm trình diễn và
// hộ tham gia, kết thúc/mở lại. Mẫu biểu dự án BM-12…BM-16 (Mẫu 01/03/04/05/07): màn /kn/cnts/du-an-mau-bieu (W11.53).
// BF-SWEEP-CNTS (#101): kinh phí dùng NumberField (số nguyên, không âm), trường bắt buộc báo lỗi theo trường khi bấm Lưu,
// bảng có STT.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { fieldErrorMap } from "@/api/problem";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  BUDGET_RULE,
  KIND_LABEL,
  createCntsModelApi,
  decimalValue,
  dmy,
  money,
  projectErrors,
  type Candidate,
  type CntsModelApi,
  type ProjectDetail,
  type ProjectSave,
} from "./api";
import { ReasonBox, SiteForm, SitePanel } from "./parts";

export function ProjectsScreen() {
  const { api, kn } = useAuth();
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const role = kn.context?.role;
  const [status, setStatus] = useState<"" | "active" | "closed">("active");
  const list = useLoad(mapi ? () => mapi.projects(status ? { status } : {}) : null, [mapi, status]);
  const officers = useLoad(mapi && (role === "kn_tinh" || role === "admin") ? () => mapi.candidates() : null, [mapi, role]);
  const [open, setOpen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  if (!mapi) return null;
  return (
    <div className="page">
      <PageHeader
        title="Chương trình, dự án"
        actions={
          list.data?.can_create && !creating ? (
            <button className="btn btn-primary" onClick={() => setCreating(true)}>
              Tạo dự án
            </button>
          ) : null
        }
      >
        <p className="mute">Chủ nhiệm quản lý dự án, điểm trình diễn, hộ tham gia thay cho bảng tính tiến độ; điểm, hộ mới tự hiện ở lịch tiêm, nhật ký của xã.</p>
      </PageHeader>
      {creating ? (
        <ProjectForm
          mapi={mapi}
          onDone={(ref) => {
            setCreating(false);
            if (ref) {
              list.reload();
              setOpen(ref);
            }
          }}
        />
      ) : null}
      <div className="row">
        <label>
          Trạng thái{" "}
          <select value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
            <option value="">Tất cả</option>
            <option value="active">Đang thực hiện</option>
            <option value="closed">Đã kết thúc</option>
          </select>
        </label>
      </div>
      <LoadState s={list}>
        {(d) => (
          <DataTable
            caption="Chương trình, dự án"
            rows={d.items}
            rowKey={(p) => p.ref}
            empty="Chưa có dự án."
            columns={[
              {
                key: "code",
                header: "Mã dự án",
                cell: (p) => (
                  <button className="link" aria-expanded={open === p.ref} onClick={() => setOpen(open === p.ref ? null : p.ref)}>
                    {p.code}
                  </button>
                ),
              },
              { key: "name", header: "Tên dự án", cell: (p) => p.name },
              { key: "kind", header: "Loại", cell: (p) => KIND_LABEL[p.kind] },
              { key: "time", header: "Thời gian", cell: (p) => `${dmy(p.start_date)} – ${dmy(p.end_date)}` },
              { key: "budget", header: "Kinh phí", cell: (p) => money(p.budget) },
              { key: "mgr", header: "Chủ nhiệm", cell: (p) => p.manager_display ?? "—" },
              { key: "sites", header: "Điểm / hộ", cell: (p) => `${p.site_count} / ${p.household_count}` },
              { key: "st", header: "Trạng thái", cell: (p) => (p.status === "closed" ? <StatusPill tone="neutral">Đã kết thúc</StatusPill> : <StatusPill tone="ok">Đang thực hiện</StatusPill>) },
            ]}
          />
        )}
      </LoadState>
      {open ? <ProjectPanel key={open} mapi={mapi} projectRef={open} officers={officers.data ?? []} onChanged={list.reload} /> : null}
    </div>
  );
}

function ProjectForm({ mapi, project, onDone }: { mapi: CntsModelApi; project?: ProjectDetail; onDone: (ref?: string) => void }) {
  const programs = useLoad(() => mapi.programs(), [mapi]);
  const [f, setF] = useState({
    code: project?.code ?? "",
    name: project?.name ?? "",
    kind: project?.kind ?? "knqg",
    basis: project?.basis ?? "",
    scale_note: project?.scale_note ?? "",
    start_date: project?.start_date ?? "",
    end_date: project?.end_date ?? "",
    budget: project?.budget ?? "",
    program_ref: project?.program_ref ?? "",
    training_note: project?.training_note ?? "",
    supplies_note: project?.supplies_note ?? "",
  });
  const [errs, setErrs] = useState<Record<string, string | undefined>>({});
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const err = (k: string) => errs[k] ?? server[k];
  const set = (k: keyof typeof f) => (v: string) => setF((x) => ({ ...x, [k]: v }));
  const body: ProjectSave = {
    ...(project ? { base_version: project.version, manager_ref: project.manager_ref } : {}),
    code: f.code.trim(),
    name: f.name.trim(),
    kind: f.kind as ProjectSave["kind"],
    basis: f.basis.trim() || null,
    scale_note: f.scale_note.trim() || null,
    start_date: f.start_date,
    end_date: f.end_date || null,
    budget: decimalValue(f.budget, BUDGET_RULE),
    program_ref: f.program_ref || null,
    training_note: f.training_note.trim() || null,
    supplies_note: f.supplies_note.trim() || null,
    attachment_refs: project?.attachment_refs ?? [],
  };
  const save = async () => {
    const e = projectErrors({ ...body, budget: f.budget.trim() || null });
    setErrs(e);
    if (Object.keys(e).length) return;
    const r = await act.run(() => (project ? mapi.updateProject(project.ref, body) : mapi.createProject(body)));
    if (r) onDone(r.ref);
  };
  return (
    <section className="card sec" aria-label={project ? "Sửa dự án" : "Tạo dự án"}>
      <h2>{project ? `Sửa dự án ${project.code}` : "Tạo chương trình, dự án"}</h2>
      {act.view}
      <div className="form-grid">
        <TextField label="Mã dự án" required value={f.code} onChange={set("code")} error={err("code")} maxLength={40} />
        <TextField label="Tên dự án" required value={f.name} onChange={set("name")} error={err("name")} maxLength={300} />
        <SelectField label="Loại" required value={f.kind} onChange={set("kind")} error={err("kind")} options={Object.entries(KIND_LABEL).map(([k, v]) => ({ value: k, label: v }))} />
        <SelectField
          label="CT/DA của KHTC"
          value={f.program_ref}
          onChange={set("program_ref")}
          placeholder="— Không gắn —"
          options={(programs.data ?? []).map((p) => ({ value: p.ref, label: `${p.code} · ${p.name}` }))}
        />
        <TextField label="Căn cứ" value={f.basis} onChange={set("basis")} maxLength={1000} />
        <TextField label="Quy mô" value={f.scale_note} onChange={set("scale_note")} maxLength={1000} />
        <DateInput label="Bắt đầu" required value={f.start_date} onChange={set("start_date")} error={err("start_date")} />
        <DateInput label="Kết thúc" value={f.end_date} onChange={set("end_date")} error={err("end_date")} />
        <NumberField label="Kinh phí" unit="đồng" rule={BUDGET_RULE} value={f.budget} onChange={set("budget")} error={err("budget")} onBlurError={(m) => setErrs((x) => ({ ...x, budget: m }))} />
        <TextField label="Tập huấn" value={f.training_note} onChange={set("training_note")} maxLength={2000} />
        <TextField label="Giống, vật tư" value={f.supplies_note} onChange={set("supplies_note")} maxLength={2000} />
      </div>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Lưu
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

function ProjectPanel({ mapi, projectRef, officers, onChanged }: { mapi: CntsModelApi; projectRef: string; officers: Candidate[]; onChanged: () => void }) {
  const s = useLoad(() => mapi.project(projectRef), [mapi, projectRef]);
  return <LoadState s={s}>{(p) => <ProjectBody mapi={mapi} p={p} officers={officers} reload={() => (s.reload(), onChanged())} />}</LoadState>;
}

function ProjectBody({ mapi, p, officers, reload }: { mapi: CntsModelApi; p: ProjectDetail; officers: Candidate[]; reload: () => void }) {
  const [mode, setMode] = useState<"edit" | "close" | "reopen" | "site" | null>(null);
  const [site, setSite] = useState<string | null>(null);
  const [editSite, setEditSite] = useState<string | null>(null);
  const act = useAction();
  const communeName = useCommuneNames(p.sites.map((x) => x.commune_code));
  const communes = [...new Set(officers.flatMap((o) => o.commune_codes))].sort();
  const editing = p.sites.find((x) => x.ref === editSite);
  return (
    <section className="card sec" aria-label="Chi tiết dự án">
      <h2>
        {p.code} · {p.name}
      </h2>
      <p className="mute">
        {KIND_LABEL[p.kind]} · {dmy(p.start_date)} – {dmy(p.end_date)} · Kinh phí {money(p.budget)} · Chủ nhiệm {p.manager_display ?? p.manager_ref}
        {p.program ? ` · CT/DA KHTC ${p.program.code}` : ""}
      </p>
      {p.basis ? <p>Căn cứ: {p.basis}</p> : null}
      {p.scale_note ? <p>Quy mô: {p.scale_note}</p> : null}
      {p.training_note ? <p>Tập huấn: {p.training_note}</p> : null}
      {p.supplies_note ? <p>Giống, vật tư: {p.supplies_note}</p> : null}
      {p.status_reason ? <p className="mute">Lý do trạng thái: {p.status_reason}</p> : null}
      {act.view}
      {p.can_edit || p.status === "closed" ? (
        <div className="row">
          {p.can_edit ? (
            <>
              <button className="btn" onClick={() => setMode("edit")}>
                Sửa dự án
              </button>
              <button className="btn" onClick={() => setMode("site")}>
                Thêm điểm trình diễn
              </button>
              <button className="btn" onClick={() => setMode("close")}>
                Kết thúc dự án
              </button>
            </>
          ) : null}
          {p.status === "closed" ? (
            <button className="btn" onClick={() => setMode("reopen")}>
              Mở lại
            </button>
          ) : null}
        </div>
      ) : null}
      {mode === "edit" ? <ProjectForm mapi={mapi} project={p} onDone={(ref) => (setMode(null), ref && reload())} /> : null}
      {mode === "close" || mode === "reopen" ? (
        <ReasonBox
          label={mode === "close" ? "Kết thúc dự án" : "Mở lại dự án"}
          busy={act.busy}
          onCancel={() => setMode(null)}
          onConfirm={async (reason) => {
            if (await act.run(() => mapi.projectStatus(p.ref, mode === "close", p.version, reason), "Đã cập nhật trạng thái dự án")) {
              setMode(null);
              reload();
            }
          }}
        />
      ) : null}
      {mode === "site" ? <SiteForm mapi={mapi} projectRef={p.ref} officers={officers} communes={communes} onDone={(ref) => (setMode(null), ref && (reload(), setSite(ref)))} /> : null}
      {editing ? <SiteForm key={editing.ref} mapi={mapi} projectRef={p.ref} site={editing} officers={officers} communes={communes} onDone={(ref) => (setEditSite(null), ref && reload())} /> : null}
      <h3>Điểm trình diễn</h3>
      <DataTable
        caption="Điểm trình diễn của dự án"
        rows={p.sites}
        rowKey={(x) => x.ref}
        empty="Chưa có điểm trình diễn."
        columns={[
          {
            key: "name",
            header: "Điểm",
            cell: (x) => (
              <>
                <button className="link" aria-expanded={site === x.ref} onClick={() => setSite(site === x.ref ? null : x.ref)}>
                  {x.name}
                </button>{" "}
                {x.status === "closed" ? <StatusPill tone="neutral">Đã đóng</StatusPill> : null}
              </>
            ),
          },
          { key: "commune", header: "Xã", cell: (x) => communeName(x.commune_code) },
          { key: "model", header: "Loại mô hình", cell: (x) => x.model_type_name ?? x.model_type_code },
          { key: "scale", header: "Quy mô", cell: (x) => (x.scale ? `${x.scale} ${x.scale_unit ?? ""}` : "—") },
          { key: "officer", header: "Cán bộ theo dõi", cell: (x) => x.officer_display ?? "—" },
          { key: "hh", header: "Số hộ", cell: (x) => x.household_count },
          {
            key: "act",
            header: "",
            cell: (x) =>
              x.can_edit && x.status === "active" ? (
                <button className="btn btn-sm" onClick={() => setEditSite(x.ref)}>
                  Sửa
                </button>
              ) : null,
          },
        ]}
      />
      {site ? <SitePanel key={site} mapi={mapi} siteRef={site} officers={officers} communeName={communeName} onChanged={reload} /> : null}
      <details>
        <summary>Lịch sử phiên bản ({p.versions.length})</summary>
        <ul>
          {p.versions.map((v) => (
            <li key={v.version}>
              v{v.version} · {v.event} · {new Date(v.updated_at).toLocaleString("vi-VN")} · {v.actor_display ?? v.updated_by}
              {v.status_reason ? ` · ${v.status_reason}` : ""}
            </li>
          ))}
        </ul>
      </details>
    </section>
  );
}

/** Ô ngày có nhãn, dấu * và lỗi theo trường như UI-KIT (ô `type="date"` gốc). */
function DateInput({ label, value, onChange, error, required }: { label: string; value: string; onChange: (v: string) => void; error?: string; required?: boolean }) {
  return (
    <div className="field">
      <label>
        {label}
        {required ? (
          <span className="req" aria-hidden="true">
            {" "}*
          </span>
        ) : null}
        <input className="input" type="date" aria-required={required || undefined} aria-invalid={error ? true : undefined} value={value} onChange={(e) => onChange(e.target.value)} />
      </label>
      {error ? (
        <span className="err" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
