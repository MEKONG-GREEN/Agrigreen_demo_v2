// BF-TT-MACH-WEB (#103, TT-10): Máy móc HTX — Năm SX bắt buộc (1900 – năm hiện tại giờ VN), Số lượng nguyên > 0,
// không trùng loại; lưu số lượng (pf/mechanization) rồi năm (hồ sơ KN riêng #119); lỗi theo trường máy chủ hiện dưới ô.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import { MachinesScreen, machineServerErrors, machineYearError, validateMachineLines, vnYear } from "./DataScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/may-moc",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
}));

const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: {},
    kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

const coop = { ref: "htx-1", code: "HTX01", name: "HTX Mỹ Hòa", commune_code: "91301", active: true };
const YEAR = vnYear();

beforeEach(() => {
  auth.role = "kn_xa";
  auth.collectApi = {
    cooperatives: vi.fn().mockResolvedValue({ items: [coop] }),
    machines: vi.fn().mockResolvedValue({ commune_totals: [] }),
    coopMachines: vi.fn().mockResolvedValue({ cooperative_ref: "htx-1", name: "HTX Mỹ Hòa", lines: [{ machine_type: "may_gat", machine_type_name: "Máy gặt", quantity: 2, note: null }], total: 2 }),
    machineProfile: vi.fn().mockResolvedValue({ cooperative_ref: "htx-1", commune_code: "91301", department_code: "trong_trot", items: [{ machine_type: "may_gat", machine_type_name: "Máy gặt", year_made: 2019 }] }),
    putMachines: vi.fn().mockResolvedValue({ total: 3 }),
    putMachineProfile: vi.fn().mockResolvedValue({ items: [] }),
  };
});

async function openCoop() {
  const u = userEvent.setup();
  render(<MachinesScreen />);
  await waitFor(() => expect(screen.getByRole("option", { name: "HTX Mỹ Hòa" })).toBeInTheDocument());
  await u.selectOptions(screen.getByLabelText("HTX"), "htx-1");
  const table = await screen.findByRole("table", { name: "Khai báo máy" });
  return { u, table };
}

describe("TT-10 quy tắc kiểm (thuần)", () => {
  it("Năm SX: bắt buộc, nguyên, 1900 ≤ năm ≤ năm hiện tại", () => {
    expect(machineYearError("", 2026)).toBe("Bắt buộc nhập.");
    expect(machineYearError("2015,5", 2026)).toBe("Chỉ nhập số nguyên.");
    expect(machineYearError("-2015", 2026)).toBe("Không được nhập số âm.");
    expect(machineYearError("1899", 2026)).toBe("Không được nhỏ hơn 1900.");
    expect(machineYearError("2027", 2026)).toBe("Không được lớn hơn 2026.");
    expect(machineYearError("1900", 2026)).toBeUndefined();
    expect(machineYearError("2026", 2026)).toBeUndefined();
  });

  it("bảng: số lượng nguyên 1 – 1.000.000, trùng loại báo ở dòng sau", () => {
    const e = validateMachineLines(
      [
        { machine_type: "may_gat", quantity: "0", year_made: "2020", note: "" },
        { machine_type: "may_gat", quantity: "1000001", year_made: "2020", note: "" },
        { machine_type: "may_say", quantity: "5", year_made: "", note: "" },
      ],
      2026,
    );
    expect(e).toEqual({ "0.quantity": "Phải lớn hơn 0.", "1.type": "Trùng loại máy — sửa dòng đã có.", "1.quantity": "Không được lớn hơn 1000000.", "2.year": "Bắt buộc nhập." });
  });

  it("lỗi máy chủ: lines[i].quantity (#113) và lines.<loại>.year_made (#119) → đúng ô", () => {
    const rows = [{ machine_type: "may_gat" }, { machine_type: "may_say" }];
    expect(machineServerErrors({ "lines[1].quantity": "số nguyên 1–1.000.000", "lines.may_gat.year_made": "quá năm hiện tại", "lines.drone.year_made": "x", other: "y" }, rows)).toEqual({
      "1.quantity": "số nguyên 1–1.000.000",
      "0.year": "quá năm hiện tại",
    });
  });
});

describe("Máy móc HTX — màn hình (TT-10)", () => {
  it("hiện cột Năm SX lấy từ hồ sơ KN; dòng thiếu năm bị chặn trước khi gọi API", async () => {
    const { u, table } = await openCoop();
    expect(within(table).getByRole("columnheader", { name: "Năm SX" })).toBeInTheDocument();
    expect(screen.getByLabelText("Năm sản xuất dòng 1")).toHaveValue("2019");
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    // Dòng mới chọn sẵn loại chưa khai báo, năm để trống (bắt buộc).
    expect(screen.getByLabelText("Loại máy dòng 2")).not.toHaveValue("may_gat");
    expect(screen.getByLabelText("Năm sản xuất dòng 2")).toHaveValue("");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(within(table).getByRole("alert")).toHaveTextContent("Bắt buộc nhập.");
    expect(auth.collectApi.putMachines).not.toHaveBeenCalled();
    expect(auth.collectApi.putMachineProfile).not.toHaveBeenCalled();
  });

  it("năm tương lai và trùng loại bị chặn; số lượng 0 bị chặn", async () => {
    const { u, table } = await openCoop();
    const year = screen.getByLabelText("Năm sản xuất dòng 1");
    await u.clear(year);
    await u.type(year, String(YEAR + 1));
    await u.tab();
    expect(within(table).getByRole("alert")).toHaveTextContent(`Không được lớn hơn ${YEAR}.`);
    await u.clear(year);
    await u.type(year, "2019");
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await u.selectOptions(screen.getByLabelText("Loại máy dòng 2"), "may_gat");
    await u.type(screen.getByLabelText("Năm sản xuất dòng 2"), "2020");
    await u.clear(screen.getByLabelText("Số lượng dòng 2"));
    await u.type(screen.getByLabelText("Số lượng dòng 2"), "0");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    const alerts = within(table).getAllByRole("alert").map((a) => a.textContent);
    expect(alerts).toEqual(["Trùng loại máy — sửa dòng đã có.", "Phải lớn hơn 0."]);
    expect(auth.collectApi.putMachines).not.toHaveBeenCalled();
  });

  it("hợp lệ: lưu số lượng rồi năm SX của cùng các dòng", async () => {
    const { u } = await openCoop();
    await u.click(screen.getByRole("button", { name: "+ Thêm dòng" }));
    await u.selectOptions(screen.getByLabelText("Loại máy dòng 2"), "may_say");
    await u.clear(screen.getByLabelText("Số lượng dòng 2"));
    await u.type(screen.getByLabelText("Số lượng dòng 2"), "4");
    await u.type(screen.getByLabelText("Năm sản xuất dòng 2"), String(YEAR));
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.collectApi.putMachineProfile).toHaveBeenCalled());
    expect(auth.collectApi.putMachines).toHaveBeenCalledWith("htx-1", [
      { machine_type: "may_gat", quantity: 2, note: null },
      { machine_type: "may_say", quantity: 4, note: null },
    ]);
    expect(auth.collectApi.putMachineProfile).toHaveBeenCalledWith("htx-1", [
      { machine_type: "may_gat", year_made: 2019 },
      { machine_type: "may_say", year_made: YEAR },
    ]);
    expect(auth.collectApi.putMachines!.mock.invocationCallOrder[0]!).toBeLessThan(auth.collectApi.putMachineProfile!.mock.invocationCallOrder[0]!);
    expect(await screen.findByRole("status")).toHaveTextContent("Đã lưu khai báo máy (3 máy).");
  });

  it("máy chủ từ chối năm (#119) → lỗi hiện dưới ô Năm SX của đúng dòng", async () => {
    auth.collectApi.putMachineProfile = vi.fn().mockRejectedValue(
      new ApiProblem({ type: "about:blank", title: "x", status: 422, code: "extension.kn_attrs_year_invalid", errors: [{ field: "lines.may_gat.year_made", reason: "năm sản xuất phải ≤ năm hiện tại" }] }),
    );
    const { u, table } = await openCoop();
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await within(table).findByRole("alert")).toHaveTextContent("năm sản xuất phải ≤ năm hiện tại");
    expect(screen.getByLabelText("Năm sản xuất dòng 1")).toHaveAttribute("aria-invalid", "true");
  });

  it("Cấp Quốc gia chỉ xem: ô bị khóa, không có nút Lưu", async () => {
    auth.role = "kn_tw";
    await openCoop();
    expect(screen.getByLabelText("Năm sản xuất dòng 1")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
  });
});
