// BF-CN-CATCH #112 (UP-09): Khai thác thủy sản tuần theo 4 sheet của biểu "ĐÁNH BẮT (nhập liệu theo xã)".
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi, type FormsTask } from "../periods/api";
import { catchMatrix, draftOf, editableWeeks, sheetTotal, sheetValue, SHEETS, unitTotal } from "./catch";
import { CntsPl2Screen } from "../aggregate/CntsPl2Screen";
import { CatchScreen } from "./CatchScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/khai-thac-thuy-san",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_xa" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: auth.api,
    kn: {
      context: {
        role: auth.role,
        communes: [
          { code: "91301", name: "Mỹ Hòa Hưng" },
          { code: "91302", name: "Bình Đức" },
        ],
        department_code: "chan_nuoi_thuy_san",
      },
    },
  }),
}));

const task = (p: string, unit: string, extra: Partial<FormsTask> = {}): FormsTask =>
  ({
    id: `${p}:${unit}`,
    period_id: p,
    unit,
    template_code: "CNTS-BM17",
    label: `Tuần ${p}`,
    period_from: `2026-10-${p}`,
    due_at: "2099-01-01T00:00:00Z",
    period_status: "open",
    open: false,
    status: "submitted",
    version: 1,
    values: {},
    nil_report: false,
    fields: [],
    ...extra,
  }) as unknown as FormsTask;

describe("4 sheet ĐÁNH BẮT — hàm thuần", () => {
  it("sheet đúng tên và thứ tự tệp gốc", () => {
    expect(SHEETS.map((s) => s.tab)).toEqual(["Đánh bắt cá các loại", "Đánh bắt tôm các loại", "Đánh bắt mực", "Đánh bắt thủy sản khác"]);
    expect(SHEETS.map((s) => s.key)).toEqual(["ca", "tom", "muc", "khac"]);
  });

  it("ma trận xã × tuần, dòng TỔNG cộng thập phân chính xác, báo không phát sinh = 0, chưa nộp = trống", () => {
    const ts = [
      task("06", "91301", { values: { ca: "0.1", tom: "0.2", muc: "0", khac: "1" } }),
      task("06", "91302", { values: { ca: "0.2", tom: "0", muc: "0", khac: "0" } }),
      task("13", "91301", { nil_report: true, values: { ca: "0", tom: "0", muc: "0", khac: "0" } }),
      task("13", "91302", { version: 0, status: "not_submitted", open: true }),
      task("20", "91301", { template_code: "CNTS-BM04" }),
    ];
    const m = catchMatrix(ts);
    expect(m.weeks.map((w) => w.periodId)).toEqual(["06", "13"]);
    expect(m.units).toEqual(["91301", "91302"]);
    expect(sheetTotal(m, "06", "ca")).toBe("0.3"); // không phải 0.30000000000000004
    expect(sheetValue(m.cell("13", "91301"), "ca")).toBe("0");
    expect(sheetValue(m.cell("13", "91302"), "ca")).toBeNull();
    expect(sheetTotal(m, "13", "ca")).toBe("0");
    expect(unitTotal(m.cell("06", "91301"))).toBe("1.3");
    expect(catchMatrix(ts, 1).weeks.map((w) => w.periodId)).toEqual(["13"]);
    expect(editableWeeks(m).map((w) => w.periodId)).toEqual(["13"]);
    expect(draftOf(m.cell("13", "91302")!)).toEqual({ ca: "0", tom: "0", muc: "0", khac: "0" }); // ô mặc định 0
    expect(draftOf(m.cell("06", "91301")!)).toEqual({ ca: "0.1", tom: "0.2", muc: "0", khac: "1" });
  });
});

async function contextOf(personaId: string, assignmentId: string, dept: string | null) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

const apiOf = (persona: string, ctx: string | null) =>
  createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => ctx, fetch: mockFetchNoDelay });

describe("màn Khai thác thủy sản tuần", () => {
  let xaCtx: string;
  let labels: string[] = [];
  beforeEach(async () => {
    resetKnAdmin();
    resetCntsPeriods();
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active").catch(() => undefined); // đã bật ở ca trước
    await createCntsPeriodApi(apiOf("kn-admin", adminCtx)).install();
    await createCntsPeriodApi(apiOf("kn-binh", null)).publish({ code: "CNTS-BM17", from_date: "2099-01-06", to_date: "2099-01-19", units: ["91301", "91302"] });
    xaCtx = await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san");
    labels = [...new Set((await createCntsPeriodApi(apiOf("kn-binh", null)).tasks("CNTS-BM17")).sort((a, b) => a.period_from!.localeCompare(b.period_from!)).map((t) => t.label))];
  });

  it("Cấp Xã nhập cột tuần trên từng sheet, số âm bị chặn, gửi một bài BM-17 gồm 4 nhóm cho mỗi xã", async () => {
    auth.role = "kn_xa";
    auth.api = apiOf("kn-an", xaCtx);
    const u = userEvent.setup();
    render(<CatchScreen />);
    const tabs = await screen.findByRole("tablist", { name: "Sheet" });
    expect(within(tabs).getAllByRole("tab").map((t) => t.textContent)).toEqual(SHEETS.map((s) => s.tab));
    const ca = screen.getByRole("table", { name: "SẢN LƯỢNG ĐÁNH BẮT CÁ CÁC LOẠI" });
    expect(within(ca).getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "STT",
      "Xã, phường",
      ...labels.map((l) => `Trong kỳ báo cáo (${l}) (Tấn)`),
    ]);
    expect(labels).toHaveLength(2);
    const first = labels[0]!;
    // Tuần đang nhập mặc định là tuần mở mới nhất; ô mặc định 0.
    const weekSel = screen.getByRole("combobox", { name: "Tuần đang nhập" });
    expect(within(weekSel).getAllByRole("option")[0]!.textContent).toContain(labels[1]!);
    await u.selectOptions(weekSel, within(weekSel).getAllByRole("option").find((o) => o.textContent!.startsWith(first))!);
    const cell = (sheet: string, xa: string) => screen.getByRole("textbox", { name: `${sheet} ${xa} ${first} (tấn)` });
    expect(cell("Đánh bắt cá các loại", "Mỹ Hòa Hưng")).toHaveValue("0");
    await u.clear(cell("Đánh bắt cá các loại", "Mỹ Hòa Hưng"));
    await u.type(cell("Đánh bắt cá các loại", "Mỹ Hòa Hưng"), "-1");
    await u.click(screen.getByRole("button", { name: "Gửi tuần" }));
    expect(await screen.findByText("Không được nhập số âm.")).toBeInTheDocument();
    await u.clear(cell("Đánh bắt cá các loại", "Mỹ Hòa Hưng"));
    await u.type(cell("Đánh bắt cá các loại", "Mỹ Hòa Hưng"), "1,25");
    await u.click(within(tabs).getByRole("tab", { name: "Đánh bắt tôm các loại" }));
    await u.clear(cell("Đánh bắt tôm các loại", "Mỹ Hòa Hưng"));
    await u.type(cell("Đánh bắt tôm các loại", "Mỹ Hòa Hưng"), "0.5");
    await u.click(screen.getByRole("checkbox", { name: "Không phát sinh khai thác trong tuần — Bình Đức" }));
    await u.click(screen.getByRole("button", { name: "Gửi tuần" }));
    expect(await screen.findByText("Đã gửi số liệu khai thác 2 xã của tuần.", {}, { timeout: 3000 })).toBeInTheDocument();
    const tasks = await createCntsPeriodApi(apiOf("kn-an", xaCtx)).tasks("CNTS-BM17");
    const w1 = tasks.filter((t) => t.label === first);
    expect(w1.find((t) => t.unit === "91301")!.values).toEqual({ ca: "1.25", tom: "0.5", muc: "0", khac: "0" });
    expect(w1.find((t) => t.unit === "91302")!.nil_report).toBe(true);
    // Sau tải lại: sheet cá hiện số đã nộp, xã báo không phát sinh = 0, dòng TỔNG cộng các xã.
    await waitFor(() => {
      const rows = within(screen.getByRole("table", { name: "SẢN LƯỢNG ĐÁNH BẮT CÁ CÁC LOẠI" })).getAllByRole("row");
      expect(rows[1]).toHaveTextContent("Mỹ Hòa Hưng1,25");
      expect(rows[2]).toHaveTextContent("(không phát sinh)");
      expect(rows[3]).toHaveTextContent(/^TỔNG1,25/);
    });
  });

  it("Cấp Tỉnh xem 4 sheet của mọi xã, không có ô nhập", async () => {
    auth.role = "kn_tinh";
    auth.api = apiOf("kn-binh", null);
    const u = userEvent.setup();
    render(<CatchScreen />);
    const tabs = await screen.findByRole("tablist", { name: "Sheet" });
    await u.click(within(tabs).getByRole("tab", { name: "Đánh bắt mực" }));
    const muc = screen.getByRole("table", { name: "SẢN LƯỢNG ĐÁNH BẮT MỰC" });
    expect(within(muc).getAllByRole("row")).toHaveLength(4); // tiêu đề + 2 xã + TỔNG
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Gửi tuần" })).toBeNull();
    expect(screen.getByRole("link", { name: /Phụ lục 2/ })).toHaveAttribute("href", "/kn/cnts/phu-luc-2");
  });

  it("Phụ lục 2 (tỉnh): khai thác theo xã xem được từng sheet cá / tôm / mực / khác hoặc tổng", async () => {
    auth.role = "kn_tinh";
    auth.api = apiOf("kn-binh", null);
    const u = userEvent.setup();
    render(<CntsPl2Screen />);
    const tabs = await screen.findByRole("tablist", { name: "Sheet khai thác" });
    expect(within(tabs).getAllByRole("tab").map((t) => t.textContent)).toEqual(["Tổng khai thác", "Cá các loại", "Tôm các loại", "Mực", "Thủy sản khác"]);
    expect(screen.getByRole("heading", { name: "Khai thác theo xã (tổng sản lượng, tấn)" })).toBeInTheDocument();
    await u.click(within(tabs).getByRole("tab", { name: "Mực" }));
    expect(screen.getByRole("heading", { name: "Khai thác theo xã (Mực, tấn)" })).toBeInTheDocument();
  });
});
