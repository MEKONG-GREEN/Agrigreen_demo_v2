import { expect, test, type Page } from "@playwright/test";
import { accountMenu, logout, sidebar } from "./helpers";

async function login(page: Page, user: string) {
  await page.getByLabel("Email hoặc số điện thoại").fill(user);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
}

test("multipart183: tệp PDF qua trình duyệt, sai chữ ký không ghi, tệp đúng vào kho HCTH", async ({ page }) => {
  // Phòng mặc định chưa mở: bật đúng phòng rồi chọn assignment HCTH qua UI.
  await page.goto("/login");
  await login(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login(page, "hung.tq@angiang.gov.vn");
  await expect(accountMenu(page)).toContainText("Trần Quốc Hưng");
  await sidebar(page).getByRole("link", { name: "Hồ sơ dữ liệu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Hồ sơ dữ liệu" })).toBeVisible();
  await page.getByRole("button", { name: /Tài liệu nhân sự khác/ }).click();
  await page.getByRole("button", { name: "Tải lên", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: /^Tải lên/ });
  await dialog.getByLabel("Tệp", { exact: true }).setInputFiles({ name: "183.pdf", mimeType: "application/pdf", buffer: Buffer.from("không phải PDF") });
  await dialog.getByLabel("Tên tài liệu", { exact: true }).fill("Multipart183 kiểm thử PDF");
  await dialog.getByRole("button", { name: "Tải lên", exact: true }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("Tệp", { exact: true }).setInputFiles({ name: "183.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.7\nMultipart183") });
  await dialog.getByRole("button", { name: "Tải lên", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText("Đã tải lên tài liệu.", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Multipart183 kiểm thử PDF", exact: true })).toHaveCount(1);
});
