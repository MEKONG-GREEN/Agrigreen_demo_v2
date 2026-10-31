import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { components } from "../generated/application";
import { createKnAdminApi } from "@/kn/cycle/api";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { provinceOrgRef } from "./data";
import { findPersona } from "./personas";
import { mockKnLogin } from "./kn";
import { KN_ACCOUNTS, KN_DEPARTMENTS, MOCK_KN_PASSWORD, resetKnAccounts } from "./kn-accounts";
import { resetKnAdmin } from "./kn-admin";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcTemplateNarrativeUsage, khtcTemplatePreviewContent } from "./kn-khtc-reports";
import { seedTemplate } from "./kn-khtc-template-render";
import { resetKhtcTemplates, useTemplateMockNarrativeUsage } from "./kn-khtc-templates";
import type { Ctx } from "./http";
type S = components["schemas"];
const original = structuredClone(KN_ACCOUNTS), departments = structuredClone(KN_DEPARTMENTS);
let seq = 0, admin = "";
async function context(persona: string, assignment: string, department: string | null) {
 const r = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-usage-${++seq}` }, body: JSON.stringify({ assignment_id: assignment, department_code: department }) }));
 expect(r.status).toBe(201); return ((await r.json()) as { context_id: string }).context_id;
}
function call(persona: string, ctx: string, path: string, method = "GET", body?: unknown) {
 return mockFetchNoDelay(new Request(`http://mock${path}`, { method, headers: { Authorization: `Bearer mock.${persona}`, "X-AG-Context": ctx, "Content-Type": "application/json", "Idempotency-Key": `usage-${++seq}` }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }));
}
function usageContext(): Ctx { const request = new Request("http://mock/usage", { headers: { "X-AG-Context": admin } }); return { url: new URL(request.url), request, params: [], body: undefined, persona: structuredClone(findPersona("kn-admin")!) }; }
function removedD() { const d = seedTemplate("mau_1")!.definition, node = d.nodes.find(n => n.id === "D")!; node.text_slot = false; node.bindings = []; node.body = "Ý kiến theo khung mới"; return d; }
beforeEach(async () => { resetKnAdmin(); resetKhtcTemplates(); useTemplateMockNarrativeUsage(undefined); admin = await context("kn-admin", "asg-admin", null); });
afterEach(() => { resetKnAccounts(original); for (let i=0;i<KN_DEPARTMENTS.length;i++) Object.assign(KN_DEPARTMENTS[i]!,structuredClone(departments[i]!)); });
describe("coordinated report narrative metadata seam", () => {
 it("does not count v0/autoload, counts persisted default D, and preserves cleared history without text leakage", async () => {
  const period = khtcMockStore().find(x => x.p.report_type === "week")!, ctx = usageContext();
  expect(khtcTemplateNarrativeUsage(ctx, period.p.id)).toEqual({ draftID: null, version: 0, hasSavedNarrative: false, revisions: [] });
  expect(khtcTemplatePreviewContent(ctx, period.p.id)?.texts.D).toBe("Không có.");
  expect(khtcTemplateNarrativeUsage(ctx, period.p.id)?.hasSavedNarrative).toBe(false);
  // A v0 preview's auto D must not force an acknowledgement.
  const initial = await call("kn-admin", admin, "/v1/extension/khtc/templates/mau_1/versions", "POST", { base_version: 0, definition: removedD() }); expect(initial.status).toBe(201);
  const back = await call("kn-admin", admin, "/v1/extension/khtc/templates/mau_1/versions", "POST", { base_version: 1, definition: seedTemplate("mau_1")!.definition }); expect(back.status).toBe(201);
  // Build the writer through the actual administration/context APIs, not a forged context.
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => admin, fetch: mockAdminFetchNoDelay });
  const dept = (await adm.departments()).items.find(d => d.code === "ke_hoach_tai_chinh")!; await adm.departmentStatus(dept.id, "active");
  const created = await adm.createKnUser({ full_name: "Cán bộ KHTC thử", email: "khtc-template-usage@demo.agrigreen.test", assignments: [{ role: "kn_tinh", org_ref: provinceOrgRef(1), department_codes: ["ke_hoach_tai_chinh"] }] });
  const login = mockKnLogin("khtc-template-usage@demo.agrigreen.test", MOCK_KN_PASSWORD); expect(login.ok).toBe(true); if (!login.ok) throw Error("mock login");
  const writer = await context(login.personaId, created.assignments[0]!.assignment_id, "ke_hoach_tai_chinh");
  const path = `/v1/extension/khtc/periods/${period.p.id}/report`;
  const current = (await (await call(login.personaId, writer, path)).json()) as S["KHTCReportDetail"]; expect(current.version).toBe(0);
  const saved = await call(login.personaId, writer, path + "/draft", "POST", { base_version: 0, admin: {}, texts: {} }); expect(saved.status).toBe(201);
  const first = khtcTemplateNarrativeUsage(ctx, period.p.id)!; expect(first.version).toBe(1); expect(first.hasSavedNarrative).toBe(true); expect(first.revisions[0]!.sections).toContain("D");
  expect(JSON.stringify(first)).not.toContain("Không có.");
  first.revisions[0]!.sections.length = 0; expect(khtcTemplateNarrativeUsage(ctx, period.p.id)!.revisions[0]!.sections).toContain("D");
  const cleared = await call(login.personaId, writer, path + "/draft", "POST", { base_version: 1, admin: {}, texts: { D: "" } }); expect(cleared.status).toBe(201);
  const history = khtcTemplateNarrativeUsage(ctx, period.p.id)!; expect(history.version).toBe(2); expect(history.hasSavedNarrative).toBe(true); expect(history.revisions).toHaveLength(2); expect(history.revisions[1]!.sections).not.toContain("D");
  const denied = await call("kn-admin", admin, "/v1/extension/khtc/templates/mau_1/versions", "POST", { base_version: 2, definition: removedD() }); expect(denied.status).toBe(409); expect((await denied.json()).code).toBe("extension.khtc_template_text_in_use");
  const acknowledged = await call("kn-admin", admin, "/v1/extension/khtc/templates/mau_1/versions", "POST", { base_version: 2, definition: removedD(), acknowledge_text_loss: true }); expect(acknowledged.status).toBe(201);
  expect(khtcTemplateNarrativeUsage(ctx, period.p.id)?.hasSavedNarrative).toBe(true);
  const foreign = structuredClone(ctx.persona); foreign.me.tenant_id = "01927b3e-0000-7000-8000-00000000b002";
  expect(khtcTemplateNarrativeUsage({ ...ctx, persona: foreign }, period.p.id)).toBeNull(); expect(khtcTemplatePreviewContent({ ...ctx, persona: foreign }, period.p.id)).toBeNull();
  expect(khtcTemplateNarrativeUsage(ctx, "01927b3e-0000-7000-8000-00000000ffff")).toBeNull();
 });
});
