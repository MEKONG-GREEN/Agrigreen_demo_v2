import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { components } from "../generated/application";
import { mockFetchNoDelay } from "./handler";
import { knStoredContext } from "./kn";
import { resetKnAdmin } from "./kn-admin";
import { findPersona, TENANT } from "./personas";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcTemplateRoutes, resetKhtcTemplates, useTemplateMockNarrativeUsage } from "./kn-khtc-templates";
import { seedTemplate } from "./kn-khtc-template-render";
import type { Ctx } from "./http";
type S = components["schemas"];
let admin = "", sequence = 0;
const request = (path: string, method = "GET", body?: unknown, ctx = admin, persona = "kn-admin", idem = `template-${++sequence}`) => new Request(`http://mock/v1/extension/khtc/templates${path}`, { method, headers: { Authorization: `Bearer mock.${persona}`, "X-AG-Context": ctx, "Content-Type": "application/json", "Idempotency-Key": idem }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
const api = (path: string, method = "GET", body?: unknown, ctx = admin, persona = "kn-admin", idem?: string) => mockFetchNoDelay(request(path, method, body, ctx, persona, idem));
async function detail(key = "mau_1"): Promise<S["KHTCTemplateDetail"]> { const res = await api(`/${key}`); expect(res.status).toBe(200); return res.json(); }
function changed(): S["KHTCTemplateDefinition"] { const d = seedTemplate("mau_1")!.definition; d.nodes.push({ id: "admin.extra", parent: "A", order: 100, title: "Mục bổ sung", repeat: "none", body: "Văn bản bổ sung", bindings: [], text_slot: false, locked_admin: [] }); return d; }
beforeEach(async () => {
    resetKnAdmin();
    resetKhtcTemplates();
    useTemplateMockNarrativeUsage(null);
    const res = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `context-${++sequence}` }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
    expect(res.status).toBe(201);
    admin = ((await res.json()) as {
        context_id: string;
    }).context_id;
});
afterEach(() => { useTemplateMockNarrativeUsage(null); });
describe("mock template authoring routes", () => {
    it("lists exactly five fixed templates and enforces context/role/read-only before reading", async () => {
        const list = await api("");
        expect(list.status).toBe(200);
        expect(((await list.json()) as {
            items: S["KHTCTemplateDetail"][];
        }).items.map(x => x.key)).toEqual(["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"]);
        expect((await api("/adhoc")).status).toBe(404);
        expect((await api("/mau_1", "GET", undefined, "")).status).toBe(403);
        expect((await api("/mau_1", "GET", undefined, admin, "kn-an")).status).toBe(403);
        const session = knStoredContext(admin)!.ctx;
        session.read_only = true;
        try {
            expect((await detail()).editable).toBe(false);
            expect((await api("/mau_1/versions", "POST", { base_version: 0, definition: changed() })).status).toBe(403);
        }
        finally {
            session.read_only = false;
        }
    });
    it("keeps immutable versions, replays lost responses, rejects stale/concurrent saves and restores as a new version", async () => {
        const body = { base_version: 0, definition: changed() }, idem = `save-${++sequence}`;
        const first = await api("/mau_1/versions", "POST", body, admin, "kn-admin", idem);
        expect(first.status).toBe(201);
        const one = (await first.json()) as S["KHTCTemplateVersion"];
        const replay = await api("/mau_1/versions", "POST", body, admin, "kn-admin", idem);
        expect(replay.status).toBe(201);
        expect(await replay.json()).toEqual(one);
        expect((await api("/mau_1/versions", "POST", { ...body, base_version: 1 }, admin, "kn-admin", idem)).status).toBeGreaterThanOrEqual(400);
        expect((await detail()).version).toBe(1);
        const next = structuredClone(body.definition);
        next.nodes.at(-1)!.body = "Bản hai";
        const pair = await Promise.all([api("/mau_1/versions", "POST", { base_version: 1, definition: next }), api("/mau_1/versions", "POST", { base_version: 1, definition: next })]);
        expect(pair.map(x => x.status).sort()).toEqual([201, 409]);
        const restored = await api("/mau_1/restore", "POST", { base_version: 2, version_id: one.id });
        expect(restored.status).toBe(201);
        const three = (await restored.json()) as S["KHTCTemplateVersion"];
        expect(three).toMatchObject({ version: 3, restored_from: one.id, definition: one.definition, hash: one.hash });
        const pub = await api("/mau_1/publish", "POST", { base_version: 3, version_id: three.id, applies_from: "2027-01-01" });
        expect(pub.status).toBe(201);
        expect((await api("/mau_1/publish", "POST", { base_version: 3, version_id: one.id, applies_from: "2027-01-01" })).status).toBe(409);
        expect((await detail()).history).toHaveLength(3);
        expect((await detail("mau_2")).publications).toEqual([]);
    });
    it("denies forged metadata, hostile order inputs and unverifiable text removal without writing", async () => {
        for (const value of ["1e100000", "1e-100000", "9".repeat(100000)]) {
            const definition = changed();
            definition.nodes.at(-1)!.order = value as never;
            expect((await api("/mau_1/versions", "POST", { base_version: 0, definition })).status).toBe(422);
        }
        const definition = changed();
        (definition.nodes.at(-1)! as unknown as Record<string, unknown>).protected = false;
        expect((await api("/mau_1/versions", "POST", { base_version: 0, definition })).status).toBe(422);
        const removed = changed(), slot = removed.nodes.find(n => n.text_slot)!;
        slot.text_slot = false;
        slot.bindings = [];
        slot.body = "Văn bản cố định";
        expect((await api("/mau_1/versions", "POST", { base_version: 0, definition: removed })).status).toBe(503);
        expect((await detail()).version).toBe(0);
    });
    it("uses the composer's captured projection for preview and guards cross-tenant access", async () => {
        const period = khtcMockStore().find(p => p.p.report_type === "week")!;
        const body = { period_id: period.p.id, scenario: "actual" };
        const preview = await api("/mau_1/preview", "POST", body);
        expect(preview.status).toBe(200);
        const one = (await preview.json()) as S["KHTCTemplatePreview"];
        const again = (await (await api("/mau_1/preview", "POST", body)).json()) as S["KHTCTemplatePreview"];
        expect(again.context.input_hash).toBe(one.context.input_hash);
        expect(one.document.input_hash).toBe(one.context.input_hash);
        const req = request("/mau_1/preview", "POST", body), route = khtcTemplateRoutes.find(r => r.method === "POST")!;
        const ctx: Ctx = { url: new URL(req.url), request: req, body, params: ["mau_1", "preview"], persona: structuredClone(findPersona("kn-admin")!) };
        ctx.persona.me.tenant_id = "01927b3e-0000-7000-8000-00000000b002";
        expect(ctx.persona.me.tenant_id).not.toBe(TENANT);
        expect(route.handler(ctx).status).toBe(404);
        const ownReq = request("/mau_1", "GET"), own: Ctx = { ...ctx, request: ownReq, body: undefined, params: ["mau_1"] };
        expect(((await route.handler(own).json()) as S["KHTCTemplateDetail"]).version).toBe(0);
    });
});
