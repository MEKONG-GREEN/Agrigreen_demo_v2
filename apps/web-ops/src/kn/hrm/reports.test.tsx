import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmReports } from "@/api/mock/kn-hrm-reports";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { createReportsApi } from "./reports-api";
import {
  HrmDashboardScreen,
  HrmReportDetailScreen,
  HrmReportListScreen,
} from "./ReportScreens";

const params: { code: string } = { code: "NS06" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/bao-cao-so",
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

async function clientFor(
  personaId: string,
  assignmentId: string,
  department = "trong_trot",
) {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({
        assignment_id: assignmentId,
        department_code: department,
      }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, ra: createReportsApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("báo cáo Sở NS01–NS06 + dashboard (W11.28) — giả lập", () => {
  beforeEach(() => {
    resetHrmStaff();
    resetHrmReports();
  });

  it("Admin xem trước, xuất, lịch sử, tải lại; Cấp Tỉnh ngoài HCTH bị từ chối", async () => {
    const { ra } = await clientFor("kn-admin", "asg-admin");
    const list = await ra.list();
    expect(list.items.map((x) => x.code)).toEqual([
      "NS01",
      "NS02",
      "NS03",
      "NS04",
      "NS05",
      "NS06",
    ]);
    const v = await ra.preview("NS01");
    expect(v.rows[0]?.cells[1]).toBe("I. TRUNG TÂM KHUYẾN NÔNG");
    expect(v.period).toMatch(/^THÁNG /);
    const ex = await ra.exportReport("NS01", {});
    expect(ex.file_name).toMatch(/^NS01_/);
    const f = await ra.file(ex.id);
    expect(f.drift).toBe(false);
    const h = await ra.history("NS01");
    expect(h.items.map((x) => x.action)).toEqual(["download", "export"]);
    const n6 = await ra.preview("NS06");
    const total = n6.rows.find((r) => r.kind === "total")!;
    const c = await ra.cell("NS06", n6.as_of, "_center", "tong");
    expect(String(c.items.length)).toBe(total.cells[2]);
    expect(
      await problemCode(ra.cell("NS01", n6.as_of, "_center", "tong")),
    ).toBe("validation_failed");
    expect(await problemCode(ra.preview("NS09" as never))).toBe(
      "hrm.report_unknown",
    );
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tt.ra.list())).toBe("forbidden");
  });

  it("dashboard: tổng = số dòng NS01 cùng ngày; thành viên chỉ số", async () => {
    const { ra } = await clientFor("kn-admin", "asg-admin");
    const d = await ra.dashboard();
    const n1 = await ra.preview("NS01");
    expect(d.total).toBe(n1.rows.filter((r) => r.kind === "person").length);
    const m = await ra.members("total");
    expect(m.total).toBe(d.total);
  });

  it("màn danh sách, chi tiết (nhấn ô) và dashboard hiển thị", async () => {
    const { api } = await clientFor("kn-admin", "asg-admin");
    auth.api = api;
    const u = userEvent.setup();
    const { unmount } = render(<HrmReportListScreen />);
    expect(
      await screen.findByText("Báo cáo chất lượng CCVC, HĐLĐ (Phụ lục 2)"),
    ).toBeTruthy();
    unmount();
    const d = render(<HrmReportDetailScreen />);
    expect(await screen.findByText("Kiểm tra số liệu")).toBeTruthy();
    const cellBtn = (
      await screen.findAllByRole("button", {
        name: /^Xem \d+ người: TỔNG CỘNG — Tổng số/,
      })
    )[0]!;
    await u.click(cellBtn);
    expect(await screen.findByRole("dialog")).toBeTruthy();
    d.unmount();
    render(<HrmDashboardScreen />);
    await waitFor(() =>
      expect(screen.getByText("Viên chức có mặt (NS01)")).toBeTruthy(),
    );
  });
});
