import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("HTX US-SUPPORT-01 / KN US-TASK-01: Quản lý HTX thấy yêu cầu hỗ trợ và trạng thái nhiệm vụ tự sinh", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: "Yêu cầu hỗ trợ và nhiệm vụ tự sinh" });
  const pest = table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" });
  await expect(pest).toContainText("Dịch hại");
  await expect(pest).toContainText("Khẩn");
  await expect(pest).toContainText("Mới — chờ phân công");
  await expect(table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" })).toContainText("Đang xử lý");
  await expect(page.getByRole("status")).toContainText(/\d+ yêu cầu · \d+ chưa xử lý xong/);

  await page.getByLabel("Trạng thái nhiệm vụ").selectOption("resolved");
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toHaveCount(0);
  await expect(table).toContainText("Đã hướng dẫn phun thuốc đặc trị");
});

test("cán bộ KN xã thấy yêu cầu hỗ trợ của nông dân trong xã", async ({ page }) => {
  await loginAs(page, "xa", "/htx/yeu-cau-ho-tro");
  await expect(page.getByRole("table", { name: "Yêu cầu hỗ trợ và nhiệm vụ tự sinh" }).getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toBeVisible();
});
