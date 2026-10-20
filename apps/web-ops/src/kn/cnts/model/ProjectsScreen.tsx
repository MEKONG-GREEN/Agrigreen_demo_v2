"use client";

// Chương trình, dự án, điểm trình diễn (W11.47 — BRD CNTS v3.7 UR-TINH-19, FN-19 phần CT/DA; bố cục prototype A
// "r-projects"): danh sách dự án, tạo/sửa (chủ nhiệm sửa dự án mình — FN-19 BR-01), chi tiết dự án với điểm trình diễn và
// hộ tham gia, kết thúc/mở lại. Mẫu biểu dự án BM-12…BM-16 là gói W11.53 (GĐ2).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  KIND_LABEL,
  createCntsModelApi,
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
          <table className="table">
            <thead>
              <tr>
                <th>Mã dự án</th>
                <th>Tên dự án</th>
                <th>Loại</th>
                <th>Thời gian</th>
                <th>Kinh phí</th>
                <th>Chủ nhiệm</th>
                <th>Điểm / hộ</th>
                <th>Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {d.items.length ? (
                d.items.map((p) => (
                  <tr key={p.ref} className={open === p.ref ? "selected" : undefined}>
                    <td>
                      <button className="link" onClick={() => setOpen(open === p.ref ? null : p.ref)}>
                        {p.code}
                      </button>
                    </td>
                    <td>{p.name}</td>
                    <td>{KIND_LABEL[p.kind]}</td>
                    <td>
                      {dmy(p.start_date)} – {dmy(p.end_date)}
                    </td>
                    <td>{money(p.budget)}</td>
                    <td>{p.manager_display ?? "—"}</td>
                    <td>
                      {p.site_count} / {p.household_count}
                    </td>
                    <td>{p.status === "closed" ? <StatusPill tone="neutral">Đã kết thúc</StatusPill> : <StatusPill tone="ok">Đang thực hiện</StatusPill>}</td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={8} className="empty">
                    Chưa có dự án.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
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
  const act = useAction();
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const body: ProjectSave = {
    ...(project ? { base_version: project.version, manager_ref: project.manager_ref } : {}),
    code: f.code.trim(),
    name: f.name.trim(),
    kind: f.kind as ProjectSave["kind"],
    basis: f.basis.trim() || null,
    scale_note: f.scale_note.trim() || null,
    start_date: f.start_date,
    end_date: f.end_date || null,
    budget: f.budget.trim() || null,
    program_ref: f.program_ref || null,
    training_note: f.training_note.trim() || null,
    supplies_note: f.supplies_note.trim() || null,
    attachment_refs: project?.attachment_refs ?? [],
  };
  const errs = projectErrors(body);
  return (
    <section className="card sec" aria-label={project ? "Sửa dự án" : "Tạo dự án"}>
      <h2>{project ? `Sửa dự án ${project.code}` : "Tạo chương trình, dự án"}</h2>
      {act.view}
      <div className="form-grid">
        <label>
          Mã dự án
          <input value={f.code} onChange={set("code")} maxLength={40} />
        </label>
        <label>
          Tên dự án
          <input value={f.name} onChange={set("name")} maxLength={300} />
        </label>
        <label>
          Loại
          <select value={f.kind} onChange={set("kind")}>
            {Object.entries(KIND_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label>
          CT/DA của KHTC
          <select value={f.program_ref} onChange={set("program_ref")}>
            <option value="">— Không gắn —</option>
            {(programs.data ?? []).map((p) => (
              <option key={p.ref} value={p.ref}>
                {p.code} · {p.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Căn cứ
          <input value={f.basis} onChange={set("basis")} maxLength={1000} />
        </label>
        <label>
          Quy mô
          <input value={f.scale_note} onChange={set("scale_note")} maxLength={1000} />
        </label>
        <label>
          Bắt đầu
          <input type="date" value={f.start_date} onChange={set("start_date")} />
        </label>
        <label>
          Kết thúc
          <input type="date" value={f.end_date} onChange={set("end_date")} />
        </label>
        <label>
          Kinh phí (đồng)
          <input inputMode="numeric" value={f.budget} onChange={set("budget")} />
        </label>
        <label>
          Tập huấn
          <input value={f.training_note} onChange={set("training_note")} maxLength={2000} />
        </label>
        <label>
          Giống, vật tư
          <input value={f.supplies_note} onChange={set("supplies_note")} maxLength={2000} />
        </label>
      </div>
      {Object.keys(errs).length ? <p className="mute">{Object.values(errs).join(" · ")}</p> : null}
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || Object.keys(errs).length > 0}
          onClick={async () => {
            const r = await act.run(() => (project ? mapi.updateProject(project.ref, body) : mapi.createProject(body)));
            if (r) onDone(r.ref);
          }}
        >
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
      <table className="table">
        <thead>
          <tr>
            <th>Điểm</th>
            <th>Xã</th>
            <th>Loại mô hình</th>
            <th>Quy mô</th>
            <th>Cán bộ theo dõi</th>
            <th>Số hộ</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {p.sites.length ? (
            p.sites.map((x) => (
              <tr key={x.ref}>
                <td>
                  <button className="link" onClick={() => setSite(site === x.ref ? null : x.ref)}>
                    {x.name}
                  </button>{" "}
                  {x.status === "closed" ? <StatusPill tone="neutral">Đã đóng</StatusPill> : null}
                </td>
                <td>{communeName(x.commune_code)}</td>
                <td>{x.model_type_name ?? x.model_type_code}</td>
                <td>{x.scale ? `${x.scale} ${x.scale_unit ?? ""}` : "—"}</td>
                <td>{x.officer_display ?? "—"}</td>
                <td>{x.household_count}</td>
                <td>
                  {x.can_edit && x.status === "active" ? (
                    <button className="btn btn-sm" onClick={() => setEditSite(x.ref)}>
                      Sửa
                    </button>
                  ) : null}
                </td>
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan={7} className="empty">
                Chưa có điểm trình diễn.
              </td>
            </tr>
          )}
        </tbody>
      </table>
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
