import type { PriceTicketView } from "./api";

export interface HistoryFilters {
  from: string;
  to: string;
  view: "list" | "calendar";
  month: string;
  status: PriceTicketView | "";
  point: string;
}
const statuses: readonly string[] = ["not_entered", "draft", "submitted", "locked", "late", "approved", "missed", "deleted"];

export function historyDay(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && value >= "0001-01-01" &&
    Number.isFinite(Date.parse(`${value}T00:00:00Z`)) && new Date(`${value}T00:00:00Z`).toISOString().slice(0, 10) === value;
}
export function shiftHistoryDay(day: string, delta: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + delta);
  return d.toISOString().slice(0, 10);
}
export function shiftHistoryMonth(month: string, delta: number): string {
  const d = new Date(`${month}-01T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() + delta);
  return d.toISOString().slice(0, 7);
}
export function historyFilters(q: Pick<URLSearchParams, "get">, today: string): HistoryFilters {
  const from = q.get("from") || shiftHistoryDay(today, -30);
  const to = q.get("to") || today;
  const status = q.get("status") || "";
  const first = historyDay(from) ? from.slice(0, 7) : today.slice(0, 7);
  const last = historyDay(to) ? to.slice(0, 7) : first;
  const m = q.get("month") || last;
  const month = /^\d{4}-(0[1-9]|1[0-2])$/.test(m) && m >= first && m <= last ? m : last;
  return { from, to, view: q.get("view") === "calendar" ? "calendar" : "list", month,
    status: statuses.includes(status) ? status as PriceTicketView : "", point: q.get("point") || "" };
}
export function historyRangeProblem(f: Pick<HistoryFilters, "from" | "to">): string {
  if (!historyDay(f.from) || !historyDay(f.to)) return "Nhập ngày hợp lệ theo định dạng YYYY-MM-DD.";
  if (f.to < f.from) return "Đến ngày phải từ Từ ngày trở đi.";
  if (Date.parse(f.to) - Date.parse(f.from) > 366 * 86_400_000) return "Khoảng thời gian tối đa 12 tháng — thu hẹp lại.";
  return "";
}
export function historyHref(f: HistoryFilters): string {
  const q = new URLSearchParams({ from: f.from, to: f.to, view: f.view, month: f.month });
  if (f.status) q.set("status", f.status);
  if (f.point) q.set("point", f.point);
  return `/kn/bao-gia/lich-su?${q}`;
}
export function historyMonthDays(month: string): (string | null)[] {
  const first = `${month}-01`;
  const next = new Date(`${first}T00:00:00Z`);
  next.setUTCMonth(next.getUTCMonth() + 1);
  const count = next.getTime() - Date.parse(`${first}T00:00:00Z`);
  const offset = (new Date(`${first}T00:00:00Z`).getUTCDay() + 6) % 7;
  return [...Array<null>(offset).fill(null), ...Array.from({ length: count / 86_400_000 }, (_, i) => shiftHistoryDay(first, i))];
}
