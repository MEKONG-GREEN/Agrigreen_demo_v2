import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { CostReportScreen } from "./SurveyScreens";
import { ColumnChart } from "@/components/BarChart";
import { CostCharts } from "./CostCharts";
import { createSurveyApi, type SurveyCostReport } from "./api";

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api }) }));

async function clientFor(persona = "kn-quan", assignment = "asg-quan-tw") {
  const context = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
    method: "POST", headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `cost-charts-${persona}` },
    body: JSON.stringify({ assignment_id: assignment, department_code: "trong_trot" }),
  }));
  const { context_id } = await context.json() as { context_id: string };
  return createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => context_id, fetch: mockFetchNoDelay });
}

function stub(fetcher: typeof fetch) {
  return createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-quan", fetch: fetcher });
}

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("PRICE-COST-CHARTS — FN-TW-01.04 v2", () => {
  it("Cấp TW xem hai biểu đồ từ cùng báo cáo điều tra đã chốt", async () => {
    auth.api = await clientFor();
    render(<CostReportScreen />);
    expect((await screen.findAllByText("Lúa OM 18")).length).toBeGreaterThan(0);
    expect(screen.getByRole("img", { name: /Cơ cấu chi phí bình quân/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Giá thành – giá bán theo vùng/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).not.toBeInTheDocument();
    const price = screen.getByRole("img", { name: /Giá thành – giá bán theo vùng/ });
    expect(price).toHaveTextContent("5.427 đồng/kg");
    expect(price).toHaveTextContent("7.100 đồng/kg");
    expect(price).toHaveTextContent("5.384 đồng/kg");
    expect(price).toHaveTextContent("6.800 đồng/kg");
    expect(price).toHaveTextContent("2 phiếu · Mẫu nhỏ");
    expect(screen.getByRole("table", { name: /Giá thành theo mặt hàng/ })).toHaveTextContent("5.412.667");
    fireEvent.change(screen.getByLabelText("Nhóm cây"), { target: { value: "lua" } });
    fireEvent.change(screen.getByLabelText("Mặt hàng"), { target: { value: "MH00004" } });
    fireEvent.change(screen.getByLabelText("Nhóm vùng"), { target: { value: "kien_giang" } });
    fireEvent.change(screen.getByLabelText("Xã"), { target: { value: "30544" } });
    await waitFor(() => expect(screen.getByRole("img", { name: /Giá thành – giá bán theo vùng/ })).not.toHaveTextContent("An Giang"));
    expect(screen.getByRole("img", { name: /Cơ cấu chi phí bình quân/ })).toHaveTextContent("5.384.000 đồng/phiếu");
    expect(within(screen.getByRole("table", { name: /Giá thành theo mặt hàng/ })).getAllByRole("row")).toHaveLength(2);
    expect(screen.getByRole("table", { name: /Giá thành theo mặt hàng/ })).toHaveTextContent("Mẫu nhỏ");
    // Hai bộ lọc không giao nhau: bảng và cả hai biểu đồ đều rỗng; danh mục lựa chọn vẫn còn.
    fireEvent.change(screen.getByLabelText("Xã"), { target: { value: "30280" } });
    await waitFor(() => expect(screen.queryByRole("img", { name: /Cơ cấu chi phí bình quân/ })).not.toBeInTheDocument());
    expect(screen.getAllByText("Không có phiếu đã duyệt phù hợp để vẽ biểu đồ.")).toHaveLength(2);
    expect(within(screen.getByLabelText("Xã")).getAllByRole("option")).toHaveLength(3);
  });

  it("giữ đúng sáu khoản, mẫu nhỏ, giá 0; hình học không tạo cột giá trị giả", async () => {
    const data = await createSurveyApi(await clientFor()).report({});
    const block = data.main!;
    block.rows[0]!.avg_costs = { seed: "100", fertilizer: "200", pesticide: "0", labor: "300", machine: "400", other: "500" };
    block.regions = [{ ...block.regions[0]!, avg_cost_per_kg: "0", avg_price: "0" }];
    render(<CostCharts block={block} />);
    const cost = screen.getByRole("img", { name: /Cơ cấu chi phí bình quân/ });
    const rects = [...cost.querySelectorAll("rect")];
    expect(rects).toHaveLength(6);
    [12, 24, 0, 36, 48, 60].forEach((height, i) => expect(Number(rects[i]!.getAttribute("height"))).toBeCloseTo(height, 4));
    expect(rects.map((r) => r.getAttribute("x"))).toEqual(Array(6).fill(rects[0]!.getAttribute("x")));
    expect(cost).toHaveTextContent("Phân bón: 200 đồng/phiếu");
    const prices = screen.getByRole("img", { name: /Giá thành – giá bán theo vùng/ });
    expect([...prices.querySelectorAll("rect")].map((r) => r.getAttribute("height"))).toEqual(["0", "0"]);
    expect(prices).not.toHaveTextContent(/NaN|Infinity/);
  });

  it.each(["1e100000", "1e-100000", "9".repeat(100_000)])("chặn chuỗi số không hợp lệ trước parse (%#)", (value) => {
    render(<ColumnChart title="Cơ cấu chi phí" unit="đồng/phiếu" series={[{ key: "seed", label: "Giống", color: "green" }]} data={[{ key: "a", label: "Lúa", note: "1 phiếu · Mẫu nhỏ", values: { seed: value } }]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("dữ liệu số không hợp lệ");
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  it("loading, lỗi/thử lại và đổi đợt không hiển thị biểu đồ cũ", async () => {
    const fixture = await createSurveyApi(await clientFor()).report({});
    let release: (response: Response) => void = () => {};
    let mode: "pending" | "ok" = "pending";
    let pending = 0;
    auth.api = stub(async () => {
      if (mode === "pending") return new Promise<Response>((resolve) => { pending++; release = resolve; });
      return json(fixture);
    });
    render(<CostReportScreen />);
    expect(screen.getByRole("status")).toHaveTextContent("Đang tải dữ liệu");
    await waitFor(() => expect(pending).toBe(1));
    mode = "ok";
    release(json(fixture));
    expect(await screen.findByRole("img", { name: /Cơ cấu chi phí bình quân/ })).toBeInTheDocument();
    mode = "pending";
    fireEvent.change(screen.getByLabelText("Nhóm cây"), { target: { value: "nam" } });
    expect(await screen.findByRole("status")).toHaveTextContent("Đang tải dữ liệu");
    expect(screen.queryByRole("img", { name: /Cơ cấu chi phí bình quân/ })).not.toBeInTheDocument();
    await waitFor(() => expect(pending).toBe(2));
    release(json({ type: "about:blank", title: "Không tải được báo cáo", status: 503, code: "unavailable" }, 503));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    mode = "ok";
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(await screen.findByRole("img", { name: /Cơ cấu chi phí bình quân/ })).toBeInTheDocument();
  });

  it("không có đợt đã chốt và quyền xuất Cấp Tỉnh giữ nguyên", async () => {
    const fixture: SurveyCostReport = { rounds: [], main: null, compare: null, regions: [], items: [], communes: [], can_export: false };
    auth.api = stub(async () => json(fixture));
    const { unmount } = render(<CostReportScreen />);
    expect(await screen.findByText("Chưa có đợt điều tra đã chốt.")).toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    unmount();
    auth.api = await clientFor("kn-binh", "asg-binh-tinh");
    render(<CostReportScreen />);
    expect(await screen.findByRole("button", { name: "Xuất Excel" })).toBeEnabled();
  });

  it("so sánh hai đợt giữ hai cặp biểu đồ riêng và truyền cùng bộ lọc", async () => {
    const fixture = await createSurveyApi(await clientFor()).report({});
    const compare = structuredClone(fixture.main!);
    compare.round.id = "0190c0de-5000-7000-8000-0000000000b1";
    compare.round.name = "Thu Đông 2026";
    compare.rows[0]!.avg_costs.seed = "9000000";
    fixture.rounds.push(compare.round);
    const requests: URL[] = [];
    auth.api = stub(async (input) => {
      const url = new URL(input instanceof Request ? input.url : String(input));
      requests.push(url);
      return json({ ...fixture, compare: url.searchParams.get("compare") ? compare : null });
    });
    render(<CostReportScreen />);
    await screen.findByRole("img", { name: /Cơ cấu chi phí bình quân/ });
    fireEvent.change(screen.getByLabelText("So sánh với"), { target: { value: compare.round.id } });
    expect(await screen.findByRole("img", { name: "Cơ cấu chi phí bình quân — Thu Đông 2026" })).toHaveTextContent("9.000.000 đồng/phiếu");
    expect(screen.getAllByRole("img")).toHaveLength(4);
    fireEvent.change(screen.getByLabelText("Nhóm vùng"), { target: { value: "an_giang" } });
    await waitFor(() => expect(requests.at(-1)!.searchParams.get("region")).toBe("an_giang"));
    expect(requests.at(-1)!.searchParams.get("compare")).toBe(compare.round.id);
  });
});
