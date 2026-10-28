"use client";

// Biểu kỳ Cấp Xã CNTS — Cấp Tỉnh / Admin (W11.42, BRD CNTS v3.7 UR-TINH-07/08, D4 CN2): biểu BM-01/04/05/17 (cài vào
// cap/forms — Admin), phát hành lô kỳ (kỳ ngày BM-01 cho cơ sở giết mổ; tuần Thứ Ba – Thứ Hai BM-04/BM-17 theo lịch CNTS D8; tuần làm việc
// BM-05) với hạn riêng từng biểu, theo dõi nộp theo kỳ, chốt / mở lại / hủy kỳ.
// BF-SWEEP-CNTS (#101): bảng có STT (DataTable), trường bắt buộc báo lỗi theo trường trước khi gọi API, lý do mở lại / hủy
// kỳ nhập trong pop-up ngắn thay `window.prompt`.
// BF-CN-PUBLISH (#108, CN-07): cấu hình phát hành theo bước — biểu; đối tượng (tất cả xã / khu vực / xã có nuôi thủy sản /
// xã có mô hình / chọn từng xã từ danh sách, cơ sở giết mổ với BM-01); kỳ, hạn nộp, giờ chốt, cán bộ tỉnh phụ trách; xem
// trước số nhiệm vụ. Màn theo dõi có Đã nộp / Nháp / Chưa nhập / Trễ / Trả lại / Không nộp theo kỳ và từng đơn vị, nút Nhắc.
import { useId, useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField, parseNumberInput } from "@/components/form";
import { DataTable, PageHeader, SearchBox, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { createAlertApi } from "../alert/api";
import { ReasonDialog } from "../ReasonDialog";
import {
  TARGET_LABEL,
  TRACK_LABEL,
  TRACK_TONE,
  createCntsPeriodApi,
  periodRows,
  type CntsForm,
  type CntsSeries,
  type CntsSeriesInput,
  type CntsTarget,
  type PeriodRow,
} from "./api";

const PERIOD_STATUS: Record<string, [string, "neutral" | "ok" | "warn" | "bad" | "info"]> = {
  open: ["Đang mở", "info"],
  reopened: ["Mở lại", "warn"],
  closed: ["Đã chốt", "ok"],
  cancelled: ["Đã hủy", "bad"],
};

/** Đối tượng chọn được cho biểu nhập theo xã (thứ tự hiển thị). */
export const COMMUNE_TARGETS: CntsTarget[] = ["province", "regions", "aquaculture", "model", "communes"];

const CLOSE_RULE = { integer: true, maxDecimals: 0 } as const;

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

type PublishErrors = Partial<Record<"from" | "to" | "units" | "dueTime" | "regions" | "owner" | "closeAfter", string>>;

export interface PublishDraft {
  from: string;
  to: string;
  target: CntsTarget;
  units: string[];
  regions: string[];
  dueTime: string;
  owner: string;
  closeAfter: string;
}

/** Kiểm cấu hình phát hành trước khi xem trước / phát hành (R-A4; máy chủ kiểm lại). */
export function publishErrors(v: PublishDraft): PublishErrors {
  const e: PublishErrors = {};
  if (!v.from) e.from = "Bắt buộc nhập.";
  if (!v.to) e.to = "Bắt buộc nhập.";
  else if (v.from && v.to < v.from) e.to = "Đến ngày không được trước Từ ngày.";
  if ((v.target === "communes" || v.target === "facilities") && v.units.length === 0) e.units = "Chọn ít nhất một đơn vị nhận.";
  if (v.target === "regions" && v.regions.length === 0) e.regions = "Chọn ít nhất một khu vực.";
  if (v.dueTime.trim() && !/^([01]\d|2[0-3]):[0-5]\d$/.test(v.dueTime.trim())) e.dueTime = "Giờ dạng HH:MM, vd 16:00.";
  if (!v.owner) e.owner = "Chọn cán bộ tỉnh phụ trách biểu.";
  if (v.closeAfter.trim()) {
    const p = parseNumberInput(v.closeAfter, CLOSE_RULE);
    if (!p.ok) e.closeAfter = p.error;
    else if (Number(p.value) > 720) e.closeAfter = "Tối đa 720 giờ (30 ngày) sau hạn.";
  }
  return e;
}

/** Thân yêu cầu xem trước / phát hành từ cấu hình trên màn. */
export function seriesBody(code: string, v: PublishDraft, form: Pick<CntsForm, "deadline_offset_days"> | undefined): CntsSeriesInput {
  const close = v.closeAfter.trim() ? parseNumberInput(v.closeAfter, CLOSE_RULE) : null;
  return {
    code,
    from_date: v.from,
    to_date: v.to,
    target: v.target,
    units: v.target === "communes" || v.target === "facilities" ? v.units : [],
    ...(v.target === "regions" ? { regions: v.regions } : {}),
    ...(v.owner ? { owner_ref: v.owner } : {}),
    ...(close?.ok ? { close_after_hours: Number(close.value) } : {}),
    ...(v.dueTime.trim() && form ? { deadline: { offset_days: form.deadline_offset_days, time: v.dueTime.trim() } } : {}),
  };
}

export function CntsPublishScreen() {
  const { api, kn } = useAuth();
  const role = kn.context?.role ?? null;
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const aapi = useMemo(() => (api ? createAlertApi(api) : null), [api]);
  const forms = useLoad(capi ? () => capi.forms() : null, [capi]);
  const [filter, setFilter] = useState("");
  const [asking, setAsking] = useState<{ kind: "reopen" | "cancel"; periodId: string; label: string } | null>(null);
  const tasks = useLoad(capi ? () => capi.tasks(filter || undefined) : null, [capi, filter]);
  const periodIds = useMemo(() => [...new Set((tasks.data ?? []).map((t) => t.period_id))], [tasks.data]);
  const tracking = useLoad(capi && tasks.data ? () => capi.tracking(filter || undefined, periodIds) : null, [capi, filter, periodIds.join(",")]);
  const act = useAction();
  const rows = useMemo(() => periodRows(tasks.data ?? [], tracking.data), [tasks.data, tracking.data]);
  const [open, setOpen] = useState<string | null>(null);
  const name = useCommuneNames(rows.flatMap((r) => r.units.map((u) => u.parent)));

  if (!capi) return null;
  const reload = () => {
    tasks.reload();
    tracking.reload();
  };
  const run = async (f: () => Promise<unknown>, ok: string) => {
    const r = await act.run(f, ok);
    if (r) reload();
    return r;
  };
  const remind = (r: PeriodRow, units: string[] = []) =>
    void act.run(() => capi.remind(r.periodId, units), (x) => `Đã nhắc ${x.reminded} đơn vị${x.skipped.length ? `, ${x.skipped.length} đơn vị đã được nhắc trong giờ này` : ""}.`);
  const isOpen = (r: PeriodRow) => r.periodStatus === "open" || r.periodStatus === "reopened";

  return (
    <div className="page">
      <PageHeader title="Biểu kỳ Cấp Xã — Chăn nuôi, Thủy sản">
        <p className="mute">BM-01 KSGM hằng ngày, BM-04 nhập liệu tuần, BM-17 khai thác thủy sản, BM-05 công việc &amp; kế hoạch tuần (FN-07, UR-TINH-08).</p>
      </PageHeader>
      {act.view}

      <section className="card sec">
        <h2>Biểu mẫu</h2>
        <LoadState s={forms}>
          {(fs: CntsForm[]) => (
            <>
              <DataTable
                caption="Biểu mẫu"
                rows={fs}
                rowKey={(f) => f.code}
                columns={[
                  { key: "bm", header: "Biểu", cell: (f) => f.bm },
                  { key: "title", header: "Tên", cell: (f) => f.title },
                  { key: "unit", header: "Đơn vị nhập", cell: (f) => (f.unit_kind === "facility" ? "Cơ sở giết mổ" : "Xã/phường") },
                  { key: "period", header: "Kỳ", cell: (f) => (f.code.startsWith("CNTS-BM05") ? "Tuần làm việc" : f.period_type === "day" ? "Ngày" : "Tuần (Thứ Ba – Thứ Hai)") },
                  { key: "due", header: "Hạn mặc định", cell: (f) => f.due },
                  {
                    key: "ver",
                    header: "Phiên bản",
                    cell: (f) => (f.template_version ? <StatusPill tone={f.template_up_to_date ? "ok" : "warn"}>v{f.template_version}</StatusPill> : <StatusPill tone="bad">Chưa cài</StatusPill>),
                  },
                ]}
              />
              {role === "admin" ? (
                <button className="btn btn-primary" disabled={act.busy} onClick={async () => (await act.run(() => capi.install(), "Đã cài biểu kỳ CNTS.")) && forms.reload()}>
                  Cài / cập nhật biểu
                </button>
              ) : null}
            </>
          )}
        </LoadState>
      </section>

      {role === "kn_tinh" ? <PublishPanel capi={capi} aapi={aapi} forms={forms.data ?? []} onPublished={reload} /> : null}

      <section className="card sec">
        <h2>Theo dõi nộp</h2>
        <select className="select" aria-label="Lọc biểu" value={filter} onChange={(e) => setFilter(e.target.value)}>
          <option value="">Mọi biểu</option>
          {(forms.data ?? []).map((f) => (
            <option key={f.code} value={f.code}>
              {f.bm}
            </option>
          ))}
        </select>
        <LoadState s={tasks}>
          {() => (
            <DataTable
              caption="Kỳ đã phát hành"
              rows={rows}
              rowKey={(r) => r.periodId}
              empty="Chưa có kỳ nào."
              columns={[
                { key: "title", header: "Biểu", cell: (r) => r.title },
                { key: "label", header: "Kỳ", cell: (r) => r.label },
                { key: "due", header: "Hạn nộp", cell: (r) => dt(r.dueAt) },
                {
                  key: "close",
                  header: "Giờ chốt",
                  cell: (r) =>
                    r.closeAt ? (
                      <>
                        {dt(r.closeAt)}
                        {isOpen(r) && Date.parse(r.closeAt) <= Date.now() ? <StatusPill tone="warn">Đến giờ chốt</StatusPill> : null}
                      </>
                    ) : (
                      "—"
                    ),
                },
                { key: "owner", header: "Phụ trách", cell: (r) => r.ownerName ?? "—" },
                {
                  key: "st",
                  header: "Trạng thái",
                  cell: (r) => {
                    const [label, tone] = PERIOD_STATUS[r.periodStatus] ?? [r.periodStatus, "neutral"];
                    return <StatusPill tone={tone}>{label}</StatusPill>;
                  },
                },
                { key: "sub", header: "Đã nộp", cell: (r) => `${r.submitted}/${r.total}` },
                { key: "draft", header: "Nháp", cell: (r) => r.draft },
                { key: "ne", header: "Chưa nhập", cell: (r) => r.notEntered },
                { key: "late", header: "Trễ hạn", cell: (r) => r.overdue },
                { key: "ret", header: "Trả lại", cell: (r) => r.returned },
                { key: "miss", header: "Không nộp", cell: (r) => r.missing },
                {
                  key: "act",
                  header: "",
                  cell: (r) => (
                    <>
                      <button className="btn btn-sm" aria-expanded={open === r.periodId} onClick={() => setOpen(open === r.periodId ? null : r.periodId)}>
                        {open === r.periodId ? "Ẩn đơn vị" : "Đơn vị"}
                      </button>
                      {role === "kn_tinh" && isOpen(r) && r.pending > 0 ? (
                        <button className="btn btn-sm" disabled={act.busy} onClick={() => remind(r)}>
                          Nhắc ({r.pending})
                        </button>
                      ) : null}
                      {role === "kn_tinh" && isOpen(r) ? (
                        <button className="btn btn-sm" onClick={() => void run(() => capi.close(r.periodId), `Đã chốt ${r.label}.`)}>
                          Chốt kỳ
                        </button>
                      ) : null}
                      {role === "kn_tinh" && r.periodStatus === "closed" ? (
                        <button className="btn btn-sm" onClick={() => setAsking({ kind: "reopen", periodId: r.periodId, label: r.label })}>
                          Mở lại
                        </button>
                      ) : null}
                      {role === "kn_tinh" && r.periodStatus === "open" ? (
                        <button className="btn btn-sm" onClick={() => setAsking({ kind: "cancel", periodId: r.periodId, label: r.label })}>
                          Hủy
                        </button>
                      ) : null}
                    </>
                  ),
                },
              ]}
            />
          )}
        </LoadState>
        {open
          ? (() => {
              const r = rows.find((x) => x.periodId === open);
              if (!r) return null;
              return (
                <div className="card sec" aria-label={`Đơn vị — ${r.label}`}>
                  <h3>
                    {r.title} — {r.label}
                  </h3>
                  <DataTable
                    caption="Trạng thái từng đơn vị"
                    rows={r.units}
                    rowKey={(u) => u.unit}
                    columns={[
                      { key: "unit", header: "Đơn vị", cell: (u) => (u.unit === u.parent ? name(u.unit) : `${u.unit} · ${name(u.parent)}`) },
                      { key: "st", header: "Trạng thái", cell: (u) => <StatusPill tone={TRACK_TONE[u.state]}>{TRACK_LABEL[u.state]}</StatusPill> },
                      { key: "at", header: "Nộp lúc", cell: (u) => dt(u.submittedAt) },
                      { key: "note", header: "Lý do trả lại", cell: (u) => u.returnNote ?? "" },
                      {
                        key: "act",
                        header: "",
                        cell: (u) =>
                          role === "kn_tinh" && isOpen(r) && u.state !== "submitted" && u.state !== "missed" ? (
                            <button className="btn btn-sm" disabled={act.busy} onClick={() => remind(r, [u.unit])}>
                              Nhắc
                            </button>
                          ) : null,
                      },
                    ]}
                  />
                </div>
              );
            })()
          : null}
      </section>
      {asking ? (
        <ReasonDialog
          title={`${asking.kind === "reopen" ? "Mở lại" : "Hủy"} ${asking.label}`}
          label={asking.kind === "reopen" ? "Lý do mở lại kỳ" : "Lý do hủy kỳ"}
          confirmLabel={asking.kind === "reopen" ? "Mở lại" : "Hủy kỳ"}
          busy={act.busy}
          onClose={() => setAsking(null)}
          onConfirm={(why) =>
            void run(
              () => (asking.kind === "reopen" ? capi.reopen(asking.periodId, why) : capi.cancel(asking.periodId, why)),
              `Đã ${asking.kind === "reopen" ? "mở lại" : "hủy"} ${asking.label}.`,
            ).then((r) => r && setAsking(null))
          }
        />
      ) : null}
    </div>
  );
}

type Capi = ReturnType<typeof createCntsPeriodApi>;
type Aapi = ReturnType<typeof createAlertApi> | null;

/** Cấu hình phát hành theo bước (CN-07): biểu → đối tượng → kỳ, hạn, giờ chốt, người phụ trách → xem trước → phát hành. */
function PublishPanel({ capi, aapi, forms, onPublished }: { capi: Capi; aapi: Aapi; forms: CntsForm[]; onPublished: () => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [code, setCode] = useState("CNTS-BM04");
  const form = forms.find((f) => f.code === code);
  const facilityMode = form?.unit_kind === "facility";
  const [v, setV] = useState<PublishDraft>({ from: vnToday(), to: vnToday(), target: "province", units: [], regions: [], dueTime: "", owner: "", closeAfter: "" });
  const [errs, setErrs] = useState<PublishErrors>({});
  const [preview, setPreview] = useState<CntsSeries | null>(null);
  const [q, setQ] = useState("");
  const target: CntsTarget = facilityMode ? "facilities" : v.target === "facilities" ? "province" : v.target;
  const draft: PublishDraft = { ...v, target };
  const set = (patch: Partial<PublishDraft>) => {
    setV((x) => ({ ...x, ...patch }));
    setPreview(null);
  };
  const facilities = useLoad(
    api ? async () => ((await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_slaughter_facility" }, query: {} } }))).items as CatalogItem[]) : null,
    [api],
  );
  const owners = useLoad(aapi ? async () => (await aapi.assignees()).filter((o) => o.level === "province") : null, [aapi]);
  // Xã của tỉnh kèm khu vực: lấy từ Xem trước "Tất cả xã" của biểu xã (như phát hành TT) — không gõ mã xã thô.
  const scope = useLoad(form && !facilityMode ? () => capi.preview({ code, from_date: vnToday(), to_date: vnToday(), target: "province", units: [] }) : null, [capi, code, facilityMode, !!form]);
  const communes = scope.data?.communes ?? [];
  const name = useCommuneNames([...communes.map((c) => c.commune_code), ...(preview?.units ?? [])]);
  const regionList = [...new Map(communes.filter((c) => c.region_code).map((c) => [c.region_code, c.region_name])).entries()];
  const shown = communes.filter((c) => !q || `${c.commune_code} ${name(c.commune_code)} ${c.region_name}`.toLowerCase().includes(q.toLowerCase()));
  const activeFacilities = (facilities.data ?? []).filter((f) => f.active);
  const check = () => {
    const e = publishErrors(draft);
    setErrs(e);
    return Object.keys(e).length === 0;
  };
  const body = () => seriesBody(code, draft, form);
  const toggle = (list: "units" | "regions", x: string, on: boolean) => set({ [list]: on ? [...v[list], x] : v[list].filter((y) => y !== x) } as Partial<PublishDraft>);

  return (
    <section className="card sec" aria-label="Phát hành kỳ">
      <h2>Phát hành kỳ</h2>
      {act.view}
      <fieldset className="field">
        <legend>Bước 1 — Biểu</legend>
        <label className="field">
          <span>Biểu</span>
          <select className="select" aria-label="Biểu phát hành" value={code} onChange={(e) => (setCode(e.target.value), set({ units: [], regions: [] }))}>
            {forms.map((f) => (
              <option key={f.code} value={f.code}>
                {f.bm} — {f.title}
              </option>
            ))}
          </select>
        </label>
      </fieldset>

      <fieldset className="field">
        <legend>
          Bước 2 — Đối tượng<span className="req" aria-hidden="true"> *</span>
        </legend>
        {facilityMode ? (
          <>
            <p className="small mute">BM-01 nhập theo cơ sở giết mổ (danh mục CNTS — cơ sở phải thuộc xã).</p>
            <label className="row small">
              <input
                type="checkbox"
                checked={activeFacilities.length > 0 && activeFacilities.every((f) => v.units.includes(f.attrs.facility_ref ?? ""))}
                onChange={(e) => set({ units: e.target.checked ? activeFacilities.map((f) => f.attrs.facility_ref ?? "").filter(Boolean) : [] })}
              />{" "}
              Chọn tất cả cơ sở đang hoạt động ({activeFacilities.length})
            </label>
            {activeFacilities.map((f) => {
              const ref = f.attrs.facility_ref ?? "";
              return (
                <label key={f.code} className="row">
                  <input type="checkbox" checked={v.units.includes(ref)} onChange={(e) => toggle("units", ref, e.target.checked)} />
                  {f.name} · xã {f.attrs.commune_code}
                </label>
              );
            })}
          </>
        ) : (
          <>
            <div className="row" role="radiogroup" aria-label="Đối tượng">
              {COMMUNE_TARGETS.map((t) => (
                <label key={t} className="row small">
                  <input type="radio" name="cnts-target" checked={target === t} onChange={() => set({ target: t })} /> {TARGET_LABEL[t]}
                </label>
              ))}
            </div>
            <p className="small mute">
              Xã có nuôi thủy sản: xã có điểm ghi nhật ký thủy sản BM-19; xã có mô hình: xã có điểm trình diễn đang hoạt động. Hộ mô hình nhận nhật ký BM-08/09/19 ở mục Nhật
              ký — biểu kỳ không giao theo hộ.
            </p>
            <LoadState s={scope}>
              {() => (
                <>
                  {target === "province" ? <p className="small mute">Tất cả: {communes.length} xã/phường.</p> : null}
                  {target === "regions" ? (
                    <div>
                      {regionList.map(([rc, rn]) => (
                        <label key={rc} className="row small">
                          <input type="checkbox" checked={v.regions.includes(rc)} onChange={(e) => toggle("regions", rc, e.target.checked)} /> {rn} (
                          {communes.filter((c) => c.region_code === rc).length} xã)
                        </label>
                      ))}
                      {regionList.length === 0 ? <p className="small mute">Tỉnh chưa gán khu vực cho xã.</p> : null}
                    </div>
                  ) : null}
                  {target === "communes" ? (
                    <div>
                      <SearchBox value={q} onChange={setQ} placeholder="Tìm xã / khu vực" />
                      {shown.map((c) => (
                        <label key={c.commune_code} className="row small">
                          <input type="checkbox" checked={v.units.includes(c.commune_code)} onChange={(e) => toggle("units", c.commune_code, e.target.checked)} />{" "}
                          {name(c.commune_code)} · {c.region_name || "Chưa gán khu vực"}
                        </label>
                      ))}
                    </div>
                  ) : null}
                </>
              )}
            </LoadState>
          </>
        )}
        {errs.units || errs.regions ? (
          <span className="err" role="alert">
            {errs.units ?? errs.regions}
          </span>
        ) : null}
      </fieldset>

      <fieldset className="field">
        <legend>Bước 3 — Kỳ, hạn nộp, giờ chốt, người phụ trách</legend>
        <div className="ws-two">
          <DateField label="Từ ngày" value={v.from} onChange={(x) => set({ from: x })} error={errs.from} />
          <DateField label="Đến ngày" value={v.to} onChange={(x) => set({ to: x })} error={errs.to} />
          <TextField label="Giờ hạn nộp" placeholder="HH:MM" value={v.dueTime} onChange={(x) => set({ dueTime: x })} error={errs.dueTime}
            hint={`${form?.due ?? ""}${form ? ` — để trống = ${form.deadline_time}` : ""}`} />
          <NumberField label="Giờ chốt sau hạn nộp" unit="giờ" rule={CLOSE_RULE} value={v.closeAfter} onChange={(x) => set({ closeAfter: x })} error={errs.closeAfter}
            hint="0–720; để trống = Cấp Tỉnh tự chốt" />
          <SelectField
            label="Cán bộ tỉnh phụ trách biểu"
            required
            value={v.owner}
            onChange={(x) => set({ owner: x })}
            error={errs.owner}
            options={(owners.data ?? []).map((o) => ({ value: o.actor.ref, label: o.actor.display ?? o.actor.ref }))}
            hint="Nhận cảnh báo trễ nộp liên tiếp của biểu"
          />
        </div>
        <p className="small mute">Kỳ theo lịch của biểu: BM-04/BM-17 tuần Thứ Ba – Thứ Hai; BM-01 từng ngày; BM-05 tuần làm việc Thứ Hai – Thứ Sáu.</p>
      </fieldset>

      <div className="row">
        <button className="btn" disabled={act.busy} onClick={async () => check() && setPreview(await act.run(() => capi.preview(body())))}>
          Bước 4 — Xem trước
        </button>
        <button
          className="btn btn-primary"
          disabled={act.busy || !preview}
          onClick={async () => {
            if (!check()) return;
            const r = await act.run(() => capi.publish(body()), (x) => `Đã phát hành ${x.periods.length} kỳ, ${x.tasks} nhiệm vụ.`);
            if (r) {
              setPreview(null);
              onPublished();
            }
          }}
        >
          Phát hành
        </button>
      </div>
      {preview ? (
        <>
          <p>
            <strong>
              {preview.periods.length} kỳ × {preview.units.length} đơn vị = {preview.tasks} nhiệm vụ
            </strong>
            {preview.calendar_id ? ` — lịch tuần ${preview.calendar_id} v${preview.calendar_version}` : " — tuần làm việc Thứ Hai – Thứ Sáu"}
          </p>
          <p className="small mute">
            Đối tượng: {TARGET_LABEL[preview.target]}
            {preview.unit_kind === "commune" ? ` — ${preview.units.map(name).join(", ")}` : ""}
          </p>
          <DataTable
            caption="Kỳ xem trước"
            rows={preview.periods}
            rowKey={(p) => p.key}
            columns={[
              { key: "label", header: "Kỳ", cell: (p) => p.label },
              { key: "range", header: "Từ – đến", cell: (p) => `${p.from_date} – ${p.to_date}` },
              { key: "due", header: "Hạn nộp", cell: (p) => dt(p.due_at) },
              {
                key: "close",
                header: "Giờ chốt",
                cell: (p) => (preview.close_after_hours != null ? dt(new Date(Date.parse(p.due_at) + preview.close_after_hours * 3_600_000).toISOString()) : "—"),
              },
            ]}
          />
        </>
      ) : null}
    </section>
  );
}

/** Ô ngày bắt buộc có lỗi theo trường (ô `type="date"` gốc, nhãn như UI-KIT). */
function DateField({ label, value, onChange, error }: { label: string; value: string; onChange: (v: string) => void; error?: string }) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        <span className="req" aria-hidden="true">
          {" "}*
        </span>
      </label>
      <input id={id} className="input" type="date" aria-required aria-invalid={error ? true : undefined} value={value} onChange={(e) => onChange(e.target.value)} />
      {error ? (
        <span className="err" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
