import { validateForm } from "@/components/form";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type AppraisalForm = S["HrmAppraisalForm"];
export type AppraisalAnswer = S["HrmAppraisalAnswer"];
export type AppraisalRating = S["HrmAppraisalRating"];
export const APPRAISAL_RATINGS = ["HTXSNV", "HTTNV", "HTNV", "KHTNV"] as const;
export const APPRAISAL_RATING_LABEL: Record<AppraisalRating, string> = {
  "": "Chưa có", HTXSNV: "Hoàn thành xuất sắc nhiệm vụ", HTTNV: "Hoàn thành tốt nhiệm vụ",
  HTNV: "Hoàn thành nhiệm vụ", KHTNV: "Không hoàn thành nhiệm vụ",
};

/** Chặn độ dài/cú pháp/biên bằng chuỗi trước validateForm → Decimal/BigInt. */
export function appraisalPointGuard(raw: string, maximum: string): string | null {
  if (raw.length > 32) return "Tối đa 32 ký tự số.";
  const value = raw.trim().replace(",", ".");
  if (!/^[0-9]{1,7}(?:\.[0-9]{1,2})?$/.test(value)) return "Nhập số thập phân thường không âm, tối đa hai số lẻ.";
  if (!/^[0-9]{1,7}(?:\.[0-9]{1,2})?$/.test(maximum)) return "Điểm tối đa của mẫu không hợp lệ; tải lại mẫu.";
  const parts = (v: string) => {
    const [whole = "0", fraction = ""] = v.split(".");
    return [whole.replace(/^0+/, "") || "0", fraction.padEnd(2, "0")] as const;
  };
  const [w, f] = parts(value), [mw, mf] = parts(maximum);
  if (w.length > mw.length || w.length === mw.length && (w > mw || w === mw && f > mf)) return `Không vượt quá ${maximum} điểm.`;
  return null;
}

export function validateAppraisalAnswers(form: AppraisalForm, answers: AppraisalAnswer[], submit: boolean) {
  const errors: Record<string, string> = {};
  const normalized: AppraisalAnswer[] = [];
  for (const [i, criterion] of form.criteria.entries()) {
    const answer = answers.find((x) => x.code === criterion.code) ?? { code: criterion.code, points: "", note: "" };
    const field = `answers.${i}.points`;
    if (!answer.points.trim() && !submit) {
      normalized.push({ ...answer, points: "" });
      continue;
    }
    const guard = appraisalPointGuard(answer.points, criterion.maximum);
    if (guard) errors[field] = guard;
    else {
      const checked = validateForm({ points: answer.points }, { points: { kind: "number", required: submit, max: criterion.maximum, maxDecimals: 2, maxInputLength: 32 } });
      if (checked.errors.points) errors[field] = checked.errors.points;
      normalized.push({ ...answer, points: checked.values.points });
    }
    if (answer.note.length > 2000) errors[`answers.${i}.note`] = "Tối đa 2.000 ký tự.";
  }
  if (answers.some((a) => !form.criteria.some((c) => c.code === a.code)) || new Set(answers.map((a) => a.code)).size !== answers.length) errors.answers = "Phiếu có tiêu chí lạ/trùng; tải lại mẫu.";
  return { valid: Object.keys(errors).length === 0, errors, answers: normalized };
}

export function appraisalSuggestion(form: AppraisalForm, answers: AppraisalAnswer[]): { total: string; rating: AppraisalRating } | null {
  const checked = validateAppraisalAnswers(form, answers, true);
  if (!checked.valid) return null;
  const units = (raw: string) => {
    const [whole = "0", fraction = ""] = raw.split(".");
    return BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0"));
  };
  const sum = checked.answers.reduce((n, a) => n + units(a.points), 0n);
  let rating: AppraisalRating = "";
  for (const band of form.bands) {
    if (appraisalPointGuard(band.minimum, "1000000")) return null;
    if (!rating && sum >= units(band.minimum)) rating = band.rating;
  }
  const fraction = String(sum % 100n).padStart(2, "0").replace(/0+$/, "");
  return { total: `${sum / 100n}${fraction ? `.${fraction}` : ""}`, rating };
}

export function appraisalYear(raw: string): number | null {
  if (!/^[0-9]{4}$/.test(raw)) return null;
  if (raw < "2020" || raw > "2100") return null;
  return Number(raw);
}
