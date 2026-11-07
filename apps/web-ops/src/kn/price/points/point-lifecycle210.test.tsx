import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { PricePointDetailScreen } from "./PointDetail";
import { PointsScreen } from "./PointsScreen";
import { createPricePointApi } from "./api";
import { parsePointRoute } from "./point-routes";

const nav = vi.hoisted(() => ({ query: "at=2026-11-06&region=an_giang", push: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/xa-bao-gia",
  useSearchParams: () => new URLSearchParams(nav.query),
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
}));
const auth = vi.hoisted(() => ({ api: null as ApiClient | null, subject: "kn-admin", contextId: "" }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({
  api: auth.api,
  principal: { tenantId: "tenant210", subject: auth.subject },
  kn: { context: { context_id: auth.contextId } },
}) }));

async function setupClient(persona = "kn-admin", assignment = "asg-admin") {
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
    method: "POST",
    headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `ctx210-${persona}-${crypto.randomUUID()}` },
    body: JSON.stringify({ assignment_id: assignment, department_code: "trong_trot" }),
  }));
  const context = await response.json() as { context_id: string };
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => context.context_id, fetch });
  auth.api = api;
  auth.subject = persona;
  auth.contextId = context.context_id;
  return { api, fetch, pa: createPricePointApi(api) };
}
function retireWrites(fetch: ReturnType<typeof vi.fn>) {
  return fetch.mock.calls.map(([request]) => request as Request).filter(request => request.method === "POST" && /\/v1\/price\/points\/[^/]+\/retire$/.test(new URL(request.url).pathname));
}
const route = { kind: "point", code: "XBG0001", mode: "view" } as const;

beforeEach(() => {
  vi.setSystemTime(new Date("2026-11-06T02:00:00Z"));
  resetPricePoints();
  nav.query = "at=2026-11-06&region=an_giang";
  nav.push.mockReset();
  nav.replace.mockReset();
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("FN-AD-04.02 — vòng đời xã báo giá210", () => {
  it("chi tiết Admin ngưng theo ngày hiệu lực, giữ lịch sử/bộ lọc và chỉ gửi một lệnh", async () => {
    const { pa, fetch } = await setupClient();
    render(<PricePointDetailScreen route={route} />);
    await userEvent.click(await screen.findByRole("button", { name: "Ngưng" }));
    const dialog = await screen.findByRole("dialog", { name: /Ngưng xã XBG0001/ });
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận ngưng" }));
    expect(await within(dialog).findByText("Bắt buộc nhập.")).toBeInTheDocument();
    expect(retireWrites(fetch)).toHaveLength(0);
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Lý do ngưng" }), "  Ngưng thu thập tại xã  ");
    fireEvent.change(within(dialog).getByLabelText("Ngưng từ ngày"), { target: { value: "2026-11-07" } });
    let release: (() => void) | undefined;
    let submitted: unknown;
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const request = args[0] as Request;
      const retiring = new URL(request.url).pathname.endsWith("/retire");
      if (retiring) submitted = await request.clone().json();
      const response = await mockFetchNoDelay(...args);
      if (retiring) await new Promise<void>(resolve => { release = resolve; });
      return response;
    });
    const confirm = within(dialog).getByRole("button", { name: "Xác nhận ngưng" });
    act(() => { confirm.click(); confirm.click(); });
    await waitFor(() => expect(release).toBeTypeOf("function"));
    expect(retireWrites(fetch)).toHaveLength(1);
    await act(async () => { release!(); });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(submitted).toMatchObject({ reason: "Ngưng thu thập tại xã", valid_from: "2026-11-07" });
    expect((await pa.point("XBG0001", "2026-11-06")).point.state).toBe("active");
    expect((await pa.point("XBG0001", "2026-11-07")).point.state).toBe("retired");
    expect((await pa.point("XBG0001", "2026-11-07")).versions.filter(event => event.status === "retired")).toHaveLength(1);
    expect(screen.getByRole("link", { name: /Danh mục xã báo giá/ })).toHaveAttribute("href", "/kn/bao-gia/xa-bao-gia?at=2026-11-06&tab=points&region=an_giang");
    expect(nav.push).not.toHaveBeenCalled();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("BR03: cán bộ chỉ hiển thị, danh sách/chi tiết không Phân công và URL cũ không là route", async () => {
    const { fetch } = await setupClient();
    const view = render(<PointsScreen />);
    await screen.findByRole("table", { name: "Danh mục xã báo giá" });
    expect(screen.queryAllByRole("link", { name: "Phân công" })).toHaveLength(0);
    view.unmount();
    render(<PricePointDetailScreen route={route} />);
    await screen.findByRole("heading", { name: /Xã XBG0001/ });
    expect(screen.queryAllByRole("link", { name: "Phân công" })).toHaveLength(0);
    expect(screen.getByRole("table", { name: "Lịch sử phân công" })).toBeInTheDocument();
    expect(parsePointRoute(["XBG0001", "phan-cong"])).toBeNull();
    expect(fetch.mock.calls.map(([request]) => request as Request).filter(request => request.method === "POST")).toHaveLength(0);
  });

  it("Cấp Tỉnh và xã đã ngưng không có thao tác Ngưng ở chi tiết", async () => {
    await setupClient("kn-binh", "asg-binh-tinh");
    const view = render(<PricePointDetailScreen route={route} />);
    await screen.findByRole("heading", { name: /Xã XBG0001/ });
    expect(screen.queryByRole("button", { name: "Ngưng" })).not.toBeInTheDocument();
    view.unmount();
    const { pa } = await setupClient();
    await pa.retirePoint("XBG0001", { reason: "Ngưng theo danh mục", valid_from: "2026-11-07" });
    nav.query = "at=2026-11-07&region=an_giang";
    render(<PricePointDetailScreen route={route} />);
    await screen.findByRole("heading", { name: /Xã XBG0001/ });
    expect(screen.queryByRole("button", { name: "Ngưng" })).not.toBeInTheDocument();
  });

  it("mất phản hồi Ngưng sau commit: thử lại giữ khóa và không thêm phiên bản lần hai", async () => {
    const { pa, fetch } = await setupClient();
    render(<PricePointDetailScreen route={route} />);
    await userEvent.click(await screen.findByRole("button", { name: "Ngưng" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Lý do ngưng" }), "Ngưng theo danh mục xã");
    let lost = false;
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const response = await mockFetchNoDelay(...args);
      if (!lost && new URL((args[0] as Request).url).pathname.endsWith("/retire")) {
        lost = true;
        throw new TypeError("lost committed retirement response210");
      }
      return response;
    });
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận ngưng" }));
    await waitFor(() => expect(lost).toBe(true));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Xác nhận ngưng" })).toBeEnabled());
    expect(within(dialog).getByRole("textbox", { name: "Lý do ngưng" })).toHaveValue("Ngưng theo danh mục xã");
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận ngưng" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const writes = retireWrites(fetch);
    expect(writes).toHaveLength(2);
    expect(writes[0]!.headers.get("Idempotency-Key")).toBe(writes[1]!.headers.get("Idempotency-Key"));
    expect((await pa.point("XBG0001", "2026-11-07")).versions.filter(event => event.status === "retired")).toHaveLength(1);
  });

  it("đóng hộp Ngưng cũ rồi mở xã khác: phản hồi cũ không đóng hộp mới", async () => {
    const { fetch } = await setupClient();
    render(<PointsScreen />);
    const table = await screen.findByRole("table", { name: "Danh mục xã báo giá" });
    await userEvent.click(within(within(table).getByRole("row", { name: /XBG0001/ })).getByRole("button", { name: "Ngưng" }));
    const oldDialog = await screen.findByRole("dialog", { name: /XBG0001/ });
    await userEvent.type(within(oldDialog).getByRole("textbox", { name: "Lý do ngưng" }), "Ngưng xã thứ nhất");
    let release: (() => void) | undefined;
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const response = await mockFetchNoDelay(...args);
      if (new URL((args[0] as Request).url).pathname.endsWith("/retire")) await new Promise<void>(resolve => { release = resolve; });
      return response;
    });
    await userEvent.click(within(oldDialog).getByRole("button", { name: "Xác nhận ngưng" }));
    await waitFor(() => expect(release).toBeTypeOf("function"));
    await userEvent.click(within(oldDialog).getByRole("button", { name: "Đóng" }));
    await userEvent.click(within(within(table).getByRole("row", { name: /XBG0002/ })).getByRole("button", { name: "Ngưng" }));
    await screen.findByRole("dialog", { name: /XBG0002/ });
    await act(async () => { release!(); });
    expect(screen.getByRole("dialog", { name: /XBG0002/ })).toBeInTheDocument();
    expect(retireWrites(fetch)).toHaveLength(1);
  });

  it("ngày đã qua: hiện lỗi server ở ô ngày, giữ lý do rồi cho sửa và gửi lại", async () => {
    const { pa } = await setupClient();
    render(<PricePointDetailScreen route={route} />);
    await userEvent.click(await screen.findByRole("button", { name: "Ngưng" }));
    const dialog = await screen.findByRole("dialog");
    const reason = within(dialog).getByRole("textbox", { name: "Lý do ngưng" });
    const day = within(dialog).getByLabelText("Ngưng từ ngày");
    await userEvent.type(reason, "Ngưng có ngày hiệu lực");
    fireEvent.change(day, { target: { value: "2026-11-05" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận ngưng" }));
    await waitFor(() => expect(day).toHaveAttribute("aria-invalid", "true"));
    expect(reason).toHaveValue("Ngưng có ngày hiệu lực");
    expect((await pa.point("XBG0001", "2026-11-07")).versions.filter(event => event.status === "retired")).toHaveLength(0);
    fireEvent.change(day, { target: { value: "2026-11-07" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận ngưng" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect((await pa.point("XBG0001", "2026-11-07")).point.state).toBe("retired");
  });

  it("Lưu xã không phát thông báo thành công, vẫn lưu và điều hướng đúng bản ghi", async () => {
    const { pa } = await setupClient();
    render(<PricePointDetailScreen route={{ kind: "point-new" }} />);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: /ĐVHC \(xã\)/ }), "31126");
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("/XBG0004?at=2026-11-06")));
    expect((await pa.point("XBG0004", "2026-11-07")).point.admin_unit_code).toBe("31126");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
