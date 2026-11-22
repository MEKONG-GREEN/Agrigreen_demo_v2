import { isCalendarDay } from "../calendar-routes";

export const ITEMS_BASE = "/kn/bao-gia/mat-hang";
export const isItemCode = (value: string) => /^MH[0-9]{5}$/.test(value);
export const isGroupCode = (value: string) => /^[a-z][a-z0-9_]{0,39}$/.test(value);
export type ItemRoute =
  | { kind: "item-new" }
  | { kind: "item"; code: string; mode: "view" | "edit" | "retire" | "replace" }
  | { kind: "group-new" }
  | { kind: "group"; code: string; edit: boolean };

export function parseItemRoute(parts: readonly string[]): ItemRoute | null {
  const [first, second, third] = parts;
  if (parts.length === 1 && first === "them-moi") return { kind: "item-new" };
  if (first === "nhom-hang") {
    if (parts.length === 2 && second === "them-moi") return { kind: "group-new" };
    if (second && isGroupCode(second) && (parts.length === 2 || (parts.length === 3 && third === "sua")))
      return { kind: "group", code: second, edit: third === "sua" };
  }
  if (first && isItemCode(first) && (parts.length === 1 || (parts.length === 2 && ["sua", "ngung", "thay-the"].includes(second ?? ""))))
    return { kind: "item", code: first, mode: second === "sua" ? "edit" : second === "ngung" ? "retire" : second === "thay-the" ? "replace" : "view" };
  return null;
}

export interface ItemFilters {
  at: string;
  tab: "items" | "groups";
  group: string;
  kind: string;
  state: string;
  q: string;
}
export function itemFilters(query: Pick<URLSearchParams, "get">, today: string): ItemFilters {
  const at = query.get("at") ?? "";
  const group = query.get("group") ?? "";
  const kind = query.get("kind") ?? "";
  const state = query.get("state") ?? "";
  return {
    at: isCalendarDay(at) ? at : today,
    tab: query.get("tab") === "groups" ? "groups" : "items",
    group: isGroupCode(group) ? group : "",
    kind: ["basic", "local"].includes(kind) ? kind : "",
    state: ["active", "pending", "retired"].includes(state) ? state : "",
    q: (query.get("q") ?? "").slice(0, 100),
  };
}
export function itemHref(path: string, filters: ItemFilters): string {
  const query = new URLSearchParams({ at: filters.at, tab: filters.tab });
  for (const key of ["group", "kind", "state", "q"] as const)
    if (filters[key]) query.set(key, filters[key]);
  return `${ITEMS_BASE}${path ? `/${path}` : ""}?${query}`;
}
export function vnItemToday() {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}
