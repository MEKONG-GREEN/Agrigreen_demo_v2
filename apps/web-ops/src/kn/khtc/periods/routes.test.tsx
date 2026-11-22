import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { createKhtcPeriodApi, type KhtcPeriod } from "./api";
import { KhtcPeriodsScreen, PERIOD_BASE, periodHref, periodLog } from "./KhtcPeriodsScreen";
import { KhtcPeriodWorkspace } from "./KhtcPeriodWorkspace";
import { periodDateISO, validatePeriodForm } from "./form";
const nav = vi.hoisted(() => ({ path: "/kn/khtc/ky-bao-cao", push: vi.fn() }));
vi.mock("next/navigation", () => ({ usePathname: () => nav.path, useRouter: () => ({ push: nav.push }) }));
const auth: { api: ApiClient | null; context: { context_id: string; role: string; department_code: string; read_only: boolean } } = { api: null, context: { context_id: "test-context", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", read_only: false } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: auth.context } }) }));
let writeGate: Promise<void> | null = null;
let releaseWrite: (() => void) | undefined;
const writes: { path: string; body: Record<string, unknown> }[] = [];
const track: typeof mockFetchNoDelay = async request => {
  if (request.method === "POST" && new URL(request.url).pathname.startsWith("/v1/extension/khtc/periods")) writes.push({ path: new URL(request.url).pathname, body: await request.clone().json() });
  if (writeGate && request.method === "POST" && new URL(request.url).pathname === "/v1/extension/khtc/periods") { const gate = writeGate; writeGate = null; await gate; }
  return mockFetchNoDelay(request);
};
let period: KhtcPeriod;
beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-01T09:00:00Z"));
  nav.path = PERIOD_BASE; nav.push.mockReset(); writes.length = 0; writeGate = null; releaseWrite = undefined;
  auth.context = { context_id: "test-context", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", read_only: false };
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch: track });
  khtcMockStore().splice(0);
  period = await createKhtcPeriodApi(auth.api).open({ report_type: "month", year: 2030, no: 1, departments: ["trong_trot"] });
  writes.length = 0;
});
afterEach(() => { releaseWrite?.(); cleanup(); vi.useRealTimers(); });
function navigate(view: ReturnType<typeof render>, path: string) { nav.path = path; view.rerender(<KhtcPeriodWorkspace />); }

describe("Kỳ báo cáo — route UI-KIT", () => {
  it("danh sách tự đánh STT, mở chi tiết bằng liên kết riêng", async () => {
    render(<KhtcPeriodsScreen route={{ view: "list", go: nav.push }} />);
    const table = await screen.findByRole("table", { name: "Danh sách kỳ báo cáo" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: period.name })).toHaveAttribute("href", periodHref(period.id));
    fireEvent.click(screen.getByRole("button", { name: "Mở kỳ" }));
    expect(nav.push).toHaveBeenCalledWith(`${PERIOD_BASE}/them-moi`);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("pane kỳ nhúng không trùng tên nút chọn bản soạn báo cáo", async () => {
    render(<><KhtcPeriodsScreen /><button>{period.name}</button></>);
    const cell = await screen.findByRole("cell", { name: period.name });
    expect(screen.getAllByRole("button", { name: period.name })).toHaveLength(1);
    fireEvent.click(cell);
    expect(await screen.findByRole("heading", { level: 1, name: period.name })).toBeInTheDocument();
  });
  it("URL trực tiếp chi tiết có Log từ sự kiện thật và bảng tiến độ STT", async () => {
    nav.path = periodHref(period.id);
    render(<KhtcPeriodWorkspace />);
    expect(await screen.findByRole("heading", { name: period.name, level: 1 })).toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Mở kỳ và giao 1 phiếu");
    expect(screen.getByRole("link", { name: "Danh sách kỳ báo cáo" })).toHaveAttribute("href", PERIOD_BASE);
    expect(within(screen.getByRole("table", { name: "Tiến độ nộp phiếu và nguồn module" })).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Sửa hạn nộp" }));
    expect(nav.push).toHaveBeenCalledWith(`${periodHref(period.id)}/sua-han`);
    expect(periodLog(period)[0]).toMatchObject({ at: period.opened_at, actor_display: period.opened_by });
  });
  it("nháp mở kỳ giữ qua danh sách/chi tiết, được xóa khi đổi ngữ cảnh", async () => {
    nav.path = `${PERIOD_BASE}/them-moi`;
    const view = render(<KhtcPeriodWorkspace />);
    fireEvent.change(screen.getByRole("combobox", { name: "Loại báo cáo" }), { target: { value: "program" } });
    fireEvent.change(screen.getByLabelText("Đến ngày"), { target: { value: "2030-06-02" } });
    fireEvent.change(screen.getByLabelText("Hạn nộp"), { target: { value: "2030-06-03T10:00" } });
    navigate(view, PERIOD_BASE); await screen.findByRole("table", { name: "Danh sách kỳ báo cáo" });
    navigate(view, periodHref(period.id)); await screen.findByRole("heading", { name: period.name, level: 1 });
    navigate(view, `${PERIOD_BASE}/them-moi`);
    expect(screen.getByLabelText("Đến ngày")).toHaveValue("2030-06-02");
    expect(screen.getByLabelText("Hạn nộp")).toHaveValue("2030-06-03T10:00");
    auth.context = { ...auth.context, context_id: "another-context" };
    view.rerender(<KhtcPeriodWorkspace />);
    expect(screen.getByRole("combobox", { name: "Loại báo cáo" })).toHaveValue("week");
    expect(screen.getByLabelText("Hạn nộp")).toHaveValue("");
    expect(writes).toHaveLength(0);
  });
  it("bắt buộc và lỗi từng trường trước preview; không gọi API khi thiếu hạn Mẫu5", async () => {
    nav.path = `${PERIOD_BASE}/them-moi`; render(<KhtcPeriodWorkspace />);
    fireEvent.change(screen.getByRole("combobox", { name: "Loại báo cáo" }), { target: { value: "program" } });
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    expect(screen.getByLabelText("Đến ngày")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Hạn nộp")).toHaveAttribute("aria-invalid", "true");
    expect(writes).toHaveLength(0);
  });
  it("mở kỳ mới qua preview rồi ghi đúng kỳ/phòng và chuyển chi tiết", async () => {
    nav.path = `${PERIOD_BASE}/them-moi`; render(<KhtcPeriodWorkspace />);
    fireEvent.change(screen.getByRole("combobox", { name: "Loại báo cáo" }), { target: { value: "month" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Số kỳ" }), { target: { value: "12" } });
    expect(screen.queryByLabelText("Hạn xuất")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    const open = await screen.findByRole("button", { name: "Mở kỳ và giao phiếu" });
    fireEvent.click(open);
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(expect.stringMatching(/\/ky\//)));
    expect(writes.map(w => w.path)).toEqual(["/v1/extension/khtc/periods/preview", "/v1/extension/khtc/periods"]);
    expect(writes[1]!.body).toMatchObject({ report_type: "month", year: 2030, no: 12 });
  });
  it("sửa hạn có lỗi cục bộ, ghi giờ Việt Nam và Log API sau lưu", async () => {
    nav.path = `${periodHref(period.id)}/sua-han`; render(<KhtcPeriodWorkspace />);
    const input = await screen.findByRole("textbox", { name: "Hạn nộp" });
    fireEvent.change(input, { target: { value: "2030-02-30T10:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(input).toHaveAttribute("aria-invalid", "true"); expect(writes).toHaveLength(0);
    fireEvent.change(input, { target: { value: "2030-07-03T10:00" } });
    expect(screen.queryByLabelText("Hạn xuất")).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Lý do" }), { target: { value: "Điều chỉnh theo lịch phòng" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith(periodHref(period.id)));
    expect(writes[0]!.body).not.toHaveProperty("export_due_at");
    expect(writes[0]!.body).toMatchObject({ due_at: "2030-07-03T03:00:00.000Z", reason: "Điều chỉnh theo lịch phòng" });
    const saved = await createKhtcPeriodApi(auth.api!).get(period.id);
    expect(periodLog(saved).at(-1)?.event).toContain("Sửa hạn nộp");
    expect(saved.due_at).toBe("2030-07-03T03:00:00.000Z");
  });
  it("phản hồi mở kỳ về muộn không kéo người đã rời form về chi tiết", async () => {
    nav.path = `${PERIOD_BASE}/them-moi`; const view = render(<KhtcPeriodWorkspace />);
    fireEvent.change(screen.getByRole("combobox", { name: "Loại báo cáo" }), { target: { value: "month" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Số kỳ" }), { target: { value: "12" } });
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    const open = await screen.findByRole("button", { name: "Mở kỳ và giao phiếu" });
    writeGate = new Promise<void>(resolve => { releaseWrite = resolve; });
    fireEvent.click(open);
    expect(screen.getByRole("combobox", { name: "Loại báo cáo" })).toBeDisabled();
    navigate(view, PERIOD_BASE);
    releaseWrite!();
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Đã mở kỳ"));
    expect(nav.push).not.toHaveBeenCalled();
    expect(nav.path).toBe(PERIOD_BASE);
  });
  it("Hủy bản nháp bỏ dữ liệu, kỳ đã hủy không hiện Lưu dù vào URL sửa trực tiếp", async () => {
    nav.path = `${PERIOD_BASE}/them-moi`; const view = render(<KhtcPeriodWorkspace />);
    fireEvent.change(screen.getByRole("combobox", { name: "Loại báo cáo" }), { target: { value: "program" } });
    fireEvent.change(screen.getByLabelText("Đến ngày"), { target: { value: "2030-06-02" } });
    fireEvent.click(screen.getByRole("button", { name: "Hủy" }));
    navigate(view, PERIOD_BASE); navigate(view, `${PERIOD_BASE}/them-moi`);
    expect(screen.getByRole("combobox", { name: "Loại báo cáo" })).toHaveValue("week");
    const cancelled = await createKhtcPeriodApi(auth.api!).cancel(period.id, "Hủy kỳ mở nhầm để kiểm");
    writes.length = 0;
    navigate(view, `${periodHref(cancelled.id)}/sua-han`);
    await screen.findByText("Kỳ đã đóng hoặc hủy; không sửa hạn.");
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  });
  it.each([{ role: "kn_tinh", department_code: "trong_trot", read_only: false }, { role: "kn_tinh", department_code: "ke_hoach_tai_chinh", read_only: true }, { role: "kn_tw", department_code: "ke_hoach_tai_chinh", read_only: false }])("URL mở/sửa không cho ghi với $role/$department_code/$read_only", async context => {
    auth.context = { ...auth.context, ...context };
    nav.path = `${PERIOD_BASE}/them-moi`; const view = render(<KhtcPeriodWorkspace />);
    expect(screen.getByRole("alert")).toHaveTextContent("Chỉ xem");
    expect(screen.queryByRole("button", { name: "Xem trước" })).not.toBeInTheDocument();
    navigate(view, `${periodHref(period.id)}/sua-han`);
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  });
  it.each(["1e100000", "1e-100000", "9".repeat(100000), "2030-02-30T10:00"])("ngày giờ không hợp lệ bị chặn trước chuyển kiểu", raw => {
    expect(periodDateISO(raw)).toBeNull();
    expect(validatePeriodForm("program", { name: "", asOf: "2030-06-02", from: "", to: "", cutoff: "", due: raw, exportDue: "" })).toHaveProperty("due");
  });
});
