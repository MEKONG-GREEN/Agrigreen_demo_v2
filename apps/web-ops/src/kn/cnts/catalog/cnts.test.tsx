import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { CATALOGS } from "@/api/mock/data";
import { resetCntsVersions } from "@/api/mock/kn-cnts-catalogs";
import { toCsvText } from "@/lib/csv";
import { CntsCatalogItemFormScreen, CntsCatalogItemScreen, CntsCatalogScreen, catalogBase } from "./CntsCatalogScreen";
import { CNTS_TABS, cleanAttrs, effectiveThreshold, expectedDate, exportRows, formatDoses, normalizeAttrs, parseDoses, tabOf, templateRows, validateAttrs } from "./schema";

const nav = { path: "/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts", params: {} as Record<string, string>, search: "" };
const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => nav.path,
  useRouter: () => ({ replace: vi.fn(), push }),
  useParams: () => nav.params,
  useSearchParams: () => new URLSearchParams(nav.search),
}));

const auth = { roles: ["masterdata_admin"] as string[], persona: "setter" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    principal: { roles: auth.roles },
    api: createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${auth.persona}`, fetch: mockFetchNoDelay }),
    adminApi: createAdminApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${auth.persona}`, fetch: mockAdminFetchNoDelay }),
  }),
}));

describe("danh mục CNTS — trường thuộc tính (W11.41)", () => {
  it("12 danh mục theo FN-06, mỗi danh mục có tệp mẫu đúng cột", () => {
    expect(CNTS_TABS).toHaveLength(12);
    const rows = templateRows(tabOf("cnts_slaughter_facility"));
    expect(rows[0]!.slice(0, 4)).toEqual(["catalog", "code", "name", "valid_from"]);
    expect(rows[1]).toHaveLength(rows[0]!.length);
    expect(rows[1]![0]).toBe("cnts_slaughter_facility");
  });

  it("quy trình tiêm: đọc/ghi danh sách mũi, ngày dự kiến theo ngày tuổi", () => {
    const ds = parseDoses("VX_ND_F/1@3, VX_GUMBORO/1@7,hỏng");
    expect(ds).toEqual([
      { vaccine: "VX_ND_F", shot: 1, age_days: 3 },
      { vaccine: "VX_GUMBORO", shot: 1, age_days: 7 },
    ]);
    expect(formatDoses(ds)).toBe("VX_ND_F/1@3,VX_GUMBORO/1@7");
    expect(expectedDate("2026-10-01", 1, { vaccine: "X", shot: 1, age_days: 17 })).toBe("2026-10-17");
    expect(validateAttrs(tabOf("cnts_vaccination_schedule"), { doses: "" })).toMatchObject({ model_type: expect.any(String), doses: expect.any(String) });
  });

  it("chỉ tiêu theo dõi (#170): ngưỡng tối thiểu/tối đa là số thập phân không âm có giới hạn, tối thiểu ≤ tối đa", () => {
    const tab = tabOf("cnts_indicator");
    expect(tab.fields.map((f) => f.key)).toEqual(expect.arrayContaining(["threshold_min", "threshold_max", "diary_metric"]));
    for (const bad of ["1e100000", "-1", "1.23456", "12345678", "abc", "9".repeat(5000)]) {
      expect(validateAttrs(tab, { value_type: "decimal", threshold_min: bad }).threshold_min, bad).toBeTruthy();
    }
    expect(validateAttrs(tab, { value_type: "decimal", threshold_min: "33", threshold_max: "32" }).threshold_max).toMatch(/lớn hơn hoặc bằng/);
    expect(validateAttrs(tab, { value_type: "decimal", threshold_min: "25,5", threshold_max: "32" })).toEqual({});
    expect(normalizeAttrs(tab, { threshold_min: "25,5" }).threshold_min).toBe("25.5");
  });

  it("Xuất danh mục (#170): cùng cột với tệp mẫu (nạp lại được) + trạng thái", () => {
    const tab = tabOf("cnts_indicator");
    const rows = exportRows(tab, [
      { code: "PH", name: "pH", attrs: { value_type: "decimal", threshold_min: "7.5", threshold_max: "8.5" }, active: true, valid_from: "2026-10-01" },
      { code: "CU", name: "Chỉ tiêu cũ", attrs: { value_type: "integer" }, active: false },
    ]);
    expect(rows[0]).toEqual([...templateRows(tab)[0]!, "trang_thai"]);
    expect(rows[1]).toEqual(["cnts_indicator", "PH", "pH", "2026-10-01", "", "decimal", "7.5", "8.5", "", "", "Đang dùng"]);
    expect(rows[2]!.at(-1)).toBe("Ngừng dùng");
    expect(effectiveThreshold("7", "7.5")).toEqual({ value: "7", source: "param" });
    expect(effectiveThreshold(null, "7.5")).toEqual({ value: "7.5", source: "catalog" });
  });

  it("cơ sở giết mổ: bắt buộc xã + loại, mã không trùng GS/GC, không gửi facility_ref", () => {
    const tab = tabOf("cnts_slaughter_facility");
    expect(validateAttrs(tab, {})).toMatchObject({ commune_code: "Bắt buộc nhập.", facility_kind: "Bắt buộc nhập." });
    expect(validateAttrs(tab, { commune_code: "30292", facility_kind: "tap_trung", stamp_codes_gs: "91.1, 91.2", stamp_codes_gc: "91.2" }).stamp_codes_gc).toMatch(/91.2/);
    expect(cleanAttrs({ facility_ref: "x", commune_code: " 30292 ", address: " " })).toEqual({ commune_code: "30292" });
  });
});

describe("Xuất danh mục → Nạp lại (#170, review Codex #168)", () => {
  it("tệp vừa xuất (có valid_from, trang_thai, chỉ tiêu ngưỡng 25–32) nạp lại nguyên vẹn: mọi dòng bỏ qua, không dòng lỗi", async () => {
    resetCntsVersions();
    const admin = createAdminApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockAdminFetchNoDelay });
    const tab = tabOf("cnts_indicator");
    const at = new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
    await unwrap(
      admin.POST("/v1/masterdata/catalogs/{catalog}/items", {
        params: { path: { catalog: "cnts_indicator" } },
        body: { code: "NHIET_DO_RT", name: "Nhiệt độ ao (xuất-nạp)", valid_from: at, attrs: { value_type: "decimal", unit: "°C", diary_metric: "temp_c", threshold_min: "25", threshold_max: "32" } },
      } as never),
    );
    const page = await unwrap(
      admin.GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog: "cnts_indicator" }, query: { at, include_inactive: true } } }),
    );
    const items = page.items as { code: string; name: string; attrs: Record<string, string>; active: boolean; valid_from?: string | null }[];
    expect(items.find((i) => i.code === "NHIET_DO_RT")?.attrs.threshold_max).toBe("32");
    const rows = exportRows(tab, items);
    expect(rows[0]).toEqual(expect.arrayContaining(["valid_from", "trang_thai"]));
    expect(rows.find((r) => r[1] === "NHIET_DO_RT")?.[3]).toBe(at);
    const res = await unwrap(
      admin.POST("/v1/masterdata/catalog-imports", {
        params: { header: { "Idempotency-Key": "rt-170" } },
        body: String.fromCharCode(0xfeff) + toCsvText(rows), // như nút Xuất danh mục (toCsv: BOM + CRLF)
        bodySerializer: (b: unknown) => b as BodyInit,
        headers: { "Content-Type": "text/csv" },
      } as never),
    );
    expect(items.length).toBeGreaterThanOrEqual(4);
    expect(res).toMatchObject({ total: items.length, skipped: items.length, created: 0, rejected: 0 });
    // Dọn mục vừa tạo khỏi dữ liệu giả lập dùng chung để không ảnh hưởng các test màn hình bên dưới.
    const list = CATALOGS.cnts_indicator;
    list.splice(list.findIndex((i) => i.code === "NHIET_DO_RT"), 1);
    resetCntsVersions();
  });
});

describe("màn Danh mục CNTS (mock theo hợp đồng /v1/masterdata/catalogs/cnts_*)", () => {
  beforeEach(() => {
    resetCntsVersions();
    push.mockReset();
    nav.path = "/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts";
    nav.params = {};
    nav.search = "";
    auth.roles = ["masterdata_admin"];
    auth.persona = "setter";
  });

  it("danh sách không còn pop-up: Thêm, Xem, Sửa là liên kết tới trang riêng; mở đúng tab theo ?tab=", async () => {
    nav.search = "tab=cnts_slaughter_facility";
    render(<CntsCatalogScreen />);
    expect(await screen.findByText("Cơ sở giết mổ Bình Đức (giả lập)")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Thêm cơ sở giết mổ/ })).toHaveAttribute("href", "/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts/cnts_slaughter_facility/them-moi");
    const view = screen.getByRole("link", { name: "Xem Cơ sở giết mổ Bình Đức (giả lập)" });
    expect(view.getAttribute("href")).toMatch(/^\/kn\/phong-ban\/chan_nuoi_thuy_san\/danh-muc-cnts\/cnts_slaughter_facility\/[^/]+$/);
    expect(screen.getByRole("link", { name: "Sửa Cơ sở giết mổ Bình Đức (giả lập)" }).getAttribute("href")).toBe(`${view.getAttribute("href")}/sua`);
    expect(screen.getByRole("columnheader", { name: "STT" })).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("Ngừng dùng: pop-up lý do ngắn, lý do < 10 ký tự bị chặn trước khi gọi API", async () => {
    const user = userEvent.setup();
    render(<CntsCatalogScreen />);
    await user.click((await screen.findAllByRole("button", { name: "Ngừng dùng" }))[0]!);
    const dlg = screen.getByRole("dialog");
    await user.type(within(dlg).getByLabelText(/Lý do/), "ngắn");
    await user.click(within(dlg).getByRole("button", { name: "Ngừng dùng" }));
    expect(within(dlg).getByRole("alert")).toHaveTextContent("Tối thiểu 10 ký tự.");
  });

  it("Thêm cơ sở giết mổ trên trang riêng: thiếu trường → lỗi theo trường; mã mộc dấu trùng → lỗi; hợp lệ → về danh sách", async () => {
    nav.path = "/kn/cnts/danh-muc/cnts_slaughter_facility/them-moi";
    nav.params = { type: "cnts_slaughter_facility" };
    const user = userEvent.setup();
    render(<CntsCatalogItemFormScreen />);
    expect(screen.getByRole("heading", { level: 1, name: /Thêm cơ sở giết mổ/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/cnts/danh-muc?tab=cnts_slaughter_facility");
    const xa = await screen.findByLabelText(/^Xã\/phường\/đặc khu/);
    expect(xa.tagName).toBe("SELECT"); // chọn từ danh mục ĐVHC, không gõ mã (#169)
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByRole("alert").filter((a) => a.textContent === "Bắt buộc nhập.").length).toBeGreaterThanOrEqual(4); // Mã, Tên, Xã, Loại cơ sở
    expect(push).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText(/^Mã \*$/), "CS_MOI");
    await user.type(screen.getByLabelText(/^Tên \*$/), "Cơ sở mới");
    await user.selectOptions(await screen.findByLabelText(/Tỉnh\/thành phố của xã/), "91");
    await waitFor(() => expect(within(screen.getByLabelText(/^Xã\/phường\/đặc khu/)).getByRole("option", { name: "Xã Bình Thạnh (91101)" })).toBeTruthy());
    await user.selectOptions(screen.getByLabelText(/^Xã\/phường\/đặc khu/), "91101");
    await user.selectOptions(screen.getByLabelText(/Loại cơ sở/), "nho_le");
    await user.type(screen.getByLabelText(/Mã số khắc mộc dấu gia súc \(GS\)/), "91.589.05.1");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/đã thuộc cơ sở giết mổ khác/)).toBeTruthy();
    await user.clear(screen.getByLabelText(/Mã số khắc mộc dấu gia súc \(GS\)/));
    await user.type(screen.getByLabelText(/Mã số khắc mộc dấu gia súc \(GS\)/), "91.600.01");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/cnts/danh-muc?tab=cnts_slaughter_facility"));
  });

  it("CSGM (#169): danh sách có cột riêng Xã (tên), Mã xã, Loại, mộc dấu GS/GC, Loài, Địa chỉ và lọc theo xã", async () => {
    nav.search = "tab=cnts_slaughter_facility";
    const user = userEvent.setup();
    render(<CntsCatalogScreen />);
    expect(await screen.findByText("Cơ sở giết mổ tập trung mẫu số 2 (giả lập)")).toBeTruthy();
    for (const h of ["Xã", "Mã xã", "Loại", "Mộc dấu GS", "Mộc dấu GC", "Loài", "Địa chỉ", "Trạng thái"]) expect(screen.getByRole("columnheader", { name: h })).toBeTruthy();
    const row = screen.getByText("Cơ sở giết mổ tập trung mẫu số 2 (giả lập)").closest("tr")!;
    expect(await within(row).findByText("Xã Bình Thạnh")).toBeTruthy();
    expect(within(row).getByText("91101")).toBeTruthy();
    expect(within(row).getByText("91.900.21")).toBeTruthy();
    expect(within(row).getByText("91.900.22")).toBeTruthy();
    expect(within(row).getByText("Ấp Mẫu 2")).toBeTruthy();
    await user.selectOptions(screen.getByLabelText("Lọc theo xã"), "91102");
    expect(screen.queryByText("Cơ sở giết mổ tập trung mẫu số 2 (giả lập)")).toBeNull();
    expect(screen.getByText("Cơ sở giết mổ nhỏ lẻ mẫu số 3 (giả lập)")).toBeTruthy();
  });

  it("CSGM (#169): trang chi tiết hiện tên và mã xã; máy chủ (mock) từ chối mã xã không có trong ĐVHC", async () => {
    nav.path = "/kn/cnts/danh-muc/cnts_slaughter_facility/CSGM_MAU_03";
    nav.params = { type: "cnts_slaughter_facility", code: "CSGM_MAU_03" };
    render(<CntsCatalogItemScreen />);
    expect(await screen.findByText("Xã An Phước (91102)")).toBeTruthy();
    const r = await admin().POST("/v1/masterdata/catalogs/{catalog}/items", {
      params: { path: { catalog: "cnts_slaughter_facility" }, header: { "Idempotency-Key": "csgm-xa-la" } },
      body: { code: "CS_XA_LA", name: "Cơ sở xã lạ", attrs: { commune_code: "99999", facility_kind: "nho_le" } },
    });
    expect(r.response.status).toBe(422);
    expect((r.error as { code?: string }).code).toBe("masterdata.cnts_commune_invalid");
  });

  it("Vật nuôi: Thứ tự cột âm / thập phân / quá 999 bị chặn (R-A1), không gọi API", async () => {
    nav.path = "/kn/cnts/danh-muc/cnts_species/them-moi";
    nav.params = { type: "cnts_species" };
    const user = userEvent.setup();
    render(<CntsCatalogItemFormScreen />);
    const order = await screen.findByLabelText(/Thứ tự cột/);
    await user.type(screen.getByLabelText(/^Mã \*$/), "VN_MOI");
    await user.type(screen.getByLabelText(/^Tên \*$/), "Vật nuôi mới");
    await user.selectOptions(screen.getByLabelText(/^Nhóm/), "gia_cam");
    await user.type(screen.getByLabelText(/Đơn vị tính/), "con");
    for (const [raw, msg] of [
      ["-1", "Không được nhập số âm."],
      ["1,5", "Chỉ nhập số nguyên."],
      ["1000", "Không được lớn hơn 999."],
    ] as const) {
      await user.clear(order);
      await user.type(order, raw);
      await user.click(screen.getByRole("button", { name: "Lưu" }));
      expect(screen.getAllByRole("alert").map((a) => a.textContent)).toContain(msg);
    }
    expect(push).not.toHaveBeenCalled();
  });

  it("Sửa mục: trang riêng có mã cố định, Hủy về trang chi tiết; trang chi tiết có các giá trị theo ngày", async () => {
    nav.path = "/kn/cnts/danh-muc/cnts_species/TRAU/sua";
    nav.params = { type: "cnts_species", code: "TRAU" };
    const user = userEvent.setup();
    const { unmount } = render(<CntsCatalogItemFormScreen />);
    const code = await screen.findByLabelText(/^Mã \*$/);
    expect(code).toBeDisabled();
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/cnts/danh-muc/cnts_species/TRAU");
    await user.clear(screen.getByLabelText(/^Tên \*$/));
    await user.type(screen.getByLabelText(/^Tên \*$/), "Trâu ta");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/cnts/danh-muc/cnts_species/TRAU"));
    unmount();
    nav.path = "/kn/cnts/danh-muc/cnts_species/TRAU";
    render(<CntsCatalogItemScreen />);
    expect(await screen.findByRole("heading", { level: 1, name: /Trâu ta/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sửa" })).toHaveAttribute("href", "/kn/cnts/danh-muc/cnts_species/TRAU/sua");
    expect(await screen.findByRole("table", { name: "Các giá trị có hiệu lực từ ngày" })).toBeTruthy();
  });

  // Codex review #101: danh sách đọc tại "Xem tại ngày" nhưng liên kết Xem/Sửa mất ngày đó → trang riêng nạp hôm nay.
  const admin = () => createAdminApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockAdminFetchNoDelay });
  const followFromList = async (at: string, linkName: string, params: Record<string, string>) => {
    const { unmount } = render(<CntsCatalogScreen />);
    fireEvent.change(screen.getByLabelText("Xem tại ngày"), { target: { value: at } });
    const href = (await screen.findByRole("link", { name: linkName })).getAttribute("href")!;
    unmount();
    const [path, search = ""] = href.split("?");
    nav.path = path!;
    nav.search = search;
    nav.params = params;
    return href;
  };

  it("mục có hiệu lực tương lai, chọn tại ngày đó: liên kết Sửa/Xem mang ?at= và trang riêng nạp đúng ngày", async () => {
    nav.path = "/kn/cnts/danh-muc";
    const today = await unwrap(admin().GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog: "cnts_species" }, query: { include_inactive: true } } }));
    const attrs = today.items.find((x) => x.code === "TRAU")!.attrs;
    await unwrap(
      admin().POST("/v1/masterdata/catalogs/{catalog}/items", {
        params: { path: { catalog: "cnts_species" }, header: { "Idempotency-Key": "review101-future" } },
        body: { code: "REVIEW_FUTURE_101", name: "Vật nuôi hiệu lực tương lai", attrs, valid_from: "2099-01-01" },
      }),
    );
    const edit = await followFromList("2099-01-01", "Sửa Vật nuôi hiệu lực tương lai", { type: "cnts_species", code: "REVIEW_FUTURE_101" });
    expect(edit).toBe("/kn/cnts/danh-muc/cnts_species/REVIEW_FUTURE_101/sua?at=2099-01-01");
    const { unmount } = render(<CntsCatalogItemFormScreen />);
    expect(await screen.findByRole("heading", { level: 1 })).not.toHaveTextContent("Không có mục");
    expect(screen.getByLabelText(/^Tên \*$/)).toHaveValue("Vật nuôi hiệu lực tương lai");
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/cnts/danh-muc/cnts_species/REVIEW_FUTURE_101?at=2099-01-01");
    unmount();
    nav.path = "/kn/cnts/danh-muc/cnts_species/REVIEW_FUTURE_101";
    render(<CntsCatalogItemScreen />);
    expect(await screen.findByRole("heading", { level: 1, name: /Vật nuôi hiệu lực tương lai/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sửa" })).toHaveAttribute("href", "/kn/cnts/danh-muc/cnts_species/REVIEW_FUTURE_101/sua?at=2099-01-01");
    expect(screen.getByRole("link", { name: "Vật nuôi" })).toHaveAttribute("href", "/kn/cnts/danh-muc?tab=cnts_species&at=2099-01-01");
  });

  it("mục có hai giá trị theo ngày: trang Sửa/Xem hiện giá trị tại ngày đã chọn, ?at= sai → hôm nay", async () => {
    nav.path = "/kn/cnts/danh-muc";
    const cur = (await unwrap(admin().GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog: "cnts_species" }, query: { include_inactive: true } } }))).items.find(
      (x) => x.code === "TRAU",
    )!;
    await unwrap(
      admin().PUT("/v1/masterdata/catalogs/{catalog}/items/{code}", {
        params: { path: { catalog: "cnts_species", code: "TRAU" } },
        body: { name: "Trâu (từ 2099)", attrs: cur.attrs, valid_from: "2099-01-01" },
      }),
    );
    await followFromList("2099-06-01", "Sửa Trâu (từ 2099)", { type: "cnts_species", code: "TRAU" });
    let r = render(<CntsCatalogItemFormScreen />);
    expect(await screen.findByLabelText(/^Tên \*$/)).toHaveValue("Trâu (từ 2099)");
    r.unmount();
    nav.path = "/kn/cnts/danh-muc/cnts_species/TRAU";
    r = render(<CntsCatalogItemScreen />);
    expect(await screen.findByRole("heading", { level: 1, name: /Trâu \(từ 2099\)/ })).toBeTruthy();
    r.unmount();
    nav.search = "at=2099-13-45";
    render(<CntsCatalogItemScreen />);
    const h = await screen.findByRole("heading", { level: 1, name: new RegExp(cur.name) });
    expect(h).not.toHaveTextContent("2099");
    expect(screen.getByRole("link", { name: "Sửa" })).toHaveAttribute("href", "/kn/cnts/danh-muc/cnts_species/TRAU/sua");
  });

  it("#170: tab Ngưỡng mặc định sinh từ danh mục chỉ tiêu — chỉ tiêu mới có ngưỡng hiện ngay; nút Xuất danh mục", async () => {
    await unwrap(
      admin().POST("/v1/masterdata/catalogs/{catalog}/items", {
        params: { path: { catalog: "cnts_indicator" }, header: { "Idempotency-Key": "ind-170" } },
        body: { code: "NHIET_DO_AO", name: "Nhiệt độ nước ao", attrs: { value_type: "decimal", unit: "°C", threshold_min: "25", threshold_max: "32", diary_metric: "temp_c" } },
      }),
    );
    const bad = await admin().POST("/v1/masterdata/catalogs/{catalog}/items", {
      params: { path: { catalog: "cnts_indicator" }, header: { "Idempotency-Key": "ind-170-bad" } },
      body: { code: "XAU", name: "Ngưỡng hỏng", attrs: { value_type: "decimal", threshold_min: "1e100000" } },
    });
    expect(bad.response.status).toBe(422);
    const user = userEvent.setup();
    nav.search = "tab=cnts_indicator";
    render(<CntsCatalogScreen />);
    expect(await screen.findByText("Nhiệt độ nước ao")).toBeTruthy();
    for (const h of ["Ngưỡng tối thiểu", "Ngưỡng tối đa", "Chỉ số nhật ký"]) expect(screen.getByRole("columnheader", { name: h })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Xuất danh mục/ })).toBeEnabled();
    await user.click(screen.getByRole("tab", { name: "Ngưỡng mặc định" }));
    const table = await screen.findByRole("table", { name: "Ngưỡng theo chỉ tiêu theo dõi" });
    expect(await within(table).findAllByText("Nhiệt độ nước ao (NHIET_DO_AO)")).toHaveLength(2);
    expect(within(table).getByText("32 °C")).toBeTruthy();
    expect(within(table).getByText("cnts.indicator_max:NHIET_DO_AO")).toBeTruthy();
    expect(within(table).getAllByText("Ngưỡng mặc định của danh mục").length).toBeGreaterThanOrEqual(2);
  });

  it("gốc route danh mục cho cả Phòng CNTS và Admin › Phòng ban", () => {
    expect(catalogBase("/kn/cnts/danh-muc/cnts_species/them-moi")).toBe("/kn/cnts/danh-muc");
    expect(catalogBase("/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts/cnts_species/HEO/sua")).toBe("/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts");
  });

  it("Cấp Tỉnh chỉ xem: không có nút Thêm, ngưỡng mặc định đọc từ tham số", async () => {
    auth.roles = [];
    auth.persona = "tinh";
    const user = userEvent.setup();
    render(<CntsCatalogScreen />);
    expect(await screen.findByText(/Chỉ xem/)).toBeTruthy();
    expect(await screen.findByText("Heo")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Thêm/ })).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Ngưỡng mặc định" }));
    expect(await screen.findByText("Biến động tổng đàn BM-04")).toBeTruthy();
    expect(await screen.findByText("15 %")).toBeTruthy();
  });
});
