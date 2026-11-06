// #255 AUTH-V2-RESET-NOPIN (BRD 2.0 FN-CH-00.01 "Quên mật khẩu?"): màn nhập email gọi route công khai không cần đăng
// nhập và báo cùng một câu dù email có tài khoản hay không; màn đặt mật khẩu mới đọc mã từ '#token=', từ chối mật khẩu
// ngắn/không khớp, mã đã dùng → yêu cầu liên kết mới.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import ResetPasswordPage from "@/app/login/dat-lai-mat-khau/page";
import ForgotPasswordPage from "@/app/login/quen-mat-khau/page";

const auth: { api: ApiClient } = {
  api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => null, fetch: mockFetchNoDelay }),
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const TOKEN = "A".repeat(43) + "b".repeat(43);

describe("Quên mật khẩu — tự đặt lại qua email", () => {
  it("không đăng nhập vẫn gửi được yêu cầu; thông báo không lộ email có tồn tại", async () => {
    render(<ForgotPasswordPage />);
    await userEvent.click(screen.getByRole("button", { name: "Gửi liên kết đặt lại" }));
    expect(screen.getByText("Nhập email.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Email"), "khong.ton.tai@kn.example.vn");
    await userEvent.click(screen.getByRole("button", { name: "Gửi liên kết đặt lại" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Nếu email này thuộc một tài khoản đang hoạt động");
    expect(screen.getByRole("status")).toHaveTextContent("30 phút");
  });

  it("email sai cú pháp → báo lỗi, không báo đã gửi", async () => {
    render(<ForgotPasswordPage />);
    await userEvent.type(screen.getByLabelText("Email"), "khong-phai-email");
    await userEvent.click(screen.getByRole("button", { name: "Gửi liên kết đặt lại" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Email không hợp lệ");
    expect(screen.queryByRole("status")).toBeNull();
  });
});

describe("Đặt lại mật khẩu từ liên kết email", () => {
  beforeEach(() => window.history.replaceState(null, "", `/login/dat-lai-mat-khau#token=${TOKEN}`));

  it("mã lấy từ '#', xóa khỏi thanh địa chỉ; mật khẩu ngắn/không khớp bị chặn; đặt xong; dùng lại mã → liên kết mới", async () => {
    const { unmount } = render(<ResetPasswordPage />);
    expect(window.location.hash).toBe("");
    const pw = screen.getByLabelText("Mật khẩu mới");
    const again = screen.getByLabelText("Nhập lại mật khẩu mới");
    await userEvent.type(pw, "ngan");
    await userEvent.type(again, "ngan");
    await userEvent.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("tối thiểu 12 ký tự");
    await userEvent.clear(pw);
    await userEvent.type(pw, "Mat-khau-moi-2026!");
    await userEvent.clear(again);
    await userEvent.type(again, "Mat-khau-khac-2026!");
    await userEvent.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("không khớp");
    await userEvent.clear(again);
    await userEvent.type(again, "Mat-khau-moi-2026!");
    await userEvent.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Mọi phiên đăng nhập cũ đã bị đăng xuất");
    unmount();

    window.history.replaceState(null, "", `/login/dat-lai-mat-khau#token=${TOKEN}`);
    render(<ResetPasswordPage />);
    await userEvent.type(screen.getByLabelText("Mật khẩu mới"), "Mat-khau-khac-2026!");
    await userEvent.type(screen.getByLabelText("Nhập lại mật khẩu mới"), "Mat-khau-khac-2026!");
    await userEvent.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("đã dùng hoặc đã hết hạn");
    expect(screen.getByRole("link", { name: "Yêu cầu liên kết mới" })).toHaveAttribute("href", "/login/quen-mat-khau");
  });

  it("không có mã → báo liên kết không hợp lệ", () => {
    window.history.replaceState(null, "", "/login/dat-lai-mat-khau");
    render(<ResetPasswordPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("không hợp lệ");
  });
});
