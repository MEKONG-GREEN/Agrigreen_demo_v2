export const PLAN_BASE = "/kn/nhan-su/ke-hoach";
export type AdjustmentRoute = { year: number; unit: string; mode: "edit" | "history" | "detail"; seq?: number };
export function parseAdjustmentRoute(year: string, unit: string, mode: AdjustmentRoute["mode"], rawSeq?: string): AdjustmentRoute | null {
  if (!/^(20[2-9][0-9]|2100)$/.test(year) || !unit || unit.length > 200 || /[/\\?#\x00-\x1f]/.test(unit)) return null;
  if (mode !== "detail") return { year: Number(year), unit, mode };
  if (!rawSeq || !/^[1-9][0-9]{0,15}$/.test(rawSeq)) return null;
  const seq = Number(rawSeq);
  return Number.isSafeInteger(seq) ? { year: Number(year), unit, mode, seq } : null;
}
export function planUnitHref(year: number, unit: string): string { return `${PLAN_BASE}/${year}/${encodeURIComponent(unit)}`; }
export function adjustmentHref(year: number, unit: string, target?: "history" | number): string {
  const suffix = target === "history" ? "/lich-su" : target === undefined ? "" : `/${target}`;
  return `${planUnitHref(year, unit)}/dieu-chinh${suffix}`;
}
