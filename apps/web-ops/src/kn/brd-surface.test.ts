// @vitest-environment node
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { parseDocument } from "yaml";

type Kind = "menu" | "web" | "api" | "mobile";
type Entry = {
  id: string; kind: Kind; keys: string[]; status: string;
  fn?: string[]; needs?: string[]; package?: string; note?: string;
};
type Manifest = {
  version: number; mode: string;
  catalog: Record<string, { version: string; functions: string[] }>;
  entries: Entry[];
};

function object(value: unknown, fields: string[]): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("expected object");
  const result = value as Record<string, unknown>;
  if (Object.keys(result).some((key) => !fields.includes(key))) throw new Error("unknown field");
  return result;
}

function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string" || !item)) throw new Error("expected string array");
  return value;
}

function parseManifest(raw: string): Manifest {
  const document = parseDocument(raw);
  if (document.errors.length) throw document.errors[0];
  const value = object(document.toJS({ maxAliasCount: 0 }), ["version", "mode", "catalog", "entries"]);
  if (value.version !== 1 || value.mode !== "report") throw new Error("expected version 1 and report mode");
  const refs = new Set<string>();
  const catalog = object(value.catalog, ["tt", "cnts", "hcth", "khtc", "price"]);
  if (!Object.keys(catalog).length) throw new Error("missing catalog");
  for (const [module, source] of Object.entries(catalog)) {
    const c = object(source, ["version", "functions"]);
    if (typeof c.version !== "string" || !c.version || !strings(c.functions).length) throw new Error("invalid catalog");
    for (const fn of strings(c.functions)) {
      const ref = `${module}/${fn}`;
      if (!/^FN-(?:[0-9]{2}|(?:CH|TW|TI|XA|PN|AD)-[0-9]{2}\.[0-9]{2})$/.test(fn) || refs.has(ref)) throw new Error("invalid function");
      refs.add(ref);
    }
  }
  if (!Array.isArray(value.entries) || !value.entries.length) throw new Error("missing entries");
  const ids = new Set<string>();
  const keys = new Set<string>();
  for (const item of value.entries) {
    const e = object(item, ["id", "kind", "keys", "status", "fn", "needs", "package", "note"]);
    if (typeof e.id !== "string" || !e.id || ids.has(e.id) || !["menu", "web", "api", "mobile"].includes(e.kind as string) || !strings(e.keys).length) throw new Error("invalid entry");
    ids.add(e.id);
    const fn = e.fn === undefined ? [] : strings(e.fn);
    const needs = e.needs === undefined ? [] : strings(e.needs);
    if ([...fn, ...needs].some((ref) => !refs.has(ref))) throw new Error("unknown function");
    if ((e.package !== undefined && typeof e.package !== "string") || (e.note !== undefined && typeof e.note !== "string")) throw new Error("expected string");
    switch (e.status) {
      case "FN": if (!fn.length || needs.length || e.package) throw new Error("FN requires fn only"); break;
      case "HẠ_TẦNG": if (!needs.length || fn.length || e.package) throw new Error("infrastructure requires needs only"); break;
      case "GỠ": if (!e.package || fn.length || needs.length) throw new Error("removal requires package without fn"); break;
      case "HOÃN": if (!e.package || !fn.length || needs.length) throw new Error("deferred requires package and fn"); break;
      default: throw new Error("unknown status");
    }
    for (const key of strings(e.keys)) {
      const identity = `${e.kind}:${key}`;
      if (keys.has(identity) || (key.includes("*") && (e.kind !== "api" || !key.endsWith("/**") || key.slice(0, -3).includes("*")))) throw new Error("invalid or duplicate key");
      keys.add(identity);
    }
  }
  return value as Manifest;
}

function matches(kind: Kind, selector: string, key: string): boolean {
  if (kind === "api" && selector.endsWith("/**")) {
    const base = selector.slice(0, -3);
    return key === base || key.startsWith(`${base}/`);
  }
  return key === selector;
}

function compare(manifest: Manifest, kind: Kind, actual: string[]) {
  const missing: string[] = [], ambiguous: string[] = [];
  const used = new Set<string>();
  const entries = manifest.entries.filter((entry) => entry.kind === kind);
  for (const key of new Set(actual)) {
    const found: string[] = [];
    for (const entry of entries) for (const selector of entry.keys) {
      if (matches(kind, selector, key)) { used.add(selector); found.push(entry.id); }
    }
    if (!found.length) missing.push(key);
    else if (found.length > 1) ambiguous.push(`${key} -> ${found.join(", ")}`);
  }
  return {
    missing: missing.sort(), ambiguous: ambiguous.sort(),
    unused: entries.flatMap((entry) => entry.keys).filter((key) => !used.has(key)).sort(),
  };
}

function files(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((item) => {
    const path = join(directory, item.name);
    return item.isDirectory() ? files(path) : [path];
  });
}

type MenuItem = { key: string; route?: string; children?: MenuItem[] };
type MenuFixture = { cases: { department: string; menu: { groups: { key: string; items: MenuItem[] }[] } }[] };

function menuKeys(fixture: MenuFixture): string[] {
  const keys = new Set<string>();
  for (const item of fixture.cases) {
    const scope = item.department || "admin";
    function walk(items: MenuItem[]) {
      for (const node of items) {
        let owner = scope;
        if (scope === "admin" && node.route?.startsWith("/kn/phong-ban/")) owner += `/${node.route.split("/")[3]}`;
        keys.add(`${owner}:${node.key}`);
        walk(node.children || []);
      }
    }
    for (const group of item.menu.groups) { keys.add(`${scope}:${group.key}`); walk(group.items); }
  }
  return [...keys].sort();
}

const validPolicy = `version: 1
mode: report
catalog:
  tt:
    version: "7.7"
    functions: [FN-XA-03.05]
entries:
  - id: plots
    kind: api
    keys: [/v1/kn/plots/**]
    status: FN
    fn: [tt/FN-XA-03.05]
`;

describe("KN surface YAML schema and report mode", () => {
  it("rejects malformed policies and obsolete/unqualified references", () => {
    expect(parseManifest(validPolicy).entries).toHaveLength(1);
    for (const raw of [
      "entries: [", `${validPolicy}surprise: true\n`, `${validPolicy}version: 1\n`, `${validPolicy}---\nversion: 1\n`,
      validPolicy.replace("mode: report", "mode: enforce"),
      validPolicy.replace("tt/FN-XA-03.05", "hcth/FN-21"),
      validPolicy.replace("fn: [tt/FN-XA-03.05]", "fn: []"),
      validPolicy.replace("status: FN", "status: HẠ_TẦNG"),
      validPolicy.replace("status: FN\n    fn: [tt/FN-XA-03.05]", "status: GỠ"),
      validPolicy.replace("status: FN", "status: HOÃN"),
      validPolicy.replace("[/v1/kn/plots/**]", "[/v1/kn/plots/**, /v1/kn/plots/**]"),
    ]) expect(() => parseManifest(raw)).toThrow();
  });

  it("reports new, stale and overlapping roots without rejecting surface differences", () => {
    const m = parseManifest(validPolicy);
    expect(compare(m, "api", ["/v1/kn/plots/a", "/v1/kn/plots-extra"])).toEqual({ missing: ["/v1/kn/plots-extra"], unused: [], ambiguous: [] });
    expect(compare(m, "api", ["/v1/new-resource"]).unused).toEqual(["/v1/kn/plots/**"]);
    m.entries.push({ id: "specific", kind: "api", keys: ["/v1/kn/plots/a"], status: "FN", fn: ["tt/FN-XA-03.05"] });
    expect(compare(m, "api", ["/v1/kn/plots/a"]).ambiguous).toEqual(["/v1/kn/plots/a -> plots, specific"]);
  });

  it("keeps department identities distinct for copied menu keys", () => {
    const fixture: MenuFixture = { cases: ["trong_trot", "hanh_chinh_tong_hop"].map((department) => ({ department, menu: { groups: [{ key: "dieu_hanh", items: [{ key: "tw.dashboard", children: [] }] }] } })) };
    expect(menuKeys(fixture)).toEqual(["hanh_chinh_tong_hop:dieu_hanh", "hanh_chinh_tong_hop:tw.dashboard", "trong_trot:dieu_hanh", "trong_trot:tw.dashboard"]);
  });

  it("reports the actual route tree, generated menu fixture and mobile screens", () => {
    const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
    const manifest = parseManifest(readFileSync(join(root, "docs/kn/brd-surface.yaml"), "utf8"));
    const app = join(root, "apps/web-ops/src/app");
    const web = files(app).filter((path) => path.endsWith("/page.tsx") || path.endsWith("\\page.tsx") || (path.endsWith("route.ts") && relative(app, path).replaceAll("\\", "/").startsWith("kn/"))).map((path) => {
      const route = relative(app, dirname(path)).replaceAll("\\", "/");
      return `/${route}`;
    });
    const dart = ["apps/field-app/lib", "packages/agrigreen_mobile/lib"].flatMap((path) => files(join(root, path))).filter((path) => path.endsWith(".dart"));
    const mobile = dart.flatMap((path) => [...readFileSync(path, "utf8").matchAll(/\bclass ([A-Z][A-Za-z0-9]*Screen) extends\b/g)].map((match) => match[1]!));
    const fixture = JSON.parse(readFileSync(join(root, "apps/web-ops/src/api/mock/fixtures/kn-menu.json"), "utf8")) as MenuFixture;
    const inventories = { menu: menuKeys(fixture), web, mobile };
    for (const kind of ["menu", "web", "mobile"] as const) {
      const difference = compare(manifest, kind, inventories[kind]);
      // Deliberately no equality assertion: #272 enables blocking after removals.
      console.info(`KN surface REPORT ONLY ${kind}: actual=${new Set(inventories[kind]).size}`, difference);
    }
  });
});
