// BF-TT-MAT (#105, PRD TT-12): Vật tư dạng bảng nhập — lọc Xã + Thời điểm, Thêm dòng, Lưu (gom theo Thời điểm),
// ĐVT tự điền theo danh mục, số lượng không âm/nguyên theo ĐVT, Nạp Excel (máy chủ đọc tệp, không ghi), Lịch sử.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { vnToday } from "@/lib/vnDate";
import { MaterialsScreen, validateLines } from "./MaterialsScreen";

vi.mock("next/navigation", () => ({ usePathname: () => "/kn/du-lieu/vat-tu", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }), useParams: () => ({}) }));

const COMMUNES = [
  { code: "91301", name: "Mỹ Hòa Hưng" },
  { code: "91302", name: "Bình Đức" },
];
const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: {}, kn: { context: { role: auth.role, communes: COMMUNES, department_code: "trong_trot" } }, collectApi: auth.collectApi }),
}));

const CATALOG = [
  { catalog: "material", code: "phan_ure", name: "Phân urê", attrs: { unit: "tấn" }, active: true },
  { catalog: "material", code: "bay_den", name: "Bẫy đèn", attrs: { unit: "cái" }, active: true },
  { catalog: "material", code: "cu", name: "Thuốc cũ", attrs: { unit: "chai" }, active: false },
];
const REC = {
  id: "0192aaaa-0000-7000-8000-000000000001",
  commune_code: "91301",
  recorded_on: "2026-09-30",
  material_code: "phan_ure",
  material_name: "Phân urê",
  quantity: "2.5",
  unit: "tấn",
  note: null,
  actor_ref: "a",
  actor_display: "Cán bộ xã",
  recorded_at: "2026-09-30T03:00:00Z",
  supersedes_id: null,
  correction_reason: null,
};
const today = vnToday();

beforeEach(() => {
  auth.role = "kn_xa";
  auth.collectApi = {
    materialCatalog: vi.fn().mockResolvedValue({ items: CATALOG, next_cursor: null }),
    materials: vi.fn().mockResolvedValue({ items: [], next_cursor: null }),
    materialSummary: vi.fn().mockResolvedValue({ items: [] }),
    recordMaterials: vi.fn().mockResolvedValue({ items: [] }),
    materialLog: vi.fn().mockResolvedValue({ items: [{ id: REC.id, event: "recorded", actor_ref: "a", actor_display: "Cán bộ xã", at: REC.recorded_at, changes: [{ field: "quantity", old: null, new: "2.5" }], reason: null }], next_cursor: null }),
    importMaterials: vi.fn(),
  };
});

const table = () => screen.getByRole("region", { name: "Bảng nhập vật tư" });

describe("Vật tư — bảng nhập nhiều dòng (TT-12)", () => {
  it("lọc Xã + Thời điểm: danh sách đã ghi nhận theo xã và đúng ngày; có Thêm dòng, Lưu, Nạp Excel, Lịch sử", async () => {
    render(<MaterialsScreen />);
    await waitFor(() => expect(auth.collectApi.materials).toHaveBeenCalledWith({ commune: "91301", from: today, to: today }));
    expect(screen.getByRole("button", { name: "+ Thêm dòng" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lưu" })).toBeDisabled(); // chưa có dòng
    expect(screen.getByLabelText("Tệp vật tư (Excel/CSV)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Lịch sử/ })).toBeInTheDocument();
    const u = userEvent.setup();
    await u.selectOptions(screen.getByRole("combobox", { name: "Xã/phường" }), "91302");
    await waitFor(() => expect(auth.collectApi.materials).toHaveBeenLastCalledWith({ commune: "91302", from: today, to: today }));
    expect(screen.queryByRole("option", { name: "Tất cả xã phụ trách" })).toBeNull(); // nhập luôn cho một xã cụ thể
  });

  it("dòng trống/số âm/số lẻ với ĐVT đếm được → lỗi theo dòng, không gọi API; ĐVT tự điền; vật tư ngừng dùng không có để chọn", async () => {
    const u = userEvent.setup();
    render(<MaterialsScreen />);
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await screen.findByRole("option", { name: "Phân urê" });
    expect(screen.queryByRole("option", { name: "Thuốc cũ" })).toBeNull();
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(within(table()).getAllByRole("alert").map((a) => a.textContent)).toEqual(["Bắt buộc nhập.", "Bắt buộc nhập."]);
    await u.selectOptions(screen.getByLabelText("Loại vật tư dòng 1"), "bay_den");
    expect(screen.getByLabelText("Đơn vị tính dòng 1")).toHaveTextContent("cái");
    await u.type(screen.getByLabelText("Số lượng dòng 1"), "-3");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(within(table()).getByRole("alert")).toHaveTextContent("Không được nhập số âm.");
    await u.clear(screen.getByLabelText("Số lượng dòng 1"));
    await u.type(screen.getByLabelText("Số lượng dòng 1"), "2,5");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(within(table()).getByRole("alert")).toHaveTextContent("Chỉ nhập số nguyên.");
    expect(auth.collectApi.recordMaterials).not.toHaveBeenCalled();
  });

  it("Lưu nhiều dòng: gom theo Thời điểm (mỗi ngày một lần ghi), số đã chuẩn hóa; dòng đã lưu rời bảng", async () => {
    const u = userEvent.setup();
    render(<MaterialsScreen />);
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await waitFor(() => expect(screen.getAllByRole("option", { name: "Phân urê" })).toHaveLength(3));
    await u.selectOptions(screen.getByLabelText("Loại vật tư dòng 1"), "phan_ure");
    await u.type(screen.getByLabelText("Số lượng dòng 1"), "1.234,5");
    await u.selectOptions(screen.getByLabelText("Loại vật tư dòng 2"), "bay_den");
    await u.type(screen.getByLabelText("Số lượng dòng 2"), "10");
    await u.type(screen.getByLabelText("Ghi chú dòng 2"), "Đợt 1");
    await u.selectOptions(screen.getByLabelText("Loại vật tư dòng 3"), "phan_ure");
    await u.type(screen.getByLabelText("Số lượng dòng 3"), "3");
    const d3 = screen.getByLabelText("Thời điểm dòng 3");
    await u.clear(d3);
    await u.type(d3, "2026-09-01");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.collectApi.recordMaterials).toHaveBeenCalledTimes(2));
    const calls = auth.collectApi.recordMaterials!.mock.calls.map((c) => c[0]);
    expect(calls[0]).toEqual({ commune_code: "91301", recorded_on: today, lines: [{ material_code: "phan_ure", quantity: "1234.5" }, { material_code: "bay_den", quantity: "10", note: "Đợt 1" }] });
    expect(calls[1]).toEqual({ commune_code: "91301", recorded_on: "2026-09-01", lines: [{ material_code: "phan_ure", quantity: "3" }] });
    expect(await screen.findByText(/Đã lưu 3 dòng vật tư/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Loại vật tư dòng 1")).toBeNull();
  });

  it("Xóa bỏ dòng chưa lưu", async () => {
    const u = userEvent.setup();
    render(<MaterialsScreen />);
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await u.click(screen.getByRole("button", { name: "Xóa dòng 1" }));
    expect(screen.queryByLabelText("Loại vật tư dòng 1")).toBeNull();
  });

  it("Nạp Excel: dòng đọc được vào bảng (chưa ghi), dòng lỗi hiện lý do theo dòng tệp", async () => {
    auth.collectApi.importMaterials!.mockResolvedValue({
      accepted: 1,
      rejected: 1,
      lines: [
        { row: 2, material_code: "phan_ure", material_name: "Phân urê", unit: "tấn", quantity: "12.5", recorded_on: "2026-09-30", note: "Đợt 1", status: "accepted", reason: null },
        { row: 3, material_code: "bay_den", material_name: "Bẫy đèn", unit: "cái", quantity: null, recorded_on: null, note: null, status: "rejected", reason: "ĐVT đếm được: số lượng phải là số nguyên" },
      ],
    });
    const u = userEvent.setup();
    render(<MaterialsScreen />);
    await waitFor(() => expect(auth.collectApi.materialCatalog).toHaveBeenCalled());
    await u.upload(screen.getByLabelText("Tệp vật tư (Excel/CSV)"), new File(["Vật tư,Số lượng\n"], "vat-tu.csv", { type: "text/csv" }));
    expect(await screen.findByText(/Đã nạp 2 dòng, 1 dòng cần sửa/)).toBeInTheDocument();
    expect(auth.collectApi.importMaterials).toHaveBeenCalledTimes(1);
    expect(auth.collectApi.recordMaterials).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Số lượng dòng 1")).toHaveValue("12.5");
    expect(screen.getByLabelText("Thời điểm dòng 1")).toHaveValue("2026-09-30");
    expect(screen.getByLabelText("Thời điểm dòng 2")).toHaveValue(today); // không có trong tệp → Thời điểm đang lọc
    expect(within(table()).getByRole("alert")).toHaveTextContent("Dòng 3 của tệp: ĐVT đếm được: số lượng phải là số nguyên");
  });

  it("Lịch sử: sổ ghi nhận theo khoảng ngày + tổng hợp theo xã; Lịch sử bản ghi mở log", async () => {
    auth.collectApi.materials!.mockResolvedValue({ items: [REC], next_cursor: null });
    const u = userEvent.setup();
    render(<MaterialsScreen />);
    await u.click(screen.getByRole("button", { name: /Lịch sử$/ }));
    await waitFor(() => expect(auth.collectApi.materials).toHaveBeenLastCalledWith({ commune: "91301", from: undefined, to: undefined, material_code: undefined }));
    expect(screen.queryByRole("region", { name: "Bảng nhập vật tư" })).toBeNull();
    expect(screen.getByRole("region", { name: "Tổng hợp vật tư theo xã" })).toBeInTheDocument();
    await u.selectOptions(screen.getByRole("combobox", { name: "Xã/phường" }), "");
    await waitFor(() => expect(auth.collectApi.materials).toHaveBeenLastCalledWith({ commune: undefined, from: undefined, to: undefined, material_code: undefined }));
    await u.click(await screen.findByRole("button", { name: "Lịch sử bản ghi Phân urê 2026-09-30" }));
    await waitFor(() => expect(auth.collectApi.materialLog).toHaveBeenCalledWith(REC.id));
    const log = await screen.findByRole("complementary", { name: "Lịch sử bản ghi" });
    expect(await within(log).findByText("Ghi nhận")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "← Bảng nhập" }));
    expect(table()).toBeInTheDocument();
  });

  it("Cấp Tỉnh chỉ xem: không có bảng nhập, Nạp Excel; xem được tổng hợp các xã", async () => {
    auth.role = "kn_tinh";
    render(<MaterialsScreen />);
    await waitFor(() => expect(auth.collectApi.materials).toHaveBeenCalledWith({ commune: undefined, from: undefined, to: undefined, material_code: undefined }));
    expect(screen.queryByRole("region", { name: "Bảng nhập vật tư" })).toBeNull();
    expect(screen.queryByLabelText("Tệp vật tư (Excel/CSV)")).toBeNull();
    expect(screen.queryByRole("button", { name: "← Bảng nhập" })).toBeNull();
    expect(screen.getByRole("region", { name: "Tổng hợp vật tư theo xã" })).toBeInTheDocument();
  });
});

describe("validateLines", () => {
  it("Thời điểm bắt buộc và không ở tương lai; vật tư phải thuộc danh mục", () => {
    const r = validateLines(
      [
        { key: "a", material_code: "x", quantity: "1", recorded_on: "", note: "" },
        { key: "b", material_code: "phan_ure", quantity: "1", recorded_on: "2999-01-01", note: "" },
      ],
      { phan_ure: "tấn" },
      "2026-10-01",
    );
    expect(r.errors.a).toEqual({ material_code: "Vật tư không có trong danh mục đang dùng.", recorded_on: "Bắt buộc nhập." });
    expect(r.errors.b).toEqual({ recorded_on: "Không được ở tương lai." });
  });
});
