import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

test("KN-STANDALONE-7: retired KHTC URLs return actual404", async ({ page, request }) => {
  for (const path of ["/kn/khtc/so-lieu-ngoai", "/kn/khtc/mau-bao-cao"]) {
    const direct = await request.get(path);
    expect(direct.status()).toBe(404);
    const navigation = await page.goto(path);
    expect(navigation?.status()).toBe(404);
    await expect(page.getByText("Không tìm thấy trang", { exact: true })).toBeVisible();
  }
});

test("KN-STANDALONE-7: Admin templates and KN report editor remain accessible without external cells", async ({ page }) => {
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
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
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  await logout(page);
  await login();
  await sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true }).click();
  await expect(sidebar(page).getByRole("link", { name: /^Mẫu (báo cáo|phiếu số liệu KHTC)$/ })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: "Mục lục mẫu báo cáo", exact: true })).toBeVisible();
  await expect(sidebar(page).getByText("Số liệu nguồn ngoài", { exact: true })).toHaveCount(0);
  await sidebar(page).getByRole("link", { name: "Báo cáo KHTC", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Soạn báo cáo KHTC", exact: true })).toBeVisible();
  await page.getByRole("button", { name: /^Tuần / }).first().click();
  await expect(page.getByRole("heading", { name: "Số liệu tổng hợp", exact: true })).toBeVisible();
  await expect(page.getByText(/Số liệu nguồn ngoài|Nhập ngoài/)).toHaveCount(0);
  await expect(page.getByLabel("Người ký", { exact: true })).toBeDisabled();
  await page.screenshot({ path: test.info().outputPath("khtc267-desktop.png"), animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("heading", { name: "Số liệu tổng hợp", exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("khtc267-mobile.png"), animations: "disabled" });
});
