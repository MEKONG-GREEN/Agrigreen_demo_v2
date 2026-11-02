import { validateForm } from "@/components/form";
import type { KhtcReportType } from "./api";

export function localPeriodDate(iso: string): string { return new Date(Date.parse(iso) + 7 * 3_600_000).toISOString().slice(0, 16); }
export function periodDateISO(raw: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(raw)) return null;
  const value = new Date(`${raw}:00+07:00`);
  if (!Number.isFinite(value.getTime()) || localPeriodDate(value.toISOString()) !== raw) return null;
  return value.toISOString();
}
function dateValid(raw: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(raw) && periodDateISO(`${raw}T00:00`) !== null;
}
export function validatePeriodForm(type: KhtcReportType, values: { name: string; asOf: string; from: string; to: string; cutoff: string; due: string; exportDue: string }) {
  const checked = validateForm(values, {
    name: { kind: "text", required: type === "adhoc", maxLength: 500 },
    asOf: { kind: "text", required: type === "program" },
    from: { kind: "text", required: type === "adhoc" },
    to: { kind: "text", required: type === "adhoc" },
    due: { kind: "text", required: type === "program" || type === "adhoc" },
  });
  const errors: Record<string, string> = { ...checked.errors };
  for (const key of ["asOf", "from", "to", "cutoff"] as const) if (values[key] && !dateValid(values[key])) errors[key] = "Nhập ngày hợp lệ.";
  for (const key of ["due", "exportDue"] as const) if (values[key] && !periodDateISO(values[key])) errors[key] = "Nhập ngày giờ hợp lệ theo giờ Việt Nam.";
  if (type === "adhoc" && values.from && values.to && values.from > values.to) errors.to = "Đến ngày không được trước Từ ngày.";
  return errors;
}
