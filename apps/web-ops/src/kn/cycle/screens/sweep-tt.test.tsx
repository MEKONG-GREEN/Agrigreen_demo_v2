// BF-SWEEP-TT (#100): màn Trồng trọt dùng UI-KIT — ô số không nhận số âm, trường bắt buộc báo lỗi theo trường và
// không gọi API khi còn lỗi, thêm/sửa trên trang riêng (DetailPage) thay pop-up.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AssignmentEditScreen, UserCreateScreen } from "./AdminScreens";
import { CoopCreateScreen, CoopsScreen, HouseholdCreateScreen, MachinesScreen } from "./DataScreens";
import { BatchesScreen, PublishScreen } from "./TinhScreens";

const push = vi.fn();
const params: Record<string, string> = {};
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/htx",
  useRouter: () => ({ push, replace: vi.fn() }),
  useParams: () => params,
}));

const COMMUNES = [
  { code: "91301", name: "Mỹ Hòa Hưng" },
  { code: "91302", name: "Bình Đức" },
];
const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>>; knAdminApi: Record<string, ReturnType<typeof vi.fn>>; cycleApi: Record<string, ReturnType<typeof vi.fn>> } = {
  role: "kn_xa",
  collectApi: {},
  knAdminApi: {},
  cycleApi: {},
};
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: {},
    kn: { context: { role: auth.role, communes: COMMUNES, department_code: "trong_trot" } },
    collectApi: auth.collectApi,
    knAdminApi: auth.knAdminApi,
    cycleApi: auth.cycleApi,
  }),
}));

const coop = { ref: "htx-1", code: "HTX01", name: "HTX Mỹ Hòa", commune_code: "91301", active: true, area_ha: "120", household_count: 2, representative: null, updated_at: "2026-10-01T00:00:00Z" };

beforeEach(() => {
  push.mockReset();
  for (const k of Object.keys(params)) delete params[k];
  auth.role = "kn_xa";
  auth.collectApi = {
    cooperatives: vi.fn().mockResolvedValue({ items: [coop] }),
    cooperative: vi.fn().mockResolvedValue(coop),
    createCooperative: vi.fn().mockResolvedValue({ ...coop, ref: "htx-new" }),
    addHousehold: vi.fn().mockResolvedValue({ ref: "hh-1" }),
    machines: vi.fn().mockResolvedValue({ commune_totals: [] }),
    coopMachines: vi.fn().mockResolvedValue({ cooperative_ref: "htx-1", name: "HTX Mỹ Hòa", lines: [{ machine_type: "may_gat", machine_type_name: "Máy gặt", quantity: 2, note: null }], total: 2 }),
    putMachines: vi.fn().mockResolvedValue({ total: 3 }),
    machineProfile: vi.fn().mockResolvedValue({ items: [{ machine_type: "may_gat", year_made: 2019 }] }),
    putMachineProfile: vi.fn().mockResolvedValue({ items: [] }),
    materials: vi.fn().mockResolvedValue({ items: [] }),
    materialSummary: vi.fn().mockResolvedValue({ items: [] }),
    recordMaterials: vi.fn().mockResolvedValue({ items: [] }),
  };
});

describe("Dữ liệu xã — Thêm HTX, nông hộ trên trang riêng (R-A2, R-A4, R-A1)", () => {
  it("danh sách HTX không còn pop-up: nút Thêm HTX dẫn tới trang riêng", async () => {
    render(<CoopsScreen />);
    expect(screen.getByRole("link", { name: "Thêm HTX" })).toHaveAttribute("href", "/kn/du-lieu/htx/them-moi");
    expect(await screen.findByText("HTX Mỹ Hòa")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("Thêm HTX: thiếu trường bắt buộc hoặc diện tích âm → lỗi theo trường, không gọi API", async () => {
    const u = userEvent.setup();
    render(<CoopCreateScreen />);
    expect(screen.getByRole("heading", { level: 1, name: /Thêm HTX/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/du-lieu/htx");
    await u.type(screen.getByLabelText(/Diện tích/), "-5");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
    expect(alerts.filter((t) => t === "Bắt buộc nhập.")).toHaveLength(4); // Tên, Xã/phường, Người đại diện, Điện thoại (TT-08)
    expect(alerts).toContain("Không được nhập số âm.");
    expect(auth.collectApi.createCooperative).not.toHaveBeenCalled();
  });

  it("Thêm HTX hợp lệ: gửi số đã chuẩn hóa rồi mở chi tiết HTX mới", async () => {
    const u = userEvent.setup();
    render(<CoopCreateScreen />);
    await u.type(screen.getByLabelText(/Tên HTX/), "HTX Bình Đức");
    await u.selectOptions(screen.getByLabelText(/Xã\/phường/), "91302");
    await u.type(screen.getByLabelText(/Diện tích/), "1.234,5");
    await u.type(screen.getByLabelText(/Người đại diện/), "Nguyễn Văn A");
    await u.type(screen.getByLabelText(/Điện thoại/), "0912345678");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/du-lieu/htx/htx-new"));
    expect(auth.collectApi.createCooperative).toHaveBeenCalledWith({ name: "HTX Bình Đức", commune_code: "91302", area_ha: "1234.5", representative: { full_name: "Nguyễn Văn A", phone: "0912345678" } });
  });

  it("Thêm nông hộ: diện tích âm bị chặn; hợp lệ thì gọi API và quay về chi tiết HTX", async () => {
    params.id = "htx-1";
    const u = userEvent.setup();
    render(<HouseholdCreateScreen />);
    expect(await screen.findByRole("link", { name: /HTX Mỹ Hòa/ })).toHaveAttribute("href", "/kn/du-lieu/htx/htx-1");
    await u.type(screen.getByLabelText(/Chủ hộ/), "Trần Thị B");
    await u.type(screen.getByLabelText(/Số điện thoại/), "0912000111");
    await u.type(screen.getByLabelText(/Diện tích canh tác/), "-1");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Không được nhập số âm.");
    expect(auth.collectApi.addHousehold).not.toHaveBeenCalled();
    await u.clear(screen.getByLabelText(/Diện tích canh tác/));
    await u.type(screen.getByLabelText(/Diện tích canh tác/), "2,5");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/du-lieu/htx/htx-1"));
    expect(auth.collectApi.addHousehold).toHaveBeenCalledWith("htx-1", { holder: { full_name: "Trần Thị B", phone: "0912000111" }, cultivated_area_ha: "2.5" });
  });

  it("Máy móc: bảng có STT; số lượng phải là số nguyên > 0 (TT-10) trước khi lưu", async () => {
    const u = userEvent.setup();
    render(<MachinesScreen />);
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Mỹ Hòa" })).toBeInTheDocument());
    await u.selectOptions(screen.getByLabelText("HTX"), "htx-1");
    const qty = await screen.findByLabelText("Số lượng dòng 1");
    const table = screen.getByRole("table", { name: "Khai báo máy" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    for (const [raw, msg] of [
      ["0", "Phải lớn hơn 0."],
      ["-2", "Không được nhập số âm."],
      ["1,5", "Chỉ nhập số nguyên."],
    ] as const) {
      await u.clear(qty);
      await u.type(qty, raw);
      await u.click(screen.getByRole("button", { name: "Lưu" }));
      expect(within(table).getByRole("alert")).toHaveTextContent(msg);
    }
    expect(auth.collectApi.putMachines).not.toHaveBeenCalled();
    await u.clear(qty);
    await u.type(qty, "3");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.collectApi.putMachines).toHaveBeenCalledWith("htx-1", [{ machine_type: "may_gat", quantity: 3, note: null }]));
  });
  // Vật tư (bảng nhập nhiều dòng, BF-TT-MAT #105): MaterialsScreen.test.tsx.
});

describe("Quản trị người dùng — trang riêng thay pop-up", () => {
  beforeEach(() => {
    auth.role = "admin";
    auth.knAdminApi = {
      orgTree: vi.fn().mockResolvedValue({ items: [{ ref: "org-tw", children: [{ ref: "org-ag", children: [] }] }] }),
      createKnUser: vi.fn().mockResolvedValue({ user: { id: "u-new" }, assignments: [{}], warnings: [] }),
      user: vi.fn().mockResolvedValue({ id: "u-1", first_name: "An", last_name: "Nguyễn Văn", email: "an@vi-du.vn" }),
      assignments: vi.fn().mockResolvedValue({ items: [] }),
      grant: vi.fn().mockResolvedValue({}),
    };
  });

  it("Tạo tài khoản: thiếu Họ tên, Email → lỗi theo trường, không gọi API; đủ thì tạo và mở chi tiết", async () => {
    const u = userEvent.setup();
    render(<UserCreateScreen />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled());
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByRole("alert").map((a) => a.textContent)).toEqual(["Bắt buộc nhập.", "Bắt buộc nhập."]);
    expect(auth.knAdminApi.createKnUser).not.toHaveBeenCalled();
    await u.type(screen.getByLabelText(/Họ tên/), "Nguyễn Văn An");
    await u.type(screen.getByLabelText(/^Email/), "an@vi-du.vn");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/quan-tri/nguoi-dung/u-new"));
    expect(auth.knAdminApi.createKnUser!.mock.calls[0]![0]).toMatchObject({ full_name: "Nguyễn Văn An", email: "an@vi-du.vn", assignments: [{ role: "kn_xa", org_ref: "org-ag" }] });
  });

  it("Thêm vai trò: trang riêng, Hủy quay về chi tiết tài khoản, Lưu gọi grant", async () => {
    params.id = "u-1";
    const u = userEvent.setup();
    render(<AssignmentEditScreen />);
    expect(screen.getByRole("heading", { level: 1, name: /Thêm vai trò/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute("href", "/kn/quan-tri/nguoi-dung/u-1");
    await screen.findByLabelText("Vai trò");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.knAdminApi.grant).toHaveBeenCalledWith("u-1", expect.objectContaining({ role: "kn_xa", department_codes: ["trong_trot"] })));
    expect(push).toHaveBeenCalledWith("/kn/quan-tri/nguoi-dung/u-1");
  });
});

describe("Phát hành — Đợt đã phát hành trên trang riêng", () => {
  it("bảng có STT; thu hồi đợt bắt buộc lý do ≥ 10 ký tự trước khi gọi API", async () => {
    auth.role = "kn_tinh";
    auth.cycleApi = {
      batches: vi.fn().mockResolvedValue({ items: [{ id: "b1", season_label: "Thu Đông 2026", forms: [{ code: "T01" }], communes: ["91301"], period_count: 4, status: "issued" }] }),
      withdraw: vi.fn().mockResolvedValue({}),
    };
    const u = userEvent.setup();
    render(<BatchesScreen />);
    const table = await screen.findByRole("table", { name: "Đợt đã phát hành" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    await u.click(within(table).getByRole("button", { name: "Thu hồi" }));
    const dlg = screen.getByRole("dialog");
    await u.type(within(dlg).getByLabelText(/Lý do thu hồi/), "ngắn");
    await u.click(within(dlg).getByRole("button", { name: "Thu hồi" }));
    expect(within(dlg).getByRole("alert")).toHaveTextContent("Tối thiểu 10 ký tự.");
    expect(auth.cycleApi.withdraw).not.toHaveBeenCalled();
    await u.type(within(dlg).getByLabelText(/Lý do thu hồi/), " vì phát hành nhầm biểu");
    await u.click(within(dlg).getByRole("button", { name: "Thu hồi" }));
    await waitFor(() => expect(auth.cycleApi.withdraw).toHaveBeenCalledWith("b1", "ngắn vì phát hành nhầm biểu"));
  });
});

// Codex review #100 (P2): ô "Tỉnh giao" của biểu đã bỏ chọn bị ẩn nhưng lỗi của nó vẫn chặn Xem trước / Phát hành.
describe("Phát hành — Tỉnh giao chỉ kiểm/gửi cho biểu đang chọn", () => {
  const FORMS = ["T01", "T02"].map((code) => ({ template_id: code, version_id: `${code}v1`, code, name: code, version: 1, report_types: ["weekly"], period_block: false, completeness: "complete", provincial_columns: [{ letter: "H", title: "Tỉnh giao", unit: "ha" }] }));
  const PREVIEW = { report_type: "weekly", period_type: "week", period_rule: "week_start=wed", calendar_id: "trong_trot", calendar_version: 2, calendar_source: "builtin", deadline: { offset_days: 1, time: "12:00" }, close_after_days: 1, forms: FORMS.map((f) => ({ ...f, slots: 1 })), periods: [], communes: [{ commune_code: "91301", region_code: "LX", region_name: "Long Xuyên", officer_count: 1 }], communes_without_officer: [], provincial_columns: [], form_count: 2, commune_count: 1, period_count: 1, task_count: 2, warnings: [] };
  const NEG = "Không được nhập số âm.";
  const setup = async () => {
    auth.role = "kn_tinh";
    auth.cycleApi = { reportForms: vi.fn().mockResolvedValue({ items: FORMS }), batches: vi.fn().mockResolvedValue({ items: [] }), preview: vi.fn().mockResolvedValue(PREVIEW) };
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    await waitFor(() => expect(auth.cycleApi.preview).toHaveBeenCalledTimes(1));
    return u;
  };

  it("bỏ chọn T02 có ô âm → Xem trước T01 vẫn gọi API, không gửi giá trị của T02", async () => {
    const u = await setup();
    await u.type(await screen.findByLabelText("T02 H 91301"), "-1");
    await u.type(screen.getByLabelText("T01 H 91301"), "5");
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    expect(screen.queryByLabelText("T02 H 91301")).toBeNull();
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    await waitFor(() => expect(auth.cycleApi.preview).toHaveBeenCalledTimes(2));
    const sent = auth.cycleApi.preview!.mock.calls[1]![0];
    expect(sent).toMatchObject({ template_ids: ["T01"], provincial_values: { T01: { "91301": { H: "5" } } } });
    expect(sent.provincial_values).not.toHaveProperty("T02");
    expect(screen.queryByText(NEG)).toBeNull();
  });

  it("biểu đang chọn có ô -1 → vẫn chặn, hiện lỗi dưới ô, không gọi API", async () => {
    const u = await setup();
    await u.type(await screen.findByLabelText("T02 H 91301"), "-1");
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    expect(await screen.findByText(NEG)).toBeInTheDocument();
    expect(screen.getByLabelText("T02 H 91301")).toHaveAttribute("aria-invalid", "true");
    expect(auth.cycleApi.preview).toHaveBeenCalledTimes(1);
  });

  it("chọn lại T02 sau khi bỏ chọn → ô trống, không hồi sinh giá trị âm cũ hay lỗi cũ", async () => {
    const u = await setup();
    await u.type(await screen.findByLabelText("T02 H 91301"), "-1");
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    expect(await screen.findByText(NEG)).toBeInTheDocument();
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    const field = await screen.findByLabelText("T02 H 91301");
    expect(field).toHaveValue("");
    expect(field).not.toHaveAttribute("aria-invalid");
    expect(screen.queryByText(NEG)).toBeNull();
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    await waitFor(() => expect(auth.cycleApi.preview).toHaveBeenCalledTimes(2));
    expect(auth.cycleApi.preview!.mock.calls[1]![0]).toMatchObject({ template_ids: ["T01", "T02"] });
  });
});
