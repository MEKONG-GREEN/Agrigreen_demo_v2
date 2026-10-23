"use client";

// Lịch tiêm phòng BM-07 (W11.46, BRD CNTS v3.7 UR-TINH-11, FN-15 BR-02, AC-03):
// - Chủ nhiệm Cấp Tỉnh, Admin: "Tạo lịch theo quy trình của mô hình" cho một lứa (hộ/địa bàn, ngày nhận giống, tuổi lúc
//   nhận), sửa ngày dự kiến từng hộ, hủy lịch.
// - Cấp Xã: xem lứa trong xã phụ trách, ghi ngày tiêm thực tế. Mũi quá hạn chưa tiêm → nhãn đỏ.
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { fieldErrorMap } from "@/api/problem";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { ReasonDialog } from "../ReasonDialog";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  BATCH_STATUS_LABEL,
  AGE_RULE,
  BATCH_STATUS_TONE,
  HEAD_RULE,
  batchErrors,
  intValue,
  createCntsCalApi,
  type BatchCreate,
  type BatchStatus,
  type CntsCalApi,
  type VaccinationBatchDetail,
} from "./api";

const dmy = (s: string | null | undefined) => (s ? s.split("-").reverse().join("/") : "—");

export function VaccinationScreen() {
  const { api, kn } = useAuth();
  const capi = useMemo(() => (api ? createCntsCalApi(api) : null), [api]);
  const role = kn.context?.role;
  const readOnly = !!kn.context?.read_only;
  const myCommunes = (kn.context?.communes ?? []).map((c) => c.code);
  const [status, setStatus] = useState<BatchStatus | "">("");
  const list = useLoad(capi ? () => capi.batches(status ? { status } : {}) : null, [capi, status]);
  const communeName = useCommuneNames((list.data ?? []).map((b) => b.commune_code));
  const [open, setOpen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  if (!capi) return null;
  const planner = (role === "kn_tinh" || role === "admin") && !readOnly;
  return (
    <div className="page">
      <PageHeader
        title="Lịch tiêm phòng"
        actions={
          planner ? (
            <button className="btn btn-primary" onClick={() => setCreating(true)}>
              Tạo lịch theo quy trình
            </button>
          ) : null
        }
      >
        <p className="mute">
          Ngày dự kiến = ngày nhận giống + ngày tuổi của mũi − tuổi lúc nhận. Đổi quy trình chỉ áp dụng lứa mới. Mũi quá ngày dự kiến chưa có ngày tiêm thực tế
          hiện nhãn đỏ.
        </p>
      </PageHeader>
      {creating ? (
        <CreateBatch
          capi={capi}
          communes={role === "kn_xa" ? myCommunes : []}
          onDone={(ref) => {
            setCreating(false);
            list.reload();
            if (ref) setOpen(ref);
          }}
        />
      ) : null}
      <section className="card sec" aria-label="Các lứa">
        <div className="row" role="tablist" aria-label="Trạng thái">
          {(["", "overdue", "open", "done", "cancelled"] as const).map((k) => (
            <button key={k} role="tab" aria-selected={status === k} className={`btn btn-sm${status === k ? " btn-primary" : ""}`} onClick={() => setStatus(k)}>
              {k === "" ? "Tất cả" : BATCH_STATUS_LABEL[k]}
            </button>
          ))}
        </div>
        <LoadState s={list}>
          {(items) =>
            items.length === 0 ? (
              <p className="mute">Chưa có lịch tiêm.</p>
            ) : (
              <DataTable
                caption="Các lứa"
                rows={items}
                rowKey={(b) => b.ref}
                columns={[
                  { key: "commune", header: "Xã", cell: (b) => communeName(b.commune_code) },
                  { key: "loc", header: "Hộ / địa bàn", cell: (b) => b.locality ?? (b.household_ref ? `Hộ ${b.household_ref.slice(0, 8)}` : "—") },
                  { key: "model", header: "Mô hình", cell: (b) => b.model_type_name ?? b.model_type_code },
                  { key: "recv", header: "Ngày nhận giống", cell: (b) => dmy(b.received_date) },
                  { key: "next", header: "Mũi kế tiếp", cell: (b) => (b.next_dose ? `${b.next_dose.vaccine_name ?? b.next_dose.vaccine_code} L${b.next_dose.shot} · ${dmy(b.next_dose.expected_date)}` : "—") },
                  {
                    key: "st",
                    header: "Trạng thái",
                    cell: (b) => (
                      <StatusPill tone={BATCH_STATUS_TONE[b.status]}>
                        {BATCH_STATUS_LABEL[b.status]}
                        {b.overdue_count ? ` (${b.overdue_count})` : ""}
                      </StatusPill>
                    ),
                  },
                  {
                    key: "act",
                    header: "",
                    cell: (b) => (
                      <button className="btn btn-sm" onClick={() => setOpen(open === b.ref ? null : b.ref)}>
                        {open === b.ref ? "Đóng" : "Xem"}
                      </button>
                    ),
                  },
                ]}
              />
            )
          }
        </LoadState>
      </section>
      {open ? <BatchDetail key={open} capi={capi} ref_={open} onChanged={list.reload} /> : null}
    </div>
  );
}

function CreateBatch({ capi, communes, onDone }: { capi: CntsCalApi; communes: string[]; onDone: (ref?: string) => void }) {
  const { api } = useAuth();
  const models = useLoad(
    api ? async () => (await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_model_type" }, query: {} } }))).items as CatalogItem[] : null,
    [api],
  );
  const [f, setF] = useState({ commune_code: communes[0] ?? "", model_type_code: "", locality: "", received_date: "", age_at_receipt: "1", head_count: "", note: "" });
  const [errs, setErrs] = useState<Record<string, string | undefined>>({});
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const err = (k: string) => errs[k] ?? server[k];
  const set = (k: keyof typeof f) => (v: string) => setF((x) => ({ ...x, [k]: v }));
  const save = async () => {
    const age = intValue(f.age_at_receipt, AGE_RULE);
    const head = intValue(f.head_count, HEAD_RULE);
    const body: Partial<BatchCreate> = {
      commune_code: f.commune_code.trim(),
      model_type_code: f.model_type_code,
      locality: f.locality.trim() || null,
      received_date: f.received_date,
      age_at_receipt: age.ok ? (age.value ?? undefined) : undefined,
      head_count: head.ok ? head.value : null,
      note: f.note.trim() || null,
    };
    const e: Record<string, string | undefined> = batchErrors(body);
    if (!age.ok) e.age_at_receipt = age.error;
    if (!head.ok) e.head_count = head.error;
    setErrs(e);
    if (Object.values(e).some(Boolean)) return;
    const r = await act.run(() => capi.createBatch(body as BatchCreate));
    if (r) onDone(r.ref);
  };
  return (
    <section className="card sec" aria-label="Tạo lịch tiêm">
      <h2>Tạo lịch theo quy trình của mô hình</h2>
      {act.view}
      <div className="form-grid">
        {communes.length ? (
          <SelectField label="Xã/phường" required value={f.commune_code} onChange={set("commune_code")} error={err("commune_code")} options={communes.map((c) => ({ value: c, label: c }))} />
        ) : (
          <TextField label="Mã xã/phường" required value={f.commune_code} onChange={set("commune_code")} error={err("commune_code")} maxLength={10} />
        )}
        <SelectField
          label="Loại mô hình"
          required
          value={f.model_type_code}
          onChange={set("model_type_code")}
          error={err("model_type_code")}
          placeholder="— Chọn —"
          options={(models.data ?? []).map((m) => ({ value: m.code, label: m.name }))}
        />
        <TextField label="Địa bàn (ấp, khu vực)" required value={f.locality} onChange={set("locality")} error={err("locality")} placeholder="Không ghi họ tên, số điện thoại" maxLength={300} />
        <div className="field">
          <label>
            Ngày nhận giống
            <span className="req" aria-hidden="true">
              {" "}*
            </span>
            <input className="input" type="date" aria-required aria-invalid={err("received_date") ? true : undefined} value={f.received_date} onChange={(e) => set("received_date")(e.target.value)} />
          </label>
          {err("received_date") ? (
            <span className="err" role="alert">
              {err("received_date")}
            </span>
          ) : null}
        </div>
        <NumberField label="Tuổi lúc nhận" unit="ngày" rule={AGE_RULE} value={f.age_at_receipt} onChange={set("age_at_receipt")} error={err("age_at_receipt")} onBlurError={(m) => setErrs((x) => ({ ...x, age_at_receipt: m }))} />
        <NumberField label="Số con" rule={HEAD_RULE} value={f.head_count} onChange={set("head_count")} error={err("head_count")} onBlurError={(m) => setErrs((x) => ({ ...x, head_count: m }))} />
        <TextField label="Ghi chú" value={f.note} onChange={set("note")} maxLength={1000} />
      </div>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Tạo lịch
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

function BatchDetail({ capi, ref_, onChanged }: { capi: CntsCalApi; ref_: string; onChanged: () => void }) {
  const s = useLoad(() => capi.batch(ref_), [capi, ref_]);
  return <LoadState s={s}>{(b) => <BatchEditor capi={capi} b={b} onSaved={() => (s.reload(), onChanged())} />}</LoadState>;
}

function BatchEditor({ capi, b, onSaved }: { capi: CntsCalApi; b: VaccinationBatchDetail; onSaved: () => void }) {
  const act = useAction();
  const [edit, setEdit] = useState<Record<string, { expected: string; actual: string }>>({});
  const [cancelling, setCancelling] = useState(false);
  const key = (v: string, shot: number) => `${v}/${shot}`;
  const changes = b.doses.flatMap((d) => {
    const e = edit[key(d.vaccine_code, d.shot)];
    if (!e) return [];
    const u: DosesChange = { vaccine_code: d.vaccine_code, shot: d.shot };
    if (b.can_plan && e.expected && e.expected !== d.expected_date) u.expected_date = e.expected;
    if (e.actual && e.actual !== d.actual_date) u.actual_date = e.actual;
    return u.expected_date || u.actual_date ? [u] : [];
  });
  return (
    <section className="card sec" aria-label="Chi tiết lịch tiêm">
      <h2>
        {b.model_type_name ?? b.model_type_code} · nhận giống {dmy(b.received_date)} ({b.age_at_receipt} ngày tuổi) · {b.schedule_name ?? b.schedule_code}
      </h2>
      {act.view}
      <DataTable
        caption="Mũi tiêm của lứa"
        rows={b.doses}
        rowKey={(d) => key(d.vaccine_code, d.shot)}
        columns={[
          { key: "vx", header: "Vaccine", cell: (d) => d.vaccine_name ?? d.vaccine_code },
          { key: "shot", header: "Lần", cell: (d) => d.shot },
          { key: "age", header: "Ngày tuổi", cell: (d) => d.age_days },
          {
            key: "exp",
            header: "Ngày dự kiến",
            cell: (d) => {
              const k = key(d.vaccine_code, d.shot);
              const e = edit[k] ?? { expected: d.expected_date, actual: d.actual_date ?? "" };
              return (
                <>
                  {b.can_plan ? (
                    <input type="date" aria-label={`Ngày dự kiến ${k}`} value={e.expected} onChange={(x) => setEdit({ ...edit, [k]: { ...e, expected: x.target.value } })} />
                  ) : (
                    dmy(d.expected_date)
                  )}{" "}
                  {d.overdue ? <StatusPill tone="bad">Quá hạn</StatusPill> : null}
                </>
              );
            },
          },
          {
            key: "act",
            header: "Ngày tiêm thực tế",
            cell: (d) => {
              const k = key(d.vaccine_code, d.shot);
              const e = edit[k] ?? { expected: d.expected_date, actual: d.actual_date ?? "" };
              return b.can_record ? (
                <input type="date" aria-label={`Ngày tiêm thực tế ${k}`} value={e.actual} onChange={(x) => setEdit({ ...edit, [k]: { ...e, actual: x.target.value } })} />
              ) : (
                dmy(d.actual_date)
              );
            },
          },
        ]}
      />
      <div className="row">
        {b.can_record ? (
          <button
            className="btn btn-primary"
            disabled={act.busy || changes.length === 0}
            onClick={async () => {
              const r = await act.run(() => capi.updateDoses(b.ref, { base_version: b.version, doses: changes }), "Đã ghi lịch tiêm.");
              if (r) {
                setEdit({});
                onSaved();
              }
            }}
          >
            Lưu mũi tiêm
          </button>
        ) : null}
        {b.can_plan ? (
          <button className="btn" disabled={act.busy} onClick={() => setCancelling(true)}>
            Hủy lịch
          </button>
        ) : null}
      </div>
      {cancelling ? (
        <ReasonDialog
          title="Hủy lịch tiêm của lứa"
          label="Lý do hủy lịch"
          confirmLabel="Hủy lịch"
          min={5}
          max={1000}
          busy={act.busy}
          onClose={() => setCancelling(false)}
          onConfirm={async (reason) => {
            const r = await act.run(() => capi.cancelBatch(b.ref, b.version, reason), "Đã hủy lịch tiêm của lứa.");
            if (r) {
              setCancelling(false);
              onSaved();
            }
          }}
        />
      ) : null}
      <h3>Lịch sử</h3>
      <ul>
        {b.versions.map((v) => (
          <li key={v.version}>
            Phiên bản {v.version} · {dt(v.updated_at)} · {v.actor_display ?? v.updated_by.slice(0, 8)} · {EVENT_LABEL[v.event]}
            {v.status_reason ? ` · ${v.status_reason}` : ""}
          </li>
        ))}
      </ul>
    </section>
  );
}

type DosesChange = { vaccine_code: string; shot: number; expected_date?: string; actual_date?: string };

const EVENT_LABEL: Record<VaccinationBatchDetail["event"], string> = {
  created: "Tạo lịch",
  updated: "Sửa ngày dự kiến",
  dose_recorded: "Ghi ngày tiêm",
  cancelled: "Hủy",
};
