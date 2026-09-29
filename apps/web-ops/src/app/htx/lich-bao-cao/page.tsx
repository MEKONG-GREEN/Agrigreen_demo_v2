"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { ReportRecipient, ReportSchedule } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { useCatalog } from "@/htx/common";
import { deliveryWarning, scheduleWhen, validateSchedule, type ScheduleDraft } from "@/htx/schedules";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const WEEKDAYS = ["Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy", "Chủ nhật"];

function ScheduleForm({ recipients, onDone }: { recipients: ReportRecipient[]; onDone: () => void }) {
  const { api } = useAuth();
  const seasons = useCatalog("season");
  const [d, setD] = useState<ScheduleDraft>({ format: "xlsx", frequency: "monthly", weekday: "1", day: "1", hour: "7", seasonCode: "", seasonYear: "", recipients: [] });
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const v = validateSchedule(d);
  const set = (patch: Partial<ScheduleDraft>) => {
    setD((x) => ({ ...x, ...patch }));
    setKey(newIdempotencyKey());
  };
  const toggle = (ref: string) => set({ recipients: d.recipients.includes(ref) ? d.recipients.filter((r) => r !== ref) : [...d.recipients, ref] });
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!v.ok) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(api.POST("/v1/reports/schedules", { params: { header: { "Idempotency-Key": key } }, body: v.body }));
      setTouched(false);
      setD((x) => ({ ...x, recipients: [] }));
      setKey(newIdempotencyKey());
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Lập lịch gửi báo cáo sản lượng">
      <b>Lập lịch gửi báo cáo sản lượng HTX qua email</b>
      <div className="row">
        <div className="field">
          <label htmlFor="ls-fmt">Định dạng</label>
          <select id="ls-fmt" className="select" value={d.format} onChange={(e) => set({ format: e.target.value === "pdf" ? "pdf" : "xlsx" })}>
            <option value="xlsx">Excel</option>
            <option value="pdf">PDF</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="ls-freq">Tần suất</label>
          <select id="ls-freq" className="select" value={d.frequency} onChange={(e) => set({ frequency: e.target.value === "weekly" ? "weekly" : "monthly" })}>
            <option value="weekly">Hằng tuần</option>
            <option value="monthly">Hằng tháng</option>
          </select>
        </div>
        {d.frequency === "weekly" ? (
          <div className="field">
            <label htmlFor="ls-wd">Thứ</label>
            <select id="ls-wd" className="select" value={d.weekday} onChange={(e) => set({ weekday: e.target.value })}>
              {WEEKDAYS.map((w, i) => (
                <option key={w} value={String(i + 1)}>
                  {w}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className="field">
            <label htmlFor="ls-day">Ngày trong tháng (1–28)</label>
            <input id="ls-day" className="input" inputMode="numeric" style={{ width: 90 }} value={d.day} onChange={(e) => set({ day: e.target.value })} />
          </div>
        )}
        <div className="field">
          <label htmlFor="ls-hour">Giờ gửi (giờ Việt Nam)</label>
          <input id="ls-hour" className="input" inputMode="numeric" style={{ width: 90 }} value={d.hour} onChange={(e) => set({ hour: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="ls-season">Vụ (tùy chọn)</label>
          <select id="ls-season" className="select" value={d.seasonCode} onChange={(e) => set({ seasonCode: e.target.value })}>
            <option value="">Mọi vụ</option>
            {seasons.map((s) => (
              <option key={s.code} value={s.code}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        {d.seasonCode ? (
          <div className="field">
            <label htmlFor="ls-year">Năm vụ</label>
            <input id="ls-year" className="input" inputMode="numeric" style={{ width: 90 }} value={d.seasonYear} onChange={(e) => set({ seasonYear: e.target.value })} />
          </div>
        ) : null}
      </div>
      <fieldset className="plain">
        <legend>Người nhận ({d.recipients.length}) — chỉ tài khoản đã đăng ký của HTX, email đã xác minh</legend>
        {recipients.length === 0 ? <p className="mute small">HTX chưa có tài khoản nào có email đã xác minh.</p> : null}
        <div className="check-grid">
          {recipients.map((r) => (
            <label key={r.actor_ref} className="row small" style={{ gap: 6 }}>
              <input type="checkbox" checked={d.recipients.includes(r.actor_ref)} onChange={() => toggle(r.actor_ref)} />
              {r.display_name} · {r.masked_email}
            </label>
          ))}
        </div>
      </fieldset>
      {touched && !v.ok ? (
        <ul className="err small" role="alert">
          {v.errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lập lịch…" : "Lập lịch"}
        </button>
      </div>
    </form>
  );
}

function ScheduleRow({ s, names, onChanged }: { s: ReportSchedule; names: ReadonlyMap<string, string>; onChanged: () => void }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const runs = useAsync(
    () => (open ? unwrap(api.GET("/v1/reports/schedules/{id}/runs", { params: { path: { id: s.id } } })) : Promise.resolve(null)),
    [api, s.id, open],
  );
  const stop = async () => {
    setProblem(null);
    try {
      await unwrap(api.POST("/v1/reports/schedules/{id}/deactivate", { params: { path: { id: s.id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { reason: reason.trim() } }));
      onChanged();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };
  const warn = deliveryWarning(runs.data?.items ?? []);
  return (
    <li className="panel stack" style={{ gap: 6, marginBottom: 8 }} aria-label={`Lịch ${scheduleWhen(s)}`}>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <div>
          <b>Báo cáo sản lượng HTX</b> <span className="mute small">({s.format.toUpperCase()})</span>
          <div className="small">
            {scheduleWhen(s)} · gửi {s.recipients.map((r) => names.get(r) ?? r.slice(-6)).join(", ")}
          </div>
        </div>
        <div className="row">
          {s.active ? <span className="chip c-ok">Đang chạy</span> : <span className="chip c-mute">Đã ngừng</span>}
          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen((x) => !x)} aria-expanded={open}>
            {open ? "Thu gọn" : "Chi tiết"}
          </button>
        </div>
      </div>
      {open ? (
        <div className="stack" style={{ gap: 6 }}>
          {runs.loading && !runs.data ? <Loading /> : null}
          {runs.data && runs.data.items.length === 0 ? <span className="mute small">Chưa tới mốc gửi đầu tiên.</span> : null}
          {warn ? (
            <div className="alert alert-warn small" role="alert">
              <Icon name="alert" />
              <span>{warn}</span>
            </div>
          ) : null}
          <ul className="list-plain small">
            {(runs.data?.items ?? []).map((r) => (
              <li key={r.slot_key}>
                {r.slot_key} · {r.status === "queued" ? "đã tổng hợp" : `bỏ qua${r.skip_reason ? ` (${r.skip_reason})` : ""}`} · email:{" "}
                {r.delivery_status === "sent" ? `đã gửi ${r.sent_count ?? 0}` : (r.delivery_status ?? "—")}
                {r.delivery_note ? ` (${r.delivery_note})` : ""} · {formatDateTime(r.fired_at)}
              </li>
            ))}
          </ul>
          {s.active ? (
            <div className="row" style={{ alignItems: "flex-end" }}>
              <div className="field" style={{ flex: "1 1 240px" }}>
                <label htmlFor={`ls-stop-${s.id}`}>Lý do ngừng lịch (≥ 10 ký tự)</label>
                <input id={`ls-stop-${s.id}`} className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
              </div>
              <button type="button" className="btn btn-o btn-sm" disabled={reason.trim().length < 10} onClick={() => void stop()}>
                Ngừng lịch
              </button>
            </div>
          ) : (
            <span className="small">Đã ngừng: {s.deactivated_reason}</span>
          )}
          {problem ? <ProblemAlert problem={problem} /> : null}
        </div>
      ) : null}
    </li>
  );
}

/** Lịch gửi báo cáo của Quản lý HTX (HTX US-DASH-03) — Application API, chỉ HTX của mình. */
export default function CoopSchedulesPage() {
  const { api } = useAuth();
  const [version, setVersion] = useState(0);
  const recipients = useAsync(() => unwrap(api.GET("/v1/reports/recipients")), [api]);
  const schedules = useAsync(() => unwrap(api.GET("/v1/reports/schedules")), [api, version]);
  const names = new Map((recipients.data?.items ?? []).map((r) => [r.actor_ref, r.display_name]));
  const reload = () => setVersion((x) => x + 1);
  return (
    <>
      <PageHead title="Lịch gửi báo cáo" subtitle="Hệ thống tự tổng hợp báo cáo sản lượng HTX và gửi email theo lịch (giờ Việt Nam)" />
      <div className="card sec">
        {recipients.error ? <ProblemAlert problem={recipients.error} onRetry={recipients.reload} /> : null}
        {recipients.data ? <ScheduleForm recipients={recipients.data.items} onDone={reload} /> : recipients.loading ? <Loading /> : null}
        <h2 className="sec-h">Lịch đã lập</h2>
        {schedules.error ? <ProblemAlert problem={schedules.error} onRetry={schedules.reload} /> : null}
        {schedules.loading && !schedules.data ? <Loading /> : null}
        {schedules.data && schedules.data.items.length === 0 ? <Empty>Chưa có lịch gửi báo cáo.</Empty> : null}
        <ul className="list-plain" aria-label="Lịch gửi báo cáo">
          {(schedules.data?.items ?? []).map((s) => (
            <ScheduleRow key={`${s.id}-${s.updated_at}`} s={s} names={names} onChanged={reload} />
          ))}
        </ul>
      </div>
    </>
  );
}
