import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

test("S6 FN-18: bản đồ giữ lớp CNTS, gỡ lớp thửa HTX", async ({ page }, testInfo) => {
  const login = async (email: string) => {
    await page.getByLabel("Email hoặc số điện thoại").fill(email);
    await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  };
  await page.goto("/login");
  await login("admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login("cnts.thu@demo.agrigreen.test");
  await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
  await page.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Vào hệ thống/ }).click();
  const link = sidebar(page).getByRole("link", { name: "Bản đồ vùng dịch", exact: true });
  await expect(link).toBeVisible();
  await link.click();
  await expect(page.getByRole("heading", { level: 1, name: "Bản đồ vùng nuôi, vùng dịch", exact: true })).toBeVisible();
  for (const name of ["Vùng", "Ổ dịch", "Điểm trình diễn", "Hộ mô hình", "Trang trại"]) {
    await expect(page.getByRole("checkbox", { name, exact: true })).toBeChecked();
  }
  await expect(page.getByText(/Thửa HTX|dữ liệu công bố của HTX/)).toHaveCount(0);
  await expect(page.getByRole("columnheader", { name: "Số thửa", exact: true })).toHaveCount(0);
  const farms = page.getByRole("checkbox", { name: "Trang trại", exact: true });
  await farms.uncheck();
  await expect(farms).not.toBeChecked();
  await farms.check();
  const day = page.getByLabel("Ngày xem bản đồ", { exact: true });
  const today = await day.inputValue();
  const previous = new Date(today+"T00:00:00Z");
  previous.setUTCDate(previous.getUTCDate()-1);
  await day.fill(previous.toISOString().slice(0, 10));
  await expect(day).toHaveValue(previous.toISOString().slice(0, 10));
  await expect(page.getByRole("heading", { level: 3, name: "Vùng ngày "+previous.toISOString().slice(0, 10).split("-").reverse().join("/"), exact: true })).toBeVisible();
  await expect(page.getByRole("checkbox", { name: "Trang trại", exact: true })).toBeChecked();
  await expect(page.getByText(/Thửa HTX|dữ liệu công bố của HTX/)).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("cnts-map-standalone.png"), fullPage: true });
});
