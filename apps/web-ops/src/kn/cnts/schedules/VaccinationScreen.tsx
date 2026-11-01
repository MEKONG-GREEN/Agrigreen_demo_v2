"use client";

// Lịch tiêm phòng BM-07 (W11.46, BF-CN-VACC #168 — BRD CNTS v3.7 UR-TINH-11, FN-15 BR-02, AC-03):
// - Chủ nhiệm Cấp Tỉnh, Admin: "Tạo lịch theo quy trình của mô hình" cho một hoặc nhiều hộ của mô hình (FN-10: dự án →
//   điểm → hộ; xã, loại mô hình theo điểm) hoặc theo địa bàn (chọn xã từ ĐVHC), giống (quy trình theo giống nếu có), ngày
//   nhận giống, tuổi lúc nhận; sửa ngày dự kiến từng hộ, hủy lịch.
// - Cấp Xã: xem lứa trong xã phụ trách, ghi ngày tiêm thực tế. Mũi quá hạn chưa tiêm → nhãn đỏ.
// - Bảng BM-07: hàng = hộ (xã, chủ hộ, CB theo dõi MH, ngày nhận giống), cột = vaccine × lần (dự kiến/thực tế), sửa ngay
//   trên bảng, xuất Excel đúng mẫu.
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { fieldErrorMap } from "@/api/problem";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { saveBlob } from "@/lib/files";
import { ReasonDialog } from "../ReasonDialog";
import { CommuneField } from "../catalog/CommuneField";
import { createCntsModelApi, type CntsModelApi } from "../model/api";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  BATCH_STATUS_LABEL,
  AGE_RULE,
  BATCH_STATUS_TONE,
  HEAD_RULE,
  batchErrors,
  householdText,
  intValue,
  rowChanges,
  vaccineGroups,
  createCntsCalApi,
  type BatchCreate,
  type BatchStatus,
  type CntsCalApi,
  type MatrixQuery,
  type MatrixRow,
  type VaccinationBatchDetail,
} from "./api";

const dmy = (s: string | null | undefined) => (s ? s.split("-").reverse().join("/") : "—");

function useCatalog(code: "cnts_model_type" | "cnts_breed") {
  const { api } = useAuth();
  return useLoad(
    api ? async () => (await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: code }, query: {} } }))).items as CatalogItem[] : null,
    [api, code],
  );
}

export function VaccinationScreen() {
  const { api, kn } = useAuth();
  const capi = useMemo(() => (api ? createCntsCalApi(api) : null), [api]);
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const role = kn.context?.role;
  const readOnly = !!kn.context?.read_only;
  const myCommunes = (kn.context?.communes ?? []).map((c) => c.code);
  const [tab, setTab] = useState<"list" | "matrix">("list");
  const [status, setStatus] = useState<BatchStatus | "">("");
  const list = useLoad(capi && tab === "list" ? () => capi.batches(status ? { status } : {}) : null, [capi, status, tab]);
  const communeName = useCommuneNames((list.data ?? []).map((b) => b.commune_code));
  const [open, setOpen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  if (!capi || !mapi) return null;
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
          hiện nhãn đỏ và sinh cảnh báo cho cán bộ theo dõi điểm.
        </p>
      </PageHeader>
      {creating ? (
        <CreateBatch
          capi={capi}
          mapi={mapi}
          communes={role === "kn_xa" ? myCommunes : []}
          onDone={(ref) => {
            setCreating(false);
            list.reload();
            if (ref) {
              setTab("list");
              setOpen(ref);
            }
          }}
        />
      ) : null}
      <div className="row" role="tablist" aria-label="Cách xem">
        <button role="tab" aria-selected={tab === "list"} className={`btn btn-sm${tab === "list" ? " btn-primary" : ""}`} onClick={() => setTab("list")}>
          Danh sách lứa
        </button>
        <button role="tab" aria-selected={tab === "matrix"} className={`btn btn-sm${tab === "matrix" ? " btn-primary" : ""}`} onClick={() => setTab("matrix")}>
          Bảng BM-07
        </button>
      </div>
      {tab === "matrix" ? (
        <MatrixView capi={capi} mapi={mapi} />
      ) : (
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
                    { key: "loc", header: "Hộ / địa bàn", cell: (b) => householdText(b) },
                    { key: "site", header: "Điểm (mô hình)", cell: (b) => (b.model ? `${b.model.site_name} · ${b.model.project_code}` : "—") },
                    { key: "model", header: "Loại mô hình", cell: (b) => b.model_type_name ?? b.model_type_code },
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
      )}
      {open && tab === "list" ? <BatchDetail key={open} capi={capi} ref_={open} onChanged={list.reload} /> : null}
    </div>
  );
}

const today = () => new Date().toISOString().slice(0, 10);

function CreateBatch({ capi, mapi, communes, onDone }: { capi: CntsCalApi; mapi: CntsModelApi; communes: string[]; onDone: (ref?: string) => void }) {
  const models = useCatalog("cnts_model_type");
  const breeds = useCatalog("cnts_breed");
  const [mode, setMode] = useState<"household" | "locality">("household");
  const [f, setF] = useState({
    project_ref: "", site_ref: "", commune_code: communes[0] ?? "", model_type_code: "", breed_code: "", locality: "", received_date: "",
    age_at_receipt: "1", head_count: "", note: "",
  });
  const [picked, setPicked] = useState<string[]>([]);
  const projects = useLoad(mode === "household" ? async () => (await mapi.projects({ status: "active" })).items : null, [mapi, mode]);
  const sites = useLoad(f.project_ref ? () => mapi.sites({ project_ref: f.project_ref, status: "active" }) : null, [mapi, f.project_ref]);
  const households = useLoad(f.site_ref ? () => mapi.households({ site_ref: f.site_ref, status: "active" }) : null, [mapi, f.site_ref]);
  const site = (sites.data ?? []).find((s) => s.ref === f.site_ref);
  const [errs, setErrs] = useState<Record<string, string | undefined>>({});
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const err = (k: string) => errs[k] ?? server[k];
  const set = (k: keyof typeof f) => (v: string) => setF((x) => ({ ...x, [k]: v }));
  const modelType = mode === "household" ? (site?.model_type_code ?? "") : f.model_type_code;
  const save = async () => {
    const age = intValue(f.age_at_receipt, AGE_RULE);
    const head = intValue(f.head_count, HEAD_RULE);
    const base: Partial<BatchCreate> = {
      breed_code: f.breed_code || null,
      received_date: f.received_date,
      age_at_receipt: age.ok ? (age.value ?? undefined) : undefined,
      head_count: head.ok ? head.value : null,
      note: f.note.trim() || null,
    };
    const bodies: Partial<BatchCreate>[] =
      mode === "household"
        ? picked.map((h) => ({ ...base, household_ref: h }))
        : [{ ...base, commune_code: f.commune_code.trim(), model_type_code: f.model_type_code, locality: f.locality.trim() || null }];
    const e: Record<string, string | undefined> = mode === "household" ? batchErrors({ ...base, household_ref: picked[0] }) : batchErrors(bodies[0]!);
    if (mode === "household" && !f.site_ref) e.site_ref = "Chọn điểm trình diễn của mô hình.";
    if (mode === "household" && picked.length === 0) e.household_ref = "Chọn ít nhất một hộ của mô hình.";
    if (!age.ok) e.age_at_receipt = age.error;
    if (!head.ok) e.head_count = head.error;
    setErrs(e);
    if (Object.values(e).some(Boolean)) return;
    // Mỗi hộ một lứa; bấm lại sau lỗi mạng dùng lại khóa của từng hộ (useAction) — không tạo lứa trùng.
    const r = await act.run(async (idem) => {
      let last: VaccinationBatchDetail | null = null;
      for (const b of bodies) last = await idem(() => capi.createBatch(b as BatchCreate));
      return last;
    });
    if (r) onDone(bodies.length === 1 ? r.ref : undefined);
  };
  const hh = households.data ?? [];
  return (
    <section className="card sec" aria-label="Tạo lịch tiêm">
      <h2>Tạo lịch theo quy trình của mô hình</h2>
      {act.view}
      <div className="row" role="radiogroup" aria-label="Lập lịch cho">
        <label>
          <input type="radio" name="vx-mode" checked={mode === "household"} onChange={() => setMode("household")} /> Hộ của mô hình (FN-10)
        </label>
        <label>
          <input type="radio" name="vx-mode" checked={mode === "locality"} onChange={() => setMode("locality")} /> Theo địa bàn (không gắn hộ)
        </label>
      </div>
      <div className="form-grid">
        {mode === "household" ? (
          <>
            <SelectField
              label="Chương trình, dự án"
              required
              value={f.project_ref}
              onChange={(v) => (setF((x) => ({ ...x, project_ref: v, site_ref: "" })), setPicked([]))}
              placeholder={projects.loading ? "Đang tải…" : "— Chọn —"}
              options={(projects.data ?? []).map((p) => ({ value: p.ref, label: `${p.code} · ${p.name}` }))}
            />
            <SelectField
              label="Điểm trình diễn (mô hình)"
              required
              value={f.site_ref}
              error={err("site_ref")}
              onChange={(v) => (set("site_ref")(v), setPicked([]))}
              placeholder={!f.project_ref ? "Chọn dự án trước" : "— Chọn —"}
              options={(sites.data ?? []).map((s) => ({ value: s.ref, label: `${s.name} · ${s.model_type_name ?? s.model_type_code}` }))}
            />
            {site ? (
              <p className="mute">
                Loại mô hình: {site.model_type_name ?? site.model_type_code} · CB theo dõi MH: {site.officer_display ?? "—"}
              </p>
            ) : null}
            {f.site_ref ? (
              <fieldset className="field" aria-label="Hộ của mô hình">
                <legend>
                  Hộ của mô hình <span className="req" aria-hidden="true">*</span>
                </legend>
                {hh.length === 0 ? <p className="mute">Điểm chưa có hộ đang tham gia.</p> : null}
                {hh.map((h) => (
                  <label key={h.ref} style={{ display: "block" }}>
                    <input
                      type="checkbox"
                      checked={picked.includes(h.ref)}
                      onChange={(e) => setPicked((p) => (e.target.checked ? [...p, h.ref] : p.filter((x) => x !== h.ref)))}
                    />{" "}
                    {h.owner_label ?? `Hộ ${h.ref.slice(0, 8)}`}
                    {h.scale ? ` · ${h.scale} ${h.scale_unit ?? ""}` : ""}
                  </label>
                ))}
                {err("household_ref") ? (
                  <span className="err" role="alert">
                    {err("household_ref")}
                  </span>
                ) : null}
              </fieldset>
            ) : null}
          </>
        ) : (
          <>
            {communes.length ? (
              <SelectField label="Xã/phường" required value={f.commune_code} onChange={set("commune_code")} error={err("commune_code")} options={communes.map((c) => ({ value: c, label: c }))} />
            ) : (
              <CommuneField label="Xã/phường/đặc khu" required value={f.commune_code} at={f.received_date || today()} error={err("commune_code")} onChange={set("commune_code")} />
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
          </>
        )}
        <SelectField
          label="Giống"
          value={f.breed_code}
          onChange={set("breed_code")}
          error={err("breed_code")}
          placeholder="— Không chọn (quy trình theo loại mô hình) —"
          hint="Có quy trình riêng cho giống thì dùng quy trình đó (BR-02)."
          options={(breeds.data ?? []).filter((b) => b.active !== false).map((b) => ({ value: b.code, label: b.name }))}
        />
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
      {modelType ? null : mode === "household" && f.site_ref ? <p className="mute">Điểm chưa có loại mô hình.</p> : null}
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          {mode === "household" && picked.length > 1 ? `Tạo lịch cho ${picked.length} hộ` : "Tạo lịch"}
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

function MatrixView({ capi, mapi }: { capi: CntsCalApi; mapi: CntsModelApi }) {
  const models = useCatalog("cnts_model_type");
  const [q, setQ] = useState<MatrixQuery>({ model_type_code: "" });
  const projects = useLoad(async () => (await mapi.projects()).items, [mapi]);
  const sites = useLoad(q.project_ref ? () => mapi.sites({ project_ref: q.project_ref }) : null, [mapi, q.project_ref]);
  const m = useLoad(q.model_type_code ? () => capi.matrix(q) : null, [capi, q.model_type_code, q.project_ref, q.site_ref]);
  const act = useAction();
  return (
    <section className="card sec" aria-label="Bảng BM-07">
      <div className="form-grid">
        <SelectField
          label="Loại mô hình (bảng)"
          required
          value={q.model_type_code}
          onChange={(v) => setQ((x) => ({ ...x, model_type_code: v }))}
          placeholder="— Chọn —"
          options={(models.data ?? []).map((x) => ({ value: x.code, label: x.name }))}
        />
        <SelectField
          label="Chương trình, dự án"
          value={q.project_ref ?? ""}
          onChange={(v) => setQ((x) => ({ ...x, project_ref: v || undefined, site_ref: undefined }))}
          placeholder="Tất cả"
          options={(projects.data ?? []).map((p) => ({ value: p.ref, label: `${p.code} · ${p.name}` }))}
        />
        <SelectField
          label="Điểm trình diễn"
          value={q.site_ref ?? ""}
          onChange={(v) => setQ((x) => ({ ...x, site_ref: v || undefined }))}
          placeholder="Tất cả"
          options={(sites.data ?? []).map((s) => ({ value: s.ref, label: s.name }))}
        />
      </div>
      {act.view}
      {!q.model_type_code ? (
        <p className="mute">Chọn loại mô hình — mỗi bảng BM-07 là một mô hình như sheet của tệp mẫu.</p>
      ) : (
        <LoadState s={m}>
          {(mx) => (
            <>
              <div className="row">
                <button
                  className="btn"
                  disabled={act.busy}
                  onClick={() => void act.run(() => capi.matrixXlsx(q).then((b) => saveBlob(b, `lich-tiem-phong-bm07-${q.model_type_code.toLowerCase()}.xlsx`)))}
                >
                  Xuất Excel BM-07
                </button>
                {mx.truncated ? <span className="mute">Chỉ hiện {mx.max_rows} lứa đầu — thu hẹp bộ lọc.</span> : null}
              </div>
              {mx.rows.length === 0 ? (
                <p className="mute">Chưa có lịch tiêm của loại mô hình này.</p>
              ) : (
                <div style={{ overflowX: "auto" }}>
                  <table className="table" aria-label={`Lịch tiêm phòng ${mx.model_type_name ?? mx.model_type_code}`}>
                    <thead>
                      <tr>
                        {["Stt", "Địa chỉ", "Họ tên nông dân", "CB theo dõi MH", "Ngày nhận giống"].map((h) => (
                          <th key={h} rowSpan={2} scope="col">
                            {h}
                          </th>
                        ))}
                        {vaccineGroups(mx.columns).map((g) => (
                          <th key={g.code} colSpan={2 * g.span} scope="colgroup">
                            Vaccin {g.name}
                          </th>
                        ))}
                        <th rowSpan={2} />
                      </tr>
                      <tr>
                        {mx.columns.flatMap((c) => {
                          const shot = mx.columns.filter((x) => x.vaccine_code === c.vaccine_code).length > 1 ? ` (Lần ${c.shot})` : "";
                          return [
                            <th key={`${c.vaccine_code}/${c.shot}/e`} scope="col">
                              Ngày tiêm dự kiến{shot}
                            </th>,
                            <th key={`${c.vaccine_code}/${c.shot}/a`} scope="col">
                              Ngày tiêm thực tế{shot}
                            </th>,
                          ];
                        })}
                      </tr>
                    </thead>
                    <tbody>
                      {mx.rows.map((r, i) => (
                        <MatrixRowView key={r.ref} i={i} r={r} capi={capi} onSaved={m.reload} />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}
        </LoadState>
      )}
    </section>
  );
}

function MatrixRowView({ i, r, capi, onSaved }: { i: number; r: MatrixRow; capi: CntsCalApi; onSaved: () => void }) {
  const act = useAction();
  const [edit, setEdit] = useState<Record<string, { expected?: string; actual?: string }>>({});
  const changes = rowChanges(r, edit);
  const who = r.household_text ?? "—";
  return (
    <tr>
      <td>{i + 1}</td>
      <td>{r.commune_name ?? r.commune_code}</td>
      <td>{who}</td>
      <td>{r.model?.officer_display ?? "—"}</td>
      <td>{dmy(r.received_date)}</td>
      {r.cells.flatMap((d, k) => {
        if (!d) return [<td key={`${k}e`}>—</td>, <td key={`${k}a`}>—</td>];
        const key = `${d.vaccine_code}/${d.shot}`;
        const e = edit[key] ?? {};
        return [
          <td key={`${k}e`} className={d.overdue ? "bad" : undefined}>
            {r.can_plan ? (
              <input
                type="date"
                aria-label={`Ngày dự kiến ${key} hộ ${i + 1}`}
                value={e.expected ?? d.expected_date}
                onChange={(x) => setEdit({ ...edit, [key]: { ...e, expected: x.target.value } })}
              />
            ) : (
              dmy(d.expected_date)
            )}
          </td>,
          <td key={`${k}a`}>
            {r.can_record ? (
              <input
                type="date"
                aria-label={`Ngày tiêm thực tế ${key} hộ ${i + 1}`}
                value={e.actual ?? d.actual_date ?? ""}
                onChange={(x) => setEdit({ ...edit, [key]: { ...e, actual: x.target.value } })}
              />
            ) : (
              dmy(d.actual_date)
            )}{" "}
            {d.overdue ? <StatusPill tone="bad">Quá hạn</StatusPill> : null}
          </td>,
        ];
      })}
      <td>
        {r.can_record ? (
          <button
            className="btn btn-sm btn-primary"
            aria-label={`Lưu hộ ${i + 1}`}
            disabled={act.busy || changes.length === 0}
            onClick={async () => {
              const ok = await act.run(() => capi.updateDoses(r.ref, { base_version: r.version, doses: changes }), "Đã lưu.");
              if (ok) {
                setEdit({});
                onSaved();
              }
            }}
          >
            Lưu
          </button>
        ) : null}
        {act.problem ? act.view : null}
      </td>
    </tr>
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
      {b.model ? (
        <p className="mute">
          Hộ {householdText(b)} · {b.model.site_name} ({b.model.project_code}) · CB theo dõi MH: {b.model.officer_display ?? "—"}
          {b.breed_name || b.breed_code ? ` · Giống: ${b.breed_name ?? b.breed_code}` : ""}
        </p>
      ) : null}
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
