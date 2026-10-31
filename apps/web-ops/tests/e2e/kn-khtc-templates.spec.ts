import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

async function login(page: Page) {
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
}

test("W11.65 Admin cấu hình, xem trước, phát hành và khôi phục một mẫu cố định", async ({ page }) => {
  await page.goto("/login");
  await login(page);
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login(page);
  await sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Mục lục mẫu báo cáo", exact: true }).click();
  const catalog = page.getByRole("table", { name: "Năm mẫu báo cáo KHTC", exact: true });
  await expect(catalog.getByRole("link")).toHaveCount(5);
  await catalog.getByRole("link", { name: "Mẫu 1 — Báo cáo tuần", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: /^Mẫu 1 — Báo cáo tuần/ })).toBeVisible();
  await page.getByRole("button", { name: "Lưu khung ban đầu", exact: true }).click();
  await expect(page.getByText("Bản nháp v1", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Thêm mục con", exact: true }).click();
  await page.getByRole("combobox", { name: "Mục cha", exact: true }).selectOption("A");
  await page.getByRole("textbox", { name: "Tên mục", exact: true }).fill("Mục cấu hình E2E 31");
  await page.getByRole("textbox", { name: "Khuôn câu", exact: true }).fill("Nội dung cấu hình vẫn giữ sau phát hành.");
  await page.getByRole("button", { name: "Lưu bản nháp", exact: true }).first().click();
  await expect(page.getByText("Bản nháp v2", { exact: true })).toBeVisible();
  const outline = page.getByRole("table", { name: "Mục lục mẫu", exact: true });
  await expect(outline).toContainText("Mục cấu hình E2E 31");
  await page.getByRole("button", { name: "Xem trước toàn mẫu", exact: true }).click();
  await expect(page.getByRole("article", { name: "Nội dung xem trước", exact: true })).toContainText("Nội dung cấu hình vẫn giữ sau phát hành.");

  const periods = page.getByRole("combobox", { name: "Kỳ bắt đầu áp dụng", exact: true });
  await periods.selectOption({ index: 1 });
  const reportName = await periods.locator("option:checked").textContent();
  expect(reportName).toBeTruthy();
  await page.getByRole("button", { name: "Phát hành bản nháp", exact: true }).click();
  const log = page.getByRole("complementary", { name: "Log", exact: true });
  await expect(log).toContainText("Phát hành v2");

  // The consumer uses the published source rather than the latest authoring draft.
  await sidebar(page).getByRole("link", { name: "Báo cáo KHTC", exact: true }).click();
  await page.getByRole("button", { name: reportName!, exact: true }).click();
  const compiled = page.getByRole("region", { name: "Bản báo cáo theo mẫu đã lưu", exact: true });
  await expect(compiled).toContainText("Mục cấu hình E2E 31");
  await expect(compiled).toContainText("Nội dung cấu hình vẫn giữ sau phát hành.");
  await expect(page.getByRole("button", { name: "Lưu bản soạn", exact: true })).toBeDisabled();
  await sidebar(page).getByRole("link", { name: "Mục lục mẫu báo cáo", exact: true }).click();
  await page.getByRole("link", { name: "Mẫu 1 — Báo cáo tuần", exact: true }).click();

  const versions = page.getByRole("combobox", { name: "Phiên bản nguồn", exact: true });
  const firstVersion = await versions.locator("option").filter({ hasText: /^Phiên bản 1 ·/ }).getAttribute("value");
  expect(firstVersion).toBeTruthy();
  await versions.selectOption(firstVersion!);
  page.once("dialog", dialog => void dialog.accept());
  await page.getByRole("button", { name: "Khôi phục thành bản nháp mới", exact: true }).click();
  await expect(page.getByText("Bản nháp v3", { exact: true })).toBeVisible();
  await expect(outline).not.toContainText("Mục cấu hình E2E 31");
  await expect(log).toContainText("Khôi phục thành bản nháp v3");
  await expect(log).toContainText("Phát hành v2");
});