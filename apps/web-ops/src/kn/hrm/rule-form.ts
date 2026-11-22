import { compareExact, validateForm, type FieldRule } from "@/components/form";
import { type HrmAchievementConversion, type HrmItem, type HrmPromotionRules, type HrmRule, type HrmRuleKey, vnDecimal } from "./api";
import { scaleToday, validScaleDay } from "./scale-form";

export type PromotionNumber = "rating_years" | "achievement_years" | "nlth_quota_pct" | "nlth_retirement_months" | "nlth_retirement_within_months" | "tnvk_start_pct" | "tnvk_step_pct" | "tnvk_cycle_months" | "tnvk_first_months" | "retirement_age_male_months" | "retirement_age_female_months";
export const PROMOTION_INPUTS: readonly { key: PromotionNumber; label: string; min: number; max: number; pct?: boolean }[] = [
 { key: "rating_years", label: "Số năm xếp loại xét", min: 1, max: 10 },
 { key: "achievement_years", label: "Số năm thành tích xét (NLTH)", min: 1, max: 10 },
 { key: "nlth_quota_pct", label: "Tỷ lệ chỉ tiêu NLTH (%)", min: 0, max: 100, pct: true },
 { key: "nlth_retirement_months", label: "NLTH nghỉ hưu: số tháng nâng trước tối đa", min: 0, max: 36 },
 { key: "nlth_retirement_within_months", label: "NLTH nghỉ hưu: trong vòng (tháng)", min: 0, max: 36 },
 { key: "tnvk_start_pct", label: "% TNVK khởi điểm", min: 0, max: 100, pct: true },
 { key: "tnvk_step_pct", label: "% TNVK tăng mỗi chu kỳ", min: 0, max: 100, pct: true },
 { key: "tnvk_cycle_months", label: "Chu kỳ TNVK (tháng)", min: 1, max: 60 },
 { key: "tnvk_first_months", label: "Giữ bậc cuối trước TNVK lần đầu (tháng)", min: 1, max: 120 },
 { key: "retirement_age_male_months", label: "Tuổi nghỉ hưu nam (tháng tuổi)", min: 600, max: 840 },
 { key: "retirement_age_female_months", label: "Tuổi nghỉ hưu nữ (tháng tuổi)", min: 600, max: 840 },
];
export const RULE_RATINGS = ["HTXSNV", "HTTNV", "HTNV", "KHTNV"] as const;
export const ruleToday = scaleToday;
export const validRuleDay = validScaleDay;
export function isRuleKey(value: unknown): value is HrmRuleKey { return value === "promotion" || value === "achievement_conversion"; }
export function rulePath(base: string, key: HrmRuleKey, day: string, create = false, version?: number) {
 const query = new URLSearchParams({ at: day });
 if (version !== undefined) query.set("version", String(version));
 return `${base}/quy-tac/${key}${create ? "/them-moi" : ""}?${query}`;
}
export function parseRuleVersion(value: string | null): number | null | undefined {
 if (value === null) return undefined;
 if (!/^[1-9][0-9]{0,15}$/.test(value) || value.length > 16 || (value.length === 16 && value > "9007199254740991")) return null;
 return Number(value); // Safe integer, after bounded plain syntax and lexical maximum.
}
export function effectiveRule(versions: HrmRule[], day: string) {
 return [...versions].filter((v) => v.valid_from <= day).sort((a,b) => b.valid_from.localeCompare(a.valid_from) || b.version-a.version)[0] ?? null;
}
export type CodeMonths = { code: string; months: string };
export type RuleValues = { from: string; note: string; rating: string; fields: Record<PromotionNumber,string>; reminders: string[]; rows: CodeMonths[] };
export function initialRuleValues(key: HrmRuleKey, base: HrmRule | null, day: string): RuleValues {
 const p = base?.payload as unknown as HrmPromotionRules | undefined;
 return { from: day, note: "", rating: key === "promotion" ? p?.min_rating ?? "" : "",
  fields: Object.fromEntries(PROMOTION_INPUTS.map((f) => [f.key, key === "promotion" && p?.[f.key] !== undefined ? f.pct ? vnDecimal(String(p[f.key])) : String(p[f.key]) : ""])) as Record<PromotionNumber,string>,
  reminders: key === "promotion" ? p?.remind_months?.map(String) ?? ["1","3","6"] : [],
  rows: key === "promotion" ? Object.entries(p?.discipline_extension_months ?? {}).map(([code, months]) => ({ code, months: String(months) }))
   : ((base?.payload as unknown as HrmAchievementConversion | undefined)?.rules ?? []).map((r) => ({ code: r.award_code, months: String(r.months) })),
 };
}

/** Raw length/plain syntax before validateForm's normalization/BigInt; domain bounds before Number(). */
export function validateRule(key: HrmRuleKey, values: RuleValues, references: HrmItem[]) {
 const errors: Record<string,string> = {};
 if (!validRuleDay(values.from)) errors.from = "Bắt buộc chọn ngày hiệu lực hợp lệ.";
 if ([...values.note].length > 500) errors.note = "Tối đa 500 ký tự.";
 if (key === "achievement_conversion" && (values.rows.length < 1 || values.rows.length > 50)) errors.rows = "Bảng quy đổi có từ 1 đến 50 dòng.";
 if (key === "promotion" && (values.reminders.length < 1 || values.reminders.length > 6)) errors.reminders = "Có từ 1 đến 6 mốc nhắc.";
 if (values.rows.length > references.length) errors.rows = "Mỗi mục danh mục chỉ có một dòng; kiểm lại các mục đã chọn.";
 if (errors.rows || errors.reminders) return { valid: false as const, errors, body: null };
 const numeric: Record<string,string> = {};
 const rules: Record<string,FieldRule> = {};
 if (key === "promotion") {
  PROMOTION_INPUTS.forEach((f) => { numeric[f.key] = values.fields[f.key]; rules[f.key] = { kind: "number", required: true, integer: !f.pct, max: f.max, maxDecimals: f.pct ? 2 : 0, maxInputLength: 32 }; });
  values.reminders.forEach((value,i) => { numeric[`reminder_${i}`] = value; rules[`reminder_${i}`] = { kind: "number", required: true, positive: true, integer: true, max: 24, maxInputLength: 32 }; });
  if (!RULE_RATINGS.includes(values.rating as typeof RULE_RATINGS[number])) errors.rating = "Bắt buộc chọn xếp loại hợp lệ.";
 }
 const seen = new Set<string>();
 values.rows.forEach((row,i) => {
  if (!row.code) errors[`row_${i}_code`] = "Bắt buộc chọn mục danh mục.";
  else if (!references.some((r) => r.code === row.code && r.active)) errors[`row_${i}_code`] = "Mục không có hoặc đã ngừng sử dụng.";
  else if (seen.has(row.code)) errors[`row_${i}_code`] = "Mục danh mục bị trùng.";
  seen.add(row.code);
  numeric[`row_${i}_months`] = row.months;
  rules[`row_${i}_months`] = { kind: "number", required: true, positive: key === "achievement_conversion", integer: true, max: key === "promotion" ? 60 : 36, maxInputLength: 32 };
 });
 for (const [field,raw] of Object.entries(numeric)) {
  if (raw.length > 32) { errors[field] = "Tối đa 32 ký tự số."; continue; }
  const text = raw.trim();
  if (!text) { errors[field] = "Bắt buộc nhập."; continue; }
  const plain = text.includes(",") ? /^(?:[0-9]+|[0-9]{1,3}(?:\.[0-9]{3})+),[0-9]+$/.test(text) : /^[0-9]+(?:\.[0-9]+)?$/.test(text);
  if (text && !plain) errors[field] = "Nhập số thập phân thường không âm, không dùng số mũ.";
 }
 if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
 const checked = validateForm(numeric,rules);
 Object.assign(errors,checked.errors);
 if (key === "promotion") {
  PROMOTION_INPUTS.forEach((f) => { const value = checked.values[f.key]; if (!errors[f.key] && compareExact(value!,String(f.min)) < 0) errors[f.key] = `Không nhỏ hơn ${f.min}.`; });
  values.reminders.forEach((_,i) => {
   if (i > 0 && !errors[`reminder_${i}`] && !errors[`reminder_${i-1}`] && compareExact(checked.values[`reminder_${i}`]!,checked.values[`reminder_${i-1}`]!) <= 0) errors[`reminder_${i}`] = "Mốc nhắc phải tăng dần, không trùng.";
  });
 }
 if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
 const n = (field: string) => Number(checked.values[field]); // Every value now plain, bounded, integer where needed.
 const payload: HrmPromotionRules | HrmAchievementConversion = key === "achievement_conversion" ? { rules: values.rows.map((r,i) => ({ award_code: r.code, months: n(`row_${i}_months`) })) } : {
  min_rating: values.rating as HrmPromotionRules["min_rating"], rating_years: n("rating_years"), achievement_years: n("achievement_years"),
  nlth_quota_pct: checked.values.nlth_quota_pct!, nlth_retirement_months: n("nlth_retirement_months"), nlth_retirement_within_months: n("nlth_retirement_within_months"),
  tnvk_start_pct: checked.values.tnvk_start_pct!, tnvk_step_pct: checked.values.tnvk_step_pct!, tnvk_cycle_months: n("tnvk_cycle_months"), tnvk_first_months: n("tnvk_first_months"),
  retirement_age_male_months: n("retirement_age_male_months"), retirement_age_female_months: n("retirement_age_female_months"),
  remind_months: values.reminders.map((_,i) => n(`reminder_${i}`)), discipline_extension_months: Object.fromEntries(values.rows.map((r,i) => [r.code,n(`row_${i}_months`)])),
 };
 return { valid: true as const, errors, body: { valid_from: values.from, payload, ...(values.note.trim() ? { note: values.note.trim() } : {}) } };
}
