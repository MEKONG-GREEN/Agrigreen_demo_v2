// Strict routes and validation for BF-SWEEP-PRICE-FORMS (points slice).
import { isCalendarDay } from "../calendar-routes";
export const POINTS_BASE = "/kn/bao-gia/xa-bao-gia";
export type PointRoute =
  | { kind: "point-new" }
  | { kind: "point"; code: string; mode: "view" | "edit" | "officers" }
  | { kind: "region-new" }
  | { kind: "region"; code: string; edit: boolean };
export function parsePointRoute(parts: readonly string[]): PointRoute | null {
  const [first, second, third] = parts;
  if (parts.length === 1 && first === "them-moi") return { kind: "point-new" };
  if (first === "nhom-vung") {
    if (parts.length === 2 && second === "them-moi")
      return { kind: "region-new" };
    if (
      second &&
      /^[a-z][a-z0-9_]{0,39}$/.test(second) &&
      (parts.length === 2 || (parts.length === 3 && third === "sua"))
    )
      return { kind: "region", code: second, edit: third === "sua" };
  }
  if (
    first &&
    /^XBG\d{4}$/.test(first) &&
    (parts.length === 1 ||
      (parts.length === 2 && (second === "sua" || second === "phan-cong")))
  )
    return {
      kind: "point",
      code: first,
      mode:
        second === "sua"
          ? "edit"
          : second === "phan-cong"
            ? "officers"
            : "view",
    };
  return null;
}
export interface PointFilters {
  at: string;
  tab: "points" | "regions";
  region: string;
}
export function pointFilters(
  q: Pick<URLSearchParams, "get">,
  today: string,
): PointFilters {
  const raw = q.get("at") ?? "";
  const region = q.get("region") ?? "";
  return {
    at: isCalendarDay(raw) ? raw : today,
    tab: q.get("tab") === "regions" ? "regions" : "points",
    region: /^[a-z][a-z0-9_]{0,39}$/.test(region) ? region : "",
  };
}
export function pointHref(path: string, f: PointFilters): string {
  const q = new URLSearchParams({ at: f.at, tab: f.tab });
  if (f.region) q.set("region", f.region);
  return `${POINTS_BASE}${path ? `/${path}` : ""}?${q}`;
}
export function vnPointToday() {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}
