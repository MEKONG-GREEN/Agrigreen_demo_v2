"use client";

// Cấu hình Báo giá W11.74 (BRD Báo giá v1.3): FN-AD-04.04 Kỳ báo giá & giờ khóa + Lịch ngoại lệ, FN-AD-04.05 Nhắc nhập
// giá, FN-AD-04.06 Ngưỡng cảnh báo; nhật ký job khóa/nhắc. Đọc: mọi cấp KN; ghi: Admin KN (QTNV báo giá ở W11.72).
// Mọi lần lưu là một phiên bản mới (sổ chỉ ghi thêm ở pf/price); số % là chuỗi decimal.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { RelatedTabs } from "@/components/patterns/detail";
import {
  DataTable,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import {
  createPriceConfigApi,
  DAY_KIND_LABEL,
  renderTemplate,
  RUN_KIND_LABEL,
  scheduleProblems,
  validPct,
  WEEKDAYS,
  type PriceCalendar,
  type PriceCalendarDay,
  type PriceCalendarRun,
  type PriceScheduleInput,
  type PriceThreshold,
  type PriceThresholdInput,
  type PriceThresholds,
} from "./api";

const pad = (n: number) => String(n).padStart(2, "0");
/** "dd/mm/yyyy" từ yyyy-mm-dd. */
export function dmy(day: string): string {
  const [y, m, d] = day.split("-");
  return `${d}/${m}/${y}`;
}
/** "HH:MM" giờ Việt Nam từ ISO. */
function hm(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(Date.parse(iso) + 7 * 3_600_000);
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}
const WD_LABEL = Object.fromEntries(
  WEEKDAYS.map((w) => [w.code, w.label]),
) as Record<string, string>;

function dayTone(d: PriceCalendarDay) {
  return d.kind === "regular"
    ? "ok"
    : d.kind === "extra"
      ? "info"
      : d.kind === "holiday"
        ? "warn"
        : "neutral";
}

export function PriceCalendarScreen() {
  const { api } = useAuth();
  const priceApi = useMemo(() => createPriceConfigApi(api), [api]);
  const act = useAction();
  const cal = useLoad(() => priceApi.calendar(), [act.note]);
  const th = useLoad(() => priceApi.thresholds(), [act.note]);
  const runs = useLoad(() => priceApi.runs(), [act.note]);
  return (
    <>
      <PageHeader title="Lịch báo giá, giờ khóa, nhắc nhập giá, ngưỡng cảnh báo">
        Báo giá nông sản — Phòng Thông tin – Truyền thông
      </PageHeader>
      {act.view}
      <LoadState s={cal}>
        {(c) => (
          <RelatedTabs
            label="Cấu hình báo giá"
            tabs={[
              {
                id: "lich",
                label: "Kỳ báo giá & giờ khóa",
                content: (
                  <ScheduleTab c={c} act={act} save={priceApi.saveSchedule} />
                ),
              },
              {
                id: "ngoai-le",
                label: "Lịch ngoại lệ",
                content: (
                  <ExceptionsTab
                    c={c}
                    act={act}
                    add={priceApi.addException}
                    retire={priceApi.retireException}
                  />
                ),
              },
              {
                id: "nhac",
                label: "Nhắc nhập giá",
                content: (
                  <ReminderTab
                    c={c}
                    act={act}
                    save={priceApi.saveSchedule}
                    test={priceApi.reminderTest}
                    runs={runs.data?.items ?? []}
                  />
                ),
              },
              {
                id: "nguong",
                label: "Ngưỡng cảnh báo",
                content: (
                  <LoadState s={th}>
                    {(t) => (
                      <ThresholdsTab
                        t={t}
                        act={act}
                        save={priceApi.saveThreshold}
                        retire={priceApi.retireThreshold}
                      />
                    )}
                  </LoadState>
                ),
              },
              {
                id: "lich-su",
                label: "Lịch sử thay đổi",
                content: <HistoryTab c={c} runs={runs.data?.items ?? []} />,
              },
            ]}
          />
        )}
      </LoadState>
    </>
  );
}

type Act = ReturnType<typeof useAction>;

function toInput(c: PriceCalendar, validFrom: string): PriceScheduleInput {
  const s = c.upcoming.length ? c.upcoming[c.upcoming.length - 1]! : c.current;
  return {
    weekdays: [...s.weekdays],
    lock_at: s.lock_at,
    respond_by: s.respond_by,
    remind1_at: s.remind1_at,
    remind2_at: s.remind2_at,
    channels: [...s.channels],
    template: s.template,
    valid_from: validFrom,
  };
}

function ScheduleTab({
  c,
  act,
  save,
}: {
  c: PriceCalendar;
  act: Act;
  save: (b: PriceScheduleInput) => Promise<unknown>;
}) {
  const [form, setForm] = useState<PriceScheduleInput>(() =>
    toInput(c, c.earliest_valid_from),
  );
  const [page, setPage] = useState(1);
  const problems = scheduleProblems(form).filter(
    (p) => !p.includes("nhắc") && !p.includes("kênh") && !p.includes("mẫu"),
  );
  const tooEarly = form.valid_from < c.earliest_valid_from;
  const columns: Column<PriceCalendarDay>[] = [
    {
      key: "day",
      header: "Ngày",
      cell: (d) => `${WD_LABEL[d.weekday]} ${dmy(d.day)}`,
    },
    {
      key: "kind",
      header: "Loại",
      cell: (d) => (
        <StatusPill tone={dayTone(d)}>{DAY_KIND_LABEL[d.kind]}</StatusPill>
      ),
    },
    { key: "lock", header: "Giờ khóa", cell: (d) => hm(d.lock_at) },
    { key: "respond", header: "Hạn phản hồi", cell: (d) => hm(d.respond_by) },
    { key: "reason", header: "Lý do", cell: (d) => d.reason || "—" },
  ];
  return (
    <div className="sec">
      <div className="card sec" aria-label="Lịch báo giá hằng ngày">
        <h2>Lịch báo giá hằng ngày</h2>
        <p className="mute">
          Đang áp dụng: {c.current.weekdays.map((w) => WD_LABEL[w]).join(", ")}{" "}
          · khóa {c.current.lock_at} · hạn phản hồi {c.current.respond_by}
          {c.current.valid_from
            ? ` (từ ${dmy(c.current.valid_from)})`
            : " (mặc định)"}
          . Hôm nay: {DAY_KIND_LABEL[c.today.kind]}
          {c.today.quote
            ? c.locked
              ? " — đã khóa nhập."
              : ` — khóa lúc ${hm(c.today.lock_at)}.`
            : "."}
        </p>
        {c.upcoming.map((u) => (
          <p key={u.version} className="mute">
            Đã lên lịch: phiên bản {u.version} từ{" "}
            {u.valid_from ? dmy(u.valid_from) : "—"} — khóa {u.lock_at}, hạn
            phản hồi {u.respond_by}.
          </p>
        ))}
        {c.can_manage ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void act.run(
                () => save(form),
                "Đã lưu lịch báo giá — áp dụng từ " +
                  dmy(form.valid_from) +
                  ".",
              );
            }}
          >
            <fieldset className="field">
              <legend className="req">Ngày báo giá trong tuần</legend>
              {WEEKDAYS.map((w) => (
                <label key={w.code} style={{ marginRight: 12 }}>
                  <input
                    type="checkbox"
                    checked={form.weekdays.includes(w.code)}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        weekdays: e.target.checked
                          ? [...form.weekdays, w.code]
                          : form.weekdays.filter((x) => x !== w.code),
                      })
                    }
                  />{" "}
                  {w.label}
                </label>
              ))}
            </fieldset>
            <div className="ws-two">
              <div className="field">
                <label htmlFor="pc-lock" className="req">
                  Giờ khóa nhập
                </label>
                <input
                  id="pc-lock"
                  type="time"
                  className="input"
                  value={form.lock_at}
                  onChange={(e) =>
                    setForm({ ...form, lock_at: e.target.value })
                  }
                />
              </div>
              <div className="field">
                <label htmlFor="pc-respond" className="req">
                  Hạn phản hồi xác minh
                </label>
                <input
                  id="pc-respond"
                  type="time"
                  className="input"
                  value={form.respond_by}
                  onChange={(e) =>
                    setForm({ ...form, respond_by: e.target.value })
                  }
                />
              </div>
              <div className="field">
                <label htmlFor="pc-from" className="req">
                  Ngày áp dụng
                </label>
                <input
                  id="pc-from"
                  type="date"
                  className="input"
                  min={c.earliest_valid_from}
                  value={form.valid_from}
                  onChange={(e) =>
                    setForm({ ...form, valid_from: e.target.value })
                  }
                />
              </div>
            </div>
            {[
              ...problems,
              ...(tooEarly
                ? [
                    `Ngày áp dụng sớm nhất ${dmy(c.earliest_valid_from)} (đổi trong ngày báo giá chỉ áp dụng từ ngày kế tiếp).`,
                  ]
                : []),
            ].map((p) => (
              <p key={p} className="err" role="alert">
                {p}
              </p>
            ))}
            <button
              type="submit"
              className="btn btn-primary"
              disabled={act.busy || problems.length > 0 || tooEarly}
            >
              Lưu cấu hình
            </button>
          </form>
        ) : (
          <p className="mute">
            Chỉ xem — cấu hình do Quản trị nghiệp vụ báo giá thực hiện.
          </p>
        )}
      </div>
      <DataTable
        caption="Lịch khóa nhập 30 ngày tới"
        columns={columns}
        rows={c.days.slice((page - 1) * 10, page * 10)}
        rowKey={(d) => d.day}
        page={page}
        pageSize={10}
        total={c.days.length}
        onPage={setPage}
      />
    </div>
  );
}

function ExceptionsTab({
  c,
  act,
  add,
  retire,
}: {
  c: PriceCalendar;
  act: Act;
  add: (b: {
    day: string;
    kind: "holiday" | "extra";
    reason: string;
  }) => Promise<unknown>;
  retire: (day: string) => Promise<unknown>;
}) {
  const [day, setDay] = useState("");
  const [kind, setKind] = useState<"holiday" | "extra">("holiday");
  const [reason, setReason] = useState("");
  const tomorrow = c.days[1]?.day ?? "";
  return (
    <div className="card sec" aria-label="Lịch ngoại lệ">
      <h2>Lịch ngoại lệ</h2>
      <p className="mute">
        Ngày nghỉ không sinh kỳ báo giá, không gửi nhắc, không tính phiếu phải
        nộp. Chỉ thêm/xóa được ngày từ ngày mai.
      </p>
      <table className="dt">
        <thead>
          <tr>
            <th>Ngày</th>
            <th>Loại</th>
            <th>Lý do</th>
            {c.can_manage ? <th>Thao tác</th> : null}
          </tr>
        </thead>
        <tbody>
          {c.exceptions.length === 0 ? (
            <tr>
              <td colSpan={4} className="mute">
                Chưa có ngày ngoại lệ.
              </td>
            </tr>
          ) : (
            c.exceptions.map((e) => (
              <tr key={e.day}>
                <td>{dmy(e.day)}</td>
                <td>
                  {e.kind === "holiday" ? "Ngày nghỉ" : "Ngày báo giá bổ sung"}
                </td>
                <td>{e.reason}</td>
                {c.can_manage ? (
                  <td>
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={act.busy || e.day < tomorrow}
                      onClick={() =>
                        void act.run(
                          () => retire(e.day),
                          `Đã xóa ngoại lệ ${dmy(e.day)}.`,
                        )
                      }
                    >
                      Xóa
                    </button>
                  </td>
                ) : null}
              </tr>
            ))
          )}
        </tbody>
      </table>
      {c.can_manage ? (
        <form
          className="ws-two"
          onSubmit={(e) => {
            e.preventDefault();
            void act.run(
              () => add({ day, kind, reason }),
              `Đã thêm ${kind === "holiday" ? "ngày nghỉ" : "ngày báo giá bổ sung"} ${dmy(day)}.`,
            );
          }}
        >
          <div className="field">
            <label htmlFor="pe-day" className="req">
              Ngày
            </label>
            <input
              id="pe-day"
              type="date"
              className="input"
              min={tomorrow}
              value={day}
              onChange={(e) => setDay(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="pe-kind">Loại</label>
            <select
              id="pe-kind"
              className="select"
              value={kind}
              onChange={(e) => setKind(e.target.value as "holiday" | "extra")}
            >
              <option value="holiday">Ngày nghỉ</option>
              <option value="extra">Ngày báo giá bổ sung</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="pe-reason" className="req">
              Lý do
            </label>
            <input
              id="pe-reason"
              className="input"
              maxLength={300}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </div>
          <div className="field">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={act.busy || !day || !reason.trim() || day < tomorrow}
            >
              Thêm ngoại lệ
            </button>
          </div>
        </form>
      ) : null}
    </div>
  );
}

function ReminderTab({
  c,
  act,
  save,
  test,
  runs,
}: {
  c: PriceCalendar;
  act: Act;
  save: (b: PriceScheduleInput) => Promise<unknown>;
  test: () => Promise<{ body: string }>;
  runs: PriceCalendarRun[];
}) {
  const [form, setForm] = useState<PriceScheduleInput>(() =>
    toInput(c, c.earliest_valid_from),
  );
  const problems = scheduleProblems(form);
  const preview = renderTemplate(
    form.template,
    c.today.day,
    "Xã Mỹ Hòa Hưng",
    form.lock_at,
  );
  const reminders = runs.filter((r) => r.kind !== "lock");
  return (
    <div className="sec">
      <div className="card sec" aria-label="Lịch nhắc">
        <h2>Nhắc nhập giá</h2>
        <p className="mute">
          Lần 1 buổi sáng ngày báo giá; lần 2 trước giờ khóa ({form.lock_at}),
          chỉ xã chưa gửi. Thay đổi áp dụng từ ngày báo giá tiếp theo (
          {dmy(c.earliest_valid_from)}).
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void act.run(
              () => save({ ...form, valid_from: c.earliest_valid_from }),
              "Đã lưu cấu hình nhắc nhập giá.",
            );
          }}
        >
          <div className="ws-two">
            <div className="field">
              <label htmlFor="pr-1" className="req">
                Giờ nhắc lần 1
              </label>
              <input
                id="pr-1"
                type="time"
                className="input"
                disabled={!c.can_manage}
                value={form.remind1_at}
                onChange={(e) =>
                  setForm({ ...form, remind1_at: e.target.value })
                }
              />
            </div>
            <div className="field">
              <label htmlFor="pr-2" className="req">
                Giờ nhắc lần 2
              </label>
              <input
                id="pr-2"
                type="time"
                className="input"
                disabled={!c.can_manage}
                value={form.remind2_at}
                onChange={(e) =>
                  setForm({ ...form, remind2_at: e.target.value })
                }
              />
            </div>
          </div>
          <fieldset className="field">
            <legend className="req">Kênh</legend>
            {(
              [
                ["in_app", "Trong ứng dụng"],
                ["email", "Email"],
              ] as const
            ).map(([code, label]) => (
              <label key={code} style={{ marginRight: 12 }}>
                <input
                  type="checkbox"
                  disabled={!c.can_manage}
                  checked={form.channels.includes(code)}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      channels: e.target.checked
                        ? [...form.channels, code]
                        : form.channels.filter((x) => x !== code),
                    })
                  }
                />{" "}
                {label}
              </label>
            ))}
          </fieldset>
          <div className="field">
            <label htmlFor="pr-tpl" className="req">
              Nội dung mẫu (biến {"{ngày}"}, {"{xã}"}, {"{giờ khóa}"})
            </label>
            <textarea
              id="pr-tpl"
              className="input"
              rows={3}
              maxLength={500}
              disabled={!c.can_manage}
              value={form.template}
              onChange={(e) => setForm({ ...form, template: e.target.value })}
            />
            <p className="mute">Xem trước: {preview}</p>
          </div>
          {problems.map((p) => (
            <p key={p} className="err" role="alert">
              {p}
            </p>
          ))}
          {c.can_manage ? (
            <div className="row">
              <button
                type="submit"
                className="btn btn-primary"
                disabled={act.busy || problems.length > 0}
              >
                Lưu cấu hình
              </button>
              <button
                type="button"
                className="btn"
                disabled={act.busy}
                onClick={() =>
                  void act.run(
                    () => test(),
                    (r) => `Đã gửi thử tới tài khoản của bạn: “${r.body}”`,
                  )
                }
              >
                Gửi thử
              </button>
            </div>
          ) : null}
        </form>
      </div>
      <div className="card sec" aria-label="Lịch sử gửi nhắc">
        <h2>Lịch sử gửi nhắc</h2>
        <RunsTable runs={reminders} />
      </div>
    </div>
  );
}

function RunsTable({ runs }: { runs: PriceCalendarRun[] }) {
  return (
    <table className="dt">
      <thead>
        <tr>
          <th>Ngày</th>
          <th>Loại</th>
          <th>Theo lịch</th>
          <th>Thực tế</th>
          <th>Người nhận</th>
          <th>Ghi chú</th>
        </tr>
      </thead>
      <tbody>
        {runs.length === 0 ? (
          <tr>
            <td colSpan={6} className="mute">
              Chưa có lần chạy nào.
            </td>
          </tr>
        ) : (
          runs.map((r) => (
            <tr key={`${r.day}-${r.kind}`}>
              <td>{dmy(r.day)}</td>
              <td>{RUN_KIND_LABEL[r.kind]}</td>
              <td>{hm(r.scheduled_at)}</td>
              <td>
                {dt(r.ran_at)}
                {r.lag_seconds > 60 ? (
                  <StatusPill tone="bad">
                    {" "}
                    trễ {Math.round(r.lag_seconds / 60)} phút
                  </StatusPill>
                ) : null}
              </td>
              <td>
                {r.kind === "lock"
                  ? "—"
                  : r.targets === 0 && r.recipients === 0
                    ? "Toàn tenant"
                    : r.recipients}
              </td>
              <td>{r.note || "—"}</td>
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}

function ThresholdsTab({
  t,
  act,
  save,
  retire,
}: {
  t: PriceThresholds;
  act: Act;
  save: (b: PriceThresholdInput) => Promise<unknown>;
  retire: (scope: "group" | "item", code: string) => Promise<unknown>;
}) {
  const d = t.default;
  const [def, setDef] = useState({
    pct: d.pct,
    lookback: String(d.lookback_days ?? 7),
    flat: d.flat_pct ?? "1.00",
    survey: d.survey_pct ?? "40.00",
  });
  const [scope, setScope] = useState<"group" | "item">("group");
  const [code, setCode] = useState("");
  const [pct, setPct] = useState("");
  const lookbackOk =
    /^\d{1,2}$/.test(def.lookback) &&
    Number(def.lookback) >= 1 &&
    Number(def.lookback) <= 60;
  const defOk =
    validPct(def.pct) &&
    validPct(def.flat) &&
    validPct(def.survey) &&
    lookbackOk;
  const dup = t.items.some((x) => x.scope === scope && x.code === code);
  const columns: Column<PriceThreshold>[] = [
    {
      key: "scope",
      header: "Áp dụng cho",
      cell: (x) =>
        `${x.scope === "group" ? "Nhóm hàng" : "Mặt hàng"} ${x.code}`,
    },
    { key: "pct", header: "Ngưỡng ±%", cell: (x) => x.pct },
    { key: "at", header: "Cập nhật", cell: (x) => dt(x.recorded_at) },
    {
      key: "act",
      header: "Thao tác",
      cell: (x) =>
        t.can_manage ? (
          <button
            type="button"
            className="btn btn-sm"
            disabled={act.busy}
            onClick={() =>
              void act.run(
                () => retire(x.scope as "group" | "item", x.code),
                `Đã xóa ngưỡng ${x.code}.`,
              )
            }
          >
            Xóa
          </button>
        ) : (
          "—"
        ),
    },
  ];
  return (
    <div className="sec">
      <div className="card sec" aria-label="Tham số chung">
        <h2>Ngưỡng mặc định và tham số chung</h2>
        <p className="mute">
          Thứ tự ưu tiên: mặt hàng → nhóm hàng → mặc định. Ngưỡng mới áp dụng từ
          lần gửi tiếp theo; cảnh báo đã sinh giữ nguyên.
        </p>
        <form
          className="ws-two"
          onSubmit={(e) => {
            e.preventDefault();
            void act.run(
              () =>
                save({
                  scope: "default",
                  code: "",
                  pct: def.pct.trim(),
                  lookback_days: Number(def.lookback),
                  flat_pct: def.flat.trim(),
                  survey_pct: def.survey.trim(),
                }),
              "Đã lưu ngưỡng mặc định.",
            );
          }}
        >
          <div className="field">
            <label htmlFor="pt-def">Ngưỡng mặc định ±%</label>
            <input
              id="pt-def"
              className="input"
              inputMode="decimal"
              disabled={!t.can_manage}
              value={def.pct}
              onChange={(e) => setDef({ ...def, pct: e.target.value })}
            />
          </div>
          <div className="field">
            <label htmlFor="pt-n">Số ngày tìm giá kỳ trước (N)</label>
            <input
              id="pt-n"
              className="input"
              inputMode="numeric"
              disabled={!t.can_manage}
              value={def.lookback}
              onChange={(e) => setDef({ ...def, lookback: e.target.value })}
            />
          </div>
          <div className="field">
            <label htmlFor="pt-flat">Ngưỡng đi ngang %</label>
            <input
              id="pt-flat"
              className="input"
              inputMode="decimal"
              disabled={!t.can_manage}
              value={def.flat}
              onChange={(e) => setDef({ ...def, flat: e.target.value })}
            />
          </div>
          <div className="field">
            <label htmlFor="pt-survey">Ngưỡng phiếu điều tra %</label>
            <input
              id="pt-survey"
              className="input"
              inputMode="decimal"
              disabled={!t.can_manage}
              value={def.survey}
              onChange={(e) => setDef({ ...def, survey: e.target.value })}
            />
          </div>
          {t.can_manage ? (
            <div className="field">
              {!defOk ? (
                <p className="err" role="alert">
                  Ngưỡng phải trong (0; 100] %, tối đa 2 số lẻ; N từ 1 đến 60.
                </p>
              ) : null}
              <button
                type="submit"
                className="btn btn-primary"
                disabled={act.busy || !defOk}
              >
                Lưu tham số chung
              </button>
            </div>
          ) : null}
        </form>
      </div>
      <DataTable
        caption="Ngưỡng theo nhóm hàng / mặt hàng"
        columns={columns}
        rows={t.items}
        rowKey={(x) => `${x.scope}:${x.code}`}
        page={1}
        pageSize={Math.max(t.items.length, 1)}
        total={t.items.length}
        onPage={() => undefined}
        empty="Chưa có ngưỡng riêng — áp ngưỡng mặc định."
      />
      {t.can_manage ? (
        <form
          className="card sec ws-two"
          aria-label="Thêm ngưỡng"
          onSubmit={(e) => {
            e.preventDefault();
            void act.run(
              () => save({ scope, code: code.trim(), pct: pct.trim() }),
              `Đã lưu ngưỡng ${code}.`,
            );
          }}
        >
          <div className="field">
            <label htmlFor="pt-scope">Phạm vi áp dụng</label>
            <select
              id="pt-scope"
              className="select"
              value={scope}
              onChange={(e) => setScope(e.target.value as "group" | "item")}
            >
              <option value="group">Nhóm hàng</option>
              <option value="item">Mặt hàng</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="pt-code" className="req">
              Mã {scope === "group" ? "nhóm hàng" : "mặt hàng"}
            </label>
            <input
              id="pt-code"
              className="input"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={scope === "group" ? "rau_mau" : "MH00001"}
            />
          </div>
          <div className="field">
            <label htmlFor="pt-pct" className="req">
              Ngưỡng ±%
            </label>
            <input
              id="pt-pct"
              className="input"
              inputMode="decimal"
              value={pct}
              onChange={(e) => setPct(e.target.value)}
            />
          </div>
          <div className="field">
            {dup ? (
              <p className="mute">
                Đã có ngưỡng cho phạm vi này — lưu sẽ sửa ngưỡng hiện có.
              </p>
            ) : null}
            {pct && !validPct(pct) ? (
              <p className="err" role="alert">
                Ngưỡng phải trong (0; 100] %, tối đa 2 số lẻ.
              </p>
            ) : null}
            <button
              type="submit"
              className="btn btn-primary"
              disabled={act.busy || !code.trim() || !validPct(pct)}
            >
              {dup ? "Sửa ngưỡng" : "+ Thêm ngưỡng"}
            </button>
          </div>
        </form>
      ) : null}
    </div>
  );
}

function HistoryTab({
  c,
  runs,
}: {
  c: PriceCalendar;
  runs: PriceCalendarRun[];
}) {
  return (
    <div className="sec">
      <div className="card sec" aria-label="Lịch sử thay đổi lịch">
        <h2>Lịch sử thay đổi lịch báo giá</h2>
        <table className="dt">
          <thead>
            <tr>
              <th>Phiên bản</th>
              <th>Áp dụng từ</th>
              <th>Ngày báo giá</th>
              <th>Khóa / phản hồi</th>
              <th>Nhắc</th>
              <th>Thời điểm ghi</th>
            </tr>
          </thead>
          <tbody>
            {c.history.length === 0 ? (
              <tr>
                <td colSpan={6} className="mute">
                  Chưa đổi — đang dùng cấu hình mặc định (T2–T6, 12:00, 13:30).
                </td>
              </tr>
            ) : (
              c.history.map((h) => (
                <tr key={h.version}>
                  <td>{h.version}</td>
                  <td>{h.valid_from ? dmy(h.valid_from) : "—"}</td>
                  <td>{h.weekdays.map((w) => WD_LABEL[w]).join(", ")}</td>
                  <td>
                    {h.lock_at} / {h.respond_by}
                  </td>
                  <td>
                    {h.remind1_at}, {h.remind2_at}
                  </td>
                  <td>{dt(h.recorded_at)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className="card sec" aria-label="Mốc khóa nhập">
        <h2>Mốc khóa nhập thực tế</h2>
        <RunsTable runs={runs.filter((r) => r.kind === "lock")} />
      </div>
    </div>
  );
}
