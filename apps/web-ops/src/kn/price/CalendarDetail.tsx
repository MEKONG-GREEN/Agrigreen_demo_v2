"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField, TextField, validateForm } from "@/components/form";
import {
  DetailPage,
  LabelValueBlock,
  LogPanel,
} from "@/components/patterns/detail";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  createPriceConfigApi,
  renderTemplate,
  WEEKDAYS,
  type PriceCalendar,
  type PriceConfigApi,
  type PriceScheduleInput,
  type PriceThresholds,
} from "./api";
import {
  LOOKBACK_RULE,
  PERCENT_RULE,
  scheduleInput,
  validateSchedule,
  validateThreshold,
  type Errors,
  type ThresholdForm,
} from "./calendar-form";
import {
  configBase,
  isCalendarDay,
  thresholdPath,
  type ConfigRoute,
} from "./calendar-routes";
import {
  CalendarField,
  dmy,
  ErrorLine,
  nextDay,
  PriceNumberField,
  RunsTable,
  ScheduleLog,
  ScheduleSummary,
  thresholdLog,
  ThresholdSummary,
} from "./calendar-view";

type Act = ReturnType<typeof useAction>;
const back = (base: string) => ({ href: base, label: "Cấu hình báo giá" });
function Cancel({ base }: { base: string }) {
  return (
    <Link className="btn btn-o" href={base}>
      Hủy
    </Link>
  );
}
function ReadOnly() {
  return (
    <p className="alert alert-info" role="note">
      Chỉ xem — máy chủ chưa cấp quyền Quản trị nghiệp vụ báo giá trong ngữ cảnh
      hiện tại.
    </p>
  );
}
function fieldErrors(act: Act, local: Errors): Errors {
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  if (server.lookback_days) server.lookback = server.lookback_days;
  if (server.flat_pct) server.flat = server.flat_pct;
  if (server.survey_pct) server.survey = server.survey_pct;
  if (act.problem?.code === "price.valid_from")
    server.valid_from = act.problem.message;
  return {
    ...server,
    ...Object.fromEntries(Object.entries(local).filter(([, value]) => !!value)),
  };
}

export function PriceConfigDetailScreen({ route }: { route: ConfigRoute }) {
  const auth = useAuth();
  const base = configBase(usePathname());
  const pa = useMemo(() => createPriceConfigApi(auth.api), [auth.api]);
  const key = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}:${JSON.stringify(route)}`;
  return <ConfigData key={key} route={route} pa={pa} base={base} />;
}

function ConfigData({
  route,
  pa,
  base,
}: {
  route: ConfigRoute;
  pa: PriceConfigApi;
  base: string;
}) {
  const cal = useLoad(() => pa.calendar(), [pa]);
  const thresholds = useLoad(() => pa.thresholds(), [pa]);
  if (route.kind === "threshold" || route.kind === "threshold-new") {
    return (
      <LoadState s={thresholds}>
        {(t) => <ThresholdPage route={route} t={t} pa={pa} base={base} />}
      </LoadState>
    );
  }
  return (
    <LoadState s={cal}>
      {(c) =>
        route.kind === "schedule" || route.kind === "reminder" ? (
          <SchedulePage route={route} c={c} pa={pa} base={base} />
        ) : (
          <ExceptionPage route={route} c={c} pa={pa} base={base} />
        )
      }
    </LoadState>
  );
}

function SchedulePage({
  route,
  c,
  pa,
  base,
}: {
  route: Extract<ConfigRoute, { kind: "schedule" | "reminder" }>;
  c: PriceCalendar;
  pa: PriceConfigApi;
  base: string;
}) {
  const reminder = route.kind === "reminder";
  const [form, setForm] = useState(() => scheduleInput(c));
  const [local, setLocal] = useState<Errors>({});
  const act = useAction();
  const router = useRouter();
  const runs = useLoad(() => pa.runs(), [pa, act.note]);
  const errors = fieldErrors(act, local);
  const path = `${base}/${reminder ? "nhac" : "lich"}`;
  const title = reminder ? "Cấu hình nhắc nhập giá" : "Kỳ báo giá & giờ khóa";
  const edit = route.edit && c.can_manage;
  const change = <K extends keyof PriceScheduleInput>(
    key: K,
    value: PriceScheduleInput[K],
  ) => {
    setForm((old) => ({ ...old, [key]: value }));
    setLocal((old) => {
      const copy = { ...old };
      delete copy[key];
      return copy;
    });
  };
  const submit = async () => {
    if (!edit || act.busy) return;
    const input = reminder
      ? { ...form, valid_from: c.earliest_valid_from }
      : form;
    const issues = validateSchedule(input, c.earliest_valid_from, reminder);
    setLocal(issues);
    if (Object.keys(issues).length) return;
    const result = await act.run(
      () => pa.saveSchedule(input),
      "Đã lưu cấu hình báo giá.",
    );
    if (result !== null) router.push(path);
  };
  return (
    <DetailPage
      back={back(base)}
      title={`${edit ? "Sửa · " : ""}${title}`}
      log={<ScheduleLog c={c} />}
      actions={
        edit ? (
          <>
            <Cancel base={base} />
            <button
              className="btn btn-primary"
              type="submit"
              form="price-schedule-form"
              disabled={act.busy}
            >
              Lưu cấu hình
            </button>
          </>
        ) : c.can_manage ? (
          <Link className="btn btn-primary" href={`${path}/sua`}>
            Sửa cấu hình
          </Link>
        ) : undefined
      }
    >
      {act.view}
      {route.edit && !c.can_manage ? <ReadOnly /> : null}
      {edit ? (
        <form
          id="price-schedule-form"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <p className="mute">
            Thay đổi áp dụng sớm nhất {dmy(c.earliest_valid_from)}; kỳ đã khóa
            giữ nguyên.
          </p>
          {reminder ? (
            <>
              <div className="ws-two">
                <CalendarField
                  label="Giờ nhắc lần 1"
                  type="time"
                  value={form.remind1_at}
                  onChange={(v) => change("remind1_at", v)}
                  error={errors.remind1_at}
                />
                <CalendarField
                  label="Giờ nhắc lần 2"
                  type="time"
                  value={form.remind2_at}
                  onChange={(v) => change("remind2_at", v)}
                  error={errors.remind2_at}
                />
              </div>
              <p className="mute">
                Giờ khóa hiện được chọn: {form.lock_at}. Lần 2 chỉ gửi tới xã
                chưa gửi báo giá.
              </p>
              <fieldset className="field">
                <legend>
                  Kênh nhắc{" "}
                  <span className="req" aria-hidden="true">
                    *
                  </span>
                </legend>
                {(
                  [
                    ["in_app", "Trong ứng dụng"],
                    ["email", "Email"],
                  ] as const
                ).map(([code, label]) => (
                  <label key={code} style={{ marginRight: 16 }}>
                    <input
                      type="checkbox"
                      checked={form.channels.includes(code)}
                      onChange={(e) =>
                        change(
                          "channels",
                          e.target.checked
                            ? [...form.channels, code]
                            : form.channels.filter((x) => x !== code),
                        )
                      }
                    />{" "}
                    {label}
                  </label>
                ))}
                <ErrorLine error={errors.channels} />
              </fieldset>
              <TextField
                label="Nội dung mẫu"
                value={form.template}
                required
                multiline
                maxLength={500}
                onChange={(v) => change("template", v)}
                error={errors.template}
                hint="Biến hỗ trợ: {ngày}, {xã}, {giờ khóa}."
              />
              <p className="mute">
                Xem trước:{" "}
                {renderTemplate(form.template, c.today.day, "xã", form.lock_at)}
              </p>
            </>
          ) : (
            <>
              <fieldset className="field">
                <legend>
                  Ngày báo giá trong tuần{" "}
                  <span className="req" aria-hidden="true">
                    *
                  </span>
                </legend>
                {WEEKDAYS.map((w) => (
                  <label key={w.code} style={{ marginRight: 16 }}>
                    <input
                      type="checkbox"
                      checked={form.weekdays.includes(w.code)}
                      onChange={(e) =>
                        change(
                          "weekdays",
                          e.target.checked
                            ? [...form.weekdays, w.code]
                            : form.weekdays.filter((x) => x !== w.code),
                        )
                      }
                    />{" "}
                    {w.label}
                  </label>
                ))}
                <ErrorLine error={errors.weekdays} />
              </fieldset>
              <div className="ws-two">
                <CalendarField
                  label="Giờ khóa nhập"
                  type="time"
                  value={form.lock_at}
                  onChange={(v) => change("lock_at", v)}
                  error={errors.lock_at}
                />
                <CalendarField
                  label="Hạn phản hồi xác minh"
                  type="time"
                  value={form.respond_by}
                  onChange={(v) => change("respond_by", v)}
                  error={errors.respond_by}
                />
                <CalendarField
                  label="Ngày áp dụng"
                  type="date"
                  value={form.valid_from}
                  min={c.earliest_valid_from}
                  onChange={(v) => change("valid_from", v)}
                  error={errors.valid_from}
                />
              </div>
              <p className="mute">
                Lịch nhắc đang chọn: {form.remind1_at}, {form.remind2_at}.{" "}
                <Link href={`${base}/nhac/sua`}>Sửa lịch nhắc</Link> nếu cần
                chuyển giờ khóa sớm hơn.
              </p>
            </>
          )}
        </form>
      ) : (
        <>
          <ScheduleSummary value={c.upcoming.at(-1) ?? c.current} />
          {c.upcoming.length ? (
            <p className="mute">
              Đang xem phiên bản sắp áp dụng. Giờ khóa đang áp dụng:{" "}
              {c.current.lock_at}; hạn phản hồi: {c.current.respond_by}.
            </p>
          ) : null}
        </>
      )}
      {reminder ? (
        <section className="sec">
          <h2>Gửi thử và lịch sử gửi nhắc</h2>
          {c.can_manage ? (
            <button
              type="button"
              className="btn btn-o"
              disabled={act.busy}
              onClick={() =>
                void act.run(
                  () => pa.reminderTest(),
                  (r) => `Đã gửi thử tới tài khoản của bạn: “${r.body}”`,
                )
              }
            >
              Gửi thử cấu hình đã lưu
            </button>
          ) : null}
          <p className="mute">Gửi thử dùng cấu hình đã lưu trên máy chủ.</p>
          <LoadState s={runs}>
            {(r) => (
              <RunsTable runs={r.items.filter((x) => x.kind !== "lock")} />
            )}
          </LoadState>
        </section>
      ) : null}
    </DetailPage>
  );
}

function ExceptionPage({
  route,
  c,
  pa,
  base,
}: {
  route: Extract<ConfigRoute, { kind: "exception" | "exception-new" }>;
  c: PriceCalendar;
  pa: PriceConfigApi;
  base: string;
}) {
  const creating = route.kind === "exception-new";
  const current =
    route.kind === "exception"
      ? c.exceptions.find((x) => x.day === route.day)
      : undefined;
  const [form, setForm] = useState({
    day: "",
    kind: "holiday" as "holiday" | "extra",
    reason: "",
  });
  const [local, setLocal] = useState<Errors>({});
  const act = useAction();
  const router = useRouter();
  const errors = fieldErrors(act, local);
  const canEdit = creating && c.can_manage;
  const tomorrow = nextDay(c.today.day);
  const submit = async () => {
    if (!canEdit || act.busy) return;
    const result = validateForm(form, {
      day: { kind: "text", required: true },
      kind: { kind: "text", required: true },
      reason: { kind: "text", required: true, maxLength: 300 },
    });
    const issues: Errors = { ...result.errors };
    if (!isCalendarDay(form.day) || form.day < tomorrow)
      issues.day = `Chỉ thêm ngày ngoại lệ từ ${dmy(tomorrow)}.`;
    if (form.kind !== "holiday" && form.kind !== "extra")
      issues.kind = "Chọn loại ngày ngoại lệ.";
    setLocal(issues);
    if (Object.keys(issues).length) return;
    const saved = await act.run(
      () =>
        pa.addException({
          day: form.day,
          kind: form.kind,
          reason: form.reason.trim(),
        }),
      "Đã thêm ngày ngoại lệ.",
    );
    if (saved) router.push(`${base}/ngoai-le/${saved.day}`);
  };
  return (
    <DetailPage
      back={back(base)}
      title={creating ? "Thêm ngoại lệ" : `Ngoại lệ ngày ${dmy(route.day)}`}
      log={
        <LogPanel
          entries={[]}
          empty={
            creating
              ? "Chưa có bản ghi ngoại lệ."
              : "API ngoại lệ chỉ trả phiên bản; chưa có chi tiết người và thời điểm thay đổi."
          }
        />
      }
      actions={
        canEdit ? (
          <>
            <Cancel base={base} />
            <button
              className="btn btn-primary"
              type="submit"
              form="price-exception-form"
              disabled={act.busy}
            >
              Lưu ngoại lệ
            </button>
          </>
        ) : undefined
      }
    >
      {act.view}
      {creating && !c.can_manage ? <ReadOnly /> : null}
      {canEdit ? (
        <form
          id="price-exception-form"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <CalendarField
            label="Ngày ngoại lệ"
            type="date"
            min={tomorrow}
            value={form.day}
            onChange={(v) => {
              setForm({ ...form, day: v });
              setLocal({});
            }}
            error={errors.day}
          />
          <SelectField
            label="Loại ngoại lệ"
            required
            value={form.kind}
            onChange={(v) => {
              setForm({ ...form, kind: v as "holiday" | "extra" });
              setLocal({});
            }}
            options={[
              { value: "holiday", label: "Ngày nghỉ" },
              { value: "extra", label: "Ngày báo giá bổ sung" },
            ]}
            error={errors.kind}
          />
          <TextField
            label="Lý do"
            required
            multiline
            maxLength={300}
            value={form.reason}
            onChange={(v) => {
              setForm({ ...form, reason: v });
              setLocal({});
            }}
            error={errors.reason}
          />
        </form>
      ) : current ? (
        <LabelValueBlock
          title="Ngày ngoại lệ"
          items={[
            { label: "Ngày", value: dmy(current.day) },
            {
              label: "Loại",
              value:
                current.kind === "holiday"
                  ? "Ngày nghỉ"
                  : "Ngày báo giá bổ sung",
            },
            { label: "Lý do", value: current.reason },
            { label: "Phiên bản", value: current.version },
          ]}
        />
      ) : !creating ? (
        <p role="status">Không tìm thấy ngoại lệ đang áp dụng cho ngày này.</p>
      ) : null}
    </DetailPage>
  );
}

function ThresholdPage({
  route,
  t,
  pa,
  base,
}: {
  route: Extract<ConfigRoute, { kind: "threshold" | "threshold-new" }>;
  t: PriceThresholds;
  pa: PriceConfigApi;
  base: string;
}) {
  const creating = route.kind === "threshold-new";
  const current =
    route.kind === "threshold"
      ? route.scope === "default"
        ? t.default
        : (t.items.find(
            (x) => x.scope === route.scope && x.code === route.code,
          ) ??
          [...t.history]
            .filter((x) => x.scope === route.scope && x.code === route.code)
            .sort((a, b) => b.version - a.version)[0])
      : undefined;
  const [form, setForm] = useState<ThresholdForm>(() => ({
    scope: current?.scope ?? "group",
    code: current?.code ?? "",
    pct: current?.pct ?? "",
    lookback: String(current?.lookback_days ?? 7),
    flat: current?.flat_pct ?? "1.00",
    survey: current?.survey_pct ?? "40.00",
  }));
  const [local, setLocal] = useState<Errors>({});
  const act = useAction();
  const router = useRouter();
  const errors = fieldErrors(act, local);
  const edit =
    t.can_manage &&
    (creating || (route.kind === "threshold" && route.edit && !!current));
  const defaultScope = form.scope === "default";
  const existing = t.items.some(
    (x) => x.scope === form.scope && x.code === form.code.trim(),
  );
  const change = <K extends keyof ThresholdForm>(
    key: K,
    value: ThresholdForm[K],
  ) => {
    setForm((old) => ({ ...old, [key]: value }));
    setLocal((old) => {
      const copy = { ...old };
      delete copy[key];
      return copy;
    });
  };
  const submit = async () => {
    if (!edit || act.busy) return;
    const checked = validateThreshold(form);
    setLocal(checked.errors);
    if (!checked.valid) return;
    const v = checked.values;
    const input = {
      scope: v.scope,
      code: v.scope === "default" ? "" : v.code,
      pct: v.pct,
      ...(v.scope === "default"
        ? {
            lookback_days: Number(v.lookback),
            flat_pct: v.flat,
            survey_pct: v.survey,
          }
        : {}),
    };
    const saved = await act.run(
      () => pa.saveThreshold(input),
      "Đã lưu ngưỡng báo giá.",
    );
    if (saved) router.push(`${base}/${thresholdPath(saved.scope, saved.code)}`);
  };
  const log = (
    <LogPanel
      entries={current ? thresholdLog(t, current.scope, current.code) : []}
      empty="Chưa có thay đổi trong sổ ngưỡng này."
    />
  );
  const title = creating
    ? "Thêm ngưỡng cảnh báo"
    : `${route.kind === "threshold" && route.edit && edit ? "Sửa · " : ""}${current?.scope === "default" ? "Ngưỡng mặc định và tham số chung" : `Ngưỡng ${route.kind === "threshold" ? route.code : ""}`}`;
  return (
    <DetailPage
      back={back(base)}
      title={title}
      log={log}
      actions={
        edit ? (
          <>
            <Cancel base={base} />
            <button
              className="btn btn-primary"
              type="submit"
              form="price-threshold-form"
              disabled={act.busy}
            >
              Lưu ngưỡng
            </button>
          </>
        ) : current && t.can_manage ? (
          <Link
            className="btn btn-primary"
            href={`${base}/${thresholdPath(current.scope, current.code)}/sua`}
          >
            Sửa ngưỡng
          </Link>
        ) : undefined
      }
    >
      {act.view}
      {!t.can_manage &&
      (creating || (route.kind === "threshold" && route.edit)) ? (
        <ReadOnly />
      ) : null}
      {edit ? (
        <form
          id="price-threshold-form"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <p className="mute">
            Ngưỡng mới áp dụng từ lần gửi tiếp theo; cảnh báo đã sinh giữ
            nguyên. Không xóa ngưỡng mặc định.
          </p>
          {!defaultScope ? (
            <>
              <SelectField
                label="Phạm vi áp dụng"
                required
                disabled={!creating}
                value={form.scope}
                onChange={(v) => {
                  setForm({ ...form, scope: v as "group" | "item", code: "" });
                  setLocal({});
                }}
                options={[
                  { value: "group", label: "Nhóm hàng" },
                  { value: "item", label: "Mặt hàng" },
                ]}
                error={errors.scope}
              />
              <TextField
                label={form.scope === "group" ? "Mã nhóm hàng" : "Mã mặt hàng"}
                required
                disabled={!creating}
                maxLength={40}
                value={form.code}
                onChange={(v) => change("code", v)}
                error={errors.code}
              />
              {creating && existing ? (
                <p className="mute">
                  Đã có ngưỡng cho phạm vi này — lưu sẽ sửa ngưỡng hiện có.{" "}
                  <Link
                    href={`${base}/${thresholdPath(form.scope, form.code.trim())}`}
                  >
                    Xem ngưỡng hiện có
                  </Link>
                </p>
              ) : null}
            </>
          ) : null}
          <div className="ws-two">
            <PriceNumberField
              label={defaultScope ? "Ngưỡng mặc định ±%" : "Ngưỡng ±%"}
              required
              value={form.pct}
              rule={PERCENT_RULE}
              onChange={(v) => change("pct", v)}
              error={errors.pct}
              onBlurError={(e) => setLocal((old) => ({ ...old, pct: e ?? "" }))}
            />
            {defaultScope ? (
              <>
                <PriceNumberField
                  label="Số ngày tìm giá kỳ trước (N)"
                  required
                  value={form.lookback}
                  rule={LOOKBACK_RULE}
                  onChange={(v) => change("lookback", v)}
                  error={errors.lookback}
                  onBlurError={(e) =>
                    setLocal((old) => ({ ...old, lookback: e ?? "" }))
                  }
                />
                <PriceNumberField
                  label="Ngưỡng đi ngang %"
                  required
                  value={form.flat}
                  rule={PERCENT_RULE}
                  onChange={(v) => change("flat", v)}
                  error={errors.flat}
                  onBlurError={(e) =>
                    setLocal((old) => ({ ...old, flat: e ?? "" }))
                  }
                />
                <PriceNumberField
                  label="Ngưỡng phiếu điều tra %"
                  required
                  value={form.survey}
                  rule={PERCENT_RULE}
                  onChange={(v) => change("survey", v)}
                  error={errors.survey}
                  onBlurError={(e) =>
                    setLocal((old) => ({ ...old, survey: e ?? "" }))
                  }
                />
              </>
            ) : null}
          </div>
        </form>
      ) : current ? (
        <ThresholdSummary value={current} />
      ) : !creating ? (
        <p role="status">
          Không tìm thấy ngưỡng này trong dữ liệu được cấp quyền.
        </p>
      ) : null}
    </DetailPage>
  );
}
