import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import * as StaffScreens from "./StaffScreens";
import { hrmDashboardAlerts } from "./dashboard-alerts";
import type { PromotionList } from "./promotion-api";
import { createStaffApi, type HrmStaffEvent, type HrmStaffItem } from "./staff-api";
import {
  currentSpell,
  filterWork,
  inMonth,
  monthLabel,
  nextMonth,
  planWork,
  recentMonths,
  reportImpact,
  validateWork,
  workKpis,
} from "./work-info";
import { WorkEditor, HrmWorkInfoDetailScreen, HrmWorkInfoEditScreen, HrmWorkInfoScreen } from "./WorkInfoScreens";

// W11.29 — FN-08 Thông tin làm việc (màn `tl`, `tl.d` của prototype HCTH) và khối Cảnh báo của Dashboard nhân sự.

const push = vi.fn();
const params: { id: string } = { id: "" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/ho-so/thong-tin-lam-viec",
  useRouter: () => ({ replace: vi.fn(), push }),
  useParams: () => params,
}));
const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));

async function clientFor(personaId: string, assignmentId: string, department = "trong_trot") {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: department }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, sa: createStaffApi(api) };
}

const ev = (id: string, kind: string, from: string, extra: Partial<HrmStaffEvent> = {}): HrmStaffEvent => ({
  id,
  kind,
  valid_from: from,
  precision: "day",
  valid_to: null,
  payload: {},
  void: false,
  supersedes: null,
  reason: "",
  version: Number(id.replace(/\D/g, "")) || 1,
  recorded_at: "2026-10-01T00:00:00Z",
  actor_ref: "00000000-0000-0000-0000-000000000000",
  effective: true,
  ...extra,
});

const item = (code: string, status: "active" | "left", started: string | null, ended: string | null) =>
  ({ id: code, code, current: { status, started_on: started, ended_on: ended } }) as unknown as HrmStaffItem;

describe("FN-08 Thông tin làm việc — logic màn", () => {
  it("tháng: thuộc tháng, nhãn, tháng sau, danh sách tháng gần nhất", () => {
    expect(inMonth("2026-09-15", "2026-09")).toBe(true);
    expect(inMonth("2026", "2026-09")).toBe(false);
    expect(inMonth(null, "2026-09")).toBe(false);
    expect(monthLabel("2026-09")).toBe("Tháng 09/2026");
    expect(nextMonth("2026-12")).toBe("2027-01");
    expect(recentMonths("2026-02", 3)).toEqual(["2026-02", "2026-01", "2025-12"]);
  });

  it("thẻ số liệu và lọc biến động theo tháng (prototype `tl`)", () => {
    const rows = [
      item("A", "active", "2026-09-03", null),
      item("B", "left", "2010-01-01", "2026-09-30"),
      item("C", "active", "2015-01-01", null),
      item("D", "left", "2012-01-01", "2026-08-01"),
    ];
    expect(workKpis(rows, "2026-09")).toEqual({ active: 2, joined: 1, left: 1 });
    const codes = (f: Parameters<typeof filterWork>[1]) => filterWork(rows, f, "2026-09").map((r) => r.code);
    expect(codes({ status: "", month: "", move: "" })).toEqual(["A", "B", "C", "D"]);
    expect(codes({ status: "active", month: "", move: "" })).toEqual(["A", "C"]);
    expect(codes({ status: "", month: "2026-09", move: "vao" })).toEqual(["A"]);
    expect(codes({ status: "", month: "2026-09", move: "nghi" })).toEqual(["B"]);
    expect(codes({ status: "", month: "2026-08", move: "" })).toEqual(["D"]);
    expect(codes({ status: "left", month: "", move: "nghi" })).toEqual(["B"]);
  });

  it("ảnh hưởng đến báo cáo: NS02 tháng nghỉ, NS01 không còn từ tháng sau", () => {
    expect(reportImpact("2026-12-15")).toEqual({ ns02: "Có trong tháng 12/2026", ns01: "Không còn từ tháng 01/2027" });
    expect(reportImpact(null)).toEqual({ ns02: "Không", ns01: "Có trong danh sách" });
  });

  it("lần vào làm hiện tại bỏ qua dòng đã thay/hủy; tái tuyển dụng bắt đầu lần mới", () => {
    const evs = [
      ev("e1", "work_start", "2010-01-01"),
      ev("e2", "work_end", "2015-01-01"),
      ev("e3", "work_start", "2018-01-01"),
      ev("e4", "work_end", "2020-01-01", { effective: false }),
    ];
    const sp = currentSpell(evs);
    expect(sp.start?.id).toBe("e3");
    expect(sp.end).toBeNull();
  });

  it("kế hoạch ghi sổ: thêm nghỉ việc, sửa ngày (replace), bỏ ngày nghỉ (void); lý do sửa bắt buộc", () => {
    const evs = [ev("e1", "work_start", "2012-03-01")];
    const base = { start: "2012-03-01", end: "", leaveReason: "", decisionNo: "", note: "" };
    expect(validateWork(evs, base)?.message).toBe("Không có thay đổi để lưu.");
    expect(validateWork(evs, { ...base, start: "" })?.fields).toEqual(["start"]);
    expect(validateWork(evs, { ...base, end: "2011-01-01", leaveReason: "nghi_huu" })?.message).toMatch(/không được trước/);
    expect(validateWork(evs, { ...base, end: "2026-09-30" })?.fields).toEqual(["leaveReason"]);
    const add = { ...base, end: "2026-09-30", leaveReason: "nghi_huu", decisionNo: " 215/QĐ " };
    expect(validateWork(evs, add)).toBeNull();
    expect(planWork(evs, add)).toEqual([
      { op: "add", body: { kind: "work_end", valid_from: "2026-09-30", payload: { reason: "nghi_huu", decision_no: "215/QĐ" } } },
    ]);
    const moved = { ...base, start: "2012-04-01" };
    expect(validateWork(evs, moved)?.fields).toEqual(["note"]);
    const ops = planWork(evs, { ...moved, note: "Sai ngày theo quyết định gốc" });
    expect(ops).toHaveLength(1);
    expect(ops[0]).toMatchObject({ op: "replace", event: "e1", body: { kind: "work_start", valid_from: "2012-04-01" } });
    const ended = [...evs, ev("e2", "work_end", "2026-09-30", { payload: { reason: "nghi_huu" } })];
    expect(planWork(ended, { ...base, note: "Hủy nghỉ việc nhập nhầm" })).toEqual([
      { op: "void", event: "e2", reason: "Hủy nghỉ việc nhập nhầm" },
    ]);
  });
});

describe("Dashboard nhân sự — Cảnh báo (prototype `dash`)", () => {
  const promo = (items: Partial<PromotionList["items"][number]>[], missing = 0) =>
    ({ items, missing: Array.from({ length: missing }, () => ({})) }) as unknown as PromotionList;
  it("nâng bậc chưa đủ/cần xem xét, số đến hạn trong 3 tháng, chưa tính được; biến động tháng", () => {
    const a = hrmDashboardAlerts(
      promo(
        [
          { key: "k1", kind: "nltx", status: "pending", cond: "ok", name: "A", code: "VC-1", due: "2026-11-01", why: [] },
          { key: "k2", kind: "tnvk", status: "pending", cond: "review", name: "B", code: "VC-2", due: "2026-12-01", why: ["Thiếu xếp loại 2025"] },
          { key: "k3", kind: "nlth", status: "pending", cond: "fail", name: "C", code: "VC-3", due: "2026-12-01", why: [] },
        ],
        2,
      ),
      { joined: 1, left: 0 },
      "2026-10-21T00:00:00+07:00",
    );
    expect(a.map((x) => x.id)).toEqual(["promo-k2", "promo-due", "promo-missing", "work-moves"]);
    expect(a[0]).toMatchObject({ level: "attention", title: "B cần xem xét nâng bậc", href: "/kn/nhan-su/nang-bac/k2" });
    expect(a[0]!.description).toBe("Đến kỳ 01/12/2026 · Thiếu xếp loại 2025");
    expect(a[1]!.title).toBe("2 viên chức đến hạn nâng bậc trong 3 tháng tới");
    expect(a[3]!.title).toBe("Biến động trong tháng: +1 vào làm / −0 nghỉ việc");
  });
  it("lãnh đạo phòng (không có danh sách nâng bậc), không biến động → không cảnh báo", () => {
    expect(hrmDashboardAlerts(null, { joined: 0, left: 0 }, "2026-10-21T00:00:00+07:00")).toEqual([]);
  });
});

describe("màn Thông tin làm việc (giả lập API /v1/hrm/staff*)", () => {
  beforeEach(() => {
    push.mockClear();
    resetHrm();
    resetHrmStaff();
  });

  it("Admin: thẻ số liệu, cập nhật ngày nghỉ việc qua route → Đã nghỉ việc; chi tiết có ảnh hưởng NS01/NS02", async () => {
    const c = await clientFor("kn-admin", "asg-admin");
    auth.api = c.api;
    const user = userEvent.setup();
    const list = render(<HrmWorkInfoScreen />);
    const table = await screen.findByRole("table", { name: "Thông tin làm việc" });
    expect(await within(table).findByText("VC-00001")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Đang làm việc/ })).toHaveTextContent("3");

    const vc = (await c.sa.list({ q: "VC-00001" })).items[0]!;
    expect(screen.getByRole("link", { name: "Cập nhật VC-00001" })).toHaveAttribute("href", `/kn/nhan-su/ho-so/thong-tin-lam-viec/${vc.id}/sua`);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    list.unmount(); params.id = vc.id;
    const editor = render(<HrmWorkInfoEditScreen />);
    await screen.findByRole("heading", { name: /Cập nhật thông tin làm việc/ });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Không có thay đổi để lưu.");
    await user.type(screen.getByLabelText("Ngày nghỉ việc"), "2026-09-30");
    await waitFor(() => expect(screen.getByRole("combobox", { name: /Lý do nghỉ/ })).toBeEnabled());
    await user.selectOptions(screen.getByRole("combobox", { name: /Lý do nghỉ/ }), "nghi_huu");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith(`/kn/nhan-su/ho-so/thong-tin-lam-viec/${vc.id}`));
    editor.unmount();

    const vc1 = (await c.sa.list({ q: "VC-00001" })).items[0]!;
    expect(vc1.current).toMatchObject({ status: "left", ended_on: "2026-09-30", leave_reason: "nghi_huu" });
    params.id = vc1.id;
    render(<HrmWorkInfoDetailScreen />);
    expect(await screen.findByText(/Nguyễn Thị Mẫu · VC-00001/)).toBeInTheDocument();
    expect(screen.getByText("Có trong tháng 09/2026")).toBeInTheDocument();
    expect(screen.getByText("Không còn từ tháng 10/2026")).toBeInTheDocument();
    expect(screen.getByText("Nghỉ hưu")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Mở hồ sơ nhân viên" })).toHaveAttribute("href", `/kn/nhan-su/ho-so/${vc1.id}`);
  });

  it("Cấp Xã: máy chủ từ chối, màn không hiện bảng", async () => {
    auth.api = (await clientFor("kn-loc", "asg-loc-xa", "chan_nuoi_thuy_san")).api;
    render(<HrmWorkInfoScreen />);
    expect(await screen.findByText("Bạn không có quyền thực hiện thao tác này.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
  it("route sửa: lỗi từng trường chặn ghi; Hủy về danh sách", async () => {
    const c = await clientFor("kn-admin", "asg-admin"); auth.api = c.api;
    const vc = (await c.sa.list({ q: "VC-00001" })).items[0]!; params.id = vc.id;
    const before = (await c.sa.get(vc.id)).events.length;
    const user = userEvent.setup(); render(<HrmWorkInfoEditScreen />);
    const start = await screen.findByLabelText(/Ngày vào làm/);
    await user.clear(start); await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(start).toHaveAttribute("aria-invalid", "true");
    expect(start).toHaveAccessibleDescription("Nhập ngày vào làm.");
    expect((await c.sa.get(vc.id)).events).toHaveLength(before);
    await user.type(start, "2012-03-01");
    await user.type(screen.getByLabelText("Ngày nghỉ việc"), "2026-09-30");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("combobox", { name: /Lý do nghỉ/ })).toHaveAttribute("aria-invalid", "true");
    expect((await c.sa.get(vc.id)).events).toHaveLength(before);
    await user.click(screen.getByRole("button", { name: "Hủy" }));
    expect(push).toHaveBeenCalledWith("/kn/nhan-su/ho-so/thong-tin-lam-viec");
  });
  it("route sửa trực tiếp: cấp Xã không có form ghi", async () => {
    auth.api = (await clientFor("kn-loc", "asg-loc-xa", "chan_nuoi_thuy_san")).api;
    params.id = "00000000-0000-0000-0000-000000000001"; render(<HrmWorkInfoEditScreen />);
    expect(await screen.findByText("Bạn không có quyền thực hiện thao tác này.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
  });

  it("validation guard: ngày nghỉ trước ngày vào làm không gọi API ghi", async () => {
    const c = await clientFor("kn-admin", "asg-admin"); auth.api = c.api;
    const vc = (await c.sa.list({ q: "VC-00001" })).items[0]!;
    const d = await c.sa.get(vc.id);
    const add = vi.spyOn(c.sa, "addEvent").mockResolvedValue({} as Awaited<ReturnType<typeof c.sa.addEvent>>);
    const replace = vi.spyOn(c.sa, "replaceEvent");
    const user = userEvent.setup();
    render(<WorkEditor sa={c.sa} d={d} reasons={[{code:"nghi_huu",name:"Nghỉ hưu"}]} onClose={vi.fn()} onDone={vi.fn()} />);
    await user.type(screen.getByLabelText("Ngày nghỉ việc"), "1900-01-01");
    await user.selectOptions(screen.getByRole("combobox", { name: /Lý do nghỉ/ }), "nghi_huu");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Ngày nghỉ việc không được trước ngày vào làm.");
    expect(add).not.toHaveBeenCalled(); expect(replace).not.toHaveBeenCalled();
  });

  it.each(["scope", "record"])("route sửa fail-closed khi thiếu quyền %s", async (missing) => {
    const c = await clientFor("kn-admin", "asg-admin"); auth.api = c.api;
    const vc = (await c.sa.list({ q: "VC-00001" })).items[0]!; params.id = vc.id;
    const d = await c.sa.get(vc.id); const a = await c.sa.access();
    const get = vi.fn().mockResolvedValue({ ...d, can: {} });
    const hook = vi.spyOn(StaffScreens, "useStaffApi").mockReturnValue({ ...c.sa, get,
      access: async () => missing === "scope" ? { ...a, perms: {} } : a });
    try {
      render(<HrmWorkInfoEditScreen />);
      expect(await screen.findByRole("alert")).toHaveTextContent(missing === "scope"
        ? "Bạn không có quyền cập nhật thông tin làm việc." : "Bạn không có quyền cập nhật hồ sơ này.");
      expect(screen.queryByRole("button", { name: "Lưu" })).not.toBeInTheDocument();
      if (missing === "scope") expect(get).not.toHaveBeenCalled();
    } finally { hook.mockRestore(); }
  });

});
