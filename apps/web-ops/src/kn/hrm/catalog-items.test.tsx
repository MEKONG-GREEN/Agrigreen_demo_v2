import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { createHrmApi, FIXED_KINDS, MAIN_KINDS, type HrmAttrs } from "./api";
import { catalogHistory, HrmCatalogItemFormScreen, HrmCatalogItemScreen, HrmCatalogKindScreen } from "./CatalogItemScreens";
import { catalogBase, validateCatalogItem, type CatalogValues } from "./catalog-form";

const route = vi.hoisted(() => ({ params: { kind: "position", code: "vien_chuc" }, path: "/kn/nhan-su/danh-muc", push: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => route.params, usePathname: () => route.path, useRouter: () => ({ push: route.push }) }));
const auth: { api: ApiClient | null; context: string } = { api: null, context: "admin" };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { context_id: auth.context } } }) }));

async function setup(fetcher: (request: Request) => Promise<Response> = mockFetchNoDelay) {
  const context = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": "catalog-test-context" }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: "trong_trot" }) }));
  const { context_id } = await context.json() as { context_id: string };
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context_id, fetch: fetcher });
  return createHrmApi(auth.api);
}

const good: CatalogValues = { code: "moi", name: "Mục mới", order: "10", allowance: "", rank: "1" };

describe("FN-19: kiểm đầu vào trước khi tính", () => {
  it.each(["order", "allowance", "rank"] as const)("%s chặn âm, số mũ, chuỗi dài và số vượt biên", (key) => {
    const kind = key === "rank" ? "education_level" : "position";
    for (const raw of ["-1", "1e100000", "1e-100000", "1e1000", "9".repeat(100000), "99999999999999999999999999999999", "1.001"]) {
      const result = validateCatalogItem(kind, { ...good, [key]: raw }, { leader: key === "allowance" });
      expect(result.valid, raw.slice(0, 40)).toBe(false);
      expect(result.errors[key]).toBeTruthy();
      expect(result.body).toBeNull();
    }
  });

  it("biên domain, required và chuẩn hóa không cắt bỏ dấu âm/số mũ", () => {
    expect(validateCatalogItem("position", { ...good, order: "100000", allowance: "10,00" }, { leader: true }).body).toMatchObject({ sort_order: 100000, attrs: { allowance: "10" } });
    expect(validateCatalogItem("position", { ...good, order: "100001" }, {}).errors.order).toBeTruthy();
    expect(validateCatalogItem("education_level", { ...good, rank: "100" }, {}).body?.attrs?.rank).toBe(100);
    expect(validateCatalogItem("education_level", { ...good, rank: "101" }, {}).errors.rank).toBeTruthy();
    expect(validateCatalogItem("position", { ...good, allowance: "10.01" }, { leader: true }).errors.allowance).toBeTruthy();
    expect(validateCatalogItem("position", { ...good, code: "", name: " ", order: "" }, {}).errors).toMatchObject({ code: expect.any(String), name: expect.any(String), order: expect.any(String) });
    expect(validateCatalogItem("major_group", good, {}).errors.level).toBeTruthy();
    expect(validateCatalogItem("grade", { ...good, code: " V.03.04.12 \n" }, { equiv_group: "CV", grade_type: "vien_chuc" }).body?.code).toBe("V.03.04.12");
  });

  it.each([...MAIN_KINDS, ...FIXED_KINDS])("%s giữ thuộc tính riêng khi lưu", (kind) => {
    const attrs: HrmAttrs = kind === "grade" ? { equiv_group: "CS", grade_type: "cong_chuc", salary_class: "B" } : kind === "major_group" ? { level: "dai_hoc" } : kind === "major" ? { groups: { dai_hoc: "dh_nong_hoc" } } : kind === "ethnicity" ? { minority: true } : kind === "religion" ? { no_religion: true } : {};
    const result = validateCatalogItem(kind, good, attrs);
    expect(result.valid).toBe(true);
    expect(result.body?.attrs).toMatchObject(attrs);
    expect(attrs).not.toHaveProperty("rank");
  });
});

describe("FN-19: route thêm/sửa/chi tiết và quyền từ server", () => {
  beforeEach(() => { resetHrm(); auth.context = "admin"; route.params = { kind: "position", code: "vien_chuc" }; route.path = "/kn/nhan-su/danh-muc"; route.push.mockReset(); });

  it("Thêm: trường bắt buộc, lỗi tại ô; lưu hợp lệ về chi tiết, không có dialog", async () => {
    const ha = await setup();
    const user = userEvent.setup();
    render(<HrmCatalogItemFormScreen />);
    const code = await screen.findByLabelText(/^Mã/);
    expect(code).toHaveAttribute("aria-required", "true");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(code).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Tên/)).toHaveAttribute("aria-invalid", "true");
    await user.type(code, "TRUONG_TRAM");
    await user.type(screen.getByLabelText(/^Tên/), "Trạm trưởng");
    const order = screen.getByLabelText(/^Thứ tự/);
    fireEvent.change(order, { target: { value: "1e100000" } });
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(order).toHaveAttribute("aria-invalid", "true");
    expect((await ha.list("position")).items.some((x) => x.code === "truong_tram")).toBe(false);
    fireEvent.change(order, { target: { value: "80" } });
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(route.push).toHaveBeenCalledWith("/kn/nhan-su/danh-muc/position/truong_tram"));
    expect((await ha.list("position")).items.find((x) => x.code === "truong_tram")).toMatchObject({ name: "Trạm trưởng", sort_order: 80 });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Log" })).toBeVisible();
  });

  it("Sửa giữ mã/trạng thái; chi tiết và Log giữ cả phiên bản cũ, không sửa mảng API", async () => {
    const ha = await setup();
    await ha.revise("position", "vien_chuc", { name: "Viên chức", sort_order: 70, active: false });
    const user = userEvent.setup();
    const form = render(<HrmCatalogItemFormScreen edit />);
    expect(await screen.findByLabelText(/^Mã/)).toBeDisabled();
    await user.clear(screen.getByLabelText(/^Tên/));
    await user.type(screen.getByLabelText(/^Tên/), "Viên chức đổi tên");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(route.push).toHaveBeenCalled());
    expect((await ha.history("position", "vien_chuc")).versions.at(-1)).toMatchObject({ code: "vien_chuc", name: "Viên chức đổi tên", active: false });
    const versions = (await ha.history("position", "vien_chuc")).versions;
    const before = JSON.stringify(versions);
    const entries = catalogHistory("position", versions, new Map());
    expect(entries.map((entry) => entry.event)).toEqual(["Phiên bản 3", "Phiên bản 2", "Phiên bản 1 · Mẫu"]);
    expect(entries[0]?.changes).toEqual([{ field: "Tên", old: "Viên chức", new: "Viên chức đổi tên" }]);
    expect(entries[1]?.changes).toContainEqual({ field: "Trạng thái", old: "Đang dùng", new: "Ngừng sử dụng" });
    expect(JSON.stringify(versions)).toBe(before);
    form.unmount();
    render(<HrmCatalogItemScreen />);
    expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Viên chức đổi tên · vien_chuc");
    const log = screen.getByRole("complementary", { name: "Log" });
    expect(within(log).getByText("Phiên bản 3")).toBeVisible();
    expect(within(log).getByText("Phiên bản 1 · Mẫu")).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "STT" })).toBeVisible();
  });

  it("chuyên ngành giữ ánh xạ nhóm riêng theo trình độ NS05 khi lưu", async () => {
    route.params.kind = "major";
    const ha = await setup();
    const user = userEvent.setup();
    render(<HrmCatalogItemFormScreen />);
    await user.type(await screen.findByLabelText(/^Mã/), "chuyen_nganh_moi");
    await user.type(screen.getByLabelText(/^Tên/), "Ngành mới");
    await user.selectOptions(screen.getByRole("combobox", { name: "Đại học" }), "dh_nong_hoc");
    await user.selectOptions(screen.getByRole("combobox", { name: "Thạc sĩ" }), "ths_nong_hoc");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(route.push).toHaveBeenCalledWith("/kn/nhan-su/danh-muc/major/chuyen_nganh_moi"));
    expect((await ha.history("major", "chuyen_nganh_moi")).versions[0]?.attrs.groups).toEqual({ dai_hoc: "dh_nong_hoc", thac_si: "ths_nong_hoc" });
  });

  it.each(["order", "allowance", "rank"] as const)("ô %s chặn chuỗi dài ngay khi blur, không gọi API ghi", async (key) => {
    route.params.kind = key === "rank" ? "education_level" : "position";
    const writes = vi.fn();
    await setup(async (req) => {
      if (req.method === "POST" && req.url.includes("/hrm/catalogs/")) writes();
      return mockFetchNoDelay(req);
    });
    const user = userEvent.setup();
    render(<HrmCatalogItemFormScreen />);
    await screen.findByLabelText(/^Mã/);
    if (key === "allowance") await user.click(screen.getByRole("checkbox", { name: "Chức vụ lãnh đạo, quản lý" }));
    const input = screen.getByLabelText(key === "rank" ? /^Thứ hạng/ : key === "order" ? /^Thứ tự/ : /^Hệ số phụ cấp/);
    for (const value of ["1e100000", "1e-100000", "9".repeat(100000), "99999999999999999999999999999999"]) {
      fireEvent.change(input, { target: { value } });
      fireEvent.blur(input);
      expect(input).toHaveAttribute("aria-invalid", "true");
      await user.click(screen.getByRole("button", { name: "Lưu" }));
    }
    expect(writes).not.toHaveBeenCalled();
  });

  it("retry sau 500 dùng cùng khóa; lỗi theo trường server gắn vào ô", async () => {
    const keys: string[] = [];
    let writes = 0;
    await setup(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/hrm/catalogs/position")) {
        keys.push(req.headers.get("Idempotency-Key")!);
        writes++;
        if (writes === 1) return new Response(JSON.stringify({ type: "about:blank", title: "Tạm lỗi", code: "internal_error", status: 500 }), { status: 500, headers: { "Content-Type": "application/problem+json" } });
        if (writes === 2) return new Response(JSON.stringify({ type: "about:blank", title: "Sai tên", code: "validation_failed", status: 422, errors: [{ field: "name", reason: "Tên không hợp lệ từ server" }] }), { status: 422, headers: { "Content-Type": "application/problem+json" } });
      }
      return mockFetchNoDelay(req);
    });
    const user = userEvent.setup();
    render(<HrmCatalogItemFormScreen />);
    await user.type(await screen.findByLabelText(/^Mã/), "moi");
    await user.type(screen.getByLabelText(/^Tên/), "Mục mới");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Tên không hợp lệ từ server", { selector: ".err" })).toBeVisible();
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBeTruthy();
    expect(keys[1]).toBe(keys[0]);
    expect(route.push).not.toHaveBeenCalled();
  });

  it("chỉ xem/thiếu quyền không mở form ghi; loại lạ không gọi API; alias phòng ban giữ đúng đường", async () => {
    const fetcher = vi.fn<(request: Request) => Promise<Response>>(async (req) => {
      const res = await mockFetchNoDelay(req);
      if (req.url.endsWith("/hrm/status")) return Response.json({ ...await res.json(), can_manage: false });
      return res;
    });
    await setup(fetcher);
    const view = render(<HrmCatalogItemFormScreen edit />);
    expect(await screen.findByRole("alert")).toHaveTextContent("không có quyền");
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(fetcher.mock.calls.some(([req]) => (req as Request).url.includes("/catalogs/"))).toBe(false);
    view.unmount();
    route.params.kind = "invalid";
    const unknown = render(<HrmCatalogItemFormScreen />);
    expect(screen.getByRole("alert")).toHaveTextContent("Không tìm thấy");
    unknown.unmount();
    route.params.kind = "position";
    route.path = "/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su/position";
    await setup();
    render(<HrmCatalogKindScreen />);
    expect(await screen.findByRole("link", { name: "Thêm" })).toHaveAttribute("href", `${catalogBase(route.path)}/position/them-moi`);
  });

  it("đổi ngữ cảnh hủy form và quyền Admin cũ ngay khi status mới còn chờ", async () => {
    let manage = true;
    await setup(async (req) => {
      const res = await mockFetchNoDelay(req);
      if (req.url.endsWith("/hrm/status")) return Response.json({ ...await res.json(), can_manage: manage });
      return res;
    });
    const view = render(<HrmCatalogItemFormScreen edit />);
    expect(await screen.findByLabelText(/^Mã/)).toBeDisabled();
    expect(screen.getByRole("button", { name: "Lưu" })).toBeVisible();
    manage = false;
    auth.context = "hcth-readonly";
    view.rerender(<HrmCatalogItemFormScreen edit />);
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent("không có quyền");
  });
});
