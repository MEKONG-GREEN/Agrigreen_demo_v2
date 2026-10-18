import { describe, expect, it } from "vitest";
import { orderedRoutes } from "./registry";
import { json, type Route } from "./http";

describe("WH-A-MOCK-002: first-match route precedence", () => {
  const route = (value: string): Route => ({ method: "GET", pattern: /^\/same$/, handler: () => json({ value }) });
  it("keeps explicit precedence across both registries and preserves each batch", () => {
    const first = route("first"), second = route("second"), fallback = route("fallback");
    const routes = orderedRoutes([{ surface: "app", order: 9, routes: [fallback] }, { surface: "admin", order: 0, routes: [] }, { surface: "app", order: 1, routes: [first, second] }], "app");
    expect(routes).toEqual([first, second, fallback]);
  });
  it("rejects ambiguous priorities instead of depending on import order", () => {
    expect(() => orderedRoutes([{ surface: "app", order: 1, routes: [] }, { surface: "app", order: 1, routes: [] }], "app")).toThrow("Duplicate mock route order");
  });
});
