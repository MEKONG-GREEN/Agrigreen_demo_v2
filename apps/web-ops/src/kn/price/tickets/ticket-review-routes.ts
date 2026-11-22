export const REVIEW_BASE = "/kn/bao-gia/kiem-duyet";
export type ReviewAction = "view" | "correct" | "redate" | "delete";
export type ReviewRoute = { kind: "list" } | { kind: "ticket"; id: string; action: ReviewAction } | { kind: "invalid" };
export interface ReviewFilters { day: string; mine: boolean; tab: "tickets" | "alerts" }
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const actions: Record<string, ReviewAction> = { "hieu-chinh": "correct", "doi-ngay": "redate", xoa: "delete" };
export function calendarDay(raw: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return false;
  const d = new Date(`${raw}T00:00:00Z`);
  return Number.isFinite(d.getTime()) && d.toISOString().slice(0, 10) === raw;
}
export function reviewRoute(path: readonly string[]): ReviewRoute {
  if (!path.length) return { kind: "list" };
  if (path[0] !== "phieu" || !uuid.test(path[1] ?? "") || path.length < 2 || path.length > 3) return { kind: "invalid" };
  const action = path.length === 2 ? "view" : actions[path[2]!];
  return action ? { kind: "ticket", id: path[1]!, action } : { kind: "invalid" };
}
export function reviewFilters(q: Pick<URLSearchParams, "get">, today: string): ReviewFilters {
  const day = q.get("day") ?? "";
  return { day: calendarDay(day) ? day : today, mine: q.get("mine") !== "false", tab: q.get("tab") === "alerts" ? "alerts" : "tickets" };
}
export function reviewHref(f: ReviewFilters, id?: string, action: ReviewAction = "view"): string {
  const tail = { view: "", correct: "/hieu-chinh", redate: "/doi-ngay", delete: "/xoa" }[action];
  const path = id && uuid.test(id) ? `/phieu/${id}${tail}` : "";
  return `${REVIEW_BASE}${path}?${new URLSearchParams({ day: f.day, mine: String(f.mine), tab: f.tab })}`;
}
