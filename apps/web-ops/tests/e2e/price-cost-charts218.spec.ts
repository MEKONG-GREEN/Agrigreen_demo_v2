import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

async function login(page: Page, email: string) {
  await page.getByLabel("Email hoặc số điện thoại").fill(email);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); // Tài khoản giả lập được tài liệu hóa.
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

test("PRICE-COST-CHARTS: đăng nhập TW/Tỉnh, hai biểu đồ cùng lọc, số liệu và quyền xuất", async ({ page }, info) => {
  await page.clock.setFixedTime(new Date("2026-11-08T02:00:00Z"));
  await page.goto("/login");
  await login(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  for (const [role, email, phone] of [["kn_tw", "cost.tw218@vi-du.vn", "0912345218"], ["kn_tinh", "cost.tinh218@vi-du.vn", "0912345219"]]) {
    await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
    await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
    const form = page.getByRole("main");
    await form.getByRole("textbox", { name: "Họ tên", exact: true }).fill(`Kiểm biểu đồ ${role} 218`);
    await form.getByRole("textbox", { name: "Email", exact: true }).fill(email!);
    await form.getByLabel("Số điện thoại (tên đăng nhập)", { exact: true }).fill(phone!);
    await form.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption(role!);
    await form.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
    await form.getByRole("checkbox", { name: "thong_tin_truyen_thong", exact: true }).check();
    await form.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: `Kiểm biểu đồ ${role} 218` })).toBeVisible();
  }
  await logout(page);
  await login(page, "cost.tw218@vi-du.vn");
  await sidebar(page).getByRole("link", { name: "Giá thành sản xuất", exact: true }).click();
  const cost = page.getByRole("img", { name: /Cơ cấu chi phí bình quân/ });
  const prices = page.getByRole("img", { name: /Giá thành – giá bán theo vùng/ });
  await expect(cost).toBeVisible();
  await expect(prices).toBeVisible();
  await expect(cost).toContainText("5.412.667 đồng/phiếu");
  await expect(prices).toContainText("7.100 đồng/kg");
  await expect(prices).toContainText("6.800 đồng/kg");
  await expect(page.getByRole("button", { name: "Xuất Excel", exact: true })).toHaveCount(0);
  await expect(page.getByRole("columnheader", { name: /Nông hộ|Số điện thoại/ })).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("218-charts-desktop.png"), fullPage: true });
  await page.getByLabel("Nhóm cây", { exact: true }).selectOption("lua");
  await page.getByLabel("Mặt hàng", { exact: true }).selectOption("MH00004");
  await page.getByLabel("Nhóm vùng", { exact: true }).selectOption("kien_giang");
  await page.getByLabel("Xã", { exact: true }).selectOption("30544");
  await expect(prices).not.toContainText("An Giang");
  await expect(prices).toContainText("1 phiếu · Mẫu nhỏ");
  await expect(cost).toContainText("5.384.000 đồng/phiếu");
  await expect(page.getByRole("table", { name: /Giá thành theo mặt hàng/ })).toContainText("5.384");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: info.outputPath("218-charts-mobile.png"), fullPage: true });
  await page.getByLabel("Xã", { exact: true }).selectOption("30280");
  await expect(page.getByText("Không có phiếu đã duyệt phù hợp để vẽ biểu đồ.")).toHaveCount(2);
  await expect(cost).toHaveCount(0);
  await expect(prices).toHaveCount(0);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await logout(page);
  await login(page, "cost.tinh218@vi-du.vn");
  await sidebar(page).getByRole("link", { name: "Giá thành sản xuất", exact: true }).click();
  await expect(page.getByRole("button", { name: "Xuất Excel", exact: true })).toBeEnabled();
  await expect(cost).toBeVisible();
  await expect(prices).toBeVisible();
});
