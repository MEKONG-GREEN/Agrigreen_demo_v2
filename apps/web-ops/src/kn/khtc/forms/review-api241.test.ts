import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { createKhtcFormApi, type KhtcFormReviewInput } from "./api";

describe("review241 — client HTTP không tự chọn lại phiên bản", () => {
  it("mất ACK thì thử lại cùng nội dung và khóa đã cấp", async () => {
    const requests: { url: string; key: string | null; body: unknown }[] = [];
    const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => "ctx241",
      fetch: async request => {
        requests.push({ url: request.url, key: request.headers.get("Idempotency-Key"), body: await request.json() });
        if (requests.length === 1) throw new TypeError("Lost review response");
        return Response.json({ reviewed_submission_id: "33333333-3333-4333-8333-333333333333", reviewed_attempt: 2, review_decision: "returned" });
      },
    });
    const form = createKhtcFormApi(api), input: KhtcFormReviewInput = { decision: "returned", note: "Bổ sung diện tích", expected_submission_id: "33333333-3333-4333-8333-333333333333", base_attempt: 2 };
    const key = "review241-same-intent-key";
    await expect(form.review("11111111-1111-4111-8111-111111111111", "trong_trot", input, key)).rejects.toThrow();
    await expect(form.review("11111111-1111-4111-8111-111111111111", "trong_trot", input, key)).resolves.toMatchObject({ reviewed_submission_id: input.expected_submission_id, reviewed_attempt: 2, review_decision: "returned" });
    expect(requests).toHaveLength(2); expect(requests[0]).toEqual(requests[1]); expect(requests[0]?.key).toBe(key); expect(requests[0]?.body).toEqual(input);
  });
});
