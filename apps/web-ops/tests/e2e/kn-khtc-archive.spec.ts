import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

// Mock-only credentials, identical to the existing KN login/browser fixtures.
test("W11.69 kho báo cáo KHTC chỉ xem, lọc và không mở bản soạn", async ({ page }) => {
  test.setTimeout(180_000);
  await page.goto("/login");
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
  // Prepare an actual TW/KHTC assignment through Admin UI; do not bypass the route guard.
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const main = page.getByRole("main");
  await expect(main.getByRole("button", { name: "Lưu", exact: true })).toBeEnabled();
  await main.getByLabel("Họ tên").fill("Kiểm thử kho KHTC");
  await main.getByLabel("Email").fill("khtc.archive@example.test");
  await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345688");
  const assignment = main.getByRole("group", { name: "Vai trò và phạm vi" });
  await assignment.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tw");
  await assignment.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await assignment.getByRole("checkbox", { name: "ke_hoach_tai_chinh", exact: true }).check();
  await main.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kiểm thử kho KHTC" })).toBeVisible();
  await logout(page);
  await page.getByLabel("Email hoặc số điện thoại").fill("0912345688");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
  await expect(page).toHaveURL(/\/kn\/khtc\/bao-cao-da-xuat$/);
  await sidebar(page).getByRole("link", { name: "Báo cáo Tỉnh (KHTC)", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Báo cáo KHTC đã xuất" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Báo cáo đã xuất" })).toBeVisible();
  await page.getByLabel("Năm", { exact: true }).selectOption("2026");
  await page.getByLabel("Loại báo cáo", { exact: true }).selectOption("week");
  await page.getByLabel("Tên kỳ hoặc số văn bản").fill("12/BC");
  await page.getByRole("button", { name: "Tìm kiếm", exact: true }).click();
  await page.getByLabel("Mọi phiên bản").check();
  await expect(page.getByRole("button", { name: "Lưu bản soạn" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Xuất Excel", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Tiếp", exact: true })).toBeDisabled();
  await expect(page.getByRole("main").getByRole("alert")).toHaveCount(0);
});