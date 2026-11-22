import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { createKhtcFormApi, emptyContent } from "./api";

describe("editor229 — khóa retry qua HTTP client thật", () => {
  it.each(["draft", "submit"] as const)("%s gửi lại đúng header và body sau mất ACK", async kind => {
    const requests: { url: string; key: string | null; body: unknown }[] = [];
    const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => "ctx229",
      fetch: async request => {
        requests.push({ url: request.url, key: request.headers.get("Idempotency-Key"), body: await request.json() });
        if (requests.length === 1) throw new TypeError("Lost response after dispatch");
        return Response.json({});
      },
    });
    const form = createKhtcFormApi(api), content = { ...emptyContent(), cells: { row: { done: "1,5" } } };
    const key = `editor229-${kind}-fixed-key`;
    await expect(form[kind]("period229", "trong_trot", content, 1, key)).rejects.toThrow();
    await form[kind]("period229", "trong_trot", content, 1, key);
    expect(requests).toHaveLength(2); expect(requests[0]).toEqual(requests[1]); expect(requests[0]?.key).toBe(key);
    expect(requests[0]?.body).toMatchObject({ base_attempt: 1, sheet: { cells: { row: { done: "1.5" } } } });
  });
});
