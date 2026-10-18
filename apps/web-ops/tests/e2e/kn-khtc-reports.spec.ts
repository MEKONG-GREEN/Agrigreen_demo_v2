import { expect, test } from "@playwright/test";
import { sidebar, logout } from "./helpers";

// KH-REPORT-UI-001: ready menu routes, actual browser rendering and Admin read-only rights.
test("W11.66 Admin xem báo cáo KHTC và nguồn với quyền chỉ đọc", async ({ page }) => {
  async function login() {
    await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
    await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
    await expect(sidebar(page)).toBeVisible();
  }
  await page.goto("/login");
  await login();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login();
  await sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Báo cáo KHTC", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Soạn báo cáo KHTC", exact: true })).toBeVisible();
  await page.getByRole("button", { name: /^Tuần / }).first().click();
  await expect(page.getByLabel("Người ký", { exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Lưu bản soạn", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Cập nhật số liệu", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Xuất Excel", exact: true })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Số liệu tổng hợp", exact: true })).toBeVisible();
  await page.getByText("Số liệu nguồn và phiếu đã gửi", { exact: true }).click();
  await expect(page.getByText(/Ảnh chụp nguồn/)).toBeVisible();
});
