import { compareExact, validateForm, type FieldRule } from "@/components/form";
import type { HrmScale, HrmScaleInput } from "./api";

export const scaleToday = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);

export function validScaleDay(day: string): boolean {
  if (day.length !== 10 || !/^\d{4}-\d{2}-\d{2}$/.test(day) || day === "0001-01-01") return false;
  const parsed = new Date(`${day}T00:00:00Z`);
  return !Number.isNaN(parsed.valueOf()) && parsed.toISOString().slice(0, 10) === day;
}

export const scaleListPath = (base: string, day: string) => `${base}/he-so?at=${encodeURIComponent(day)}`;
export const scalePath = (base: string, grade: string, day: string, create = false) => `${base}/he-so/${encodeURIComponent(grade)}${create ? "/them-moi" : ""}?at=${encodeURIComponent(day)}`;

/** Đọc lịch sử theo ngày hiệu lực, rồi phiên bản; không sửa mảng của API/cache. */
export function effectiveScale(versions: HrmScale[], day: string): HrmScale | null {
  return [...versions].filter((v) => v.valid_from <= day).sort((a, b) => b.valid_from.localeCompare(a.valid_from) || b.version - a.version)[0] ?? null;
}

export type ScaleValues = { from: string; hold: string; coefficients: string[]; note: string };

/** Giới hạn và cú pháp trước normalizeNumberInput/BigInt; biên khớp domain.ValidateScale. */
export function validateScale(values: ScaleValues) {
  const errors: Record<string, string> = {};
  const numeric = { hold: values.hold, ...Object.fromEntries(values.coefficients.map((raw, i) => [`coefficient_${i}`, raw])) };
  for (const [key, raw] of Object.entries(numeric)) {
    if (raw.length > 32) errors[key] = "Tối đa 32 ký tự số.";
    else {
      const trimmed = raw.trim();
      const decimal = trimmed.includes(",") ? trimmed.replace(/\./g, "").replace(",", ".") : trimmed;
      if (trimmed && !/^\d+(\.\d+)?$/.test(decimal)) errors[key] = "Chỉ nhập số thập phân thường, không dùng số mũ.";
    }
  }
  if (!validScaleDay(values.from)) errors.from = "Bắt buộc chọn ngày hiệu lực hợp lệ.";
  if (values.coefficients.length < 1 || values.coefficients.length > 20) errors.coefficients = "Bảng có từ 1 đến 20 bậc.";
  if ([...values.note].length > 500) errors.note = "Tối đa 500 ký tự.";
  if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
  const rules: Record<string, FieldRule> = { hold: { kind: "number", required: true, integer: true, positive: true, max: 120, maxInputLength: 32 } };
  values.coefficients.forEach((_, i) => { rules[`coefficient_${i}`] = { kind: "number", required: true, positive: true, max: "99.99", maxDecimals: 2, maxInputLength: 32 }; });
  const checked = validateForm(numeric, rules);
  Object.assign(errors, checked.errors);
  if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
  const coefficients = values.coefficients.map((_, i) => checked.values[`coefficient_${i}`]!);
  coefficients.forEach((value, i) => {
    if (i > 0 && compareExact(value, coefficients[i - 1]!) <= 0) errors[`coefficient_${i}`] = "Hệ số phải lớn hơn bậc trước.";
  });
  if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
  const body: HrmScaleInput = { valid_from: values.from, hold_months: Number(checked.values.hold), coefficients, ...(values.note.trim() ? { note: values.note.trim() } : {}) };
  return { valid: true as const, errors, body };
}
