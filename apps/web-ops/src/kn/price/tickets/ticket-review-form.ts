import { compareExact, validateForm, type FieldRule, type NumberRule } from "@/components/form";
import type { ApiProblem } from "@/api/problem";
import type { PriceSheet } from "./api";
import { calendarDay, type ReviewAction } from "./ticket-review-routes";
export type ReviewFields = Record<string, string>;
export type ReviewErrors = Partial<Record<string, string>>;
export const PRICE_REVIEW_RULE: NumberRule = { required: true, positive: true, max: "999999999999.99", maxDecimals: 2, maxInputLength: 512 };
export const fieldKey = (code: string, side: "min" | "max") => `${code}.${side}`;
export function reviewInitial(sheet: PriceSheet): ReviewFields {
  const out: ReviewFields = { reason: "", day: sheet.day };
  for (const l of sheet.lines) { out[fieldKey(l.item_code, "min")] = ""; out[fieldKey(l.item_code, "max")] = ""; }
  return out;
}
// Matches strings.TrimSpace/unicode.IsSpace in the native ticket contract.
// NEL is whitespace; BOM/FEFF is deliberately not. Count Unicode codepoints, not UTF-16 units.
function reviewReason(raw: string): { value: string; error?: string } {
  const value = raw.replace(/^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/gu, "");
  if (!value) return { value, error: "Bắt buộc nhập." };
  let count = 0;
  for (const rune of value) { if (rune && ++count > 500) return { value, error: "Tối đa 500 ký tự Unicode." }; }
  return { value };
}
/** Only selected pairs are sent; untouched original/adjusted lines remain on the server. */
export function validateReview(sheet: PriceSheet, action: ReviewAction, fields: ReviewFields) {
  const rules: Record<string, FieldRule> = {};
  const reason = reviewReason(fields.reason ?? "");
  const selected = action === "correct" ? sheet.lines.filter(l => (fields[fieldKey(l.item_code, "min")] ?? "").trim() !== "" || (fields[fieldKey(l.item_code, "max")] ?? "").trim() !== "") : [];
  for (const l of selected) for (const side of ["min", "max"] as const) rules[fieldKey(l.item_code, side)] = { kind: "number", ...PRICE_REVIEW_RULE };
  if (action === "redate") rules.day = { kind: "text", required: true };
  const result = validateForm(fields, rules);
  const errors: ReviewErrors = { ...result.errors };
  if (reason.error) errors.reason = reason.error;
  if (action === "correct" && !selected.length) errors.lines = "Chọn ít nhất một dòng để hiệu chỉnh.";
  if (action === "redate" && fields.day && !calendarDay(fields.day)) errors.day = "Ngày không hợp lệ.";
  for (const l of selected) {
    const lo = fieldKey(l.item_code, "min"), hi = fieldKey(l.item_code, "max");
    if (l.no_goods || !l.min || !l.max) errors[lo] = "Chỉ hiệu chỉnh dòng có giá Xã gửi.";
    if (!errors[lo] && !errors[hi] && compareExact(result.values[lo]!, result.values[hi]!) > 0) errors[hi] = "Giá cao nhất phải lớn hơn hoặc bằng giá thấp nhất.";
  }
  return { valid: Object.keys(errors).length === 0, errors, reason: reason.value, day: fields.day ?? "", lines: selected.map(l => ({ item_code: l.item_code, min: result.values[fieldKey(l.item_code, "min")]!, max: result.values[fieldKey(l.item_code, "max")]! })) };
}
export function reviewServerErrors(problem: ApiProblem | null, fields: ReviewFields): ReviewErrors {
  const out: ReviewErrors = {};
  for (const e of problem?.fieldErrors ?? []) {
    if (e.field && e.reason) {
      if (e.field === "min" || e.field === "max") for (const key of Object.keys(fields).filter(k => k.endsWith(`.${e.field}`) && fields[k]?.trim())) out[key] = e.reason;
      else out[e.field] = e.reason;
    }
  }
  if (problem?.code === "price.ticket_exists") out.day = problem.message;
  return out;
}
