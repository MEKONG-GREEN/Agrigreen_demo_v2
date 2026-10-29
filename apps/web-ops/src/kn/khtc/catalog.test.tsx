import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKhtcMock } from "@/api/mock/kn-khtc";
import { Decimal } from "@/lib/exactDecimal";
import { CATALOG_KINDS, createKhtcApi, type KhtcCatalogKind } from "./api";
import { CATALOG_BASE, catalogCreateHref, catalogEditHref, catalogItemHref, catalogListHref, catalogLog, catalogValues, validateCatalogValues } from "./catalog";
import { KhtcCatalogScreen } from "./CatalogScreen";
import { KhtcCatalogCreateScreen, KhtcCatalogDetailScreen, KhtcCatalogEditScreen } from "./CatalogItemScreens";

const nav = vi.hoisted(() => ({ params: { kind: "unit", code: "ha" }, query: "", push: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => nav.params, useSearchParams: () => new URLSearchParams(nav.query), usePathname: () => CATALOG_BASE, useRouter: () => ({ push: nav.push, replace: nav.replace }) }));
const auth: { api: ApiClient | null; role: string; readOnly: boolean } = { api: null, role: "admin", readOnly: false };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role, department_code: "ke_hoach_tai_chinh", read_only: auth.readOnly } } }) }));

const writes: { request: Request; body: Record<string, unknown> }[] = [];
let failOnce = false;
let writeGate: Promise<void> | null = null;
let releaseWrite: (() => void) | undefined;
const trackedFetch: typeof mockFetchNoDelay = async (request) => {
  if (request.method === "POST" && new URL(request.url).pathname.startsWith("/v1/kn/khtc/catalogs/")) {
    writes.push({ request: request.clone(), body: await request.clone().json() });
    if (writeGate) {
      const gate = writeGate;
      writeGate = null;
      await gate;
    }
    if (failOnce) {
      failOnce = false;
      return new Response(JSON.stringify({ code: "internal_error", title: "Thử lại", status: 503 }), { status: 503, headers: { "Content-Type": "application/problem+json" } });
    }
  }
  return mockFetchNoDelay(request);
};

beforeEach(async () => {
  vi.restoreAllMocks();
  nav.params = { kind: "unit", code: "ha" }; nav.query = ""; nav.push.mockReset(); nav.replace.mockReset();
  auth.role = "admin"; auth.readOnly = false; writes.length = 0; failOnce = false; writeGate = null; releaseWrite = undefined;
  resetKhtcMock();
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
  expect(response.status).toBe(201);
  const context = await response.json() as { context_id: string };
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch: trackedFetch });
});
afterEach(() => { cleanup(); releaseWrite?.(); });

async function fillNew(code = "ui-test", name = "Đơn vị kiểm thử") {
  await userEvent.setup().type(screen.getByRole("textbox", { name: "Mã" }), code);
  await userEvent.setup().type(screen.getByRole("textbox", { name: "Tên" }), name);
}

describe("Danh mục KHTC — route, kiểm nhập và quyền", () => {
  it("bảy loại, DataTable tự đánh STT; Thêm/Xem/Sửa là liên kết trang riêng, giữ loại và bộ lọc", async () => {
    render(<KhtcCatalogScreen />);
    const table = await screen.findByRole("table", { name: "Đơn vị tính" });
    expect(screen.getAllByRole("tab")).toHaveLength(7);
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Thêm" })).toHaveAttribute("href", catalogCreateHref("unit"));
    expect(within(table).getByRole("link", { name: "Xem Héc ta" })).toHaveAttribute("href", catalogItemHref("unit", "ha"));
    expect(within(table).getByRole("link", { name: "Sửa Héc ta" })).toHaveAttribute("href", catalogEditHref("unit", "ha"));
    await userEvent.setup().click(screen.getByRole("tab", { name: /Người ký/ }));
    expect(nav.replace).toHaveBeenCalledWith(catalogListHref("signer"), { scroll: false });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("thiếu trường có dấu bắt buộc: lỗi từng trường, không gọi API; Hủy về đúng danh sách", async () => {
    nav.params.kind = "signer"; nav.query = "retired=1";
    render(<KhtcCatalogCreateScreen />);
    expect(screen.getByRole("textbox", { name: "Mã" })).toHaveAttribute("aria-required", "true");
    expect(screen.getByRole("textbox", { name: "Họ tên" })).toHaveAttribute("aria-required", "true");
    expect(screen.getByRole("textbox", { name: "Chức vụ" })).toHaveAttribute("aria-required", "true");
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByText("Bắt buộc nhập.")).toHaveLength(3);
    expect(writes).toHaveLength(0);
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", catalogListHref("signer", true));
    expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Chưa lưu mục danh mục.");
  });

  it("tạo mục bằng API sẵn có, chuẩn hóa mã và thứ tự; mã trùng tên route vẫn mở đúng chi tiết", async () => {
    render(<KhtcCatalogCreateScreen />);
    await fillNew("THEM-MOI");
    fireEvent.change(screen.getByRole("textbox", { name: "Thứ tự" }), { target: { value: "12" } });
    await userEvent.setup().type(screen.getByRole("textbox", { name: "Ký hiệu" }), "đv");
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(catalogItemHref("unit", "them-moi")));
    expect(writes).toHaveLength(1);
    expect(writes[0]!.body).toMatchObject({ code: "them-moi", name: "Đơn vị kiểm thử", sort_order: 12, attrs: { symbol: "đv" }, active: true });
    expect(catalogItemHref("unit", "them-moi")).toContain("/unit/muc/them-moi");
  });

  it.each([".", "..", "_a", "-a"])("mã %s không đúng quy tắc máy chủ bị chặn trước ghi", async (code) => {
    render(<KhtcCatalogCreateScreen />);
    await fillNew(code);
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("textbox", { name: "Mã" })).toHaveAttribute("aria-invalid", "true");
    expect(writes).toHaveLength(0);
  });

  it("mã đã tồn tại: lỗi máy chủ gắn lại vào ô Mã", async () => {
    render(<KhtcCatalogCreateScreen />);
    await fillNew("HA");
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Mã" })).toHaveAttribute("aria-invalid", "true"));
    expect(writes).toHaveLength(1);
    expect(nav.push).not.toHaveBeenCalled();
  });

  it.each([{ label: "số mũ dương", raw: "1e100000" }, { label: "số mũ âm", raw: "1e-100000" }, { label: "100000 chữ số", raw: "9".repeat(100000) }])("chặn $label trước parser, cả blur và Lưu không gửi dữ liệu thay thế", async ({ raw }) => {
    const parser = vi.spyOn(Decimal, "parse");
    render(<KhtcCatalogCreateScreen />);
    await fillNew();
    const input = screen.getByRole("textbox", { name: "Thứ tự" });
    fireEvent.change(input, { target: { value: raw } }); fireEvent.blur(input);
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(writes).toHaveLength(0);
    expect(parser).not.toHaveBeenCalledWith(raw);
    // Sau giá trị bị từ chối, sửa lại ô thì được Lưu (không âm thầm dùng số cũ).
    fireEvent.change(input, { target: { value: "1" } });
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body.sort_order).toBe(1);
  });

  it.each(["-1", "1,5", "100001"])("số sai biên/kiểu %s báo tại trường, không ghi", async (raw) => {
    render(<KhtcCatalogCreateScreen />); await fillNew();
    fireEvent.change(screen.getByRole("textbox", { name: "Thứ tự" }), { target: { value: raw } });
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("textbox", { name: "Thứ tự" })).toHaveAttribute("aria-invalid", "true");
    expect(writes).toHaveLength(0);
  });

  it("thử lại sau lỗi tạm dùng cùng Idempotency-Key", async () => {
    failOnce = true;
    render(<KhtcCatalogCreateScreen />); await fillNew();
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await screen.findByText("internal_error");
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalled());
    expect(writes).toHaveLength(2);
    expect(writes[0]!.request.headers.get("Idempotency-Key")).toBeTruthy();
    expect(writes[0]!.request.headers.get("Idempotency-Key")).toBe(writes[1]!.request.headers.get("Idempotency-Key"));
    expect((await createKhtcApi(auth.api!).catalogHistory("unit", "ui-test")).items).toHaveLength(1);
  });

  it("đã rời trang thì phản hồi Lưu đến muộn không kéo người dùng trở lại", async () => {
    writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
    render(<KhtcCatalogCreateScreen />); await fillNew();
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    cleanup(); releaseWrite!();
    await waitFor(async () => expect((await createKhtcApi(auth.api!).catalogHistory("unit", "ui-test")).items).toHaveLength(1));
    expect(nav.push).not.toHaveBeenCalled();
  });

  it("sửa trên trang riêng; chi tiết và Log đọc lịch sử thật, giữ cũ → mới/người/thời điểm", async () => {
    render(<KhtcCatalogEditScreen />);
    const name = await screen.findByRole("textbox", { name: "Tên" });
    expect(screen.getByRole("textbox", { name: "Mã" })).toBeDisabled();
    fireEvent.change(name, { target: { value: "Héc ta mới" } });
    await userEvent.setup().click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(catalogItemHref("unit", "ha")));
    cleanup(); render(<KhtcCatalogDetailScreen />);
    await screen.findByRole("heading", { level: 1, name: /^Héc ta mới/ });
    const log = screen.getByRole("complementary", { name: "Log" });
    expect(log).toHaveTextContent("Héc ta → Héc ta mới");
    const history = (await createKhtcApi(auth.api!).catalogHistory("unit", "ha")).items;
    expect(log).toHaveTextContent(history[0]!.actor.display!);
    expect(log).toHaveTextContent("v2");
    expect(screen.getByRole("link", { name: "Sửa" })).toHaveAttribute("href", catalogEditHref("unit", "ha"));
  });

  it("ngừng dùng/dùng lại vẫn ghi phiên bản, hiển thị Log, không xóa danh mục", async () => {
    render(<KhtcCatalogDetailScreen />);
    await userEvent.setup().click(await screen.findByRole("button", { name: "Ngừng dùng" }));
    await screen.findByRole("button", { name: "Dùng lại" });
    expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Ngừng dùng · v2");
    await userEvent.setup().click(screen.getByRole("button", { name: "Dùng lại" }));
    await waitFor(() => expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Dùng lại · v3"));
    expect(writes.map((x) => x.body.active)).toEqual([false, true]);
  });

  it.each([{ role: "kn_tinh", readOnly: false }, { role: "admin", readOnly: true }])("$role / readOnly=$readOnly không mở form ghi hoặc thấy hành động sửa", async ({ role, readOnly }) => {
    auth.role = role; auth.readOnly = readOnly;
    render(<KhtcCatalogCreateScreen />); expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    cleanup(); render(<KhtcCatalogEditScreen />); expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    cleanup(); render(<KhtcCatalogDetailScreen />); await screen.findByRole("heading", { level: 1, name: /^Héc ta/ });
    expect(screen.queryByRole("link", { name: "Sửa" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ngừng dùng" })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  });

  it("loại không hợp lệ không mở form; mục không tồn tại hiển thị lỗi API", async () => {
    nav.params.kind = "unknown"; render(<KhtcCatalogCreateScreen />);
    expect(screen.getByText("Không có loại danh mục này.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    cleanup(); nav.params = { kind: "unit", code: "missing" }; render(<KhtcCatalogEditScreen />);
    await screen.findByText("extension.khtc_catalog_not_found");
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
  });
});

it.each(CATALOG_KINDS)("%s giữ đúng trường và hợp đồng danh mục", (kind: KhtcCatalogKind) => {
  const values = { ...catalogValues(kind), code: "NEW_1", name: "Tên danh mục", ...(kind === "signer" ? { "attrs.title": "Giám đốc" } : {}) };
  const result = validateCatalogValues(kind, values, [], true);
  expect(result.errors).toEqual({}); expect(result.body).toMatchObject({ code: "new_1", name: "Tên danh mục", sort_order: 0, active: true });
  expect(catalogCreateHref(kind)).toContain(`/${kind}/them-moi`);
});

it("history helper giữ nguyên phiên bản đầu vào và không tạo Log giả", async () => {
  const history = (await createKhtcApi(auth.api!).catalogHistory("unit", "ha")).items;
  const original = JSON.stringify(history);
  expect(catalogLog(history)[0]!.at).toBe(history[0]!.recorded_at);
  expect(JSON.stringify(history)).toBe(original); expect(catalogLog([])).toEqual([]);
});
