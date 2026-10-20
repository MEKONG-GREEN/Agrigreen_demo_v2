// Module-owned menu sources -> the existing compatibility catalog. No runtime IO.
import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

export const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export function filesBelow(directory, accept) {
  return readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name, "en")).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(path, accept) : accept(path) ? [path] : [];
  });
}
export function menuSources(root) {
  return ["platform", "kn"].flatMap((area) => {
    const path = join(root, "internal", area);
    let modules;
    try { modules = readdirSync(path, { withFileTypes: true }); } catch (error) { if (error.code === "ENOENT") return []; throw error; }
    return modules.filter((x) => x.isDirectory()).flatMap((x) => {
      const directory = join(path, x.name, "menus");
      try { return filesBelow(directory, (f) => f.endsWith(".json")); } catch (error) { if (error.code === "ENOENT") return []; throw error; }
    });
  }).sort();
}
function merge(target, source, location) {
  if (target === undefined) return structuredClone(source);
  if (Array.isArray(target) && Array.isArray(source)) {
    const identifiers = new Set(target.map((x) => typeof x === "string" ? x : x?.Key).filter(Boolean));
    for (const item of source) {
      const id = typeof item === "string" ? item : item?.Key;
      if (id && identifiers.has(id)) throw Error(`duplicate menu item ${location}/${id}; use a tested replacement`);
      if (id) identifiers.add(id);
    }
    return [...target, ...structuredClone(source)];
  }
  if (target && source && typeof target === "object" && typeof source === "object" && !Array.isArray(target) && !Array.isArray(source)) {
    for (const [key, value] of Object.entries(source)) target[key] = merge(target[key], value, `${location}/${key}`);
    return target;
  }
  throw Error(`duplicate menu value ${location}`);
}
export function compileMenus(documents) {
  const tables = {}, ids = new Set();
  const names = new Set(["trongTrotMenus", "genericMenus", "adminDeptItems", "AdminOverview", "AdminSystem", "defaultRoutes", "contextDefaultRoutes", "MenuProfiles", "khtcReportProfiles", "KHTCReportItem", "KHTCReportAdminItem", "SharedGroup", "sharedByAudience", "deptBusiness", "adminDeptBusiness", "deptProfileByCode", "menuModules", "menuDeptModules"]);
  for (const doc of [...documents].sort((a, b) => a.order - b.order || a.id.localeCompare(b.id, "en"))) {
    if (!doc.id || ids.has(doc.id) || !Number.isInteger(doc.order)) throw Error("menu registration needs a unique id and integer order");
    ids.add(doc.id);
    for (const [key, value] of Object.entries(doc.tables ?? {})) {
      if (!names.has(key)) throw Error(`unknown menu table ${key}`);
      tables[key] = merge(tables[key], value, key);
    }
    for (const patch of doc.replacements ?? []) {
      let parent = tables;
      for (const key of patch.path.slice(0, -1)) {
        parent = parent?.[key];
        if (!parent) throw Error(`missing replacement path: ${patch.path}`);
      }
      const key = patch.path.at(-1);
      if (key === undefined || !isDeepStrictEqual(parent?.[key], patch.before)) throw Error(`stale menu replacement: ${patch.path}`);
      parent[key] = structuredClone(patch.after);
    }
  }
  for (const profile of tables.MenuProfiles ?? []) {
    if (!["trong_trot", "generic"].includes(profile) && !tables.deptBusiness?.[profile]) throw Error(`profile has no registered business menu: ${profile}`);
  }
  for (const [profile, roles] of Object.entries(tables.contextDefaultRoutes ?? {})) {
    if (!tables.MenuProfiles?.includes(profile)) throw Error(`navigation profile is not registered: ${profile}`);
    for (const [audience, route] of Object.entries(roles)) {
      if (!tables.defaultRoutes?.[audience] || typeof route !== "string" || !/^\/(?!\/)[^\\?#\r\n]*$/u.test(route)) throw Error(`invalid navigation route: ${profile}/${audience}`);
    }
  }
  return tables;
}
export function readyItems(tables) {
  const entries = new Map();
  function visit(value) {
    if (!value || typeof value !== "object") return;
    if (typeof value.Key === "string" && !value.ComingSoon) {
      const previous = entries.get(value.Key);
      if (previous && previous.Route !== value.Route) throw Error(`ambiguous ready menu key ${value.Key}`);
      entries.set(value.Key, value);
    }
    for (const child of Object.values(value)) visit(child);
  }
  for (const key of ["deptBusiness", "adminDeptBusiness", "KHTCReportItem", "KHTCReportAdminItem"]) visit(tables[key]);
  return entries;
}
export function validatePages(root, items) {
  const app = join(root, "apps/web-ops/src/app");
  const pages = filesBelow(app, (file) => file.endsWith("/page.tsx") || file.endsWith("\\page.tsx")).map((file) => {
    const path = relative(app, dirname(file)).replaceAll("\\", "/").split("/").filter((x) => !x.startsWith("(")).join("/");
    return new RegExp("^/" + path.split("/").map((segment) => segment.startsWith("[") ? "[^/]+" : segment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("/") + "$", "u");
  });
  for (const [key, item] of items) {
    if (item.Route && !pages.some((page) => page.test(item.Route.replace("{department}", "department")))) throw Error(`ready menu has no web page: ${key} -> ${item.Route}`);
  }
}
function replaceRegion(source, name, content) {
  const start = `// BEGIN GENERATED ${name}`, end = `// END GENERATED ${name}`;
  if (!source.includes(start) || !source.includes(end)) throw Error(`missing generated region ${name}`);
  return source.slice(0, source.indexOf(start)) + `${start}\n${content}\n\n${end}` + source.slice(source.indexOf(end) + end.length);
}
function webMenu(tables) {
  function item(value) {
    return { key: value.Key, label: value.Label, ...(value.Route ? { route: value.Route } : {}), icon: value.Icon,
      ...(value.BadgeSource ? { badge: value.BadgeSource } : {}), ...(value.Requires ? { adHoc: true } : {}),
      ...(value.ComingSoon ? { soon: true } : {}), ...(value.Children ? { children: value.Children.map(item) } : {}) };
  }
  const group = (value) => ({ key: value.Key, label: value.Label, items: value.Items.map(item) });
  const map = (value, fn) => Object.fromEntries(Object.entries(value).map(([key, child]) => [key, fn(child)]));
  const list = (value) => value.map(item);
  const definitions = [
    ["DEFAULT_ROUTE", "Record<KnRole, string>", tables.defaultRoutes],
    ["CONTEXT_DEFAULT_ROUTES", "Partial<Record<KnMenuProfile, Partial<Record<KnRole, string>>>>", tables.contextDefaultRoutes ?? {}],
    ["TRONG_TROT", 'Record<Exclude<KnRole, "admin">, Group[]>', map(tables.trongTrotMenus, (value) => value.map(group))],
    ["GENERIC", 'Record<Exclude<KnRole, "admin">, Group[]>', map(tables.genericMenus, (value) => value.map(group))],
    ["SHARED", "Group", group(tables.SharedGroup)],
    ["SHARED_BY_AUDIENCE", "Partial<Record<KnRole, Item[]>>", map(tables.sharedByAudience, list)],
    ["BUSINESS", 'Partial<Record<KnMenuProfile, Partial<Record<Exclude<KnRole, "admin">, Item[]>>>>', map(tables.deptBusiness, (value) => map(value, list))],
    ["ADMIN_BUSINESS", "Partial<Record<KnMenuProfile, Item[]>>", map(tables.adminDeptBusiness, list)],
    ["ADMIN_DEPT_BASE", "Record<string, Item[]>", map(tables.adminDeptItems, list)],
    ["KHTC_REPORT", "Item", item(tables.KHTCReportItem)],
    ["KHTC_REPORT_ADMIN", "Item", item(tables.KHTCReportAdminItem)],
    ["ADMIN_OVERVIEW", "Group", group(tables.AdminOverview)],
    ["ADMIN_SYSTEM", "Group", group(tables.AdminSystem)],
  ];
  return definitions.map(([name, type, value]) => `${name === "DEFAULT_ROUTE" || name === "CONTEXT_DEFAULT_ROUTES" ? "export " : ""}const ${name}: ${type} = ${JSON.stringify(value, null, 2)};`).join("\n\n") +
    `\n\nconst KHTC_REPORT_PROFILES = new Set<KnMenuProfile>(${JSON.stringify(Object.keys(tables.khtcReportProfiles).filter((key) => tables.khtcReportProfiles[key]))});`;
}
export function generateMenus(root, check = false) {
  const tables = compileMenus(menuSources(root).map((file) => JSON.parse(readFileSync(file, "utf8"))));
  const ready = readyItems(tables);
  validatePages(root, ready);
  const outputs = [
    ["apps/web-ops/src/api/mock/kn.ts", "WEB MENU", webMenu(tables)],
    ["internal/platform/party/domain/menu_departments.go", "MENU CATALOG", `const registeredMenuJSON = ${JSON.stringify(JSON.stringify(tables))}`],
    ["internal/platform/party/domain/kn_test.go", "IMPLEMENTED MENU", "var implementedMenu = map[string]bool{" + [...ready.keys()].sort().map((key) => `${JSON.stringify(key)}: true`).join(", ") + "}"],
  ];
  for (const [path, region, body] of outputs) {
    const file = join(root, path), old = readFileSync(file, "utf8").replaceAll("\r\n", "\n");
    const next = replaceRegion(old, region, body);
    if (check && next !== old) throw Error(`menu output is stale: ${path}`);
    if (!check && next !== old) writeFileSync(file, next);
  }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  generateMenus(repository, process.argv.includes("--check"));
}
