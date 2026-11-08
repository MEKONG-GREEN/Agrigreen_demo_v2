import { Decimal, formatDecimalVi } from "@/lib/exactDecimal";
import type { WeeklyPatch, WeeklyRow } from "./api";

export type Draft = Partial<Record<"progress" | "money" | "money_override" | "evaluation" | "note", string>>;
export type Drafts = Record<string, Draft>;
export type FieldErrors = Record<string, string>;

/** Bound ordinary decimal syntax before BigInt parsing, using the server's quantity/money limits. */
export function weeklyNumber(raw: string, money = false): Decimal | null {
  const value = raw.trim().replace(",", ".");
  if (value.length < 1 || value.length > 24 || !/^[0-9]+(?:\.[0-9]+)?$/.test(value)) return null;
  const [whole = "", fraction] = value.split(".");
  if (whole.length > (money ? 18 : 15) || (money && fraction !== undefined) || (fraction?.length ?? 0) > 4) return null;
  return Decimal.parse(value);
}

export function weeklyPatches(rows: WeeklyRow[], drafts: Drafts): { rows: WeeklyPatch[]; errors: FieldErrors } {
  const patches: WeeklyPatch[] = [], errors: FieldErrors = {};
  for (const row of rows) {
    const draft = drafts[row.id];
    if (!draft || row.group) continue;
    const patch: WeeklyPatch = { id: row.id };
    for (const field of ["progress", "money", "money_override"] as const) {
      const value = draft[field];
      if (value === undefined) continue;
      if (field === "money_override" && !value.trim()) { patch.clear_override = true; continue; }
      const parsed = weeklyNumber(value.trim() || "0", field !== "progress");
      if (!parsed) errors[`${row.id}.${field}`] = field === "progress" ? "Nhập số không âm, tối đa 15 chữ số và 4 số lẻ." : "Nhập số đồng không âm, tối đa 18 chữ số.";
      else patch[field] = parsed.toString();
    }
    for (const field of ["evaluation", "note"] as const) {
      const value = draft[field];
      if (value === undefined) continue;
      if (Array.from(value).length > 2000) errors[`${row.id}.${field}`] = "Tối đa 2000 ký tự.";
      else patch[field] = value;
    }
    patches.push(patch);
  }
  return { rows: patches, errors };
}

/** Recompute AC01–03 on every edit; invalid input never silently becomes zero. */
export function weeklyBalances(row: WeeklyRow, draft: Draft = {}) {
  const progress = weeklyNumber(draft.progress === undefined ? row.progress : draft.progress.trim() || "0");
  const money = weeklyNumber(draft.money === undefined ? row.money : draft.money.trim() || "0", true);
  const override = draft.money_override === undefined ? row.money_override : draft.money_override.trim();
  const startProgress = weeklyNumber(row.opening_progress), startMoney = weeklyNumber(row.opening_money, true);
  const cumulativeProgress = progress && startProgress ? startProgress.add(progress) : null;
  const cumulativeMoney = override ? weeklyNumber(override, true) : money && startMoney ? startMoney.add(money) : null;
  const plan = weeklyNumber(row.adjusted_plan ?? row.plan);
  const rate = cumulativeProgress && plan && plan.sign() > 0 ? cumulativeProgress.mul(Decimal.fromInt(100)).divRound(plan, 2) : null;
  return { progress: cumulativeProgress, money: cumulativeMoney, rate, achieved: rate !== null && rate.cmp(Decimal.fromInt(100)) >= 0 };
}

export const displayWeekly = (value: string) => formatDecimalVi(value, { maxDecimals: 4, group: true });
export const rateWeekly = (value: Decimal | null) => value === null ? "Chưa có quy mô kế hoạch" : `đạt ${formatDecimalVi(value.toString(), { minDecimals: 2, maxDecimals: 2 })}% KH`;
