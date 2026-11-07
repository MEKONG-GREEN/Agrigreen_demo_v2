import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { HistoryScreen } from "./XaScreens";
import { vnToday, type PriceHistoryRow, type PriceSheet } from "./api";
import { historyFilters, historyHref, historyMonthDays, historyRangeProblem, shiftHistoryMonth } from "./history-routes";

const nav = vi.hoisted(() => ({ query: "", push: vi.fn() }));
const auth = vi.hoisted(() => ({ api: null as ApiClient | null, context: "ctx219" }));
vi.mock("next/navigation", () => ({ useSearchParams: () => new URLSearchParams(nav.query), useRouter: () => ({ push: nav.push }) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, principal: { tenantId: "tenant219", subject: "xa219" }, kn: { context: { context_id: auth.context, role: "kn_xa" } } }) }));
const row = (day: string, view: PriceHistoryRow["view"] = "locked", id = day): PriceHistoryRow => ({
  day, view, ticket: view === "not_entered" ? null : { id, status: view === "deleted" ? "deleted" : "submitted", source: "xa", version: 1 },
  point: { code: "XBG0001", name: "Xã của tôi" }, priced: 3, no_goods: 2, adjusted: 1, excluded: 0, high: 0, low: 0,
} as PriceHistoryRow);
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": status === 200 ? "application/json" : "application/problem+json" } });
function client(fetcher: (req: Request) => Promise<Response>) {
  auth.api = createApiClient({ baseUrl: "http://219", getAccessToken: () => "test", getContextId: () => auth.context, fetch: fetcher });
}
function mount(query = "from=2024-02-01&to=2024-03-31") {
  nav.query = query;
  const v = render(<HistoryScreen />);
  return { ...v, route: (q: string) => { nav.query = q.includes("?") ? q.split("?")[1]! : q; v.rerender(<HistoryScreen />); }, navigate: () => { nav.query = String(nav.push.mock.calls.at(-1)![0]).split("?")[1]!; v.rerender(<HistoryScreen />); } };
}
beforeEach(() => { vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2024-02-29T17:00:00Z")); nav.push.mockReset(); auth.context = "ctx219"; });
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("FN-XA-03.03 v2 calendar/list", () => {
  it("cho phép chuyển lịch tháng cạnh danh sách; lịch dùng đủ khoảng, không cắt trang đầu", async () => {
    const rows = Array.from({ length: 45 }, (_, i) => row(new Date(Date.UTC(2024, 1, 1 + i)).toISOString().slice(0, 10)));
    const fetcher = vi.fn<(req: Request) => Promise<Response>>(async () => response({ rows })); client(fetcher);
    const v = mount();
    expect(await screen.findAllByRole("button", { name: /^Xem phiếu/ })).toHaveLength(31);
    fireEvent.click(screen.getByRole("button", { name: "Lịch" })); v.navigate();
    expect(screen.getByRole("button", { name: "Lịch" })).toHaveAttribute("aria-pressed", "true");
    expect(within(screen.getByRole("region", { name: "Lịch báo giá tháng" })).getAllByRole("button", { name: /^Xem phiếu/ })).toHaveLength(16);
    expect(fetcher).toHaveBeenCalledTimes(1);
    const req = fetcher.mock.calls[0]![0] as Request;
    expect(req.url).toContain("from=2024-02-01&to=2024-03-31");
    fireEvent.click(screen.getByRole("button", { name: "Tháng trước" })); v.navigate();
    expect(screen.getAllByRole("button", { name: /^Xem phiếu/ })).toHaveLength(29);
    expect(screen.getByRole("button", { name: "Tháng trước" })).toBeDisabled();
    expect(screen.getByText("29/02/2024")).toBeInTheDocument();
  });
  it("lọc trạng thái giống nhau ở hai view, chọn/in và giữ chi tiết hiệu chỉnh, xác minh chỉ đọc", async () => {
    const states: PriceHistoryRow["view"][] = ["draft", "submitted", "locked", "late", "approved", "missed", "deleted", "not_entered"];
    const rows = states.map((s, i) => row(`2024-02-${String(i + 1).padStart(2, "0")}`, s));
    const detail = { point: rows[0]!.point, day: "2024-02-01", view: "draft", ticket: rows[0]!.ticket, lines: [{ item_code: "MH1", name: "Lúa", group_name: "Lúa gạo", variety: "", unit_name: "kg", min: "9000", max: "10000", adj_min: "9500", adj_max: "10500", alert: { view: "responded", history: [{ status: "requested", message: "Đề nghị kiểm tra" }, { status: "responded", explanation: "Đã xác minh" }] } }] } as unknown as PriceSheet;
    client(async req => response(req.url.includes("/history") ? { rows } : detail));
    const v = mount("from=2024-02-01&to=2024-02-29&view=calendar&month=2024-02");
    await screen.findByText("Bản nháp", { selector: "span" });
    expect(screen.getByLabelText(/Chọn phiếu ngày 06\/02/)).toBeDisabled();
    expect(screen.getByLabelText(/Chọn phiếu ngày 07\/02/)).toBeDisabled();
    fireEvent.click(screen.getByLabelText(/Chọn phiếu ngày 01\/02/));
    expect(screen.getByRole("link", { name: "In phiếu (1)" })).toHaveAttribute("href", "/kn/bao-gia/in?ticket=2024-02-01");
    fireEvent.click(screen.getByRole("button", { name: /Xem phiếu ngày 01\/02/ }));
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText("9.500–10.500")).toBeInTheDocument();
    expect(within(dlg).getByText(/Đã xác minh/)).toBeInTheDocument();
    expect(within(dlg).queryByRole("button", { name: /Lưu|Sửa|Gửi/ })).not.toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.change(screen.getByLabelText("Trạng thái"), { target: { value: "late" } }); v.navigate();
    expect(screen.getAllByRole("button", { name: /^Xem phiếu/ })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "In phiếu (0)" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Danh sách" })); v.navigate();
    expect(screen.getAllByRole("button", { name: /^Xem phiếu/ })).toHaveLength(1);
    expect(screen.getByLabelText("Trạng thái")).toHaveValue("late");
    expect(screen.getByText("5 (3 / 2)")).toBeInTheDocument();
  });
  it("chỉ dùng ngày/trạng thái API; không suy Không nộp cho ô trống, ngày nghỉ hoặc tương lai", async () => {
    client(async () => response({ rows: [row("2024-02-29", "missed"), row("2024-03-01", "not_entered")] }));
    const v = mount("from=2024-02-29&to=2024-03-03&view=calendar&month=2024-02");
    await screen.findByText("Không nộp", { selector: "span" });
    expect(screen.getByText("28/02/2024").parentElement).toHaveTextContent("Ngoài khoảng chọn");
    fireEvent.click(screen.getByRole("button", { name: "Tháng sau" })); v.navigate();
    expect(screen.queryByText("Không nộp", { selector: "span" })).not.toBeInTheDocument();
    expect(screen.getByText("02/03/2024").parentElement).not.toHaveTextContent("Chưa nhập");
    expect(screen.getByText("Chưa nhập", { selector: "span" })).toBeInTheDocument();
  });
  it("đổi phạm vi/ngữ cảnh gỡ dữ liệu, chọn in và detail cũ; bỏ response cũ đến muộn", async () => {
    let release!: (r: Response) => void;
    const old = new Promise<Response>(r => { release = r; });
    client(async req => req.headers.get("X-AG-Context") === "ctx219" ? old : response({ rows: [row("2024-02-02", "locked", "new-context")] }));
    const v = mount(); auth.context = "ctx-new219"; v.rerender(<HistoryScreen />);
    await screen.findByRole("button", { name: /Xem phiếu ngày 02\/02/ });
    await act(async () => { release(response({ rows: [row("2024-02-01", "locked", "old-context")] })); await old; });
    expect(screen.queryByRole("button", { name: /Xem phiếu ngày 01\/02/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/Chọn phiếu ngày 02\/02/));
    auth.context = "ctx-third219"; v.rerender(<HistoryScreen />);
    expect(screen.queryByRole("link", { name: "In phiếu (1)" })).not.toBeInTheDocument();
  });
  it("loading, API error/retry, empty và khoảng invalid không hiển thị dữ liệu cũ", async () => {
    let calls = 0; const fetcher = vi.fn(async () => ++calls === 1 ? response({ type: "about:blank", title: "Lỗi", detail: "Không đọc được lịch sử", status: 503, code: "unavailable" }, 503) : response({ rows: [] })); client(fetcher);
    const v = mount("from=2024-03-02&to=2024-02-01");
    expect(screen.getByRole("alert")).toHaveTextContent("Đến ngày"); expect(fetcher).not.toHaveBeenCalled();
    v.route("from=2024-02-01&to=2024-03-31");
    expect(screen.getByRole("status")).toHaveTextContent(/Đang/);
    await screen.findByText("Không đọc được lịch sử");
    fireEvent.click(screen.getByRole("button", { name: /Thử lại/ }));
    await screen.findByText("Không có phiếu phù hợp.");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it("URL deep-link/back giữ khoảng/trạng thái/tháng, và phím Space đổi view", async () => {
    client(async () => response({ rows: [] }));
    const v = mount("from=2024-02-15&to=2024-03-02&view=calendar&month=2024-02&status=approved&point=XBG9999");
    await screen.findByText("Không có phiếu phù hợp trong tháng.");
    expect(screen.getByLabelText("Từ ngày")).toHaveValue("2024-02-15");
    expect(screen.getByLabelText("Trạng thái")).toHaveValue("approved");
    const user = userEvent.setup(); const b = screen.getByRole("button", { name: "Danh sách" }); b.focus(); await user.keyboard(" ");
    expect(nav.push).toHaveBeenLastCalledWith(expect.stringContaining("point=XBG9999"), { scroll: false }); v.navigate();
    expect(screen.getByRole("button", { name: "Danh sách" })).toHaveAttribute("aria-pressed", "true");
    v.route("from=2024-02-15&to=2024-03-02&view=calendar&month=2024-02&status=approved");
    expect(screen.getByRole("heading", { name: "Tháng 02/2024" })).toBeInTheDocument();
  });
});
it("lịch Gregorian năm nhuận/qua năm, URL invalid và ngày VN UTC+7 không phụ thuộc timezone máy", () => {
  expect(vnToday()).toBe("2024-03-01");
  expect(historyMonthDays("2024-02").filter(Boolean)).toHaveLength(29);
  expect(historyMonthDays("2023-02").filter(Boolean)).toHaveLength(28);
  expect(historyMonthDays("2024-12").filter(Boolean)).toHaveLength(31);
  expect(shiftHistoryMonth("2024-12", 1)).toBe("2025-01");
  expect(shiftHistoryMonth("2024-01", -1)).toBe("2023-12");
  const f = historyFilters(new URLSearchParams("view=bad&month=bad&status=bad&from=2024-02-30"), vnToday());
  expect(f.view).toBe("list"); expect(f.status).toBe(""); expect(f.month).toBe("2024-03");
  expect(historyRangeProblem(f)).toContain("ngày hợp lệ");
  expect(historyRangeProblem({ from: "2023-01-01", to: "2024-03-01" })).toContain("12 tháng");
  const valid = historyFilters(new URLSearchParams("from=2024-02-01&to=2024-02-29&view=calendar&month=2024-03"), vnToday());
  expect(valid.month).toBe("2024-02"); expect(historyRangeProblem(valid)).toBe("");
  expect(historyFilters(new URLSearchParams(historyHref(valid).split("?")[1]), vnToday())).toEqual(valid);
});
