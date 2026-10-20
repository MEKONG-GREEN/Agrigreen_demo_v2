import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { compileMenus, menuSources, readyItems, validatePages } from "./gen-module-menus.mjs";
import { compileMocks, mockDocuments } from "./gen-module-mocks.mjs";
import { parseFragment, assembleOpenAPI } from "./gen-module-openapi.mjs";
const registration = (id, tables, order = 0) => ({ id, tables, order });

test("WH-A-MENU-001: new module files are discovered in both namespaces without an index", () => {
  const root = mkdtempSync(join(tmpdir(), "agrigreen-menu-registry-"));
  try {
    for (const area of ["platform", "kn"]) {
      const directory = join(root, "internal", area, "module", "menus"); mkdirSync(directory, { recursive: true });
      writeFileSync(join(directory, "feature.json"), "{}");
    }
    assert.equal(menuSources(root).length, 2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test("WH-A-MENU-002: merge order, duplicate keys and tested replacements", () => {
  const first = registration("first", { sharedByAudience: { audience: [{ Key: "one", ComingSoon: true }] } });
  const next = registration("next", { sharedByAudience: { audience: [{ Key: "two" }] } }, 1);
  const tables = compileMenus([next, first]);
  assert.deepEqual(tables.sharedByAudience.audience.map((x) => x.Key), ["one", "two"]);
  assert.throws(() => compileMenus([first, registration("bad", first.tables, 1)]), /duplicate menu item/u);
  assert.throws(() => compileMenus([first, first]), /unique id/u);
  assert.throws(() => compileMenus([registration("typo", { unknownTable: [] })]), /unknown menu table/u);
  const patch = { id: "replace", order: 2, replacements: [{ path: ["sharedByAudience", "audience", 0], before: { Key: "one", ComingSoon: true }, after: { Key: "one", ComingSoon: false } }] };
  assert.equal(compileMenus([first, patch]).sharedByAudience.audience[0].ComingSoon, false);
  assert.throws(() => compileMenus([next, patch]), /stale menu replacement/u);
  assert.equal(first.tables.sharedByAudience.audience[0].ComingSoon, true);
});
test("WH-A-MENU-003: a ready menu requires a real page; ComingSoon is excluded", () => {
  const root = mkdtempSync(join(tmpdir(), "agrigreen-menu-pages-"));
  try {
    const directory = join(root, "apps/web-ops/src/app/kn/[dept]"); mkdirSync(directory, { recursive: true }); writeFileSync(join(directory, "page.tsx"), "");
    const ready = readyItems({ deptBusiness: { profile: { audience: [{ Key: "ready", Route: "/kn/{department}" }, { Key: "later", Route: "/missing", ComingSoon: true }] } } });
    assert.deepEqual([...ready.keys()], ["ready"]); validatePages(root, ready);
    ready.set("broken", { Route: "/missing" }); assert.throws(() => validatePages(root, ready), /no web page/u);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test("WH-A-MOCK-001: independent provider discovery, namespaces and stable precedence", () => {
  const root = mkdtempSync(join(tmpdir(), "agrigreen-mock-registry-"));
  try {
    const directory = join(root, "apps/web-ops/src/api/mock/registrations"); mkdirSync(directory, { recursive: true });
    const docs = [{ id: "feature", source: "./kn-feature", app: [{ export: "routes", order: 10 }] }, { id: "core", source: "./core", app: [{ export: "routes", order: 1 }] }];
    docs.forEach((doc, index) => writeFileSync(join(directory, `${index}.json`), JSON.stringify(doc)));
    const result = compileMocks(mockDocuments(root)); assert.equal(result.kn[0].order, 10); assert.equal(result.common[0].order, 1);
    assert.throws(() => compileMocks([...docs, { id: "bad", source: "./bad", app: [{ export: "routes", order: 1 }] }]), /duplicate mock route order/u);
    assert.throws(() => compileMocks([{ id: "bad", source: "../escape" }]), /local source/u);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test("WH-A-OAS-001: fragment assembly preserves YAML bytes and rejects conflicting owners", () => {
  const a = "paths:\n# @ag-order: 1\n  /second:\n    get: {}\ncomponents:\n  schemas:\n# @ag-order: 2\n    Thing: {type: string}\n";
  const b = "paths:\n# @ag-order: 0\n  /first:\n    post: {}\n";
  const blocks = [...parseFragment(a, "a"), ...parseFragment(b, "b")];
  assert.equal(assembleOpenAPI("openapi: 3.1.0\n", blocks), "openapi: 3.1.0\npaths:\n  /first:\n    post: {}\n  /second:\n    get: {}\ncomponents:\n  schemas:\n    Thing: {type: string}\n");
  assert.throws(() => assembleOpenAPI("", [...blocks, ...parseFragment(b, "other")]), /duplicate OpenAPI definition/u);
  assert.throws(() => parseFragment("paths:\n  /unregistered:\n", "bad"), /without order marker/u);
});


test("KHTC-NAV-REG-001: contextual navigation requires registered profiles, audiences and internal paths", () => {
  const base=registration("navigation-base",{MenuProfiles:["generic"],defaultRoutes:{reader:"/overview"}});
  const config=(profiles)=>registration("navigation-profile",{contextDefaultRoutes:profiles},1);
  assert.deepEqual(compileMenus([base,config({generic:{reader:"/periods"}})]).contextDefaultRoutes.generic,{reader:"/periods"});
  assert.throws(()=>compileMenus([base,config({unknown:{reader:"/page"}})]),/profile is not registered/u);
  assert.throws(()=>compileMenus([base,config({generic:{unknown:"/page"}})]),/invalid navigation route/u);
  assert.throws(()=>compileMenus([base,config({generic:{reader:"//external.test"}})]),/invalid navigation route/u);
});
