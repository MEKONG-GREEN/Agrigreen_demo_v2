"use client";

// Biểu kỳ Cấp Xã CNTS — Cấp Tỉnh / Admin (W11.42, BRD CNTS v3.7 UR-TINH-07/08, D4 CN2): biểu BM-01/04/05/17 (cài vào
// cap/forms — Admin), phát hành lô kỳ (kỳ ngày BM-01 cho cơ sở giết mổ; tuần Thứ Ba – Thứ Hai BM-04/BM-17 theo lịch CNTS D8; tuần làm việc
// BM-05) với hạn riêng từng biểu, theo dõi nộp theo kỳ, chốt / mở lại / hủy kỳ.
// BF-SWEEP-CNTS (#101): bảng có STT (DataTable), trường bắt buộc báo lỗi theo trường trước khi gọi API, lý do mở lại / hủy
// kỳ nhập trong pop-up ngắn thay `window.prompt`. Cấu hình phát hành đầy đủ (người phụ trách, nhóm xã…) — TODO(#108).
import { useId, useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { ReasonDialog } from "../ReasonDialog";
import { createCntsPeriodApi, periodRows, type CntsForm, type CntsSeries } from "./api";

const PERIOD_STATUS: Record<string, [string, "neutral" | "ok" | "warn" | "bad" | "info"]> = {
  open: ["Đang mở", "info"],
  reopened: ["Mở lại", "warn"],
  closed: ["Đã chốt", "ok"],
  cancelled: ["Đã hủy", "bad"],
};

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

type PublishErrors = Partial<Record<"from" | "to" | "units" | "dueTime", string>>;

/** Kiểm biểu phát hành trước khi xem trước / phát hành (R-A4; máy chủ kiểm lại). */
export function publishErrors(v: { from: string; to: string; units: string[]; dueTime: string }): PublishErrors {
  const e: PublishErrors = {};
  if (!v.from) e.from = "Bắt buộc nhập.";
  if (!v.to) e.to = "Bắt buộc nhập.";
  else if (v.from && v.to < v.from) e.to = "Đến ngày không được trước Từ ngày.";
  if (v.units.length === 0) e.units = "Chọn ít nhất một đơn vị nhận.";
  if (v.dueTime.trim() && !/^([01]\d|2[0-3]):[0-5]\d$/.test(v.dueTime.trim())) e.dueTime = "Giờ dạng HH:MM, vd 16:00.";
  return e;
}

export function CntsPublishScreen() {
  const { api, kn } = useAuth();
  const role = kn.context?.role ?? null;
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const forms = useLoad(capi ? () => capi.forms() : null, [capi]);
  const facilities = useLoad(
    api ? async () => ((await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_slaughter_facility" }, query: {} } }))).items as CatalogItem[]) : null,
    [api],
  );
  const [code, setCode] = useState("CNTS-BM04");
  const [from, setFrom] = useState(vnToday());
  const [to, setTo] = useState(vnToday());
  const [unitsText, setUnitsText] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [dueTime, setDueTime] = useState("");
  const [preview, setPreview] = useState<CntsSeries | null>(null);
  const [filter, setFilter] = useState("");
  const [errs, setErrs] = useState<PublishErrors>({});
  const [asking, setAsking] = useState<{ kind: "reopen" | "cancel"; periodId: string; label: string } | null>(null);
  const tasks = useLoad(capi ? () => capi.tasks(filter || undefined) : null, [capi, filter]);
  const act = useAction();
  const form = forms.data?.find((f) => f.code === code);
  const facilityMode = form?.unit_kind === "facility";
  const units = facilityMode ? picked : unitsText.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean);
  const name = useCommuneNames(units);
  const body = () => ({
    code,
    from_date: from,
    to_date: to,
    units,
    ...(dueTime.trim() && form ? { deadline: { offset_days: form.deadline_offset_days, time: dueTime.trim() } } : {}),
  });
  const check = () => {
    const e = publishErrors({ from, to, units, dueTime });
    setErrs(e);
    return Object.keys(e).length === 0;
  };
  const rows = useMemo(() => periodRows(tasks.data ?? []), [tasks.data]);

  if (!capi) return null;
  const run = async (f: () => Promise<unknown>, ok: string) => {
    const r = await act.run(f, ok);
    if (r) tasks.reload();
    return r;
  };

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

      {role === "kn_tinh" ? (
        <section className="card sec">
          <h2>Phát hành kỳ</h2>
          <div className="ws-two">
            <label className="field">
              <span>Biểu</span>
              <select className="select" aria-label="Biểu phát hành" value={code} onChange={(e) => { setCode(e.target.value); setPreview(null); }}>
                {(forms.data ?? []).map((f) => (
                  <option key={f.code} value={f.code}>
                    {f.bm} — {f.title}
                  </option>
                ))}
              </select>
            </label>
            <TextField label="Giờ hạn nộp" placeholder="HH:MM" value={dueTime} onChange={setDueTime} error={errs.dueTime} hint={`Để trống = ${form?.deadline_time ?? "mặc định"}`} />
            <DateField label="Từ ngày" value={from} onChange={setFrom} error={errs.from} />
            <DateField label="Đến ngày" value={to} onChange={setTo} error={errs.to} />
          </div>
          {facilityMode ? (
            <fieldset className="field">
              <legend>
                Cơ sở giết mổ nhận (danh mục CNTS — cơ sở phải thuộc xã)<span className="req" aria-hidden="true"> *</span>
              </legend>
              {(facilities.data ?? []).filter((f) => f.active).map((f) => {
                const ref = f.attrs.facility_ref ?? "";
                return (
                  <label key={f.code} className="row">
                    <input type="checkbox" checked={picked.includes(ref)} onChange={(e) => setPicked((p) => (e.target.checked ? [...p, ref] : p.filter((x) => x !== ref)))} />
                    {f.name} · xã {f.attrs.commune_code}
                  </label>
                );
              })}
              {errs.units ? (
                <span className="err" role="alert">
                  {errs.units}
                </span>
              ) : null}
            </fieldset>
          ) : (
            // TODO(#108): chọn nhóm xã (tất cả / khu vực / có nuôi thủy sản / có mô hình) thay ô nhập mã.
            <TextField
              label="Mã xã nhận (cách nhau dấu phẩy)"
              required
              value={unitsText}
              onChange={setUnitsText}
              error={errs.units}
              hint={units.length ? units.map(name).join(", ") : undefined}
            />
          )}
          <div className="row">
            <button className="btn" disabled={act.busy} onClick={async () => check() && setPreview(await act.run(() => capi.preview(body())))}>
              Xem trước
            </button>
            <button
              className="btn btn-primary"
              disabled={act.busy || !preview}
              onClick={async () => {
                if (!check()) return;
                const r = await act.run(() => capi.publish(body()), (x) => `Đã phát hành ${x.periods.length} kỳ, ${x.tasks} công việc.`);
                if (r) {
                  setPreview(null);
                  tasks.reload();
                }
              }}
            >
              Phát hành
            </button>
          </div>
          {preview ? (
            <>
              <p className="mute">
                {preview.periods.length} kỳ × {preview.units.length} đơn vị = {preview.tasks} công việc
                {preview.calendar_id ? ` — lịch tuần ${preview.calendar_id} v${preview.calendar_version}` : " — tuần làm việc Thứ Hai – Thứ Sáu"}
              </p>
              <DataTable
                caption="Kỳ xem trước"
                rows={preview.periods}
                rowKey={(p) => p.key}
                columns={[
                  { key: "label", header: "Kỳ", cell: (p) => p.label },
                  { key: "range", header: "Từ – đến", cell: (p) => `${p.from_date} – ${p.to_date}` },
                  { key: "due", header: "Hạn nộp", cell: (p) => dt(p.due_at) },
                ]}
              />
            </>
          ) : null}
        </section>
      ) : null}

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
                  key: "st",
                  header: "Trạng thái",
                  cell: (r) => {
                    const [label, tone] = PERIOD_STATUS[r.periodStatus] ?? [r.periodStatus, "neutral"];
                    return <StatusPill tone={tone}>{label}</StatusPill>;
                  },
                },
                { key: "sub", header: "Đã nộp", cell: (r) => `${r.submitted}/${r.total}` },
                { key: "late", header: "Trễ hạn", cell: (r) => r.overdue },
                { key: "miss", header: "Không nộp", cell: (r) => r.missing },
                {
                  key: "act",
                  header: "",
                  cell: (r) => (
                    <>
                      {role === "kn_tinh" && (r.periodStatus === "open" || r.periodStatus === "reopened") ? (
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
