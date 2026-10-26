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
