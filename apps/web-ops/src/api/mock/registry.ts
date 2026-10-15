import type { Route } from "./http";

export interface RouteRegistration {
  surface: "app" | "admin";
  order: number;
  routes: Route[];
}

/** Stable first-match precedence, independent of module discovery/import order. */
export function orderedRoutes(registrations: RouteRegistration[], surface: RouteRegistration["surface"]): Route[] {
  const selected = registrations.filter((item) => item.surface === surface).sort((a, b) => a.order - b.order);
  const orders = new Set<number>();
  for (const item of selected) {
    if (orders.has(item.order)) throw new Error(`Duplicate mock route order: ${surface}/${item.order}`);
    orders.add(item.order);
  }
  return selected.flatMap((item) => item.routes);
}
