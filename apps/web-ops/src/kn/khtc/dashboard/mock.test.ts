import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { createDashboardApi } from "./api";

afterEach(() => vi.useRealTimers());
describe("dashboard mock submission counts", () => {
 it("counts submitted/confirmed and excludes returned forms while retaining timestamps", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-10-14T03:00:00Z"));
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
   method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": "dashboard-count-context" },
   body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }),
  }));
  expect(response.status).toBe(201);
  const context = await response.json() as { context_id: string };
  const api = createDashboardApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch: mockFetchNoDelay }));
  const stored = khtcMockStore().find(s => s.p.report_type === "week" && s.p.year === 2026 && !s.terminal)!;
  expect(stored).toBeDefined();
  const dept = stored.p.departments[0]!.code, previous = stored.submitted[dept];
  try {
   for (const decision of ["", "accepted", "returned"] as const) {
    stored.submitted[dept] = { attempt: 1, decision, at: "2026-10-13T02:00:00Z" };
    const quarter = Math.floor((Number(stored.p.from_date.slice(5, 7)) - 1) / 3) + 1;
    const d = await api.get({ year: 2026, quarter });
    const row = d.status.find(s => s.period_id === stored.p.id && s.department === dept && s.kind === "form")!;
    expect(row.state).toBe(decision === "returned" ? "supplement" : decision === "accepted" ? "confirmed" : "submitted");
    expect(row.at).toBe("2026-10-13T02:00:00Z");
    expect(d.counts.sent).toBe(d.status.filter(s => s.kind === "form" && ["submitted", "confirmed"].includes(s.state)).length);
   }
  } finally { if (previous) stored.submitted[dept] = previous; else delete stored.submitted[dept]; }
 });
});