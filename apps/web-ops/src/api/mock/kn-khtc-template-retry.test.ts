import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { createTemplateApi } from "@/kn/khtc/templates/api";
import { mockFetchNoDelay } from "./handler";
import { resetKhtcTemplates } from "./kn-khtc-templates";
import { seedTemplate } from "./kn-khtc-template-render";
let contextID = "", seq = 0;
beforeEach(async () => {
 resetKhtcTemplates();
 const r = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `retryctx-${++seq}` }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
 contextID = ((await r.json()) as { context_id: string }).context_id;
});
describe("template client operation retry", () => {
 it("reuses save/publish/restore keys after committed responses are lost and changes the key with body/context", async () => {
  const seen: { path: string; context: string | null; key: string | null }[] = [];
  let drop = true;
  const api = createTemplateApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => contextID, fetch: async request => {
   seen.push({ path: new URL(request.url).pathname, context: request.headers.get("X-AG-Context"), key: request.headers.get("Idempotency-Key") });
   const response = await mockFetchNoDelay(request);
   if (drop && response.ok) { drop = false; throw new TypeError("response lost after commit"); }
   return response;
  } }));
  const op = new IdempotentOperation(), definition = seedTemplate("mau_1")!.definition;
  const save = () => withIdempotentOperation(op, () => api.save("mau_1", { base_version: 0, definition }));
  await expect(save()).rejects.toThrow(); const one = await save(); expect(one.version).toBe(1); expect(seen[1]!.key).toBe(seen[0]!.key);
  definition.nodes.push({ id: "extra", parent: "A", order: 100, title: "Bổ sung", repeat: "none", body: "Một câu", bindings: [], text_slot: false, locked_admin: [] });
  const two = await withIdempotentOperation(op, () => api.save("mau_1", { base_version: 1, definition })); expect(two.version).toBe(2); expect(seen[2]!.key).not.toBe(seen[1]!.key);
  drop = true;
  const publish = () => withIdempotentOperation(op, () => api.publish("mau_1", { base_version: 2, version_id: two.id, applies_from: "2027-01-01" }));
  await expect(publish()).rejects.toThrow(); const publication = await publish(); expect(publication.version_id).toBe(two.id); expect(seen[4]!.key).toBe(seen[3]!.key);
  drop = true;
  const restore = () => withIdempotentOperation(op, () => api.restore("mau_1", { base_version: 2, version_id: one.id }));
  await expect(restore()).rejects.toThrow(); const restored = await restore(); expect(restored.version).toBe(3); expect(seen[6]!.key).toBe(seen[5]!.key);
  const old = contextID;
  const r = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `retryctx-${++seq}` }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
  contextID = ((await r.json()) as { context_id: string }).context_id; expect(contextID).not.toBe(old);
  await expect(withIdempotentOperation(op, () => api.restore("mau_1", { base_version: 2, version_id: one.id }))).rejects.toThrow();
  expect(seen[7]!.key).not.toBe(seen[6]!.key); expect(seen[7]!.context).not.toBe(seen[6]!.context);
  expect((await api.get("mau_1")).history).toHaveLength(3);
 });
});
