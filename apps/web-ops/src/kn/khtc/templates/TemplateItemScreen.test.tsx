import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import seeds from "@/api/mock/fixtures/khtc-template-seeds.json";
import { TemplateItemScreen } from "./TemplateItemScreen";
import { TEMPLATE_BASE } from "./template";

const nav = vi.hoisted(() => ({ params: { dept: "ke_hoach_tai_chinh", template: "mau_1", item: "" }, push: vi.fn() }));
const auth: { api: ApiClient | null; role: string; readOnly: boolean; context: string } = { api: null, role: "admin", readOnly: false, context: "ctx1" };
vi.mock("next/navigation", () => ({ useParams: () => nav.params, usePathname: () => TEMPLATE_BASE, useRouter: () => ({ push: nav.push }) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { context_id: auth.context, role: auth.role, read_only: auth.readOnly }, support: null } }) }));
const writes: Request[] = []; let failOnce = false;
let writeGate: Promise<void> | null = null; let releaseWrite: (() => void) | undefined;
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": status >= 400 ? "application/problem+json" : "application/json" } });
beforeEach(() => {
  writes.length = 0; failOnce = false; nav.push.mockReset(); auth.role = "admin"; auth.readOnly = false; auth.context = "ctx1";
  writeGate = null; releaseWrite = undefined;
  auth.api = createApiClient({ baseUrl: "http://template.test", getAccessToken: () => "token", getContextId: () => auth.context, fetch: async (request) => {
    if (request.method === "GET") return new URL(request.url).pathname.endsWith("/periods") ? json({ items: [] }) : json(seeds[0]);
    writes.push(request.clone());
    if (writeGate) { const gate = writeGate; writeGate = null; await gate; }
    if (failOnce) { failOnce = false; return json({ code: "internal_error", title: "Phản hồi bị mất, thử lại", status: 503 }, 503); }
    return json({ ...seeds[0], id: "version-id", key: "mau_1", version: 1, action: "save", actor: "actor", actor_display: "Admin", at: "2026-10-01T00:00:00Z" }, 201);
  } });
});
afterEach(() => { cleanup(); releaseWrite?.(); vi.restoreAllMocks(); });
const save = () => fireEvent.click(screen.getAllByRole("button", { name: "Lưu bản nháp" })[0]!);
async function fill() {
  await screen.findByRole("textbox", { name: /Tên mục/ });
  fireEvent.change(screen.getByRole("combobox", { name: /Mục cha/ }), { target: { value: "B" } });
  fireEvent.change(screen.getByRole("textbox", { name: /Tên mục/ }), { target: { value: "Mục con kiểm thử" } });
  fireEvent.change(screen.getByRole("textbox", { name: /Khuôn câu/ }), { target: { value: "Nội dung kiểm thử" } });
}
describe("Trang cấu hình mục mẫu", () => {
  it("bắt buộc và số quá dài không gọi API", async () => {
    render(<TemplateItemScreen create />); await screen.findByRole("textbox", { name: /Tên mục/ }); save(); expect(writes).toHaveLength(0);
    await fill(); fireEvent.change(screen.getByRole("textbox", { name: /Thứ tự/ }), { target: { value: "9".repeat(100000) } }); save(); expect(writes).toHaveLength(0); expect(screen.getByText(/không dùng số mũ hoặc chuỗi dài/)).toBeTruthy();
  });
  it("mất phản hồi rồi thử lại giữ một khóa thao tác", async () => {
    failOnce = true; render(<TemplateItemScreen create />); await fill(); save(); await screen.findByRole("alert"); save(); await waitFor(() => expect(writes).toHaveLength(2));
    expect(writes[0]!.headers.get("Idempotency-Key")).toBe(writes[1]!.headers.get("Idempotency-Key")); await waitFor(() => expect(nav.push).toHaveBeenCalled());
  });
  it("đổi nội dung sau lỗi thì dùng khóa mới", async () => {
    failOnce = true; render(<TemplateItemScreen create />); await fill(); save(); await screen.findByRole("alert");
    fireEvent.change(screen.getByRole("textbox", { name: /Khuôn câu/ }), { target: { value: "Nội dung khác" } }); save(); await waitFor(() => expect(writes).toHaveLength(2));
    expect(writes[0]!.headers.get("Idempotency-Key")).not.toBe(writes[1]!.headers.get("Idempotency-Key"));
  });
  it("đổi ngữ cảnh xóa form cũ và không mang khóa sang phiên khác", async () => {
    failOnce = true; const view = render(<TemplateItemScreen create />); await fill(); save(); await screen.findByRole("alert");
    auth.context = "ctx2"; view.rerender(<TemplateItemScreen create />); await waitFor(() => expect((screen.getByRole("textbox", { name: /Tên mục/ }) as HTMLInputElement).value).toBe(""));
    await fill(); save(); await waitFor(() => expect(writes).toHaveLength(2)); expect(writes[1]!.headers.get("X-AG-Context")).toBe("ctx2"); expect(writes[0]!.headers.get("Idempotency-Key")).not.toBe(writes[1]!.headers.get("Idempotency-Key"));
  });
  it.each(["kn_tinh", "kn_xa", "kn_tw"])("vai %s không có thao tác ghi", (role) => { auth.role = role; render(<TemplateItemScreen create />); expect(screen.queryByRole("button", { name: "Lưu bản nháp" })).toBeNull(); });
  it("phản hồi lưu cũ về muộn không điều hướng phiên mới", async () => {
    writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
    const view = render(<TemplateItemScreen create />); await fill(); save(); await waitFor(() => expect(writes).toHaveLength(1));
    auth.context = "ctx2"; view.rerender(<TemplateItemScreen create />); await waitFor(() => expect((screen.getByRole("textbox", { name: /Tên mục/ }) as HTMLInputElement).value).toBe(""));
    await act(async () => { releaseWrite?.(); await new Promise((resolve) => setTimeout(resolve, 30)); });
    expect(nav.push).not.toHaveBeenCalled();
  });
  it("Hủy có xác nhận khi chưa lưu", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false); render(<TemplateItemScreen create />); await fill(); fireEvent.click(screen.getByRole("button", { name: "Hủy" })); expect(confirm).toHaveBeenCalled(); expect(nav.push).not.toHaveBeenCalled();
    confirm.mockReturnValue(true); fireEvent.click(screen.getByRole("button", { name: "Hủy" })); expect(nav.push).toHaveBeenCalled();
  });
});
