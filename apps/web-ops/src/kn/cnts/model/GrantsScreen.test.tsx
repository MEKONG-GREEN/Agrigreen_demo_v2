import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { GrantsScreen } from "./GrantsScreen";

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }) }));
const OFFICER = "0190f1c4-7d3e-7000-8000-00000000a0c1";
const FACILITY = "0190f1c4-7d3e-7000-8000-00000000c501";
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/cap-quyen",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams({ vien_chuc: OFFICER, co_so: FACILITY, xa: "30292" }),
}));

// BF-CN-DUTY (#171): liên kết từ cảnh báo "chưa cấp quyền" của lịch trực KSGM mở sẵn biểu cấp quyền vai KSGM.
it("mở biểu Cấp quyền chọn sẵn viên chức, vai KSGM, xã và cơ sở từ lịch trực", async () => {
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: async (req) => mockFetchNoDelay(req) });
  render(<GrantsScreen />);
  await screen.findByRole("region", { name: "Cấp quyền" });
  expect((screen.getByRole("combobox", { name: "Vai trong module" }) as HTMLSelectElement).value).toBe("ksgm");
  expect(screen.getByRole("checkbox", { name: /Chỉ cơ sở chọn từ cảnh báo lịch trực KSGM/ })).toBeChecked();
});
