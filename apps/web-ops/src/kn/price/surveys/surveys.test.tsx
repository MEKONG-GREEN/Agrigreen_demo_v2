import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { CostReportScreen, SurveyScreen } from "./SurveyScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/dieu-tra",
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
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  return createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
}

describe("điều tra giá thành W11.78 — màn (giả lập)", () => {
  it("BG-FN-TI-02.10 / 02.12: Cấp Tỉnh xem đợt, tiến độ theo xã, phiếu Mít Thái 25.000 có cờ Cao bất thường, Chốt đợt bị chặn", async () => {
    auth.api = await clientFor("kn-binh", "asg-binh-tinh");
    render(<SurveyScreen />);
    expect((await screen.findAllByText("Hè Thu 2026")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole("button", { name: "Mở" })[0]!);
    expect(await screen.findByRole("tab", { name: "Rà soát phiếu" })).toBeInTheDocument();
    expect(screen.getByText(/chưa giao cán bộ điều tra/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Rà soát phiếu" }));
    expect(await screen.findByText("▲ Cao bất thường")).toBeInTheDocument();
    expect(screen.getByText("25.000")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Chốt đợt \(còn/ })).toBeDisabled();
  });

  it("BG-FN-TW-01.04: Cấp TW xem giá thành Lúa OM 18 bình quân 5.413, trung vị 5.384, không có nút xuất; Cấp Tỉnh có Xuất Excel", async () => {
    auth.api = await clientFor("kn-quan", "asg-quan-tw");
    const { unmount } = render(<CostReportScreen />);
    expect((await screen.findAllByText("Lúa OM 18")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("5.413").length).toBeGreaterThan(0);
    expect(screen.getAllByText("5.384").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
    expect(screen.queryByText(/Nông hộ/)).toBeNull();
    unmount();
    auth.api = await clientFor("kn-binh", "asg-binh-tinh");
    render(<CostReportScreen />);
    expect(await screen.findByRole("button", { name: "Xuất Excel" })).toBeInTheDocument();
  });
});
