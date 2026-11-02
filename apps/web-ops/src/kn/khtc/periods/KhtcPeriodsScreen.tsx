"use client";

// Kỳ báo cáo & phiếu các phòng — BF-SWEEP-KHTC-REST: route riêng cho FN-TI-02.03; giữ API và lịch W11.62.
// (ô chọn Loại / Năm / Tuần·Tháng·Quý, hệ thống tự điền khoảng thời gian, tên kỳ, hạn; số lần gửi tối đa chỉ đọc), chi
// tiết kỳ (4 thẻ đếm, tiến độ phiếu các phòng và nguồn module, log), Sửa hạn nộp, Đóng kỳ, Hủy kỳ. Phòng nộp phiếu ở
// module của phòng (/v1/forms, profile kn_khtc).
import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { periodDateISO, validatePeriodForm } from "./form";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, RowActions, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import {
  FORM_TONE,
  PERIOD_TONE,
  REPORT_TYPES,
  createKhtcPeriodApi,
  numberChoices,
  progressText,
  scopeChoices,
  vnDate,
  type KhtcDraft,
  type KhtcOpenInput,
  type KhtcPeriod,
  type KhtcReportType,
} from "./api";
import { KhtcSourcesPanel } from "./KhtcSourcesPanel";
import { DetailPage, LogPanel, type LogEntry } from "@/components/patterns/detail";
import type { ReactNode } from "react";

export const PERIOD_BASE = "/kn/khtc/ky-bao-cao";
export type PeriodRoute = { view: "list" | "new" | "detail" | "deadline"; id?: string; go: (path: string) => void };
export const periodHref = (id: string) => `${PERIOD_BASE}/ky/${encodeURIComponent(id)}`;
export function periodLog(p: KhtcPeriod): LogEntry[] {
  return [{ event: `Mở kỳ và giao ${p.departments.length} phiếu`, actor_display: p.opened_by, at: p.opened_at },
    ...p.events.map((e) => ({ event: e.action === "deadline_changed" ? `Sửa hạn nộp → ${dt(e.due_at)}` : e.action === "closed" ? e.forced ? "Đóng kỳ (ngoại lệ)" : "Đóng kỳ" : "Hủy kỳ", actor_display: e.actor_ref, at: e.at, reason: e.reason }))];
}
function PeriodPage({ title, children, log, back = PERIOD_BASE }: { title: string; children: ReactNode; log?: ReactNode; back?: string }) {
  return <DetailPage title={title} back={{ href: back, label: "Danh sách kỳ báo cáo" }} log={log}>{children}</DetailPage>;
}

const KHTC = "ke_hoach_tai_chinh";

/** ISO → giá trị ô datetime-local theo giờ Việt Nam. */
function toLocal(iso: string | null | undefined): string {
  return iso
    ? new Date(Date.parse(iso) + 7 * 3_600_000).toISOString().slice(0, 16)
    : "";
}
/** Ô datetime-local (giờ Việt Nam) → ISO. */
function fromLocal(v: string): string | undefined {
  return v ? periodDateISO(v) ?? undefined : undefined;
}

export function KhtcPeriodsScreen({adhocOnly=false,onOpened,initialPeriod=null,route}:{adhocOnly?:boolean;onOpened?:()=>void;initialPeriod?:string|null;route?:PeriodRoute}={}) {
  const { api, kn } = useAuth();
  const ctx = kn.context;
  const role = ctx?.role ?? null;
  const writer =
    !ctx?.read_only && (role==="kn_tinh"&&ctx?.department_code===KHTC || adhocOnly&&role==="admin");
  const kapi = useMemo(() => (api ? createKhtcPeriodApi(api) : null), [api]);
  const [thisYear] = useState(() => new Date(Date.now() + 7 * 3_600_000).getUTCFullYear());
  const [year, setYear] = useState(thisYear);
  const periods = useLoad(kapi ? () => kapi.list(year) : null, [kapi, year]);
  const template = useLoad(kapi ? () => kapi.template() : null, [kapi]);
  const weeks = useLoad(kapi ? () => kapi.weeks(year) : null, [kapi, year]);
  const [localSelected, setLocalSelected] = useState<string | null>(initialPeriod);
  const selected = route ? route.id ?? null : localSelected;
  const setSelected = (id: string) => route ? route.go(periodHref(id)) : setLocalSelected(id);
  const detail = useLoad(kapi && selected ? () => kapi.get(selected) : null, [
    kapi,
    selected,
    route?.view,
  ]);
  const act = useAction();
  const routeEpoch = useRef({ active: true });
  useEffect(() => {
    const epoch = { active: true };
    routeEpoch.current = epoch;
    return () => { epoch.active = false; };
  }, [route?.view, route?.id]);

  // Nháp mở kỳ được layout giữ trong bộ nhớ giữa các route cùng ngữ cảnh.
  const [localOpening, setLocalOpening] = useState(false);
  const opening = route ? route.view === "new" : localOpening;
  const setOpening = (value: boolean) => route ? route.go(value ? `${PERIOD_BASE}/them-moi` : PERIOD_BASE) : setLocalOpening(value);
  const [type, setType] = useState<KhtcReportType>(adhocOnly?"adhoc":"week");
  const [no, setNo] = useState(1);
  const [scope, setScope] = useState<"quarter" | "half" | "nine_months">(
    "quarter",
  );
  const [asOf, setAsOf] = useState("");
  const [name, setName] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [cutoff, setCutoff] = useState("");
  const [due, setDue] = useState("");
  const [exportDue, setExportDue] = useState("");
  const [depts, setDepts] = useState<string[] | null>(null);
  const [draft, setDraft] = useState<KhtcDraft | null>(null);

  const [errors, setErrors] = useState<Record<string, string>>({});
  const serverErrors = act.problem ? fieldErrorMap(act.problem) : {};
  const error = (key: string) => errors[key] || serverErrors[({ asOf: "as_of_date", from: "from_date", to: "to_date", cutoff: "cutoff_date", due: "due_at", exportDue: "export_due_at" } as Record<string, string>)[key] ?? key];
  const fieldError = (key: string) => error(key) ? <span className="err" role="alert">{error(key)}</span> : null;
  const validate = () => {
    const next = validatePeriodForm(type, { name, asOf, from, to, cutoff, due, exportDue });
    setErrors(next);
    return Object.keys(next).length === 0;
  };
  if (!kapi) return null;
  const body = (): KhtcOpenInput => ({
    report_type: type,
    year,
    ...(type === "week" || type === "month" || type === "quarter"
      ? { no }
      : {}),
    ...(type === "quarter" ? { scope } : {}),
    ...(type === "program" ? { as_of_date: asOf } : {}),
    ...(type === "adhoc" ? { name, from_date: from, to_date: to } : {}),
    ...(cutoff ? { cutoff_date: cutoff } : {}),
    ...(due ? { due_at: fromLocal(due) } : {}),
    ...(exportDue ? { export_due_at: fromLocal(exportDue) } : {}),
    ...(depts ? { departments: depts } : {}),
  });
  const reset = () => {
    setDraft(null);
    setErrors({});
    setDepts(null);
    setCutoff("");
    setDue("");
    setExportDue("");
  };
  const discard = () => {
    reset(); setType(adhocOnly ? "adhoc" : "week"); setNo(1); setScope("quarter");
    setAsOf(""); setName(""); setFrom(""); setTo(""); act.resetIdempotency();
  };
  const reason = (what: string, required = true) => {
    const r = window.prompt(
      `Lý do ${what} (10–500 ký tự${required ? "" : ", có thể bỏ trống"})`,
    );
    if (r === null) return null;
    if (!required && !r.trim()) return "";
    return r.trim().length >= 10 ? r.trim() : null;
  };
  const choices = numberChoices(type, weeks.data ?? []);

  return (
    <div className="page">
      {!route || route.view === "list" ? <PageHeader title={adhocOnly?"Mở kỳ phát sinh":"Kỳ báo cáo & phiếu các phòng"}>
        <p className="mute">
          Mở kỳ bằng ô chọn, hệ thống tự điền khoảng thời gian, tên kỳ và hạn
          (tuần báo cáo Thứ Tư – Thứ Ba). Mỗi kỳ, mỗi phòng một phiếu “Báo cáo
          cho KHTC”.
        </p>
      </PageHeader> : null}
      {act.view}

      {!route || route.view === "list" ? <section className="card sec">
        <h2>Phiếu cơ sở</h2>
        <LoadState s={template}>
          {(t) => (
            <p>
              {t.installed ? (
                <StatusPill tone="ok">{`${t.code} v${t.version}`}</StatusPill>
              ) : (
                <StatusPill tone="bad">Chưa cài phiếu cơ sở</StatusPill>
              )}{" "}
              {!t.installed && role === "admin" ? (
                <button
                  className="btn"
                  disabled={act.busy}
                  onClick={async () =>
                    (await act.run(
                      () => kapi.install(),
                      "Đã cài phiếu cơ sở.",
                    )) && template.reload()
                  }
                >
                  Cài phiếu cơ sở
                </button>
              ) : null}
              {!t.installed && role !== "admin" ? (
                <span className="mute">
                  Admin cài phiếu cơ sở trước khi mở kỳ.
                </span>
              ) : null}
            </p>
          )}
        </LoadState>
      </section> : null}

      {!route || route.view === "list" ? <section className="card sec">
        <div className="row">
          <h2>Danh sách kỳ</h2>
          <label>
            Năm{" "}
            <select
              value={year}
              onChange={(e) => setYear(Number(e.target.value))}
              aria-label="Năm"
            >
              {[thisYear - 1, thisYear, thisYear + 1].map((y) => (
                <option key={y} value={y}>
                  {y}
                </option>
              ))}
            </select>
          </label>
          {writer ? (
            <button
              className="btn btn-primary"
              onClick={() => setOpening(true)}
            >
              Mở kỳ
            </button>
          ) : null}
        </div>
        <LoadState s={periods}>
          {(ps: KhtcPeriod[]) => <DataTable caption="Danh sách kỳ báo cáo" rows={ps.filter(p => !adhocOnly || p.report_type === "adhoc")} rowKey={p => p.id}
            empty={`Chưa có kỳ nào trong năm ${year}.`} columns={[
              { key: "type", header: "Loại báo cáo", cell: p => REPORT_TYPES.find(t => t.value === p.report_type)?.label ?? p.report_type },
              { key: "name", header: "Kỳ", cell: p => route ? <Link href={periodHref(p.id)}>{p.name}</Link> : <button className="btn btn-o" aria-label={`Xem kỳ ${p.name}`} onClick={() => setSelected(p.id)}>{p.name}</button> },
              { key: "cutoff", header: "Ngày chốt", cell: p => vnDate(p.cutoff_date) },
              { key: "due", header: "Hạn nộp", cell: p => dt(p.due_at) },
              { key: "departments", header: "Phòng tham gia", cell: p => p.departments.length },
              { key: "progress", header: "Tiến độ nộp", cell: p => progressText(p) },
              { key: "state", header: "Trạng thái", cell: p => <StatusPill tone={PERIOD_TONE[p.state]}>{p.state_label}</StatusPill> },
              ...(route ? [{ key: "actions", header: "Thao tác", cell: (p: KhtcPeriod) => <RowActions label={p.name} viewHref={periodHref(p.id)} editHref={writer && p.state !== "closed" && p.state !== "cancelled" ? `${periodHref(p.id)}/sua-han` : undefined} /> }] : []),
            ]} />}
        </LoadState>
      </section> : null}

      {opening && writer ? (
        <PeriodPage title="Mở kỳ báo cáo" log={<LogPanel entries={[]} empty="Chưa mở kỳ; chưa có sự kiện." />}>
          <fieldset disabled={act.busy} style={{ border: 0, padding: 0, margin: 0 }}>
          <legend className="sr-only">Thông tin mở kỳ</legend>
          {route ? <label className="field">Năm <select aria-label="Năm mở kỳ" value={year} onChange={e => { setYear(Number(e.target.value)); reset(); }}>{[thisYear - 1, thisYear, thisYear + 1].map(y => <option key={y} value={y}>{y}</option>)}</select></label> : null}
          <div className="row">
            <label>
              Loại báo cáo{" "}
              <select
                aria-label="Loại báo cáo"
                value={type}
                onChange={(e) => {
                  setType(e.target.value as KhtcReportType);
                  setNo(1);
                  setScope("quarter");
                  reset();
                }}
              >
                {REPORT_TYPES.filter(t=>!adhocOnly||t.value==="adhoc").map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
            </label>
            {choices.length ? (
              <label>
                {type === "week" ? "Tuần" : type === "month" ? "Tháng" : "Quý"}{" "}
                <select
                  aria-label="Số kỳ"
                  value={no}
                  onChange={(e) => {
                    setNo(Number(e.target.value));
                    setScope("quarter");
                    reset();
                  }}
                >
                  {choices.map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            {type === "quarter" ? (
              <label>
                Phạm vi{" "}
                <select
                  aria-label="Phạm vi"
                  value={scope}
                  onChange={(e) => (
                    setScope(e.target.value as typeof scope),
                    reset()
                  )}
                >
                  {scopeChoices(no).map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            {type === "program" ? (
              <label>
                Đến ngày <span className="req" aria-hidden="true">*</span>{" "}
                <input
                  type="date"
                  aria-label="Đến ngày"
                  aria-invalid={!!error("asOf") || undefined}
                  aria-required={true}
                  value={asOf}
                  onChange={(e) => (setAsOf(e.target.value), reset())}
                />
                {fieldError("asOf")}
              </label>
            ) : null}
            {type === "adhoc" ? (
              <>
                <label>
                  Tên báo cáo <span className="req" aria-hidden="true">*</span>{" "}
                  <input
                    aria-label="Tên báo cáo"
                  aria-invalid={!!error("name") || undefined}
                  aria-required={true}
                    value={name}
                    onChange={(e) => { setName(e.target.value); setDraft(null); }}
                  />
                {fieldError("name")}
                </label>
                <label>
                  Từ ngày <span className="req" aria-hidden="true">*</span>{" "}
                  <input
                    type="date"
                    aria-label="Từ ngày"
                  aria-invalid={!!error("from") || undefined}
                  aria-required={true}
                    value={from}
                    onChange={(e) => { setFrom(e.target.value); setDraft(null); }}
                  />
                {fieldError("from")}
                </label>
                <label>
                  Đến ngày <span className="req" aria-hidden="true">*</span>{" "}
                  <input
                    type="date"
                    aria-label="Đến ngày phát sinh"
                  aria-invalid={!!error("to") || undefined}
                  aria-required={true}
                    value={to}
                    onChange={(e) => { setTo(e.target.value); setDraft(null); }}
                  />
                {fieldError("to")}
                </label>
              </>
            ) : null}
          </div>
          <div className="row">
            <label>
              Ngày chốt{" "}
              <input
                type="date"
                aria-label="Ngày chốt"
                  aria-invalid={!!error("cutoff") || undefined}
                value={cutoff || draft?.cutoff_date || ""}
                onChange={(e) => setCutoff(e.target.value)}
              />
                {fieldError("cutoff")}
            </label>
            <label>
              Hạn nộp {(type === "program" || type === "adhoc") ? <span className="req" aria-hidden="true">*</span> : null}{" "}
              <input
                type="datetime-local"
                aria-label="Hạn nộp"
                  aria-invalid={!!error("due") || undefined}
                  aria-required={type === "program" || type === "adhoc" || undefined}
                value={due || toLocal(draft?.due_at)}
                onChange={(e) => setDue(e.target.value)}
              />
                {fieldError("due")}
            </label>
            {!route ? <label>
              Hạn xuất{" "}
              <input type="datetime-local" aria-label="Hạn xuất" value={exportDue || toLocal(draft?.export_due_at)} onChange={(e) => setExportDue(e.target.value)} />
            </label> : null}
            <label>
              Số lần gửi tối đa{" "}
              <input
                aria-label="Số lần gửi tối đa"
                readOnly
                value={draft?.max_submissions ?? ""}
                style={{ background: "var(--muted-bg, #eee)" }}
              />
            </label>
          </div>
          <div className="row">
            <button
              className="btn"
              disabled={act.busy}
              onClick={async () => {
                if (!validate()) return;
                const epoch = routeEpoch.current;
                const preview = await act.run(() => kapi.preview(body()));
                if (epoch.active) setDraft(preview ?? null);
              }}
            >
              Xem trước
            </button>
            <button
              className="btn"
              onClick={() => { discard(); setOpening(false); }}
            >
              Hủy
            </button>
          </div>
          {draft ? (
            <div aria-label="Kỳ nháp">
              <p>
                <strong>{draft.name}</strong> — {vnDate(draft.from_date)} –{" "}
                {vnDate(draft.to_date)}
                {draft.due_required && !due ? (
                  <span className="warn">
                    {" "}
                    · Nhập hạn nộp theo văn bản yêu cầu / hạn chỉ đạo.
                  </span>
                ) : null}
              </p>
              {draft.duplicate ? (
                <p className="bad">
                  Kỳ “{draft.duplicate.name}” đã mở — hủy kỳ đó trước nếu mở
                  nhầm.
                </p>
              ) : null}
              {!draft.template_version ? (
                <p className="bad">Phiếu cơ sở chưa cài.</p>
              ) : null}
              <h3>Phiếu giao các phòng</h3>
              <ul>
                {draft.departments.map((d) => (
                  <li key={d.code}>
                    <label>
                      <input
                        type="checkbox"
                        checked={depts ? depts.includes(d.code) : true}
                        onChange={(e) => {
                          const cur =
                            depts ?? draft.departments.map((x) => x.code);
                          setDepts(
                            e.target.checked
                              ? [...cur, d.code]
                              : cur.filter((x) => x !== d.code),
                          );
                        }}
                      />{" "}
                      {d.name || d.code}
                    </label>
                  </li>
                ))}
              </ul>
              <h3>Nguồn dữ liệu từ module</h3>
              {!draft.sources_linked ? (
                <p className="mute">
                  Bộ quy đổi kỳ nguồn chưa nối — kỳ mở không gắn nguồn module.
                </p>
              ) : draft.sources.length === 0 ? (
                <p className="mute">Không có kỳ nguồn.</p>
              ) : (
                <ul>
                  {draft.sources.map((s) => (
                    <li key={`${s.source}:${s.native_key}`}>
                      {s.label || s.source} — {s.department} · kỳ nguồn{" "}
                      {s.native_key} ({s.calendar_id} {s.calendar_version})
                    </li>
                  ))}
                </ul>
              )}
              <button
                className="btn btn-primary"
                disabled={
                  act.busy ||
                  !!draft.duplicate ||
                  !draft.template_version ||
                  (draft.due_required && !due)
                }
                onClick={async () => {
                  if (!writer || !validate()) return;
                  const epoch = routeEpoch.current;
                  const p = await act.run(
                    () => kapi.open(body()),
                    (r) =>
                      `Đã mở kỳ “${r.name}” và giao ${r.departments.length} phiếu.`,
                  );
                  if (p && epoch.active) {
                    setOpening(false);
                    discard();
                    setSelected(p.id);
                    onOpened?.();
                    periods.reload();
                  }
                }}
              >
                Mở kỳ và giao phiếu
              </button>
            </div>
          ) : null}
          </fieldset>
        </PeriodPage>
      ) : null}
      {opening && !writer ? <PeriodPage title="Mở kỳ báo cáo"><p role="alert">Chỉ xem — Phòng KHTC đang có quyền soạn mới được mở kỳ.</p></PeriodPage> : null}

      {selected && (!route || route.view === "detail") ? (
        <section className="card sec" aria-label="Chi tiết kỳ">
          <LoadState s={detail}>
            {(p: KhtcPeriod) => {
              const done = async (r: KhtcPeriod | null) => {
                if (r) {
                  detail.reload();
                  periods.reload();
                }
              };
              if (p.id !== selected) return null;
              return (
                <PeriodPage title={p.name} log={<LogPanel entries={periodLog(p)} />}>
                  <div className="row">
                    <h2>{p.name}</h2>
                    <StatusPill tone={PERIOD_TONE[p.state]}>
                      {p.state_label}
                    </StatusPill>
                  </div>
                  {writer ? (
                    <div className="row">
                      <button
                        className="btn"
                        disabled={
                          act.busy ||
                          p.state === "closed" ||
                          p.state === "cancelled"
                        }
                        onClick={async () => {
                          if (route) { route.go(`${periodHref(p.id)}/sua-han`); return; }
                          const v = window.prompt(
                            "Hạn nộp mới (YYYY-MM-DDTHH:mm, giờ Việt Nam)",
                            toLocal(p.due_at),
                          );
                          if (!v) return;
                          await done(
                            await act.run(
                              () => kapi.deadline(p.id, fromLocal(v)!),
                              "Đã sửa hạn nộp.",
                            ),
                          );
                        }}
                      >
                        Sửa hạn nộp
                      </button>
                      <button
                        className="btn btn-primary"
                        disabled={act.busy || !p.can_close}
                        title={
                          p.can_close
                            ? undefined
                            : `Đóng khi Đủ dữ liệu, hoặc Trễ hạn quá ${p.close_late_days} ngày`
                        }
                        onClick={async () => {
                          const r = reason(
                            "đóng kỳ khi còn thiếu dữ liệu",
                            p.close_forced,
                          );
                          if (r === null) return;
                          await done(
                            await act.run(
                              () => kapi.close(p.id, r || undefined),
                              "Đã đóng kỳ.",
                            ),
                          );
                        }}
                      >
                        {p.close_forced
                          ? "Đóng kỳ (còn thiếu dữ liệu)"
                          : "Đóng kỳ"}
                      </button>
                      <button
                        className="btn btn-danger"
                        disabled={act.busy || !p.can_cancel}
                        onClick={async () => {
                          const r = reason("hủy kỳ");
                          if (!r) return;
                          await done(
                            await act.run(
                              () => kapi.cancel(p.id, r),
                              "Đã hủy kỳ.",
                            ),
                          );
                        }}
                      >
                        Hủy kỳ
                      </button>
                    </div>
                  ) : null}
                  <div className="row" aria-label="Thẻ đếm">
                    <div className="card">Phiếu: {p.counts.forms}</div>
                    <div className="card">Đã gửi: {p.counts.forms_sent}</div>
                    <div className="card">Nguồn module: {p.counts.sources}</div>
                    <div className="card">
                      Nguồn đã chốt: {p.counts.sources_closed}
                    </div>
                  </div>
                  <p className="mute">
                    {vnDate(p.from_date)} – {vnDate(p.to_date)} · Ngày chốt{" "}
                    {vnDate(p.cutoff_date)} · Hạn nộp {dt(p.due_at)}{!route ? <> · Hạn xuất {dt(p.export_due_at)}</> : null} · Tối đa {p.max_submissions} lần gửi
                  </p>
                  <h3>Tiến độ nộp phiếu và nguồn module</h3>
                  <DataTable caption="Tiến độ nộp phiếu và nguồn module" rowKey={row => row.key} rows={[
                    ...p.forms.map(f => ({ key: `form:${f.department}`, name: f.department_name || f.department,
                      state: <StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>,
                      attempt: f.attempt ? `${f.attempt}/${p.max_submissions || "∞"}${f.limit_reached ? " (đã khóa yêu cầu bổ sung)" : ""}` : "—",
                      at: dt(f.last_submitted_at), note: f.review_note,
                      action: f.attempt ? <Link className="btn" href={`/kn/khtc/ra-soat/${p.id}?department=${f.department}`}>Rà soát</Link> : null })),
                    ...p.sources.map(source => ({ key: `source:${source.source}:${source.native_key}`, name: `${source.label || source.source} (${source.department}, kỳ nguồn ${source.native_key})`,
                      state: <StatusPill tone={!source.known ? "warn" : source.closed ? "ok" : "info"}>{!source.known ? "Không đọc được" : source.closed ? "Đã chốt" : "Chưa chốt"}</StatusPill>,
                      attempt: "—", at: dt(source.closed_at), note: `${source.calendar_id} ${source.calendar_version}`, action: null })),
                  ]} columns={[
                    { key: "name", header: "Phòng / nguồn", cell: r => r.name },
                    { key: "state", header: "Trạng thái", cell: r => r.state },
                    { key: "attempt", header: "Lần gửi", cell: r => r.attempt },
                    { key: "at", header: "Gửi lúc", cell: r => r.at },
                    { key: "note", header: "Ghi chú", cell: r => r.note },
                    { key: "action", header: "Thao tác", cell: r => r.action },
                  ]} />
                  <KhtcSourcesPanel periodId={p.id} />
                </PeriodPage>
              );
            }}
          </LoadState>
        </section>
      ) : null}
    </div>
  );
}
