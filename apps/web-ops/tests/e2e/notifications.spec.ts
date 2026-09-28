import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("chuông thông báo: số chưa đọc → mở chỉ đạo qua deep link → xác nhận đã đọc", async ({ page }) => {
  await loginAs(page, "xa", "/nhiem-vu");
  const count = page.getByTestId("bell-count");
  await expect(count).toHaveText("3");
  await page.getByTestId("bell").click();
  const panel = page.getByRole("dialog", { name: "Hộp thư thông báo" });
  await panel.getByRole("button", { name: /Chỉ đạo/ }).first().click();

  await expect(page).toHaveURL(/\/chi-dao\?id=/);
  const detail = page.getByTestId("directive-detail");
  await expect(detail.getByRole("heading", { name: /rầy nâu/ })).toBeVisible();
  await expect(count).toHaveText("2");
  await detail.getByRole("button", { name: "Xác nhận đã đọc" }).click();
  await expect(detail).toContainText("Đã xác nhận");
});

test("thông báo tài liệu và nhiệm vụ mở đúng màn hình; đánh dấu tất cả đã đọc", async ({ page }) => {
  await loginAs(page, "tinh", "/");
  await page.getByTestId("bell").click();
  await page.getByRole("dialog", { name: "Hộp thư thông báo" }).getByRole("button", { name: /Cảnh báo/ }).first().click();
  await expect(page).toHaveURL(/\/thu-vien\?doc=/);
  await expect(page.getByTestId("doc-detail")).toContainText("Cảnh báo khẩn");

  await page.getByTestId("bell").click();
  await page.getByRole("link", { name: "Xem tất cả thông báo" }).click();
  await expect(page).toHaveURL(/\/thong-bao$/);
  await page.getByRole("button", { name: /Nhiệm vụ leo thang/ }).click();
  await expect(page).toHaveURL(/\/nhiem-vu\?task=/);
  await expect(page.getByTestId("task-detail")).toContainText("Lịch sử");

  await page.getByTestId("bell").click();
  await page.getByRole("dialog", { name: "Hộp thư thông báo" }).getByRole("button", { name: "Đánh dấu tất cả đã đọc" }).click();
  await expect(page.getByTestId("bell-count")).toHaveCount(0);
});
