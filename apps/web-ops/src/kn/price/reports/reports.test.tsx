import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { ApiProblem } from "@/api/problem";
import { createPriceReportApi, signedPct } from "./api";
import {
  barPct,
  MovementScreen,
  ProgressScreen,
  StatsScreen,
  SummaryScreen,
} from "./ReportScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/bang-gia",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "kn_tinh" } } }),
}));

async function clientFor(personaId: string, assignmentId: string) {
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
        department_code: "trong_trot",
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
  return { api, ra: createPriceReportApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("báo cáo giá W11.77 — quy tắc ở trình duyệt (không float)", () => {
  it("% có dấu kiểu Việt; thanh biểu đồ tính tỉ lệ bằng Decimal", () => {
    expect(signedPct("1.56")).toBe("+1,56%");
    expect(signedPct("-1.04")).toBe("-1,04%");
    expect(signedPct(null)).toBe("—");
    expect(barPct("10666.6667", "13000")).toBe("82.1");
    expect(barPct("0", "13000")).toBe("0");
    expect(barPct("5", null)).toBe("0");
  });
});

describe("báo cáo giá W11.77 — màn và quyền (giả lập)", () => {
  it("BG-FN-TW-01.01: Cấp TW xem Bảng giá tổng hợp chỉ xem — Thấp nhất 8.000, Cao nhất 14.000, Trung bình 10.666,67, 3 xã", async () => {
    const { api } = await clientFor("kn-quan", "asg-quan-tw");
    auth.api = api;
    render(<SummaryScreen />);
    expect(await screen.findByText("Dưa leo Loại 1")).toBeInTheDocument();
    expect(screen.getByText(/Chỉ xem/)).toBeInTheDocument();
    expect(screen.getByText("14.000")).toBeInTheDocument();
    expect(screen.getByText("10.666,67")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Sửa|Xóa/ })).toBeNull();
  });

  it("BG-FN-TW-01.02 AC-03: Cấp TW không thấy nút xuất; Cấp Tỉnh thấy Xuất Excel / PDF", async () => {
    const tw = await clientFor("kn-quan", "asg-quan-tw");
    auth.api = tw.api;
    const { unmount } = render(<MovementScreen />);
    expect(await screen.findByText(/Số lần vượt ngưỡng/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Xuất PDF" })).toBeNull();
    expect(await problemCode(tw.ra.movementExport({ format: "xlsx" }))).toBe(
      "forbidden",
    );
    unmount();
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    auth.api = tinh.api;
    render(<MovementScreen />);
    expect(
      await screen.findByRole("button", { name: "Xuất PDF" }),
    ).toBeInTheDocument();
  });

  it("BG-FN-TI-02.01 / 02.08: Tiến độ nhập giá và Thống kê phiếu chỉ Cấp Tỉnh; thẻ khớp bảng", async () => {
    const tw = await clientFor("kn-quan", "asg-quan-tw");
    expect(await problemCode(tw.ra.progress({}))).toBe("forbidden");
    expect(await problemCode(tw.ra.stats({}))).toBe("forbidden");
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const p = await tinh.ra.progress({ status: "incomplete" });
    expect(p.cards.points).toBe(p.rows.length);
    auth.api = tinh.api;
    const { unmount } = render(<ProgressScreen />);
    expect(
      await screen.findByText("Đã gửi / phải báo giá"),
    ).toBeInTheDocument();
    expect(screen.getByText("Hệ thống đã nhắc")).toBeInTheDocument();
    unmount();
    render(<StatsScreen />);
    expect(await screen.findByText("Đúng hạn / phải nộp")).toBeInTheDocument();
    expect(screen.getAllByText("22").length).toBeGreaterThan(0);
  });
});
