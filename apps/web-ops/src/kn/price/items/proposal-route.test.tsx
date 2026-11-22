import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetPriceItems } from "@/api/mock/kn-price-items";
import { ItemsScreen } from "./ItemsScreen";

const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_tinh" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role } } }),
}));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/de-xuat-mat-hang",
  useSearchParams: () => new URLSearchParams(),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

async function asRole(role: "kn_tinh" | "kn_xa") {
  const persona = role === "kn_xa" ? "kn-an" : "kn-binh";
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
    method: "POST",
    headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `proposal-route-${role}` },
    body: JSON.stringify({ assignment_id: role === "kn_xa" ? "asg-an-xa" : "asg-binh-tinh", department_code: "trong_trot" }),
  }));
  const context = await response.json() as { context_id: string };
  auth.role = role;
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => context.context_id, fetch: mockFetchNoDelay });
}

const journeyNow = new Date();
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(journeyNow);
  resetPriceItems();
});
afterEach(() => vi.useRealTimers());

it("route Đề xuất mở ngay danh sách cho Cấp Tỉnh, vẫn dùng màn và quyền hiện hành", async () => {
  await asRole("kn_tinh");
  render(<ItemsScreen initialTab="proposals" />);
  const table = await screen.findByRole("table", { name: "Đề xuất mặt hàng từ xã" });
  expect(within(table).getByText("Vùng trồng mới, chợ xã có bán hằng ngày")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: /Đề xuất từ xã/ })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Đề xuất mặt hàng");
  expect(screen.queryByRole("table", { name: "Danh mục mặt hàng" })).not.toBeInTheDocument();
});

it("route danh mục cũ vẫn mở tab Mặt hàng cho Cấp Tỉnh", async () => {
  await asRole("kn_tinh");
  render(<ItemsScreen />);
  expect(await screen.findByRole("table", { name: "Danh mục mặt hàng" })).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Mặt hàng" })).toHaveAttribute("aria-selected", "true");
});

it("route riêng giữ Cấp Xã chỉ thấy Đề xuất, không có quyền duyệt", async () => {
  await asRole("kn_xa");
  render(<ItemsScreen initialTab="proposals" />);
  expect(await screen.findByRole("table", { name: "Đề xuất mặt hàng từ xã" })).toBeInTheDocument();
  expect(screen.getAllByRole("tab")).toHaveLength(1);
  expect(screen.queryByRole("button", { name: "Duyệt" })).not.toBeInTheDocument();
  expect(screen.queryByText("Vùng trồng mới, chợ xã có bán hằng ngày")).not.toBeInTheDocument();
});
