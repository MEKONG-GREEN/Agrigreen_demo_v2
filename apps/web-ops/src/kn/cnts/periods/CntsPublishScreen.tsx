"use client";

// Biểu kỳ Cấp Xã CNTS — Cấp Tỉnh / Admin (W11.42, BRD CNTS v3.7 UR-TINH-07/08, D4 CN2): biểu BM-01/04/05/17 (cài vào
// cap/forms — Admin), phát hành lô kỳ (kỳ ngày BM-01 cho cơ sở giết mổ; tuần Thứ Tư – Thứ Ba BM-04/BM-17; tuần làm việc
// BM-05) với hạn riêng từng biểu, theo dõi nộp theo kỳ, chốt / mở lại / hủy kỳ.
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
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
    ...(dueTime && form ? { deadline: { offset_days: form.deadline_offset_days, time: dueTime } } : {}),
  });
  const rows = useMemo(() => periodRows(tasks.data ?? []), [tasks.data]);

  if (!capi) return null;
  const reason = (what: string) => {
    const r = window.prompt(`Lý do ${what} (10–500 ký tự)`);
    return r && r.trim().length >= 10 ? r.trim() : null;
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
              <table className="dt">
                <thead>
                  <tr>
                    <th>Biểu</th>
                    <th>Tên</th>
                    <th>Đơn vị nhập</th>
                    <th>Kỳ</th>
                    <th>Hạn mặc định</th>
                    <th>Phiên bản</th>
                  </tr>
                </thead>
                <tbody>
                  {fs.map((f) => (
                    <tr key={f.code}>
                      <td>{f.bm}</td>
                      <td>{f.title}</td>
                      <td>{f.unit_kind === "facility" ? "Cơ sở giết mổ" : "Xã/phường"}</td>
                      <td>{f.code.startsWith("CNTS-BM05") ? "Tuần làm việc" : f.period_type === "day" ? "Ngày" : "Tuần (Thứ Tư – Thứ Ba)"}</td>
                      <td>{f.due}</td>
                      <td>{f.template_version ? <StatusPill tone={f.template_up_to_date ? "ok" : "warn"}>v{f.template_version}</StatusPill> : <StatusPill tone="bad">Chưa cài</StatusPill>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
            <label className="field">
              <span>Giờ hạn nộp (để trống = {form?.deadline_time ?? "mặc định"})</span>
              <input className="input" aria-label="Giờ hạn nộp" placeholder="HH:MM" value={dueTime} onChange={(e) => setDueTime(e.target.value)} />
            </label>
            <label className="field">
              <span>Từ ngày</span>
              <input className="input" type="date" aria-label="Từ ngày" value={from} onChange={(e) => setFrom(e.target.value)} />
            </label>
            <label className="field">
              <span>Đến ngày</span>
              <input className="input" type="date" aria-label="Đến ngày" value={to} onChange={(e) => setTo(e.target.value)} />
            </label>
          </div>
          {facilityMode ? (
            <fieldset className="field">
              <legend>Cơ sở giết mổ nhận (danh mục CNTS — cơ sở phải thuộc xã)</legend>
              {(facilities.data ?? []).filter((f) => f.active).map((f) => {
                const ref = f.attrs.facility_ref ?? "";
                return (
                  <label key={f.code} className="row">
                    <input type="checkbox" checked={picked.includes(ref)} onChange={(e) => setPicked((p) => (e.target.checked ? [...p, ref] : p.filter((x) => x !== ref)))} />
                    {f.name} · xã {f.attrs.commune_code}
                  </label>
                );
              })}
            </fieldset>
          ) : (
            <label className="field">
              <span>Mã xã nhận (cách nhau dấu phẩy)</span>
              <input className="input" aria-label="Mã xã nhận" value={unitsText} onChange={(e) => setUnitsText(e.target.value)} />
              {units.length ? <span className="mute">{units.map(name).join(", ")}</span> : null}
            </label>
          )}
          <div className="row">
            <button className="btn" disabled={act.busy} onClick={async () => setPreview(await act.run(() => capi.preview(body())))}>
              Xem trước
            </button>
            <button
              className="btn btn-primary"
              disabled={act.busy || !preview}
              onClick={async () => {
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
              </p>
              <table className="dt" aria-label="Kỳ xem trước">
                <thead>
                  <tr>
                    <th>Kỳ</th>
                    <th>Từ – đến</th>
                    <th>Hạn nộp</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.periods.map((p) => (
                    <tr key={p.key}>
                      <td>{p.label}</td>
                      <td>
                        {p.from_date} – {p.to_date}
                      </td>
                      <td>{dt(p.due_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
            <table className="dt" aria-label="Kỳ đã phát hành">
              <thead>
                <tr>
                  <th>Biểu</th>
                  <th>Kỳ</th>
                  <th>Hạn nộp</th>
                  <th>Trạng thái</th>
                  <th>Đã nộp</th>
                  <th>Trễ hạn</th>
                  <th>Không nộp</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const [label, tone] = PERIOD_STATUS[r.periodStatus] ?? [r.periodStatus, "neutral"];
                  const run = async (f: () => Promise<unknown>, ok: string) => (await act.run(f, ok)) && tasks.reload();
                  return (
                    <tr key={r.periodId}>
                      <td>{r.title}</td>
                      <td>{r.label}</td>
                      <td>{dt(r.dueAt)}</td>
                      <td>
                        <StatusPill tone={tone}>{label}</StatusPill>
                      </td>
                      <td>
                        {r.submitted}/{r.total}
                      </td>
                      <td>{r.overdue}</td>
                      <td>{r.missing}</td>
                      <td>
                        {role === "kn_tinh" && (r.periodStatus === "open" || r.periodStatus === "reopened") ? (
                          <button className="btn btn-sm" onClick={() => run(() => capi.close(r.periodId), `Đã chốt ${r.label}.`)}>
                            Chốt kỳ
                          </button>
                        ) : null}
                        {role === "kn_tinh" && r.periodStatus === "closed" ? (
                          <button
                            className="btn btn-sm"
                            onClick={() => {
                              const why = reason("mở lại kỳ");
                              if (why) void run(() => capi.reopen(r.periodId, why), `Đã mở lại ${r.label}.`);
                            }}
                          >
                            Mở lại
                          </button>
                        ) : null}
                        {role === "kn_tinh" && r.periodStatus === "open" ? (
                          <button
                            className="btn btn-sm"
                            onClick={() => {
                              const why = reason("hủy kỳ");
                              if (why) void run(() => capi.cancel(r.periodId, why), `Đã hủy ${r.label}.`);
                            }}
                          >
                            Hủy
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
                {rows.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="mute">
                      Chưa có kỳ nào.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          )}
        </LoadState>
      </section>
    </div>
  );
}
