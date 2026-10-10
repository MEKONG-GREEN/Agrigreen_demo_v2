// Backend giả lập cấu hình Báo giá W11.74 (/v1/price/calendar*, /v1/price/thresholds*) — hình dạng theo
// contracts/openapi/application.yaml (mục pf/price W11.74). Quy tắc mô phỏng pf/price: nhắc 1 < nhắc 2 < giờ khóa <
// hạn phản hồi, biến mẫu {ngày} {xã} {giờ khóa}, ngày áp dụng không quá khứ (ngày báo giá → từ mai), ngoại lệ chỉ từ
// ngày mai và phải khớp lịch, ngưỡng 0 < % ≤ 100 dạng chuỗi decimal, không xóa ngưỡng mặc định. Ghi: chỉ Admin KN.
import {
  renderTemplate,
  unknownVars,
  validPct,
  type PriceCalendar,
  type PriceCalendarDay,
  type PriceCalendarException,
  type PriceCalendarRun,
  type PriceSchedule,
  type PriceScheduleInput,
  type PriceThreshold,
  type PriceThresholdInput,
  type PriceThresholds,
  type Weekday,
} from "@/kn/price/api";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

const CODES: Weekday[] = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
const VN_OFFSET_MS = 7 * 3_600_000;
const ACTOR = "0190f000-0000-7000-8000-00000000a174";

/** Ngày yyyy-mm-dd theo giờ Việt Nam của thời điểm t (ms). */
export function vnDay(t: number): string {
  return new Date(t + VN_OFFSET_MS).toISOString().slice(0, 10);
}
function addDays(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}
function weekdayOf(day: string): Weekday {
  return CODES[new Date(`${day}T00:00:00Z`).getUTCDay()]!;
}
/** Mốc HH:MM giờ VN của ngày → ISO UTC. */
function at(day: string, hhmm: string): string {
  return new Date(Date.parse(`${day}T${hhmm}:00+07:00`))
    .toISOString()
    .replace(".000Z", "Z");
}

export const DEFAULT_SCHEDULE: PriceSchedule = {
  weekdays: ["mon", "tue", "wed", "thu", "fri"],
  lock_at: "12:00",
  respond_by: "13:30",
  remind1_at: "07:30",
  remind2_at: "11:00",
  channels: ["in_app", "email"],
  template:
    "Nhắc nhập giá ngày {ngày} cho {xã}: vui lòng gửi báo giá trước {giờ khóa}.",
  valid_from: null,
  version: 0,
};
const DEFAULT_THRESHOLD: PriceThreshold = {
  scope: "default",
  code: "",
  pct: "30.00",
  lookback_days: 7,
  flat_pct: "1.00",
  survey_pct: "40.00",
  version: 0,
};

type Hist = PriceSchedule & { actor_ref: string; recorded_at: string };
type Res<T> =
  | { ok: true; status: number; body: T }
  | { ok: false; status: number; code: string; title: string };
const fail = (status: number, code: string, title: string): Res<never> => ({
  ok: false,
  status,
  code,
  title,
});
const ok = <T>(body: T, status = 200): Res<T> => ({ ok: true, status, body });

const fixed2 = (s: string) => {
  const [i, f = ""] = s.trim().split(".");
  return `${Number.parseInt(i!, 10)}.${(f + "00").slice(0, 2)}`;
};

export function createPriceConfigStore(now: () => number = Date.now) {
  const versions: Hist[] = [];
  const exceptions = new Map<string, PriceCalendarException>();
  const thresholds = new Map<
    string,
    PriceThreshold & { status: "active" | "retired" }
  >();
  const history: PriceThreshold[] = [];
  const runs: PriceCalendarRun[] = [];
  let exceptionSeq = 0;
  let thresholdSeq = 0;

  const scheduleAt = (day: string): PriceSchedule => {
    let best: PriceSchedule = DEFAULT_SCHEDULE;
    for (const v of versions)
      if (
        v.valid_from! <= day &&
        (best.valid_from === null ||
          v.valid_from! > best.valid_from ||
          (v.valid_from === best.valid_from && v.version > best.version))
      )
        best = v;
    return best;
  };
  const plan = (day: string): PriceCalendarDay => {
    const s = scheduleAt(day);
    const wd = weekdayOf(day);
    let kind: PriceCalendarDay["kind"] = s.weekdays.includes(wd)
      ? "regular"
      : "off";
    let reason = "";
    const e = exceptions.get(day);
    if (e?.kind === "holiday" && kind === "regular")
      [kind, reason] = ["holiday", e.reason];
    if (e?.kind === "extra" && kind === "off")
      [kind, reason] = ["extra", e.reason];
    const quote = kind === "regular" || kind === "extra";
    return {
      day,
      weekday: wd,
      kind,
      quote,
      reason,
      lock_at: quote ? at(day, s.lock_at) : null,
      respond_by: quote ? at(day, s.respond_by) : null,
      remind1_at: quote ? at(day, s.remind1_at) : null,
      remind2_at: quote ? at(day, s.remind2_at) : null,
    };
  };
  const earliest = () => {
    const today = vnDay(now());
    return plan(today).quote ? addDays(today, 1) : today;
  };

  // Nhật ký mẫu: 5 ngày báo giá gần nhất — khóa đúng giờ, nhắc lần 1/2.
  {
    let d = addDays(vnDay(now()), -1);
    while (runs.length < 15) {
      const p = plan(d);
      if (p.quote) {
        runs.push({
          day: d,
          kind: "lock",
          scheduled_at: p.lock_at!,
          ran_at: new Date(Date.parse(p.lock_at!) + 12_000)
            .toISOString()
            .replace(".000Z", "Z"),
          lag_seconds: 12,
          recipients: 0,
          targets: 0,
          note: "",
        });
        runs.push({
          day: d,
          kind: "remind2",
          scheduled_at: p.remind2_at!,
          ran_at: p.remind2_at!,
          lag_seconds: 0,
          recipients: 4,
          targets: 4,
          note: "",
        });
        runs.push({
          day: d,
          kind: "remind1",
          scheduled_at: p.remind1_at!,
          ran_at: p.remind1_at!,
          lag_seconds: 0,
          recipients: 26,
          targets: 26,
          note: "",
        });
      }
      d = addDays(d, -1);
    }
  }

  return {
    calendar(canManage: boolean): PriceCalendar {
      const today = vnDay(now());
      const days = Array.from({ length: 30 }, (_, i) =>
        plan(addDays(today, i)),
      );
      return {
        profile: "kn",
        now: new Date(now()).toISOString(),
        can_manage: canManage,
        today: days[0]!,
        locked: !!days[0]!.lock_at && Date.parse(days[0]!.lock_at) <= now(),
        current: scheduleAt(today),
        upcoming: versions
          .filter((v) => v.valid_from! > today)
          .sort((a, b) => a.version - b.version),
        earliest_valid_from: earliest(),
        days,
        exceptions: [...exceptions.values()]
          .filter((e) => e.day >= today)
          .sort((a, b) => a.day.localeCompare(b.day)),
        history: [...versions].reverse(),
      };
    },
    saveSchedule(
      canManage: boolean,
      b: PriceScheduleInput,
    ): Res<PriceSchedule> {
      if (!canManage)
        return fail(
          403,
          "forbidden",
          "Chỉ vai quản trị nghiệp vụ báo giá cấu hình được lịch",
        );
      if (!b.weekdays?.length || !b.channels?.length || !b.template?.trim())
        return fail(
          422,
          "validation_failed",
          "Thiếu ngày báo giá, kênh hoặc nội dung mẫu",
        );
      if (!(b.respond_by > b.lock_at))
        return fail(
          422,
          "price.respond_before_lock",
          "Hạn phản hồi xác minh phải sau giờ khóa",
        );
      if (!(b.remind1_at < b.remind2_at && b.remind2_at < b.lock_at))
        return fail(
          422,
          "price.remind_order",
          "Giờ nhắc lần 1 phải trước lần 2, lần 2 phải trước giờ khóa",
        );
      const bad = unknownVars(b.template);
      if (bad.length)
        return fail(
          422,
          "price.template_var",
          `Nội dung mẫu có biến không hỗ trợ: ${bad.join(", ")}`,
        );
      if (b.valid_from < earliest())
        return fail(
          422,
          "price.valid_from",
          `Ngày áp dụng sớm nhất ${earliest()}`,
        );
      const v: Hist = {
        ...b,
        valid_from: b.valid_from,
        version: versions.length + 1,
        actor_ref: ACTOR,
        recorded_at: new Date(now()).toISOString(),
      };
      versions.push(v);
      return ok(
        {
          weekdays: v.weekdays,
          lock_at: v.lock_at,
          respond_by: v.respond_by,
          remind1_at: v.remind1_at,
          remind2_at: v.remind2_at,
          channels: v.channels,
          template: v.template,
          valid_from: v.valid_from,
          version: v.version,
        },
        201,
      );
    },
    addException(
      canManage: boolean,
      b: { day: string; kind: "holiday" | "extra"; reason: string },
    ): Res<PriceCalendarException> {
      if (!canManage)
        return fail(
          403,
          "forbidden",
          "Chỉ vai quản trị nghiệp vụ báo giá cấu hình được lịch ngoại lệ",
        );
      if (!/^\d{4}-\d{2}-\d{2}$/.test(b.day ?? "") || !b.reason?.trim())
        return fail(422, "validation_failed", "Thiếu ngày hoặc lý do");
      if (b.day <= vnDay(now()))
        return fail(422, "price.valid_from", "Ngày ngoại lệ phải từ ngày mai");
      if (exceptions.has(b.day))
        return fail(409, "price.exception_exists", "Ngày này đã có ngoại lệ");
      const regular = scheduleAt(b.day).weekdays.includes(weekdayOf(b.day));
      if ((b.kind === "holiday") !== regular)
        return fail(
          422,
          "price.exception_mismatch",
          "Ngày nghỉ phải là ngày báo giá theo lịch; ngày bổ sung phải là ngày ngoài lịch",
        );
      const e = {
        day: b.day,
        kind: b.kind,
        reason: b.reason.trim(),
        version: ++exceptionSeq,
      };
      exceptions.set(b.day, e);
      return ok(e, 201);
    },
    retireException(canManage: boolean, day: string): Res<null> {
      if (!canManage) return fail(403, "forbidden", "Không có quyền");
      if (day <= vnDay(now()))
        return fail(
          422,
          "price.valid_from",
          "Chỉ xóa được ngoại lệ từ ngày mai",
        );
      if (!exceptions.delete(day))
        return fail(
          404,
          "price.exception_not_found",
          "Không có ngoại lệ cho ngày này",
        );
      return ok(null, 204);
    },
    runs(
      from: string,
      to: string,
    ): { from: string; to: string; items: PriceCalendarRun[] } {
      return {
        from,
        to,
        items: runs.filter((r) => r.day >= from && r.day <= to),
      };
    },
    reminderTest(canManage: boolean): Res<{ body: string }> {
      if (!canManage) return fail(403, "forbidden", "Không có quyền");
      const s = scheduleAt(vnDay(now()));
      return ok(
        { body: renderTemplate(s.template, vnDay(now()), "", s.lock_at) },
        202,
      );
    },
    thresholds(canManage: boolean): PriceThresholds {
      const all = [...thresholds.values()];
      const def = all.find((t) => t.scope === "default");
      return {
        profile: "kn",
        can_manage: canManage,
        default: def ? { ...def } : DEFAULT_THRESHOLD,
        default_stored: !!def,
        items: all.filter(
          (t) => t.scope !== "default" && t.status === "active",
        ),
        history: [...history].reverse(),
      };
    },
    saveThreshold(
      canManage: boolean,
      b: PriceThresholdInput,
    ): Res<PriceThreshold> {
      if (!canManage)
        return fail(
          403,
          "forbidden",
          "Chỉ vai quản trị nghiệp vụ báo giá cấu hình được ngưỡng",
        );
      if (typeof b.pct !== "string" || !validPct(b.pct))
        return fail(
          422,
          "validation_failed",
          "Ngưỡng phải trong (0; 100] %, tối đa 2 số lẻ",
        );
      if (b.scope === "default") {
        if (
          !b.lookback_days ||
          b.lookback_days < 1 ||
          b.lookback_days > 60 ||
          !validPct(b.flat_pct ?? "") ||
          !validPct(b.survey_pct ?? "")
        )
          return fail(422, "validation_failed", "Tham số chung không hợp lệ");
      } else if (!/^[a-z0-9][a-z0-9_.-]{0,39}$/.test(b.code ?? ""))
        return fail(
          422,
          "validation_failed",
          "Mã nhóm hàng / mặt hàng không hợp lệ",
        );
      const t: PriceThreshold & { status: "active" } = {
        scope: b.scope,
        code: b.scope === "default" ? "" : b.code,
        pct: fixed2(b.pct),
        ...(b.scope === "default"
          ? {
              lookback_days: b.lookback_days,
              flat_pct: fixed2(b.flat_pct!),
              survey_pct: fixed2(b.survey_pct!),
            }
          : {}),
        version: ++thresholdSeq,
        status: "active",
        actor_ref: ACTOR,
        recorded_at: new Date(now()).toISOString(),
      };
      thresholds.set(`${t.scope}:${t.code}`, t);
      history.push({ ...t });
      return ok(t, 201);
    },
    retireThreshold(
      canManage: boolean,
      scope: string,
      code: string,
    ): Res<null> {
      if (!canManage) return fail(403, "forbidden", "Không có quyền");
      if (scope === "default")
        return fail(
          422,
          "price.default_threshold",
          "Không xóa được ngưỡng mặc định",
        );
      const t = thresholds.get(`${scope}:${code}`);
      if (!t || t.status !== "active")
        return fail(404, "price.threshold_not_found", "Không có ngưỡng này");
      const r = {
        ...t,
        status: "retired" as const,
        version: ++thresholdSeq,
        recorded_at: new Date(now()).toISOString(),
      };
      thresholds.set(`${scope}:${code}`, r);
      history.push({ ...r });
      return ok(null, 204);
    },
  };
}

export type PriceConfigStore = ReturnType<typeof createPriceConfigStore>;
let STORE = createPriceConfigStore();
export function resetPriceConfig(now?: () => number): PriceConfigStore {
  STORE = createPriceConfigStore(now);
  return STORE;
}

/** Vai trong ngữ cảnh KN: Admin ghi được; Tỉnh/Xã/TW chỉ đọc (QTNV báo giá của Cấp Tỉnh ở W11.72). */
function role(ctx: Ctx): string | null {
  const id = ctx.request.headers.get("X-AG-Context");
  const c = id ? knContext(id) : undefined;
  if (c) return c.role;
  return ctx.persona.me.roles.includes("platform_admin") ? "admin" : null;
}

function respond<T>(r: Res<T>): Response {
  if (!r.ok) return problem(r.status, r.code, r.title);
  if (r.status === 204) return new Response(null, { status: 204 });
  return json(r.body, r.status);
}

function guarded(ctx: Ctx, fn: (canManage: boolean) => Response): Response {
  const r = role(ctx);
  if (!r)
    return problem(
      403,
      "forbidden",
      "Vai của người gọi không thuộc cấu hình giá kn",
    );
  return fn(r === "admin");
}

export const priceConfigRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/price\/calendar$/,
    handler: (ctx) => guarded(ctx, (m) => json(STORE.calendar(m))),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/calendar\/schedule$/,
    handler: (ctx) =>
      guarded(ctx, (m) =>
        respond(STORE.saveSchedule(m, ctx.body as PriceScheduleInput)),
      ),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/calendar\/exceptions$/,
    handler: (ctx) =>
      guarded(ctx, (m) =>
        respond(
          STORE.addException(
            m,
            ctx.body as {
              day: string;
              kind: "holiday" | "extra";
              reason: string;
            },
          ),
        ),
      ),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/calendar\/exceptions\/(\d{4}-\d{2}-\d{2})\/retire$/,
    handler: (ctx) =>
      guarded(ctx, (m) => respond(STORE.retireException(m, ctx.params[0]!))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/calendar\/runs$/,
    handler: (ctx) =>
      guarded(ctx, () => {
        const to = ctx.url.searchParams.get("to") ?? vnDay(Date.now());
        const from = ctx.url.searchParams.get("from") ?? addDays(to, -30);
        return json(STORE.runs(from, to));
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/calendar\/reminder-test$/,
    handler: (ctx) => guarded(ctx, (m) => respond(STORE.reminderTest(m))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/thresholds$/,
    handler: (ctx) => guarded(ctx, (m) => json(STORE.thresholds(m))),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/thresholds$/,
    handler: (ctx) =>
      guarded(ctx, (m) =>
        respond(STORE.saveThreshold(m, ctx.body as PriceThresholdInput)),
      ),
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/thresholds\/(default|group|item)\/([^/]+)\/retire$/,
    handler: (ctx) =>
      guarded(ctx, (m) =>
        respond(
          STORE.retireThreshold(
            m,
            ctx.params[0]!,
            decodeURIComponent(ctx.params[1]!),
          ),
        ),
      ),
  },
];
