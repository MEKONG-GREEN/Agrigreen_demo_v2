import { validateForm } from "@/components/form";
export type AdjustmentForm = { leaving: string; additional: string; reason: string };
export const COUNT_RULE = { required: true, integer: true, max: 9999, maxInputLength: 32 } as const;
/** Go strings.TrimSpace uses Unicode White_Space; count codepoints, not UTF-16 units. */
export function trimReason(raw: string): string { return raw.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, ""); }
export function validateAdjustment(form: AdjustmentForm) {
  const checked = validateForm(form, { leaving: { kind: "number", ...COUNT_RULE }, additional: { kind: "number", ...COUNT_RULE } });
  const errors: Partial<Record<keyof AdjustmentForm, string>> = { ...checked.errors };
  const reason = trimReason(form.reason);
  const count = reason.length > 1000 ? 501 : Array.from(reason).length;
  if (count < 10 || count > 500) errors.reason = "Lý do điều chỉnh phải từ 10 đến 500 ký tự.";
  return { errors, valid: Object.keys(errors).length === 0, values: { ...checked.values, reason } };
}
