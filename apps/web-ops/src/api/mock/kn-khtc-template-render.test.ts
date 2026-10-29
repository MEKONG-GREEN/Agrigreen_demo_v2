import { describe, expect, it } from "vitest";
import type { components } from "../generated/application";
import { canonical, invalidDefinition, renderTemplate, seedTemplate } from "./kn-khtc-template-render";
type S = components["schemas"];
const custom = () => {
    const d = seedTemplate("mau_5")!.definition;
    d.nodes.push({ id: "extra", parent: "II", order: 100, title: "Mục bổ sung", repeat: "none", body: "{so}", bindings: [{ variable: "so", source: "auto", field: "value", ref: "module-row" }], text_slot: false, locked_admin: [] });
    return d;
};
const context = (): S["KHTCTemplateContext"] => ({ period_id: "test", period_name: "Kỳ", plan_version_id: "plan", period_type: "program", period_start: "2026-10-01T00:00:00Z", input_hash: "captured", values: {}, texts: {}, lists: { metric: [{ ref: "module-row", values: { data_source: "module", "module.value": "7", "form.value": "" } }] }, provisional: [] });
describe("mock KHTC template compiler", () => {
    it("accepts all native seeds without changing caller data", () => {
        for (const key of ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"]) {
            const d = seedTemplate(key)!.definition, old = structuredClone(d);
            expect(invalidDefinition(d)).toBeNull();
            canonical(d);
            expect(d).toEqual(old);
        }
    });
    it("rejects malformed braces, forged binding fields and invalid raw inputs", () => {
        for (const bad of ["Thiếu {", "Thừa }", "{unknown}", "{{{", "text\u0000"]) {
            const d = custom();
            d.nodes.at(-1)!.body = bad;
            expect(invalidDefinition(d), bad).not.toBeNull();
        }
        for (const binding of [null, { variable: "so", source: "__proto__", field: "value" }, { variable: "so", source: "auto", field: "value", locked: false }, { variable: "so", source: "auto", field: "value", ref: "x".repeat(201) }]) {
            const d = custom();
            d.nodes.at(-1)!.bindings = [binding as never];
            expect(() => invalidDefinition(d)).not.toThrow();
            expect(invalidDefinition(d)).not.toBeNull();
        }
        for (const order of ["1e100000", "1e-100000", "9".repeat(100000), -1, 10001]) {
            const d = custom();
            d.nodes.at(-1)!.order = order as number;
            expect(invalidDefinition(d)).not.toBeNull();
        }
    });
    it("resolves auto from the explicitly referenced captured row and locks its token", () => {
        const d = custom(), c = context();
        for (const n of d.nodes)
            for (const b of n.bindings)
                c.values[`${b.source}.${b.field}`] = "";
        const before = structuredClone(d), out = renderTemplate(d, c);
        expect(out.nodes.find(n => n.id === "extra")!.rows[0]!.tokens).toEqual([{ text: "7", variable: "so", source: "module", field: "value", locked: true }]);
        expect(d).toEqual(before);
    });
    it("fails closed for a missing source even when the binding is unused or status overrides the body", () => {
        const d = custom();
        d.nodes.at(-1)!.body = "Câu tĩnh";
        d.nodes.at(-1)!.bindings[0]!.ref = "missing";
        expect(() => renderTemplate(d, context())).toThrow();
    });
});
