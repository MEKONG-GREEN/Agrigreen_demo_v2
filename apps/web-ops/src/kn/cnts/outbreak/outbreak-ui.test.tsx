// BF-CN-OUTBREAK (PRD UP-07, UP-08): màn Báo cáo dịch bệnh — 4 thẻ thống kê, tìm kiếm + lọc, bảng Mã/Bệnh/Xã/Tiếp nhận/
// Quy mô đàn/Số chết/Nguồn tin/Trạng thái có trạng thái rỗng và phân trang; trang tạo ổ dịch 3 phần, ô số mặc định 0,
// gửi xong có Xuất Word và Xem trên bản đồ.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsOutbreaks } from "@/api/mock/kn-cnts-outbreak";
import { createKnAdminApi } from "@/kn/cycle/api";
import { OutbreakCreateScreen } from "./OutbreakCreateScreen";
import { OutbreakScreen } from "./OutbreakScreen";
import { createOutbreakApi, emptyForm, outbreakCode, payload, searchOutbreaks, statCards, type Outbreak } from "./api";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/dich-benh",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const calls: string[] = [];
const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_xa" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "chan_nuoi_thuy_san" } } }),
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
  resetCntsOutbreaks();
  const adminCtx = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
  const ctx = await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san");
  auth.role = "kn_xa";
  auth.api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.kn-an",
    getContextId: () => ctx,
    fetch: async (req: Request) => {
      calls.push(`${req.method} ${new URL(req.url).pathname}`);
      return mockFetchNoDelay(req);
    },
  });
});

const report = (patch = {}) =>
  payload({ ...emptyForm("91301"), disease_code: "ASF", species_code: "HEO", received_at: "2026-10-12T08:30", source: "can_bo", locality: "Ấp Hòa Thuận A",
    lat: "10.25341", lon: "105.9721", herd_size: "120", dead: "5", symptoms: "Sốt cao", ...patch });

describe("hàm thuần UP-07", () => {
  it("thẻ thống kê, mã ổ dịch, tìm kiếm không dấu", () => {
    expect(statCards([{ key: "reported", count: 2 }, { key: "monitoring", count: 3 }, { key: "closed", count: 1 }]).map((c) => [c.label, c.count])).toEqual([
      ["Tổng ổ dịch", 6], ["Chờ xác nhận", 2], ["Đang theo dõi", 3], ["Đã kết thúc", 1],
    ]);
    expect(outbreakCode("01a17d59-5e74-72cb-9890-329ca8e2e399")).toBe("OD-E2E399");
    const o = { ref: "01a17d59-5e74-72cb-9890-329ca8e2e399", disease_name: "Dịch tả heo Châu Phi", species_name: "Heo", commune_code: "91301", locality: "Ấp Hòa Thuận" } as Outbreak;
    expect(searchOutbreaks([o], "dich ta", () => "Mỹ Hòa Hưng")).toHaveLength(1);
    expect(searchOutbreaks([o], "my hoa", () => "Mỹ Hòa Hưng")).toHaveLength(1);
    expect(searchOutbreaks([o], "od-e2e", () => "")).toHaveLength(1);
    expect(searchOutbreaks([o], "cúm", () => "")).toHaveLength(0);
  });
});

describe("Danh sách ổ dịch (UP-07)", () => {
  it("trạng thái rỗng, 4 thẻ thống kê, cột đúng, lọc theo thẻ, phân trang", async () => {
    const u = userEvent.setup();
    const { unmount } = render(<OutbreakScreen />);
    const empty = await screen.findByRole("table", { name: "Danh sách ổ dịch" });
    expect(await within(empty).findByText(/Chưa có ổ dịch nào/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Lập báo cáo dịch bệnh" })).toHaveAttribute("href", "/kn/cnts/dich-benh/tao-moi");
    unmount();

    const oapi = createOutbreakApi(auth.api!);
    const refs: string[] = [];
    for (let i = 0; i < 12; i++) refs.push((await oapi.report(report({ locality: `Ấp ${i}` }))).ref);
    render(<OutbreakScreen />);
    const stats = await screen.findByRole("group", { name: "Thống kê ổ dịch" });
    await waitFor(() => expect(within(stats).getByRole("button", { name: /Tổng ổ dịch\s*12/ })).toBeInTheDocument());
    for (const l of ["Chờ xác nhận", "Đang theo dõi", "Đã kết thúc"]) expect(within(stats).getByRole("button", { name: new RegExp(l) })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Danh sách ổ dịch" });
    for (const h of ["Mã", "Bệnh", "Xã", "Tiếp nhận", "Quy mô đàn", "Số chết", "Nguồn tin", "Trạng thái"])
      expect(within(table).getByRole("columnheader", { name: h })).toBeInTheDocument();
    expect(within(table).getAllByRole("row")).toHaveLength(11); // tiêu đề + 10 dòng trang 1
    expect(screen.getByText(/1–10/)).toBeInTheDocument();
    await u.click(within(stats).getByRole("button", { name: /Đã kết thúc/ }));
    expect(await within(screen.getByRole("table", { name: "Danh sách ổ dịch" })).findByText("Không có ổ dịch phù hợp bộ lọc.")).toBeInTheDocument();
    await u.click(within(stats).getByRole("button", { name: /Tổng ổ dịch/ }));
    await u.type(screen.getByRole("searchbox"), outbreakCode(refs[3]!));
    await waitFor(() => expect(within(screen.getByRole("table", { name: "Danh sách ổ dịch" })).getAllByRole("row")).toHaveLength(2));
  });
});

describe("Trang tạo ổ dịch (UP-08)", () => {
  it("3 phần, ô số mặc định 0, thiếu * → lỗi, gửi xong có Xuất Word + bản đồ", async () => {
    const u = userEvent.setup();
    render(<OutbreakCreateScreen />);
    for (const l of ["Phần 1. Tình hình dịch", "Phần 2. Xét nghiệm, xử lý, khoanh vùng", "Phần 3. Phòng chống, kiến nghị"]) expect(screen.getByText(l)).toBeInTheDocument();
    for (const l of [/DB-07 Số con chết/, /DB-10 Tiêu hủy \(con/, /Tiêu hủy \(kg\)/, /Hóa chất \(lít\)/]) expect(screen.getByLabelText(l)).toHaveValue("0");
    expect(screen.getByRole("button", { name: "Lấy GPS" })).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Gửi Cấp Tỉnh" }));
    expect(screen.getAllByRole("alert").length).toBeGreaterThan(3);
    expect(calls.filter((c) => c.startsWith("POST") && c.includes("/outbreaks"))).toHaveLength(0);

    await waitFor(() => expect(screen.getByRole("option", { name: /Dịch tả heo/ })).toBeInTheDocument());
    await u.selectOptions(screen.getByLabelText("Bệnh"), "ASF");
    await u.selectOptions(screen.getByLabelText("Vật nuôi"), "HEO");
    await u.type(screen.getByLabelText(/DB-02/), "2026-10-12T08:30");
    await u.selectOptions(screen.getByLabelText("Nguồn tin"), "can_bo");
    await u.type(screen.getByLabelText(/DB-04/), "Ấp Hòa Thuận A");
    await u.type(screen.getByLabelText("Vĩ độ"), "10.25341");
    await u.type(screen.getByLabelText("Kinh độ"), "105.9721");
    await u.clear(screen.getByLabelText(/DB-06 Quy mô đàn/));
    await u.type(screen.getByLabelText(/DB-06 Quy mô đàn/), "120");
    await u.type(screen.getByLabelText(/DB-08 Triệu chứng/), "Sốt cao, bỏ ăn");
    await u.click(screen.getByRole("button", { name: "Gửi Cấp Tỉnh" }));
    expect(await screen.findByText(/cảnh báo/)).toHaveTextContent("Khẩn");
    expect(screen.getByRole("link", { name: "Xem trên bản đồ" })).toHaveAttribute("href", "/kn/cnts/ban-do-dich");
    // jsdom: Blob của jsdom khác Blob của Node — giả lập createObjectURL (trình duyệt thật có sẵn).
    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
    await u.click(screen.getByRole("button", { name: "Xuất Word (BM-10)" }));
    expect(await screen.findByText(/Đã xuất Word BM-10 — số 1\/BC-TTKN/)).toBeInTheDocument();
  });
});
