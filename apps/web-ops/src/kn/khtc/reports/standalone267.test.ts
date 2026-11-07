import { afterEach, expect, it, vi } from "vitest";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { createApiClient } from "@/api/client";
import { createKhtcPeriodApi } from "../periods/api";
import { createReportApi } from "./api";

afterEach(() => vi.useRealTimers());

it("retired external mock APIs are404 for reads and writes", async () => {
  for (const token of ["mock.tinh", "mock.kn-admin"]) {
    for (const [method, path] of [["GET", "indicators"], ["POST", "indicators"], ["GET", "figures?year=2030"], ["POST", "figures"], ["GET", "history?year=2030"], ["GET", "template?year=2030"], ["POST", "imports"], ["POST", "evidence"]]) {
      const response = await mockFetchNoDelay(new Request(`http://mock/v1/extension/khtc/external/${path}`, {
        method, headers: { Authorization: `Bearer ${token}`, "Idempotency-Key": `retired267-${token}-${path}`, "Content-Type": "application/json" },
        ...(method === "POST" ? { body: "{}" } : {}),
      }));
      expect(response.status, `${token} ${method} ${path}`).toBe(404);
    }
  }
});

it("new mock reports omit external cells and preserve normal narrative saves", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2030-06-01T09:00:00Z"));
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch: mockFetchNoDelay });
  const period = await createKhtcPeriodApi(api).open({ report_type: "week", year: new Date().getFullYear() + 5, no: 8, departments: ["trong_trot"] });
  const reports = createReportApi(api);
  const draft = await reports.get(period.id);
  expect(draft.content.data.external).toBeUndefined();
  const saved = await reports.save(period.id, { base_version: draft.version, admin: {}, texts: { B: "Lời văn kiểm thử267" } });
  expect(saved.content.data.external).toBeUndefined();
  expect(saved.content.texts.B).toBe("Lời văn kiểm thử267");
});
