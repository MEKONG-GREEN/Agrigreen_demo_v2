import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsVersions } from "@/api/mock/kn-cnts-catalogs";
import { CntsCatalogScreen } from "./CntsCatalogScreen";
import { CNTS_TABS, cleanAttrs, expectedDate, formatDoses, parseDoses, tabOf, templateRows, validateAttrs } from "./schema";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
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

  it("cơ sở giết mổ: bắt buộc xã + loại, mã không trùng GS/GC, không gửi facility_ref", () => {
    const tab = tabOf("cnts_slaughter_facility");
    expect(validateAttrs(tab, {})).toMatchObject({ commune_code: "Bắt buộc", facility_kind: "Bắt buộc" });
    expect(validateAttrs(tab, { commune_code: "30292", facility_kind: "tap_trung", stamp_codes_gs: "91.1, 91.2", stamp_codes_gc: "91.2" }).stamp_codes_gc).toMatch(/91.2/);
    expect(cleanAttrs({ facility_ref: "x", commune_code: " 30292 ", address: " " })).toEqual({ commune_code: "30292" });
  });
});

describe("màn Danh mục CNTS (mock theo hợp đồng /v1/masterdata/catalogs/cnts_*)", () => {
  beforeEach(() => {
    resetCntsVersions();
    auth.roles = ["masterdata_admin"];
    auth.persona = "setter";
  });

  it("Admin thêm cơ sở giết mổ có ngày áp dụng; mã mộc dấu trùng → lỗi", async () => {
    const user = userEvent.setup();
    render(<CntsCatalogScreen />);
    await user.click(await screen.findByRole("tab", { name: "Cơ sở giết mổ" }));
    expect(await screen.findByText("Cơ sở giết mổ Bình Đức (giả lập)")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: /Thêm cơ sở giết mổ/ }));
    await user.type(await screen.findByLabelText("Mã"), "CS_MOI");
    await user.type(screen.getByLabelText("Tên"), "Cơ sở mới");
    await user.type(await screen.findByLabelText("Mã xã/phường/đặc khu"), "30292");
    await user.selectOptions(screen.getByLabelText("Loại cơ sở"), "nho_le");
    await user.type(screen.getByLabelText("Mã số khắc mộc dấu gia súc (GS)"), "91.589.05.1");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/đã thuộc cơ sở giết mổ khác/)).toBeTruthy();
    await user.clear(screen.getByLabelText("Mã số khắc mộc dấu gia súc (GS)"));
    await user.type(screen.getByLabelText("Mã số khắc mộc dấu gia súc (GS)"), "91.600.01");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(screen.getByText("Cơ sở mới")).toBeTruthy());
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
