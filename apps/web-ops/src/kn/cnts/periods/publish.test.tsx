// BF-CN-PUBLISH (#108, CN-07): cấu hình phát hành biểu kỳ CNTS (đối tượng nhóm xã, người phụ trách, giờ chốt, xem trước
// số nhiệm vụ) và màn theo dõi (Đã nộp / Nháp / Chưa nhập / Trễ / Trả lại / Không nộp, nút Nhắc) — hàm thuần, backend giả
// lập và màn thật.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { ApiProblem } from "@/api/problem";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi, draftIndex, periodRows, trackState, type CntsDraft, type FormsTask } from "./api";
import { CntsTaskEntry } from "./CntsEntryScreen";
import { CntsPublishScreen, publishErrors, seriesBody, type PublishDraft } from "./CntsPublishScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/bieu-mau",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_tinh" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "chan_nuoi_thuy_san" } } }),
}));

const OWNER = "0192a0a0-0000-7000-8000-0000000c4301";

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

const rawClient = (personaId: string, ctx: string | null) =>
  createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx, fetch: mockFetchNoDelay });
const client = (personaId: string, ctx: string | null) => createCntsPeriodApi(rawClient(personaId, ctx));
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

async function installForms() {
  const adminCtx = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
  await client("kn-admin", adminCtx).install();
}

const base: PublishDraft = { from: "2099-01-06", to: "2099-01-12", target: "province", units: [], regions: [], dueTime: "", owner: OWNER, closeAfter: "" };

describe("CN-07 — hàm phía máy khách", () => {
  it("kiểm cấu hình: nhóm xã không cần chọn đơn vị; khu vực, từng xã, người phụ trách, giờ chốt bắt buộc đúng", () => {
    expect(publishErrors(base)).toEqual({});
    expect(publishErrors({ ...base, target: "regions" })).toEqual({ regions: "Chọn ít nhất một khu vực." });
    expect(publishErrors({ ...base, target: "communes" })).toEqual({ units: "Chọn ít nhất một đơn vị nhận." });
    expect(publishErrors({ ...base, target: "facilities" })).toEqual({ units: "Chọn ít nhất một đơn vị nhận." });
    expect(publishErrors({ ...base, owner: "" })).toEqual({ owner: "Chọn cán bộ tỉnh phụ trách biểu." });
    expect(publishErrors({ ...base, closeAfter: "-2" }).closeAfter).toBe("Không được nhập số âm.");
    expect(publishErrors({ ...base, closeAfter: "721" }).closeAfter).toBe("Tối đa 720 giờ (30 ngày) sau hạn.");
    expect(publishErrors({ ...base, closeAfter: "1,5" }).closeAfter).toBe("Chỉ nhập số nguyên.");
  });

  it("thân yêu cầu: nhóm xã không gửi danh sách đơn vị; khu vực, người phụ trách, giờ chốt, giờ hạn", () => {
    expect(seriesBody("CNTS-BM04", { ...base, units: ["91301"] }, undefined)).toEqual({
      code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "province", units: [], owner_ref: OWNER,
    });
    expect(seriesBody("CNTS-BM04", { ...base, target: "regions", regions: ["KV2"], closeAfter: "48", dueTime: "16:00" }, { deadline_offset_days: 1 })).toEqual({
      code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "regions", units: [], regions: ["KV2"], owner_ref: OWNER,
      close_after_hours: 48, deadline: { offset_days: 1, time: "16:00" },
    });
    expect(seriesBody("CNTS-BM01", { ...base, target: "facilities", units: ["f1"] }, undefined).units).toEqual(["f1"]);
  });

  it("trạng thái theo dõi: Trả lại > Không nộp > Đã nộp > Trễ > Nháp > Chưa nhập; nháp cũ hơn lần gửi bị bỏ qua", () => {
    const t = (x: Partial<FormsTask>) => ({ status: "not_submitted", version: 0, submitted_at: null, period_id: "p", unit: "u", ...x }) as FormsTask;
    const drafts = draftIndex({ drafts: [{ period_id: "p", unit: "u", saved_at: "2026-10-01T00:00:00Z" }] });
    expect(trackState(t({}), new Map())).toBe("not_entered");
    expect(trackState(t({}), drafts)).toBe("draft");
    expect(trackState(t({ status: "overdue" }), drafts)).toBe("overdue");
    expect(trackState(t({ status: "submitted", version: 1, submitted_at: "2026-10-02T00:00:00Z" }), drafts)).toBe("submitted");
    expect(trackState(t({ status: "returned", version: 1 }), drafts)).toBe("returned");
    expect(trackState(t({ status: "missed" }), drafts)).toBe("missed");
    expect(trackState(t({ submitted_at: "2026-10-02T00:00:00Z" }), drafts)).toBe("not_entered");
  });
});

describe("CN-07 — backend giả lập", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsPeriods();
  });

  it("đối tượng nhóm xã, người phụ trách, giờ chốt; theo dõi có Nháp, Chưa nhập, Đã nộp; Nhắc tối đa một lượt mỗi giờ", async () => {
    await installForms();
    const tinh = client("kn-binh", null);
    const xa = client("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san"));

    const scope = await tinh.preview({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "province", units: [] });
    expect(scope.communes.length).toBe(5);
    const kv1 = await tinh.preview({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "regions", regions: ["KV1"], units: [] });
    expect([kv1.units, kv1.tasks]).toEqual([["91301", "91302"], 2]);
    expect((await tinh.preview({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "aquaculture", units: [] })).units).toEqual(["91302", "91304"]);
    expect((await tinh.preview({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "model", units: [] })).units).toEqual(["91301", "91302", "91305"]);
    expect(await code(tinh.preview({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "regions", units: [] }))).toBe("validation_failed");
    expect(await code(tinh.preview({ code: "CNTS-BM01", from_date: "2099-01-06", to_date: "2099-01-06", target: "province", units: [] }))).toBe("validation_failed");

    const pub = await tinh.publish({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-12", target: "regions", regions: ["KV1"], units: [],
      owner_ref: OWNER, close_after_hours: 48 });
    expect(pub.publication_id).toBeTruthy();
    const pid = pub.periods[0]!.id!;

    // Xã lưu nháp → Cấp Tỉnh thấy Nháp; xã khác chưa nhập.
    const task = (await xa.tasks("CNTS-BM04")).find((t) => t.period_id === pid && t.unit === "91301")!;
    expect(await code(tinh.saveDraft(pid, { unit: "91301", values: {}, nil_report: false }))).toBe("forbidden");
    await xa.saveDraft(pid, { unit: "91301", values: { tong_dan_heo: "12" }, nil_report: false, note: "đang đếm" });
    expect((await xa.draft(pid, "91301"))?.values.tong_dan_heo).toBe("12");
    const tasks = await tinh.tasks("CNTS-BM04");
    const tr = await tinh.tracking("CNTS-BM04", [pid]);
    expect(tr.periods.find((p) => p.period_id === pid)).toMatchObject({ owner_ref: OWNER, close_after_hours: 48, target: "regions" });
    const row = periodRows(tasks, tr).find((r) => r.periodId === pid)!;
    expect(row).toMatchObject({ total: 2, draft: 1, notEntered: 1, submitted: 0, pending: 2 });
    expect(Date.parse(row.closeAt!) - Date.parse(row.dueAt)).toBe(48 * 3_600_000);

    // Gửi bài → nháp hết hiệu lực, Đã nộp.
    const vals = Object.fromEntries(task.fields.filter((f) => f.type !== "text").map((f) => [f.key, "1"]));
    await xa.submit(pid, { unit: "91301", values: vals, base_version: 0, nil_report: false });
    expect(await xa.draft(pid, "91301")).toBeNull();
    const after = periodRows(await tinh.tasks("CNTS-BM04"), await tinh.tracking("CNTS-BM04", [pid])).find((r) => r.periodId === pid)!;
    expect(after).toMatchObject({ submitted: 1, draft: 0, notEntered: 1, pending: 1 });

    // Nhắc: chỉ đơn vị chưa gửi; lần hai trong cùng giờ bị bỏ qua; Cấp Xã không nhắc được.
    expect(await tinh.remind(pid)).toEqual({ reminded: 1, skipped: [] });
    expect(await tinh.remind(pid)).toEqual({ reminded: 0, skipped: ["91302"] });
    expect(await code(xa.remind(pid))).toBe("forbidden");
  });
});

describe("CN-07 — màn phát hành và theo dõi", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsPeriods();
    auth.role = "kn_tinh";
    auth.api = rawClient("kn-binh", null);
  });

  it("chọn khu vực + người phụ trách + giờ chốt → xem trước số nhiệm vụ → phát hành → theo dõi có người phụ trách, giờ chốt, nút Nhắc", async () => {
    await installForms();
    const u = userEvent.setup();
    render(<CntsPublishScreen />);
    await u.click(await screen.findByRole("radio", { name: /Theo khu vực/ }));
    await u.click(await screen.findByRole("checkbox", { name: /Khu vực 2/ }));
    const from = screen.getByLabelText(/Từ ngày/);
    await u.clear(from);
    await u.type(from, "2099-01-06");
    const to = screen.getByLabelText(/Đến ngày/);
    await u.clear(to);
    await u.type(to, "2099-01-12");
    await u.type(screen.getByLabelText(/Giờ chốt sau hạn nộp/), "24");
    await waitFor(() => expect(screen.getByRole("option", { name: "Trần Bình (Cấp Tỉnh)" })).toBeInTheDocument());
    await u.selectOptions(screen.getByLabelText(/Cán bộ tỉnh phụ trách biểu/), OWNER);
    await u.click(screen.getByRole("button", { name: /Xem trước/ }));
    expect(await screen.findByText(/1 kỳ × 2 đơn vị = 2 nhiệm vụ/)).toBeInTheDocument();
    expect(within(screen.getByRole("table", { name: "Kỳ xem trước" })).getByRole("columnheader", { name: "Giờ chốt" })).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Phát hành" }));
    expect(await screen.findByText(/Đã phát hành 1 kỳ, 2 nhiệm vụ/)).toBeInTheDocument();

    const table = await screen.findByRole("table", { name: "Kỳ đã phát hành" });
    await waitFor(() => expect(within(table).getByText("Cán bộ tỉnh phụ trách (giả lập)")).toBeInTheDocument());
    for (const h of ["Giờ chốt", "Phụ trách", "Nháp", "Chưa nhập", "Trả lại", "Không nộp"]) expect(within(table).getByRole("columnheader", { name: h })).toBeInTheDocument();
    await u.click(within(table).getByRole("button", { name: "Đơn vị" }));
    const units = await screen.findByRole("table", { name: "Trạng thái từng đơn vị" });
    expect(within(units).getAllByText("Chưa nhập")).toHaveLength(2);
    await u.click(within(table).getByRole("button", { name: "Nhắc (2)" }));
    expect(await screen.findByText(/Đã nhắc 2 đơn vị/)).toBeInTheDocument();
  });
});

// #108 [P2] Codex: phản hồi nháp máy chủ đến chậm không được ghi đè nội dung người dùng vừa nhập.
describe("CN-07 — nạp nháp máy chủ khi mở công việc", () => {
  const task = (x: Partial<FormsTask> = {}) =>
    ({
      id: "race", period_id: "p", unit: "91301", open: true, version: 0, values: {}, nil_report: false, note: "", explanation: "",
      fields: [{ key: "ghi_nhan", label: "Ghi nhận", type: "text" }], status: "not_submitted", due_at: "2099-01-01T00:00:00Z",
      submitted_at: null, template_title: "BM-04", label: "Tuần 1", ...x,
    }) as unknown as FormsTask;
  const server: CntsDraft = { period_id: "p", unit: "91301", values: { ghi_nhan: "số cũ" }, nil_report: true, note: "nhập cũ", saved_at: "2099-01-01T08:00:00Z" };

  function open(t: FormsTask) {
    let resolve!: (d: CntsDraft | null) => void;
    const pending = new Promise<CntsDraft | null>((r) => (resolve = r));
    const draft = vi.fn(() => pending);
    const capi = { draft } as unknown as Parameters<typeof CntsTaskEntry>[0]["capi"];
    render(<CntsTaskEntry task={t} unitLabel="Mỹ Hòa Hưng" nilLabel="Không phát sinh" capi={capi} onDone={() => undefined} />);
    return {
      draft,
      settle: async (d: CntsDraft | null) => {
        await act(async () => {
          resolve(d);
          await pending;
        });
      },
    };
  }
  const value = () => screen.getByLabelText(/Ghi nhận/);
  const note = () => screen.getByLabelText("H08 Ghi chú chung");
  const nil = () => screen.getByRole("checkbox", { name: /Không phát sinh/ });

  beforeEach(() => {
    window.localStorage.clear();
    auth.api = null;
  });

  it.each([
    ["số liệu", () => fireEvent.change(value(), { target: { value: "số mới" } }), { v: "số mới", n: "", nil: false }],
    ["ghi chú", () => fireEvent.change(note(), { target: { value: "nội dung mới" } }), { v: "", n: "nội dung mới", nil: false }],
    ["không phát sinh", () => fireEvent.click(nil()), { v: "", n: "", nil: true }],
  ])("người dùng sửa %s trước khi nháp máy chủ về → giữ nội dung đã nhập, báo có nháp máy chủ, bấm nạp mới thay", async (_, edit, want) => {
    const { settle } = open(task());
    edit();
    await settle(server);
    expect(value()).toHaveValue(want.v);
    expect(note()).toHaveValue(want.n);
    expect(nil()).toHaveProperty("checked", want.nil);
    const notice = screen.getByText(/Có bản nháp đã lưu trên máy chủ/);
    fireEvent.click(within(notice.parentElement!).getByRole("button", { name: "Nạp nháp máy chủ" }));
    expect(value()).toHaveValue("số cũ");
    expect(note()).toHaveValue("nhập cũ");
    expect(nil()).toHaveProperty("checked", true);
    expect(screen.queryByText(/Có bản nháp đã lưu trên máy chủ/)).toBeNull();
  });

  it("chưa sửa gì → nháp máy chủ về được nạp (số liệu, ghi chú, không phát sinh), không hiện thông báo", async () => {
    const { settle } = open(task());
    await settle(server);
    expect(value()).toHaveValue("số cũ");
    expect(note()).toHaveValue("nhập cũ");
    expect(nil()).toHaveProperty("checked", true);
    expect(screen.queryByText(/Có bản nháp đã lưu trên máy chủ/)).toBeNull();
  });

  it("nháp máy chủ không mới hơn lần gửi gần nhất → bỏ qua", async () => {
    const sent = task({ version: 1, submitted_at: "2099-01-01T09:00:00Z", values: { ghi_nhan: "đã gửi" }, note: "bản gửi" });
    const { settle } = open(sent);
    await settle(server);
    expect(value()).toHaveValue("đã gửi");
    expect(note()).toHaveValue("bản gửi");
    expect(nil()).toHaveProperty("checked", false);
    expect(screen.queryByText(/Có bản nháp đã lưu trên máy chủ/)).toBeNull();
  });

  it("có nháp trên máy (lưu sau mỗi thao tác, mới hơn nháp máy chủ) → dùng nháp trên máy, không hỏi máy chủ", async () => {
    window.localStorage.setItem("cnts-draft:race:v0", JSON.stringify({ values: { ghi_nhan: "trên máy" }, nil: false, note: "ghi chú máy", explanation: "" }));
    const { draft, settle } = open(task());
    await settle(server);
    expect(draft).not.toHaveBeenCalled();
    expect(value()).toHaveValue("trên máy");
    expect(note()).toHaveValue("ghi chú máy");
  });
});
