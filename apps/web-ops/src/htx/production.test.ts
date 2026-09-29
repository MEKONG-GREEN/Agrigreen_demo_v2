import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { COOPERATIVES, htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { ApiProblem } from "@/api/problem";
import { requestExport, waitExport } from "@/reports/exports";
import { HTX_REPORT_KEY, productionReportParams } from "./production";

const app = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("mong đợi ApiProblem");
}

describe("HTX US-DASH-02: bộ lọc của việc xuất báo cáo sản lượng", () => {
  it("gửi đúng bộ lọc đang xem; trường trống = tất cả (không gửi)", () => {
    expect(productionReportParams({ coopRef: "c1", seasonCode: "HT", year: "2026", variety: "ST25" })).toEqual({
      cooperative_ref: "c1",
      season_code: "HT",
      season_year: 2026,
      variety_code: "ST25",
    });
    expect(productionReportParams({ coopRef: "", seasonCode: "", year: " ", variety: "" })).toEqual({});
    expect(productionReportParams({ year: "20x6" })).toEqual({});
  });
});

describe("HTX US-DASH-02: xuất báo cáo sản lượng qua pf/reports (giả lập)", () => {
  it("quản lý HTX: 202 → việc xuất chỉ HTX của mình → xong (tạm tính khi còn lô chưa khai)", async () => {
    const api = app("htx");
    const job = await requestExport(api, { template_key: HTX_REPORT_KEY, format: "pdf", params: productionReportParams({ seasonCode: "HT", year: "2026" }) }, newIdempotencyKey());
    expect(job.status).toBe("queued");
    expect(job.params.cooperative_ref).toBe(htxCoop().api.ref);
    const done = await waitExport(api, job.id, { intervalMs: 100, timeoutMs: 5000 });
    expect(done.status).toBe("completed");
    expect(done.provisional).toBe(true);
  });

  it("HTX khác → 403 reports.out_of_scope; năm vụ ngoài 2000–2100 → 422", async () => {
    const api = app("htx");
    const other = COOPERATIVES.find((c) => c.api.ref !== htxCoop().api.ref)!;
    const out = await problemOf(requestExport(api, { template_key: HTX_REPORT_KEY, format: "xlsx", params: { cooperative_ref: other.api.ref } }, newIdempotencyKey()));
    expect(out.code).toBe("reports.out_of_scope");
    const bad = await problemOf(requestExport(api, { template_key: HTX_REPORT_KEY, format: "xlsx", params: { season_year: 1999 } }, newIdempotencyKey()));
    expect(bad.status).toBe(422);
  });
});
