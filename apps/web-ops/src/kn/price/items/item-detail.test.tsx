import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { addDay, resetPriceItems, vnToday } from "@/api/mock/kn-price-items";
import { ItemsScreen } from "./ItemsScreen";
import { PriceItemDetailScreen } from "./ItemDetail";
import { createPriceItemApi } from "./api";
import { itemLog } from "./item-view";

const nav = vi.hoisted(() => ({ query: "", push: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/mat-hang", useSearchParams: () => new URLSearchParams(nav.query),
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
}));
const auth = vi.hoisted(() => ({ api: null as ApiClient | null, subject: "kn-binh", role: "kn_tinh", contextId: "" }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({
  api: auth.api, principal: { tenantId: "tenant198", subject: auth.subject },
  kn: { context: { role: auth.role, context_id: auth.contextId } },
}) }));
async function setupClient(persona = "kn-binh", assignment = "asg-binh-tinh", role = "kn_tinh") {
  const res = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
    method: "POST", headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `ctx198-${persona}-${Math.random()}` },
    body: JSON.stringify({ assignment_id: assignment, department_code: "trong_trot" }),
  }));
  expect(res.status).toBe(201);
  const context = await res.json() as { context_id: string };
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => context.context_id, fetch });
  auth.api = api; auth.subject = persona; auth.role = role; auth.contextId = context.context_id;
  return { fetch, pa: createPriceItemApi(api) };
}
const writes = (fetch: ReturnType<typeof vi.fn<typeof mockFetchNoDelay>>) => fetch.mock.calls.map((args) => args[0] as Request).filter((request) => request.method === "POST");
async function fillItem() {
  await userEvent.type(await screen.findByRole("textbox", { name: /Tên loại/ }), "Chanh");
  await userEvent.type(screen.getByRole("textbox", { name: /Giống \/ phẩm cấp/ }), "không hạt");
}
const journeyNow = new Date();
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(journeyNow);
  resetPriceItems(); nav.query = `at=${vnToday()}&tab=items`; nav.push.mockClear(); nav.replace.mockClear();
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("Price198 item/group routes", () => {
  it("list has STT and route links, retains current filters, opens no item modal", async () => {
    await setupClient();
    nav.query += "&group=lua_gao&kind=basic&q=Lúa";
    render(<ItemsScreen />);
    const table = await screen.findByRole("table", { name: "Danh mục mặt hàng" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Thêm mặt hàng" });
    expect(link).toHaveAttribute("href", expect.stringContaining("/mat-hang/them-moi?"));
    const href = new URL(link.getAttribute("href")!, "http://local");
    expect(href.searchParams.get("group")).toBe("lua_gao");
    expect(href.searchParams.get("q")).toBe("Lúa");
    expect(within(table).getAllByRole("link", { name: "Xem / Log" })[0]).toHaveAttribute("href", expect.stringContaining("/MH00001?"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("create validates required fields, saves existing API and preserves cancel URL", async () => {
    const { fetch, pa } = await setupClient();
    render(<PriceItemDetailScreen route={{ kind: "item-new" }} />);
    await screen.findByRole("textbox", { name: /Tên loại/ });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByText("Bắt buộc nhập.")).toHaveLength(2);
    expect(writes(fetch)).toHaveLength(0);
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", `/kn/bao-gia/mat-hang?at=${vnToday()}&tab=items`);
    await fillItem();
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("MH00009?")));
    expect((await pa.item("MH00009", addDay(vnToday(), 1))).name).toBe("Chanh");
    expect(writes(fetch)).toHaveLength(1);
  });
  it("detail renders actual ItemVersion actor/time/old→new and locked edit fields cannot change identity", async () => {
    const { pa } = await setupClient();
    await pa.reviseItem("MH00001", { name: "Lúa mới", variety: "OM 18 (tươi)", group_code: "lua_gao", unit_code: "kg", kind: "basic", spec: "", sale_place: "Chợ mới" });
    nav.query = `at=${addDay(vnToday(), 1)}`;
    const detail = await pa.item("MH00001", addDay(vnToday(), 1));
    const entries = itemLog(detail);
    expect(entries[0]!.at).toBe(detail.history.at(-1)!.event_time);
    expect(entries[0]!.actor_display).toContain(detail.history.at(-1)!.actor_ref);
    expect(entries[0]!.changes).toContainEqual({ field: "Tên loại", old: "Lúa", new: "Lúa mới" });
    const view = render(<PriceItemDetailScreen route={{ kind: "item", code: "MH00001", mode: "view" }} />);
    expect(await screen.findByRole("complementary", { name: "Log" })).toHaveTextContent("Mã tham chiếu");
    expect(screen.getByRole("table", { name: "Lịch sử phiên bản mặt hàng" })).toBeInTheDocument();
    view.rerender(<PriceItemDetailScreen route={{ kind: "item", code: "MH00001", mode: "edit" }} />);
    expect(await screen.findByRole("combobox", { name: /Đơn vị tính/ })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: /Giống \/ phẩm cấp/ })).toBeDisabled();
    expect(screen.getByRole("link", { name: "Tạo mặt hàng thay thế" })).toHaveAttribute("href", expect.stringContaining("MH00001/thay-the"));
  });
  it("group forms use routes and required validation; snapshot cannot invent a group Log", async () => {
    await setupClient(); nav.query = `at=${vnToday()}&tab=groups`;
    const view = render(<ItemsScreen />);
    const table = await screen.findByRole("table", { name: "Nhóm hàng" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Thêm nhóm hàng" })).toHaveAttribute("href", expect.stringContaining("nhom-hang/them-moi"));
    view.unmount();
    const form = render(<PriceItemDetailScreen route={{ kind: "group-new" }} />);
    await screen.findByRole("textbox", { name: /Tên nhóm/ });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByText("Bắt buộc nhập.")).toBeInTheDocument();
    await userEvent.type(screen.getByRole("textbox", { name: /Tên nhóm/ }), "Nhóm thử");
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("nhom-hang/")));
    form.rerender(<PriceItemDetailScreen route={{ kind: "group", code: "lua_gao", edit: false }} />);
    const log = await screen.findByRole("complementary", { name: "Log" });
    expect(log).toHaveTextContent("Chưa có lịch sử thay đổi nhóm hàng");
    expect(within(log).queryByRole("list")).not.toBeInTheDocument();
  });
  it("unknown item/group IDs and read-only direct write URLs expose no write operation", async () => {
    const client = await setupClient("kn-quan", "asg-quan-tw", "kn_tw");
    const view = render(<PriceItemDetailScreen route={{ kind: "item-new" }} />);
    expect(await screen.findByText(/Chỉ xem — ngữ cảnh/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    view.rerender(<PriceItemDetailScreen route={{ kind: "group-new" }} />);
    expect(await screen.findByText(/Chỉ xem — ngữ cảnh/)).toBeInTheDocument();
    expect(writes(client.fetch)).toHaveLength(0);
    view.rerender(<PriceItemDetailScreen route={{ kind: "item", code: "MH00001", mode: "view" }} />);
    expect(await screen.findByRole("table", { name: "Lịch sử phiên bản mặt hàng" })).toBeInTheDocument();
    expect(screen.queryByRole("complementary", { name: "Log" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Mã tham chiếu/)).not.toBeInTheDocument();
    view.rerender(<PriceItemDetailScreen route={{ kind: "item", code: "MH99999", mode: "edit" }} />);
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    view.rerender(<PriceItemDetailScreen route={{ kind: "group", code: "unknown", edit: true }} />);
    expect(await screen.findByText(/Không tìm thấy nhóm hàng/)).toBeInTheDocument();
    expect(writes(client.fetch)).toHaveLength(0);
  });
  it("ordinary Tỉnh can add and edit unpriced items but cannot open replacement controls", async () => {
    const { fetch } = await setupClient();
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const response = await mockFetchNoDelay(...args);
      const request = args[0] as Request;
      if (request.method === "GET" && new URL(request.url).pathname === "/v1/price/items") {
        const body = await response.json() as Record<string, unknown>;
        return new Response(JSON.stringify({ ...body, can_edit: true, can_steward: false }), { status: response.status, headers: response.headers });
      }
      return response;
    });
    const view = render(<PriceItemDetailScreen route={{ kind: "item-new" }} />);
    await screen.findByRole("textbox", { name: /Tên loại/ });
    expect(screen.queryByRole("textbox", { name: "Thay thế cho (mã)" })).not.toBeInTheDocument();
    await fillItem(); await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("MH00009?")));
    view.rerender(<PriceItemDetailScreen route={{ kind: "item", code: "MH00004", mode: "edit" }} />);
    expect(await screen.findByRole("button", { name: "Lưu" })).toBeInTheDocument();
    view.rerender(<PriceItemDetailScreen route={{ kind: "item", code: "MH00001", mode: "replace" }} />);
    expect(await screen.findByText(/Chỉ xem — ngữ cảnh/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Tạo mặt hàng thay thế" })).not.toBeInTheDocument();
    expect(writes(fetch)).toHaveLength(1);
  });
  it("lost ACK retries same key without duplicating a commodity", async () => {
    const { fetch, pa } = await setupClient();
    render(<PriceItemDetailScreen route={{ kind: "item-new" }} />); await fillItem();
    let lost = false;
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const response = await mockFetchNoDelay(...args);
      const request = args[0] as Request;
      if (!lost && request.method === "POST" && new URL(request.url).pathname === "/v1/price/items") { lost = true; throw new TypeError("lost committed response198"); }
      return response;
    });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(lost).toBe(true));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled());
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalled());
    const requests = writes(fetch);
    expect(requests).toHaveLength(2);
    expect(requests[0]!.headers.get("Idempotency-Key")).toBe(requests[1]!.headers.get("Idempotency-Key"));
    expect((await pa.items({ at: addDay(vnToday(), 1) })).items.filter((item) => item.name === "Chanh")).toHaveLength(1);
  });
  it("retire requires reason, records the effective event and preserves the earlier catalogue", async () => {
    const { fetch, pa } = await setupClient();
    render(<PriceItemDetailScreen route={{ kind: "item", code: "MH00004", mode: "retire" }} />);
    await screen.findByRole("textbox", { name: /Lý do/ });
    await userEvent.click(screen.getByRole("button", { name: "Ngưng" }));
    expect(screen.getByText("Bắt buộc nhập.")).toBeInTheDocument();
    expect(writes(fetch)).toHaveLength(0);
    await userEvent.type(screen.getByRole("textbox", { name: /Lý do/ }), "Ngưng kinh doanh");
    await userEvent.click(screen.getByRole("button", { name: "Ngưng" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalled());
    expect((await pa.item("MH00004", vnToday())).state).toBe("active");
    const retired = await pa.item("MH00004", addDay(vnToday(), 1));
    expect(retired.state).toBe("retired");
    expect(retired.history.at(-1)!.note).toBe("Ngưng kinh doanh");
  });
  it("replacement opens an editable new commodity with immutable source code and links old→new", async () => {
    const { pa } = await setupClient();
    const unit = (await pa.catalog()).units.find((entry) => entry.code !== "kg")!.code;
    render(<PriceItemDetailScreen route={{ kind: "item", code: "MH00001", mode: "replace" }} />);
    const input = await screen.findByRole("textbox", { name: "Thay thế cho (mã)" });
    expect(input).toHaveValue("MH00001"); expect(input).toBeDisabled();
    await userEvent.selectOptions(screen.getByRole("combobox", { name: /Đơn vị tính/ }), unit);
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("MH00009?")));
    const replacement = await pa.item("MH00009", addDay(vnToday(), 1));
    expect(replacement.unit_code).toBe(unit); expect(replacement.replaces_code).toBe("MH00001");
    expect(replacement.new_price).toBe(true);
    expect((await pa.item("MH00001", addDay(vnToday(), 1))).replaced_by).toEqual(["MH00009"]);
  });
  it("group rename keeps current order, persists a new version and never fabricates actor history", async () => {
    const { pa } = await setupClient();
    const old = (await pa.groups()).groups.find((group) => group.code === "lua_gao")!;
    render(<PriceItemDetailScreen route={{ kind: "group", code: "lua_gao", edit: true }} />);
    const name = await screen.findByRole("textbox", { name: /Tên nhóm/ });
    await userEvent.clear(name); await userEvent.type(name, "Lúa và gạo");
    expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Chưa có lịch sử thay đổi nhóm hàng");
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("nhom-hang/lua_gao?")));
    const renamed = (await pa.groups(addDay(vnToday(), 1))).groups.find((group) => group.code === "lua_gao")!;
    expect(renamed.name).toBe("Lúa và gạo"); expect(renamed.version).toBe(old.version + 1);
    expect(renamed.sort_order).toBe(old.sort_order);
  });
  it("delete confirmation keeps a server conflict visible inside the dialog", async () => {
    const { fetch, pa } = await setupClient();
    const group = await pa.createGroup({ name: "Nhóm để thử xóa198" });
    nav.query = `at=${group.valid_from}&tab=groups`;
    render(<ItemsScreen />);
    const table = await screen.findByRole("table", { name: "Nhóm hàng" });
    const row = within(table).getByRole("row", { name: /Nhóm để thử xóa198/ });
    await userEvent.click(within(row).getByRole("button", { name: "Xóa" }));
    const dialog = screen.getByRole("dialog", { name: `Xóa nhóm ${group.code}` });
    fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const request = args[0] as Request;
      if (request.method === "POST" && new URL(request.url).pathname === `/v1/price/groups/${group.code}/retire`)
        return new Response(JSON.stringify({ type: "about:blank", status: 409, title: "Nhóm còn mặt hàng", code: "price.group_in_use", detail: "Có mặt hàng vừa được thêm vào nhóm." }), { status: 409, headers: { "Content-Type": "application/problem+json" } });
      return mockFetchNoDelay(...args);
    });
    await userEvent.click(within(dialog).getByRole("button", { name: "Xác nhận xóa" }));
    expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: `Xóa nhóm ${group.code}` })).toBeInTheDocument();
  });
  it("context switch during a write discards old response/navigation and old editor values", async () => {
    const old = await setupClient();
    const view = render(<PriceItemDetailScreen route={{ kind: "item-new" }} />); await fillItem();
    let release: (() => void) | undefined;
    old.fetch.mockImplementation(async (...args: Parameters<typeof mockFetchNoDelay>) => {
      const response = await mockFetchNoDelay(...args);
      const request = args[0] as Request;
      if (request.method === "POST" && new URL(request.url).pathname === "/v1/price/items") await new Promise<void>((resolve) => { release = resolve; });
      return response;
    });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(release).toBeTypeOf("function"));
    const current = await setupClient("kn-quan", "asg-quan-tw", "kn_tw");
    view.rerender(<PriceItemDetailScreen route={{ kind: "item-new" }} />);
    expect(await screen.findByText(/Chỉ xem — ngữ cảnh/)).toBeInTheDocument();
    await act(async () => { release!(); });
    expect(nav.push).not.toHaveBeenCalled();
    expect(screen.queryByDisplayValue("Chanh")).not.toBeInTheDocument();
    expect(writes(current.fetch)).toHaveLength(0);
  });
});
