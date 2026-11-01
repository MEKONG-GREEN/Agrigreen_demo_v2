// BF-SWEEP-PRICE: routes và kiểm nhập cho FN-AD-04.04–06. Giữ API và phạm vi do máy chủ quyết định.
import { validateForm, type NumberRule } from "@/components/form";
import {
  unknownVars,
  type PriceCalendar,
  type PriceScheduleInput,
} from "./api";
import { isCalendarDay } from "./calendar-routes";

export const PRICE_NUMBER_LENGTH = 32;
export const PERCENT_RULE = {
  required: true,
  positive: true,
  max: "100",
  maxDecimals: 2,
} as const;
export const LOOKBACK_RULE = {
  required: true,
  positive: true,
  integer: true,
  max: "60",
} as const;

const GROUP = /^[a-z][a-z0-9_]{0,39}$/;
const ITEM = /^MH[0-9]{5}$/;
const TIME = /^(?:[01]\d|2[0-3]):[0-5]\d$/;

export function scheduleInput(c: PriceCalendar): PriceScheduleInput {
  const current = c.upcoming.at(-1) ?? c.current;
  return {
    weekdays: [...current.weekdays],
    lock_at: current.lock_at,
    respond_by: current.respond_by,
    remind1_at: current.remind1_at,
    remind2_at: current.remind2_at,
    channels: [...current.channels],
    template: current.template,
    valid_from: c.earliest_valid_from,
  };
}

export type Errors = Partial<Record<string, string>>;

export function validateSchedule(
  form: PriceScheduleInput,
  earliest: string,
  reminder: boolean,
): Errors {
  const keys = reminder
    ? (["remind1_at", "remind2_at", "template"] as const)
    : (["lock_at", "respond_by", "valid_from"] as const);
  const values = Object.fromEntries(keys.map((k) => [k, form[k]]));
  const rules = Object.fromEntries(
    keys.map((k) => [
      k,
      {
        kind: "text" as const,
        required: true,
        maxLength: k === "template" ? 500 : 10,
      },
    ]),
  );
  const { errors } = validateForm(values, rules);
  for (const key of keys) {
    if (key !== "template" && key !== "valid_from" && !TIME.test(form[key]))
      errors[key] = "Nhập giờ hợp lệ (HH:MM).";
  }
  if (reminder) {
    if (!form.channels.length) errors.channels = "Chọn ít nhất một kênh nhắc.";
    if (
      TIME.test(form.remind1_at) &&
      TIME.test(form.remind2_at) &&
      form.remind1_at >= form.remind2_at
    )
      errors.remind2_at = "Giờ nhắc lần 2 phải sau lần 1.";
    if (TIME.test(form.remind2_at) && form.remind2_at >= form.lock_at)
      errors.remind2_at = "Giờ nhắc lần 2 phải trước giờ khóa.";
    const bad = unknownVars(form.template);
    if (bad.length)
      errors.template = `Nội dung mẫu có biến không hỗ trợ: ${bad.join(", ")}.`;
  } else {
    if (!form.weekdays.length)
      errors.weekdays = "Chọn ít nhất một ngày báo giá trong tuần.";
    if (
      TIME.test(form.respond_by) &&
      TIME.test(form.lock_at) &&
      form.respond_by <= form.lock_at
    )
      errors.respond_by = "Hạn phản hồi xác minh phải sau giờ khóa.";
    if (TIME.test(form.lock_at) && form.lock_at <= form.remind2_at)
      errors.lock_at = `Giờ khóa phải sau giờ nhắc lần 2 (${form.remind2_at}).`;
    if (!isCalendarDay(form.valid_from) || form.valid_from < earliest)
      errors.valid_from = `Ngày áp dụng sớm nhất ${earliest}.`;
  }
  return errors;
}

/** Chặn độ dài/số mũ TRƯỚC normalizeNumberInput/Decimal.parse trong NumberField và validateForm. */
export function numberTextError(
  raw: string,
  rule: NumberRule = {},
): string | undefined {
  if (raw.length > PRICE_NUMBER_LENGTH)
    return `Tối đa ${PRICE_NUMBER_LENGTH} ký tự.`;
  if (/[eE]/.test(raw)) return "Không nhập số dạng số mũ.";
  if (!/^[\d.,+\-\s]*$/.test(raw)) return "Không phải số hợp lệ.";
  let text = raw.replace(/\s/g, "");
  if (!text) return undefined;
  if (text.includes(",")) text = text.replace(/\./g, "").replace(",", ".");
  const match = /^([+-]?)(\d+)(?:\.(\d+))?$/.exec(text);
  if (!match) return "Không phải số hợp lệ.";
  const integer = (match[2] || "0").replace(/^0+(?=\d)/, "");
  const fraction = (match[3] ?? "").replace(/0+$/, "");
  const zero = integer === "0" && !fraction;
  if (match[1] === "-" && !zero && !rule.allowNegative)
    return "Không được nhập số âm.";
  if (rule.positive && (zero || match[1] === "-")) return "Phải lớn hơn 0.";
  if (rule.integer && fraction) return "Chỉ nhập số nguyên.";
  if (rule.maxDecimals !== undefined && fraction.length > rule.maxDecimals)
    return `Tối đa ${rule.maxDecimals} chữ số thập phân.`;
  // Hai biên nghiệp vụ cố định 100%/60 ngày là số nguyên: so chuỗi trước Decimal.parse/BigInt.
  if (rule.max !== undefined) {
    const max = String(rule.max);
    if (
      integer.length > max.length ||
      (integer.length === max.length &&
        (integer > max || (integer === max && !!fraction)))
    )
      return `Không được lớn hơn ${max}.`;
  }
  return undefined;
}

export interface ThresholdForm {
  scope: "default" | "group" | "item";
  code: string;
  pct: string;
  lookback: string;
  flat: string;
  survey: string;
}

export function validateThreshold(form: ThresholdForm) {
  const keys =
    form.scope === "default"
      ? (["pct", "lookback", "flat", "survey"] as const)
      : (["pct"] as const);
  const errors: Errors = {};
  if (!["default", "group", "item"].includes(form.scope)) {
    errors.scope = "Chọn phạm vi áp dụng.";
  }
  for (const key of keys) {
    const issue = numberTextError(
      form[key],
      key === "lookback" ? LOOKBACK_RULE : PERCENT_RULE,
    );
    if (issue) errors[key] = issue;
  }
  // Không gọi bộ chuẩn hóa số nếu có chuỗi không an toàn.
  if (Object.keys(errors).length) return { errors, values: form, valid: false };
  const pct = { kind: "number" as const, ...PERCENT_RULE };
  const result = validateForm(
    { ...form },
    form.scope === "default"
      ? {
          pct,
          lookback: { kind: "number", ...LOOKBACK_RULE },
          flat: pct,
          survey: pct,
        }
      : { pct, code: { kind: "text", required: true, maxLength: 40 } },
  );
  Object.assign(errors, result.errors);
  if (
    form.scope !== "default" &&
    !(form.scope === "group" ? GROUP : ITEM).test(form.code.trim())
  ) {
    errors.code =
      form.scope === "group"
        ? "Mã nhóm bắt đầu bằng chữ thường, chỉ gồm chữ, số, _; tối đa 40 ký tự."
        : "Mã mặt hàng có dạng MH và 5 chữ số.";
  }
  const values: ThresholdForm = {
    ...form,
    ...result.values,
    scope: form.scope,
    code: form.code.trim(),
  };
  return { errors, values, valid: Object.keys(errors).length === 0 };
}
