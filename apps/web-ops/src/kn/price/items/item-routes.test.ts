import { describe, expect, it } from "vitest";
import { itemFilters, itemHref, parseItemRoute } from "./item-routes";
import { initialItemFields, itemBody, validateItemFields } from "./item-form";
import type { PriceCatalog, PriceItem } from "./api";

describe("Price198 strict routes and field validation", () => {
  const catalog: PriceCatalog = { day: "2026-11-03", units: [{ code: "kg", name: "kg", sort_order: 1, version: 1, valid_from: "2026-11-03" }], groups: [{ code: "lua_gao", name: "Lúa gạo", sort_order: 1, version: 1, valid_from: "2026-11-03" }], regions: [] };
  it("accepts only complete item/group routes; malformed code/extra segments cannot open write forms", () => {
    expect(parseItemRoute(["them-moi"])).toEqual({ kind: "item-new" });
    expect(parseItemRoute(["MH00001", "sua"])).toEqual({ kind: "item", code: "MH00001", mode: "edit" });
    expect(parseItemRoute(["MH00001", "ngung"])).toEqual({ kind: "item", code: "MH00001", mode: "retire" });
    expect(parseItemRoute(["MH00001", "thay-the"])).toEqual({ kind: "item", code: "MH00001", mode: "replace" });
    expect(parseItemRoute(["nhom-hang", "them-moi"])).toEqual({ kind: "group-new" });
    expect(parseItemRoute(["nhom-hang", "lua_gao", "sua"])).toEqual({ kind: "group", code: "lua_gao", edit: true });
    for (const parts of [[], ["them-moi", "sua"], ["MH00001", "sua", "more"], ["MH0001"], ["MH000001"], ["%2F"], ["../MH00001"], ["nhom-hang", "lua/gao"], ["nhom-hang", "lua_gao", "xoa"], ["1e100000"], ["1e-100000"], ["9".repeat(100_000)]])
      expect(parseItemRoute(parts)).toBeNull();
  });
  it("keeps date/list filters in back and edit links; query values cannot introduce a path", () => {
    const query = new URLSearchParams({ at: "2026-02-28", tab: "groups", group: "lua_gao", kind: "local", state: "pending", q: "Lúa & gạo/../x" });
    const filters = itemFilters(query, catalog.day);
    const href = itemHref("MH00001/sua", filters);
    const link = new URL(href, "https://local.test");
    expect(link.pathname).toBe("/kn/bao-gia/mat-hang/MH00001/sua");
    expect(itemFilters(link.searchParams, catalog.day)).toEqual(filters);
    const bad = itemFilters(new URLSearchParams({ at: "2026-02-30", tab: "outside", group: "../bad", kind: "other", state: "all" }), catalog.day);
    expect(bad).toEqual({ at: catalog.day, tab: "items", group: "", kind: "", state: "", q: "" });
  });
  it("requires identity fields, real catalog choices and calendar dates before posting", () => {
    const fields = initialItemFields(catalog);
    expect(validateItemFields(fields, catalog, true).errors).toMatchObject({ name: "Bắt buộc nhập.", variety: "Bắt buộc nhập." });
    const good = { ...fields, name: "Lúa", variety: "OM18", valid_from: "2026-02-28" };
    expect(validateItemFields(good, catalog, true).valid).toBe(true);
    for (const [key, value] of [["group_code", "fake"], ["unit_code", "fake"], ["kind", "other"], ["valid_from", "2026-02-30"], ["valid_to", "2026-02-27"], ["replaces_code", "MHbogus"], ["name", "9".repeat(100_000)], ["variety", "9".repeat(100_000)]] as const)
      expect(validateItemFields({ ...good, [key]: value }, catalog, true).valid).toBe(false);
    const local = { ...good, kind: "local", points: "91301, 91302;91301" };
    expect(itemBody(local, true).points).toEqual(["91301", "91302"]);
    expect(validateItemFields({ ...local, points: "bad/code" }, catalog, true).errors.points).toBeTruthy();
    expect(itemBody({ ...local, kind: "basic" }, true).points).toBeUndefined();
  });
  it("priced identity cannot be changed; name/place remain editable and revision never sends replacement/assignments", () => {
    const item = { code: "MH00001", name: "Lúa", variety: "OM18", group_code: "lua_gao", unit_code: "kg", spec: "Tươi", sale_place: "Chợ", kind: "basic" } as PriceItem;
    const fields = initialItemFields(catalog, item);
    expect(validateItemFields({ ...fields, name: "Lúa mới", sale_place: "Chợ mới" }, catalog, false, item).valid).toBe(true);
    for (const key of ["variety", "group_code", "unit_code", "spec", "kind"] as const)
      expect(validateItemFields({ ...fields, [key]: "changed" }, catalog, false, item).errors[key]).toBeTruthy();
    const body = itemBody({ ...fields, valid_to: "2026-12-31", replaces_code: "MH00002", points: "91301" }, false);
    expect(body.valid_to).toBeUndefined();
    expect(body.replaces_code).toBeUndefined();
    expect(body.points).toBeUndefined();
    expect(initialItemFields(catalog, item, true).replaces_code).toBe(item.code);
  });
});
