import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { mockFetchNoDelay } from "./handler";
import { resetKnCollect, knCollectRoutes } from "./kn-collect";
beforeEach(() => {
  vi.setSystemTime(new Date("2026-11-05T02:00:00Z"));
  resetKnCollect();
});
afterEach(() => vi.useRealTimers());
it("#160 six retired writes are404 in real mock dispatch; GET and KN records remain", async () => {
  for (const [persona, assignment] of [
    ["kn-an", "asg-an-xa"],
    ["kn-admin", "asg-admin"],
  ]) {
    const context = await mockFetchNoDelay(
      new Request("http://mock/v1/me/context", {
        method: "POST",
        headers: {
          Authorization: `Bearer mock.${persona}`,
          "Content-Type": "application/json",
          "Idempotency-Key": `ctx160:${persona}`,
        },
        body: JSON.stringify({
          assignment_id: assignment,
          department_code: persona === "kn-admin" ? null : "trong_trot",
        }),
      }),
    );
    expect(context.status).toBe(201);
    const { context_id } = await context.json();
    const headers = {
      Authorization: `Bearer mock.${persona}`,
      "X-AG-Context": context_id,
      "Content-Type": "application/json",
    };
    const get = async (path: string) =>
      mockFetchNoDelay(new Request(`http://mock${path}`, { headers }));
    const before = await (await get("/v1/kn/cooperatives")).json();
    expect(before.items.length).toBeGreaterThan(0);
    const ref = before.items[0].ref;
    const retired: [string, string][] = [
      ["POST", "/v1/kn/cooperatives"],
      ["PUT", `/v1/kn/cooperatives/${ref}`],
      ["POST", `/v1/kn/cooperatives/${ref}/deactivate`],
      ["POST", `/v1/kn/cooperatives/${ref}/reactivate`],
      ["POST", `/v1/kn/cooperatives/${ref}/households`],
      ["PUT", `/v1/kn/households/${ref}`],
    ];
    for (const [method, path] of retired) {
      expect(
        knCollectRoutes.some(
          (r) => r.method === method && r.pattern.test(path),
        ),
      ).toBe(false);
      const response = await mockFetchNoDelay(
        new Request(`http://mock${path}`, {
          method,
          headers: {
            ...headers,
            "Idempotency-Key": `retired160:${persona}:${method}:${path}`,
          },
          body: JSON.stringify({
            name: "Không tạo",
            commune_code: "91301",
            reason: "Không được đổi dữ liệu HTX",
          }),
        }),
      );
      expect(response.status, `${persona} ${method} ${path}`).toBe(404);
      expect((await response.json()).code).toBe("not_found");
    }
    expect(await (await get("/v1/kn/cooperatives")).json()).toEqual(before);
    for (const path of [
      `/v1/kn/cooperatives/${ref}`,
      `/v1/kn/cooperatives/${ref}/households`,
      "/v1/kn/producer-records?kind=cooperative",
    ])
      expect((await get(path)).status).toBe(200);
  }
});
