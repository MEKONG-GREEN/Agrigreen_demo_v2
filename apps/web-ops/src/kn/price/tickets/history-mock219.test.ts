import { afterEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { resetPriceTickets } from "@/api/mock/kn-price-tickets";
import { createPriceTicketApi } from "./api";

afterEach(() => vi.useRealTimers());
it("mock history preserves native own-commune scope even for a hand-edited point query", async () => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));
  resetPricePoints(); resetPriceTickets();
  const result = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-an", "Content-Type": "application/json", "Idempotency-Key": "219-history-context" }, body: JSON.stringify({ assignment_id: "asg-an-xa", department_code: "trong_trot" }) }));
  expect(result.status).toBe(201);
  const context = await result.json() as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => context.context_id, fetch: mockFetchNoDelay });
  const ta = createPriceTicketApi(api);
  expect((await ta.history({ from: "2026-10-01", to: "2026-10-12", point: "XBG0001" })).rows.length).toBeGreaterThan(0);
  expect((await ta.history({ from: "2026-10-01", to: "2026-10-12", point: "XBG0002" })).rows).toEqual([]);
});
