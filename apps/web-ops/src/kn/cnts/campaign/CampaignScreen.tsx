"use client";

// Chiến dịch điều tra mở rộng (W11.50 — BRD CNTS v3.7 FN-21, UR-TINH-26; Hình 38a/38b) — màn Cấp Tỉnh Phòng CNTS:
// danh sách đợt; "+ Tạo đợt điều tra" (tên, mục tiêu, lĩnh vực, biểu, phạm vi, thời gian, hạn, nhắc) → Nháp; xem trước số việc
// → "Phát hành"; theo dõi tiến độ theo Khu vực/xã, nhắc xã chưa nộp, gia hạn (lý do), đóng đợt, mở lại (Lãnh đạo phòng),
// kết quả tổng hợp + xuất Excel. Cán bộ xã nhập biểu của đợt ở màn "Nhập biểu kỳ CNTS" (UR-XA-14).
// Số liệu đợt tách khỏi lũy kế báo cáo định kỳ (BR-02) — không vào Phụ lục 2/KSGM và số liệu cho KHTC.
// #236 (BRD CNTS v4.0 FN-21): người phụ trách đợt bắt buộc, chỉ cán bộ tổng hợp hoặc người phụ trách quản lý đợt; "Sao chép
// đợt" ra đợt Nháp mới (không mang số liệu); màn riêng "Tạo chiến dịch" (/kn/cnts/chien-dich/tao).
import { useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, num, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  EVENT_LABEL,
  FIELD_LABEL,
  STATE_LABEL,
  STATE_TONE,
  UNIT_STATUS_LABEL,
  copyErrors,
  copyPayload,
  createCampaignApi,
  emptyForm,
  formErrors,
  formOf,
  payload,
  percent,
  reasonOk,
  type Campaign,
  type CampaignApi,
  type CampaignForm,
  type CampaignOwner,
  type CopyForm,
  type FormsTemplate,
} from "./api";

const LIST_PATH = "/kn/cnts/chien-dich";

const ymd = (iso: string) => iso.slice(0, 10).split("-").reverse().join("/");

function problemFields(e: unknown): Record<string, string> {
  const m: Record<string, string> = {};
  if (e instanceof ApiProblem) for (const x of e.problem.errors ?? []) if (x.field) m[x.field] = x.reason ?? "";
  return m;
}

export function CampaignScreen() {
  const { api, kn } = useAuth();
  const capi = useMemo(() => (api ? createCampaignApi(api) : null), [api]);
  const role = kn.context?.role;
  const writer = role === "kn_tinh" && !kn.context?.read_only;
  const list = useLoad(capi ? () => capi.list() : null, [capi]);
  const templates = useLoad(capi && writer ? () => capi.templates() : null, [capi, writer]);
  const owners = useLoad(capi && writer ? () => capi.owners() : null, [capi, writer]);
  const [editing, setEditing] = useState<Campaign | "new" | null>(null);
  const [open, setOpen] = useState<string | null>(useSearchParams()?.get("dot") ?? null);

  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader
        title="Chiến dịch điều tra"
        actions={
          writer ? (
            <button className="btn btn-primary" onClick={() => setEditing("new")}>
              + Tạo đợt điều tra
            </button>
          ) : null
        }
      >
        <p className="mute">
          Đợt điều tra mở rộng (điều tra tổng đàn, khảo sát thiệt hại dịch…): chọn xã, gắn biểu, theo dõi, nhắc, đóng đợt và tổng hợp
          kết quả. Số liệu của đợt tách khỏi lũy kế báo cáo định kỳ.
        </p>
      </PageHeader>

      {editing ? (
        <CampaignEditor
          key={editing === "new" ? "new" : editing.id + ":" + editing.version}
          capi={capi}
          current={editing === "new" ? null : editing}
          templates={templates.data ?? []}
          owners={owners.data ?? []}
          onDone={(c) => {
            setEditing(null);
            list.reload();
            if (c) setOpen(c.id);
          }}
        />
      ) : null}

      <section className="card sec" aria-label="Danh sách đợt điều tra">
        <h2>Đợt điều tra</h2>
        <LoadState s={list}>
          {(items) =>
            items.length === 0 ? (
              <p className="mute">Chưa có đợt điều tra.</p>
            ) : (
              <table className="dt" aria-label="Danh sách đợt điều tra">
                <thead>
                  <tr>
                    <th>Tên đợt</th>
                    <th>Lĩnh vực</th>
                    <th>Thời gian</th>
                    <th>Hạn nộp</th>
                    <th>Người phụ trách</th>
                    <th>Số xã</th>
                    <th>Tiến độ</th>
                    <th>Trạng thái</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {items.map((c) => (
                    <tr key={c.id}>
                      <td>{c.name}</td>
                      <td>{FIELD_LABEL[c.field as keyof typeof FIELD_LABEL] ?? c.field}</td>
                      <td>
                        {ymd(c.from_date)} – {ymd(c.to_date)}
                      </td>
                      <td>{dt(c.due_at)}</td>
                      <td>{c.owner?.display || "—"}</td>
                      <td>{c.tasks}</td>
                      <td>{c.progress ? `${c.progress.submitted}/${c.progress.total} (${percent(c)}%)` : "—"}</td>
                      <td>
                        <StatusPill tone={STATE_TONE[c.state]}>{STATE_LABEL[c.state]}</StatusPill>
                      </td>
                      <td>
                        <button className="btn btn-sm" onClick={() => setOpen(open === c.id ? null : c.id)}>
                          {open === c.id ? "Đóng" : "Xem"}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          }
        </LoadState>
      </section>

      {open ? (
        <CampaignDetail
          key={open}
          id={open}
          capi={capi}
          writer={writer}
          owners={owners.data ?? []}
          onEdit={(c) => setEditing(c)}
          onChanged={() => list.reload()}
          onCopied={(c) => {
            list.reload();
            setOpen(c.id);
          }}
        />
      ) : null}
    </div>
  );
}

/** Màn "Tạo chiến dịch" (menu Chiến dịch điều tra › Tạo chiến dịch): lưu Nháp rồi mở đợt ở màn quản lý để xem trước, phát hành. */
export function CampaignCreateScreen() {
  const { api, kn } = useAuth();
  const router = useRouter();
  const capi = useMemo(() => (api ? createCampaignApi(api) : null), [api]);
  const writer = kn.context?.role === "kn_tinh" && !kn.context?.read_only;
  const templates = useLoad(capi && writer ? () => capi.templates() : null, [capi, writer]);
  const owners = useLoad(capi && writer ? () => capi.owners() : null, [capi, writer]);
  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader title="Tạo chiến dịch">
        <p className="mute">Tạo đợt điều tra ở trạng thái Nháp; xem trước số việc và phát hành ở màn Quản lý chiến dịch.</p>
      </PageHeader>
      {writer ? (
        <CampaignEditor
          capi={capi}
          current={null}
          templates={templates.data ?? []}
          owners={owners.data ?? []}
          onDone={(c) => router.push(c ? `${LIST_PATH}?dot=${encodeURIComponent(c.id)}` : LIST_PATH)}
        />
      ) : (
        <p className="mute" role="alert">
          Chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản (cán bộ tổng hợp; viên chức, chủ nhiệm khi là người phụ trách) tạo đợt điều tra.
        </p>
      )}
    </div>
  );
}

function OwnerSelect({ value, owners, onChange, label = "Người phụ trách", optional = false }: {
  value: string;
  owners: CampaignOwner[];
  onChange: (v: string) => void;
  label?: string;
  optional?: boolean;
}) {
  return (
    <select className="select" aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{optional ? "— Giữ người phụ trách của đợt nguồn —" : "— Chọn người phụ trách —"}</option>
      {owners.map((o) => (
        <option key={o.ref} value={o.ref}>
          {o.display || o.ref}
        </option>
      ))}
    </select>
  );
}

function CampaignEditor({ capi, current, templates, owners, onDone }: {
  capi: CampaignApi;
  current: Campaign | null;
  templates: FormsTemplate[];
  owners: CampaignOwner[];
  onDone: (c: Campaign | null) => void;
}) {
  const [f, setF] = useState<CampaignForm>(current ? formOf(current) : emptyForm());
  const [errs, setErrs] = useState<Record<string, string>>({});
  const act = useAction();
  const set = <K extends keyof CampaignForm>(k: K, v: CampaignForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const save = async () => {
    const e = formErrors(f);
    setErrs(e);
    if (Object.keys(e).length) return;
    try {
      const body = payload(f);
      const c = await act.run(() => (current ? capi.update(current.id, body) : capi.create(body)));
      if (c) onDone(c);
    } catch (e) {
      setErrs(problemFields(e));
    }
  };
  const err = (k: string) => (errs[k] ? <span className="field-error">{errs[k]}</span> : null);
  return (
    <section className="card sec" aria-label={current ? "Sửa đợt điều tra (Nháp)" : "Tạo đợt điều tra"}>
      <h2>{current ? "Sửa đợt điều tra (Nháp)" : "+ Tạo đợt điều tra"}</h2>
      <div className="form-grid">
        <label>
          Tên đợt
          <input className="input" aria-label="Tên đợt" value={f.name} onChange={(e) => set("name", e.target.value)} />
          {err("name")}
        </label>
        <label>
          Lĩnh vực
          <select className="select" aria-label="Lĩnh vực" value={f.field} onChange={(e) => set("field", e.target.value as CampaignForm["field"])}>
            {Object.entries(FIELD_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label className="span-2">
          Mục tiêu
          <textarea className="input" aria-label="Mục tiêu" value={f.goal} onChange={(e) => set("goal", e.target.value)} />
        </label>
        <label>
          Biểu điều tra
          <select className="select" aria-label="Biểu điều tra" value={f.template_code} onChange={(e) => set("template_code", e.target.value)}>
            <option value="">— Chọn biểu (thiết kế ở Quản lý biểu mẫu) —</option>
            {templates.map((t) => (
              <option key={t.code} value={t.code}>
                {t.code} — {t.title} (v{t.version})
              </option>
            ))}
          </select>
          {err("template_code")}
        </label>
        <label>
          Phạm vi
          <select className="select" aria-label="Phạm vi" value={f.scope_mode} onChange={(e) => set("scope_mode", e.target.value as CampaignForm["scope_mode"])}>
            <option value="province">Toàn bộ xã của tỉnh</option>
            <option value="regions">Theo khu vực</option>
            <option value="communes">Danh sách xã</option>
          </select>
        </label>
        {f.scope_mode === "communes" ? (
          <label className="span-2">
            Mã xã (cách nhau bởi dấu phẩy)
            <input className="input" aria-label="Mã xã" value={f.communes} onChange={(e) => set("communes", e.target.value)} />
            {err("communes")}
          </label>
        ) : null}
        {f.scope_mode === "regions" ? (
          <label className="span-2">
            Mã khu vực (cách nhau bởi dấu phẩy)
            <input className="input" aria-label="Mã khu vực" value={f.regions} onChange={(e) => set("regions", e.target.value)} />
            {err("regions")}
          </label>
        ) : null}
        <label>
          Từ ngày
          <input className="input" type="date" aria-label="Từ ngày" value={f.from_date} onChange={(e) => set("from_date", e.target.value)} />
          {err("from_date")}
        </label>
        <label>
          Đến ngày
          <input className="input" type="date" aria-label="Đến ngày" value={f.to_date} onChange={(e) => set("to_date", e.target.value)} />
          {err("to_date")}
        </label>
        <label>
          Hạn nộp
          <input className="input" type="datetime-local" aria-label="Hạn nộp" value={f.due_at} onChange={(e) => set("due_at", e.target.value)} />
          {err("due_at")}
        </label>
        <label>
          Người phụ trách <span aria-hidden="true">*</span>
          <OwnerSelect value={f.owner_ref} owners={owners} onChange={(v) => set("owner_ref", v)} />
          {err("owner_ref")}
        </label>
        <label>
          Nhắc trước hạn (giờ)
          <input className="input" inputMode="numeric" aria-label="Nhắc trước hạn" value={f.remind_before_hours}
            onChange={(e) => set("remind_before_hours", e.target.value)} />
          {err("remind_before_hours")}
        </label>
      </div>
      {act.view}
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Lưu nháp
        </button>
        <button className="btn" onClick={() => onDone(null)}>
          Hủy
        </button>
      </div>
    </section>
  );
}

function CopyPanel({ src, capi, owners, onCopied, onCancel }: {
  src: Campaign;
  capi: CampaignApi;
  owners: CampaignOwner[];
  onCopied: (c: Campaign) => void;
  onCancel: () => void;
}) {
  const [f, setF] = useState<CopyForm>({ name: "", from_date: "", to_date: "", due_at: "", owner_ref: "" });
  const [errs, setErrs] = useState<Record<string, string>>({});
  const act = useAction();
  const set = <K extends keyof CopyForm>(k: K, v: CopyForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const err = (k: string) => (errs[k] ? <span className="field-error">{errs[k]}</span> : null);
  const save = async () => {
    const e = copyErrors(f);
    setErrs(e);
    if (Object.keys(e).length) return;
    try {
      const c = await act.run(() => capi.copy(src.id, copyPayload(f)));
      if (c) onCopied(c);
    } catch (e) {
      setErrs(problemFields(e));
    }
  };
  return (
    <div className="card sub" aria-label="Sao chép đợt">
      <p className="mute">
        Đợt mới (Nháp) giữ mục tiêu, lĩnh vực, biểu {src.template_code}, phạm vi ({src.communes.length} xã), người phụ trách và giờ nhắc của đợt
        này; không mang số liệu đã nộp.
      </p>
      <div className="form-grid">
        <label className="span-2">
          Tên đợt mới
          <input className="input" aria-label="Tên đợt mới" placeholder={src.name} value={f.name} onChange={(e) => set("name", e.target.value)} />
          {err("name")}
        </label>
        <label>
          Từ ngày <span aria-hidden="true">*</span>
          <input className="input" type="date" aria-label="Từ ngày mới" value={f.from_date} onChange={(e) => set("from_date", e.target.value)} />
          {err("from_date")}
        </label>
        <label>
          Đến ngày <span aria-hidden="true">*</span>
          <input className="input" type="date" aria-label="Đến ngày mới" value={f.to_date} onChange={(e) => set("to_date", e.target.value)} />
          {err("to_date")}
        </label>
        <label>
          Hạn nộp <span aria-hidden="true">*</span>
          <input className="input" type="datetime-local" aria-label="Hạn nộp mới" value={f.due_at} onChange={(e) => set("due_at", e.target.value)} />
          {err("due_at")}
        </label>
        <label>
          Người phụ trách
          <OwnerSelect label="Người phụ trách đợt mới" optional value={f.owner_ref} owners={owners} onChange={(v) => set("owner_ref", v)} />
          {err("owner_ref")}
        </label>
      </div>
      {act.view}
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Tạo đợt sao chép
        </button>
        <button className="btn" onClick={onCancel}>
          Hủy
        </button>
      </div>
    </div>
  );
}

function CampaignDetail({ id, capi, writer, owners, onEdit, onChanged, onCopied }: {
  id: string;
  capi: CampaignApi;
  writer: boolean;
  owners: CampaignOwner[];
  onEdit: (c: Campaign) => void;
  onChanged: () => void;
  onCopied: (c: Campaign) => void;
}) {
  const d = useLoad(() => capi.get(id), [capi, id]);
  const [showResults, setShowResults] = useState(false);
  const res = useLoad(showResults ? () => capi.results(id) : null, [capi, id, showResults]);
  const [dueMode, setDueMode] = useState<"extend" | "reopen" | null>(null);
  const [copying, setCopying] = useState(false);
  const [due, setDue] = useState("");
  const [reason, setReason] = useState("");
  const act = useAction();
  const communeName = useCommuneNames(d.data?.communes ?? []);
  const done = () => {
    d.reload();
    onChanged();
    if (showResults) res.reload();
  };
  const run = async <T,>(f: () => Promise<T>, note: string) => {
    if (await act.run(f, note)) done();
  };
  return (
    <section className="card sec" aria-label="Chi tiết đợt điều tra">
      <LoadState s={d}>
        {(c) => (
          <>
            <h2>
              {c.name} <StatusPill tone={STATE_TONE[c.state]}>{STATE_LABEL[c.state]}</StatusPill>
            </h2>
            <p className="mute">
              {FIELD_LABEL[c.field as keyof typeof FIELD_LABEL] ?? c.field} · Biểu {c.template_code}
              {c.template_version ? ` v${c.template_version}` : ""} · {ymd(c.from_date)} – {ymd(c.to_date)} · Hạn nộp {dt(c.due_at)}
              {c.period_key ? ` · Mã kỳ ${c.period_key}` : ""} · Người phụ trách {c.owner?.display || "—"}
            </p>
            {c.goal ? <p>{c.goal}</p> : null}
            {c.state === "draft" ? (
              <p className="mute">
                Xem trước: phát hành sẽ sinh <strong>{c.tasks}</strong> việc cho cán bộ xã ({c.communes.length} xã).
              </p>
            ) : null}
            {writer ? (
              <div className="row">
                {c.can_manage && c.state === "draft" ? (
                  <>
                    <button className="btn" onClick={() => onEdit(c)}>
                      Sửa nháp
                    </button>
                    <button className="btn btn-primary" disabled={act.busy} onClick={() => void run(() => capi.publish(c.id), "Đã phát hành đợt — xã thấy việc trong màn nhập biểu.")}>
                      Phát hành
                    </button>
                  </>
                ) : null}
                {c.can_manage && c.state === "collecting" ? (
                  <>
                    <button className="btn" disabled={act.busy} onClick={() => void run(() => capi.remind(c.id), "Đã nhắc các xã chưa nộp.")}>
                      Nhắc xã chưa nộp
                    </button>
                    <button className="btn" onClick={() => setDueMode("extend")}>
                      Gia hạn
                    </button>
                    <button className="btn btn-primary" disabled={act.busy}
                      onClick={() => void run(() => capi.close(c.id), "Đã đóng đợt — xã chưa gửi chuyển Không nộp.")}>
                      Đóng đợt
                    </button>
                  </>
                ) : null}
                {c.state === "closed" ? (
                  <button className="btn" title="Chỉ Lãnh đạo phòng" onClick={() => setDueMode("reopen")}>
                    Mở lại đợt
                  </button>
                ) : null}
                {c.state !== "draft" ? (
                  <button className="btn btn-o" onClick={() => setShowResults((x) => !x)}>
                    {showResults ? "Ẩn kết quả" : "Kết quả tổng hợp"}
                  </button>
                ) : null}
                <button className="btn btn-o" onClick={() => setCopying((x) => !x)}>
                  Sao chép đợt
                </button>
              </div>
            ) : c.state !== "draft" ? (
              <button className="btn btn-o" onClick={() => setShowResults((x) => !x)}>
                {showResults ? "Ẩn kết quả" : "Kết quả tổng hợp"}
              </button>
            ) : null}
            {dueMode ? (
              <div className="card sub" aria-label={dueMode === "extend" ? "Gia hạn đợt" : "Mở lại đợt"}>
                <p className="mute">
                  {dueMode === "extend" ? "Hạn nộp mới phải sau hạn hiện hành; lý do bắt buộc." : "Chỉ Lãnh đạo phòng mở lại; nhập lý do và thời hạn mới."}
                </p>
                <div className="row">
                  <input className="input" type="datetime-local" aria-label="Hạn mới" value={due} onChange={(e) => setDue(e.target.value)} />
                  <input className="input" aria-label="Lý do" placeholder="Lý do (10–500 ký tự)" value={reason} onChange={(e) => setReason(e.target.value)} />
                  <button className="btn btn-primary" disabled={act.busy || !due || !reasonOk(reason)}
                    onClick={() => {
                      const iso = new Date(due).toISOString();
                      const f = dueMode === "extend" ? () => capi.extend(c.id, iso, reason) : () => capi.reopen(c.id, iso, reason);
                      void run(f, dueMode === "extend" ? "Đã gia hạn đợt." : "Đã mở lại đợt.").then(() => {
                        setDueMode(null);
                        setReason("");
                        setDue("");
                      });
                    }}>
                    Xác nhận
                  </button>
                  <button className="btn" onClick={() => setDueMode(null)}>
                    Hủy
                  </button>
                </div>
              </div>
            ) : null}
            {copying ? (
              <CopyPanel
                src={c}
                capi={capi}
                owners={owners}
                onCancel={() => setCopying(false)}
                onCopied={(n) => {
                  setCopying(false);
                  onCopied(n);
                }}
              />
            ) : null}
            {act.view}

            {c.progress ? (
              <>
                <h3>
                  Tiến độ: {c.progress.submitted}/{c.progress.total} xã đã nộp ({percent(c)}%)
                  {c.progress.overdue ? ` · ${c.progress.overdue} trễ hạn` : ""}
                  {c.progress.missed ? ` · ${c.progress.missed} không nộp` : ""}
                </h3>
                <table className="dt" aria-label="Tiến độ theo khu vực">
                  <thead>
                    <tr>
                      <th>Khu vực</th>
                      <th>Số xã</th>
                      <th>Đã nộp</th>
                      <th>Chưa nộp</th>
                      <th>Trễ hạn</th>
                      <th>Không nộp</th>
                    </tr>
                  </thead>
                  <tbody>
                    {c.progress.regions.map((r) => (
                      <tr key={r.code || "none"}>
                        <td>{r.name}</td>
                        <td>{r.total}</td>
                        <td>{r.submitted}</td>
                        <td>{r.pending}</td>
                        <td>{r.overdue}</td>
                        <td>{r.missed}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <table className="dt" aria-label="Tiến độ theo xã">
                  <thead>
                    <tr>
                      <th>Xã</th>
                      <th>Khu vực</th>
                      <th>Trạng thái</th>
                      <th>Lần gửi</th>
                      <th>Gửi lúc</th>
                    </tr>
                  </thead>
                  <tbody>
                    {c.progress.units.map((u) => (
                      <tr key={u.commune}>
                        <td>{u.commune_name !== u.commune ? u.commune_name : communeName(u.commune)}</td>
                        <td>{u.region_name}</td>
                        <td>
                          {UNIT_STATUS_LABEL[u.status] ?? u.status}
                          {u.late ? " · gửi trễ" : ""}
                        </td>
                        <td>{u.versions || "—"}</td>
                        <td>{u.last_submitted_at ? dt(u.last_submitted_at) : "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            ) : null}

            {showResults ? (
              <LoadState s={res}>
                {(r) => (
                  <div aria-label="Kết quả tổng hợp">
                    <h3>Kết quả tổng hợp</h3>
                    <p className="mute">
                      {r.basis === "closed" ? "Số lúc đóng đợt" : "Số đã gửi (chưa đóng đợt)"} · {r.reported}/{r.units} xã có số liệu · Tách khỏi
                      lũy kế báo cáo định kỳ.
                    </p>
                    <table className="dt" aria-label="Bảng kết quả">
                      <thead>
                        <tr>
                          <th>Xã</th>
                          {r.fields.map((f) => (
                            <th key={f.key}>
                              {f.label}
                              {f.unit ? ` (${f.unit})` : ""}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {r.rows.map((x) => (
                          <tr key={x.commune}>
                            <td>{x.commune_name !== x.commune ? x.commune_name : communeName(x.commune)}</td>
                            {r.fields.map((f) => (
                              <td key={f.key}>{num(x.values[f.key], 3)}</td>
                            ))}
                          </tr>
                        ))}
                        <tr>
                          <th>Tổng</th>
                          {r.fields.map((f) => (
                            <th key={f.key}>{num(r.total[f.key], 3)}</th>
                          ))}
                        </tr>
                      </tbody>
                    </table>
                    <button className="btn btn-o btn-sm" disabled={act.busy}
                      onClick={() => void act.run(() => capi.download(c.id).then((b) => saveBlob(b, `dot-dieu-tra-${c.period_key ?? c.id}.xlsx`)))}>
                      Xuất Excel
                    </button>
                  </div>
                )}
              </LoadState>
            ) : null}

            {c.events.length ? (
              <>
                <h3>Lịch sử</h3>
                <ul className="small">
                  {c.events.map((e, i) => (
                    <li key={i}>
                      {dt(e.at)} · {EVENT_LABEL[e.action] ?? e.action}
                      {e.actor.display ? ` · ${e.actor.display}` : ""}
                      {e.due_at ? ` · hạn ${dt(e.due_at)}` : ""}
                      {e.reminded !== null ? ` · ${e.reminded} xã` : ""}
                      {e.reason ? ` — ${e.reason}` : ""}
                    </li>
                  ))}
                </ul>
              </>
            ) : null}
          </>
        )}
      </LoadState>
    </section>
  );
}
