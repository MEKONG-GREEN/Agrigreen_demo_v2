import { expect, test, type Page } from "@playwright/test";
import { loginAs, sidebar } from "./helpers";

async function openItems(page: Page) {
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  // Refresh the Admin menu through the existing supported session flow, without
  // injecting roles/context or altering the shared login helper.
  await sidebar(page).getByRole("link", { name: "Menu các cấp", exact: true }).click();
  await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
  await page.getByLabel(/^Lý do hỗ trợ/).fill("Kiểm tra danh mục mặt hàng và nhóm hàng198");
  await page.getByRole("button", { name: "Vào menu Cấp Tỉnh", exact: true }).click();
  await page.getByRole("button", { name: "Thoát chế độ hỗ trợ", exact: true }).click();
  await sidebar(page).getByRole("button", { name: "Phòng Thông tin Tuyên truyền", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Mặt hàng", exact: true }).click();
  await expect(page).toHaveURL(/\/kn\/bao-gia\/mat-hang$/);
}

test("price-items198: list → create → revise → real Log, group routes and cancel", async ({ page }) => {
  test.setTimeout(180_000);
  const now = new Date();
  await page.clock.setFixedTime(now);
  await openItems(page);
  await expect(page.getByRole("table", { name: "Danh mục mặt hàng" }).getByRole("columnheader", { name: "STT" })).toBeVisible();
  await page.getByRole("link", { name: "Thêm mặt hàng", exact: true }).click();
  await expect(page).toHaveURL(/\/mat-hang\/them-moi\?at=/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("font-family", /Noto Serif/);
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  const name = page.getByRole("textbox", { name: /Tên loại/ });
  await expect(name).toHaveAttribute("aria-invalid", "true");
  await name.fill("Lúa kiểm chứng198");
  await page.getByRole("textbox", { name: /Giống \/ phẩm cấp/ }).fill("giống thử198");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/mat-hang\/MH[0-9]{5}\?at=/);
  const log = page.getByRole("complementary", { name: "Log", exact: true });
  await expect(log).toContainText("Mã tham chiếu");
  await expect(log).toContainText("Lúa kiểm chứng198");
  await expect(page.getByRole("table", { name: "Lịch sử phiên bản mặt hàng" })).toBeVisible();
  await page.getByRole("link", { name: "Sửa", exact: true }).click();
  await expect(page).toHaveURL(/\/MH[0-9]{5}\/sua\?at=/);
  await page.getByRole("textbox", { name: /Tên loại/ }).fill("Lúa cập nhật198");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/mat-hang\/MH[0-9]{5}\?at=/);
  await expect(log).toContainText("Lúa kiểm chứng198");
  await expect(log).toContainText("Lúa cập nhật198");
  await page.getByRole("link", { name: "Danh mục mặt hàng", exact: true }).click();
  await expect(page.getByRole("table", { name: "Danh mục mặt hàng" })).toContainText("Lúa cập nhật198");
  await page.getByRole("tab", { name: "Nhóm hàng", exact: true }).click();
  await page.getByRole("link", { name: "Thêm nhóm hàng", exact: true }).click();
  await expect(page).toHaveURL(/\/nhom-hang\/them-moi\?at=.*tab=groups/);
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("textbox", { name: /Tên nhóm/ })).toHaveAttribute("aria-invalid", "true");
  await page.getByRole("textbox", { name: /Tên nhóm/ }).fill("Nhóm kiểm chứng198");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/nhom-hang\/nhom_[0-9]+\?at=/);
  await expect(log).toContainText("Chưa có lịch sử thay đổi nhóm hàng");
  await expect(log.getByRole("list")).toHaveCount(0);
  await page.getByRole("link", { name: "Sửa tên", exact: true }).click();
  await page.getByRole("textbox", { name: /Tên nhóm/ }).fill("Nhóm đã sửa198");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByText("Nhóm đã sửa198", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Sửa tên", exact: true }).click();
  await page.getByRole("link", { name: "Hủy", exact: true }).click();
  await expect(page).toHaveURL(/\/mat-hang\?at=.*tab=groups/);
  await expect(page.getByRole("tab", { name: "Nhóm hàng", exact: true })).toHaveAttribute("aria-selected", "true");
});
