import { expect, test } from "@playwright/test";
import { loginAs, logout, sidebar } from "./helpers";

test("268: production review keeps disconnected status, denies unapproved request and preserves review controls", async ({ page }) => {
  test.setTimeout(180_000);
  await page.clock.setFixedTime(new Date("2026-11-06T06:00:00Z"));
  const external: string[] = [];
  await page.route("**/*", async route => {
    const url = new URL(route.request().url());
    if ((url.protocol === "http:" || url.protocol === "https:") && !["localhost", "127.0.0.1"].includes(url.hostname)) {
      external.push(url.origin); await route.abort(); return;
    }
    await route.continue();
  });
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const form = page.getByRole("main");
  await form.getByRole("textbox", { name: "Họ tên", exact: true }).fill("Kiểm chứng website268");
  await form.getByRole("textbox", { name: "Email", exact: true }).fill("kn268@example.test");
  await form.getByLabel("Số điện thoại (tên đăng nhập)", { exact: true }).fill("0912345268");
  await form.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await form.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await form.getByRole("checkbox", { name: "thong_tin_truyen_thong", exact: true }).check();
  await form.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kiểm chứng website268" })).toBeVisible();
  await logout(page);
  await page.getByLabel("Email hoặc số điện thoại").fill("kn268@example.test");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); // Mock account fixture only.
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(page.getByTestId("account-menu")).toContainText("Thông tin Tuyên truyền");
  await sidebar(page).getByRole("link", { name: "Kiểm duyệt & công bố", exact: true }).click();
  await page.getByLabel("Ngày báo giá", { exact: true }).fill("2026-11-06");
  await expect(page.getByRole("tab", { name: "Phiếu các xã", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Cảnh báo biến động", exact: true })).toBeVisible();
  const website = page.getByLabel("Đăng website", { exact: true });
  await expect(website).toContainText("Website: Chưa kết nối");
  await expect(website.getByRole("button", { name: "Đăng lên website", exact: true })).toBeDisabled();
  await expect(website).not.toContainText("Đã ghi yêu cầu");
  await expect(page.getByRole("button", { name: "Xem trước bảng giá", exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "Cảnh báo biến động", exact: true }).click();
  await expect(website).toContainText("Website: Chưa kết nối");
  await expect(website).not.toContainText("Đã đăng");
  await page.screenshot({ path: process.env.KN268_SCREENSHOT || "test-results/price-website268.png", fullPage: true });
  expect(external).toEqual([]);
});
