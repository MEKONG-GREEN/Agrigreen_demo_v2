// KNSPLIT-2-KN-RECORDS-HTX-READ (#162, mục 1): bộ chọn HTX của Máy móc/Thửa đọc hồ sơ KN riêng (/v1/kn/producer-records)
// theo trang và tìm của máy chủ (không cắt trang đầu); dữ liệu cũ tham chiếu HTX nền tảng đối chiếu qua htx_ref, chưa liên
// kết → "chưa liên kết hồ sơ KN"; Cấp Tỉnh vẫn chỉ xem thửa (#104); web KN không còn GET /v1/kn/cooperatives.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createCollectApi } from "@/kn/cycle/api";
import { MachinesScreen } from "./DataScreens";
import { HTX_PICKER_PAGE, currentHtxLabel, htxOptionOf } from "./HtxRecordPicker";
import { knHtxRecord, recordsMock } from "./htxRecordsFixture";
import { PlotEditScreen } from "./PlotScreens";

const params: Record<string, string> = {};
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/may-moc",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => params,
}));
vi.mock("@/components/LayerMap", () => ({ LayerMap: () => null }));

const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: { GET: vi.fn().mockResolvedValue({ data: { name: "Mỹ Hòa Hưng" } }) },
    kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

const pad = (i: number) => String(i).padStart(3, "0");
// 120 hồ sơ HTX liên kết HTX đã công bố (htx-001…htx-120) + 1 hồ sơ chưa liên kết.
const MANY = Array.from({ length: 120 }, (_, k) => knHtxRecord(`kn-${pad(k + 1)}`, `HTX Số ${pad(k + 1)}`, `htx-${pad(k + 1)}`));
const UNLINKED_REC = knHtxRecord("kn-x", "HTX Chưa Liên Kết", null);
const machinesOf = (ref: string, name: string) => ({ cooperative_ref: ref, name, commune_code: "91301", lines: [{ machine_type: "may_gat", machine_type_name: "Máy gặt", quantity: 2, note: null }], total: 2 });

const plot = {
  ref: "plot-1",
  code: "TH-01",
  lot_name: "Lô 1",
  commune_code: "91301",
  cooperative_ref: "htx-001",
  farmer: { label: "Nguyễn V. A." },
  note: null,
  area_ha: "1.2345",
  declared_area_ha: null,
  area_deviation_pct: null,
  status: "active",
  version: 1,
  boundary_id: "b-1",
  updated_at: "2026-10-01T00:00:00Z",
  geometry: { type: "Polygon", coordinates: [[[105.4, 10.38], [105.401, 10.38], [105.401, 10.381], [105.4, 10.38]]] },
};

const htxSelect = () => screen.getByLabelText("HTX") as HTMLSelectElement;
const optionLabels = () => within(htxSelect()).getAllByRole("option").map((o) => o.textContent);

beforeEach(() => {
  auth.role = "kn_xa";
  for (const k of Object.keys(params)) delete params[k];
  auth.collectApi = {
    records: recordsMock([...MANY, UNLINKED_REC]),
    htxCandidates: vi.fn().mockResolvedValue({ items: [{ htx_ref: "htx-legacy", name: "HTX Cũ Nền Tảng", commune_code: "91301", linked_ref: null }] }),
    machines: vi.fn().mockResolvedValue({ cooperatives: [], commune_totals: [] }),
    coopMachines: vi.fn().mockImplementation(async (ref: string) => machinesOf(ref, ref === "htx-legacy" ? "HTX Cũ Nền Tảng" : `HTX ${ref}`)),
    machineProfile: vi.fn().mockResolvedValue({ items: [] }),
    putMachines: vi.fn().mockResolvedValue({ total: 2 }),
    putMachineProfile: vi.fn().mockResolvedValue({ items: [] }),
    plot: vi.fn().mockResolvedValue(plot),
    plotProfile: vi.fn().mockResolvedValue({ plot_ref: "plot-1", status: "active", crop_code: "LUA", crop_name: "Lúa" }),
    plotLog: vi.fn().mockResolvedValue({ items: [] }),
    plotShapes: vi.fn().mockResolvedValue({ type: "FeatureCollection", features: [] }),
    crops: vi.fn().mockResolvedValue({ items: [{ code: "LUA", name: "Lúa", attrs: {}, active: true }] }),
    updatePlot: vi.fn().mockResolvedValue(plot),
    putPlotCrop: vi.fn().mockResolvedValue({}),
  };
});

describe("#162 quy tắc bộ chọn (thuần)", () => {
  it("hồ sơ liên kết → giá trị htx_ref; chưa liên kết HTX đã công bố → mục khóa; dữ liệu cũ chưa có hồ sơ → nhãn rõ", () => {
    expect(htxOptionOf(MANY[0]!)).toEqual({ value: "htx-001", label: "HTX Số 001" });
    expect(htxOptionOf(UNLINKED_REC)).toEqual({ value: "kn:kn-x", label: "HTX Chưa Liên Kết — chưa liên kết HTX đã công bố", disabled: true });
    expect(htxOptionOf({ ...MANY[0]!, status: "inactive" }).label).toBe("HTX Số 001 (ngừng hoạt động)");
    expect(currentHtxLabel(null, "HTX Cũ")).toBe("HTX Cũ — chưa liên kết hồ sơ KN");
    expect(currentHtxLabel(null)).toBe("HTX hiện tại — chưa liên kết hồ sơ KN");
    expect(currentHtxLabel(MANY[1]!)).toBe("HTX Số 002");
  });

  it("client web KN không còn GET legacy /v1/kn/cooperatives", () => {
    const api = createCollectApi({ baseUrl: "http://x", getAccessToken: () => "t", getContextId: () => "c", fetch: async () => new Response("{}") });
    expect(api).not.toHaveProperty("cooperatives");
  });
});

describe("Máy móc HTX — bộ chọn theo hồ sơ KN (#162)", () => {
  it("quá 50 hồ sơ: tải thêm theo next_cursor của máy chủ tới hết, chọn được hồ sơ thứ 120", async () => {
    const u = userEvent.setup();
    render(<MachinesScreen />);
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Số 050" })).toBeInTheDocument());
    expect(screen.queryByRole("option", { name: "HTX Số 051" })).toBeNull();
    expect(auth.collectApi.records).toHaveBeenCalledWith({ kind: "cooperative", status: "active", limit: HTX_PICKER_PAGE });
    await u.click(screen.getByRole("button", { name: "Tải thêm HTX" }));
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Số 100" })).toBeInTheDocument());
    expect(auth.collectApi.records).toHaveBeenLastCalledWith({ kind: "cooperative", status: "active", limit: HTX_PICKER_PAGE, cursor: "kn-050" });
    await u.click(screen.getByRole("button", { name: "Tải thêm HTX" }));
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Số 120" })).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Tải thêm HTX" })).toBeNull();
    // Hồ sơ chưa liên kết HTX đã công bố hiện nhưng không chọn được.
    expect(screen.getByRole("option", { name: /HTX Chưa Liên Kết — chưa liên kết HTX đã công bố/ })).toBeDisabled();
    await u.selectOptions(htxSelect(), "htx-120");
    expect(await screen.findByRole("table", { name: "Khai báo máy" })).toBeInTheDocument();
    expect(auth.collectApi.coopMachines).toHaveBeenCalledWith("htx-120");
  });

  it("tìm theo tên gửi q lên máy chủ (không lọc trang đầu ở client)", async () => {
    const u = userEvent.setup();
    render(<MachinesScreen />);
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Số 001" })).toBeInTheDocument());
    await u.type(screen.getByLabelText("Tìm HTX theo tên"), "Số 11");
    await waitFor(() => expect(optionLabels()).toEqual(["Chọn HTX", ...Array.from({ length: 10 }, (_, k) => `HTX Số 11${k}`)]));
    expect(auth.collectApi.records).toHaveBeenLastCalledWith({ kind: "cooperative", status: "active", q: "Số 11", limit: HTX_PICKER_PAGE });
  });

  it("HTX đã khai báo máy (tham chiếu HTX nền tảng): liên kết → tên hồ sơ KN; chưa liên kết → ghi rõ và vẫn xem được", async () => {
    auth.collectApi.machines = vi.fn().mockResolvedValue({
      cooperatives: [machinesOf("htx-007", "Tên nền tảng 007"), machinesOf("htx-legacy", "HTX Cũ Nền Tảng")],
      commune_totals: [],
    });
    const u = userEvent.setup();
    render(<MachinesScreen />);
    const panel = await screen.findByRole("complementary", { name: "HTX đã khai báo máy" });
    await waitFor(() => expect(within(panel).getByRole("button", { name: "HTX Số 007" })).toBeInTheDocument());
    const legacy = within(panel).getByRole("button", { name: "HTX Cũ Nền Tảng" });
    expect(within(panel).getByText("chưa liên kết hồ sơ KN")).toBeInTheDocument();
    await u.click(legacy);
    expect(await screen.findByRole("table", { name: "Khai báo máy" })).toBeInTheDocument();
    expect(htxSelect()).toHaveValue("htx-legacy");
    await waitFor(() => expect(htxSelect().selectedOptions[0]!.textContent).toBe("HTX Cũ Nền Tảng — chưa liên kết hồ sơ KN"));
    expect(screen.getByRole("note")).toHaveTextContent("chưa liên kết hồ sơ KN");
    // Đối chiếu qua hồ sơ KN (records … htx_ref), không qua GET legacy.
    expect(auth.collectApi.records).toHaveBeenCalledWith({ kind: "cooperative", htx_ref: "htx-legacy", status: "all", limit: 1 });
  });
});

describe("Thửa — ô HTX theo hồ sơ KN (#162)", () => {
  beforeEach(() => {
    params.id = "plot-1";
  });

  it("thửa cũ tham chiếu HTX có hồ sơ KN liên kết: hiện tên hồ sơ; lưu không đổi HTX thì không gửi cooperative_ref", async () => {
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    const name = await screen.findByLabelText(/Tên thửa/);
    await waitFor(() => expect(htxSelect().selectedOptions[0]!.textContent).toBe("HTX Số 001"));
    await u.type(name, " A");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.collectApi.updatePlot).toHaveBeenCalledWith("plot-1", { lot_name: "Lô 1 A" }));
  });

  it("thửa cũ tham chiếu HTX chưa có hồ sơ KN: tên HTX đã công bố + \"chưa liên kết hồ sơ KN\"", async () => {
    auth.collectApi.plot = vi.fn().mockResolvedValue({ ...plot, cooperative_ref: "htx-legacy" });
    render(<PlotEditScreen />);
    await screen.findByLabelText(/Tên thửa/);
    await waitFor(() => expect(htxSelect().selectedOptions[0]!.textContent).toBe("HTX Cũ Nền Tảng — chưa liên kết hồ sơ KN"));
    expect(auth.collectApi.htxCandidates).toHaveBeenCalledWith("91301");
  });

  it("đổi HTX sang hồ sơ ở trang sau: gửi htx_ref của hồ sơ đã chọn", async () => {
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    await screen.findByLabelText(/Tên thửa/);
    await waitFor(() => expect(screen.getByRole("button", { name: "Tải thêm HTX" })).toBeEnabled());
    await u.click(screen.getByRole("button", { name: "Tải thêm HTX" }));
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX Số 075" })).toBeInTheDocument());
    await u.selectOptions(htxSelect(), "htx-075");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(auth.collectApi.updatePlot).toHaveBeenCalledWith("plot-1", { cooperative_ref: "htx-075" }));
  });

  it("Cấp Tỉnh chỉ xem (#104): ô HTX khóa, vẫn hiện tên hồ sơ KN; không tìm/tải danh sách, không Lưu", async () => {
    auth.role = "kn_tinh";
    render(<PlotEditScreen />);
    await screen.findByLabelText(/Tên thửa/);
    expect(htxSelect()).toBeDisabled();
    await waitFor(() => expect(htxSelect().selectedOptions[0]!.textContent).toBe("HTX Số 001"));
    expect(screen.queryByLabelText("Tìm HTX theo tên")).toBeNull();
    expect(screen.queryByRole("button", { name: "Tải thêm HTX" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    expect(auth.collectApi.records!.mock.calls.every(([q]) => (q as { htx_ref?: string }).htx_ref === "htx-001")).toBe(true);
  });
});
