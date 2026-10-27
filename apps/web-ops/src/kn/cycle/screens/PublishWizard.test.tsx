// BF-TT-PUBLISH (#106, PRD TT-14, TT-04): phát hành biểu theo 5 bước, chọn xã từ danh sách (không gõ mã thô),
// Tỉnh giao "sao từ đợt trước", danh sách kỳ đã chốt (ai chốt, lúc nào, trạng thái).
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PublishScreen, splitProvincial } from "./PublishWizard";
import { ReviewListScreen } from "./TinhScreens";

vi.mock("next/navigation", () => ({ usePathname: () => "/kn/nhiem-vu/phat-hanh", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }), useParams: () => ({}) }));

const auth: { cycleApi: Record<string, ReturnType<typeof vi.fn>> } = { cycleApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: { GET: () => Promise.reject(new Error("không tra tên trong kiểm thử")) }, kn: { context: { role: "kn_tinh", communes: [], department_code: "trong_trot" } }, cycleApi: auth.cycleApi }),
}));

const FORMS = ["T01", "T02", "T03"].map((code) => ({
  template_id: `tpl-${code}`,
  version_id: `${code}v1`,
  code,
  name: `Biểu ${code}`,
  version: 1,
  report_types: ["weekly"],
  period_block: false,
  completeness: "complete",
  provincial_columns: code === "T03" ? [] : [{ letter: "H", title: "Tỉnh giao", unit: "ha" }],
}));
const COMMUNES = [
  { commune_code: "91301", region_code: "LX", region_name: "Long Xuyên", officer_count: 1 },
  { commune_code: "91302", region_code: "LX", region_name: "Long Xuyên", officer_count: 1 },
  { commune_code: "91401", region_code: "CD", region_name: "Châu Đốc", officer_count: 0 },
];
const preview = (communes = COMMUNES) => ({
  report_type: "weekly",
  period_type: "week",
  period_rule: "week_start=wed",
  calendar_id: "trong_trot",
  calendar_version: 2,
  calendar_source: "builtin",
  deadline: { offset_days: 1, time: "12:00" },
  close_after_days: 1,
  forms: [],
  periods: [{ slot: 1, label: "Tuần 1", range_label: "05/11–11/11", from_date: "2026-11-05", to_date: "2026-11-11", due_at: "2026-11-12T05:00:00Z", auto_close_at: "2026-11-13T05:00:00Z", period_key: "2026-W45", iso_week: 45, week_year: 2026 }],
  communes,
  communes_without_officer: communes.filter((c) => !c.officer_count).map((c) => c.commune_code),
  provincial_columns: [],
  form_count: 1,
  commune_count: communes.length,
  period_count: 1,
  task_count: communes.length,
  warnings: [],
});
const PREV_BATCH = { id: "0192ffff-0000-7000-8000-000000000002", season_label: "Hè Thu 2026", status: "issued", forms: [{ template_id: "tpl-T01", code: "T01" }], communes: ["91301"], period_count: 1 };
const OLD_BATCH = { id: "0192aaaa-0000-7000-8000-000000000001", season_label: "Đông Xuân 2026", status: "issued", forms: [{ template_id: "tpl-T01", code: "T01" }], communes: ["91301"], period_count: 1 };

beforeEach(() => {
  auth.cycleApi = {
    reportForms: vi.fn().mockResolvedValue({ items: FORMS }),
    batches: vi.fn().mockResolvedValue({ items: [OLD_BATCH, PREV_BATCH] }),
    preview: vi.fn().mockImplementation((b: { scope: { mode: string; communes?: string[]; regions?: string[] } }) =>
      Promise.resolve(preview(b.scope.mode === "communes" ? COMMUNES.filter((c) => b.scope.communes!.includes(c.commune_code)) : b.scope.mode === "regions" ? COMMUNES.filter((c) => b.scope.regions!.includes(c.region_code)) : COMMUNES)),
    ),
    provincialValues: vi.fn().mockResolvedValue({ items: [{ template_id: "tpl-T01", commune_code: "91301", cells: { H: "120.5" } }, { template_id: "tpl-T01", commune_code: "99999", cells: { H: "7" } }] }),
    issue: vi.fn().mockResolvedValue({ batch: { id: "new", task_count: 2, communes: ["91301", "91302"] }, periods: [{}], warnings: [], communes_without_officer: [] }),
  };
});

const next = (u: ReturnType<typeof userEvent.setup>) => u.click(screen.getByRole("button", { name: "Tiếp →" }));
const currentStep = () => screen.getByRole("button", { current: "step" });

describe("Phát hành biểu — wizard 5 bước (TT-14)", () => {
  it("có 5 bước; B1 phải chọn biểu trước khi sang bước sau", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    const nav = screen.getByRole("navigation", { name: "Các bước phát hành" });
    expect(within(nav).getAllByRole("button").map((b) => b.textContent?.trim())).toEqual([
      "Bước 1 · Chọn biểu",
      "Bước 2 · Phạm vi xã",
      "Bước 3 · Kỳ và hạn nộp",
      "Bước 4 · Chỉ tiêu Tỉnh giao",
      "Bước 5 · Xem trước & phát hành",
    ]);
    await screen.findByRole("checkbox", { name: "Chọn T01" });
    await next(u);
    expect(screen.getByRole("alert")).toHaveTextContent("Chọn ít nhất một biểu.");
    expect(currentStep()).toHaveTextContent("Bước 1");
    await u.click(screen.getByRole("checkbox", { name: "Chọn T01" }));
    await next(u);
    expect(currentStep()).toHaveTextContent("Bước 2");
  });

  it("B2 chọn từng xã từ danh sách tỉnh (không có ô gõ mã xã); khu vực lấy từ danh sách xã", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await next(u);
    await waitFor(() => expect(auth.cycleApi.preview).toHaveBeenCalledWith(expect.objectContaining({ scope: { mode: "province" }, template_ids: ["tpl-T01"] })));
    expect(await screen.findByText("Toàn tỉnh: 3 xã/phường.")).toBeInTheDocument();
    await u.click(screen.getByRole("radio", { name: "Theo Khu vực" }));
    expect(screen.getByRole("checkbox", { name: /Long Xuyên \(2 xã\)/ })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /Châu Đốc \(1 xã\)/ })).toBeInTheDocument();
    await next(u);
    expect(screen.getByRole("alert")).toHaveTextContent("Chọn ít nhất một khu vực.");
    await u.click(screen.getByRole("radio", { name: "Chọn từng xã" }));
    expect(screen.queryByRole("textbox", { name: /Mã xã/ })).toBeNull();
    await next(u);
    expect(screen.getByRole("alert")).toHaveTextContent("Chọn ít nhất một xã/phường.");
    await u.click(screen.getByRole("checkbox", { name: /91302/ }));
    await u.click(screen.getByRole("checkbox", { name: /91401 · Châu Đốc \(chưa có cán bộ phụ trách\)/ }));
    expect(screen.getByText("Đã chọn 2 xã")).toBeInTheDocument();
    await next(u);
    expect(currentStep()).toHaveTextContent("Bước 3");
  });

  it("B3 kiểm trường bắt buộc rồi máy chủ chia kỳ; B4 nhập tay; B5 xem trước rồi Phát hành", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await u.clear(screen.getByLabelText(/Vụ \/ đợt/));
    const before = auth.cycleApi.preview!.mock.calls.length;
    await next(u);
    expect(screen.getByRole("alert")).toHaveTextContent("Bắt buộc nhập.");
    expect(auth.cycleApi.preview!.mock.calls.length).toBe(before);
    await u.type(screen.getByLabelText(/Vụ \/ đợt/), "Thu Đông 2026");
    await next(u);
    expect(currentStep()).toHaveTextContent("Bước 4");
    await u.type(await screen.findByLabelText("T01 H 91302"), "-1");
    await next(u);
    expect(await screen.findByText("Không được nhập số âm.")).toBeInTheDocument();
    expect(currentStep()).toHaveTextContent("Bước 4");
    await u.clear(screen.getByLabelText("T01 H 91302"));
    await u.type(screen.getByLabelText("T01 H 91302"), "1.234,5");
    await next(u);
    expect(currentStep()).toHaveTextContent("Bước 5");
    const region = screen.getByRole("region", { name: "Bước 5 · Xem trước" });
    expect(within(region).getByTestId("task-count")).toHaveTextContent("3");
    expect(within(region).getByText(/Tuần 1 \(05\/11–11\/11\)/)).toBeInTheDocument();
    expect(within(region).getByText(/1 xã chưa có cán bộ phụ trách/)).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: /Phát hành$/ }));
    await waitFor(() => expect(auth.cycleApi.issue).toHaveBeenCalledTimes(1));
    expect(auth.cycleApi.issue!.mock.calls[0]![0]).toMatchObject({
      template_ids: ["tpl-T01"],
      scope: { mode: "province" },
      season_label: "Thu Đông 2026",
      provincial_values: { "tpl-T01": { "91302": { H: "1234.5" } } },
      provincial_source: "manual",
    });
    expect(auth.cycleApi.issue!.mock.calls[0]![0]).not.toHaveProperty("copy_provincial_from_previous");
    expect(await screen.findByText(/Đã phát hành 2 công việc/)).toBeInTheDocument();
    expect(currentStep()).toHaveTextContent("Bước 1");
  });

  it("B4 sao từ đợt trước: điền giá trị đợt gần nhất có cùng biểu; xã không sửa → copy_provincial_from_previous, xã đã sửa gửi trực tiếp", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await next(u);
    await screen.findByLabelText("T01 H 91301");
    await u.click(screen.getByRole("button", { name: /Sao từ đợt trước/ }));
    await waitFor(() => expect(auth.cycleApi.provincialValues).toHaveBeenCalledWith(PREV_BATCH.id, "tpl-T01")); // đợt mới nhất, không phải OLD_BATCH
    expect(screen.getByLabelText("T01 H 91301")).toHaveValue("120.5");
    expect(screen.getByText(/Đã sao 1 dòng xã × biểu từ đợt “Hè Thu 2026”/)).toBeInTheDocument(); // 99999 ngoài phạm vi đợt → bỏ
    await next(u);
    await u.click(screen.getByRole("button", { name: /Phát hành$/ }));
    await waitFor(() => expect(auth.cycleApi.issue).toHaveBeenCalledTimes(1));
    const sent = auth.cycleApi.issue!.mock.calls[0]![0];
    expect(sent.copy_provincial_from_previous).toEqual(["tpl-T01"]);
    expect(sent.provincial_values).toEqual({});
  });

  it("sửa giá trị đã sao → gửi trực tiếp, không còn nhờ máy chủ chép", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await next(u);
    await screen.findByLabelText("T01 H 91301");
    await u.click(screen.getByRole("button", { name: /Sao từ đợt trước/ }));
    await waitFor(() => expect(screen.getByLabelText("T01 H 91301")).toHaveValue("120.5"));
    await u.clear(screen.getByLabelText("T01 H 91301"));
    await u.type(screen.getByLabelText("T01 H 91301"), "130");
    await next(u);
    await waitFor(() => expect(auth.cycleApi.preview!.mock.lastCall![0]).toMatchObject({ provincial_values: { "tpl-T01": { "91301": { H: "130" } } } }));
    expect(auth.cycleApi.preview!.mock.lastCall![0]).not.toHaveProperty("copy_provincial_from_previous");
  });

  it("Codex review #100: bỏ chọn biểu ở B1 thì ô Tỉnh giao lỗi của biểu đó không chặn và không bị gửi", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T01" }));
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await next(u);
    await u.type(await screen.findByLabelText("T02 H 91301"), "-1");
    await u.type(screen.getByLabelText("T01 H 91301"), "5");
    await u.click(screen.getByRole("button", { name: /Bước 1/ }));
    await u.click(screen.getByRole("checkbox", { name: "Chọn T02" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await next(u);
    expect(await screen.findByLabelText("T01 H 91301")).toHaveValue("5");
    expect(screen.queryByLabelText("T02 H 91301")).toBeNull();
    await next(u);
    expect(currentStep()).toHaveTextContent("Bước 5");
    const sent = auth.cycleApi.preview!.mock.lastCall![0];
    expect(sent).toMatchObject({ template_ids: ["tpl-T01"], provincial_values: { "tpl-T01": { "91301": { H: "5" } } } });
    expect(sent.provincial_values).not.toHaveProperty("tpl-T02");
  });

  it("biểu không có cột Tỉnh giao: B4 báo không cần nhập", async () => {
    const u = userEvent.setup();
    render(<PublishScreen />);
    await u.click(await screen.findByRole("checkbox", { name: "Chọn T03" }));
    await next(u);
    await screen.findByText("Toàn tỉnh: 3 xã/phường.");
    await next(u);
    await next(u);
    expect(await screen.findByText(/không có cột Tỉnh giao/)).toBeInTheDocument();
  });
});

describe("splitProvincial", () => {
  it("xã sao nguyên → copyFrom; xã sửa/thêm → provincial; ô rỗng bỏ", () => {
    const r = splitProvincial(
      { a: { x1: { H: "1" }, x2: { H: "9" }, x3: { H: "" } }, b: { x1: { H: "2" } } },
      { a: { x1: { H: "1" }, x2: { H: "8" } } },
      ["a"],
    );
    expect(r).toEqual({ provincial: { a: { x2: { H: "9" } } }, copyFrom: ["a"], copiedRows: 1 });
  });
});

describe("Rà soát & chốt kỳ — Kỳ đã chốt (TT-04)", () => {
  it("liệt kê kỳ đã chốt: đợt, biểu, kỳ, chốt lúc, người chốt (log) hoặc hệ thống tự chốt, trạng thái", async () => {
    const P = (id: string, extra: Record<string, unknown>) => ({ id, batch_id: PREV_BATCH.id, slot: 1, label: `Kỳ ${id}`, range_label: "01/10–07/10", from_date: "2026-10-01", to_date: "2026-10-07", due_at: "", auto_close_at: "", iso_week: null, period_key: null, week_year: null, ...extra });
    auth.cycleApi = {
      tasks: vi.fn().mockResolvedValue({ items: [] }),
      batches: vi.fn().mockResolvedValue({ items: [PREV_BATCH] }),
      periods: vi.fn().mockResolvedValue({
        items: [
          P("p1", { status: "closed", closed_at: "2026-10-09T03:00:00Z", closed_by: "u1", close_mode: "manual" }),
          P("p2", { status: "closed", closed_at: "2026-10-10T03:00:00Z", closed_by: "sys", close_mode: "auto" }),
          P("p3", { status: "open", closed_at: null, closed_by: null, close_mode: null }),
        ],
      }),
      periodLog: vi.fn().mockResolvedValue({ items: [{ event: "period.closed", at: "2026-10-09T03:00:00Z", actor_display: "Trần Thị Tỉnh", changes: [] }] }),
    };
    const u = userEvent.setup();
    render(<ReviewListScreen />);
    await u.click(screen.getByRole("tab", { name: "Kỳ đã chốt" }));
    const table = await screen.findByRole("table", { name: "Kỳ đã chốt" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2); // kỳ đang mở không hiện
    expect(rows[0]).toHaveTextContent("Kỳ p2");
    expect(rows[0]).toHaveTextContent("Hệ thống (tự chốt)");
    expect(rows[0]).toHaveTextContent("Tự động sau hạn");
    expect(rows[1]).toHaveTextContent("Hè Thu 2026");
    expect(rows[1]).toHaveTextContent("T01");
    expect(await within(rows[1]!).findByText("Trần Thị Tỉnh")).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("Thủ công");
    expect(rows[1]).toHaveTextContent("Đã chốt");
    expect(auth.cycleApi.periodLog).toHaveBeenCalledWith("p1");
    expect(auth.cycleApi.periodLog).not.toHaveBeenCalledWith("p2");
  });
});
