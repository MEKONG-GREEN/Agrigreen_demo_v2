import { afterEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { uuidv7 } from "@/api/idempotency";
import { TENANT } from "@/api/mock/personas";
import { readPending243 } from "./api";

afterEach(() => vi.useRealTimers());
it("adhoc mock preserves finite window/membership/tenant/terminal filters before LIMIT and never writes", async () => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-01-02T03:00:00Z"));
  const selected = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": "pending245-context" }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
  expect(selected.status).toBe(201); const context = await selected.json() as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch: mockFetchNoDelay });
  const store = khtcMockStore(), original = structuredClone(store), sample = store.find(s => s.p.report_type === "week")!;
  try {
    for (const item of store) item.terminal = "closed";
    const target = structuredClone(sample); target.p.id = uuidv7(Date.now() + 1); target.terminal = ""; target.p.forms_status = "open"; target.p.report_type = "adhoc"; target.adhocTenant = TENANT;
    target.p.departments = target.p.departments.filter(d => d.code === "trong_trot");
    target.submitted = { trong_trot: { attempt: 1, decision: "", at: "2030-01-01T02:00:00Z" } }; store.push(target);
    for (let i = 0; i < 27; i++) { const unrelated = structuredClone(target); unrelated.p.id = uuidv7(Date.now() + 2 + i); unrelated.p.departments = [{ code: "hanh_chinh_tong_hop", name: "HCTH" }]; unrelated.submitted = {}; store.push(unrelated); }
    for (const kind of ["foreign", "cancelled", "regular"]) { const hidden = structuredClone(target); hidden.p.id = uuidv7(Date.now() + 100 + store.length); if (kind === "foreign") hidden.adhocTenant = "foreign245"; if (kind === "cancelled") hidden.terminal = "cancelled"; if (kind === "regular") hidden.p.report_type = "week"; store.push(hidden); }
    const before = structuredClone(store), signal = new AbortController().signal;
    const out = await readPending243(api, null, signal, { report_type: "adhoc", department: "trong_trot" });
    expect(out.rows).toHaveLength(1); expect(out.rows[0]!.period_id).toBe(target.p.id); expect(out.next_cursor).toBeNull();
    const exact = await readPending243(api, null, signal, { report_type: "adhoc", period_id: target.p.id });
    expect(exact.rows).toEqual(out.rows); expect(store).toEqual(before);
  } finally { store.splice(0, store.length, ...original); }
});
