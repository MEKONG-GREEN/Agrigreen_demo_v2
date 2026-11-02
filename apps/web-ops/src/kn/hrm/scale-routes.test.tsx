import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { HrmCatalogScreen } from "./CatalogScreen";
import { HrmScaleScreen } from "./ScaleScreens";
import { createHrmApi } from "./api";

const route = vi.hoisted(() => ({ params: { grade: "01.003" }, path: "/kn/nhan-su/danh-muc", query: "at=2026-10-12", push: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => route.params, usePathname: () => route.path, useSearchParams: () => new URLSearchParams(route.query), useRouter: () => ({ push: route.push, replace: route.replace }) }));
const auth: { api: ApiClient | null; context: string } = { api: null, context: "admin" };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { context_id: auth.context } } }) }));

async function setup() {
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": "scale179-context" }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: "trong_trot" }) }));
  const { context_id } = await response.json() as { context_id: string };
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context_id, fetch: mockFetchNoDelay });
}

afterEach(() => vi.useRealTimers());
beforeEach(() => { vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2026-10-12T03:00:00Z")); resetHrm(); route.params.grade = "01.003"; route.path = "/kn/nhan-su/danh-muc"; route.query = "at=2026-10-12"; auth.context = "admin"; route.push.mockClear(); route.replace.mockClear(); });

describe("FN19 bảng hệ số trên route", () => {
  it("danh sách có STT và bảng mới/chi tiết dùng link thay popup", async () => {
    await setup();
    render(<HrmCatalogScreen />);
    await userEvent.click(await screen.findByRole("button", { name: /^Bảng hệ số theo bậc/ }));
    const table = await screen.findByRole("table", { name: "Bảng hệ số các ngạch" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeVisible();
    expect(within(table).getByRole("link", { name: "Bảng hệ số mới 01.003" })).toHaveAttribute("href", "/kn/nhan-su/danh-muc/he-so/01.003/them-moi?at=2026-10-12");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("chi tiết theo ngày, Log thật và liên kết phòng ban giữ prefix", async () => {
    await setup();
    route.path = "/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/he-so/01.003";
    const ha = createHrmApi(auth.api!);
    await ha.addScale("01.003", { valid_from: "2027-01-01", hold_months: 24, coefficients: ["3.00", "3.50"], note: "Căn cứ mới" });
    const before = await ha.scaleHistory("01.003");
    render(<HrmScaleScreen />);
    expect(await screen.findByRole("heading", { name: /Bảng hệ số —/ })).toBeVisible();
    expect(screen.getByRole("link", { name: "Bảng mới" })).toHaveAttribute("href", "/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/he-so/01.003/them-moi?at=2026-10-12");
    expect(screen.getByRole("table", { name: "Lịch sử bảng hệ số" })).toHaveTextContent("Căn cứ mới");
    expect(screen.getByRole("heading", { name: "Log" })).toBeVisible();
    expect(screen.getAllByText(/Chưa có thông tin người thực hiện/).length).toBeGreaterThan(0);
    expect(await ha.scaleHistory("01.003")).toEqual(before);
  });
  it("mỗi ô số thù địch có lỗi tại ô, không gọi API; Hủy không ghi", async () => {
    await setup();
    const spy = vi.spyOn(auth.api!, "POST");
    render(<HrmScaleScreen create />);
    await screen.findByRole("heading", { name: /Bảng hệ số mới/ });
    const numbers = screen.getAllByRole("textbox").filter((el) => el.getAttribute("inputmode") === "decimal" || el.getAttribute("inputmode") === "numeric");
    expect(numbers.length).toBeGreaterThan(2);
    for (const input of numbers) {
      const original = (input as HTMLInputElement).value;
      for (const raw of ["1e100000", "1e-100000", "9".repeat(100_000)]) {
        fireEvent.change(input, { target: { value: raw } });
        fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
        expect(input).toHaveAttribute("aria-invalid", "true");
        expect(spy).not.toHaveBeenCalled();
      }
      fireEvent.change(input, { target: { value: original } });
    }
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/nhan-su/danh-muc/he-so?at=2026-10-12");
    expect(spy).not.toHaveBeenCalled();
  });
  it("lưu phiên bản mới chuẩn hóa, điều hướng về chi tiết và giữ lịch sử cũ", async () => {
    await setup(); const ha = createHrmApi(auth.api!); const old = await ha.scaleHistory("01.003");
    render(<HrmScaleScreen create />);
    await screen.findByRole("heading", { name: /Bảng hệ số mới/ });
    fireEvent.change(screen.getByLabelText(/Ngày hiệu lực/), { target: { value: "2027-01-01" } });
    fireEvent.change(screen.getByRole("textbox", { name: /^Hệ số bậc 1$/ }), { target: { value: "2,40" } });
    fireEvent.change(screen.getByRole("textbox", { name: /Ghi chú/ }), { target: { value: "Quyết định mới" } });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(route.push).toHaveBeenCalledWith("/kn/nhan-su/danh-muc/he-so/01.003?at=2027-01-01"));
    const history = await ha.scaleHistory("01.003");
    expect(history.versions).toHaveLength(old.versions.length + 1);
    expect(history.versions).toEqual(expect.arrayContaining(old.versions));
  });
  it("ngày URL sai chặn tải và lưu", async () => {
    await setup(); const get = vi.spyOn(auth.api!, "GET"); route.query = "at=2026-02-30";
    render(<HrmScaleScreen create />);
    expect(screen.getByRole("alert")).toHaveTextContent("Ngày hiệu lực không hợp lệ");
    expect(get).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
  });
  it("đổi ngữ cảnh xóa form cũ; quyền chỉ xem chặn tải editor và nút lưu", async () => {
    await setup();
    const view = render(<HrmScaleScreen create />);
    await screen.findByRole("heading", { name: /Bảng hệ số mới/ });
    fireEvent.change(screen.getByRole("textbox", { name: /Ghi chú/ }), { target: { value: "Bản nháp ngữ cảnh cũ" } });
    const source = auth.api!;
    auth.api = { ...source, GET: vi.fn(async () => ({ data: { can_manage: false }, response: new Response() })) } as unknown as ApiClient;
    auth.context = "hcth-readonly";
    view.rerender(<HrmScaleScreen create />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Bạn không có quyền thêm bảng hệ số");
    expect(screen.queryByDisplayValue("Bản nháp ngữ cảnh cũ")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(auth.api.GET).toHaveBeenCalledTimes(1);
  });
  it("lỗi quyền/status fail closed và không hiển thị dữ liệu mẫu cũ", async () => {
    await setup();
    const get = vi.fn(async () => ({ error: { type: "about:blank", title: "Forbidden", status: 403, code: "forbidden" }, response: new Response(null, { status: 403 }) }));
    auth.api = { ...auth.api, GET: get } as unknown as ApiClient;
    render(<HrmScaleScreen create />);
    await screen.findByRole("alert");
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(1);
  });
});
