import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsDiary } from "@/api/mock/kn-cnts-diary";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import type * as Common from "@/kn/cycle/common";
import { DiaryScreen } from "./DiaryScreen";
import { ReviewScreen } from "./ReviewScreen";
import { GrowthSection } from "./parts";
import type { Cycle } from "./api";

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }) }));
vi.mock("next/navigation", () => ({ usePathname: () => "/kn/cnts/nhat-ky", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
vi.mock("@/kn/cycle/common", async (orig) => ({ ...(await orig<typeof Common>()), useCommuneNames: () => (c: string) => c }));

beforeEach(() => {
  resetCntsModel();
  resetCntsDiary();
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockFetchNoDelay });
});

it("W11.48 Rà soát nhật ký: hàng chờ, chi tiết (vị trí xa điểm), xác thực", async () => {
  const user = userEvent.setup();
  render(<ReviewScreen />);
  const rows = await screen.findAllByRole("button", { name: /\/\d{4}$/ });
  expect(rows.length).toBe(2);
  await user.click(rows[0]!);
  const detail = await screen.findByRole("region", { name: "Chi tiết lần ghi" });
  expect(await within(detail).findByText("Xa điểm")).toBeInTheDocument();
  await user.click(within(detail).getByRole("button", { name: "Xác thực" }));
  expect(await screen.findByText("Đã xác thực lần ghi")).toBeInTheDocument();
});

it("W11.48 Nhật ký sản xuất: chọn hộ → lứa → lần ghi bị chặn NC-07 hiện lỗi", async () => {
  const user = userEvent.setup();
  render(<DiaryScreen />);
  const hh = await screen.findByRole("combobox", { name: "Hộ mô hình" });
  await screen.findByRole("option", { name: /Điểm ấp Bình Hòa/ });
  await user.selectOptions(hh, within(hh).getAllByRole("option")[1]!);
  await user.click(await screen.findByRole("button", { name: "Gà lai Mía" }));
  await user.click(await screen.findByRole("button", { name: "Ghi nhật ký" }));
  const form = await screen.findByLabelText("Ghi nhật ký", { selector: "div" });
  await user.type(within(form).getByLabelText(/Số con sống/), "495");
  await user.type(within(form).getByLabelText(/Số con chết/), "2");
  await user.type(within(form).getByLabelText(/Thức ăn sử dụng/), "12");
  await user.click(within(form).getByRole("button", { name: "Gửi" }));
  expect(await within(form).findByText(/NC-07/)).toBeInTheDocument();
});

it("BF-CN-DIARY #109: đầu sổ, vật tư mục 1, mục 4 nguyên liệu thức ăn, phần 3 họ tên cán bộ, phần 2 sinh trưởng", async () => {
  const user = userEvent.setup();
  render(<DiaryScreen />);
  const hh = await screen.findByRole("combobox", { name: "Hộ mô hình" });
  await screen.findByRole("option", { name: /Điểm ấp Bình Hòa/ });
  await user.selectOptions(hh, within(hh).getAllByRole("option")[1]!);
  await user.click(await screen.findByRole("button", { name: "Gà lai Mía" }));
  const head = await screen.findByLabelText("Đầu sổ");
  expect(within(head).getByText("Thời gian thực hiện")).toBeInTheDocument();
  expect(within(head).getByText("Cán bộ theo dõi điểm")).toBeInTheDocument();
  expect(within(head).getByText("Viên chức xã (giả lập)")).toBeInTheDocument();
  const sup = screen.getByRole("table", { name: "Vật tư nhận" });
  expect(within(sup).getByText("Thức ăn gà con")).toBeInTheDocument();
  expect(within(sup).getByText("Cám hỗn hợp gà con")).toBeInTheDocument();
  const growth = await screen.findByRole("table", { name: "Chỉ tiêu sinh trưởng theo tuần tuổi" });
  expect(within(growth).getByText("Tuần 12")).toBeInTheDocument();
  expect(screen.getByRole("table", { name: "Yêu cầu kỹ thuật cần đạt" })).toHaveTextContent("Tỷ lệ sống của gà 4 tuần tuổi");
  // Lần ghi có mục 3, mục 4 và phần 3.
  const day = new Date(Date.now() - 3 * 86_400_000).toLocaleDateString("sv-SE").split("-").reverse().join("/");
  await user.click(await screen.findByRole("button", { name: day }));
  const mat = await screen.findByRole("table", { name: "Mua nguyên liệu thức ăn chăn nuôi (NC-09)" });
  expect(within(mat).getByText("Tấm gạo")).toBeInTheDocument();
  expect(within(mat).getByText("90.000 đ")).toBeInTheDocument();
  expect(screen.getByRole("table", { name: "Ý kiến cán bộ theo dõi (NC-14)" })).toHaveTextContent("Viên chức xã (giả lập)");
  // Biểu ghi có mục 4 và cột họ tên cán bộ.
  await user.click(screen.getByRole("button", { name: "Ghi nhật ký" }));
  const form = await screen.findByLabelText("Ghi nhật ký", { selector: "div" });
  expect(within(form).getByText("Mua nguyên liệu thức ăn chăn nuôi (NC-09)")).toBeInTheDocument();
});

it("BF-CN-DIARY #109 (Codex P2): tuần tuổi bất thường không sinh dòng, hiện ghi chú", () => {
  const c = { form: "BM-08", received_date: "2027-01-01", age_days: 1, initial_count: 100, initial_weight_g: "40" } as unknown as Cycle;
  const e = (d: string, week: string) => ({ entry_date: d, status: "submitted" as const, metrics: { alive: "99", dead: "0", feed_kg: "1", age_week: week } });
  render(<GrowthSection c={c} entries={[e("2027-01-02", "1"), e("2027-01-03", "100000000")]} />);
  const growth = screen.getByRole("table", { name: "Chỉ tiêu sinh trưởng theo tuần tuổi" });
  expect(within(growth).getAllByText(/^Tuần \d+$/)).toHaveLength(8);
  expect(screen.getByRole("note")).toHaveTextContent("Dữ liệu tuần tuổi bất thường: 1 lần ghi");
  expect(screen.getByRole("note")).toHaveTextContent("ngày 03/01/2027 — tuần 100000000");
});
