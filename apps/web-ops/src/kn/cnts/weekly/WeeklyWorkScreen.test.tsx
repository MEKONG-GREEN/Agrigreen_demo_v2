// BF-CN-WEEKLY (PRD UP-06): màn Báo cáo công việc tuần — hai phần, bảng T2–CN, thiếu trường báo lỗi theo ô và KHÔNG gửi;
// Phần II có "Sao chép tuần trước", Phần I có "Gợi ý điền"; cột phải có Người báo cáo và Log.
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi } from "../periods/api";
import { WeeklyWorkScreen } from "./WeeklyWorkScreen";
import { PART_I, PART_II } from "./weekly";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/cong-viec-tuan",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const calls: string[] = [];
const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: auth.api,
    principal: { subject: "x", displayName: "Nguyễn Văn An" },
    kn: { context: { role: "kn_xa", communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "chan_nuoi_thuy_san", department_name: "Chăn nuôi – Thủy sản", org_name: "Xã Mỹ Hòa Hưng" } },
  }),
}));

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

beforeEach(async () => {
  calls.length = 0;
  resetKnAdmin();
  resetCntsPeriods();
  const adminCtx = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
  await createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockFetchNoDelay })).install();
  const tinh = createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => null, fetch: mockFetchNoDelay }));
  await tinh.publish({ code: PART_I, from_date: "2099-01-05", to_date: "2099-01-05", units: ["91301"] });
  await tinh.publish({ code: PART_II, from_date: "2099-01-12", to_date: "2099-01-12", units: ["91301"] });
  const xaCtx = await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san");
  auth.api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.kn-an",
    getContextId: () => xaCtx,
    fetch: async (req: Request) => {
      calls.push(`${req.method} ${new URL(req.url).pathname}`);
      return mockFetchNoDelay(req);
    },
  });
});

const writes = () => calls.filter((c) => !c.startsWith("GET "));

describe("Báo cáo công việc tuần (UP-06)", () => {
  it("Phần I: bảng T2–CN, Gợi ý điền; thiếu ô bắt buộc → lỗi theo ô, không gửi; cột phải Người báo cáo + Log", async () => {
    const u = userEvent.setup();
    render(<WeeklyWorkScreen />);
    const table = await screen.findByRole("table", { name: "Bảng công việc theo ngày" });
    expect(within(table).getAllByRole("row")).toHaveLength(8); // tiêu đề + 7 ngày
    expect(within(table).getByRole("rowheader", { name: /Chủ Nhật/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Gợi ý điền/ })).toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Người báo cáo" })).toHaveTextContent("Nguyễn Văn An");
    expect(screen.getByRole("complementary", { name: "Log" })).toHaveTextContent("Chưa gửi lần nào.");
    await u.click(screen.getByRole("button", { name: "Gửi báo cáo" }));
    expect(screen.getAllByText("Chọn loại công việc.")).toHaveLength(5);
    expect(writes()).toHaveLength(0);
  });

  it("Phần II: Sao chép tuần trước, đi công tác bắt buộc Địa điểm", async () => {
    const u = userEvent.setup();
    render(<WeeklyWorkScreen />);
    await u.click(await screen.findByRole("tab", { name: /Phần II/ }));
    expect(await screen.findByRole("button", { name: "Sao chép tuần trước" })).toBeInTheDocument();
    await u.selectOptions(screen.getByLabelText("Thứ Hai — Loại công việc"), "Công tác khác");
    await u.click(screen.getByRole("button", { name: "Đăng ký kế hoạch" }));
    expect(screen.getByText("Đi công tác: bắt buộc Địa điểm.")).toBeInTheDocument();
    expect(writes()).toHaveLength(0);
  });
});
