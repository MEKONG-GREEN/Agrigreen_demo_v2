import { DOCUMENTS } from "./data";
import { json, norm, paginate, scopeUnits, type Route } from "./http";

export const appRoutes41: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/documents$/,
    handler: ({ url, persona }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const kind = url.searchParams.get("kind");
      const category = url.searchParams.get("category");
      const tag = url.searchParams.get("tag");
      const s = scopeUnits(persona);
      const list = DOCUMENTS.filter(
        (d) =>
          (!s || d.audience.level === "national" || d.audience.units.some((u) => s.has(u))) &&
          (!q || norm(`${d.title ?? ""} ${d.summary ?? ""}`).includes(q)) &&
          (!kind || d.kind === kind) &&
          (!category || d.category === category) &&
          (!tag || d.tags.includes(tag)),
      );
      return json(paginate(list, url));
    },
  }];
