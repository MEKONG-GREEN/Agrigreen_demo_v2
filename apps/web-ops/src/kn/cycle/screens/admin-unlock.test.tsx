// ADMIN-UNLOCK-WEB (#61): Chi tiết tài khoản KN (FN-AD-04.04) — tài khoản đang hoạt động (enabled=true) nhưng Keycloak
// khóa do đăng nhập sai: nhãn riêng + giờ tự mở (khóa tạm 15 phút, P0-VPS-OPS), nút chọn MỞ khóa, lý do ≥ 20 ký tự,
// làm mới sau khi mở; lỗi máy chủ không hiện thành công.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import type { AccountsUser } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { UserDetailScreen } from "./AdminScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/quan-tri/nguoi-dung/u-1",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({ id: "u-1" }),
}));
const auth: { knAdminApi: Record<string, ReturnType<typeof vi.fn>> } = { knAdminApi: {} };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: {}, kn: { context: { role: "admin" } }, knAdminApi: auth.knAdminApi }) }));

const UNTIL = "2026-10-26T03:15:00Z";
const base: AccountsUser = {
  id: "u-1",
  username: "0912345601",
  email: "an.nv@angiang.gov.vn",
  first_name: "An",
  last_name: "Nguyễn Văn",
  phone: "0912345601",
  enabled: true,
  email_verified: true,
  org_ref: null,
  roles: ["kn_user"],
  lock_cause: null,
  required_actions: [],
  created_at: "2026-08-20T02:10:00Z",
  tracked: true,
  mfa: { required: true, configured: true, enrollment_pending: false },
};
const reason = "Cán bộ gọi điện xác minh danh tính, mở khóa cho đăng nhập lại";

beforeEach(() => {
  auth.knAdminApi = {
    user: vi.fn().mockResolvedValueOnce({ ...base, login_locked: true, login_locked_until: UNTIL }).mockResolvedValue({ ...base, login_locked: false }),
    assignments: vi.fn().mockResolvedValue({ items: [] }),
    log: vi.fn().mockResolvedValue({ items: [] }),
    internalUnits: vi.fn().mockResolvedValue({ items: [] }),
    delegations: vi.fn().mockResolvedValue({ items: [] }),
    lock: vi.fn().mockResolvedValue({}),
    unlock: vi.fn().mockResolvedValue({}),
  };
});

describe("ADMIN-UNLOCK-WEB — chi tiết tài khoản KN", () => {
  it("enabled=true + login_locked=true → nhãn khóa đăng nhập sai có giờ tự mở; Mở khóa gọi /unlock rồi làm mới", async () => {
    const u = userEvent.setup();
    render(<UserDetailScreen />);
    const badge = await screen.findByTestId("login-lock");
    expect(badge).toHaveTextContent(`Khóa do đăng nhập sai — tự mở lúc ${formatDateTime(UNTIL)}`);
    expect(screen.getByText("Hoạt động")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Khóa tài khoản/ })).toBeNull();
    await u.click(screen.getByRole("button", { name: /Mở khóa/ }));
    const dlg = screen.getByRole("dialog", { name: "Mở khóa tài khoản" });
    await u.type(within(dlg).getByLabelText(/Lý do/), "ngắn");
    await u.click(within(dlg).getByRole("button", { name: "Xác nhận" }));
    expect(within(dlg).getByText("Tối thiểu 20 ký tự.")).toBeInTheDocument();
    expect(auth.knAdminApi.unlock).not.toHaveBeenCalled();
    await u.clear(within(dlg).getByLabelText(/Lý do/));
    await u.type(within(dlg).getByLabelText(/Lý do/), reason);
    await u.click(within(dlg).getByRole("button", { name: "Xác nhận" }));
    await waitFor(() => expect(auth.knAdminApi.unlock).toHaveBeenCalledWith("u-1", reason));
    expect(auth.knAdminApi.lock).not.toHaveBeenCalled();
    expect(await screen.findByRole("status")).toHaveTextContent("Đã mở khóa tài khoản.");
    await waitFor(() => expect(screen.queryByTestId("login-lock")).toBeNull());
    expect(auth.knAdminApi.user).toHaveBeenCalledTimes(2);
    expect(await screen.findByRole("button", { name: /Khóa tài khoản/ })).toBeInTheDocument();
  }, 30_000);

  it("lỗi máy chủ khi mở khóa → hiện lỗi, không báo thành công, giữ pop-up", async () => {
    auth.knAdminApi.unlock = vi.fn().mockRejectedValue(
      new ApiProblem({ type: "https://errors.agrigreen.vn/accounts.dependency_unavailable", title: "Keycloak không phản hồi", status: 503, code: "accounts.dependency_unavailable" }),
    );
    const u = userEvent.setup();
    render(<UserDetailScreen />);
    await screen.findByTestId("login-lock");
    await u.click(screen.getByRole("button", { name: /Mở khóa/ }));
    const dlg = screen.getByRole("dialog", { name: "Mở khóa tài khoản" });
    await u.type(within(dlg).getByLabelText(/Lý do/), reason);
    await u.click(within(dlg).getByRole("button", { name: "Xác nhận" }));
    expect(await screen.findByText("accounts.dependency_unavailable")).toBeInTheDocument();
    expect(screen.queryByText("Đã mở khóa tài khoản.")).toBeNull();
    expect(screen.getByRole("dialog", { name: "Mở khóa tài khoản" })).toBeInTheDocument();
    expect(screen.getByTestId("login-lock")).toBeInTheDocument();
  }, 30_000);

  it("login_locked=false hoặc thiếu field → không có nhãn khóa đăng nhập sai, thao tác là Khóa", async () => {
    auth.knAdminApi.user = vi.fn().mockResolvedValue({ ...base });
    render(<UserDetailScreen />);
    expect(await screen.findByRole("button", { name: /Khóa tài khoản/ })).toBeInTheDocument();
    expect(screen.queryByTestId("login-lock")).toBeNull();
  }, 30_000);
});
