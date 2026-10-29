import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

async function login(page: Page) {
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

test("BF-SWEEP-KHTC: Danh mục có route riêng, lỗi từng trường, Log và vòng đời ngừng dùng", async ({ page }) => {
  await page.goto("/login");
  await login(page);
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  // Đăng nhập trong cùng tab để lấy menu mới, giữ dữ liệu mock của tab.
  await logout(page); await login(page);
  await sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Danh mục KHTC", exact: true }).click();
  const table = page.getByRole("table", { name: "Đơn vị tính", exact: true });
  await expect(table.getByRole("columnheader", { name: "STT", exact: true })).toBeVisible();
  await expect(page.getByRole("tab")).toHaveCount(7);
  await page.getByRole("link", { name: "Thêm", exact: true }).click();
  await expect(page).toHaveURL(/\/unit\/them-moi$/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const code = page.getByRole("textbox", { name: "Mã", exact: true });
  const name = page.getByRole("textbox", { name: "Tên", exact: true });
  const sort = page.getByRole("textbox", { name: "Thứ tự", exact: true });
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(code).toHaveAttribute("aria-invalid", "true");
  await expect(name).toHaveAttribute("aria-invalid", "true");
  await code.fill("them-moi"); await name.fill("Đơn vị kiểm thử 164");
  await sort.fill("1e100000");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(sort).toHaveAttribute("aria-invalid", "true");
  await expect(page).toHaveURL(/\/unit\/them-moi$/);
  await sort.fill("12");
  await page.getByRole("textbox", { name: "Ký hiệu", exact: true }).fill("đv");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  // Mã hợp lệ trùng tên route không bị nhầm sang trang Thêm.
  await expect(page).toHaveURL(/\/unit\/muc\/them-moi$/);
  await expect(page.getByRole("heading", { level: 1, name: /^Đơn vị kiểm thử 164/ })).toBeVisible();
  const log = page.getByRole("complementary", { name: "Log", exact: true });
  await expect(log).toContainText("Thêm mới · v1");
  await page.getByRole("link", { name: "Sửa", exact: true }).click();
  await expect(page).toHaveURL(/\/unit\/muc\/them-moi\/sua$/);
  await expect(code).toBeDisabled();
  await name.fill("Đơn vị kiểm thử 164 đã sửa");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/unit\/muc\/them-moi$/);
  await expect(log).toContainText("Đơn vị kiểm thử 164 → Đơn vị kiểm thử 164 đã sửa");
  await page.getByRole("button", { name: "Ngừng dùng", exact: true }).click();
  await expect(page.getByRole("button", { name: "Dùng lại", exact: true })).toBeVisible();
  await expect(log).toContainText("Ngừng dùng · v3");
  await page.getByRole("link", { name: "Danh sách danh mục", exact: true }).click();
  await expect(table.getByText("Đơn vị kiểm thử 164 đã sửa", { exact: true })).toHaveCount(0);
  await page.getByLabel("Hiện cả mục đã ngừng dùng", { exact: true }).check();
  const row = table.getByRole("row").filter({ hasText: "Đơn vị kiểm thử 164 đã sửa" });
  await expect(row).toContainText("Ngừng dùng");
  await row.getByRole("link", { name: "Xem Đơn vị kiểm thử 164 đã sửa", exact: true }).click();
  await expect(page).toHaveURL(/\/unit\/muc\/them-moi\?retired=1$/);
  await page.getByRole("button", { name: "Dùng lại", exact: true }).click();
  await expect(log).toContainText("Dùng lại · v4");
  await page.getByRole("link", { name: "Danh sách danh mục", exact: true }).click();
  await expect(page.getByRole("tab", { name: /Đơn vị tính/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByLabel("Hiện cả mục đã ngừng dùng", { exact: true })).toBeChecked();

  await page.getByRole("tab", { name: /Người ký/ }).click();
  await page.getByRole("link", { name: "Thêm", exact: true }).click();
  await code.fill("pgd-164");
  await page.getByRole("textbox", { name: "Họ tên", exact: true }).fill("Nguyễn Thử");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  const title = page.getByRole("textbox", { name: "Chức vụ", exact: true });
  await expect(title).toHaveAttribute("aria-invalid", "true");
  await title.fill("Phó Giám đốc");
  await page.getByRole("checkbox", { name: "Mẫu 1 (tuần)", exact: true }).check();
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByText("extension.khtc_default_conflict", { exact: true })).toBeVisible();
  await page.getByRole("checkbox", { name: "Mẫu 1 (tuần)", exact: true }).uncheck();
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/signer\/muc\/pgd-164\?retired=1$/);
  await expect(page.getByRole("heading", { name: /^Nguyễn Thử/, level: 1 })).toBeVisible();
});
