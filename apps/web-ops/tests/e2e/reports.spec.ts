import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("xuất báo cáo: 202 + việc xuất → theo dõi trạng thái → tải tệp; nhãn Số liệu tạm tính", async ({ page }) => {
  await loginAs(page, "tinh", "/bao-cao");
  await expect(page.getByRole("heading", { level: 1, name: "Báo cáo & xuất dữ liệu" })).toBeVisible();
  await page.getByLabel("Mẫu báo cáo").selectOption("kn.sowing_progress");
  await page.getByLabel("Kỳ").selectOption("month");
  await page.getByRole("radio", { name: /Excel/ }).check();
  await page.getByRole("button", { name: "Xuất báo cáo" }).click();

  const row = page.getByTestId("export-row").first();
  await expect(row).toContainText("Tiến độ gieo sạ theo đơn vị");
  await expect(row).toContainText("Sẵn sàng tải", { timeout: 15_000 });
  await expect(row).toContainText("Số liệu tạm tính");
  const download = page.waitForEvent("download");
  await row.getByRole("button", { name: "Tải xuống" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/^kn\.sowing_progress/);
});

test("mẫu CGH cho Cục: chọn vụ; PDF chỉ bật khi mẫu hỗ trợ", async ({ page }) => {
  await loginAs(page, "cuc", "/bao-cao");
  await page.getByLabel("Mẫu báo cáo").selectOption("cgh.season_compare");
  await expect(page.getByLabel("Vụ A", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Vụ B", { exact: true })).toBeVisible();
  await expect(page.getByRole("radio", { name: /PDF/ })).toBeDisabled();
});
