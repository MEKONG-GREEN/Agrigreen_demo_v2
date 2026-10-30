// BF-TT-PLOT-WEB (#104, TT-11): Thửa ruộng — sửa được thửa; bắt buộc Tên thửa, Chủ hộ, Xã/phường, Cây trồng; diện tích
// tính từ hình (chỉ đọc); chồng lấn bị máy chủ chặn (409 geo.plot_overlap) và web báo, không ghi tiếp thuộc tính/cây trồng.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IDEMPOTENCY_HEADER } from "@/api/idempotency";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnCollect } from "@/api/mock/kn-collect";
import { ApiProblem } from "@/api/problem";
import { createCollectApi } from "@/kn/cycle/api";
import type { LngLat } from "@/kn/cycle/plotGeometry";
import { PlotCreateScreen, PlotEditScreen, PlotsScreen, drawnAreaHa, outerRing, plotDraftError, plotUpdateBody } from "./PlotScreens";
import { knHtxRecord, recordsMock } from "./htxRecordsFixture";

const push = vi.fn();
const params: Record<string, string> = {};
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/ve-thua",
  useRouter: () => ({ push, replace: vi.fn() }),
  useParams: () => params,
}));
// Bản đồ MapLibre không chạy trong jsdom: vẽ bằng ô "Thêm điểm theo toạ độ" của PlotDrawPanel (đường bàn phím có sẵn).
vi.mock("@/components/LayerMap", () => ({ LayerMap: () => null }));

const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: { GET: vi.fn().mockResolvedValue({ data: { name: "Mỹ Hòa Hưng" } }) },
    kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

// ~1,23 ha quanh (105.4, 10.38).
const SQUARE: LngLat[] = [
  [105.4, 10.38],
  [105.401, 10.38],
  [105.401, 10.381],
  [105.4, 10.381],
];
const plot = {
  ref: "plot-1",
  code: "TH-01",
  lot_name: "Lô 1",
  commune_code: "91301",
  cooperative_ref: "htx-1",
  farmer: { label: "Nguyễn V. A." },
  note: null,
  area_ha: "1.2345",
  declared_area_ha: null,
  area_deviation_pct: null,
  status: "active",
  version: 1,
  boundary_id: "b-1",
  updated_at: "2026-10-01T00:00:00Z",
  geometry: { type: "Polygon", coordinates: [[...SQUARE, SQUARE[0]]] },
};
const CROPS = { items: [{ code: "RAU_MAU", name: "Rau màu", attrs: { is_group: "true" }, active: true }, { code: "LUA", name: "Lúa", attrs: {}, active: true }, { code: "NGO", name: "Ngô", attrs: {}, active: true }] };
const overlap = () => new ApiProblem({ type: "about:blank", title: "Ranh giới bị chồng lấn với lô đã đăng ký — không lưu được, vui lòng vẽ lại.", status: 409, code: "geo.plot_overlap" });

beforeEach(() => {
  push.mockReset();
  for (const k of Object.keys(params)) delete params[k];
  auth.role = "kn_xa";
  auth.collectApi = {
    plots: vi.fn().mockResolvedValue({ items: [plot, { ...plot, ref: "plot-2", code: "TH-02", lot_name: "Lô 2" }], next_cursor: null }),
    plotCrops: vi.fn().mockResolvedValue({ items: [{ plot_ref: "plot-1", crop_code: "LUA", crop_name: "Lúa" }], next_cursor: null }),
    plot: vi.fn().mockResolvedValue(plot),
    plotProfile: vi.fn().mockResolvedValue({ plot_ref: "plot-1", status: "active", crop_code: "LUA", crop_name: "Lúa" }),
    plotLog: vi.fn().mockResolvedValue({ items: [] }),
    plotShapes: vi.fn().mockResolvedValue({ type: "FeatureCollection", features: [] }),
    records: recordsMock([knHtxRecord("kn-1", "HTX Mỹ Hòa", "htx-1")]),
    htxCandidates: vi.fn().mockResolvedValue({ items: [] }),
    crops: vi.fn().mockResolvedValue(CROPS),
    createPlot: vi.fn().mockResolvedValue({ ...plot, ref: "plot-new" }),
    updatePlot: vi.fn().mockResolvedValue(plot),
    correctPlotBoundary: vi.fn().mockResolvedValue(plot),
    putPlotCrop: vi.fn().mockResolvedValue({}),
  };
});

async function draw(u: ReturnType<typeof userEvent.setup>, coords: LngLat[]) {
  const input = screen.getByLabelText(/Thêm điểm theo toạ độ/);
  for (const [x, y] of coords) {
    await u.clear(input);
    await u.type(input, `${x}, ${y}{Enter}`);
  }
}

describe("TT-11 quy tắc (thuần)", () => {
  it("diện tích tính từ hình; hình thiếu/thẳng hàng/quá nhỏ bị chặn", () => {
    expect(drawnAreaHa(SQUARE)).toBeGreaterThan(1.2);
    expect(drawnAreaHa(SQUARE)).toBeLessThan(1.25);
    expect(plotDraftError([])).toBe("Bắt buộc vẽ ranh giới thửa trên bản đồ.");
    expect(plotDraftError(SQUARE.slice(0, 2))).toBe("Vùng cần ít nhất 3 điểm phân biệt.");
    expect(plotDraftError([[105, 10], [105, 10.001], [105, 10.002]])).toBe("Các điểm thẳng hàng — không tạo thành vùng.");
    expect(plotDraftError([[105, 10], [105.000001, 10], [105.000001, 10.000001]])).toBe("Diện tích phải từ 0,001 ha.");
    expect(plotDraftError(SQUARE)).toBeUndefined();
  });

  it("vòng ngoài bỏ điểm khép; Polygon và MultiPolygon", () => {
    expect(outerRing(plot.geometry)).toEqual(SQUARE);
    expect(outerRing({ type: "MultiPolygon", coordinates: [[[...SQUARE, SQUARE[0]]]] })).toEqual(SQUARE);
  });

  it("PUT chỉ gửi trường đã đổi; Chủ hộ trống = giữ nguyên; không đổi gì → không gọi", () => {
    const base = { lot_name: "Lô 1", farmer_name: "", cooperative_ref: "htx-1", note: "" };
    expect(plotUpdateBody(plot, base)).toBeNull();
    expect(plotUpdateBody(plot, { ...base, lot_name: " Lô 1A ", farmer_name: "Trần Văn B", cooperative_ref: "" })).toEqual({ lot_name: "Lô 1A", farmer_name: "Trần Văn B", cooperative_ref: null });
  });
});

describe("Danh sách thửa", () => {
  it("có cột Cây trồng (thiếu → Chưa khai báo), nút Thêm thửa và liên kết Sửa", async () => {
    render(<PlotsScreen />);
    const table = await screen.findByRole("table", { name: "Thửa ruộng" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(table).getByText("Lúa")).toBeInTheDocument();
    expect(within(table).getByText("Chưa khai báo")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Thêm thửa" })).toHaveAttribute("href", "/kn/du-lieu/ve-thua/them-moi");
    expect(within(table).getByRole("link", { name: "Sửa thửa TH-01" })).toHaveAttribute("href", "/kn/du-lieu/ve-thua/plot-1");
  });

  it("Cấp Quốc gia chỉ xem: không có Thêm thửa, chỉ liên kết Xem", async () => {
    auth.role = "kn_tw";
    render(<PlotsScreen />);
    const table = await screen.findByRole("table", { name: "Thửa ruộng" });
    expect(screen.queryByRole("link", { name: "Thêm thửa" })).toBeNull();
    expect(within(table).getByRole("link", { name: "Xem thửa TH-01" })).toBeInTheDocument();
  });

  // Review Codex #104 [P2]: máy chủ chỉ cho Cấp Xã/Admin ghi thửa và cây trồng — Cấp Tỉnh cũng chỉ xem.
  it("Cấp Tỉnh chỉ xem: không có Thêm thửa/Sửa, chỉ liên kết Xem", async () => {
    auth.role = "kn_tinh";
    render(<PlotsScreen />);
    const table = await screen.findByRole("table", { name: "Thửa ruộng" });
    expect(screen.queryByRole("link", { name: "Thêm thửa" })).toBeNull();
    expect(within(table).queryByRole("link", { name: /^Sửa thửa/ })).toBeNull();
    expect(within(table).getByRole("link", { name: "Xem thửa TH-01" })).toHaveAttribute("href", "/kn/du-lieu/ve-thua/plot-1");
  });
});

describe("Thửa — Cấp Tỉnh chỉ xem (review Codex #104)", () => {
  it("vào thẳng URL Thêm thửa: không có form, không có nút Lưu, không gọi API ghi", async () => {
    auth.role = "kn_tinh";
    render(<PlotCreateScreen />);
    expect(await screen.findByText(/Chỉ Cấp Xã được thêm thửa/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    expect(screen.queryByLabelText(/Tên thửa/)).toBeNull();
    expect(auth.collectApi.createPlot).not.toHaveBeenCalled();
  });

  it("trang thửa: xem được dữ liệu, ô bị khóa, không có Lưu/Sửa ranh giới", async () => {
    auth.role = "kn_tinh";
    params.id = "plot-1";
    render(<PlotEditScreen />);
    const name = await screen.findByLabelText(/Tên thửa/);
    expect(name).toHaveValue("Lô 1");
    expect(name).toBeDisabled();
    expect(screen.getByLabelText(/Cây trồng/)).toBeDisabled();
    expect(screen.getByText(/Chỉ xem — chỉ Cấp Xã sửa thửa/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Sửa ranh giới" })).toBeNull();
    expect(screen.getByRole("heading", { name: /^Thửa TH-01/ })).toBeInTheDocument();
  });
});

describe("Thêm thửa", () => {
  it("thiếu trường bắt buộc và chưa vẽ → lỗi theo trường, không gọi API", async () => {
    const u = userEvent.setup();
    render(<PlotCreateScreen />);
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
    expect(alerts.filter((t) => t === "Bắt buộc nhập.")).toHaveLength(3); // Tên thửa, Chủ hộ, Cây trồng (Xã tự chọn khi chỉ một xã)
    expect(alerts).toContain("Bắt buộc vẽ ranh giới thửa trên bản đồ.");
    expect(screen.getByLabelText(/Diện tích tự tính/)).toHaveAttribute("readonly");
    expect(auth.collectApi.createPlot).not.toHaveBeenCalled();
  });

  it("hợp lệ: diện tích tự tính hiện theo hình; tạo thửa rồi đặt cây trồng, mở trang thửa", async () => {
    const u = userEvent.setup();
    render(<PlotCreateScreen />);
    await waitFor(() => expect(screen.getByRole("option", { name: "Lúa" })).toBeInTheDocument());
    expect(screen.queryByRole("option", { name: "Rau màu" })).toBeNull(); // mục nhóm không chọn được
    await u.type(screen.getByLabelText(/Tên thửa/), "Lô Bờ Bao");
    await u.type(screen.getByLabelText(/Chủ hộ/), "Trần Văn B");
    await u.selectOptions(screen.getByLabelText(/Cây trồng/), "LUA");
    await draw(u, SQUARE);
    expect((screen.getByLabelText(/Diện tích tự tính/) as HTMLInputElement).value).toMatch(/^1,2\d{2}/);
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/du-lieu/ve-thua/plot-new"));
    const body = auth.collectApi.createPlot!.mock.calls[0]![0];
    expect(body).toMatchObject({ commune_code: "91301", lot_name: "Lô Bờ Bao", farmer_name: "Trần Văn B", shape: "polygon", geometry: { type: "Polygon", coordinates: [[...SQUARE, SQUARE[0]]] } });
    expect(body).not.toHaveProperty("area_ha");
    expect(body).not.toHaveProperty("declared_area_ha");
    expect(auth.collectApi.putPlotCrop).toHaveBeenCalledWith("plot-new", "LUA");
  });

  it("chồng lấn (409) → báo, không đặt cây trồng, không rời trang", async () => {
    auth.collectApi.createPlot = vi.fn().mockRejectedValue(overlap());
    const u = userEvent.setup();
    render(<PlotCreateScreen />);
    await waitFor(() => expect(screen.getByRole("option", { name: "Lúa" })).toBeInTheDocument());
    await u.type(screen.getByLabelText(/Tên thửa/), "Lô 3");
    await u.type(screen.getByLabelText(/Chủ hộ/), "Trần Văn B");
    await u.selectOptions(screen.getByLabelText(/Cây trồng/), "LUA");
    await draw(u, SQUARE);
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/Thửa chồng lấn thửa đã có trong xã nên không được lưu/)).toBeInTheDocument();
    expect(auth.collectApi.putPlotCrop).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });
});

describe("Sửa thửa", () => {
  beforeEach(() => {
    params.id = "plot-1";
  });

  it("nạp thửa: Xã chỉ đọc, Chủ hộ trống = giữ nguyên; xóa Tên thửa → chặn", async () => {
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    const name = await screen.findByLabelText(/Tên thửa/);
    expect(name).toHaveValue("Lô 1");
    expect(screen.getByLabelText(/Xã\/phường/)).toHaveAttribute("readonly");
    expect(screen.getByLabelText(/Chủ hộ/)).not.toHaveAttribute("aria-required");
    expect(screen.getByLabelText(/Diện tích tự tính/)).toHaveValue("1,2345");
    await u.clear(name);
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Bắt buộc nhập.");
    expect(auth.collectApi.updatePlot).not.toHaveBeenCalled();
  });

  it("đổi tên và cây trồng: PUT thuộc tính đã đổi rồi PUT cây trồng, về danh sách", async () => {
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    const name = await screen.findByLabelText(/Tên thửa/);
    await waitFor(() => expect(screen.getByRole("option", { name: "Ngô" })).toBeInTheDocument());
    await u.clear(name);
    await u.type(name, "Lô 1 mới");
    await u.selectOptions(screen.getByLabelText(/Cây trồng/), "NGO");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/du-lieu/ve-thua"));
    expect(auth.collectApi.updatePlot).toHaveBeenCalledWith("plot-1", { lot_name: "Lô 1 mới" });
    expect(auth.collectApi.putPlotCrop).toHaveBeenCalledWith("plot-1", "NGO");
    expect(auth.collectApi.correctPlotBoundary).not.toHaveBeenCalled();
  });

  it("thửa chưa có cây trồng / chủ hộ: cả hai bắt buộc", async () => {
    auth.collectApi.plot = vi.fn().mockResolvedValue({ ...plot, farmer: null });
    auth.collectApi.plotProfile = vi.fn().mockResolvedValue({ plot_ref: "plot-1", status: "active", crop_code: null, crop_name: null });
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    expect(await screen.findByText(/Thửa chưa khai báo cây trồng/)).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByRole("alert").map((a) => a.textContent)).toEqual(["Bắt buộc nhập.", "Bắt buộc nhập."]);
    expect(auth.collectApi.updatePlot).not.toHaveBeenCalled();
    expect(auth.collectApi.putPlotCrop).not.toHaveBeenCalled();
  });

  it("vẽ lại ranh giới: bắt buộc lý do; chồng lấn (409) → báo, không ghi thuộc tính/cây trồng", async () => {
    auth.collectApi.correctPlotBoundary = vi.fn().mockRejectedValue(overlap());
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    await screen.findByLabelText(/Tên thửa/);
    await u.click(screen.getByRole("button", { name: "Sửa ranh giới" }));
    expect(screen.getByRole("list", { name: "Các đỉnh đã vẽ" }).children).toHaveLength(4); // nạp hình cũ để sửa
    await u.click(screen.getByRole("button", { name: "Xoá đỉnh 4" }));
    await draw(u, [[105.4005, 10.3815]]);
    await u.type(screen.getByLabelText(/Tên thửa/), "X");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Bắt buộc nhập."); // lý do
    expect(auth.collectApi.correctPlotBoundary).not.toHaveBeenCalled();
    await u.type(screen.getByLabelText(/Lý do sửa ranh giới/), "Đo lại bằng GPS ngoài đồng");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/Thửa chồng lấn thửa đã có trong xã nên không được lưu/)).toBeInTheDocument();
    expect(auth.collectApi.correctPlotBoundary).toHaveBeenCalledWith("plot-1", expect.objectContaining({ supersedes_boundary_id: "b-1", shape: "polygon", reason: "Đo lại bằng GPS ngoài đồng" }));
    expect(auth.collectApi.updatePlot).not.toHaveBeenCalled();
    expect(auth.collectApi.putPlotCrop).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });

  it("thửa ngừng dùng: chỉ xem, không có nút Lưu", async () => {
    auth.collectApi.plot = vi.fn().mockResolvedValue({ ...plot, status: "retired" });
    render(<PlotEditScreen />);
    expect(await screen.findByText(/Thửa đã ngừng dùng — chỉ xem/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Tên thửa/)).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Sửa ranh giới" })).toBeNull();
  });
});

// #129 (như review Codex #103 P2): bước ghi đứng sau await phải dùng idem của CÙNG lượt — mất phản hồi ở bước cuối
// rồi bấm Lưu lại thì mọi bước dùng lại khóa cũ, không báo thành công khi bước cuối lỗi.
describe("Thửa — bấm Lưu lại sau mất phản hồi giữ khóa của mọi bước", () => {
  async function realClient(losePath: RegExp) {
    resetKnCollect();
    const r = await mockFetchNoDelay(
      new Request("http://mock/v1/me/context", {
        method: "POST",
        headers: { Authorization: "Bearer mock.kn-an", "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
        body: JSON.stringify({ assignment_id: "asg-an-xa", department_code: "trong_trot" }),
      }),
    );
    const ctx = ((await r.json()) as { context_id: string }).context_id;
    const writes: { path: string; key: string }[] = [];
    let lose = true;
    const fetch = async (req: Request): Promise<Response> => {
      const path = new URL(req.url).pathname;
      if (req.method !== "GET") writes.push({ path, key: req.headers.get(IDEMPOTENCY_HEADER) ?? "" });
      const res = await mockFetchNoDelay(req);
      if (req.method !== "GET" && losePath.test(path) && lose) {
        lose = false; // máy chủ đã xử lý, phản hồi mất
        throw new TypeError("Failed to fetch");
      }
      return res;
    };
    const api = createCollectApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => ctx, fetch });
    auth.collectApi = api as unknown as typeof auth.collectApi;
    return { api, writes };
  }
  const kind = (w: { path: string }) => (w.path.startsWith("/v1/kn/plot-profiles/") ? "crop" : w.path.endsWith("/boundary") ? "boundary" : "plot");

  it("Thêm thửa: đặt cây trồng mất phản hồi → Lưu lại dùng lại khóa của tạo thửa và cây trồng", async () => {
    const { writes } = await realClient(/^\/v1\/kn\/plot-profiles\//);
    const u = userEvent.setup();
    render(<PlotCreateScreen />);
    const crop = screen.getByLabelText(/Cây trồng/);
    await waitFor(() => expect(within(crop).getAllByRole("option").length).toBeGreaterThan(1));
    await u.type(screen.getByLabelText(/Tên thửa/), "Lô thử lại");
    await u.type(screen.getByLabelText(/Chủ hộ/), "Trần Văn B");
    await u.selectOptions(crop, within(crop).getAllByRole("option")[1]!);
    await draw(u, SQUARE.map(([x, y]) => [x + 0.8, y + 0.2] as LngLat));
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(2));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled());
    expect(push).not.toHaveBeenCalled();
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    expect(writes.map(kind)).toEqual(["plot", "crop", "plot", "crop"]);
    expect(writes[2]!.key).toBe(writes[0]!.key);
    expect(writes[3]!.key).toBe(writes[1]!.key);
  });

  it("Sửa thửa: vẽ lại ranh giới rồi sửa tên (mất phản hồi) → Lưu lại dùng lại khóa của cả hai bước", async () => {
    const { api, writes } = await realClient(/^\/v1\/kn\/plots\/[^/]+$/);
    const [first] = (await api.plots({ commune: "91301" })).items;
    params.id = first!.ref;
    const u = userEvent.setup();
    render(<PlotEditScreen />);
    const name = await screen.findByLabelText(/Tên thửa/);
    await u.type(name, " sửa");
    await u.click(screen.getByRole("button", { name: "Sửa ranh giới" }));
    await u.type(screen.getByLabelText(/Lý do sửa ranh giới/), "Đo lại bằng GPS ngoài đồng");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(2));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled());
    expect(push).not.toHaveBeenCalled();
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/kn/du-lieu/ve-thua"));
    expect(writes.map(kind)).toEqual(["boundary", "plot", "boundary", "plot"]);
    expect(writes[2]!.key).toBe(writes[0]!.key);
    expect(writes[3]!.key).toBe(writes[1]!.key);
  });
});
