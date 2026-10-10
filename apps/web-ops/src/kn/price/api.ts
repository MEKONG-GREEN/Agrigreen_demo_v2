// Client có kiểu cho cấu hình Báo giá W11.74 (/v1/price/calendar*, /v1/price/thresholds*) trên openapi-fetch sinh từ
// contracts/openapi/application.yaml. Bearer, X-AG-Context, Idempotency-Key do middleware của createApiClient gắn.
// Số % là chuỗi decimal (schema Decimal) — không đổi sang number.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type PriceCalendar = S["PriceCalendar"];
export type PriceSchedule = S["PriceSchedule"];
export type PriceScheduleInput = S["PriceScheduleInput"];
export type PriceCalendarDay = S["PriceCalendarDay"];
export type PriceCalendarException = S["PriceCalendarException"];
export type PriceCalendarRun = S["PriceCalendarRun"];
export type PriceThreshold = S["PriceThreshold"];
export type PriceThresholdInput = S["PriceThresholdInput"];
export type PriceThresholds = S["PriceThresholds"];
export type Weekday = PriceScheduleInput["weekdays"][number];

export const WEEKDAYS: { code: Weekday; label: string }[] = [
  { code: "mon", label: "Thứ Hai" },
  { code: "tue", label: "Thứ Ba" },
  { code: "wed", label: "Thứ Tư" },
  { code: "thu", label: "Thứ Năm" },
  { code: "fri", label: "Thứ Sáu" },
  { code: "sat", label: "Thứ Bảy" },
  { code: "sun", label: "Chủ nhật" },
];

export const DAY_KIND_LABEL: Record<PriceCalendarDay["kind"], string> = {
  regular: "Ngày báo giá",
  extra: "Ngày bổ sung",
  holiday: "Ngày nghỉ",
  off: "Không báo giá",
};

export const RUN_KIND_LABEL: Record<PriceCalendarRun["kind"], string> = {
  lock: "Khóa nhập",
  remind1: "Nhắc lần 1",
  remind2: "Nhắc lần 2",
};

/** Biến được hỗ trợ trong nội dung mẫu nhắc (FN-AD-04.05 BR-02). */
export const TEMPLATE_VARS = ["{ngày}", "{xã}", "{giờ khóa}"] as const;

/** Biến lạ trong nội dung mẫu (khớp domain.UnknownVars của pf/price). */
export function unknownVars(tpl: string): string[] {
  const out: string[] = [];
  for (const m of tpl.match(/\{[^{}]*\}/g) ?? []) {
    if (!(TEMPLATE_VARS as readonly string[]).includes(m) && !out.includes(m))
      out.push(m);
  }
  return out;
}

/** Kiểm thứ tự giờ phía máy khách (máy chủ kiểm lại): nhắc 1 < nhắc 2 < giờ khóa < hạn phản hồi. */
export function scheduleProblems(
  s: Pick<
    PriceScheduleInput,
    | "lock_at"
    | "respond_by"
    | "remind1_at"
    | "remind2_at"
    | "weekdays"
    | "channels"
    | "template"
  >,
): string[] {
  const p: string[] = [];
  if (!s.weekdays.length) p.push("Chọn ít nhất một ngày báo giá trong tuần.");
  if (!(s.respond_by > s.lock_at))
    p.push("Hạn phản hồi xác minh phải sau giờ khóa.");
  if (!(s.remind1_at < s.remind2_at && s.remind2_at < s.lock_at))
    p.push("Giờ nhắc lần 1 phải trước lần 2, lần 2 phải trước giờ khóa.");
  if (!s.channels.length) p.push("Chọn ít nhất một kênh nhắc.");
  const bad = unknownVars(s.template);
  if (bad.length)
    p.push(`Nội dung mẫu có biến không hỗ trợ: ${bad.join(", ")}.`);
  return p;
}

/** Xem trước nội dung mẫu (khớp domain.RenderReminder). */
export function renderTemplate(
  tpl: string,
  day: string,
  commune: string,
  lockAt: string,
): string {
  const [y, m, d] = day.split("-");
  return tpl
    .replaceAll("{ngày}", `${d}/${m}/${y}`)
    .replaceAll("{xã}", commune || "xã")
    .replaceAll("{giờ khóa}", lockAt);
}

/** Ngưỡng % hợp lệ: 0 < x ≤ 100, tối đa 2 chữ số thập phân (chuỗi, không số thực). */
export function validPct(s: string): boolean {
  const m = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(s.trim());
  if (!m) return false;
  const int = Number.parseInt(m[1]!, 10);
  const frac = m[2] ?? "";
  if (int > 100 || (int === 100 && /[1-9]/.test(frac))) return false;
  return int > 0 || /[1-9]/.test(frac);
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createPriceConfigApi(api: ApiClient, profile = "kn") {
  const query = { profile };
  return {
    calendar: (): Promise<PriceCalendar> =>
      unwrap(api.GET("/v1/price/calendar", { params: { query } })),
    saveSchedule: (body: PriceScheduleInput): Promise<PriceSchedule> =>
      unwrap(
        api.POST("/v1/price/calendar/schedule", {
          params: { query, ...idem() },
          body,
        }),
      ),
    addException: (body: {
      day: string;
      kind: "holiday" | "extra";
      reason: string;
    }): Promise<PriceCalendarException> =>
      unwrap(
        api.POST("/v1/price/calendar/exceptions", {
          params: { query, ...idem() },
          body,
        }),
      ),
    retireException: (day: string): Promise<unknown> =>
      unwrap(
        api.POST("/v1/price/calendar/exceptions/{day}/retire", {
          params: { path: { day }, query, ...idem() },
        }),
      ),
    runs: (
      from?: string,
      to?: string,
    ): Promise<{ from: string; to: string; items: PriceCalendarRun[] }> =>
      unwrap(
        api.GET("/v1/price/calendar/runs", {
          params: {
            query: {
              ...query,
              ...(from ? { from } : {}),
              ...(to ? { to } : {}),
            },
          },
        }),
      ),
    reminderTest: (): Promise<{ body: string }> =>
      unwrap(
        api.POST("/v1/price/calendar/reminder-test", {
          params: { query, ...idem() },
        }),
      ),
    thresholds: (): Promise<PriceThresholds> =>
      unwrap(api.GET("/v1/price/thresholds", { params: { query } })),
    saveThreshold: (body: PriceThresholdInput): Promise<PriceThreshold> =>
      unwrap(
        api.POST("/v1/price/thresholds", {
          params: { query, ...idem() },
          body,
        }),
      ),
    retireThreshold: (
      scope: "group" | "item",
      code: string,
    ): Promise<unknown> =>
      unwrap(
        api.POST("/v1/price/thresholds/{scope}/{code}/retire", {
          params: { path: { scope, code }, query, ...idem() },
        }),
      ),
  };
}

export type PriceConfigApi = ReturnType<typeof createPriceConfigApi>;
