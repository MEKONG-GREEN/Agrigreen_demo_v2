// BF-CN-XA-DATA #175 (BRD CNTS v3.7 FN-09): Số liệu cấp xã › Nhập liệu tuần BM-04 — Cấp Tỉnh.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi, type FormsField, type FormsSubmission, type FormsTask } from "./api";
import { BM04_CODE, bm04Data, cellText, cellValue, changesBetween, columnGroups, columnTotal, cumulative, editedAfterAccept, historyFields, isFlow } from "./bm04";
import { Bm04ProvinceScreen } from "./Bm04ProvinceScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/so-lieu-xa/nhap-lieu-tuan",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_tinh" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: auth.api,
    principal: { subject: "00000000-0000-7000-8000-00000000b1b1", displayName: "Trần Thị Bình" },
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

const F = (key: string, group: string, type: "integer" | "decimal" | "text" = "integer"): FormsField =>
  ({ key, label: key, type, required: type !== "text", unit: "con", group }) as FormsField;
const FIELDS = [F("tong_dan_heo", "CN-01"), F("tiem_ta_heo_cp", "CN-02"), F("hoa_chat_gia_suc", "CN-04", "decimal"), F("ghi_chu", "", "text")];

const task = (week: number, unit: string, extra: Partial<FormsTask> = {}): FormsTask => {
  const from = new Date(Date.UTC(2026, 0, 6 + (week - 1) * 7)).toISOString().slice(0, 10);
  const to = new Date(Date.UTC(2026, 0, 12 + (week - 1) * 7)).toISOString().slice(0, 10);
  return {
    id: `${week}:${unit}`,
    period_id: `p${week}`,
    unit,
    template_code: BM04_CODE,
    label: `Tuần ${week}/2026`,
    period_key: `2026-W${String(week).padStart(2, "0")}`,
    period_from: from,
    period_to: to,
    due_at: "2026-12-31T00:00:00Z",
    period_status: "open",
    open: true,
    status: "submitted",
    version: 1,
    submitted_at: `${from}T05:00:00Z`,
    values: {},
    nil_report: false,
    fields: FIELDS,
    ...extra,
  } as unknown as FormsTask;
};

const sub = (version: number, status: FormsSubmission["status"], values: Record<string, string>): FormsSubmission =>
  ({ id: `s${version}`, version, status, values, review: status === "submitted" ? null : { decision: status }, nil_report: false }) as unknown as FormsSubmission;

describe("BM-04 Tỉnh — hàm thuần", () => {
  it("AC-03: lũy kế tuần 39 = 15 và Thiếu kỳ 38; nộp bù tuần 38 (3 liều) → 18; tổng đàn không lũy kế", () => {
    const a = (w: number, v: string | null) =>
      v === null ? task(w, "A", { version: 0, status: "overdue", submitted_at: null }) : task(w, "A", { values: { tiem_ta_heo_cp: v, tong_dan_heo: "100" } });
    const before = bm04Data([a(37, "10"), a(38, null), a(39, "5")]);
    const c = cumulative(before, "A", "2026-W39");
    expect(c.values).toEqual({ tiem_ta_heo_cp: "15", hoa_chat_gia_suc: null });
    expect(c.missing.map((w) => w.key)).toEqual(["2026-W38"]);
    expect(c.counted).toBe(2);
    const after = bm04Data([a(37, "10"), a(38, "3"), a(39, "5")]);
    expect(cumulative(after, "A", "2026-W39").values.tiem_ta_heo_cp).toBe("18");
    expect(cumulative(after, "A", "2026-W39").missing).toEqual([]);
    // Tuần đang xem là 38: không cộng tuần sau.
    expect(cumulative(after, "A", "2026-W38").values.tiem_ta_heo_cp).toBe("13");
    expect(isFlow("tong_dan_heo")).toBe(false);
    expect(isFlow("ksgm_gia_suc")).toBe(true);
  });

  it("lũy kế quý chỉ cộng tuần cùng quý; tuần bị trả lại chưa cộng; báo không phát sinh = 0", () => {
    const d = bm04Data([
      task(12, "A", { values: { tiem_ta_heo_cp: "7" } }), // 24–30/3 → quý 1
      task(14, "A", { values: { tiem_ta_heo_cp: "2" } }), // 7–13/4 → quý 2
      task(15, "A", { status: "returned", values: { tiem_ta_heo_cp: "50" } }),
      task(16, "A", { nil_report: true, values: { tiem_ta_heo_cp: "0" } }),
    ]);
    expect(cumulative(d, "A", "2026-W16", "quarter").values.tiem_ta_heo_cp).toBe("2");
    expect(cumulative(d, "A", "2026-W16", "year").values.tiem_ta_heo_cp).toBe("9");
    expect(cumulative(d, "A", "2026-W16").returned.map((w) => w.key)).toEqual(["2026-W15"]);
    expect(cellValue(d.cell("2026-W16", "A"), "tiem_ta_heo_cp")).toBe("0");
    expect(cellValue(task(1, "B", { version: 0 }), "tiem_ta_heo_cp")).toBeNull();
  });

  it("chỉ trường số, đúng thứ tự mẫu; nhóm cột; dòng TỔNG cộng thập phân chính xác; bỏ kỳ đã hủy", () => {
    const d = bm04Data([task(1, "A"), task(2, "B", { period_status: "cancelled" }), task(1, "C", { template_code: "CNTS-BM17" })]);
    expect(d.fields.map((f) => f.key)).toEqual(["tong_dan_heo", "tiem_ta_heo_cp", "hoa_chat_gia_suc"]);
    expect(d.weeks.map((w) => w.key)).toEqual(["2026-W01"]);
    expect(d.units).toEqual(["A"]);
    expect(columnGroups(d.fields)).toEqual([
      { group: "CN-01", span: 1 },
      { group: "CN-02", span: 1 },
      { group: "CN-04", span: 1 },
    ]);
    expect(columnTotal(["0.1", "0.2", null])).toBe("0.3");
    expect(columnTotal([null, null])).toBeNull();
  });

  it("#175 review: cột bảng tuần gồm CN-10 Ghi chú ở cuối biểu; ghi chú giữ nguyên văn, không quy 0", () => {
    const d = bm04Data([task(1, "A", { values: { tiem_ta_heo_cp: "3", ghi_chu: "Heo bệnh tai xanh" } }), task(1, "B", { nil_report: true, values: {} })]);
    expect(d.columns.map((f) => f.key)).toEqual(["tong_dan_heo", "tiem_ta_heo_cp", "hoa_chat_gia_suc", "ghi_chu"]);
    expect(d.fields.map((f) => f.key)).not.toContain("ghi_chu"); // không vào lũy kế / TỔNG
    expect(cellText(d.cell("2026-W01", "A"), "ghi_chu")).toBe("Heo bệnh tai xanh");
    expect(cellText(d.cell("2026-W01", "B"), "ghi_chu")).toBeNull();
    expect(cellText(task(1, "C", { version: 0, values: { ghi_chu: "nháp" } }), "ghi_chu")).toBeNull();
  });

  it("AC-06 cờ Đã sửa sau xác thực; AC-05 lịch sử giá trị cũ → mới", () => {
    expect(editedAfterAccept([sub(2, "submitted", {}), sub(1, "accepted", {})])).toBe(true);
    expect(editedAfterAccept([sub(2, "accepted", {}), sub(1, "accepted", {})])).toBe(false);
    expect(editedAfterAccept([sub(2, "submitted", {}), sub(1, "returned", {})])).toBe(false);
    expect(editedAfterAccept([sub(1, "accepted", {})])).toBe(false);
    const ch = changesBetween(FIELDS, sub(1, "accepted", { tiem_ta_heo_cp: "10", tong_dan_heo: "5" }), sub(2, "submitted", { tiem_ta_heo_cp: "12", tong_dan_heo: "5" }));
    expect(ch).toEqual([{ key: "tiem_ta_heo_cp", label: "CN-02 › tiem_ta_heo_cp", old: "10", new: "12" }]);
  });

  it("#175 review: lịch sử dùng đủ trường của kỳ — chỉ đổi ghi chú vẫn có dòng thay đổi cũ → mới", () => {
    const d = bm04Data([task(1, "A")]);
    const hf = historyFields(task(1, "A"), d.columns);
    expect(hf.map((f) => f.key)).toEqual(["tong_dan_heo", "tiem_ta_heo_cp", "hoa_chat_gia_suc", "ghi_chu"]);
    expect(historyFields(task(1, "A", { fields: [] }), d.columns)).toBe(d.columns);
    const older = sub(1, "accepted", { tiem_ta_heo_cp: "10", ghi_chu: "cũ" });
    const newer = sub(2, "submitted", { tiem_ta_heo_cp: "10", ghi_chu: "mới" });
    expect(changesBetween(hf, older, newer)).toEqual([{ key: "ghi_chu", label: "ghi_chu", old: "cũ", new: "mới" }]);
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

describe("màn Nhập liệu tuần — Cấp Tỉnh", () => {
  const y = new Date().getFullYear();
  async function setup(owner?: string, note?: string) {
    resetKnAdmin();
    resetCntsPeriods();
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active").catch(() => undefined);
    await createCntsPeriodApi(apiOf("kn-admin", adminCtx)).install();
    const tinh = createCntsPeriodApi(apiOf("kn-binh", null));
    await tinh.publish({ code: BM04_CODE, from_date: `${y}-01-07`, to_date: `${y}-01-07`, units: ["91301", "91302"], ...(owner ? { owner_ref: owner } : {}) });
    const xa = createCntsPeriodApi(apiOf("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san")));
    const t = (await xa.tasks(BM04_CODE)).find((x) => x.unit === "91301")!;
    const values = Object.fromEntries(t.fields.filter((f) => f.type !== "text").map((f) => [f.key, "1"]));
    const v1 = { ...values, tiem_ta_heo_cp: "12", ...(note ? { ghi_chu: note } : {}) };
    await xa.submit(t.period_id, { unit: "91301", values: v1, base_version: 0, nil_report: false, explanation: "Mất mạng" });
    auth.role = "kn_tinh";
    auth.api = apiOf("kn-binh", null);
    return { xa, periodId: t.period_id, v1 };
  }

  beforeEach(() => {
    auth.api = null;
  });

  it("bảng một dòng một xã + TỔNG, lọc trạng thái; người xác thực khi kỳ chưa phân công người phụ trách", async () => {
    await setup();
    const u = userEvent.setup();
    render(<Bm04ProvinceScreen />);
    const table = await screen.findByRole("table", { name: "Số liệu BM-04 theo xã" });
    await waitFor(() => expect(within(table).getByText("Mỹ Hòa Hưng")).toBeInTheDocument());
    expect(within(table).getByText("Bình Đức")).toBeInTheDocument();
    expect(within(table).getByText("TỔNG")).toBeInTheDocument();
    expect(within(table).getAllByText("12").length).toBeGreaterThanOrEqual(2); // ô xã + dòng tổng
    expect(screen.getByRole("combobox", { name: "Khu vực" })).toBeInTheDocument();
    await u.selectOptions(screen.getByRole("combobox", { name: "Trạng thái nộp" }), "submitted");
    await waitFor(() => expect(within(table).queryByText("Bình Đức")).not.toBeInTheDocument());
    // Chế độ lũy kế: cột tiêm phòng, không cột tổng đàn.
    await u.selectOptions(screen.getByRole("combobox", { name: "Hiển thị" }), "year");
    expect(within(table).getByRole("columnheader", { name: "Thiếu kỳ" })).toBeInTheDocument();
    expect(within(table).queryByRole("columnheader", { name: /^Heo \(con\)$/ })).not.toBeInTheDocument();
    await u.click(within(table).getByRole("button", { name: "Chi tiết Mỹ Hòa Hưng" }));
    const detail = await screen.findByRole("region", { name: "Chi tiết Mỹ Hòa Hưng" });
    expect(within(detail).getByText(/lý do sửa sau hạn: Mất mạng/i)).toBeInTheDocument();
    await waitFor(() => expect(within(detail).getByText(/Lịch sử phiên bản gửi \(1\)/)).toBeInTheDocument());
    await u.click(within(detail).getByRole("button", { name: "Xác thực" }));
    await waitFor(() => expect(screen.getAllByText("Đã xác thực").length).toBeGreaterThan(0));
    // Không có nút sửa số liệu xã.
    expect(screen.queryByRole("button", { name: /Gửi|Lưu/ })).not.toBeInTheDocument();
  });

  it("#175 review (UR-XA-07): Số liệu tuần có cột CN-10 Ghi chú ở cuối biểu, hiện ghi chú xã; TỔNG để trống; lũy kế không có cột này", async () => {
    await setup(undefined, "Dịch tả heo châu Phi ở ấp 3");
    const u = userEvent.setup();
    render(<Bm04ProvinceScreen />);
    const table = await screen.findByRole("table", { name: "Số liệu BM-04 theo xã" });
    await waitFor(() => expect(within(table).getByText("Mỹ Hòa Hưng")).toBeInTheDocument());
    const [, fieldHeader] = within(table).getAllByRole("row");
    const heads = within(fieldHeader!).getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads.at(-1)).toBe("CN-10 Ghi chú");
    const col = 3 + heads.length - 1; // STT, Xã, Trạng thái, rồi các cột biểu
    const rowOf = (text: string) => within(table).getByText(text).closest("tr")!;
    expect(within(rowOf("Mỹ Hòa Hưng")).getAllByRole("cell")[col]).toHaveTextContent("Dịch tả heo châu Phi ở ấp 3");
    expect(within(rowOf("Bình Đức")).getAllByRole("cell")[col]).toHaveTextContent("—");
    // Dòng TỔNG: ô TỔNG gộp 3 cột → ô ghi chú ở vị trí heads.length, để trống (không cộng trường chữ).
    expect(within(rowOf("TỔNG")).getAllByRole("cell")[heads.length]).toHaveTextContent(/^$/);
    await u.selectOptions(screen.getByRole("combobox", { name: "Hiển thị" }), "year");
    expect(within(table).queryByRole("columnheader", { name: "CN-10 Ghi chú" })).not.toBeInTheDocument();
  });

  it("#175 review (AC-05): lịch sử phiên bản hiện thay đổi chỉ ở CN-10 Ghi chú và lý do đã lưu của từng phiên bản", async () => {
    const { xa, periodId, v1 } = await setup(undefined, "Heo sốt nhẹ");
    // Phiên bản 2: số liệu giữ nguyên, chỉ đổi ghi chú, có lý do riêng.
    await xa.submit(periodId, { unit: "91301", values: { ...v1, ghi_chu: "Đã khỏi, theo dõi thêm" }, base_version: 1, nil_report: false, explanation: "Bổ sung ghi chú" });
    const u = userEvent.setup();
    render(<Bm04ProvinceScreen />);
    const table = await screen.findByRole("table", { name: "Số liệu BM-04 theo xã" });
    await waitFor(() => expect(within(table).getByText("Mỹ Hòa Hưng")).toBeInTheDocument());
    await u.click(within(table).getByRole("button", { name: "Chi tiết Mỹ Hòa Hưng" }));
    const detail = await screen.findByRole("region", { name: "Chi tiết Mỹ Hòa Hưng" });
    await waitFor(() => expect(within(detail).getByText(/Lịch sử phiên bản gửi \(2\)/)).toBeInTheDocument());
    const items = within(detail).getAllByRole("listitem").filter((li) => /^Phiên bản \d/.test(li.textContent ?? ""));
    const v2 = items.find((li) => li.textContent!.startsWith("Phiên bản 2"))!;
    const ver1 = items.find((li) => li.textContent!.startsWith("Phiên bản 1"))!;
    expect(within(v2).getByText("CN-10 Ghi chú: Heo sốt nhẹ → Đã khỏi, theo dõi thêm")).toBeInTheDocument();
    expect(within(v2).queryByText(/Không đổi/)).not.toBeInTheDocument();
    expect(within(v2).getByText("Lý do: Bổ sung ghi chú")).toBeInTheDocument();
    expect(within(ver1).getByText("Lý do: Mất mạng")).toBeInTheDocument();
  });

  it("người không phụ trách biểu chỉ xem", async () => {
    await setup("00000000-0000-7000-8000-0000000000aa");
    const u = userEvent.setup();
    render(<Bm04ProvinceScreen />);
    const table = await screen.findByRole("table", { name: "Số liệu BM-04 theo xã" });
    await waitFor(() => expect(within(table).getByText("Mỹ Hòa Hưng")).toBeInTheDocument());
    await u.click(within(table).getByRole("button", { name: "Chi tiết Mỹ Hòa Hưng" }));
    const detail = await screen.findByRole("region", { name: "Chi tiết Mỹ Hòa Hưng" });
    await waitFor(() => expect(within(detail).getByText(/bạn chỉ xem/)).toBeInTheDocument());
    expect(within(detail).queryByRole("button", { name: "Xác thực" })).not.toBeInTheDocument();
  });

  it("#175 review: máy chủ (giả lập) chặn người không phụ trách gọi thẳng API rà soát", async () => {
    await setup("00000000-0000-7000-8000-0000000000aa");
    const tinh = createCntsPeriodApi(apiOf("kn-binh", null));
    const t = (await tinh.tasksIn(BM04_CODE, `${y}-01-01`, `${y}-12-31`)).find((x) => x.unit === "91301" && x.submission_id)!;
    await expect(tinh.review(t.submission_id!, "accepted")).rejects.toMatchObject({ status: 403 });
    await expect(tinh.review(t.submission_id!, "returned", "Sai số")).rejects.toMatchObject({ status: 403 });
  });
});

describe("tasksIn — đọc hết các trang next_cursor (#175 review)", () => {
  it("nối các trang tới khi next_cursor null; quá số trang thì báo lỗi, không trả thiếu", async () => {
    const page = (n: number, next: string | null) => ({ items: [{ period_id: `p${n}`, unit: "91301" }], next_cursor: next });
    const seen: (string | null)[] = [];
    const fetchPages = async (req: Request) => {
      const cur = new URL(req.url).searchParams.get("cursor");
      seen.push(cur);
      const body = cur === null ? page(1, "c1") : cur === "c1" ? page(2, "c2") : page(3, null);
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    };
    const api = createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "x", getContextId: () => null, fetch: fetchPages }));
    const all = await api.tasksIn(BM04_CODE, "2026-01-01", "2026-12-31");
    expect(all.map((t) => t.period_id)).toEqual(["p1", "p2", "p3"]);
    expect(seen).toEqual([null, "c1", "c2"]);
    const endless = async () =>
      new Response(JSON.stringify(page(9, "again")), { status: 200, headers: { "Content-Type": "application/json" } });
    const api2 = createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "x", getContextId: () => null, fetch: endless }));
    await expect(api2.tasksIn(BM04_CODE, "2026-01-01", "2026-12-31", 200, 3)).rejects.toThrow(/Quá nhiều kỳ/);
  });
});
