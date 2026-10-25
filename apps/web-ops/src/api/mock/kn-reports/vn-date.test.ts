// KN-VN-DATE (#149): from_date mặc định của GET report-forms và POST provincial-import là ngày lịch VN của đồng hồ giả
// lập, không phải ngày UTC (00:00–07:00 VN ngày UTC còn là hôm qua).
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createCycleApi, RawFile } from "@/kn/cycle/api";
import { mockFetchNoDelay } from "../handler";
import * as forms from "../forms/routes";
import { resetReportCycle } from "./store";

let now = 0;

async function tinhClient() {
  const ctxRes = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: "Bearer mock.kn-binh", "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: "asg-binh-tinh", department_code: "trong_trot" }),
    }),
  );
  const ctx = (await ctxRes.json()) as { context_id: string };
  return createCycleApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
}

beforeEach(() => {
  vi.restoreAllMocks();
  forms.resetMockForms();
  resetReportCycle(() => now);
});

describe("from_date mặc định theo ngày VN", () => {
  it.each([
    ["Thứ Hai 00:30 VN", "2026-10-25T17:30:00Z", "2026-10-26"],
    ["Chủ Nhật 23:59 VN", "2026-10-25T16:59:00Z", "2026-10-25"],
  ])("%s → %s", async (_label, iso, want) => {
    now = Date.parse(iso);
    const tinh = await tinhClient();
    const spy = vi.spyOn(forms, "mockAssignedForms");
    await tinh.reportForms("weekly");
    expect(spy).toHaveBeenLastCalledWith("trong_trot", "commune", "weekly", want);
    await tinh.provincialImport({ report_type: "weekly", template_ids: ["khong-co"] }, new RawFile("a\n", "text/csv")).catch(() => undefined);
    expect(spy).toHaveBeenLastCalledWith("trong_trot", "commune", "weekly", want);
  });
});
