"use client";

// Cảnh báo CNTS (W11.43, BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5) — một màn cho hai cấp:
// - Cấp Tỉnh: danh sách cảnh báo (Khẩn / Cần chú ý, trạng thái, quá hạn), giao người xử lý + hạn xử lý, đóng có kết quả;
//   tab "Cấu hình nhắc nhở" (theo biểu: trước hạn, khi trễ, nhắc lặp, trễ liên tiếp, kênh, nội dung) và "Ngưỡng & xử lý"
//   (hạn xử lý theo loại, mức Khẩn của môi trường nước; ngưỡng cnts.* sửa qua quy trình tham số).
// - Cấp Xã: cảnh báo của các xã phụ trách; nhận xử lý và đóng cảnh báo được giao.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  ACTION_LABEL,
  CHANNEL_LABEL,
  LEVEL_LABEL,
  RECIPIENT_LABEL,
  STATUS_LABEL,
  STATUS_TABS,
  STATUS_TONE,
  createAlertApi,
  findingsOf,
  reasonOf,
  reminderErrors,
  type AlertApi,
  type AlertConfig,
  type AlertQuery,
  type FormReminder,
} from "./api";

type Tab = "alerts" | "reminders" | "settings";

export function AlertScreen() {
  const { api, kn } = useAuth();
  const aapi = useMemo(() => (api ? createAlertApi(api) : null), [api]);
  const role = kn.context?.role;
  const tinh = role === "kn_tinh" || role === "admin";
  const readOnly = !!kn.context?.read_only;
  const [tab, setTab] = useState<Tab>("alerts");
  if (!aapi) return null;
  return (
    <div className="page">
      <PageHeader title="Cảnh báo">
        <p className="mute">
          {tinh
            ? "Cảnh báo tự động theo công thức Mục III.5 (biến động tổng đàn, khai thác, KSGM, ổ dịch mới, trễ nộp liên tiếp…): giao người xử lý, theo dõi hạn xử lý, đóng có kết quả."
            : "Cảnh báo của các xã phụ trách. Nhận xử lý và ghi kết quả cho cảnh báo được giao."}
        </p>
      </PageHeader>
      {tinh ? (
        <div className="row" role="tablist" aria-label="Mục">
          {(
            [
              ["alerts", "Danh sách cảnh báo"],
              ["reminders", "Cấu hình nhắc nhở"],
              ["settings", "Ngưỡng & xử lý"],
            ] as const
          ).map(([k, l]) => (
            <button key={k} role="tab" aria-selected={tab === k} className={`btn btn-sm${tab === k ? " btn-primary" : ""}`} onClick={() => setTab(k)}>
              {l}
            </button>
          ))}
        </div>
      ) : null}
      {tab === "alerts" ? <AlertList aapi={aapi} tinh={tinh} /> : null}
      {tab === "reminders" ? <ReminderConfig aapi={aapi} canEdit={tinh && !readOnly} /> : null}
      {tab === "settings" ? <KindSettings aapi={aapi} canEdit={tinh && !readOnly} /> : null}
    </div>
  );
}

function AlertList({ aapi, tinh }: { aapi: AlertApi; tinh: boolean }) {
  const [status, setStatus] = useState<(typeof STATUS_TABS)[number]["key"]>("open");
  const [level, setLevel] = useState<"" | "urgent" | "attention">("");
  const [mine, setMine] = useState(false);
  const q: AlertQuery = { ...(status ? { status } : {}), ...(level ? { level } : {}), ...(mine ? { mine } : {}) };
  const list = useLoad(() => aapi.list(q), [aapi, status, level, mine]);
  const communeName = useCommuneNames((list.data?.items ?? []).map((a) => a.commune_code ?? "").filter(Boolean));
  const [open, setOpen] = useState<string | null>(null);
  return (
    <>
      <section className="card sec" aria-label="Danh sách cảnh báo">
        <h2>Cảnh báo</h2>
        {list.data ? (
          <p className="mute small">
            Chưa đóng {list.data.counts.open} · Khẩn {list.data.counts.urgent_open} · Quá hạn xử lý {list.data.counts.overdue}
          </p>
        ) : null}
        <div className="row">
          <div className="row" role="tablist" aria-label="Trạng thái">
            {STATUS_TABS.map((t) => (
              <button key={t.key || "all"} role="tab" aria-selected={status === t.key} className={`btn btn-sm${status === t.key ? " btn-primary" : ""}`}
                onClick={() => setStatus(t.key)}>
                {t.label}
              </button>
            ))}
          </div>
          <select className="select" aria-label="Mức" value={level} onChange={(e) => setLevel(e.target.value as typeof level)}>
            <option value="">Mọi mức</option>
            <option value="urgent">Khẩn</option>
            <option value="attention">Cần chú ý</option>
          </select>
          <label className="row small">
            <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} /> Giao cho tôi
          </label>
        </div>
        <LoadState s={list}>
          {(d) => (
            <table className="dt" aria-label="Bảng cảnh báo">
              <thead>
                <tr>
                  <th>Mức</th>
                  <th>Loại</th>
                  <th>Nội dung</th>
                  <th>Xã</th>
                  <th>Thời điểm</th>
                  <th>Người xử lý</th>
                  <th>Hạn xử lý</th>
                  <th>Trạng thái</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {d.items.map((a) => (
                  <tr key={a.alert_ref}>
                    <td>
                      <StatusPill tone={a.level === "urgent" ? "bad" : "warn"}>{LEVEL_LABEL[a.level]}</StatusPill>
                    </td>
                    <td>{a.kind_no ? `${a.kind_no}. ` : ""}{a.kind_label}</td>
                    <td>
                      {a.title}
                      {reasonOf(a) ? <div className="mute small">{reasonOf(a)}</div> : null}
                    </td>
                    <td>{a.commune_code ? communeName(a.commune_code) : "—"}</td>
                    <td>{dt(a.raised_at)}</td>
                    <td>{a.assignee?.display ?? (a.assignee ? "Đã giao" : "—")}</td>
                    <td>
                      {dt(a.due_at)}
                      {a.overdue ? <StatusPill tone="bad"> Quá hạn</StatusPill> : null}
                    </td>
                    <td>
                      <StatusPill tone={STATUS_TONE[a.status]}>{a.auto_closed ? "Tự đóng" : STATUS_LABEL[a.status]}</StatusPill>
                    </td>
                    <td>
                      <button className="btn btn-sm" onClick={() => setOpen(a.alert_ref)}>
                        Xem
                      </button>
                    </td>
                  </tr>
                ))}
                {d.items.length === 0 ? (
                  <tr>
                    <td colSpan={9} className="mute">
                      Không có cảnh báo.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          )}
        </LoadState>
      </section>
      {open ? (
        <AlertDetailView key={open} aref={open} aapi={aapi} tinh={tinh} communeName={communeName} onChanged={list.reload} onClose={() => setOpen(null)} />
      ) : null}
    </>
  );
}

function AlertDetailView({
  aref,
  aapi,
  tinh,
  communeName,
  onChanged,
  onClose,
}: {
  aref: string;
  aapi: AlertApi;
  tinh: boolean;
  communeName: (c: string) => string;
  onChanged: () => void;
  onClose: () => void;
}) {
  const detail = useLoad(() => aapi.get(aref), [aapi, aref]);
  const commune = detail.data?.commune_code ?? null;
  const people = useLoad(tinh && detail.data ? () => aapi.assignees(commune) : null, [aapi, tinh, commune, !!detail.data]);
  const act = useAction();
  const [assignee, setAssignee] = useState("");
  const [due, setDue] = useState("");
  const [note, setNote] = useState("");
  const [result, setResult] = useState("");
  const done = (ok: unknown) => {
    if (ok) {
      detail.reload();
      onChanged();
    }
  };
  return (
    <section className="card sec" aria-label="Chi tiết cảnh báo">
      <LoadState s={detail}>
        {(a) => {
          const fs = findingsOf(a);
          return (
            <>
              <h2>
                <StatusPill tone={a.level === "urgent" ? "bad" : "warn"}>{LEVEL_LABEL[a.level]}</StatusPill> {a.kind_label}{" "}
                <StatusPill tone={STATUS_TONE[a.status]}>{a.auto_closed ? "Tự đóng" : STATUS_LABEL[a.status]}</StatusPill>
              </h2>
              {act.view}
              <dl className="kv">
                <dt>Nội dung</dt>
                <dd>{a.title}</dd>
                <dt>Xã</dt>
                <dd>{a.commune_code ? communeName(a.commune_code) : "—"}</dd>
                <dt>Biểu, kỳ</dt>
                <dd>{[a.template_code, a.period_key].filter(Boolean).join(" · ") || "—"}</dd>
                <dt>Tạo lúc</dt>
                <dd>{dt(a.raised_at)}</dd>
                <dt>Hạn xử lý</dt>
                <dd>
                  {dt(a.due_at)} {a.overdue ? <StatusPill tone="bad">Quá hạn</StatusPill> : null}
                </dd>
                <dt>Người xử lý</dt>
                <dd>{a.assignee?.display ?? a.assignee?.ref ?? "Chưa giao"}</dd>
                {a.result ? (
                  <>
                    <dt>Kết quả</dt>
                    <dd>{a.result}</dd>
                  </>
                ) : null}
              </dl>
              {fs.length ? (
                <table className="dt" aria-label="Vi phạm">
                  <thead>
                    <tr>
                      <th>Chỉ tiêu</th>
                      <th>Kỳ này</th>
                      <th>So sánh</th>
                      <th>Biến động</th>
                      <th>Lý do</th>
                    </tr>
                  </thead>
                  <tbody>
                    {fs.map((f) => (
                      <tr key={f.field}>
                        <td>{f.label}</td>
                        <td>{f.current ?? "—"}</td>
                        <td>{f.compare ?? "—"}</td>
                        <td>{f.new_entry ? "Phát sinh mới" : f.change_pct ? `${f.change_pct}%` : "—"}</td>
                        <td>{f.reason}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : reasonOf(a) ? (
                <p>{reasonOf(a)}</p>
              ) : null}

              {a.can_assign ? (
                <div className="row" aria-label="Giao xử lý">
                  <select className="select" aria-label="Người xử lý" value={assignee} onChange={(e) => setAssignee(e.target.value)}>
                    <option value="">Chọn người xử lý…</option>
                    {(people.data ?? []).map((p) => (
                      <option key={p.actor.ref} value={p.actor.ref}>
                        {p.actor.display ?? p.actor.ref} {p.level === "commune" ? "(Cấp Xã)" : p.manager ? "(Lãnh đạo phòng)" : "(Cấp Tỉnh)"}
                      </option>
                    ))}
                  </select>
                  <label className="row small">
                    Hạn xử lý <input className="input" type="datetime-local" aria-label="Hạn xử lý" value={due} onChange={(e) => setDue(e.target.value)} />
                  </label>
                  <input className="input" aria-label="Ghi chú giao" placeholder="Ghi chú (tùy chọn)" value={note} onChange={(e) => setNote(e.target.value)} />
                  <button className="btn btn-primary btn-sm" disabled={!assignee || act.busy}
                    onClick={async () => done(await act.run(() => aapi.assign(a.alert_ref, a.seq, assignee, due ? new Date(due).toISOString() : undefined, note.trim() || undefined), "Đã giao xử lý."))}>
                    Giao xử lý
                  </button>
                </div>
              ) : null}
              {a.can_start ? (
                <button className="btn btn-sm" disabled={act.busy} onClick={async () => done(await act.run(() => aapi.start(a.alert_ref, a.seq), "Đã nhận xử lý."))}>
                  Nhận xử lý
                </button>
              ) : null}
              {a.can_close ? (
                <div className="row" aria-label="Đóng cảnh báo">
                  <input className="input" aria-label="Kết quả xử lý" placeholder="Kết quả xử lý (bắt buộc, ≥ 10 ký tự)" value={result}
                    onChange={(e) => setResult(e.target.value)} />
                  <button className="btn btn-sm" disabled={[...result.trim()].length < 10 || act.busy}
                    onClick={async () => done(await act.run(() => aapi.close(a.alert_ref, a.seq, result.trim()), "Đã đóng cảnh báo."))}>
                    Đóng cảnh báo
                  </button>
                </div>
              ) : null}

              <h3>Lịch sử</h3>
              <ol className="small">
                {a.history.map((h) => (
                  <li key={h.seq}>
                    {dt(h.event_time)} — {ACTION_LABEL[h.action]} · {h.actor.display ?? "Hệ thống"}
                    {h.assignee ? ` → ${h.assignee.display ?? h.assignee.ref}` : ""}
                    {h.note ? `: ${h.note}` : ""}
                  </li>
                ))}
              </ol>
              <button className="btn btn-sm" onClick={onClose}>
                Đóng chi tiết
              </button>
            </>
          );
        }}
      </LoadState>
    </section>
  );
}

function ReminderConfig({ aapi, canEdit }: { aapi: AlertApi; canEdit: boolean }) {
  const cfg = useLoad(() => aapi.config(), [aapi]);
  const [edit, setEdit] = useState<{ code: string; r: FormReminder } | null>(null);
  const act = useAction();
  const errs = edit ? reminderErrors(edit.r) : {};
  const set = (patch: Partial<FormReminder>) => setEdit((e) => (e ? { ...e, r: { ...e.r, ...patch } } : e));
  return (
    <section className="card sec" aria-label="Cấu hình nhắc nhở">
      <h2>Cấu hình nhắc nhở theo biểu</h2>
      <p className="mute small">
        Mặc định: nhắc trước hạn 1 ngày, nhắc ngay khi trễ, nhắc lặp mỗi ngày đến khi gửi hoặc chốt kỳ; trễ liên tiếp 2 kỳ (BM-04, BM-17, BM-05) hoặc 3 kỳ
        (BM-01) báo người phụ trách biểu và Lãnh đạo phòng.
      </p>
      {act.view}
      <LoadState s={cfg}>
        {(c: AlertConfig) => (
          <table className="dt" aria-label="Nhắc nhở theo biểu">
            <thead>
              <tr>
                <th>Biểu</th>
                <th>Trước hạn</th>
                <th>Khi trễ</th>
                <th>Nhắc lặp</th>
                <th>Trễ liên tiếp</th>
                <th>Kênh</th>
                <th>Đã nhắc hôm nay</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {c.forms.map((f) => (
                <tr key={f.code}>
                  <td>
                    {f.label}
                    {f.custom ? <span className="mute small"> · phiên bản {f.version}</span> : <span className="mute small"> · mặc định</span>}
                  </td>
                  <td>{f.reminder.before_due_hours ? `${f.reminder.before_due_hours} giờ` : "Không"}</td>
                  <td>{f.reminder.on_overdue ? "Có" : "Không"}</td>
                  <td>{f.reminder.repeat_daily ? `Mỗi ngày ${f.reminder.repeat_at}` : "Không"}</td>
                  <td>{f.reminder.late_streak ? `${f.reminder.late_streak} kỳ` : "Tắt"}</td>
                  <td>{f.reminder.channels.map((ch) => CHANNEL_LABEL[ch]).join(", ")}</td>
                  <td>{f.sent_today}</td>
                  <td>
                    {canEdit ? (
                      <button className="btn btn-sm" onClick={() => setEdit({ code: f.code, r: { ...f.reminder } })}>
                        Sửa
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </LoadState>
      {edit ? (
        <div className="card sec" aria-label="Sửa cấu hình nhắc">
          <h3>Sửa nhắc nhở — {edit.code}</h3>
          <div className="row">
            <label className="row small">
              Nhắc trước hạn (giờ){" "}
              <input className="input" type="number" min={0} max={168} aria-label="Nhắc trước hạn (giờ)" value={edit.r.before_due_hours}
                onChange={(e) => set({ before_due_hours: Number(e.target.value) })} />
            </label>
            <label className="row small">
              <input type="checkbox" checked={edit.r.on_overdue} onChange={(e) => set({ on_overdue: e.target.checked })} /> Nhắc khi trễ hạn
            </label>
            <label className="row small">
              <input type="checkbox" checked={edit.r.repeat_daily} onChange={(e) => set({ repeat_daily: e.target.checked })} /> Nhắc lặp mỗi ngày lúc
              <input className="input" aria-label="Giờ nhắc lặp" value={edit.r.repeat_at} onChange={(e) => set({ repeat_at: e.target.value })} />
            </label>
            <label className="row small">
              Trễ liên tiếp từ{" "}
              <input className="input" type="number" min={0} max={12} aria-label="Số kỳ trễ liên tiếp" value={edit.r.late_streak}
                onChange={(e) => set({ late_streak: Number(e.target.value) })} />{" "}
              kỳ
            </label>
          </div>
          <div className="row">
            {(["in_app", "push", "email"] as const).map((ch) => (
              <label key={ch} className="row small">
                <input type="checkbox" disabled={ch === "in_app"} checked={edit.r.channels.includes(ch)}
                  onChange={(e) => set({ channels: e.target.checked ? [...edit.r.channels, ch] : edit.r.channels.filter((x) => x !== ch) })} />{" "}
                {CHANNEL_LABEL[ch]}
              </label>
            ))}
          </div>
          <label className="small">
            Nội dung mẫu ({"{bieu}"}, {"{ky}"}, {"{han}"})
            <textarea className="input" aria-label="Nội dung mẫu" value={edit.r.message} onChange={(e) => set({ message: e.target.value })} />
          </label>
          {Object.entries(errs).map(([k, v]) => (
            <p key={k} className="err small">
              {k}: {v}
            </p>
          ))}
          <div className="row">
            <button className="btn btn-primary btn-sm" disabled={Object.keys(errs).length > 0 || act.busy}
              onClick={async () => {
                const r = await act.run(() => aapi.putForm(edit.code, edit.r), "Đã lưu cấu hình nhắc nhở.");
                if (r) {
                  setEdit(null);
                  cfg.reload();
                }
              }}>
              Lưu
            </button>
            <button className="btn btn-sm" onClick={() => setEdit(null)}>
              Hủy
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function KindSettings({ aapi, canEdit }: { aapi: AlertApi; canEdit: boolean }) {
  const cfg = useLoad(() => aapi.config(), [aapi]);
  const act = useAction();
  const [sla, setSla] = useState<Record<string, string>>({});
  return (
    <>
      <section className="card sec" aria-label="Xử lý theo loại cảnh báo">
        <h2>Loại cảnh báo và hạn xử lý</h2>
        {act.view}
        <LoadState s={cfg}>
          {(c) => (
            <table className="dt" aria-label="Loại cảnh báo">
              <thead>
                <tr>
                  <th>#</th>
                  <th>Loại</th>
                  <th>Mức</th>
                  <th>Gửi tới</th>
                  <th>Nguồn</th>
                  <th>Hạn xử lý (giờ)</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {c.kinds.map((k) => (
                  <tr key={k.kind.code}>
                    <td>{k.kind.no || "—"}</td>
                    <td>{k.kind.label}</td>
                    <td>
                      {k.kind.level_configurable && canEdit ? (
                        <label className="row small">
                          <input type="checkbox" checked={k.urgent}
                            onChange={async (e) => {
                              const r = await act.run(() => aapi.putKind(k.kind.code, { sla_hours: k.sla_hours, urgent: e.target.checked }), "Đã lưu mức cảnh báo.");
                              if (r) cfg.reload();
                            }} />{" "}
                          Khẩn
                        </label>
                      ) : (
                        LEVEL_LABEL[k.level]
                      )}
                    </td>
                    <td>{k.kind.recipients.map((r) => RECIPIENT_LABEL[r] ?? r).join(", ")}</td>
                    <td className="small mute">{k.kind.source === "log" || k.kind.source === "schedule" ? "Chờ nhật ký / lịch tiêm" : "Đang chạy"}</td>
                    <td>
                      <input className="input" type="number" min={1} max={720} aria-label={`Hạn xử lý ${k.kind.label}`} disabled={!canEdit}
                        value={sla[k.kind.code] ?? String(k.sla_hours)} onChange={(e) => setSla({ ...sla, [k.kind.code]: e.target.value })} />
                    </td>
                    <td>
                      {canEdit && sla[k.kind.code] !== undefined && sla[k.kind.code] !== String(k.sla_hours) ? (
                        <button className="btn btn-sm" disabled={act.busy}
                          onClick={async () => {
                            const r = await act.run(() => aapi.putKind(k.kind.code, { sla_hours: Number(sla[k.kind.code]), urgent: k.urgent }), "Đã lưu hạn xử lý.");
                            if (r) cfg.reload();
                          }}>
                          Lưu
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </LoadState>
      </section>
      <section className="card sec" aria-label="Ngưỡng">
        <h2>Ngưỡng đang áp dụng</h2>
        <p className="mute small">Ngưỡng là tham số có phiên bản (người đặt ≠ người duyệt) — sửa ở mục Tham số; ngưỡng mới áp dụng cho số liệu nhập sau thời điểm hiệu lực.</p>
        <LoadState s={cfg}>
          {(c) => (
            <table className="dt" aria-label="Bảng ngưỡng">
              <thead>
                <tr>
                  <th>Ngưỡng</th>
                  <th>Giá trị</th>
                  <th>Hiệu lực từ</th>
                </tr>
              </thead>
              <tbody>
                {c.thresholds.map((t) => (
                  <tr key={t.key}>
                    <td>
                      {t.label} <span className="mute small">({t.key})</span>
                    </td>
                    <td>{t.value === null ? "Chưa đặt" : `${t.value.replace(".", ",")} ${t.unit}`}</td>
                    <td>{t.effective_from ? t.effective_from.split("-").reverse().join("/") : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </LoadState>
      </section>
    </>
  );
}
