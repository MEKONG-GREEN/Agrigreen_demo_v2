"use client";

// Kỳ báo cáo & phiếu các phòng — Phòng KHTC (W11.62, BRD KHTC v2.2 FN-TI-02.03, Hình 8–10): danh sách kỳ, popup "Mở kỳ"
// (ô chọn Loại / Năm / Tuần·Tháng·Quý, hệ thống tự điền khoảng thời gian, tên kỳ, hạn; số lần gửi tối đa chỉ đọc), chi
// tiết kỳ (4 thẻ đếm, tiến độ phiếu các phòng và nguồn module, log), Sửa hạn nộp, Đóng kỳ, Hủy kỳ. Phòng nộp phiếu ở
// module của phòng (/v1/forms, profile kn_khtc).
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
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

const KHTC = "ke_hoach_tai_chinh";

/** ISO → giá trị ô datetime-local theo giờ Việt Nam. */
function toLocal(iso: string | null | undefined): string {
  return iso
    ? new Date(Date.parse(iso) + 7 * 3_600_000).toISOString().slice(0, 16)
    : "";
}
/** Ô datetime-local (giờ Việt Nam) → ISO. */
function fromLocal(v: string): string | undefined {
  return v ? new Date(`${v}:00+07:00`).toISOString() : undefined;
}

export function KhtcPeriodsScreen() {
  const { api, kn } = useAuth();
  const ctx = kn.context;
  const role = ctx?.role ?? null;
  const writer =
    role === "kn_tinh" && ctx?.department_code === KHTC && !ctx?.read_only;
  const kapi = useMemo(() => (api ? createKhtcPeriodApi(api) : null), [api]);
  const thisYear = new Date().getFullYear();
  const [year, setYear] = useState(thisYear);
  const periods = useLoad(kapi ? () => kapi.list(year) : null, [kapi, year]);
  const template = useLoad(kapi ? () => kapi.template() : null, [kapi]);
  const weeks = useLoad(kapi ? () => kapi.weeks(year) : null, [kapi, year]);
  const [selected, setSelected] = useState<string | null>(null);
  const detail = useLoad(kapi && selected ? () => kapi.get(selected) : null, [
    kapi,
    selected,
  ]);
  const act = useAction();

  // Popup "Mở kỳ"
  const [opening, setOpening] = useState(false);
  const [type, setType] = useState<KhtcReportType>("week");
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
    setDepts(null);
    setCutoff("");
    setDue("");
    setExportDue("");
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
      <PageHeader title="Kỳ báo cáo & phiếu các phòng">
        <p className="mute">
          Mở kỳ bằng ô chọn, hệ thống tự điền khoảng thời gian, tên kỳ và hạn
          (tuần báo cáo Thứ Tư – Thứ Ba). Mỗi kỳ, mỗi phòng một phiếu “Báo cáo
          cho KHTC”.
        </p>
      </PageHeader>
      {act.view}

      <section className="card sec">
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
      </section>

      <section className="card sec">
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
          {(ps: KhtcPeriod[]) =>
            ps.length === 0 ? (
              <p className="mute">Chưa có kỳ nào trong năm {year}.</p>
            ) : (
              <table className="dt">
                <thead>
                  <tr>
                    <th>Loại báo cáo</th>
                    <th>Kỳ</th>
                    <th>Ngày chốt</th>
                    <th>Hạn nộp</th>
                    <th>Phòng tham gia</th>
                    <th>Tiến độ nộp</th>
                    <th>Trạng thái</th>
                  </tr>
                </thead>
                <tbody>
                  {ps.map((p) => (
                    <tr
                      key={p.id}
                      className={p.id === selected ? "sel" : undefined}
                      onClick={() => setSelected(p.id)}
                      style={{ cursor: "pointer" }}
                    >
                      <td>
                        {REPORT_TYPES.find((t) => t.value === p.report_type)
                          ?.label ?? p.report_type}
                      </td>
                      <td>{p.name}</td>
                      <td>{vnDate(p.cutoff_date)}</td>
                      <td>{dt(p.due_at)}</td>
                      <td>{p.departments.length}</td>
                      <td>{progressText(p)}</td>
                      <td>
                        <StatusPill tone={PERIOD_TONE[p.state]}>
                          {p.state_label}
                        </StatusPill>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          }
        </LoadState>
      </section>

      {opening && writer ? (
        <section className="card sec" aria-label="Mở kỳ">
          <h2>Mở kỳ</h2>
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
                {REPORT_TYPES.map((t) => (
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
                Đến ngày{" "}
                <input
                  type="date"
                  aria-label="Đến ngày"
                  value={asOf}
                  onChange={(e) => (setAsOf(e.target.value), reset())}
                />
              </label>
            ) : null}
            {type === "adhoc" ? (
              <>
                <label>
                  Tên báo cáo{" "}
                  <input
                    aria-label="Tên báo cáo"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </label>
                <label>
                  Từ ngày{" "}
                  <input
                    type="date"
                    aria-label="Từ ngày"
                    value={from}
                    onChange={(e) => setFrom(e.target.value)}
                  />
                </label>
                <label>
                  Đến ngày{" "}
                  <input
                    type="date"
                    aria-label="Đến ngày phát sinh"
                    value={to}
                    onChange={(e) => setTo(e.target.value)}
                  />
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
                value={cutoff || draft?.cutoff_date || ""}
                onChange={(e) => setCutoff(e.target.value)}
              />
            </label>
            <label>
              Hạn nộp{" "}
              <input
                type="datetime-local"
                aria-label="Hạn nộp"
                value={due || toLocal(draft?.due_at)}
                onChange={(e) => setDue(e.target.value)}
              />
            </label>
            <label>
              Hạn xuất{" "}
              <input
                type="datetime-local"
                aria-label="Hạn xuất"
                value={exportDue || toLocal(draft?.export_due_at)}
                onChange={(e) => setExportDue(e.target.value)}
              />
            </label>
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
              onClick={async () =>
                setDraft((await act.run(() => kapi.preview(body()))) ?? null)
              }
            >
              Xem trước
            </button>
            <button
              className="btn"
              onClick={() => (setOpening(false), reset())}
            >
              Đóng
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
                  const p = await act.run(
                    () => kapi.open(body()),
                    (r) =>
                      `Đã mở kỳ “${r.name}” và giao ${r.departments.length} phiếu.`,
                  );
                  if (p) {
                    setOpening(false);
                    reset();
                    setSelected(p.id);
                    periods.reload();
                  }
                }}
              >
                Mở kỳ và giao phiếu
              </button>
            </div>
          ) : null}
        </section>
      ) : null}

      {selected ? (
        <section className="card sec" aria-label="Chi tiết kỳ">
          <LoadState s={detail}>
            {(p: KhtcPeriod) => {
              const done = async (r: KhtcPeriod | null) => {
                if (r) {
                  detail.reload();
                  periods.reload();
                }
              };
              return (
                <>
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
                    {vnDate(p.cutoff_date)} · Hạn nộp {dt(p.due_at)} · Hạn xuất{" "}
                    {dt(p.export_due_at)} · Tối đa {p.max_submissions} lần gửi
                  </p>
                  <h3>Tiến độ nộp phiếu và nguồn module</h3>
                  <table className="dt">
                    <thead>
                      <tr>
                        <th>Phòng / nguồn</th>
                        <th>Trạng thái</th>
                        <th>Lần gửi</th>
                        <th>Gửi lúc</th>
                        <th>Ghi chú</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {p.forms.map((f) => (
                        <tr key={f.department}>
                          <td>{f.department_name || f.department}</td>
                          <td>
                            <StatusPill tone={FORM_TONE[f.state]}>
                              {f.state_label}
                            </StatusPill>
                          </td>
                          <td>
                            {f.attempt
                              ? `${f.attempt}/${p.max_submissions || "∞"}`
                              : "—"}
                            {f.limit_reached
                              ? " (đã khóa yêu cầu bổ sung)"
                              : ""}
                          </td>
                          <td>{dt(f.last_submitted_at)}</td>
                          <td>{f.review_note}</td>
                          <td>
                            {f.attempt ? (
                              <Link
                                className="btn"
                                href={`/kn/khtc/ra-soat/${p.id}?department=${f.department}`}
                              >
                                Rà soát
                              </Link>
                            ) : null}
                          </td>
                        </tr>
                      ))}
                      {p.sources.map((s) => (
                        <tr key={`${s.source}:${s.native_key}`}>
                          <td>
                            {s.label || s.source} ({s.department}, kỳ nguồn{" "}
                            {s.native_key})
                          </td>
                          <td>
                            <StatusPill
                              tone={
                                !s.known ? "warn" : s.closed ? "ok" : "info"
                              }
                            >
                              {!s.known
                                ? "Không đọc được"
                                : s.closed
                                  ? "Đã chốt"
                                  : "Chưa chốt"}
                            </StatusPill>
                          </td>
                          <td>—</td>
                          <td>{dt(s.closed_at)}</td>
                          <td>
                            {s.calendar_id} {s.calendar_version}
                          </td>
                          <td />
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <KhtcSourcesPanel periodId={p.id} />
                  <h3>Log</h3>
                  <ul aria-label="Log kỳ">
                    <li>
                      {dt(p.opened_at)} — Mở kỳ và giao {p.departments.length}{" "}
                      phiếu
                    </li>
                    {p.events.map((e) => (
                      <li key={e.id}>
                        {dt(e.at)} —{" "}
                        {e.action === "deadline_changed"
                          ? `Sửa hạn nộp → ${dt(e.due_at)}`
                          : e.action === "closed"
                            ? e.forced
                              ? "Đóng kỳ (ngoại lệ)"
                              : "Đóng kỳ"
                            : "Hủy kỳ"}
                        {e.reason ? `: ${e.reason}` : ""}
                      </li>
                    ))}
                  </ul>
                </>
              );
            }}
          </LoadState>
        </section>
      ) : null}
    </div>
  );
}
