import { validateForm } from "@/components/form";

export interface VersionHead { decision_no: string; decision_date: string; effective_from: string; note: string }
export const newVersionHead = (year: number, adjust: boolean): VersionHead => ({ decision_no: "", decision_date: "", effective_from: adjust ? "" : `${year}-01-01`, note: "" });
export function calendarDate(raw: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return false;
  const d = new Date(`${raw}T00:00:00Z`);
  return Number.isFinite(d.getTime()) && d.toISOString().slice(0, 10) === raw;
}
export function validateVersionHead(year: number, adjust: boolean, head: VersionHead) {
  const checked = validateForm(head, {
    decision_no: { kind: "text", required: adjust, maxLength: 100 },
    decision_date: { kind: "text", required: adjust, maxLength: 10 },
    effective_from: { kind: "text", required: adjust, maxLength: 10 },
    note: { kind: "text", maxLength: 1000 },
  });
  const errors: Record<string, string> = { ...checked.errors };
  for (const field of ["decision_date", "effective_from"] as const) {
    if (head[field] && !calendarDate(head[field])) errors[field] = "Nhập ngày YYYY-MM-DD hợp lệ.";
  }
  if (head.effective_from && !errors.effective_from && head.effective_from.slice(0, 4) !== String(year)) errors.effective_from = `Ngày hiệu lực phải trong năm ${year}.`;
  return { errors, valid: Object.keys(errors).length === 0 };
}
