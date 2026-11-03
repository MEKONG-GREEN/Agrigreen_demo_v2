export const PLAN_BASE = "/kn/khtc/ke-hoach-nam";
export type PlanView = "list" | "save" | "history" | "version";
export interface PlanRoute {
  base: string; year: number; view: PlanView; version?: number;
  go: (href: string) => void;
}
export function planHref(base: string, year: number, view: PlanView = "list", version?: number): string {
  const tail = view === "save" ? "/luu-phien-ban" : view === "history" ? "/lich-su" : view === "version" ? `/phien-ban/${version}` : "";
  return `${base}${tail}?year=${year}`;
}
/** No Number/parseInt until a bounded canonical URL value has been checked. */
export function parsePlanRoute(path: string, search: URLSearchParams, currentYear: number): Omit<PlanRoute, "go"> | null {
  const base = path.match(/^\/kn\/(?:khtc|phong-ban\/[a-z_]+)\/ke-hoach-nam(?=\/|$)/)?.[0];
  if (!base || search.getAll("year").length > 1) return null;
  const rawYear = search.get("year") ?? String(currentYear);
  if (!/^(20\d{2}|2100)$/.test(rawYear)) return null;
  const year = Number(rawYear);
  const tail = path.slice(base.length);
  if (tail === "" || tail === "/") return { base, year, view: "list" };
  if (tail === "/luu-phien-ban") return { base, year, view: "save" };
  if (tail === "/lich-su") return { base, year, view: "history" };
  const match = /^\/phien-ban\/([1-9][0-9]{0,9})$/.exec(tail);
  if (!match) return null;
  const version = Number(match[1]);
  return version <= 2147483647 ? { base, year, view: "version", version } : null;
}
