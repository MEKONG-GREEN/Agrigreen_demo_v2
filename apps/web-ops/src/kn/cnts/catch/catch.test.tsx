import { createDesignApi } from "../design/api";
import { cntsSelection } from "@/kn/cnts/periods/selection.fixture";
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
    const publishing = createCntsPeriodApi(apiOf("kn-binh", null));
    await publishing.publish({ ...(await cntsSelection(publishing, "CNTS-BM17")), code: "CNTS-BM17", from_date: "2099-01-06", to_date: "2099-01-19", units: ["91301", "91302"] });
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

  // #112 P2 (Codex): gửi nhiều xã trong một lượt — mỗi bước ghi giữ Idempotency-Key của lượt; lỗi giữa chừng đọc lại trạng thái,
  // giữ số chưa gửi; bấm lại phát lại đúng khóa, không 409.
  type Write = { unit: string; base: number; key: string | null; status: number | "lost" | "dropped" | "conflict" };
  function flakyApi(fail: (n: number) => "lost" | "dropped" | "conflict" | null) {
    const writes: Write[] = [];
    let n = 0;
    const network = async (input: RequestInfo | URL, init?: RequestInit) => {
      const req = input instanceof Request ? input : new Request(input, init);
      const isSubmit = req.method === "POST" && new URL(req.url).pathname.endsWith("/submissions");
      if (!isSubmit) return mockFetchNoDelay(req);
      const body = (await req.clone().json()) as { unit: string; base_version: number };
      const key = req.headers.get("Idempotency-Key");
      const mode = fail(++n);
      if (mode === "dropped") {
        writes.push({ unit: body.unit, base: body.base_version, key, status: "dropped" });
        throw new TypeError("mất mạng trước khi tới máy chủ");
      }
      if (mode === "conflict") {
        writes.push({ unit: body.unit, base: body.base_version, key, status: "conflict" });
        return new Response(JSON.stringify({ type: "about:blank", title: "Conflict", status: 409, code: "forms.submission_conflict" }), {
          status: 409,
          headers: { "Content-Type": "application/problem+json" },
        });
      }
      const r = await mockFetchNoDelay(req);
      writes.push({ unit: body.unit, base: body.base_version, key, status: mode === "lost" ? "lost" : r.status });
      if (mode === "lost") throw new TypeError("mất phản hồi sau khi máy chủ đã lưu");
      return r;
    };
    return { writes, api: createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => xaCtx, fetch: network }) };
  }
  const statusOf = (xa: string) => screen.getByText(xa, { selector: "strong" }).closest("li")!;

  it("mất phản hồi ở xã thứ hai: bấm lại phát lại cả hai bằng đúng khóa cũ, hoàn tất, cả hai hiện đã nộp", async () => {
    auth.role = "kn_xa";
    const f = flakyApi((n) => (n === 2 ? "lost" : null));
    auth.api = f.api;
    const u = userEvent.setup();
    render(<CatchScreen />);
    const submit = await screen.findByRole("button", { name: "Gửi tuần" });
    await u.click(submit);
    // Lỗi giữa chừng → đọc lại trạng thái: cả hai xã đã lưu hiện "Đã nộp" ngay, không còn "Chưa nhập".
    await waitFor(() => {
      expect(within(statusOf("Mỹ Hòa Hưng")).getByText("Đã nộp")).toBeInTheDocument();
      expect(within(statusOf("Bình Đức")).getByText("Đã nộp")).toBeInTheDocument();
    });
    expect(screen.getByText(/Lần gửi trước dừng giữa chừng/)).toBeInTheDocument();
    await u.click(submit);
    expect(await screen.findByText("Đã gửi số liệu khai thác 2 xã của tuần.")).toBeInTheDocument();
    const [a1, b1, a2, b2] = f.writes;
    expect(f.writes).toHaveLength(4);
    expect([a1!.unit, b1!.unit, a2!.unit, b2!.unit]).toEqual(["91301", "91302", "91301", "91302"]);
    // Khóa ổn định qua lần bấm lại, mỗi xã một khóa riêng; máy chủ phát lại (201), không 409.
    expect(a2!.key).toBe(a1!.key);
    expect(b2!.key).toBe(b1!.key);
    expect(a1!.key).not.toBe(b1!.key);
    expect([a2!.status, b2!.status]).toEqual([201, 201]);
    expect([a2!.base, b2!.base]).toEqual([0, 0]);
    const saved = (await createCntsPeriodApi(apiOf("kn-an", xaCtx)).tasks("CNTS-BM17")).filter((t) => t.label === labels[1]);
    expect(saved.map((t) => t.version)).toEqual([1, 1]); // không ghi lần hai
  });

  it("lỗi mạng ở xã thứ hai: xã đã lưu hiện đã nộp, số chưa gửi giữ nguyên, bấm lại gửi tiếp không 409", async () => {
    auth.role = "kn_xa";
    const f = flakyApi((n) => (n === 2 ? "dropped" : null));
    auth.api = f.api;
    const u = userEvent.setup();
    render(<CatchScreen />);
    const submit = await screen.findByRole("button", { name: "Gửi tuần" });
    const cell = (xa: string) => screen.getByRole("textbox", { name: `Đánh bắt cá các loại ${xa} ${labels[1]} (tấn)` });
    await u.clear(cell("Mỹ Hòa Hưng"));
    await u.type(cell("Mỹ Hòa Hưng"), "2");
    await u.clear(cell("Bình Đức"));
    await u.type(cell("Bình Đức"), "3,5");
    await u.click(submit);
    await waitFor(() => expect(within(statusOf("Mỹ Hòa Hưng")).getByText("Đã nộp")).toBeInTheDocument());
    expect(within(statusOf("Bình Đức")).queryByText("Đã nộp")).toBeNull();
    expect(cell("Bình Đức")).toHaveValue("3,5"); // số chưa gửi giữ nguyên
    expect(cell("Mỹ Hòa Hưng")).toHaveValue("2");
    await u.click(submit);
    expect(await screen.findByText("Đã gửi số liệu khai thác 2 xã của tuần.")).toBeInTheDocument();
    expect(f.writes.map((w) => [w.unit, w.status])).toEqual([
      ["91301", 201],
      ["91302", "dropped"],
      ["91301", 201],
      ["91302", 201],
    ]);
    expect(f.writes[2]!.key).toBe(f.writes[0]!.key);
    expect(f.writes[3]!.key).toBe(f.writes[1]!.key);
    const saved = (await createCntsPeriodApi(apiOf("kn-an", xaCtx)).tasks("CNTS-BM17")).filter((t) => t.label === labels[1]);
    expect(saved.find((t) => t.unit === "91301")).toMatchObject({ version: 1, values: { ca: "2" } });
    expect(saved.find((t) => t.unit === "91302")).toMatchObject({ version: 1, values: { ca: "3.5" } });
  });

  it("sửa số xã đã lưu sau lỗi giữa chừng: gửi bài mới trên phiên bản đọc lại, không 409", async () => {
    auth.role = "kn_xa";
    const f = flakyApi((n) => (n === 2 ? "lost" : null));
    auth.api = f.api;
    const u = userEvent.setup();
    render(<CatchScreen />);
    const submit = await screen.findByRole("button", { name: "Gửi tuần" });
    await u.click(submit);
    await waitFor(() => expect(within(statusOf("Bình Đức")).getByText("Đã nộp")).toBeInTheDocument());
    const cell = screen.getByRole("textbox", { name: `Đánh bắt cá các loại Bình Đức ${labels[1]} (tấn)` });
    await u.clear(cell);
    await u.type(cell, "4");
    await u.click(submit);
    expect(await screen.findByText("Đã gửi số liệu khai thác 2 xã của tuần.")).toBeInTheDocument();
    const last = f.writes[3]!;
    expect(last).toMatchObject({ unit: "91302", base: 1, status: 201 });
    expect(last.key).not.toBe(f.writes[1]!.key);
    const saved = (await createCntsPeriodApi(apiOf("kn-an", xaCtx)).tasks("CNTS-BM17")).find((t) => t.label === labels[1] && t.unit === "91302");
    expect(saved).toMatchObject({ version: 2, values: { ca: "4" } });
  });

  it("lỗi nghiệp vụ dứt khoát (409) ở xã thứ hai: khóa bước đó bị bỏ, bấm lại gửi bằng khóa mới", async () => {
    auth.role = "kn_xa";
    const f = flakyApi((n) => (n === 2 ? "conflict" : null));
    auth.api = f.api;
    const u = userEvent.setup();
    render(<CatchScreen />);
    const submit = await screen.findByRole("button", { name: "Gửi tuần" });
    await u.click(submit);
    await waitFor(() => expect(within(statusOf("Mỹ Hòa Hưng")).getByText("Đã nộp")).toBeInTheDocument());
    await u.click(submit);
    expect(await screen.findByText("Đã gửi số liệu khai thác 2 xã của tuần.")).toBeInTheDocument();
    expect(f.writes.map((w) => [w.unit, w.status])).toEqual([
      ["91301", 201],
      ["91302", "conflict"],
      ["91301", 201],
      ["91302", 201],
    ]);
    expect(f.writes[2]!.key).toBe(f.writes[0]!.key);
    expect(f.writes[3]!.key).not.toBe(f.writes[1]!.key);
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

  it("#275: nhập đủ trường của phiên bản mở rộng và đọc lại kỳ chuẩn cũ", async () => {
    const admin = apiOf("kn-admin", await contextOf("kn-admin", "asg-admin", null));
    const designer = createDesignApi(admin);
    const bm17 = (await designer.list()).find(f => f.code === "CNTS-BM17")!;
    const saved = await designer.save(bm17.code, bm17.version!, "BM17 có cột mở rộng", [...bm17.fields,
      { key: "cua", label: "Cua theo mẫu", type: "decimal", required: true, non_negative: true, unit: "tấn" }]);
    const publishing = createCntsPeriodApi(apiOf("kn-binh", null));
    const published = await publishing.publish({ code: bm17.code, template_id: saved.version.template_id, template_version: saved.version.version,
      from_date: "2099-01-20", to_date: "2099-01-26", units: ["91301"] });
    auth.role = "kn_xa"; auth.api = apiOf("kn-an", xaCtx);
    const u = userEvent.setup();
    const rendered = render(<CatchScreen />);
    const panel = await screen.findByRole("region", { name: "Biểu BM-17 theo phiên bản" });
    await u.click(within(panel).getByRole("button", { name: /Xem hoặc nhập.*Mỹ Hòa Hưng/ }));
    const extra = await screen.findByRole("textbox", { name: /Cua theo mẫu/ });
    for (const [label, value] of [[/TS-01/, "4"], [/TS-02/, "0"], [/TS-03/, "1"], [/TS-04/, "0"]] as const) {
      await u.type(screen.getByRole("textbox", { name: label }), value);
    }
    await u.click(screen.getByRole("button", { name: "Gửi báo cáo" }));
    expect(await screen.findByText("Bắt buộc nhập.")).toBeInTheDocument();
    await u.type(extra, "2,5");
    await u.click(screen.getByRole("button", { name: "Gửi báo cáo" }));
    const pid = published.periods[0]!.id!;
    await waitFor(async () => {
      const task = (await publishing.tasks("CNTS-BM17")).find(t => t.period_id === pid)!;
      expect(task.values).toMatchObject({ ca: "4", cua: "2.5" });
      expect(task.template_version).toBe(2);
    });
    const old = (await publishing.tasks("CNTS-BM17")).find(t => t.period_id !== pid)!;
    await waitFor(() => expect(screen.queryByRole("button", { name: "Gửi báo cáo" })).not.toBeInTheDocument());
    await u.selectOptions(within(screen.getByRole("region", { name: "Biểu BM-17 theo phiên bản" })).getByLabelText("Kỳ theo phiên bản"), old.period_id);
    await u.click(within(screen.getByRole("region", { name: "Biểu BM-17 theo phiên bản" })).getByRole("button", { name: /Xem hoặc nhập.*Mỹ Hòa Hưng/ }));
    expect(screen.getByRole("textbox", { name: /TS-01/ })).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: /Cua theo mẫu/ })).not.toBeInTheDocument();
    const oldTask = (await publishing.tasks("CNTS-BM17")).find(t => t.id === old.id)!;
    expect(oldTask.fields.some(f => f.key === "cua")).toBe(false);
    expect(oldTask.template_version).toBe(1);
    auth.role = "kn_tinh"; auth.api = apiOf("kn-binh", null); rendered.rerender(<CatchScreen />);
    await u.selectOptions(within(screen.getByRole("region", { name: "Biểu BM-17 theo phiên bản" })).getByLabelText("Kỳ theo phiên bản"), pid);
    await u.click(within(screen.getByRole("region", { name: "Biểu BM-17 theo phiên bản" })).getByRole("button", { name: /Xem hoặc nhập.*Mỹ Hòa Hưng/ }));
    expect(await screen.findByRole("textbox", { name: /Cua theo mẫu/ })).toHaveValue("2.5");
    expect(screen.getByRole("textbox", { name: /Cua theo mẫu/ })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Gửi báo cáo" })).not.toBeInTheDocument();
  });

});
