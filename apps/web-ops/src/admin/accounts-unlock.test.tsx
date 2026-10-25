// ADMIN-UNLOCK-WEB (#61): màn Tài khoản cán bộ (/quan-tri/tai-khoan) qua client sinh từ OpenAPI và backend giả lập
// theo hợp đồng máy chủ. Tài khoản enabled=true nhưng Keycloak khóa do đăng nhập sai → hiện nhãn riêng + giờ tự mở,
// form chọn MỞ khóa (lý do ≥ 20 ký tự), làm mới sau khi mở; lỗi máy chủ không hiện thành công; chưa qua MFA → không thấy gì.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, type AdminApiClient } from "@/api/client";
import { ACCOUNTS, lockAccountByBruteForce, resetAccountLoginLocks } from "@/api/mock/admin-ops";
import { mockAdminFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import AccountsPage from "@/app/quan-tri/tai-khoan/page";
import { fullName } from "./accounts";

const auth: { adminApi: AdminApiClient | null; principal: { mfa: boolean } | null } = { adminApi: null, principal: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const as = (persona: string, mfa = true) => {
  auth.adminApi = createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
  auth.principal = { mfa };
};
const reason = "Cán bộ gọi điện xác minh danh tính, mở khóa cho đăng nhập lại";
const target = () => ACCOUNTS.find((a) => a.user.enabled && a.user.required_actions.length === 0)!.user;

async function openPanel() {
  const u = target();
  render(<AccountsPage />);
  await userEvent.click(await screen.findByRole("button", { name: `Chi tiết ${fullName(u) || u.username}` }));
  return screen.findByTestId("user-panel");
}

beforeEach(() => resetAccountLoginLocks());
afterEach(() => resetAccountLoginLocks());

describe("ADMIN-UNLOCK-WEB — khóa do đăng nhập sai trên màn Tài khoản cán bộ", () => {
  it("enabled=true + login_locked=true: nhãn riêng có giờ tự mở, gọi /unlock, làm mới về Đang hoạt động", async () => {
    as("setter");
    lockAccountByBruteForce(target().id);
    const panel = await openPanel();
    const badge = await within(panel).findByTestId("login-lock");
    expect(badge).toHaveTextContent(/Khóa do đăng nhập sai — tự mở lúc/);
    // Trạng thái kích hoạt giữ nguyên nghĩa: tài khoản vẫn "Đang hoạt động".
    expect(within(panel).getByText("Đang hoạt động")).toBeInTheDocument();
    const form = within(panel).getByTestId("lock-form");
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText(/Lý do mở khóa/), "ngắn");
    await u.click(within(form).getByRole("button", { name: "Mở khóa tài khoản" }));
    expect(within(form).getByRole("alert")).toHaveTextContent(/còn thiếu/);
    await u.clear(within(form).getByLabelText(/Lý do mở khóa/));
    await u.type(within(form).getByLabelText(/Lý do mở khóa/), reason);
    await u.click(within(form).getByRole("button", { name: "Mở khóa tài khoản" }));
    await waitFor(() => expect(within(panel).queryByTestId("login-lock")).toBeNull());
    expect(await within(panel).findByRole("button", { name: "Khóa tài khoản" })).toBeInTheDocument();
    expect(await within(panel).findByText("Mở khóa")).toBeInTheDocument(); // nhật ký thao tác
  }, 30_000);

  it("danh sách không tự suy trạng thái khóa đăng nhập (không có field ở danh sách)", async () => {
    as("setter");
    lockAccountByBruteForce(target().id);
    render(<AccountsPage />);
    const table = await screen.findByRole("table", { name: "Tài khoản trong phạm vi" });
    await within(table).findAllByRole("button", { name: /^Chi tiết / });
    expect(within(table).queryByText(/đăng nhập sai/)).toBeNull();
  }, 30_000);

  it("khóa đã tự hết trước khi bấm: máy chủ trả 409 → hiện lỗi, không báo thành công, giữ lý do", async () => {
    as("setter");
    lockAccountByBruteForce(target().id);
    const panel = await openPanel();
    await within(panel).findByTestId("login-lock");
    lockAccountByBruteForce(target().id, Date.now() - 1); // khóa tạm 15 phút đã hết
    const form = within(panel).getByTestId("lock-form");
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText(/Lý do mở khóa/), reason);
    await u.click(within(form).getByRole("button", { name: "Mở khóa tài khoản" }));
    expect(await within(form).findByText(/accounts\.state_conflict/)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Lý do mở khóa/)).toHaveValue(reason);
  }, 30_000);

  it("quản trị chưa qua MFA: máy chủ từ chối, không thấy trạng thái khóa hay nút mở khóa", async () => {
    as("admin-no-mfa", false);
    lockAccountByBruteForce(target().id);
    render(<AccountsPage />);
    expect(await screen.findByTestId("mfa-notice")).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText(/mfa_required/).length).toBeGreaterThan(0));
    expect(screen.queryByRole("button", { name: /^Chi tiết / })).toBeNull();
    expect(screen.queryByTestId("login-lock")).toBeNull();
    expect(screen.queryByRole("button", { name: "Mở khóa tài khoản" })).toBeNull();
  }, 30_000);
});
