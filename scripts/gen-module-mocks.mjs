import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { filesBelow, repository } from "./gen-module-menus.mjs";
export function mockDocuments(root) {
  const base = join(root, "apps/web-ops/src/api/mock");
  return filesBelow(join(base, "registrations"), (file) => file.endsWith(".json")).map((file) => JSON.parse(readFileSync(file, "utf8")));
}
export function compileMocks(documents) {
  const ids = new Set(), orders = new Set();
  const output = { common: [], kn: [] };
  for (const doc of [...documents].sort((a, b) => a.id.localeCompare(b.id, "en"))) {
    if (!doc.id || ids.has(doc.id) || !/^\.\/[a-z0-9/-]+$/u.test(doc.source) || doc.source.includes("..")) throw Error("mock registration needs a unique id and local source");
    ids.add(doc.id);
    for (const surface of ["app", "admin"]) for (const item of doc[surface] ?? []) {
      if (!Number.isInteger(item.order) || !/^[A-Za-z_$][\w$]*$/u.test(item.export)) throw Error(`invalid mock route registration ${doc.id}`);
      const order = `${surface}/${item.order}`;
      if (orders.has(order)) throw Error(`duplicate mock route order ${order}`);
      orders.add(order);
      output[doc.source.startsWith("./kn") ? "kn" : "common"].push({ ...item, surface, source: doc.source });
    }
  }
  return output;
}
function render(entries, name, ownSource) {
  const imports = entries.flatMap((item, index) => item.source === ownSource ? [] : [`import { ${item.export} as route${index} } from ${JSON.stringify(item.source)};`]).join("\n");
  const values = entries.map((item, index) => `  { surface: ${JSON.stringify(item.surface)}, order: ${item.order}, routes: ${item.source === ownSource ? item.export : `route${index}`} },`).join("\n");
  return ownSource ? `${imports}\n\nexport function ${name}(): RouteRegistration[] {\n  return [\n${values}\n  ];\n}` : `${imports}\n\nexport const ${name}: RouteRegistration[] = [\n${values}\n];`;
}
export function generateMocks(root, check = false) {
  const base = join(root, "apps/web-ops/src/api/mock"), entries = compileMocks(mockDocuments(root));
  for (const item of [...entries.common, ...entries.kn]) {
    const file = [".ts", "/index.ts"].map((suffix) => join(base, item.source + suffix)).find(existsSync);
    if (!file) throw Error(`mock provider missing: ${item.source}`);
    const source = readFileSync(file, "utf8");
    if (!new RegExp(`export (?:const|function) ${item.export}\\b`, "u").test(source)) throw Error(`mock export missing: ${item.source}/${item.export}`);
  }
  const outputs = [
    ["handler.ts", 'import { orderedRoutes, type RouteRegistration } from "./registry";\nimport { registeredKnRoutes } from "./kn-admin";\n' + render(entries.common, "commonRouteRegistrations") + '\n\nconst registrations = [...commonRouteRegistrations, ...registeredKnRoutes()];\nconst appRoutes = orderedRoutes(registrations, "app");\nconst adminRoutes = orderedRoutes(registrations, "admin");'],
    ["kn-admin.ts", 'import type { RouteRegistration } from "./registry";\n' + render(entries.kn, "registeredKnRoutes", "./kn-admin")],
  ];
  for (const [path, content] of outputs) {
    const file = join(base, path), old = readFileSync(file, "utf8").replaceAll("\r\n", "\n");
    const start = "// BEGIN GENERATED MOCK ROUTES", end = "// END GENERATED MOCK ROUTES";
    if (!old.includes(start) || !old.includes(end)) throw Error(`missing generated mock region: ${path}`);
    const next = old.slice(0, old.indexOf(start)) + `${start}\n${content}\n${end}` + old.slice(old.indexOf(end) + end.length);
    if (check && next !== old) throw Error(`mock output is stale: ${path}`);
    if (!check && next !== old) writeFileSync(file, next);
  }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) generateMocks(repository, process.argv.includes("--check"));
