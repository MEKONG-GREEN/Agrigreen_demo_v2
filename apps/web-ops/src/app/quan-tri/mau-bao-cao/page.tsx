"use client";

import { useMemo, useState, type FormEvent } from "react";
import { fullName } from "@/admin/accounts";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import type { AdminReportTemplate as ReportTemplate, ReportSchedule } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const WEEKDAY = ["", "Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy", "Chủ nhật"];

function useTemplates() {
  const { adminApi } = useAuth();
  return useAsync(() => unwrap(adminApi.GET("/v1/reports/templates")), [adminApi]);
}

function TemplateEditor({ t, onDone }: { t: ReportTemplate; onDone: () => void }) {
  const { adminApi } = useAuth();
  const [title, setTitle] = useState(t.definition.title);
  const [note, setNote] = useState(t.definition.form_note ?? "");
  const [formats, setFormats] = useState<Set<"xlsx" | "pdf">>(new Set(t.definition.formats));
  const [retireReason, setRetireReason] = useState("");
  const [busy, setBusy] = useState<"save" | "retire" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const versions = useAsync(
    () => unwrap(adminApi.GET("/v1/reports/templates/{key}/versions", { params: { path: { key: t.key } } })),
    [adminApi, t.key, t.version],
  );
  const errs = problem ? fieldErrorMap(problem) : {};

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy("save");
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/reports/templates", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { key: t.key, definition: { ...t.definition, title: title.trim(), form_note: note.trim() || undefined, formats: Array.from(formats) } },
        }),
      );
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(null);
    }
  };

  const retire = async () => {
    setBusy("retire");
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/reports/templates/{key}/retire", {
          params: { path: { key: t.key }, header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { reason: retireReason.trim() },
        }),
      );
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="stack">
      <div>
        <h2 style={{ fontSize: 17 }}>{t.definition.title}</h2>
        <code className="mute small">
          {t.key} · phiên bản {t.version} {t.builtin ? "(mẫu hệ thống)" : ""}
        </code>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {t.status === "active" ? (
        <form className="panel stack" style={{ gap: 8 }} onSubmit={(e) => void save(e)}>
          <b>Tạo phiên bản mới</b>
          <span className="small mute">Việc xuất cũ giữ ảnh chụp phiên bản cũ; phiên bản mới áp dụng cho lần xuất sau.</span>
          <div className="field">
            <label htmlFor="tp-title">Tiêu đề</label>
            <input id="tp-title" className="input" value={title} onChange={(e) => setTitle(e.target.value)} aria-invalid={!!errs["definition.title"]} />
            {errs["definition.title"] ? <span className="err">{errs["definition.title"]}</span> : null}
          </div>
          <div className="field">
            <label htmlFor="tp-note">Ghi chú mẫu biểu</label>
            <input id="tp-note" className="input" value={note} onChange={(e) => setNote(e.target.value)} />
          </div>
          <fieldset className="plain">
            <legend>Định dạng</legend>
            <div className="row">
              {(["xlsx", "pdf"] as const).map((f) => (
                <label key={f} className="row small" style={{ gap: 6 }}>
                  <input
                    type="checkbox"
                    checked={formats.has(f)}
                    onChange={() => {
                      const n = new Set(formats);
                      if (n.has(f)) n.delete(f);
                      else n.add(f);
                      setFormats(n);
                    }}
                  />
                  {f === "xlsx" ? "Excel" : "PDF"}
                </label>
              ))}
            </div>
          </fieldset>
          <p className="small mute">Cột: {t.definition.columns.map((c) => c.title).join(", ")}</p>
          <div>
            <button type="submit" className="btn btn-p btn-sm" disabled={!!busy || formats.size === 0}>
              {busy === "save" ? "Đang lưu…" : "Lưu phiên bản mới"}
            </button>
          </div>
          <div className="field">
            <label htmlFor="tp-retire">Lý do ngừng dùng (≥ 10 ký tự)</label>
            <input id="tp-retire" className="input" value={retireReason} onChange={(e) => setRetireReason(e.target.value)} />
          </div>
          <div>
            <button type="button" className="btn btn-o btn-sm" onClick={() => void retire()} disabled={!!busy || retireReason.trim().length < 10}>
              {busy === "retire" ? "Đang gửi…" : "Ngừng dùng mẫu"}
            </button>
          </div>
        </form>
      ) : (
        <p className="small">
          Đã ngừng dùng{t.reason ? ` — ${t.reason}` : ""}.
        </p>
      )}
      <div>
        <h3 style={{ fontSize: 14, marginBottom: 6 }}>Lịch sử phiên bản</h3>
        {versions.error ? <ProblemAlert problem={versions.error} onRetry={versions.reload} /> : null}
        <ol style={{ marginLeft: 18 }}>
          {(versions.data?.items ?? []).map((v) => (
            <li key={v.version} className="small">
              Phiên bản {v.version} · {v.status === "active" ? "đang dùng" : "ngừng dùng"} {v.created_at ? `· ${formatDateTime(v.created_at)}` : "· mẫu hệ thống"}
              {v.reason ? <div className="mute">{v.reason}</div> : null}
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}

function TemplatesTab() {
  const templates = useTemplates();
  const [selected, setSelected] = useState<string | null>(null);
  const t = templates.data?.items.find((x) => x.key === selected);
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1fr) minmax(320px, 1fr)", alignItems: "start" }}>
      <div>
        {templates.error ? <ProblemAlert problem={templates.error} onRetry={templates.reload} /> : null}
        {templates.loading && !templates.data ? <Loading /> : null}
        <ul className="list-plain">
          {(templates.data?.items ?? []).map((x) => (
            <li key={x.key}>
              <button type="button" onClick={() => setSelected(x.key)} style={{ textAlign: "left", width: "100%" }}>
                <div className="row" style={{ justifyContent: "space-between" }}>
                  <b>{x.definition.title}</b>
                  {x.status === "active" ? <span className="chip c-ok">Đang dùng</span> : <span className="chip c-mute">Ngừng dùng</span>}
                </div>
                <div className="mute small">
                  <code>{x.key}</code> · v{x.version} · {x.definition.formats.join(", ")}
                  {x.definition.provisional_layout ? " · bố cục tạm" : ""}
                </div>
              </button>
            </li>
          ))}
        </ul>
      </div>
      <aside className="card sec" aria-label="Chi tiết mẫu">
        {t ? <TemplateEditor key={`${t.key}-${t.version}`} t={t} onDone={templates.reload} /> : <p className="mute">Chọn một mẫu để tạo phiên bản mới hoặc ngừng dùng.</p>}
      </aside>
    </div>
  );
}

function ScheduleForm({ templates, people, onDone }: { templates: ReportTemplate[]; people: { id: string; name: string }[]; onDone: () => void }) {
  const { adminApi, api } = useAuth();
  const active = templates.filter((t) => t.status === "active");
  const [key, setKey] = useState(active[0]?.key ?? "");
  const [format, setFormat] = useState<"xlsx" | "pdf">("xlsx");
  const [unit, setUnit] = useState("");
  const [frequency, setFrequency] = useState<"weekly" | "monthly">("weekly");
  const [weekday, setWeekday] = useState("1");
  const [day, setDay] = useState("1");
  const [hour, setHour] = useState("7");
  const [recipients, setRecipients] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const units = useAsync(() => listChildUnits(api, null), [api]);
  const tpl = active.find((t) => t.key === key);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/reports/schedules", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: {
            template_key: key,
            format: tpl?.definition.formats.includes(format) ? format : (tpl?.definition.formats[0] ?? "xlsx"),
            unit_code: unit || undefined,
            frequency,
            weekday: frequency === "weekly" ? Number(weekday) : undefined,
            day_of_month: frequency === "monthly" ? Number(day) : undefined,
            hour: Number(hour),
            recipients: Array.from(recipients),
          },
        }),
      );
      setRecipients(new Set());
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-labelledby="sch-title">
      <b id="sch-title">Lập lịch gửi báo cáo qua email</b>
      <div className="row">
        <div className="field">
          <label htmlFor="sch-tpl">Mẫu</label>
          <select id="sch-tpl" className="select" value={key} onChange={(e) => setKey(e.target.value)}>
            {active.map((t) => (
              <option key={t.key} value={t.key}>
                {t.definition.title}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="sch-fmt">Định dạng</label>
          <select id="sch-fmt" className="select" value={format} onChange={(e) => setFormat(e.target.value === "pdf" ? "pdf" : "xlsx")}>
            {(tpl?.definition.formats ?? ["xlsx"]).map((f) => (
              <option key={f} value={f}>
                {f === "xlsx" ? "Excel" : "PDF"}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="sch-unit">Đơn vị</label>
          <select id="sch-unit" className="select" value={unit} onChange={(e) => setUnit(e.target.value)}>
            <option value="">Toàn phạm vi</option>
            {(units.data ?? []).map((u) => (
              <option key={u.code} value={u.code}>
                {u.name}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="row">
        <div className="field">
          <label htmlFor="sch-freq">Tần suất</label>
          <select id="sch-freq" className="select" value={frequency} onChange={(e) => setFrequency(e.target.value === "monthly" ? "monthly" : "weekly")}>
            <option value="weekly">Hằng tuần</option>
            <option value="monthly">Hằng tháng</option>
          </select>
        </div>
        {frequency === "weekly" ? (
          <div className="field">
            <label htmlFor="sch-wd">Thứ</label>
            <select id="sch-wd" className="select" value={weekday} onChange={(e) => setWeekday(e.target.value)}>
              {WEEKDAY.slice(1).map((w, i) => (
                <option key={w} value={String(i + 1)}>
                  {w}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className="field">
            <label htmlFor="sch-day">Ngày trong tháng (1–28)</label>
            <input id="sch-day" className="input" inputMode="numeric" value={day} onChange={(e) => setDay(e.target.value)} style={{ width: 90 }} />
          </div>
        )}
        <div className="field">
          <label htmlFor="sch-hour">Giờ gửi (giờ Việt Nam)</label>
          <input id="sch-hour" className="input" inputMode="numeric" value={hour} onChange={(e) => setHour(e.target.value)} style={{ width: 90 }} />
        </div>
      </div>
      <fieldset className="plain">
        <legend>Người nhận ({recipients.size}) — chọn tài khoản cán bộ; email tra lúc gửi, không lưu trong lịch</legend>
        {people.length === 0 ? <p className="mute small">Không đọc được danh sách tài khoản (cần vai quản trị tài khoản).</p> : null}
        <div className="check-grid">
          {people.map((p) => (
            <label key={p.id} className="row small" style={{ gap: 6 }}>
              <input
                type="checkbox"
                checked={recipients.has(p.id)}
                onChange={() => {
                  const n = new Set(recipients);
                  if (n.has(p.id)) n.delete(p.id);
                  else n.add(p.id);
                  setRecipients(n);
                }}
              />
              {p.name}
            </label>
          ))}
        </div>
      </fieldset>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || !key || recipients.size === 0}>
          {busy ? "Đang lập lịch…" : "Lập lịch"}
        </button>
      </div>
    </form>
  );
}

function ScheduleRow({ s, title, names, onChanged }: { s: ReportSchedule; title: string; names: ReadonlyMap<string, string>; onChanged: () => void }) {
  const { adminApi } = useAuth();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const runs = useAsync(
    () => (open ? unwrap(adminApi.GET("/v1/reports/schedules/{id}/runs", { params: { path: { id: s.id } } })) : Promise.resolve(null)),
    [adminApi, s.id, open],
  );
  const deactivate = async () => {
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/reports/schedules/{id}/deactivate", { params: { path: { id: s.id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { reason: reason.trim() } }));
      onChanged();
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    }
  };
  const when = s.frequency === "weekly" ? `Hằng tuần, ${WEEKDAY[s.weekday ?? 1] ?? ""}` : `Hằng tháng, ngày ${s.day_of_month ?? 1}`;
  return (
    <li>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <div>
          <b>{title}</b> <span className="mute small">({s.format.toUpperCase()})</span>
          <div className="small">
            {when} lúc {s.hour} giờ · {s.unit_code ? `đơn vị ${s.unit_code}` : "toàn phạm vi"} · gửi {s.recipients.map((r) => names.get(r) ?? r.slice(-6)).join(", ")}
          </div>
        </div>
        <div className="row">
          {s.active ? <span className="chip c-ok">Đang chạy</span> : <span className="chip c-mute">Đã ngừng</span>}
          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
            {open ? "Thu gọn" : "Chi tiết"}
          </button>
        </div>
      </div>
      {open ? (
        <div className="panel stack" style={{ gap: 8, marginTop: 8 }}>
          <b className="small">Các lần chạy</b>
          {runs.data && runs.data.items.length === 0 ? <span className="mute small">Chưa tới mốc chạy đầu tiên.</span> : null}
          <ul className="list-plain small">
            {(runs.data?.items ?? []).map((r) => (
              <li key={r.slot_key}>
                {r.slot_key} · {r.status === "queued" ? "đã dựng" : `bỏ qua${r.skip_reason ? ` (${r.skip_reason})` : ""}`} · email:{" "}
                {r.delivery_status === "sent" ? `đã gửi ${r.sent_count ?? 0}` : (r.delivery_status ?? "—")} · {formatDateTime(r.fired_at)}
              </li>
            ))}
          </ul>
          {s.active ? (
            <div className="row" style={{ alignItems: "flex-end" }}>
              <div className="field" style={{ flex: "1 1 240px" }}>
                <label htmlFor={`sd-${s.id}`}>Lý do ngừng lịch (≥ 10 ký tự)</label>
                <input id={`sd-${s.id}`} className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
              </div>
              <button type="button" className="btn btn-o btn-sm" disabled={reason.trim().length < 10} onClick={() => void deactivate()}>
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

function SchedulesTab() {
  const { adminApi } = useAuth();
  const templates = useTemplates();
  const schedules = useAsync(() => unwrap(adminApi.GET("/v1/reports/schedules")), [adminApi]);
  const people = useAsync(
    () =>
      collectPages((cursor) => unwrap(adminApi.GET("/v1/accounts/users", { params: { query: { cursor, limit: 100 } } })), 500).then(
        (list) => list.filter((u) => u.enabled).map((u) => ({ id: u.id, name: `${fullName(u) || u.username} — ${u.email}` })),
        () => [] as { id: string; name: string }[],
      ),
    [adminApi],
  );
  const names = useMemo(() => new Map((people.data ?? []).map((p) => [p.id, p.name.split(" — ")[0] ?? p.name])), [people.data]);
  const titleOf = (key: string) => templates.data?.items.find((t) => t.key === key)?.definition.title ?? key;
  return (
    <>
      {templates.data ? <ScheduleForm templates={templates.data.items} people={people.data ?? []} onDone={schedules.reload} /> : null}
      {schedules.error ? <ProblemAlert problem={schedules.error} onRetry={schedules.reload} /> : null}
      {schedules.loading && !schedules.data ? <Loading /> : null}
      {schedules.data && schedules.data.items.length === 0 ? <Empty>Chưa có lịch gửi.</Empty> : null}
      <ul className="list-plain">
        {(schedules.data?.items ?? []).map((s) => (
          <ScheduleRow key={`${s.id}-${s.updated_at}`} s={s} title={titleOf(s.template_key)} names={names} onChanged={schedules.reload} />
        ))}
      </ul>
    </>
  );
}

/** Mẫu & lịch báo cáo (pf/reports, Admin API — reports_admin, masterdata_admin, platform_admin; MFA). */
export default function ReportAdminPage() {
  const [tab, setTab] = useState("templates");
  return (
    <>
      <PageHead title="Mẫu & lịch báo cáo" subtitle="Mẫu biểu có phiên bản (việc xuất cũ giữ phiên bản cũ) và lịch gửi email hằng tuần/hằng tháng" />
      <MfaNotice />
      <div className="card sec">
        <Tabs
          label="Quản trị báo cáo"
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "templates", label: "Mẫu báo cáo" },
            { id: "schedules", label: "Lịch gửi" },
          ]}
        >
          {tab === "templates" ? <TemplatesTab /> : <SchedulesTab />}
        </Tabs>
      </div>
    </>
  );
}
