import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmPlans } from "@/api/mock/kn-hrm-plans";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { createPlanApi, planDemand } from "./plan-api";
import { HrmPlanCenterScreen, HrmPlanUnitScreen } from "./PlanScreens";

const params: { year: string; unit: string } = { year: "2027", unit: "trong_trot" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/ke-hoach",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useParams: () => params,
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));
vi.mock("@/lib/files", async (orig) => ({
  ...(await orig<object>()),
  saveBlob: vi.fn(),
}));

async function clientFor(personaId: string, assignmentId: string, department = "trong_trot") {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: department }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, pa: createPlanApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("kế hoạch nhân lực năm (W11.27, FN-11) — giả lập", () => {
  beforeEach(() => {
    resetHrmStaff();
    resetHrmPlans();
  });

  it("nhu cầu theo kế hoạch = hiện có − nghỉ hưu − nghỉ/chuyển khác + cần bổ sung", () => {
    expect(planDemand({ current: 10, retiring: 1, leaving: 2, additional: 3 })).toBe(10);
  });

  it("Admin mở kỳ, bảng tổng hợp cộng đúng; mở trùng 409; Cấp Tỉnh không phải lãnh đạo/HCTH bị từ chối", async () => {
    const { pa } = await clientFor("kn-admin", "asg-admin");
    const p = await pa.open({ year: 2027, due_on: "2026-11-30", cutoff_on: "2026-10-12" });
    expect(p.status).toBe("open");
    expect(await problemCode(pa.open({ year: 2027, due_on: "2026-11-30" }))).toBe("hrm.plan_exists");
    const s = await pa.summary(2027);
    expect(s.rows.length).toBe(p.units.length);
    expect(s.totals.current).toBe(s.rows.reduce((a, r) => a + r.values.current, 0));
    expect(s.rows.every((r) => r.status === "not_started")).toBe(true);
    // Admin không lập kế hoạch của đơn vị (chỉ lãnh đạo đơn vị).
    expect(await problemCode(pa.submit(2027, "trong_trot", { leaving: 0, additional: 1, base_version: 0 }))).toBe("forbidden");
    // Chốt rồi chỉ điều chỉnh có lý do.
    expect(await problemCode(pa.adjust(2027, "trong_trot", { leaving: 0, additional: 2, reason: "Bổ sung theo chỉ tiêu mới" }))).toBe(
      "hrm.plan_not_closed",
    );
    const closed = await pa.close(2027);
    expect(closed.rows.every((r) => r.status === "missed")).toBe(true);
    const a = await pa.adjust(2027, "trong_trot", { leaving: 0, additional: 2, reason: "Bổ sung theo chỉ tiêu mới" });
    expect(a.values.demand).toBe(a.values.current - a.values.retiring + 2);
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tt.pa.list())).toBe("forbidden");
  });

  it("màn tổng hợp: mở kỳ qua hộp thoại, bảng có dòng Toàn Trung tâm; chi tiết đơn vị hiện số tự động", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    const u = userEvent.setup();
    const { unmount } = render(<HrmPlanCenterScreen />);
    await u.click(await screen.findByRole("button", { name: "Mở kỳ kế hoạch" }));
    const year = screen.getByLabelText("Năm kế hoạch") as HTMLInputElement;
    await u.clear(year);
    await u.type(year, "2027");
    await u.click(screen.getByRole("button", { name: "Mở kỳ" }));
    expect(await screen.findByText("Toàn Trung tâm")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Kế hoạch nhân lực năm 2027" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Chốt kế hoạch" })).toBeTruthy();
    unmount();
    render(<HrmPlanUnitScreen />);
    await waitFor(() => expect(screen.getByText("Số liệu hiện có (tự động)")).toBeTruthy());
    expect(screen.getByRole("heading", { name: "Nhu cầu theo vị trí" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Gửi Phòng HCTH" })).toBeNull();
  });

  it("Nhắc phòng chưa lập (NOTIFY-HCTH-KN #50): nhắc đơn vị chưa gửi; sau chốt 409, nút tắt; Cấp Tỉnh thường bị từ chối", async () => {
    const admin = await clientFor("kn-admin", "asg-admin");
    const p = await admin.pa.open({ year: 2027, due_on: "2026-11-30", cutoff_on: "2026-10-12" });
    const r = await admin.pa.remind(2027);
    expect(r.units.map((x) => x.unit)).toEqual(p.units);
    expect(r.units.every((x) => x.unit_name !== "")).toBe(true);
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tt.pa.remind(2027))).toBe("forbidden");

    auth.api = admin.api;
    const u = userEvent.setup();
    const { unmount } = render(<HrmPlanCenterScreen />);
    await u.click(await screen.findByRole("button", { name: "Nhắc phòng chưa lập" }));
    expect(await screen.findByText(new RegExp(`Đã nhắc ${p.units.length} đơn vị chưa gửi kế hoạch`))).toBeTruthy();
    unmount();

    await admin.pa.close(2027);
    expect(await problemCode(admin.pa.remind(2027))).toBe("hrm.plan_closed");
    render(<HrmPlanCenterScreen />);
    const btn = (await screen.findByRole("button", { name: "Nhắc phòng chưa lập" })) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });
});
