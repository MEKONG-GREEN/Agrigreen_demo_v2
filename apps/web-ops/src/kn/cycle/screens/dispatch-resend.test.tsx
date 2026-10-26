// Review #128 (P0-WORKER-REPORTS): nút "Gửi lại" ở màn Báo cáo Chi cục theo can_resend/resumable_at máy chủ tính —
// "Đang gửi" còn hạn nhận việc thì khóa (hiện mốc mở), quá hạn (worker chết) thì mở "Gửi lại (tiếp tục lượt đang kẹt)"
// và POST tiếp tục cùng lượt; gửi khi còn hạn → 409 reports.dispatch_sending.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetMockForms } from "@/api/mock/forms/routes";
import { MOCK_SEND_LEASE_MS, dispatchOf, resetReportCycle, stallDispatch } from "@/api/mock/kn-reports/store";
import { ApiProblem } from "@/api/problem";
import { vnDate } from "@/kn/schedule/week";
import { createCycleApi } from "../api";
import { DispatchScreen } from "./ReportScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-cao/chi-cuc",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

type CycleApi = ReturnType<typeof createCycleApi>;
const auth: { api: ApiClient | null; cycleApi: CycleApi | null } = { api: null, cycleApi: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, cycleApi: auth.cycleApi, kn: { context: { role: "kn_tinh", communes: [] } } }),
}));

// Đồng hồ máy chủ giả lập (thời điểm thật để kỳ mặc định của màn trùng kỳ của test).
let now = Date.now();

async function tinhClient() {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: "Bearer mock.kn-binh", "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: "asg-binh-tinh", department_code: "trong_trot" }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const opts = { baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => ctx.context_id, fetch: mockFetchNoDelay };
  return { api: createApiClient(opts), cycle: createCycleApi(opts) };
}

const code = async (p: Promise<unknown>) => {
  try {
    await p;
    return "ok";
  } catch (e) {
    return e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e);
  }
};

/** Xuất + gửi trên đúng kỳ màn chọn mặc định, rồi cho worker giả "chết" giữa chừng. */
async function stuckDispatch(cycle: CycleApi) {
  const batch = (await cycle.batches()).items.find((b) => b.status === "issued")!;
  const { periods } = await cycle.batch(batch.id);
  // Cùng quy tắc kỳ mặc định của màn (useBatchPeriod): from_date so theo ngày lịch VN.
  const today = vnDate(Date.now());
  const period = [...periods].reverse().find((p) => p.from_date <= today) ?? periods[0]!;
  await cycle.consolidate(period.id);
  const exp = await cycle.exportDispatch({ period_id: period.id, formats: ["xlsx"] });
  const sent = await cycle.send(exp.dispatch.id, {});
  stallDispatch(dispatchOf(exp.dispatch.id)!);
  return sent.dispatch;
}

beforeEach(() => {
  now = Date.now();
  resetMockForms();
  resetReportCycle(() => now);
});

describe("Báo cáo Chi cục — Gửi lại khi lượt gửi kẹt (review #128)", () => {
  it("API giả lập: còn hạn → can_resend=false + 409; quá hạn → can_resend=true, Gửi lại 202 cùng lượt", async () => {
    const { cycle } = await tinhClient();
    const d = await stuckDispatch(cycle);
    expect(d).toMatchObject({ status: "sending", can_resend: false, attempts: 1 });
    expect(Date.parse(d.resumable_at!)).toBe(Date.parse(d.updated_at) + MOCK_SEND_LEASE_MS);
    expect(await code(cycle.send(d.id, {}))).toBe("409 reports.dispatch_sending");

    now += MOCK_SEND_LEASE_MS + 1000;
    const late = (await cycle.dispatch(d.id)).dispatch;
    expect(late).toMatchObject({ status: "sending", can_resend: true });
    const resumed = await cycle.send(d.id, {});
    expect(resumed.dispatch).toMatchObject({ status: "sending", attempts: 1, can_resend: false });
    expect(resumed.log.at(-1)).toMatchObject({ event: "dispatch.send_requested", detail: { resumed: true } });
    now += 5000;
    expect((await cycle.dispatch(d.id)).dispatch).toMatchObject({ status: "sent", can_resend: true, resumable_at: null });
  });

  it("màn: còn hạn khóa nút + hiện mốc mở; quá hạn mở “Gửi lại (tiếp tục lượt đang kẹt)” và bấm được", async () => {
    const { api, cycle } = await tinhClient();
    auth.api = api;
    auth.cycleApi = cycle;
    const d = await stuckDispatch(cycle);
    render(<DispatchScreen />);

    const locked = await screen.findByRole("button", { name: /Gửi email/ });
    await waitFor(() => expect(screen.getByTestId("dispatch-status")).toHaveTextContent("Đang gửi"));
    expect(locked).toBeDisabled();
    expect(screen.getByTestId("dispatch-resend-hint")).toHaveTextContent(/nút “Gửi lại” mở từ \d\d\/\d\d\/\d{4} \d\d:\d\d/);

    now += MOCK_SEND_LEASE_MS + 1000;
    fireEvent.click(screen.getByRole("button", { name: "Làm mới trạng thái" }));
    const resume = await screen.findByRole("button", { name: /Gửi lại \(tiếp tục lượt đang kẹt\)/ });
    expect(resume).toBeEnabled();
    expect(screen.getByTestId("dispatch-resend-hint")).toHaveTextContent(/quá hạn xử lý/);

    fireEvent.click(resume);
    expect(await screen.findByText(/Đã xếp gửi lại \(202\) — tiếp tục lượt đang kẹt/)).toBeInTheDocument();
    expect(dispatchOf(d.id)).toMatchObject({ status: "sending", attempts: 1 });
    await waitFor(() => expect(screen.getByRole("button", { name: /Gửi email/ })).toBeDisabled());
  });

  it("màn: “Gửi lỗi” → nút “Gửi lại” mở", async () => {
    const { api, cycle } = await tinhClient();
    auth.api = api;
    auth.cycleApi = cycle;
    const d = await stuckDispatch(cycle);
    Object.assign(dispatchOf(d.id)!, { status: "failed", last_error: "SMTP từ chối" });
    render(<DispatchScreen />);
    await waitFor(() => expect(screen.getByTestId("dispatch-status")).toHaveTextContent("Gửi lỗi"));
    expect(screen.getByRole("button", { name: /^\s*Gửi lại$/ })).toBeEnabled();
    expect(screen.queryByTestId("dispatch-resend-hint")).toBeNull();
  });
});
